// Package wiredesk reads the pending-ACK queue of a subscription wire desk
// (the Xueqiu desk xueqiu-tool and the Tonghuashun desk ths-wire-desk share
// one API contract) and ACKs posts once FinanceSys has stored them. Desks only
// keep original posts (no reposts or replies).
package wiredesk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"finance-sys/internal/config"
)

// maxAckBatch mirrors the desk's POST /api/acks limit.
const maxAckBatch = 500

// Author is the post's author on the source platform.
type Author struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Domain string `json:"domain,omitempty"`
}

// Image is one picture attached to a post.
type Image struct {
	URL string `json:"url"`
}

// Article is one stored post as returned by GET /api/articles.
type Article struct {
	ID           int64      `json:"id"`
	StatusID     int64      `json:"status_id"`
	Title        string     `json:"title"`
	DisplayTitle string     `json:"display_title"`
	Author       Author     `json:"author"`
	Text         string     `json:"text"`
	URL          string     `json:"url"`
	Images       []Image    `json:"images"`
	Symbols      []string   `json:"symbols"`
	Partial      bool       `json:"partial"`
	Origin       string     `json:"origin"`
	PublishedAt  *time.Time `json:"published_at"`
	SyncedAt     time.Time  `json:"synced_at"`
	Acked        bool       `json:"acked"`
}

// Page is one ascending page of pending posts.
type Page struct {
	Items       []Article `json:"items"`
	HasMore     bool      `json:"has_more"`
	NextAfterID int64     `json:"next_after_id"`
}

// AckResult is the desk's answer to a batch ACK.
type AckResult struct {
	Acked        []int64 `json:"acked"`
	AlreadyAcked []int64 `json:"already_acked"`
	Missing      []int64 `json:"missing"`
}

// Client talks to one wire desk.
type Client struct {
	name    string
	baseURL *url.URL
	token   string
	http    *http.Client
}

// New builds a client from the FinanceSys source config; name labels errors
// (for example "xueqiu desk").
func New(name string, cfg config.WireDeskDocumentSourceConfig) (*Client, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "wire desk"
	}
	raw := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	base, err := url.Parse(raw)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return nil, fmt.Errorf("%s base_url %q must be an absolute http(s) URL", name, cfg.BaseURL)
	}
	if cfg.RequestTimeoutMS <= 0 {
		return nil, fmt.Errorf("%s request_timeout_ms must be positive", name)
	}
	return &Client{
		name:    name,
		baseURL: base,
		token:   strings.TrimSpace(cfg.APIToken),
		http:    &http.Client{Timeout: time.Duration(cfg.RequestTimeoutMS) * time.Millisecond},
	}, nil
}

// Pending returns pending posts with id > afterID, oldest first.
func (c *Client) Pending(ctx context.Context, afterID int64, limit int) (Page, error) {
	query := url.Values{"state": {"pending"}, "order": {"asc"}, "limit": {strconv.Itoa(limit)}}
	if afterID > 0 {
		query.Set("after_id", strconv.FormatInt(afterID, 10))
	}
	var page Page
	if err := c.do(ctx, http.MethodGet, "/api/articles", query, nil, &page); err != nil {
		return Page{}, err
	}
	return page, nil
}

// Ack marks posts as consumed; ids are sent in batches the desk accepts.
func (c *Client) Ack(ctx context.Context, ids []int64, by string) (AckResult, error) {
	var total AckResult
	for start := 0; start < len(ids); start += maxAckBatch {
		end := start + maxAckBatch
		if end > len(ids) {
			end = len(ids)
		}
		var result AckResult
		body := map[string]any{"ids": ids[start:end], "by": by}
		if err := c.do(ctx, http.MethodPost, "/api/acks", nil, body, &result); err != nil {
			return total, err
		}
		total.Acked = append(total.Acked, result.Acked...)
		total.AlreadyAcked = append(total.AlreadyAcked, result.AlreadyAcked...)
		total.Missing = append(total.Missing, result.Missing...)
	}
	return total, nil
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	target := *c.baseURL
	target.Path = strings.TrimRight(target.Path, "/") + path
	target.RawQuery = query.Encode()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("%s %s %s: encode body: %w", c.name, method, path, err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, target.String(), reader)
	if err != nil {
		return fmt.Errorf("%s %s %s: %w", c.name, method, path, err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s %s: %w", c.name, method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return fmt.Errorf("%s %s %s: read body: %w", c.name, method, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s %s: http %d: %s", c.name, method, path, resp.StatusCode, snippet(raw))
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s %s %s: decode: %w", c.name, method, path, err)
	}
	return nil
}

func snippet(raw []byte) string {
	text := strings.ToValidUTF8(strings.TrimSpace(string(raw)), "?")
	runes := []rune(text)
	if len(runes) > 200 {
		return string(runes[:200]) + "..."
	}
	return text
}
