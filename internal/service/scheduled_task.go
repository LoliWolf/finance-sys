package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/dal"
	"finance-sys/internal/domain"
	"finance-sys/internal/domain/db_model"

	"gorm.io/gorm"
)

const (
	ScheduledTaskStatusQueued    = "QUEUED"
	ScheduledTaskStatusRunning   = "RUNNING"
	ScheduledTaskStatusSucceeded = "SUCCEEDED"
	ScheduledTaskStatusFailed    = "FAILED"
	ScheduledTaskStatusSkipped   = "SKIPPED"
)

type ScheduledTaskHandler func(context.Context, json.RawMessage) (any, error)

type ScheduledTaskService struct {
	db       *gorm.DB
	runtime  *config.Runtime
	logger   *slog.Logger
	handlers map[domain.ScheduledTaskType]ScheduledTaskHandler
	now      func() time.Time
}

func NewScheduledTaskService(db *gorm.DB, runtime *config.Runtime, logger *slog.Logger) *ScheduledTaskService {
	return &ScheduledTaskService{
		db:       db,
		runtime:  runtime,
		logger:   logger,
		handlers: make(map[domain.ScheduledTaskType]ScheduledTaskHandler),
		now:      time.Now,
	}
}

func (s *ScheduledTaskService) RegisterHandler(taskType domain.ScheduledTaskType, handler ScheduledTaskHandler) error {
	if !taskType.Valid() {
		return fmt.Errorf("invalid scheduled task type %d", taskType)
	}
	if handler == nil {
		return fmt.Errorf("handler for scheduled task type %s is nil", taskType)
	}
	if _, exists := s.handlers[taskType]; exists {
		return fmt.Errorf("handler for scheduled task type %s is already registered", taskType)
	}
	s.handlers[taskType] = handler
	return nil
}

func (s *ScheduledTaskService) Enqueue(ctx context.Context, taskType domain.ScheduledTaskType, taskKey string, scheduledAt time.Time, input any) (bool, *db_model.ScheduledTaskRun, error) {
	if !taskType.Valid() {
		return false, nil, fmt.Errorf("invalid scheduled task type %d", taskType)
	}
	if taskKey == "" {
		return false, nil, fmt.Errorf("scheduled task key is required")
	}
	rawInput, err := json.Marshal(input)
	if err != nil {
		return false, nil, fmt.Errorf("marshal scheduled task input: %w", err)
	}
	model := &db_model.ScheduledTaskRun{
		TaskType:      uint16(taskType),
		TaskKey:       taskKey,
		Status:        ScheduledTaskStatusQueued,
		ScheduledAt:   scheduledAt.UTC(),
		InputJSON:     rawInput,
		ErrorMessage:  "",
		ConfigVersion: s.currentConfigVersion(),
	}
	created, err := dal.ScheduledTaskRuns.CreateIfAbsent(ctx, s.db, model)
	if err != nil {
		return false, nil, err
	}
	if created {
		return true, model, nil
	}
	existing, err := dal.ScheduledTaskRuns.QueryByTypeAndKey(ctx, s.db, taskType, taskKey)
	return false, existing, err
}

func (s *ScheduledTaskService) ClaimAndExecuteNext(ctx context.Context, workerID string, claimTimeout time.Duration, types ...domain.ScheduledTaskType) (bool, error) {
	run, err := dal.ScheduledTaskRuns.ClaimNextDue(ctx, s.db, workerID, s.now().UTC(), claimTimeout, types...)
	if errors.Is(err, dal.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, s.execute(ctx, run)
}

func (s *ScheduledTaskService) execute(ctx context.Context, run *db_model.ScheduledTaskRun) error {
	taskType := domain.ScheduledTaskType(run.TaskType)
	if taskType == domain.ScheduledTaskTypeOpenListDocuments {
		if s.runtime == nil || s.runtime.Config() == nil {
			return s.fail(ctx, run.ID, fmt.Errorf("OpenList scheduled task config unavailable"))
		}
		reason, err := openListScheduledRunSkipReason(run, s.runtime.Config(), s.now())
		if err != nil {
			return s.fail(ctx, run.ID, err)
		}
		if reason != "" {
			return s.skip(ctx, run, reason)
		}
	}
	handler, ok := s.handlers[taskType]
	if !ok {
		return s.fail(ctx, run.ID, fmt.Errorf("no handler registered for scheduled task type %s", taskType))
	}
	output, err := handler(ctx, json.RawMessage(run.InputJSON))
	if err != nil {
		return s.fail(ctx, run.ID, err)
	}
	rawOutput, err := json.Marshal(output)
	if err != nil {
		return s.fail(ctx, run.ID, fmt.Errorf("marshal scheduled task output: %w", err))
	}
	now := s.now().UTC()
	if err := dal.ScheduledTaskRuns.UpdateByID(ctx, s.db, run.ID, map[string]any{
		"status":        ScheduledTaskStatusSucceeded,
		"output_json":   rawOutput,
		"error_message": "",
		"finished_at":   now,
	}); err != nil {
		return err
	}
	if s.logger != nil {
		s.logger.InfoContext(ctx, "scheduled task completed", "task_run_id", run.ID, "task_type", taskType.String(), "task_key", run.TaskKey)
	}
	return nil
}

func openListScheduledRunSkipReason(run *db_model.ScheduledTaskRun, cfg *config.Config, now time.Time) (string, error) {
	schedule := cfg.Scheduler.OpenListDocumentIngestion
	if !cfg.Scheduler.Enabled || !schedule.Enabled || !cfg.ExternalDocuments.OpenList.Enabled {
		return "OPENLIST_SCHEDULE_DISABLED", nil
	}
	location, err := time.LoadLocation(cfg.Meta.Timezone)
	if err != nil {
		return "", fmt.Errorf("load OpenList scheduler timezone %q: %w", cfg.Meta.Timezone, err)
	}
	now = now.In(location)
	scheduledAt := dailyScheduledAt(now, schedule.Hour, schedule.Minute)
	// Retire legacy hourly keys, old dates, and schedules changed since enqueue.
	if run.TaskKey != scheduledAt.Format(time.DateOnly) || !run.ScheduledAt.Equal(scheduledAt) {
		return "OBSOLETE_OPENLIST_SCHEDULE", nil
	}
	if !withinScheduledMinute(now, scheduledAt) {
		return "MISSED_OPENLIST_START_WINDOW", nil
	}
	// A crashed batch must not be reclaimed into another round of model calls.
	if run.AttemptCount > 1 {
		return "OPENLIST_DAILY_ATTEMPT_ALREADY_STARTED", nil
	}
	return "", nil
}

func (s *ScheduledTaskService) skip(ctx context.Context, run *db_model.ScheduledTaskRun, reason string) error {
	rawOutput, err := json.Marshal(map[string]string{"skip_reason": reason})
	if err != nil {
		return fmt.Errorf("marshal scheduled task skip reason: %w", err)
	}
	if err := dal.ScheduledTaskRuns.UpdateByID(ctx, s.db, run.ID, map[string]any{
		"status":        ScheduledTaskStatusSkipped,
		"output_json":   rawOutput,
		"error_message": "",
		"finished_at":   s.now().UTC(),
	}); err != nil {
		return err
	}
	if s.logger != nil {
		s.logger.InfoContext(ctx, "scheduled task skipped", "task_run_id", run.ID, "task_type", run.TaskType, "task_key", run.TaskKey, "skip_reason", reason)
	}
	return nil
}

func (s *ScheduledTaskService) fail(ctx context.Context, id int64, taskErr error) error {
	now := s.now().UTC()
	_ = dal.ScheduledTaskRuns.UpdateByID(context.WithoutCancel(ctx), s.db, id, map[string]any{
		"status":        ScheduledTaskStatusFailed,
		"error_message": taskErr.Error(),
		"finished_at":   now,
	})
	return taskErr
}

func (s *ScheduledTaskService) currentConfigVersion() int64 {
	if s.runtime == nil || s.runtime.Config() == nil {
		return 0
	}
	return s.runtime.Config().Meta.ConfigVersion
}
