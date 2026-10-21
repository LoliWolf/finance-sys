package tradingagentclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"finance-sys/internal/config"
	tradingdomain "finance-sys/internal/trading/domain"
)

type Client struct {
	runtime *config.Runtime
	client  *http.Client
}

func New(runtime *config.Runtime) *Client {
	return &Client{runtime: runtime, client: &http.Client{}}
}

func (c *Client) Run(ctx context.Context, request tradingdomain.AgentRunRequest) (*tradingdomain.AgentRunResponse, error) {
	cfg := c.runtime.Config()
	if cfg == nil || !cfg.Trading.Agent.Enabled {
		return nil, fmt.Errorf("trading agent is disabled")
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("marshal trading agent request: %w", err)
	}
	attempts := cfg.Trading.Agent.MaxRetries + 1
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.Trading.Agent.TimeoutMS)*time.Millisecond)
		result, runErr := c.doRun(attemptCtx, cfg.Trading.Agent.Endpoint, cfg.Trading.Agent.InternalToken, body)
		cancel()
		if runErr == nil {
			if result.SchemaVersion != cfg.Trading.Agent.SchemaVersion || result.RunKey != request.RunKey {
				return nil, fmt.Errorf("trading agent response identity mismatch")
			}
			return result, nil
		}
		lastErr = runErr
	}
	return nil, lastErr
}

func (c *Client) doRun(ctx context.Context, endpoint, token string, body []byte) (*tradingdomain.AgentRunResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call trading agent: %w", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("trading agent returned %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	var result tradingdomain.AgentRunResponse
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return nil, fmt.Errorf("decode trading agent response: %w", err)
	}
	return &result, nil
}
