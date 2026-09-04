package service

import (
	"context"
	"time"

	"finance-sys/internal/domain/db_model"
	tradingdomain "finance-sys/internal/trading/domain"
)

type RunRequest struct {
	TriggerType string    `json:"trigger_type"`
	AsOfTime    time.Time `json:"as_of_time"`
	DryRun      bool      `json:"dry_run"`
}

type RunView struct {
	Run     db_model.TradingAgentRun `json:"run"`
	Intents []db_model.TradingIntent `json:"intents"`
	Orders  []db_model.TradingOrder  `json:"orders"`
}

type KillSwitchRequest struct {
	Enabled          bool   `json:"enabled"`
	Reason           string `json:"reason"`
	ChangedBy        string `json:"changed_by"`
	CancelOpenOrders bool   `json:"cancel_open_orders"`
}

type BloggerPerformance struct {
	BloggerID        int64   `json:"blogger_id"`
	WindowDays       int     `json:"window_days"`
	EvaluableCount   int64   `json:"evaluable_count"`
	WinCount         int64   `json:"win_count"`
	WinRate          float64 `json:"win_rate"`
	AverageReturn    float64 `json:"average_return"`
	UnevaluableCount int64   `json:"unevaluable_count"`
}

type MarketSnapshot struct {
	Symbol        string    `json:"symbol"`
	TSCode        string    `json:"ts_code"`
	BoardType     string    `json:"board_type"`
	Price         string    `json:"price"`
	TradeDate     string    `json:"trade_date"`
	ObservedAt    time.Time `json:"observed_at"`
	Source        string    `json:"source"`
	MissingReason string    `json:"missing_reason,omitempty"`
}

type DailyHistoryItem struct {
	Symbol     string  `json:"symbol"`
	TSCode     string  `json:"ts_code"`
	TradeDate  string  `json:"trade_date"`
	OpenPrice  float64 `json:"open_price"`
	HighPrice  float64 `json:"high_price"`
	LowPrice   float64 `json:"low_price"`
	ClosePrice float64 `json:"close_price"`
	PctChg     float64 `json:"pct_chg"`
	Volume     float64 `json:"volume"`
	Source     string  `json:"source"`
}

type PortfolioView struct {
	Account    *db_model.TradingAccountSnapshot   `json:"account,omitempty"`
	Positions  []db_model.TradingPositionSnapshot `json:"positions"`
	Cycles     []db_model.TradingPositionCycle    `json:"cycles"`
	OpenOrders []db_model.TradingOrder            `json:"open_orders"`
}

type RiskBudgetView struct {
	TradingEnabled      bool    `json:"trading_enabled"`
	NacosKillSwitch     bool    `json:"nacos_kill_switch"`
	RuntimeKillSwitch   bool    `json:"runtime_kill_switch"`
	MaxTotalRatio       float64 `json:"max_total_position_ratio"`
	MaxSymbolRatio      float64 `json:"max_symbol_position_ratio"`
	MaxSingleOrderRatio float64 `json:"max_single_order_ratio"`
	RiskVersion         string  `json:"risk_version"`
	ConfigVersion       int64   `json:"config_version"`
}

type BridgeClient interface {
	Health(ctx context.Context) (*tradingdomain.BridgeHealth, error)
	PlaceOrder(ctx context.Context, idempotencyKey string, request tradingdomain.BridgeOrderRequest) (*tradingdomain.BridgeCommandResponse, error)
	CancelOrder(ctx context.Context, clientOrderID, idempotencyKey string) (*tradingdomain.BridgeCommandResponse, error)
	RefreshSnapshot(ctx context.Context, idempotencyKey string) (*tradingdomain.BridgeCommandResponse, error)
	RefreshQuotes(ctx context.Context, symbols []string, idempotencyKey string) (*tradingdomain.BridgeCommandResponse, error)
	Quotes(ctx context.Context, symbols []string) ([]tradingdomain.QuoteSnapshot, error)
	SetKillSwitch(ctx context.Context, enabled bool, reason, idempotencyKey string) error
	ReconciliationSnapshot(ctx context.Context, cursor string) (*tradingdomain.ReconciliationSnapshot, error)
	Account(ctx context.Context) (*tradingdomain.AccountSnapshot, error)
	Positions(ctx context.Context) ([]tradingdomain.PositionSnapshot, error)
}

type AgentClient interface {
	Run(ctx context.Context, request tradingdomain.AgentRunRequest) (*tradingdomain.AgentRunResponse, error)
}
