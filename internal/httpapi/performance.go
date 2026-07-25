package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"finance-sys/internal/dal"
	"finance-sys/internal/stats"

	"github.com/go-chi/chi/v5"
)

func (s *Server) handleBloggerRankings(w http.ResponseWriter, r *http.Request) {
	filter, err := parsePerformanceFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	response, err := s.performanceStats().BloggerRankings(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleBloggerPerformanceSummary(w http.ResponseWriter, r *http.Request) {
	id, filter, err := parseBloggerPerformanceRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	response, err := s.performanceStats().BloggerSummary(r.Context(), id, filter)
	writeStatsResponse(w, response, err)
}

func (s *Server) handleBloggerPerformanceTimeseries(w http.ResponseWriter, r *http.Request) {
	id, filter, err := parseBloggerPerformanceRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	response, err := s.performanceStats().BloggerTimeseries(r.Context(), id, filter)
	writeStatsResponse(w, response, err)
}

func (s *Server) handleBloggerRecommendationPerformance(w http.ResponseWriter, r *http.Request) {
	id, filter, err := parseBloggerPerformanceRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	filter.BloggerID = id
	response, err := s.performanceStats().RecommendationPerformanceList(r.Context(), filter)
	writeStatsResponse(w, response, err)
}

func (s *Server) handleGetRecommendationPerformance(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	response, err := s.performanceStats().RecommendationDetail(r.Context(), id)
	writeStatsResponse(w, response, err)
}

func (s *Server) handleGetRecommendationPriceSeries(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	daysBefore, err := queryInt(r, "days_before", 5)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	daysAfter, err := queryInt(r, "days_after", 90)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	response, err := s.performanceStats().PriceSeries(r.Context(), id, daysBefore, daysAfter)
	writeStatsResponse(w, response, err)
}

func (s *Server) handleListRecommendationPerformance(w http.ResponseWriter, r *http.Request) {
	filter, err := parsePerformanceFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	response, err := s.performanceStats().RecommendationPerformanceList(r.Context(), filter)
	writeStatsResponse(w, response, err)
}

func (s *Server) handleSecurityRankings(w http.ResponseWriter, r *http.Request) {
	filter, err := parsePerformanceFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	response, err := s.performanceStats().SecurityRankings(r.Context(), filter)
	writeStatsResponse(w, response, err)
}

func (s *Server) handleSecurityPerformanceSummary(w http.ResponseWriter, r *http.Request) {
	filter, err := parsePerformanceFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	response, err := s.performanceStats().SecuritySummary(r.Context(), chi.URLParam(r, "tsCode"), filter)
	writeStatsResponse(w, response, err)
}

func (s *Server) handleSecurityRecommendationPerformance(w http.ResponseWriter, r *http.Request) {
	filter, err := parsePerformanceFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	filter.TSCode = strings.ToUpper(strings.TrimSpace(chi.URLParam(r, "tsCode")))
	response, err := s.performanceStats().RecommendationPerformanceList(r.Context(), filter)
	writeStatsResponse(w, response, err)
}

func (s *Server) performanceStats() performanceStatsService {
	return s.stats
}

func parseBloggerPerformanceRequest(r *http.Request) (int64, stats.Filter, error) {
	id, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		return 0, stats.Filter{}, err
	}
	filter, err := parsePerformanceFilter(r)
	return id, filter, err
}

func parsePerformanceFilter(r *http.Request) (stats.Filter, error) {
	values := r.URL.Query()
	filter := stats.Filter{
		Market:    strings.TrimSpace(values.Get("market")),
		AssetType: strings.TrimSpace(values.Get("asset_type")),
		Direction: strings.TrimSpace(values.Get("direction")),
		Status:    strings.TrimSpace(values.Get("status")),
		TSCode:    strings.TrimSpace(values.Get("ts_code")),
		Symbol:    strings.TrimSpace(values.Get("symbol")),
		Sort:      strings.TrimSpace(values.Get("sort")),
	}
	var err error
	if filter.WindowDays, err = queryInt(r, "window_days", 0); err != nil {
		return filter, err
	}
	if filter.MinSampleCount, err = queryInt(r, "min_sample_count", 0); err != nil {
		return filter, err
	}
	if filter.Limit, err = queryInt(r, "limit", 50); err != nil {
		return filter, err
	}
	if filter.Offset, err = queryInt(r, "offset", 0); err != nil {
		return filter, err
	}
	if raw := strings.TrimSpace(values.Get("blogger_id")); raw != "" {
		filter.BloggerID, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || filter.BloggerID <= 0 {
			return filter, fmt.Errorf("invalid blogger_id %q", raw)
		}
	}
	if raw := strings.TrimSpace(values.Get("date_from")); raw != "" {
		value, parseErr := time.Parse(time.DateOnly, raw)
		if parseErr != nil {
			return filter, fmt.Errorf("invalid date_from %q, expected YYYY-MM-DD", raw)
		}
		filter.DateFrom = &value
	}
	if raw := strings.TrimSpace(values.Get("date_to")); raw != "" {
		value, parseErr := time.Parse(time.DateOnly, raw)
		if parseErr != nil {
			return filter, fmt.Errorf("invalid date_to %q, expected YYYY-MM-DD", raw)
		}
		filter.DateTo = &value
	}
	if filter.DateFrom != nil && filter.DateTo != nil && filter.DateFrom.After(*filter.DateTo) {
		return filter, errors.New("date_from must not be after date_to")
	}
	if filter.Direction != "" && !stringInSet(strings.ToUpper(filter.Direction), "LONG", "SHORT") {
		return filter, fmt.Errorf("invalid direction %q", filter.Direction)
	}
	if filter.Status != "" && !stringInSet(strings.ToUpper(filter.Status), "READY", "PENDING", "INCOMPLETE", "NO_SECURITY", "UNSUPPORTED", "FAILED") {
		return filter, fmt.Errorf("invalid status %q", filter.Status)
	}
	if filter.Sort != "" && !stringInSet(filter.Sort, "win_rate", "avg_return", "sample_count", "performance_score") {
		return filter, fmt.Errorf("invalid sort %q", filter.Sort)
	}
	return filter, nil
}

func queryInt(r *http.Request, name string, fallback int) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q", name, raw)
	}
	return value, nil
}

func stringInSet(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if value == candidate {
			return true
		}
	}
	return false
}

func writeStatsResponse(w http.ResponseWriter, payload any, err error) {
	if errors.Is(err, dal.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}
