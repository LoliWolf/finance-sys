package httpapi

import (
	"errors"
	"net/http"

	"finance-sys/internal/stats"
)

func (s *Server) handleRecommendationExport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.stats == nil {
		writeError(w, http.StatusNotImplemented, errors.New("recommendation report service not enabled"))
		return
	}
	filter, err := parsePerformanceFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	response, err := s.stats.RecommendationExport(r.Context(), filter)
	var tooLarge *stats.RecommendationExportLimitError
	if errors.As(err, &tooLarge) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": err.Error(), "code": "EXPORT_TOO_LARGE", "total": tooLarge.Total, "max_items": tooLarge.Limit})
		return
	}
	writeStatsResponse(w, response, err)
}
