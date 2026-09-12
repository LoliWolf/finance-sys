package agentclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"finance-sys/internal/config"
)

type Client struct {
	httpClient *http.Client
	logger     *slog.Logger
}

func NewClient(httpClient *http.Client, logger *slog.Logger) *Client {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Client{httpClient: httpClient, logger: logger}
}

func (c *Client) ResolveDocument(ctx context.Context, cfg config.AgentConfig, request ResolveDocumentRequest) (*ResolveDocumentResponse, error) {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, fmt.Errorf("agent endpoint is required")
	}
	if cfg.TimeoutMS <= 0 {
		return nil, fmt.Errorf("agent timeout_ms must be positive")
	}
	if cfg.MaxRetries < 0 {
		return nil, fmt.Errorf("agent max_retries must be zero or positive")
	}
	// Retries share the same budget; never launch another long-running request
	// after the caller has already timed out.
	ctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutMS)*time.Millisecond)
	defer cancel()
	if request.SchemaVersion == "" {
		request.SchemaVersion = RequestSchemaVersion
	}

	raw, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	attempts := cfg.MaxRetries + 1
	var lastErr error
	usedAttempts := 0
	for attempt := 1; attempt <= attempts; attempt++ {
		if ctx.Err() != nil {
			lastErr = ctx.Err()
			break
		}
		usedAttempts = attempt
		response, err := c.resolveDocumentOnce(ctx, cfg, raw)
		if err == nil {
			return response, nil
		}
		lastErr = err
		if ctx.Err() != nil || !isRetryableAgentError(err) || attempt == attempts {
			break
		}
		if c.logger != nil {
			c.logger.WarnContext(ctx, "agent resolve document failed; retrying", "attempt", attempt, "max_attempts", attempts, "error", err.Error())
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("agent request failed")
	}
	return nil, fmt.Errorf("agent resolve document failed after %d attempts: %w", usedAttempts, lastErr)
}

func (c *Client) resolveDocumentOnce(ctx context.Context, cfg config.AgentConfig, raw []byte) (*ResolveDocumentResponse, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutMS)*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(timeoutCtx, http.MethodPost, cfg.Endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if deadline, ok := timeoutCtx.Deadline(); ok {
		req.Header.Set("X-Finance-Deadline-Unix-Ms", strconv.FormatInt(deadline.UnixMilli(), 10))
	}
	if cfg.Auth.Enabled {
		req.Header.Set(cfg.Auth.HeaderName, cfg.Auth.StaticToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, retryableAgentError{err: err}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, retryableAgentError{err: err}
	}
	if resp.StatusCode >= http.StatusInternalServerError {
		return nil, retryableAgentError{err: fmt.Errorf("agent http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))}
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("agent http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var response ResolveDocumentResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, retryableAgentError{err: fmt.Errorf("decode agent response: %w", err)}
	}
	return &response, nil
}

type retryableAgentError struct {
	err error
}

func (e retryableAgentError) Error() string {
	return e.err.Error()
}

func (e retryableAgentError) Unwrap() error {
	return e.err
}

func isRetryableAgentError(err error) bool {
	_, ok := err.(retryableAgentError)
	return ok
}
