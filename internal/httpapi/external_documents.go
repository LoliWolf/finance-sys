package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"finance-sys/internal/dal"
	"finance-sys/internal/domain"
	"finance-sys/internal/domain/db_model"
	"finance-sys/internal/service"

	"github.com/go-chi/chi/v5"
)

// handleEnqueueXueqiuRun and handleEnqueueTHSRun queue an out-of-schedule
// wire desk ingestion run. The run executes in the scheduler's xueqiu / ths
// lane; poll it via GET /admin/scheduled-task-runs/{id}.
func (s *Server) handleEnqueueXueqiuRun(w http.ResponseWriter, r *http.Request) {
	if s.wireDeskRuns == nil {
		writeError(w, http.StatusNotImplemented, errors.New("Xueqiu ingestion is not available"))
		return
	}
	s.enqueueWireDeskRun(w, r, s.wireDeskRuns.EnqueueManualXueqiuRun)
}

func (s *Server) handleEnqueueTHSRun(w http.ResponseWriter, r *http.Request) {
	if s.wireDeskRuns == nil {
		writeError(w, http.StatusNotImplemented, errors.New("THS ingestion is not available"))
		return
	}
	s.enqueueWireDeskRun(w, r, s.wireDeskRuns.EnqueueManualTHSRun)
}

func (s *Server) handleEnqueueEastmoneyRun(w http.ResponseWriter, r *http.Request) {
	if s.wireDeskRuns == nil {
		writeError(w, http.StatusNotImplemented, errors.New("Eastmoney ingestion is not available"))
		return
	}
	s.enqueueWireDeskRun(w, r, s.wireDeskRuns.EnqueueManualEastmoneyRun)
}

func (s *Server) enqueueWireDeskRun(w http.ResponseWriter, r *http.Request, enqueue func(context.Context, service.WireDeskIngestionRequest) (*db_model.ScheduledTaskRun, error)) {
	var request service.WireDeskIngestionRequest
	if r.Body != nil {
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	run, err := enqueue(r.Context(), request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"task_run_id": run.ID,
		"task_key":    run.TaskKey,
		"status":      run.Status,
	})
}

func (s *Server) handleGetScheduledTaskRun(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	run, err := dal.ScheduledTaskRuns.QueryByID(r.Context(), s.db, id)
	if errors.Is(err, dal.ErrNotFound) {
		writeError(w, http.StatusNotFound, errors.New("scheduled task run not found"))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":             run.ID,
		"task_type":      domain.ScheduledTaskType(run.TaskType).String(),
		"task_key":       run.TaskKey,
		"status":         run.Status,
		"scheduled_at":   run.ScheduledAt,
		"started_at":     run.StartedAt,
		"finished_at":    run.FinishedAt,
		"attempt_count":  run.AttemptCount,
		"error_message":  run.ErrorMessage,
		"output":         json.RawMessage(nonEmptyJSON(run.OutputJSON)),
		"config_version": run.ConfigVersion,
	})
}

func nonEmptyJSON(raw []byte) []byte {
	if len(raw) == 0 {
		return []byte("null")
	}
	return raw
}
