package tradingbridgeclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"finance-sys/internal/config"
	tradingdomain "finance-sys/internal/trading/domain"
)

type Client struct {
	runtime *config.Runtime
}

func New(runtime *config.Runtime) *Client { return &Client{runtime: runtime} }

func (c *Client) Health(ctx context.Context) (*tradingdomain.BridgeHealth, error) {
	var result tradingdomain.BridgeHealth
	if err := c.do(ctx, http.MethodGet, c.config().HealthPath, nil, nil, "", &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) PlaceOrder(ctx context.Context, idempotencyKey string, request tradingdomain.BridgeOrderRequest) (*tradingdomain.BridgeCommandResponse, error) {
	var result tradingdomain.BridgeCommandResponse
	if err := c.do(ctx, http.MethodPost, "/v1/orders", nil, request, idempotencyKey, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) CancelOrder(ctx context.Context, clientOrderID, idempotencyKey string) (*tradingdomain.BridgeCommandResponse, error) {
	var result tradingdomain.BridgeCommandResponse
	path := "/v1/orders/" + url.PathEscape(clientOrderID) + "/cancel"
	if err := c.do(ctx, http.MethodPost, path, nil, map[string]string{"client_order_id": clientOrderID}, idempotencyKey, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) RefreshSnapshot(ctx context.Context, idempotencyKey string) (*tradingdomain.BridgeCommandResponse, error) {
	keySuffix, err := commandKeySuffix(idempotencyKey)
	if err != nil {
		return nil, err
	}
	var result tradingdomain.BridgeCommandResponse
	if err := c.do(ctx, http.MethodPost, "/v1/refresh-snapshot", nil, map[string]string{"client_order_id": "snapshot-" + keySuffix, "reason": "finance_sys"}, idempotencyKey, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) RefreshQuotes(ctx context.Context, symbols []string, idempotencyKey string) (*tradingdomain.BridgeCommandResponse, error) {
	keySuffix, err := commandKeySuffix(idempotencyKey)
	if err != nil {
		return nil, err
	}
	var result tradingdomain.BridgeCommandResponse
	if err := c.do(ctx, http.MethodPost, "/v1/quotes/refresh", nil, map[string]any{"client_order_id": "quotes-" + keySuffix, "symbols": symbols}, idempotencyKey, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func commandKeySuffix(idempotencyKey string) (string, error) {
	keySuffix := strings.TrimSpace(idempotencyKey)
	if len(keySuffix) > 24 {
		keySuffix = keySuffix[:24]
	}
	if keySuffix == "" {
		return "", fmt.Errorf("Bridge command idempotency key is empty")
	}
	return keySuffix, nil
}

func (c *Client) Quotes(ctx context.Context, symbols []string) ([]tradingdomain.QuoteSnapshot, error) {
	query := url.Values{}
	query.Set("symbols", strings.Join(symbols, ","))
	var result []tradingdomain.QuoteSnapshot
	if err := c.do(ctx, http.MethodGet, "/v1/quotes", query, nil, "", &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) SetKillSwitch(ctx context.Context, enabled bool, reason, idempotencyKey string) error {
	return c.do(ctx, http.MethodPost, "/v1/kill-switch", nil, map[string]any{"enabled": enabled, "reason": reason}, idempotencyKey, nil)
}

func (c *Client) ReconciliationSnapshot(ctx context.Context, cursor string) (*tradingdomain.ReconciliationSnapshot, error) {
	query := url.Values{}
	if cursor != "" {
		query.Set("cursor", cursor)
	}
	var result tradingdomain.ReconciliationSnapshot
	if err := c.do(ctx, http.MethodGet, "/v1/reconciliation-snapshot", query, nil, "", &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) Account(ctx context.Context) (*tradingdomain.AccountSnapshot, error) {
	var result tradingdomain.AccountSnapshot
	if err := c.do(ctx, http.MethodGet, "/v1/account", nil, nil, "", &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) Positions(ctx context.Context) ([]tradingdomain.PositionSnapshot, error) {
	var result []tradingdomain.PositionSnapshot
	if err := c.do(ctx, http.MethodGet, "/v1/positions", nil, nil, "", &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, request any, idempotencyKey string, response any) error {
	cfg := c.config()
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return fmt.Errorf("trading Bridge base URL is empty")
	}
	body := []byte{}
	var err error
	if request != nil {
		body, err = json.Marshal(request)
		if err != nil {
			return err
		}
	}
	endpoint := strings.TrimRight(cfg.BaseURL, "/") + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	timeout := time.Duration(cfg.RequestTimeoutMS) * time.Millisecond
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if request != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	nonce, err := newNonce()
	if err != nil {
		return err
	}
	canonical := CanonicalString(method, path, query, body, timestamp, nonce)
	req.Header.Set("X-FS-Key-Id", cfg.HMAC.KeyID)
	req.Header.Set("X-FS-Timestamp", timestamp)
	req.Header.Set("X-FS-Nonce", nonce)
	req.Header.Set("X-FS-Signature", Sign(cfg.HMAC.Secret, canonical))

	httpClient, err := newHTTPClient(cfg)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("call trading Bridge: %w", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("trading Bridge returned %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	if response == nil || len(responseBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(responseBody, response); err != nil {
		return fmt.Errorf("decode trading Bridge response: %w", err)
	}
	return nil
}

func (c *Client) config() config.TradingBridgeConfig {
	cfg := c.runtime.Config()
	if cfg == nil {
		return config.TradingBridgeConfig{}
	}
	return cfg.Trading.Bridge
}

func newHTTPClient(cfg config.TradingBridgeConfig) (*http.Client, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if cfg.TLS.Verify {
		caFile, err := resolveCAFile(cfg.TLS.CAFile)
		if err != nil {
			return nil, err
		}
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read trading Bridge CA: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("parse trading Bridge CA")
		}
		tlsConfig.RootCAs = pool
	} else {
		tlsConfig.InsecureSkipVerify = true // #nosec G402 -- only permitted by explicit Nacos config for local diagnostics.
	}
	transport := &http.Transport{TLSClientConfig: tlsConfig}
	return &http.Client{Transport: transport, Timeout: time.Duration(cfg.RequestTimeoutMS) * time.Millisecond}, nil
}

func resolveCAFile(configured string) (string, error) {
	configured = filepath.Clean(configured)
	if filepath.IsAbs(configured) {
		return configured, nil
	}
	candidates := []string{configured}
	if executable, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(executable), "..", configured))
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return filepath.Clean(candidate), nil
		}
	}
	return "", fmt.Errorf("trading Bridge CA %q was not found relative to the working directory or executable", configured)
}

func newNonce() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate HMAC nonce: %w", err)
	}
	return hex.EncodeToString(raw), nil
}
