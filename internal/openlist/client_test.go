package openlist

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"finance-sys/internal/config"
)

func TestDiscoverSupportsDirectAndMonthDateDirectories(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.July, 26, 20, 0, 0, 0, location)
	modifiedNow := now.Format(time.RFC3339)
	oldModified := now.AddDate(0, 0, -60).Format(time.RFC3339)
	lists := map[string][]map[string]any{
		"/articles": {
			{"name": "20260723", "is_dir": true, "modified": modifiedNow},
			{"name": "7月", "is_dir": true, "modified": modifiedNow},
			{"name": "20260101", "is_dir": true, "modified": modifiedNow},
			{"name": "20251231", "is_dir": true, "modified": oldModified},
			{"name": "ignore", "is_dir": true, "modified": modifiedNow},
		},
		"/articles/7月": {
			{"name": "20260724", "is_dir": true, "modified": modifiedNow},
		},
		"/articles/20260723": {
			{"name": "直接目录7.23文章.pdf", "is_dir": false, "size": 101, "modified": modifiedNow},
			{"name": "ignore.txt", "is_dir": false, "size": 1, "modified": modifiedNow},
		},
		"/articles/7月/20260724": {
			{"name": "月份目录7.24文章.pdf", "is_dir": false, "size": 102, "modified": modifiedNow},
		},
		"/articles/20260101": {
			{"name": "迟到转存1.1文章.pdf", "is_dir": false, "size": 103, "modified": modifiedNow},
		},
	}
	server := newOpenListTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/login" {
			writeOpenListResponse(t, w, map[string]any{"token": "test-token"})
			return
		}
		if r.Header.Get("Authorization") != "test-token" {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}
		var request struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		writeOpenListResponse(t, w, map[string]any{"content": lists[request.Path], "total": len(lists[request.Path])})
	})
	defer server.Close()

	client := mustOpenListClient(t, server.URL)
	articles, err := client.Discover(context.Background(), "/articles", now, 14, false)
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 0, len(articles))
	for _, article := range articles {
		paths = append(paths, article.Path)
	}
	sort.Strings(paths)
	want := []string{
		"/articles/20260723/直接目录7.23文章.pdf",
		"/articles/7月/20260724/月份目录7.24文章.pdf",
	}
	sort.Strings(want)
	if len(paths) != len(want) {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("paths = %#v, want %#v", paths, want)
		}
	}
	for _, article := range articles {
		dateDirectory := strings.ReplaceAll(article.ArticleDate.Format(time.DateOnly), "-", "")
		if article.ArticleDate.Location() != time.UTC || !strings.Contains(article.Path, "/"+dateDirectory+"/") {
			t.Fatalf("article date %v does not preserve directory date for %s", article.ArticleDate, article.Path)
		}
	}

	fullScanArticles, err := client.Discover(context.Background(), "/articles", now, 14, true)
	if err != nil {
		t.Fatal(err)
	}
	fullScanPaths := make([]string, 0, len(fullScanArticles))
	for _, article := range fullScanArticles {
		fullScanPaths = append(fullScanPaths, article.Path)
	}
	sort.Strings(fullScanPaths)
	if len(fullScanPaths) != 3 || fullScanPaths[0] != "/articles/20260101/迟到转存1.1文章.pdf" {
		t.Fatalf("full scan paths = %#v", fullScanPaths)
	}
}

func TestDownloadDoesNotForwardAuthorizationToExternalRawURL(t *testing.T) {
	rawServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("authorization forwarded to raw host: %q", got)
		}
		_, _ = w.Write([]byte("pdf-content"))
	}))
	defer rawServer.Close()

	server := newOpenListTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth/login":
			writeOpenListResponse(t, w, map[string]any{"token": "test-token"})
		case "/api/fs/get":
			if r.Header.Get("Authorization") != "test-token" {
				http.Error(w, "missing token", http.StatusUnauthorized)
				return
			}
			writeOpenListResponse(t, w, map[string]any{"raw_url": rawServer.URL + "/article.pdf"})
		default:
			http.NotFound(w, r)
		}
	})
	defer server.Close()

	client := mustOpenListClient(t, server.URL)
	content, err := client.Download(context.Background(), "/articles/20260723/a.pdf", 1024)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "pdf-content" {
		t.Fatalf("content = %q", content)
	}
}

func mustOpenListClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(config.OpenListDocumentSourceConfig{
		BaseURL:          baseURL,
		Username:         "reader",
		Password:         "secret",
		RequestTimeoutMS: 5000,
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func newOpenListTestServer(t *testing.T, handler func(http.ResponseWriter, *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(handler))
}

func writeOpenListResponse(t *testing.T, w http.ResponseWriter, data any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"code": 200, "message": "success", "data": data}); err != nil {
		t.Fatal(err)
	}
}
