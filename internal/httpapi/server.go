package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

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
}

func NewServer(
	repo *repository.Repository,
	runtime *config.Runtime,
	documents *service.DocumentService,
	reloader ConfigReloader,
) *Server {
	return &Server{
		repo:      repo,
		runtime:   runtime,
		documents: documents,
		reloader:  reloader,
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
	if !cfg.Document.APIUploadEnabled {
		writeError(w, http.StatusForbidden, errors.New("api upload disabled"))
		return
	}

	limit := int64(cfg.Document.MaxFileSizeMB) * 1024 * 1024
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	if err := r.ParseMultipartForm(limit); err != nil {
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
		writeError(w, http.StatusBadRequest, err)
		return
	}

	response := map[string]any{
		"duplicate": duplicate,
		"document":  document,
	}
	if cfg.Document.AutoAnalyzeUpload && !duplicate {
		plans, err := s.documents.AnalyzeDocument(r.Context(), document.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		response["plans"] = plans
	}
	if duplicate {
		if plans, err := s.documents.ListPlansByDocumentID(r.Context(), document.ID); err == nil {
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
		writeError(w, http.StatusBadRequest, err)
		return
	}
	plans, err := s.documents.AnalyzeDocument(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "planned",
		"plans":  plans,
	})
}

func (s *Server) handleListDocumentPlans(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	items, err := s.documents.ListPlansByDocumentID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleListPlans(w http.ResponseWriter, r *http.Request) {
	items, err := s.repo.ListPlans(r.Context(), 100)
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
