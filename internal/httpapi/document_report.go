package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"finance-sys/internal/dal"
	"finance-sys/internal/domain"
	"finance-sys/internal/service"
	"finance-sys/internal/stats"

	"github.com/go-chi/chi/v5"
)

const maxDocumentEvaluationBatchSize = 100

type documentEvaluationRequest struct {
	DocumentIDs  []int64 `json:"document_ids"`
	ForceRebuild bool    `json:"force_rebuild"`
}

type documentEvaluationResponse struct {
	RunID         int64  `json:"run_id"`
	Status        string `json:"status"`
	RunType       string `json:"run_type"`
	Message       string `json:"message"`
	DocumentCount int    `json:"document_count"`
}

func (s *Server) handleListDocumentReports(w http.ResponseWriter, r *http.Request) {
	if s.stats == nil {
		writeError(w, http.StatusNotImplemented, errors.New("document report service not enabled"))
		return
	}
	filter, err := parseDocumentReportListFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	response, err := s.stats.DocumentReports(r.Context(), filter)
	if err != nil {
		s.logRequest(r, slog.LevelError, "list document reports failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleGetDocumentReport(w http.ResponseWriter, r *http.Request) {
	if s.stats == nil {
		writeError(w, http.StatusNotImplemented, errors.New("document report service not enabled"))
		return
	}
	id, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	response, err := s.stats.DocumentReport(r.Context(), id)
	if errors.Is(err, dal.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		s.logRequest(r, slog.LevelError, "get document report failed", "document_id", id, "error", err.Error())
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleCreateDocumentEvaluationRun(w http.ResponseWriter, r *http.Request) {
	if s.evaluation == nil {
		writeError(w, http.StatusNotImplemented, errors.New("recommendation evaluation service not enabled"))
		return
	}
	var request documentEvaluationRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	documentIDs, err := normalizeDocumentEvaluationIDs(request.DocumentIDs)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	onlyActive := false
	response, err := s.evaluation.CreateRun(r.Context(), service.RecommendationEvaluationRequest{
		DocumentIDs:       documentIDs,
		ForceRebuild:      request.ForceRebuild,
		OnlyActive:        &onlyActive,
		ExcludeSuperseded: true,
	})
	if err != nil {
		s.logRequest(r, slog.LevelWarn, "create document evaluation run failed", "document_count", len(documentIDs), "error", err.Error())
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusAccepted, documentEvaluationResponse{
		RunID:         response.RunID,
		Status:        response.Status,
		RunType:       response.RunType,
		Message:       "document recommendation evaluation task queued",
		DocumentCount: len(documentIDs),
	})
}

func parseDocumentReportListFilter(r *http.Request) (stats.DocumentReportListFilter, error) {
	values := r.URL.Query()
	filter := stats.DocumentReportListFilter{
		Query:  strings.TrimSpace(values.Get("query")),
		Status: strings.ToUpper(strings.TrimSpace(values.Get("status"))),
	}
	var err error
	if filter.Limit, err = queryInt(r, "limit", 50); err != nil {
		return filter, err
	}
	if filter.Offset, err = queryInt(r, "offset", 0); err != nil {
		return filter, err
	}
	if filter.Status != "" && !validDocumentStatus(filter.Status) {
		return filter, fmt.Errorf("invalid document status %q", filter.Status)
	}
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	var dateFrom, dateTo *time.Time
	if raw := strings.TrimSpace(values.Get("date_from")); raw != "" {
		value, parseErr := time.ParseInLocation(time.DateOnly, raw, location)
		if parseErr != nil {
			return filter, fmt.Errorf("invalid date_from %q, expected YYYY-MM-DD", raw)
		}
		dateFrom = &value
		fromUTC := value.UTC()
		filter.CreatedFrom = &fromUTC
	}
	if raw := strings.TrimSpace(values.Get("date_to")); raw != "" {
		value, parseErr := time.ParseInLocation(time.DateOnly, raw, location)
		if parseErr != nil {
			return filter, fmt.Errorf("invalid date_to %q, expected YYYY-MM-DD", raw)
		}
		dateTo = &value
		beforeUTC := value.AddDate(0, 0, 1).UTC()
		filter.CreatedBefore = &beforeUTC
	}
	if dateFrom != nil && dateTo != nil && dateFrom.After(*dateTo) {
		return filter, errors.New("date_from must not be after date_to")
	}
	return filter, nil
}

func normalizeDocumentEvaluationIDs(values []int64) ([]int64, error) {
	if len(values) == 0 {
		return nil, errors.New("document_ids must not be empty")
	}
	seen := make(map[int64]struct{}, len(values))
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			return nil, fmt.Errorf("invalid document_id %d", value)
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	if len(result) > maxDocumentEvaluationBatchSize {
		return nil, fmt.Errorf("document_ids must contain at most %d unique values", maxDocumentEvaluationBatchSize)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result, nil
}

func validDocumentStatus(value string) bool {
	switch domain.DocumentStatus(value) {
	case domain.DocumentStatusIngested, domain.DocumentStatusParsed, domain.DocumentStatusPlanned, domain.DocumentStatusFailed, domain.DocumentStatusInvalid:
		return true
	default:
		return false
	}
}
