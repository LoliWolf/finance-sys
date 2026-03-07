package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/service"

	"github.com/robfig/cron/v3"
)

type Scheduler struct {
	cron       *cron.Cron
	documents  *service.DocumentService
	evaluation *service.EvaluationService
	runtime    *config.Runtime
	logger     *slog.Logger
}

func New(runtime *config.Runtime, documents *service.DocumentService, evaluation *service.EvaluationService, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		cron:       cron.New(cron.WithSeconds()),
		documents:  documents,
		evaluation: evaluation,
		runtime:    runtime,
		logger:     logger,
	}
}

func (s *Scheduler) Register() error {
	cfg := s.runtime.Config()
	if cfg == nil {
		return nil
	}
	pollSeconds := cfg.Service.Worker.JobPollIntervalSeconds
	if pollSeconds <= 0 {
		pollSeconds = 5
	}
	if _, err := s.cron.AddFunc(fmt.Sprintf("@every %ds", pollSeconds), func() {
		if err := s.documents.ProcessPendingDocuments(context.Background(), int32(cfg.Runtime.MaxParallelJobs)); err != nil {
			s.logger.Error("scheduled document processing", "error", err.Error())
		}
	}); err != nil {
		return err
	}
	if _, err := s.cron.AddFunc("0 */10 * * * *", func() {
		if err := s.evaluation.EvaluateTradeDate(context.Background(), time.Now()); err != nil {
			s.logger.Error("scheduled evaluation", "error", err.Error())
		}
	}); err != nil {
		return err
	}
	return nil
}

func (s *Scheduler) Start() {
	s.cron.Start()
}

func (s *Scheduler) Stop(ctx context.Context) error {
	stopCtx := s.cron.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-stopCtx.Done():
		return nil
	}
}
