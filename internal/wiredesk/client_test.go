package wiredesk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"finance-sys/internal/config"

	"github.com/stretchr/testify/require"
)

func newTestClient(t *testing.T, handler http.HandlerFunc, token string) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := New("xueqiu desk", config.WireDeskDocumentSourceConfig{BaseURL: server.URL + "/", APIToken: token, RequestTimeoutMS: 2000})
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

func TestEastmoneyAuthorIDsAreDecodedWithoutFloatingPointRounding(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"items":[{"id":1,"status_id":1799500915,"author":{"id":9688305908969519,"uid":"9688305908969519","name":"东方财富作者"},"text":"看好中际旭创","images":[{"url":"https://gbres.dfcfw.com/a.jpg"}],"published_at":"2026-11-21T09:00:00+08:00"}],"has_more":false}`))
	}, "")
	page, err := client.Pending(context.Background(), 0, 20)
	require.NoError(t, err)
	require.Equal(t, int64(9688305908969519), page.Items[0].Author.ID)
	require.Equal(t, "东方财富作者", page.Items[0].Author.Name)
	require.Len(t, page.Items[0].Images, 1)
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
	require.True(t, strings.HasPrefix(err.Error(), "xueqiu desk GET /api/articles"), err.Error())
}

// The Tonghuashun desk returns the same contract with avatar/thumb fields and
// no symbols; unknown fields must not break decoding.
func TestPendingDecodesTonghuashunDeskArticles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"items":[{"id":3,"status_id":728850093,"title":"盘前提示","display_title":"盘前提示",
			"author":{"id":429085934,"name":"首创证券伍彩云","avatar":"https://u.thsi.cn/avatar/1.gif"},
			"text":"关注半导体设备","html":"<p>关注半导体设备</p>","url":"https://t.10jqka.com.cn/lgt/short_view/index.html?contentId=x",
			"images":[{"url":"https://u.thsi.cn/a.png","thumb":"https://u.thsi.cn/a.png"}],"origin":"new",
			"published_at":"2026-11-20T08:45:01+08:00","synced_at":"2026-11-20T08:50:00+08:00","acked":false,"partial":false}],
			"has_more":true,"next_after_id":3}`))
	}))
	t.Cleanup(server.Close)
	client, err := New("ths desk", config.WireDeskDocumentSourceConfig{BaseURL: server.URL, RequestTimeoutMS: 2000})
	require.NoError(t, err)
	page, err := client.Pending(context.Background(), 0, 20)
	require.NoError(t, err)
	require.True(t, page.HasMore)
	require.Len(t, page.Items, 1)
	item := page.Items[0]
	require.Equal(t, int64(728850093), item.StatusID)
	require.Equal(t, "首创证券伍彩云", item.Author.Name)
	require.Len(t, item.Images, 1)
	require.Empty(t, item.Symbols)
	require.Equal(t, "2026-11-20T00:45:01Z", item.PublishedAt.UTC().Format(time.RFC3339))
}

func TestNewValidatesConfig(t *testing.T) {
	_, err := New("ths desk", config.WireDeskDocumentSourceConfig{BaseURL: "127.0.0.1:30013", RequestTimeoutMS: 1})
	require.ErrorContains(t, err, "ths desk base_url")
	_, err = New("ths desk", config.WireDeskDocumentSourceConfig{BaseURL: "http://127.0.0.1:30013"})
	require.ErrorContains(t, err, "ths desk request_timeout_ms")
}
