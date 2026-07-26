package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"finance-sys/internal/domain/db_model"
	"finance-sys/internal/service"

	"github.com/stretchr/testify/require"
)

func TestHandleCreateStockDailySyncRunReturnsAccepted(t *testing.T) {
	fake := &fakeMarketDataService{
		createResponse: &service.StockDailySyncResponse{
			SyncRunID: 42,
			SyncType:  service.MarketDataSyncTypeStockDaily,
			TradeDate: "2026-06-26",
			Status:    service.MarketDataSyncStatusQueued,
			Deduped:   false,
			Message:   "stock daily sync task queued",
		},
	}
	server := &Server{marketData: fake}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/market/stock-daily/sync", strings.NewReader(`{"trade_date":"2026-06-26"}`))
	rec := httptest.NewRecorder()

	server.handleCreateStockDailySyncRun(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)
	require.JSONEq(t, `{"sync_run_id":42,"sync_type":"stock_daily","trade_date":"2026-06-26","status":"QUEUED","deduped":false,"message":"stock daily sync task queued"}`, rec.Body.String())
	require.Equal(t, "2026-06-26", fake.createRequest.TradeDate)
}

func TestHandleListMarketDataSyncRunsParsesFilters(t *testing.T) {
	tradeDate := time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC)
	fake := &fakeMarketDataService{
		listRows: []db_model.MarketDataSyncRun{{
			ID:        42,
			SyncType:  service.MarketDataSyncTypeStockDaily,
			TradeDate: tradeDate,
			Status:    service.MarketDataSyncStatusQueued,
		}},
	}
	server := &Server{marketData: fake}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/market/sync-runs?sync_type=stock_daily&trade_date=2026-06-26&limit=10", nil)
	rec := httptest.NewRecorder()

	server.handleListMarketDataSyncRuns(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, service.MarketDataSyncTypeStockDaily, fake.listQuery.SyncType)
	require.NotNil(t, fake.listQuery.TradeDate)
	require.Equal(t, tradeDate, *fake.listQuery.TradeDate)
	require.Equal(t, 10, fake.listQuery.Limit)
	require.Contains(t, rec.Body.String(), `"sync_type":"stock_daily"`)
}

type fakeMarketDataService struct {
	createRequest  service.StockDailySyncRequest
	createResponse *service.StockDailySyncResponse
	listQuery      service.MarketDataSyncRunQuery
	listRows       []db_model.MarketDataSyncRun
}

func (f *fakeMarketDataService) CreateStockDailySyncRun(ctx context.Context, request service.StockDailySyncRequest) (*service.StockDailySyncResponse, error) {
	f.createRequest = request
	return f.createResponse, nil
}

func (f *fakeMarketDataService) ListSyncRuns(ctx context.Context, query service.MarketDataSyncRunQuery) ([]db_model.MarketDataSyncRun, error) {
	f.listQuery = query
	return f.listRows, nil
}
