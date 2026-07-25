package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"finance-sys/internal/dal"
	"finance-sys/internal/service"

	"github.com/go-chi/chi/v5"
)

func (s *Server) handleCreateRecommendationEvaluationRun(w http.ResponseWriter, r *http.Request) {
	if s.evaluation == nil {
		writeError(w, http.StatusNotImplemented, errors.New("recommendation evaluation service not enabled"))
		return
	}
	var request service.RecommendationEvaluationRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	response, err := s.evaluation.CreateRun(r.Context(), request)
	if err != nil {
		s.logRequest(r, slog.LevelWarn, "create recommendation evaluation run failed", "error", err.Error())
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusAccepted, response)
}

func (s *Server) handleListRecommendationEvaluationRuns(w http.ResponseWriter, r *http.Request) {
	if s.evaluation == nil {
		writeError(w, http.StatusNotImplemented, errors.New("recommendation evaluation service not enabled"))
		return
	}
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("invalid limit %q", raw))
			return
		}
		limit = value
	}
	items, err := s.evaluation.ListRuns(r.Context(), service.RecommendationEvaluationRunQuery{
		Status: r.URL.Query().Get("status"),
		Limit:  limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleGetRecommendationEvaluationRun(w http.ResponseWriter, r *http.Request) {
	if s.evaluation == nil {
		writeError(w, http.StatusNotImplemented, errors.New("recommendation evaluation service not enabled"))
		return
	}
	id, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	item, err := s.evaluation.GetRun(r.Context(), id)
	if errors.Is(err, dal.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
