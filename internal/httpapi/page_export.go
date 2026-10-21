package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"finance-sys/internal/stats"
	tradingservice "finance-sys/internal/trading/service"
	"github.com/go-chi/chi/v5"
)

func (s *Server) handlePageExport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.stats == nil {
		writeError(w, http.StatusNotImplemented, errors.New("report service not enabled"))
		return
	}
	request := stats.PageExportRequest{Kind: chi.URLParam(r, "kind"), Search: strings.TrimSpace(r.URL.Query().Get("search")), SelectedTSCode: strings.TrimSpace(r.URL.Query().Get("selected_ts_code"))}
	var err error
	switch request.Kind {
	case "documents":
		request.Documents, err = parseDocumentReportListFilter(r)
	case "dashboard", "blogger", "securities":
		request.Filter, err = parsePerformanceFilter(r)
		if err == nil && request.Kind == "blogger" && request.Filter.BloggerID <= 0 {
			err = errors.New("blogger_id is required")
		}
	case "recommendation":
		request.EventID, err = parseID(r.URL.Query().Get("event_id"))
		if err == nil && request.EventID <= 0 {
			err = errors.New("event_id must be positive")
		}
	case "workbench":
	default:
		writeError(w, http.StatusNotFound, errors.New("unsupported report kind"))
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result, err := s.stats.PageExport(r.Context(), request)
	var tooLarge *stats.PageExportLimitError
	if errors.As(err, &tooLarge) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": err.Error(), "code": "EXPORT_TOO_LARGE", "max_items": tooLarge.Limit, "total": tooLarge.Total})
		return
	}
	writeStatsResponse(w, result, err)
}

func (s *Server) handleTradingReport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.trading == nil {
		writeError(w, http.StatusNotImplemented, errors.New("trading service not enabled"))
		return
	}
	result, err := s.trading.TradingDashboardReport(r.Context(), r.URL.Query().Get("trade_date"))
	var tooLarge *tradingservice.TradingReportLimitError
	if errors.As(err, &tooLarge) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": err.Error(), "code": "EXPORT_TOO_LARGE", "max_items": tooLarge.Limit})
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
