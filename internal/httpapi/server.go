package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"finance-sys/internal/approval"
	"finance-sys/internal/config"
	"finance-sys/internal/domain"
	"finance-sys/internal/report"
	"finance-sys/internal/repository"
	"finance-sys/internal/service"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type ConfigReloader interface {
	Reload(context.Context) error
}

type Server struct {
	repo       *repository.Repository
	runtime    *config.Runtime
	documents  *service.DocumentService
	evaluation *service.EvaluationService
	approval   *approval.Service
	reports    *report.Service
	reloader   ConfigReloader
}

func NewServer(
	repo *repository.Repository,
	runtime *config.Runtime,
	documents *service.DocumentService,
	evaluation *service.EvaluationService,
	approvalSvc *approval.Service,
	reportSvc *report.Service,
	reloader ConfigReloader,
) *Server {
	return &Server{
		repo:       repo,
		runtime:    runtime,
		documents:  documents,
		evaluation: evaluation,
		approval:   approvalSvc,
		reports:    reportSvc,
		reloader:   reloader,
	}
}

func (s *Server) Router() http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.Recoverer)
	router.Use(s.authMiddleware)
	router.Use(s.corsMiddleware)

	cfg := s.runtime.Config()
	apiPrefix := "/api/v1"
	if cfg != nil && cfg.Service.HTTP.APIPrefix != "" {
		apiPrefix = cfg.Service.HTTP.APIPrefix
	}

	router.Get("/healthz", s.handleHealth)
	router.Get("/metrics", s.handleMetrics)
	router.Route(apiPrefix, func(r chi.Router) {
		r.Get("/documents", s.handleListDocuments)
		r.Post("/documents/upload", s.handleUploadDocument)
		r.Post("/documents/{id}/process", s.handleProcessDocument)
		r.Post("/jobs/process-documents", s.handleProcessPendingDocuments)
		r.Get("/plans", s.handleListPlans)
		r.Post("/plans/{id}/approve", s.handleApprovePlan)
		r.Get("/evaluations", s.handleListEvaluations)
		r.Post("/jobs/evaluate", s.handleEvaluateTradeDate)
		r.Get("/reports/scorecards", s.handleScorecards)
		r.Post("/admin/config/reload", s.handleReloadConfig)
	})
	return router
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	err := s.repo.Ping(r.Context())
	status := http.StatusOK
	payload := map[string]any{"status": "ok"}
	if err != nil {
		status = http.StatusServiceUnavailable
		payload = map[string]any{"status": "degraded", "error": err.Error()}
	}
	writeJSON(w, status, payload)
}

func (s *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = io.WriteString(w, "expert_trade_up 1\n")
}

func (s *Server) handleListDocuments(w http.ResponseWriter, r *http.Request) {
	items, err := s.repo.ListDocuments(r.Context(), 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleUploadDocument(w http.ResponseWriter, r *http.Request) {
	cfg := s.runtime.Config()
	if cfg == nil {
		writeError(w, http.StatusInternalServerError, errors.New("config runtime unavailable"))
		return
	}
	if !cfg.DocumentIngestion.APIUploadEnabled {
		writeError(w, http.StatusForbidden, errors.New("api upload disabled"))
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, int64(cfg.DocumentIngestion.MaxFileSizeMB)*1024*1024)
	if err := r.ParseMultipartForm(int64(cfg.DocumentIngestion.MaxFileSizeMB) * 1024 * 1024); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	document, duplicate, err := s.documents.IngestDocument(r.Context(), domain.DocumentIngestRequest{
		SourceType:  r.FormValue("source_type"),
		SourceName:  r.FormValue("source_name"),
		Author:      r.FormValue("author"),
		Institution: r.FormValue("institution"),
		Title:       r.FormValue("title"),
		FileName:    header.Filename,
		ContentType: header.Header.Get("Content-Type"),
		Content:     content,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	status := http.StatusCreated
	if duplicate {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{
		"duplicate": duplicate,
		"document":  document,
	})
}

func (s *Server) handleProcessDocument(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.documents.ProcessDocument(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "processed"})
}

func (s *Server) handleProcessPendingDocuments(w http.ResponseWriter, r *http.Request) {
	if err := s.documents.ProcessPendingDocuments(r.Context(), 100); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "queued"})
}

func (s *Server) handleListPlans(w http.ResponseWriter, r *http.Request) {
	items, err := s.repo.ListPlans(r.Context(), 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleApprovePlan(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var payload struct {
		ApprovedBy string `json:"approved_by"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)
	if payload.ApprovedBy == "" {
		payload.ApprovedBy = "api"
	}
	if err := s.approval.ApprovePlan(r.Context(), id, payload.ApprovedBy); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "approved"})
}

func (s *Server) handleListEvaluations(w http.ResponseWriter, r *http.Request) {
	items, err := s.repo.ListEvaluations(r.Context(), 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleEvaluateTradeDate(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		TradeDate string `json:"trade_date"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)
	tradeDate := time.Now()
	if payload.TradeDate != "" {
		parsed, err := time.Parse(time.DateOnly, payload.TradeDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		tradeDate = parsed
	}
	if err := s.evaluation.EvaluateTradeDate(r.Context(), tradeDate); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "evaluated"})
}

func (s *Server) handleScorecards(w http.ResponseWriter, r *http.Request) {
	items, err := s.reports.Scorecards(r.Context(), 200)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleReloadConfig(w http.ResponseWriter, r *http.Request) {
	if s.reloader == nil {
		writeError(w, http.StatusNotImplemented, errors.New("config reload not enabled"))
		return
	}
	if err := s.reloader.Reload(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "reloaded"})
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := s.runtime.Config()
		if cfg == nil || !cfg.Security.Auth.Enabled {
			next.ServeHTTP(w, r)
			return
		}
		token := r.Header.Get(cfg.Security.Auth.HeaderName)
		for _, candidate := range cfg.Security.Auth.StaticTokens {
			expected := cfg.Security.Auth.TokenPrefix + candidate
			if token == expected {
				next.ServeHTTP(w, r)
				return
			}
		}
		writeError(w, http.StatusUnauthorized, errors.New("unauthorized"))
	})
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := s.runtime.Config()
		if cfg != nil && cfg.Service.HTTP.CORS.Enabled {
			origin := r.Header.Get("Origin")
			for _, allowed := range cfg.Service.HTTP.CORS.AllowOrigins {
				if allowed == origin || allowed == "*" {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
					w.Header().Set("Access-Control-Allow-Headers", "Authorization,Content-Type")
					break
				}
			}
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{
		"error": err.Error(),
	})
}

func parseID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid id %q", raw)
	}
	return id, nil
}
