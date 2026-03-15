package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/domain"
	"finance-sys/internal/repository"
	"finance-sys/internal/service"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type ConfigReloader interface {
	Reload(context.Context) error
}

type Server struct {
	repo      *repository.Repository
	runtime   *config.Runtime
	documents *service.DocumentService
	reloader  ConfigReloader
	logger    *slog.Logger
}

func NewServer(
	repo *repository.Repository,
	runtime *config.Runtime,
	documents *service.DocumentService,
	reloader ConfigReloader,
	logger *slog.Logger,
) *Server {
	return &Server{
		repo:      repo,
		runtime:   runtime,
		documents: documents,
		reloader:  reloader,
		logger:    logger,
	}
}

func (s *Server) Router() http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.Recoverer)
	router.Use(s.requestLogMiddleware)
	router.Use(s.authMiddleware)
	router.Use(s.corsMiddleware)

	cfg := s.runtime.Config()
	apiPrefix := "/api/v1"
	if cfg != nil && cfg.Service.HTTP.APIPrefix != "" {
		apiPrefix = cfg.Service.HTTP.APIPrefix
	}

	router.Get("/", s.handleUploadPage)
	router.Get("/upload", s.handleUploadPage)
	router.Get("/healthz", s.handleHealth)
	router.Route(apiPrefix, func(r chi.Router) {
		r.Get("/documents", s.handleListDocuments)
		r.Post("/documents/upload", s.handleUploadDocument)
		r.Post("/documents/{id}/analyze", s.handleAnalyzeDocument)
		r.Get("/documents/{id}/plans", s.handleListDocumentPlans)
		r.Get("/plans", s.handleListPlans)
		r.Post("/admin/config/reload", s.handleReloadConfig)
	})
	return router
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.logRequest(r, slog.LevelDebug, "handle health")
	err := s.repo.Ping(r.Context())
	status := http.StatusOK
	payload := map[string]any{"status": "ok"}
	if err != nil {
		status = http.StatusServiceUnavailable
		payload = map[string]any{"status": "degraded", "error": err.Error()}
	}
	writeJSON(w, status, payload)
}

func (s *Server) handleListDocuments(w http.ResponseWriter, r *http.Request) {
	s.logRequest(r, slog.LevelInfo, "handle list documents start")
	items, err := s.repo.ListDocuments(r.Context(), 100)
	if err != nil {
		s.logRequest(r, slog.LevelError, "handle list documents failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.logRequest(r, slog.LevelInfo, "handle list documents success", "count", len(items))
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleUploadDocument(w http.ResponseWriter, r *http.Request) {
	s.logRequest(r, slog.LevelInfo, "handle upload document start")
	cfg := s.runtime.Config()
	if cfg == nil {
		s.logRequest(r, slog.LevelError, "handle upload document missing config")
		writeError(w, http.StatusInternalServerError, errors.New("config runtime unavailable"))
		return
	}
	if !cfg.Document.APIUploadEnabled {
		s.logRequest(r, slog.LevelWarn, "handle upload document forbidden")
		writeError(w, http.StatusForbidden, errors.New("api upload disabled"))
		return
	}

	limit := int64(cfg.Document.MaxFileSizeMB) * 1024 * 1024
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	if err := r.ParseMultipartForm(limit); err != nil {
		s.logRequest(r, slog.LevelWarn, "handle upload document parse multipart failed", "error", err.Error())
		writeError(w, http.StatusBadRequest, err)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		s.logRequest(r, slog.LevelWarn, "handle upload document missing file", "error", err.Error())
		writeError(w, http.StatusBadRequest, err)
		return
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		s.logRequest(r, slog.LevelWarn, "handle upload document read file failed", "error", err.Error(), "file_name", header.Filename)
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.logRequest(r, slog.LevelInfo, "handle upload document file loaded", "file_name", header.Filename, "size_bytes", len(content))

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
		s.logRequest(r, slog.LevelError, "handle upload document ingest failed", "file_name", header.Filename, "error", err.Error())
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.logRequest(r, slog.LevelInfo, "handle upload document ingest success", "document_id", document.ID, "duplicate", duplicate, "file_name", header.Filename)

	response := map[string]any{
		"duplicate": duplicate,
		"document":  document,
	}
	shouldAutoAnalyze := (!duplicate && cfg.Document.AutoAnalyzeUpload) || (duplicate && document.Status == "FAILED")
	if shouldAutoAnalyze {
		reason := "new_document"
		if duplicate {
			reason = "duplicate_failed_rerun"
		}
		s.logRequest(r, slog.LevelInfo, "handle upload document auto analyze start", "document_id", document.ID, "reason", reason, "document_status", document.Status)
		plans, err := s.documents.AnalyzeDocument(r.Context(), document.ID)
		if err != nil {
			s.logRequest(r, slog.LevelError, "handle upload document auto analyze failed", "document_id", document.ID, "reason", reason, "error", err.Error())
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if refreshed, reloadErr := s.repo.GetDocumentByID(r.Context(), document.ID); reloadErr == nil {
			document = refreshed
			response["document"] = document
		}
		s.logRequest(r, slog.LevelInfo, "handle upload document auto analyze success", "document_id", document.ID, "reason", reason, "plan_count", len(plans))
		response["plans"] = plans
	}
	if duplicate && !shouldAutoAnalyze {
		if plans, err := s.documents.ListPlansByDocumentID(r.Context(), document.ID); err == nil {
			s.logRequest(r, slog.LevelInfo, "handle upload document duplicate reused plans", "document_id", document.ID, "plan_count", len(plans))
			response["plans"] = plans
		}
	}

	status := http.StatusCreated
	if duplicate {
		status = http.StatusOK
	}
	writeJSON(w, status, response)
}

func (s *Server) handleAnalyzeDocument(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		s.logRequest(r, slog.LevelWarn, "handle analyze document invalid id", "error", err.Error())
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.logRequest(r, slog.LevelInfo, "handle analyze document start", "document_id", id)
	plans, err := s.documents.AnalyzeDocument(r.Context(), id)
	if err != nil {
		s.logRequest(r, slog.LevelError, "handle analyze document failed", "document_id", id, "error", err.Error())
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.logRequest(r, slog.LevelInfo, "handle analyze document success", "document_id", id, "plan_count", len(plans))
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "planned",
		"plans":  plans,
	})
}

func (s *Server) handleListDocumentPlans(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		s.logRequest(r, slog.LevelWarn, "handle list document plans invalid id", "error", err.Error())
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.logRequest(r, slog.LevelInfo, "handle list document plans start", "document_id", id)
	items, err := s.documents.ListPlansByDocumentID(r.Context(), id)
	if err != nil {
		s.logRequest(r, slog.LevelError, "handle list document plans failed", "document_id", id, "error", err.Error())
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.logRequest(r, slog.LevelInfo, "handle list document plans success", "document_id", id, "count", len(items))
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleListPlans(w http.ResponseWriter, r *http.Request) {
	s.logRequest(r, slog.LevelInfo, "handle list plans start")
	items, err := s.repo.ListPlans(r.Context(), 100)
	if err != nil {
		s.logRequest(r, slog.LevelError, "handle list plans failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.logRequest(r, slog.LevelInfo, "handle list plans success", "count", len(items))
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleReloadConfig(w http.ResponseWriter, r *http.Request) {
	if s.reloader == nil {
		s.logRequest(r, slog.LevelWarn, "handle reload config not enabled")
		writeError(w, http.StatusNotImplemented, errors.New("config reload not enabled"))
		return
	}
	s.logRequest(r, slog.LevelInfo, "handle reload config start")
	if err := s.reloader.Reload(r.Context()); err != nil {
		s.logRequest(r, slog.LevelError, "handle reload config failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.logRequest(r, slog.LevelInfo, "handle reload config success")
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
		s.logRequest(r, slog.LevelWarn, "auth middleware rejected request")
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
					w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
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

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (s *Server) requestLogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		s.logRequest(r, slog.LevelInfo, "http request start")
		next.ServeHTTP(recorder, r)
		s.logRequest(r, slog.LevelInfo, "http request completed", "status", recorder.status, "duration_ms", time.Since(start).Milliseconds())
	})
}

func (s *Server) logRequest(r *http.Request, level slog.Level, msg string, args ...any) {
	if s.logger == nil {
		return
	}
	base := []any{
		"method", r.Method,
		"path", r.URL.Path,
		"query", r.URL.RawQuery,
		"request_id", middleware.GetReqID(r.Context()),
	}
	s.logger.Log(r.Context(), level, msg, append(base, args...)...)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func parseID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid id %q", raw)
	}
	return id, nil
}
