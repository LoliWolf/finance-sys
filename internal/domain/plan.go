package domain

import "time"

type TradePlan struct {
	ID                int64     `json:"id"`
	SignalID          int64     `json:"signal_id"`
	DocumentID        int64     `json:"document_id"`
	Symbol            string    `json:"symbol"`
	Strategy          string    `json:"strategy"`
	TradeDate         time.Time `json:"trade_date"`
	Direction         string    `json:"direction"`
	EntryPrice        float64   `json:"entry_price"`
	StopLoss          float64   `json:"stop_loss"`
	TakeProfit        float64   `json:"take_profit"`
	InvalidationPrice float64   `json:"invalidation_price"`
	PositionPct       float64   `json:"position_pct"`
	Status            string    `json:"status"`
	Rationale         string    `json:"rationale"`
	ConfigVersion     int64     `json:"config_version"`
	RuleVersion       string    `json:"rule_version"`
	MarketSnapshotID  int64     `json:"market_snapshot_id"`
	ApprovedBy        string    `json:"approved_by"`
	ApprovedAt        time.Time `json:"approved_at"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type PlanEvaluation struct {
	ID                 int64     `json:"id"`
	PlanID             int64     `json:"plan_id"`
	TradeDate          time.Time `json:"trade_date"`
	Status             string    `json:"status"`
	EntryPrice         float64   `json:"entry_price"`
	ExitPrice          float64   `json:"exit_price"`
	ClosePrice         float64   `json:"close_price"`
	PNLPct             float64   `json:"pnl_pct"`
	MFEPct             float64   `json:"mfe_pct"`
	MAEPct             float64   `json:"mae_pct"`
	BenchmarkReturnPct float64   `json:"benchmark_return_pct"`
	ExcessReturnPct    float64   `json:"excess_return_pct"`
	EvaluationReason   string    `json:"evaluation_reason"`
	DataQualityFlag    string    `json:"data_quality_flag"`
	ConfigVersion      int64     `json:"config_version"`
	CreatedAt          time.Time `json:"created_at"`
}

type ExpertScorecard struct {
	ExpertName    string  `json:"expert_name"`
	ExpertOrg     string  `json:"expert_org"`
	PlanCount     int     `json:"plan_count"`
	SuccessCount  int     `json:"success_count"`
	FailCount     int     `json:"fail_count"`
	WinRate       float64 `json:"win_rate"`
	AveragePNLPct float64 `json:"average_pnl_pct"`
	AverageMFEPct float64 `json:"average_mfe_pct"`
	AverageMAEPct float64 `json:"average_mae_pct"`
	AverageExcess float64 `json:"average_excess_pct"`
}
