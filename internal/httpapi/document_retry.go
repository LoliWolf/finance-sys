package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"finance-sys/internal/dal"
	"finance-sys/internal/service"
	"github.com/go-chi/chi/v5"
)

func (s *Server) handleRetryDocumentAnalysis(w http.ResponseWriter, r *http.Request) {
	if s.documentRetry == nil {
		writeError(w, http.StatusNotImplemented, errors.New("document retry service not enabled"))
		return
	}
	id, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var body struct {
		TradeDate string `json:"trade_date"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, errors.New("expected one JSON object"))
		return
	}
	date, present, err := parseOptionalTradeDate(body.TradeDate)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var requested *time.Time
	if present {
		requested = &date
	}
	result, err := s.documentRetry.RetryFailedDocument(r.Context(), id, requested)
	switch {
	case errors.Is(err, dal.ErrNotFound):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, service.ErrDocumentRetryNotAllowed), errors.Is(err, service.ErrDocumentAnalysisBusy), errors.Is(err, service.ErrDocumentRetryDateConflict):
		writeError(w, http.StatusConflict, err)
	case errors.Is(err, service.ErrDocumentRetryDateRequired):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error(), "code": "RETRY_DATE_REQUIRED"})
	case err != nil:
		writeError(w, http.StatusInternalServerError, err)
	default:
		writeJSON(w, http.StatusOK, result)
	}
}
