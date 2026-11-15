package xueqiudesk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"finance-sys/internal/config"

	"github.com/stretchr/testify/require"
)

func newTestClient(t *testing.T, handler http.HandlerFunc, token string) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := New(config.XueqiuDocumentSourceConfig{BaseURL: server.URL + "/", APIToken: token, RequestTimeoutMS: 2000})
	require.NoError(t, err)
	return client
}

func TestPendingQueriesAscendingPendingPage(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/articles", r.URL.Path)
		q := r.URL.Query()
		require.Equal(t, "pending", q.Get("state"))
		require.Equal(t, "asc", q.Get("order"))
		require.Equal(t, "50", q.Get("limit"))
		require.Equal(t, "7", q.Get("after_id"))
		require.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"items":[{"id":8,"status_id":123,"title":"标题","author":{"id":9,"name":"博主"},
			"text":"看好 $贵州茅台(SH600519)$","url":"https://xueqiu.com/9/123","symbols":["SH600519"],
			"published_at":"2026-11-15T21:30:00+08:00","origin":"new"}],"has_more":false,"next_after_id":8}`))
	}, "secret")
	page, err := client.Pending(context.Background(), 7, 50)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	item := page.Items[0]
	require.Equal(t, int64(123), item.StatusID)
	require.Equal(t, "博主", item.Author.Name)
	require.Equal(t, []string{"SH600519"}, item.Symbols)
	require.NotNil(t, item.PublishedAt)
	require.Equal(t, int64(8), page.NextAfterID)
}

func TestAckSplitsIntoDeskSizedBatches(t *testing.T) {
	var batches []int
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/acks", r.URL.Path)
		require.Empty(t, r.Header.Get("Authorization"))
		var body struct {
			IDs []int64 `json:"ids"`
			By  string  `json:"by"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "financesys", body.By)
		batches = append(batches, len(body.IDs))
		_ = json.NewEncoder(w).Encode(map[string]any{"acked": body.IDs})
	}, "")
	ids := make([]int64, 1200)
	for i := range ids {
		ids[i] = int64(i + 1)
	}
	result, err := client.Ack(context.Background(), ids, "financesys")
	require.NoError(t, err)
	require.Equal(t, []int{500, 500, 200}, batches)
	require.Len(t, result.Acked, 1200)
}

func TestErrorsCarryStatusAndBody(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"missing or invalid API token"}`, http.StatusUnauthorized)
	}, "")
	_, err := client.Pending(context.Background(), 0, 10)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "http 401") && strings.Contains(err.Error(), "invalid API token"), err.Error())
}

func TestNewValidatesConfig(t *testing.T) {
	_, err := New(config.XueqiuDocumentSourceConfig{BaseURL: "127.0.0.1:30010", RequestTimeoutMS: 1})
	require.Error(t, err)
	_, err = New(config.XueqiuDocumentSourceConfig{BaseURL: "http://127.0.0.1:30010"})
	require.Error(t, err)
}
