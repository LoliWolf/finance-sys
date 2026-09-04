package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/domain"
	tradingservice "finance-sys/internal/trading/service"
)

const maxScheduledTasksPerTick = 100

type AutomaticScheduler struct {
	tasks             *ScheduledTaskService
	marketData        *MarketDataService
	evaluation        *RecommendationEvaluationService
	externalDocuments *ExternalDocumentIngestionService
	trading           *tradingservice.Service
	runtime           *config.Runtime
	logger            *slog.Logger
	workerID          string
	now               func() time.Time
}

func NewAutomaticScheduler(
	tasks *ScheduledTaskService,
	marketData *MarketDataService,
	evaluation *RecommendationEvaluationService,
	externalDocuments *ExternalDocumentIngestionService,
	trading *tradingservice.Service,
	runtime *config.Runtime,
	logger *slog.Logger,
) (*AutomaticScheduler, error) {
	host, _ := os.Hostname()
	if host == "" {
		host = "unknown-host"
	}
	scheduler := &AutomaticScheduler{
		tasks:             tasks,
		marketData:        marketData,
		evaluation:        evaluation,
		externalDocuments: externalDocuments,
		trading:           trading,
		runtime:           runtime,
		logger:            logger,
		workerID:          host + "-automatic-scheduler",
		now:               time.Now,
	}
	if tasks == nil {
		return nil, fmt.Errorf("scheduled task service is required")
	}
	if err := tasks.RegisterHandler(domain.ScheduledTaskTypeStockDailyPreviousDay, scheduler.handleStockDailyPreviousDay); err != nil {
		return nil, err
	}
	if err := tasks.RegisterHandler(domain.ScheduledTaskTypeEvaluationRecent, scheduler.handleEvaluationRecent); err != nil {
		return nil, err
	}
	if err := tasks.RegisterHandler(domain.ScheduledTaskTypeOpenListDocuments, scheduler.handleOpenListDocuments); err != nil {
		return nil, err
	}
	if err := tasks.RegisterHandler(domain.ScheduledTaskTypeSecurityMasterRefresh, scheduler.handleSecurityMasterRefresh); err != nil {
		return nil, err
	}
	if err := tasks.RegisterHandler(domain.ScheduledTaskTypeTradingPreopenDecision, scheduler.handleTradingDecision); err != nil {
		return nil, err
	}
	if err := tasks.RegisterHandler(domain.ScheduledTaskTypeTradingExitMonitor, scheduler.handleTradingDecision); err != nil {
		return nil, err
	}
	if err := tasks.RegisterHandler(domain.ScheduledTaskTypeTradingReconciliation, scheduler.handleTradingReconciliation); err != nil {
		return nil, err
	}
	if err := tasks.RegisterHandler(domain.ScheduledTaskTypeTradingEODSnapshot, scheduler.handleTradingReconciliation); err != nil {
		return nil, err
	}
	if err := tasks.RegisterHandler(domain.ScheduledTaskTypeTradingPreflight, scheduler.handleTradingPreflight); err != nil {
		return nil, err
	}
	if err := tasks.RegisterHandler(domain.ScheduledTaskTypeTradingCancelOpen, scheduler.handleTradingCancelOpen); err != nil {
		return nil, err
	}
	return scheduler, nil
}

func (s *AutomaticScheduler) Run(ctx context.Context) {
	if s == nil || s.tasks == nil || s.runtime == nil {
		return
	}
	interval := time.Second
	for {
		cfg := s.runtime.Config()
		if cfg != nil && cfg.Scheduler.Enabled {
			if cfg.Scheduler.PollIntervalMS > 0 {
				interval = time.Duration(cfg.Scheduler.PollIntervalMS) * time.Millisecond
			}
			if err := s.tick(ctx, cfg); err != nil && s.logger != nil {
				s.logger.ErrorContext(ctx, "automatic scheduler tick failed", "error", err.Error())
			}
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (s *AutomaticScheduler) tick(ctx context.Context, cfg *config.Config) error {
	location, err := time.LoadLocation(cfg.Meta.Timezone)
	if err != nil {
		return fmt.Errorf("load scheduler timezone %q: %w", cfg.Meta.Timezone, err)
	}
	now := s.now().In(location)
	if err := s.enqueueDueTasks(ctx, cfg, now); err != nil {
		return err
	}
	claimTimeout := time.Duration(cfg.Scheduler.ClaimTimeoutMS) * time.Millisecond
	for i := 0; i < maxScheduledTasksPerTick; i++ {
		claimed, executeErr := s.tasks.ClaimAndExecuteNext(ctx, s.workerID, claimTimeout)
		if executeErr != nil {
			return executeErr
		}
		if !claimed {
			return nil
		}
	}
	return nil
}

func (s *AutomaticScheduler) enqueueDueTasks(ctx context.Context, cfg *config.Config, now time.Time) error {
	masterSchedule := cfg.Scheduler.SecurityMasterRefresh
	if masterSchedule.Enabled {
		scheduledAt := dailyScheduledAt(now, masterSchedule.Hour, masterSchedule.Minute)
		if !now.Before(scheduledAt) {
			if _, _, err := s.tasks.Enqueue(
				ctx,
				domain.ScheduledTaskTypeSecurityMasterRefresh,
				scheduledAt.Format(time.DateOnly),
				scheduledAt,
				SecurityMasterRefreshRequest{AsOfDate: scheduledAt.Format(time.DateOnly)},
			); err != nil {
				return fmt.Errorf("enqueue security master refresh task: %w", err)
			}
		}
	}

	stockSchedule := cfg.Scheduler.StockDailyPreviousDay
	if stockSchedule.Enabled {
		scheduledAt := dailyScheduledAt(now, stockSchedule.Hour, stockSchedule.Minute)
		if !now.Before(scheduledAt) {
			tradeDate := scheduledAt.AddDate(0, 0, -1).Format(time.DateOnly)
			if _, _, err := s.tasks.Enqueue(
				ctx,
				domain.ScheduledTaskTypeStockDailyPreviousDay,
				scheduledAt.Format(time.DateOnly),
				scheduledAt,
				StockDailySyncRequest{TradeDate: tradeDate},
			); err != nil {
				return fmt.Errorf("enqueue previous-day stock daily task: %w", err)
			}
		}
	}

	evaluationSchedule := cfg.Scheduler.RecommendationEvaluationRecent
	if evaluationSchedule.Enabled {
		scheduledAt := dailyScheduledAt(now, evaluationSchedule.Hour, evaluationSchedule.Minute)
		if !now.Before(scheduledAt) {
			onlyActive := false
			request := RecommendationEvaluationRequest{
				DateFrom:     scheduledAt.AddDate(0, 0, -evaluationSchedule.LookbackDays).Format(time.DateOnly),
				DateTo:       scheduledAt.Format(time.DateOnly),
				ForceRebuild: false,
				OnlyActive:   &onlyActive,
			}
			if _, _, err := s.tasks.Enqueue(
				ctx,
				domain.ScheduledTaskTypeEvaluationRecent,
				scheduledAt.Format(time.DateOnly),
				scheduledAt,
				request,
			); err != nil {
				return fmt.Errorf("enqueue recent recommendation evaluation task: %w", err)
			}
		}
	}

	openListSchedule := cfg.Scheduler.OpenListDocumentIngestion
	if openListSchedule.Enabled {
		scheduledAt := hourlyScheduledAt(now, openListSchedule.Minute)
		if !now.Before(scheduledAt) {
			if _, _, err := s.tasks.Enqueue(
				ctx,
				domain.ScheduledTaskTypeOpenListDocuments,
				scheduledAt.Format("2006-01-02T15"),
				scheduledAt,
				OpenListDocumentIngestionRequest{FullScan: false},
			); err != nil {
				return fmt.Errorf("enqueue OpenList document ingestion task: %w", err)
			}
		}
	}

	if cfg.Trading.Scheduler.Enabled {
		preflight := cfg.Trading.Scheduler.Preflight
		if preflight.Enabled {
			scheduledAt := dailyScheduledAt(now, preflight.Hour, preflight.Minute)
			if !now.Before(scheduledAt) {
				if _, _, err := s.tasks.Enqueue(ctx, domain.ScheduledTaskTypeTradingPreflight, scheduledAt.Format(time.DateOnly), scheduledAt, map[string]any{"trade_date": scheduledAt.Format(time.DateOnly)}); err != nil {
					return fmt.Errorf("enqueue trading preflight: %w", err)
				}
			}
		}
		preOpen := cfg.Trading.Scheduler.PreOpenDecision
		if preOpen.Enabled {
			scheduledAt := dailyScheduledAt(now, preOpen.Hour, preOpen.Minute)
			if !now.Before(scheduledAt) {
				request := tradingservice.RunRequest{TriggerType: "PRE_OPEN", AsOfTime: scheduledAt, DryRun: cfg.Trading.KillSwitch || !cfg.Trading.Enabled}
				if _, _, err := s.tasks.Enqueue(ctx, domain.ScheduledTaskTypeTradingPreopenDecision, scheduledAt.Format(time.DateOnly), scheduledAt, request); err != nil {
					return fmt.Errorf("enqueue trading pre-open decision: %w", err)
				}
			}
		}

		for label, schedule := range map[string]config.DailyTaskScheduleConfig{"MORNING": cfg.Trading.Scheduler.MorningCancel, "AFTERNOON": cfg.Trading.Scheduler.AfternoonCancel} {
			if !schedule.Enabled {
				continue
			}
			scheduledAt := dailyScheduledAt(now, schedule.Hour, schedule.Minute)
			if !now.Before(scheduledAt) {
				if _, _, err := s.tasks.Enqueue(ctx, domain.ScheduledTaskTypeTradingCancelOpen, scheduledAt.Format(time.DateOnly)+"-"+label, scheduledAt, map[string]any{"session": label}); err != nil {
					return fmt.Errorf("enqueue trading open-order cancellation: %w", err)
				}
			}
		}

		interval := cfg.Trading.Reconciliation.IntervalSeconds
		if cfg.Trading.Reconciliation.Enabled && interval > 0 {
			scheduledAt := now.Truncate(time.Duration(interval) * time.Second)
			if _, _, err := s.tasks.Enqueue(ctx, domain.ScheduledTaskTypeTradingReconciliation, scheduledAt.Format("2006-01-02T15:04:05"), scheduledAt, map[string]string{"run_type": "INTERVAL"}); err != nil {
				return fmt.Errorf("enqueue trading reconciliation: %w", err)
			}
		}

		eod := cfg.Trading.Scheduler.EndOfDayReconcile
		if eod.Enabled {
			scheduledAt := dailyScheduledAt(now, eod.Hour, eod.Minute)
			if !now.Before(scheduledAt) {
				if _, _, err := s.tasks.Enqueue(ctx, domain.ScheduledTaskTypeTradingEODSnapshot, scheduledAt.Format(time.DateOnly), scheduledAt, map[string]string{"run_type": "EOD"}); err != nil {
					return fmt.Errorf("enqueue trading EOD reconciliation: %w", err)
				}
			}
		}
	}
	return nil
}

func dailyScheduledAt(now time.Time, hour int, minute int) time.Time {
	year, month, day := now.Date()
	return time.Date(year, month, day, hour, minute, 0, 0, now.Location())
}

func hourlyScheduledAt(now time.Time, minute int) time.Time {
	year, month, day := now.Date()
	return time.Date(year, month, day, now.Hour(), minute, 0, 0, now.Location())
}

func (s *AutomaticScheduler) handleStockDailyPreviousDay(ctx context.Context, rawInput json.RawMessage) (any, error) {
	if s.marketData == nil {
		return nil, fmt.Errorf("market data service is unavailable")
	}
	var request StockDailySyncRequest
	if err := json.Unmarshal(rawInput, &request); err != nil {
		return nil, fmt.Errorf("decode stock daily scheduled task input: %w", err)
	}
	return s.marketData.CreateStockDailySyncRun(ctx, request)
}

func (s *AutomaticScheduler) handleSecurityMasterRefresh(ctx context.Context, rawInput json.RawMessage) (any, error) {
	if s.marketData == nil {
		return nil, fmt.Errorf("market data service is unavailable")
	}
	var request SecurityMasterRefreshRequest
	if err := json.Unmarshal(rawInput, &request); err != nil {
		return nil, fmt.Errorf("decode security master refresh scheduled task input: %w", err)
	}
	return s.marketData.RefreshSecurityMaster(ctx, request)
}

func (s *AutomaticScheduler) handleEvaluationRecent(ctx context.Context, rawInput json.RawMessage) (any, error) {
	if s.evaluation == nil {
		return nil, fmt.Errorf("recommendation evaluation service is unavailable")
	}
	var request RecommendationEvaluationRequest
	if err := json.Unmarshal(rawInput, &request); err != nil {
		return nil, fmt.Errorf("decode evaluation scheduled task input: %w", err)
	}
	return s.evaluation.CreateScheduledRun(ctx, request)
}

func (s *AutomaticScheduler) handleOpenListDocuments(ctx context.Context, rawInput json.RawMessage) (any, error) {
	if s.externalDocuments == nil {
		return nil, fmt.Errorf("external document ingestion service is unavailable")
	}
	var request OpenListDocumentIngestionRequest
	if err := json.Unmarshal(rawInput, &request); err != nil {
		return nil, fmt.Errorf("decode OpenList document ingestion scheduled task input: %w", err)
	}
	return s.externalDocuments.SyncOpenList(ctx, request)
}

func (s *AutomaticScheduler) handleTradingDecision(ctx context.Context, rawInput json.RawMessage) (any, error) {
	if s.trading == nil {
		return nil, fmt.Errorf("trading service is unavailable")
	}
	var request tradingservice.RunRequest
	if err := json.Unmarshal(rawInput, &request); err != nil {
		return nil, fmt.Errorf("decode trading decision scheduled task input: %w", err)
	}
	return s.trading.StartRun(ctx, request)
}

func (s *AutomaticScheduler) handleTradingReconciliation(ctx context.Context, rawInput json.RawMessage) (any, error) {
	if s.trading == nil {
		return nil, fmt.Errorf("trading service is unavailable")
	}
	var request struct {
		RunType string `json:"run_type"`
	}
	if err := json.Unmarshal(rawInput, &request); err != nil {
		return nil, fmt.Errorf("decode trading reconciliation scheduled task input: %w", err)
	}
	return s.trading.Reconcile(ctx, request.RunType)
}

func (s *AutomaticScheduler) handleTradingPreflight(ctx context.Context, rawInput json.RawMessage) (any, error) {
	if s.trading == nil {
		return nil, fmt.Errorf("trading service is unavailable")
	}
	return s.trading.Preflight(ctx, s.now())
}

func (s *AutomaticScheduler) handleTradingCancelOpen(ctx context.Context, rawInput json.RawMessage) (any, error) {
	if s.trading == nil {
		return nil, fmt.Errorf("trading service is unavailable")
	}
	return s.trading.CancelOpenOrders(ctx, "SCHEDULED_SESSION_END")
}
