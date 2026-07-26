package openlist

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/utils"
)

const userAgent = "finance-sys-openlist/1.0"

type Client struct {
	baseURL    *url.URL
	username   string
	password   string
	httpClient *http.Client
	mu         sync.Mutex
	token      string
}

type Object struct {
	Name      string            `json:"name"`
	Size      int64             `json:"size"`
	IsDir     bool              `json:"is_dir"`
	Modified  time.Time         `json:"modified"`
	Created   time.Time         `json:"created"`
	Sign      string            `json:"sign"`
	HashInfo  string            `json:"hashinfo"`
	HashInfos map[string]string `json:"hash_info"`
}

type DateDirectory struct {
	Path         string
	ArticleDate  time.Time
	Modified     time.Time
	ArticleFiles []RemoteArticle
}

type RemoteArticle struct {
	Path          string
	Name          string
	ArticleDate   time.Time
	Size          int64
	Modified      time.Time
	SourceVersion string
}

type apiResponse[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    T      `json:"data"`
}

type loginData struct {
	Token string `json:"token"`
}

type listData struct {
	Content []Object `json:"content"`
	Total   int64    `json:"total"`
}

type getData struct {
	Object
	RawURL string `json:"raw_url"`
}

func New(cfg config.OpenListDocumentSourceConfig) (*Client, error) {
	baseURL, err := url.Parse(strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"))
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, fmt.Errorf("parse OpenList base URL: %w", err)
	}
	timeout := time.Duration(cfg.RequestTimeoutMS) * time.Millisecond
	return &Client{
		baseURL:  baseURL,
		username: strings.TrimSpace(cfg.Username),
		password: cfg.Password,
		httpClient: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) > 0 && !sameOrigin(req.URL, via[0].URL) {
					req.Header.Del("Authorization")
				}
				return nil
			},
		},
	}, nil
}

func (c *Client) List(ctx context.Context, objectPath string) ([]Object, error) {
	request := map[string]any{
		"path":     cleanObjectPath(objectPath),
		"password": "",
		"page":     1,
		"per_page": 0,
		"refresh":  false,
	}
	var response apiResponse[listData]
	if err := c.api(ctx, http.MethodPost, "/api/fs/list", request, &response); err != nil {
		return nil, err
	}
	return response.Data.Content, nil
}

func (c *Client) Discover(ctx context.Context, rootPath string, now time.Time, lookbackDays int, fullScan bool) ([]RemoteArticle, error) {
	rootPath = cleanObjectPath(rootPath)
	rootObjects, err := c.List(ctx, rootPath)
	if err != nil {
		return nil, fmt.Errorf("list OpenList root %s: %w", rootPath, err)
	}

	dateDirectories := make([]DateDirectory, 0)
	for _, object := range rootObjects {
		if !object.IsDir {
			continue
		}
		if articleDate, ok := parseDateDirectory(object.Name, now.Location()); ok {
			dateDirectories = append(dateDirectories, DateDirectory{Path: joinObjectPath(rootPath, object.Name), ArticleDate: articleDate, Modified: object.Modified})
			continue
		}
		if !strings.HasSuffix(strings.TrimSpace(object.Name), "月") {
			continue
		}
		monthPath := joinObjectPath(rootPath, object.Name)
		monthObjects, listErr := c.List(ctx, monthPath)
		if listErr != nil {
			return nil, fmt.Errorf("list OpenList month directory %s: %w", monthPath, listErr)
		}
		for _, child := range monthObjects {
			if !child.IsDir {
				continue
			}
			articleDate, ok := parseDateDirectory(child.Name, now.Location())
			if !ok {
				continue
			}
			dateDirectories = append(dateDirectories, DateDirectory{Path: joinObjectPath(monthPath, child.Name), ArticleDate: articleDate, Modified: child.Modified})
		}
	}

	cutoffDate := beginningOfDay(now).AddDate(0, 0, -lookbackDays)
	articles := make([]RemoteArticle, 0)
	seenDirectories := make(map[string]struct{}, len(dateDirectories))
	for _, directory := range dateDirectories {
		if _, exists := seenDirectories[directory.Path]; exists {
			continue
		}
		seenDirectories[directory.Path] = struct{}{}
		// Date directories are the source of truth for scheduled ingestion. NAS
		// remounts and bulk syncs can rewrite directory mtimes, so using mtime as
		// a fallback here could unexpectedly enqueue a full historical archive.
		if !fullScan && directory.ArticleDate.Before(cutoffDate) {
			continue
		}
		objects, listErr := c.List(ctx, directory.Path)
		if listErr != nil {
			return nil, fmt.Errorf("list OpenList date directory %s: %w", directory.Path, listErr)
		}
		for _, object := range objects {
			if object.IsDir || !strings.EqualFold(path.Ext(object.Name), ".pdf") {
				continue
			}
			articles = append(articles, RemoteArticle{
				Path:          joinObjectPath(directory.Path, object.Name),
				Name:          object.Name,
				ArticleDate:   directory.ArticleDate,
				Size:          object.Size,
				Modified:      object.Modified,
				SourceVersion: sourceVersion(object),
			})
		}
	}
	sort.Slice(articles, func(i, j int) bool {
		if articles[i].ArticleDate.Equal(articles[j].ArticleDate) {
			return articles[i].Path < articles[j].Path
		}
		return articles[i].ArticleDate.Before(articles[j].ArticleDate)
	})
	return articles, nil
}

func (c *Client) Download(ctx context.Context, objectPath string, maxBytes int64) ([]byte, error) {
	request := map[string]any{"path": cleanObjectPath(objectPath), "password": ""}
	var response apiResponse[getData]
	if err := c.api(ctx, http.MethodPost, "/api/fs/get", request, &response); err != nil {
		return nil, err
	}
	if strings.TrimSpace(response.Data.RawURL) == "" {
		return nil, fmt.Errorf("OpenList returned an empty raw_url for %s", objectPath)
	}
	rawURL, err := url.Parse(response.Data.RawURL)
	if err != nil {
		return nil, fmt.Errorf("parse OpenList raw_url for %s: %w", objectPath, err)
	}
	if !rawURL.IsAbs() {
		rawURL = c.baseURL.ResolveReference(rawURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if sameOrigin(rawURL, c.baseURL) {
		token, tokenErr := c.authToken(ctx)
		if tokenErr != nil {
			return nil, tokenErr
		}
		req.Header.Set("Authorization", token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download OpenList object %s: %w", objectPath, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("download OpenList object %s: http %d: %s", objectPath, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if maxBytes <= 0 {
		return nil, fmt.Errorf("max download size must be positive")
	}
	if resp.ContentLength > maxBytes {
		return nil, fmt.Errorf("OpenList object %s is %d bytes, exceeds limit %d", objectPath, resp.ContentLength, maxBytes)
	}
	content, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read OpenList object %s: %w", objectPath, err)
	}
	if int64(len(content)) > maxBytes {
		return nil, fmt.Errorf("OpenList object %s exceeds limit %d", objectPath, maxBytes)
	}
	return content, nil
}

func (c *Client) api(ctx context.Context, method string, endpoint string, requestBody any, response any) error {
	for attempt := 0; attempt < 2; attempt++ {
		token, err := c.authToken(ctx)
		if err != nil {
			return err
		}
		status, err := c.doAPI(ctx, method, endpoint, token, requestBody, response)
		if err == nil {
			return nil
		}
		if status != http.StatusUnauthorized || attempt > 0 {
			return err
		}
		c.clearToken(token)
	}
	return fmt.Errorf("OpenList request failed after re-authentication")
}

func (c *Client) doAPI(ctx context.Context, method string, endpoint string, token string, requestBody any, response any) (int, error) {
	raw, err := json.Marshal(requestBody)
	if err != nil {
		return 0, err
	}
	reqURL := c.baseURL.ResolveReference(&url.URL{Path: endpoint})
	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), bytes.NewReader(raw))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("OpenList %s %s: %w", method, endpoint, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	if err != nil {
		return resp.StatusCode, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return resp.StatusCode, fmt.Errorf("OpenList %s %s: http %d: %s", method, endpoint, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, response); err != nil {
		return resp.StatusCode, fmt.Errorf("decode OpenList %s response: %w", endpoint, err)
	}
	code, message := responseCode(response)
	if code != http.StatusOK {
		return code, fmt.Errorf("OpenList %s: code %d: %s", endpoint, code, message)
	}
	return resp.StatusCode, nil
}

func (c *Client) authToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" {
		return c.token, nil
	}
	request := map[string]string{"username": c.username, "password": c.password, "otp_code": ""}
	var response apiResponse[loginData]
	status, err := c.doAPI(ctx, http.MethodPost, "/api/auth/login", "", request, &response)
	if err != nil {
		return "", fmt.Errorf("OpenList login failed (http %d): %w", status, err)
	}
	if strings.TrimSpace(response.Data.Token) == "" {
		return "", fmt.Errorf("OpenList login returned an empty token")
	}
	c.token = response.Data.Token
	return c.token, nil
}

func (c *Client) clearToken(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token == token {
		c.token = ""
	}
}

func cleanObjectPath(value string) string {
	cleaned := path.Clean("/" + strings.TrimSpace(value))
	if cleaned == "." {
		return "/"
	}
	return cleaned
}

func joinObjectPath(parent string, name string) string {
	return cleanObjectPath(path.Join(parent, name))
}

func parseDateDirectory(name string, _ *time.Location) (time.Time, bool) {
	if len(name) != 8 {
		return time.Time{}, false
	}
	// MySQL DATE values are calendar dates without a timezone. The project DSN
	// intentionally uses the driver's UTC default, so keep date-only values at
	// UTC midnight; converting an Asia/Shanghai midnight to UTC would otherwise
	// persist the previous calendar day.
	parsed, err := time.ParseInLocation("20060102", name, time.UTC)
	return parsed, err == nil
}

func beginningOfDay(value time.Time) time.Time {
	year, month, day := value.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, value.Location())
}

func sourceVersion(object Object) string {
	parts := make([]string, 0, len(object.HashInfos)+1)
	if strings.TrimSpace(object.HashInfo) != "" {
		parts = append(parts, strings.TrimSpace(object.HashInfo))
	}
	keys := make([]string, 0, len(object.HashInfos))
	for key := range object.HashInfos {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts = append(parts, key+"="+object.HashInfos[key])
	}
	if len(parts) > 0 {
		return "hash:" + utils.SHA256Hex([]byte(strings.Join(parts, "|")))
	}
	return fmt.Sprintf("size:%d:modified:%s", object.Size, object.Modified.UTC().Format(time.RFC3339Nano))
}

func sameOrigin(left *url.URL, right *url.URL) bool {
	return strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
}

func responseCode(response any) (int, string) {
	raw, err := json.Marshal(response)
	if err != nil {
		return 0, err.Error()
	}
	var header struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return 0, err.Error()
	}
	return header.Code, header.Message
}
