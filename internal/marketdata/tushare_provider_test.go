package marketdata

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTushareProviderFetchStockDailyUsesSDKDailyPayload(t *testing.T) {
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{
			"api_name":"daily",
			"token":"token-a",
			"params":{"trade_date":"20260626"},
			"fields":["ts_code","trade_date","open","close"]
		}`, string(body))
		return jsonResponse(`{"request_id":"1","code":0,"msg":"","data":{"fields":["ts_code","trade_date","open","close"],"items":[["000001.SZ","20260626",10.1,10.5]]}}`), nil
	})
	provider := NewTushareProvider(&http.Client{Transport: transport})

	rows, err := provider.FetchStockDaily(context.Background(), "token-a", time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC), []string{"ts_code", "trade_date", "open", "close"})

	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "000001.SZ", rows[0].Values["ts_code"])
	require.Equal(t, 10.5, rows[0].Values["close"])
}

func TestTushareProviderFetchETFDailyUsesFundDailyFallback(t *testing.T) {
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{
			"api_name":"fund_daily",
			"token":"token-b",
			"params":{"trade_date":"20260626"},
			"fields":["ts_code","trade_date","open","close"]
		}`, string(body))
		return jsonResponse(`{"request_id":"1","code":0,"msg":"","data":{"fields":["ts_code","trade_date","open","close"],"items":[["510300.SH","20260626",4.1,4.2]]}}`), nil
	})
	provider := NewTushareProvider(&http.Client{Transport: transport})

	rows, err := provider.FetchETFDaily(context.Background(), "token-b", time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC), []string{"ts_code", "trade_date", "open", "close"})

	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "510300.SH", rows[0].Values["ts_code"])
	require.Equal(t, 4.2, rows[0].Values["close"])
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}
