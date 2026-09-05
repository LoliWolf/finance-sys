package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"

	"finance-sys/internal/dal"
	"finance-sys/internal/service"

	"github.com/go-chi/chi/v5"
)

// This is a local maintenance entry point, not an external customer input API.
func (s *Server) handleRecoverUntrackableTarget(w http.ResponseWriter, r *http.Request) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		writeError(w, http.StatusForbidden, errors.New("target recovery is restricted to local maintenance"))
		return
	}
	documentID, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	targetID, err := parseID(chi.URLParam(r, "targetID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var request service.TargetRecoveryRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, errors.New("exactly one JSON request is required"))
		return
	}
	if s.documents == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("document service unavailable"))
		return
	}
	result, err := s.documents.RecoverUntrackableTarget(r.Context(), documentID, targetID, request)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, dal.ErrNotFound):
			status = http.StatusNotFound
		case errors.Is(err, service.ErrInvalidTargetRecovery):
			status = http.StatusBadRequest
		case errors.Is(err, service.ErrTargetRecoveryConflict):
			status = http.StatusConflict
		}
		writeError(w, status, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
