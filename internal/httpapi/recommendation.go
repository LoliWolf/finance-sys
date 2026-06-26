package httpapi

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"finance-sys/internal/dal"
	"finance-sys/internal/domain"

	"github.com/go-chi/chi/v5"
)

const (
	defaultRecommendationLimit = 100
	maxRecommendationLimit     = 500
)

func (s *Server) handleListRecommendations(w http.ResponseWriter, r *http.Request) {
	limit, err := normalizeRecommendationLimit(r.URL.Query().Get("limit"))
	if err != nil {
		s.logRequest(r, slog.LevelWarn, "handle list recommendations invalid limit", "error", err.Error())
		writeError(w, http.StatusBadRequest, err)
		return
	}
	direction := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("direction")))
	if err := validateRecommendationDirection(direction); err != nil {
		s.logRequest(r, slog.LevelWarn, "handle list recommendations invalid direction", "error", err.Error())
		writeError(w, http.StatusBadRequest, err)
		return
	}
	status := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status")))
	if err := validateRecommendationStatus(status); err != nil {
		s.logRequest(r, slog.LevelWarn, "handle list recommendations invalid status", "error", err.Error())
		writeError(w, http.StatusBadRequest, err)
		return
	}
	query := domain.RecommendationEventQuery{
		Limit:     limit,
		Symbol:    strings.TrimSpace(r.URL.Query().Get("symbol")),
		Direction: domain.TradeDirection(direction),
		Status:    domain.RecommendationEventStatus(status),
	}
	items, err := s.documents.ListRecommendationEventsByQuery(r.Context(), query)
	if err != nil {
		s.logRequest(r, slog.LevelError, "handle list recommendations failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.logRequest(r, slog.LevelInfo, "handle list recommendations success", "count", len(items))
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleGetRecommendation(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		s.logRequest(r, slog.LevelWarn, "handle get recommendation invalid id", "error", err.Error())
		writeError(w, http.StatusBadRequest, err)
		return
	}
	item, err := s.documents.GetRecommendationEventByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, dal.ErrNotFound) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		s.logRequest(r, slog.LevelError, "handle get recommendation failed", "recommendation_event_id", id, "error", err.Error())
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.logRequest(r, slog.LevelInfo, "handle get recommendation success", "recommendation_event_id", id)
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleListDocumentRecommendations(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		s.logRequest(r, slog.LevelWarn, "handle list document recommendations invalid id", "error", err.Error())
		writeError(w, http.StatusBadRequest, err)
		return
	}
	items, err := s.documents.ListRecommendationEventsByDocumentID(r.Context(), id)
	if err != nil {
		s.logRequest(r, slog.LevelError, "handle list document recommendations failed", "document_id", id, "error", err.Error())
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.logRequest(r, slog.LevelInfo, "handle list document recommendations success", "document_id", id, "count", len(items))
	writeJSON(w, http.StatusOK, items)
}

func normalizeRecommendationLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultRecommendationLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid limit %q", raw)
	}
	if limit <= 0 {
		return defaultRecommendationLimit, nil
	}
	if limit > maxRecommendationLimit {
		return maxRecommendationLimit, nil
	}
	return limit, nil
}

func validateRecommendationDirection(value string) error {
	switch value {
	case "", string(domain.TradeDirectionLong), string(domain.TradeDirectionShort):
		return nil
	default:
		return fmt.Errorf("invalid direction %q", value)
	}
}

func validateRecommendationStatus(value string) error {
	switch value {
	case "", string(domain.RecommendationEventStatusActive), string(domain.RecommendationEventStatusNeedsReview), string(domain.RecommendationEventStatusSuperseded):
		return nil
	default:
		return fmt.Errorf("invalid status %q", value)
	}
}
