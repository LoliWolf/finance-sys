package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"finance-sys/internal/service"
)

func (s *Server) handleCreateStockDailySyncRun(w http.ResponseWriter, r *http.Request) {
	if s.marketData == nil {
		s.logRequest(r, slog.LevelWarn, "handle create stock daily sync run not enabled")
		writeError(w, http.StatusNotImplemented, errors.New("market data service not enabled"))
		return
	}
	var request service.StockDailySyncRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		s.logRequest(r, slog.LevelWarn, "handle create stock daily sync run invalid json", "error", err.Error())
		writeError(w, http.StatusBadRequest, err)
		return
	}
	response, err := s.marketData.CreateStockDailySyncRun(r.Context(), request)
	if err != nil {
		s.logRequest(r, slog.LevelWarn, "handle create stock daily sync run failed", "error", err.Error())
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.logRequest(r, slog.LevelInfo, "handle create stock daily sync run success", "sync_run_id", response.SyncRunID, "deduped", response.Deduped)
	writeJSON(w, http.StatusAccepted, response)
}

func (s *Server) handleListMarketDataSyncRuns(w http.ResponseWriter, r *http.Request) {
	if s.marketData == nil {
		s.logRequest(r, slog.LevelWarn, "handle list market data sync runs not enabled")
		writeError(w, http.StatusNotImplemented, errors.New("market data service not enabled"))
		return
	}
	query, err := parseMarketDataSyncRunQuery(r)
	if err != nil {
		s.logRequest(r, slog.LevelWarn, "handle list market data sync runs invalid query", "error", err.Error())
		writeError(w, http.StatusBadRequest, err)
		return
	}
	items, err := s.marketData.ListSyncRuns(r.Context(), query)
	if err != nil {
		s.logRequest(r, slog.LevelError, "handle list market data sync runs failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.logRequest(r, slog.LevelInfo, "handle list market data sync runs success", "count", len(items))
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func parseMarketDataSyncRunQuery(r *http.Request) (service.MarketDataSyncRunQuery, error) {
	values := r.URL.Query()
	query := service.MarketDataSyncRunQuery{
		SyncType: strings.TrimSpace(values.Get("sync_type")),
		Limit:    50,
	}
	if raw := strings.TrimSpace(values.Get("trade_date")); raw != "" {
		tradeDate, err := time.Parse(time.DateOnly, raw)
		if err != nil {
			return query, fmt.Errorf("invalid trade_date %q, expected YYYY-MM-DD", raw)
		}
		query.TradeDate = &tradeDate
	}
	if raw := strings.TrimSpace(values.Get("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil {
			return query, fmt.Errorf("invalid limit %q", raw)
		}
		query.Limit = limit
	}
	return query, nil
}
