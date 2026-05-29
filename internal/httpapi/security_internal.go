package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"finance-sys/internal/dal"
	"finance-sys/internal/domain"
)

const (
	defaultSecurityToolMaxCandidates = 5
	maxSecurityToolCandidates        = 20
)

type securityResolveRequest struct {
	Query         string `json:"query"`
	MaxCandidates int    `json:"max_candidates"`
}

type securityResolveResponse struct {
	Query      string                                 `json:"query"`
	Candidates []domain.InstrumentResolutionCandidate `json:"candidates"`
}

type securityVerifyRequest struct {
	TSCode    string `json:"ts_code"`
	RawSymbol string `json:"raw_symbol"`
}

type securityVerifyResponse struct {
	Verified bool                                  `json:"verified"`
	Security *domain.InstrumentResolutionCandidate `json:"security,omitempty"`
}

func (s *Server) handleResolveSecurity(w http.ResponseWriter, r *http.Request) {
	if s.security == nil {
		s.logRequest(r, slog.LevelWarn, "handle internal security resolve not enabled")
		writeError(w, http.StatusNotImplemented, errors.New("security service not enabled"))
		return
	}
	var request securityResolveRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		s.logRequest(r, slog.LevelWarn, "handle internal security resolve invalid json", "error", err.Error())
		writeError(w, http.StatusBadRequest, err)
		return
	}
	request.Query = strings.TrimSpace(request.Query)
	if request.Query == "" {
		s.logRequest(r, slog.LevelWarn, "handle internal security resolve missing query")
		writeError(w, http.StatusBadRequest, errors.New("query is required"))
		return
	}
	maxCandidates := normalizeSecurityToolMaxCandidates(request.MaxCandidates)
	candidates, err := s.security.ResolveTradableCandidates(r.Context(), request.Query, maxCandidates)
	if err != nil {
		s.logRequest(r, slog.LevelError, "handle internal security resolve failed", "query", request.Query, "error", err.Error())
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.logRequest(r, slog.LevelInfo, "handle internal security resolve success", "query", request.Query, "candidate_count", len(candidates))
	writeJSON(w, http.StatusOK, securityResolveResponse{
		Query:      request.Query,
		Candidates: candidates,
	})
}

func (s *Server) handleVerifySecurity(w http.ResponseWriter, r *http.Request) {
	if s.security == nil {
		s.logRequest(r, slog.LevelWarn, "handle internal security verify not enabled")
		writeError(w, http.StatusNotImplemented, errors.New("security service not enabled"))
		return
	}
	var request securityVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		s.logRequest(r, slog.LevelWarn, "handle internal security verify invalid json", "error", err.Error())
		writeError(w, http.StatusBadRequest, err)
		return
	}
	request.TSCode = strings.ToUpper(strings.TrimSpace(request.TSCode))
	if request.TSCode == "" {
		s.logRequest(r, slog.LevelWarn, "handle internal security verify missing ts_code")
		writeError(w, http.StatusBadRequest, errors.New("ts_code is required"))
		return
	}
	candidate, err := s.security.VerifyTradableCandidate(r.Context(), request.TSCode)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, dal.ErrNotFound) {
			status = http.StatusNotFound
		}
		s.logRequest(r, slog.LevelWarn, "handle internal security verify failed", "ts_code", request.TSCode, "raw_symbol", request.RawSymbol, "error", err.Error())
		writeError(w, status, fmt.Errorf("security %s is not verified", request.TSCode))
		return
	}
	s.logRequest(r, slog.LevelInfo, "handle internal security verify success", "ts_code", request.TSCode, "raw_symbol", request.RawSymbol)
	writeJSON(w, http.StatusOK, securityVerifyResponse{
		Verified: true,
		Security: candidate,
	})
}

func normalizeSecurityToolMaxCandidates(value int) int {
	if value <= 0 {
		return defaultSecurityToolMaxCandidates
	}
	if value > maxSecurityToolCandidates {
		return maxSecurityToolCandidates
	}
	return value
}
