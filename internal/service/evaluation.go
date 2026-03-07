package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/domain"
	"finance-sys/internal/market"
	"finance-sys/internal/repository"
)

type evaluator interface {
	Evaluate(plan domain.TradePlan, bars []domain.MinuteBar, daily domain.DailyBar, benchmarkReturnPct float64, configVersion int64) domain.PlanEvaluation
}

type EvaluationService struct {
	repo      *repository.Repository
	runtime   *config.Runtime
	market    marketProvider
	evaluator evaluator
	logger    *slog.Logger
}

func NewEvaluationService(repo *repository.Repository, runtime *config.Runtime, marketProvider marketProvider, evaluator evaluator, logger *slog.Logger) *EvaluationService {
	return &EvaluationService{
		repo:      repo,
		runtime:   runtime,
		market:    marketProvider,
		evaluator: evaluator,
		logger:    logger,
	}
}

func (s *EvaluationService) EvaluateTradeDate(ctx context.Context, tradeDate time.Time) error {
	cfg := s.runtime.Config()
	plans, err := s.repo.ListApprovedPlansForTradeDateWithoutEvaluation(ctx, tradeDate)
	if err != nil {
		return err
	}
	for _, plan := range plans {
		if err := s.evaluatePlan(ctx, plan, cfg); err != nil {
			s.logger.Error("evaluate plan", "plan_id", plan.ID, "error", err.Error())
		}
	}
	return nil
}

func (s *EvaluationService) evaluatePlan(ctx context.Context, plan domain.TradePlan, cfg *config.Config) error {
	dailyBars, err := s.market.GetDailyBars(ctx, plan.Symbol, plan.TradeDate, plan.TradeDate.Add(24*time.Hour), "qfq")
	if err != nil {
		return err
	}
	if len(dailyBars) == 0 {
		return fmt.Errorf("missing daily bar for %s", plan.Symbol)
	}

	minuteBars, err := s.market.GetMinuteBars(ctx, plan.Symbol, plan.TradeDate, plan.TradeDate.Add(24*time.Hour), cfg.Evaluation.MinuteBarInterval, "qfq")
	if err != nil && err != market.ErrNotSupported {
		return err
	}
	benchmarkReturn := 0.0
	if cfg.Evaluation.BenchmarkSymbol != "" {
		benchmarkBars, err := s.market.GetDailyBars(ctx, cfg.Evaluation.BenchmarkSymbol, plan.TradeDate.AddDate(0, 0, -2), plan.TradeDate.Add(24*time.Hour), "qfq")
		if err == nil && len(benchmarkBars) > 1 {
			last := benchmarkBars[len(benchmarkBars)-1]
			prev := benchmarkBars[len(benchmarkBars)-2]
			if prev.Close != 0 {
				benchmarkReturn = ((last.Close - prev.Close) / prev.Close) * 100
			}
		}
	}

	evaluation := s.evaluator.Evaluate(plan, minuteBars, dailyBars[len(dailyBars)-1], benchmarkReturn, cfg.Meta.ConfigVersion)
	_, err = s.repo.CreateEvaluation(ctx, evaluation)
	return err
}
