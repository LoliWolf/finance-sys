package evaluation

import (
	"math"
	"time"

	"finance-sys/internal/domain"
)

type Evaluator struct{}

func New() *Evaluator {
	return &Evaluator{}
}

func (e *Evaluator) Evaluate(plan domain.TradePlan, bars []domain.MinuteBar, daily domain.DailyBar, benchmarkReturnPct float64, configVersion int64) domain.PlanEvaluation {
	if len(bars) == 0 {
		return evaluateFromDaily(plan, daily, benchmarkReturnPct, configVersion)
	}

	triggered := false
	entryPrice := 0.0
	exitPrice := 0.0
	status := "NOT_TRIGGERED"
	mfe := 0.0
	mae := 0.0

	for _, bar := range bars {
		if !triggered && touches(plan.EntryPrice, bar.Low, bar.High) {
			triggered = true
			entryPrice = plan.EntryPrice
			status = "OPEN"
		}
		if !triggered {
			continue
		}

		mfe = math.Max(mfe, favorableMove(plan, entryPrice, bar.High, bar.Low))
		mae = math.Min(mae, adverseMove(plan, entryPrice, bar.High, bar.Low))

		stopHit, takeHit := stopAndTakeHit(plan, bar.High, bar.Low)
		switch {
		case stopHit && takeHit:
			exitPrice = plan.StopLoss
			status = "FAIL"
		case takeHit:
			exitPrice = plan.TakeProfit
			status = "SUCCESS"
		case stopHit:
			exitPrice = plan.StopLoss
			status = "FAIL"
		}
		if exitPrice != 0 {
			break
		}
	}

	if triggered && exitPrice == 0 {
		exitPrice = daily.Close
		switch {
		case pnlPct(plan, entryPrice, exitPrice) > 0:
			status = "WEAK_SUCCESS"
		case pnlPct(plan, entryPrice, exitPrice) < 0:
			status = "FAIL"
		default:
			status = "OPEN"
		}
	}

	if !triggered {
		return domain.PlanEvaluation{
			PlanID:             plan.ID,
			TradeDate:          daily.TradeDate,
			Status:             "NOT_TRIGGERED",
			ClosePrice:         daily.Close,
			BenchmarkReturnPct: benchmarkReturnPct,
			ExcessReturnPct:    -benchmarkReturnPct,
			EvaluationReason:   "entry not touched during session",
			DataQualityFlag:    "MINUTE_OK",
			ConfigVersion:      configVersion,
		}
	}

	pnl := pnlPct(plan, entryPrice, exitPrice)
	return domain.PlanEvaluation{
		PlanID:             plan.ID,
		TradeDate:          daily.TradeDate,
		Status:             status,
		EntryPrice:         entryPrice,
		ExitPrice:          exitPrice,
		ClosePrice:         daily.Close,
		PNLPct:             pnl,
		MFEPct:             mfe,
		MAEPct:             math.Abs(mae),
		BenchmarkReturnPct: benchmarkReturnPct,
		ExcessReturnPct:    pnl - benchmarkReturnPct,
		EvaluationReason:   "evaluated with minute bars",
		DataQualityFlag:    "MINUTE_OK",
		ConfigVersion:      configVersion,
	}
}

func evaluateFromDaily(plan domain.TradePlan, daily domain.DailyBar, benchmarkReturnPct float64, configVersion int64) domain.PlanEvaluation {
	triggered := touches(plan.EntryPrice, daily.Low, daily.High)
	if !triggered {
		return domain.PlanEvaluation{
			PlanID:             plan.ID,
			TradeDate:          daily.TradeDate,
			Status:             "NOT_TRIGGERED",
			ClosePrice:         daily.Close,
			BenchmarkReturnPct: benchmarkReturnPct,
			ExcessReturnPct:    -benchmarkReturnPct,
			EvaluationReason:   "entry not touched by daily range",
			DataQualityFlag:    "DAILY_ONLY",
			ConfigVersion:      configVersion,
		}
	}

	stopHit, takeHit := stopAndTakeHit(plan, daily.High, daily.Low)
	exitPrice := daily.Close
	status := "OPEN"
	switch {
	case stopHit && takeHit:
		exitPrice = plan.StopLoss
		status = "FAIL"
	case takeHit:
		exitPrice = plan.TakeProfit
		status = "SUCCESS"
	case stopHit:
		exitPrice = plan.StopLoss
		status = "FAIL"
	case pnlPct(plan, plan.EntryPrice, daily.Close) > 0:
		status = "WEAK_SUCCESS"
	case pnlPct(plan, plan.EntryPrice, daily.Close) < 0:
		status = "FAIL"
	}

	pnl := pnlPct(plan, plan.EntryPrice, exitPrice)
	return domain.PlanEvaluation{
		PlanID:             plan.ID,
		TradeDate:          daily.TradeDate,
		Status:             status,
		EntryPrice:         plan.EntryPrice,
		ExitPrice:          exitPrice,
		ClosePrice:         daily.Close,
		PNLPct:             pnl,
		MFEPct:             favorableMove(plan, plan.EntryPrice, daily.High, daily.Low),
		MAEPct:             math.Abs(adverseMove(plan, plan.EntryPrice, daily.High, daily.Low)),
		BenchmarkReturnPct: benchmarkReturnPct,
		ExcessReturnPct:    pnl - benchmarkReturnPct,
		EvaluationReason:   "evaluated with daily bars only",
		DataQualityFlag:    "DAILY_ONLY",
		ConfigVersion:      configVersion,
	}
}

func touches(price float64, low float64, high float64) bool {
	return price >= low && price <= high
}

func stopAndTakeHit(plan domain.TradePlan, high float64, low float64) (bool, bool) {
	if plan.Direction == "LONG" {
		return low <= plan.StopLoss, high >= plan.TakeProfit
	}
	return high >= plan.StopLoss, low <= plan.TakeProfit
}

func favorableMove(plan domain.TradePlan, entry float64, high float64, low float64) float64 {
	if plan.Direction == "LONG" {
		return ((high - entry) / entry) * 100
	}
	return ((entry - low) / entry) * 100
}

func adverseMove(plan domain.TradePlan, entry float64, high float64, low float64) float64 {
	if plan.Direction == "LONG" {
		return ((low - entry) / entry) * 100
	}
	return ((entry - high) / entry) * 100
}

func pnlPct(plan domain.TradePlan, entry float64, exit float64) float64 {
	if entry == 0 {
		return 0
	}
	if plan.Direction == "LONG" {
		return ((exit - entry) / entry) * 100
	}
	return ((entry - exit) / entry) * 100
}

func tomorrow(base time.Time) time.Time {
	return base.Add(24 * time.Hour)
}
