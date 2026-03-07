package report

import (
	"context"

	"finance-sys/internal/domain"
	"finance-sys/internal/repository"
)

type Service struct {
	repo *repository.Repository
}

func NewService(repo *repository.Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Scorecards(ctx context.Context, limit int32) ([]domain.ExpertScorecard, error) {
	evaluations, err := s.repo.ListEvaluations(ctx, limit)
	if err != nil {
		return nil, err
	}
	plans, err := s.repo.ListPlans(ctx, limit)
	if err != nil {
		return nil, err
	}

	evaluationByPlan := make(map[int64]domain.PlanEvaluation, len(evaluations))
	for _, item := range evaluations {
		evaluationByPlan[item.PlanID] = item
	}

	type aggregate struct {
		domain.ExpertScorecard
	}
	scorecards := make(map[string]*aggregate)
	for _, plan := range plans {
		evaluation, ok := evaluationByPlan[plan.ID]
		if !ok {
			continue
		}
		signal, err := s.repo.GetSignalByID(ctx, plan.SignalID)
		if err != nil {
			continue
		}
		key := signal.ExpertName + "|" + signal.ExpertOrg
		current, ok := scorecards[key]
		if !ok {
			current = &aggregate{ExpertScorecard: domain.ExpertScorecard{
				ExpertName: signal.ExpertName,
				ExpertOrg:  signal.ExpertOrg,
			}}
			scorecards[key] = current
		}
		current.PlanCount++
		current.AveragePNLPct += evaluation.PNLPct
		current.AverageMFEPct += evaluation.MFEPct
		current.AverageMAEPct += evaluation.MAEPct
		current.AverageExcess += evaluation.ExcessReturnPct
		switch evaluation.Status {
		case "SUCCESS", "WEAK_SUCCESS":
			current.SuccessCount++
		case "FAIL":
			current.FailCount++
		}
	}

	items := make([]domain.ExpertScorecard, 0, len(scorecards))
	for _, item := range scorecards {
		if item.PlanCount > 0 {
			item.WinRate = float64(item.SuccessCount) / float64(item.PlanCount)
			item.AveragePNLPct /= float64(item.PlanCount)
			item.AverageMFEPct /= float64(item.PlanCount)
			item.AverageMAEPct /= float64(item.PlanCount)
			item.AverageExcess /= float64(item.PlanCount)
		}
		items = append(items, item.ExpertScorecard)
	}
	return items, nil
}
