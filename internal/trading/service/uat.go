package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"finance-sys/internal/dal"
	"finance-sys/internal/domain/db_model"
	"finance-sys/internal/rules"
	tradingdomain "finance-sys/internal/trading/domain"
	"finance-sys/internal/trading/instrument"
	"finance-sys/internal/utils"

	"gorm.io/gorm"
)

const (
	oneLotUATBuyConfirm  = "SIMULATION_ONE_LOT"
	oneLotUATSellConfirm = "SIMULATION_ONE_LOT_SELL"
)

type OneLotUATRequest struct {
	UATKey    string `json:"uat_key"`
	Symbol    string `json:"symbol"`
	Market    string `json:"market"`
	Action    string `json:"action"`
	Confirm   string `json:"confirm"`
	AssetType string `json:"asset_type"`
}

func (s *Service) ExecuteOneLotUAT(ctx context.Context, request OneLotUATRequest) (*RunView, error) {
	cfg := s.runtime.Config()
	if cfg == nil {
		return nil, fmt.Errorf("runtime config unavailable")
	}
	request.UATKey = strings.TrimSpace(request.UATKey)
	request.Symbol = strings.TrimSpace(request.Symbol)
	request.Market = strings.ToUpper(strings.TrimSpace(request.Market))
	request.Action = strings.ToUpper(strings.TrimSpace(request.Action))
	expectedConfirm := oneLotUATBuyConfirm
	if request.Action == "SELL" {
		expectedConfirm = oneLotUATSellConfirm
	}
	if request.Confirm != expectedConfirm {
		return nil, fmt.Errorf("confirm must equal %s for %s", expectedConfirm, request.Action)
	}
	security, err := instrument.Parse(request.Symbol, request.Market, request.AssetType)
	if err != nil || request.UATKey == "" || (request.Action != "BUY" && request.Action != "SELL") {
		return nil, fmt.Errorf("valid canonical security, uat_key and BUY/SELL action are required")
	}
	unit, err := instrument.UnitRule(security.BoardType)
	if err != nil {
		return nil, err
	}
	volume := unit.MinimumBuyVolume
	if cfg.Trading.Environment != "SIMULATION" || cfg.Trading.AllowLive || !cfg.Trading.Bridge.SimulationOnly {
		return nil, fmt.Errorf("one-lot UAT is restricted to simulation")
	}
	if !cfg.Trading.Enabled || cfg.Trading.KillSwitch {
		return nil, fmt.Errorf("Nacos trading switch does not allow UAT")
	}
	if cfg.Trading.Scheduler.Enabled || cfg.Trading.Reconciliation.Enabled {
		return nil, fmt.Errorf("automatic trading and reconciliation must remain disabled during UAT")
	}
	now := s.now().Truncate(time.Millisecond)
	if !tradingSessionOpen(now, cfg.Trading) {
		return nil, fmt.Errorf("one-lot UAT must run inside a configured A-share trading window")
	}
	if !uatAllowed(cfg.Trading, request.UATKey, security.EastmoneySymbol, now) {
		return nil, fmt.Errorf("temporary symbol-scoped UAT authorization required in Nacos")
	}
	runKey := hashParts("one-lot-uat", request.Action, request.UATKey, security.TSCode)
	if existing, err := dal.TradingAgentRuns.QueryByRunKey(ctx, s.db, runKey); err == nil {
		return s.GetRun(ctx, existing.ID)
	} else if !errors.Is(err, dal.ErrNotFound) {
		return nil, err
	}
	control, err := dal.TradingRuntimeControls.KillSwitch(ctx, s.db)
	if err != nil || control.Enabled {
		return nil, fmt.Errorf("runtime kill switch does not allow UAT")
	}
	openOrders, err := dal.TradingOrders.QueryOpen(ctx, s.db)
	if err != nil {
		return nil, err
	}
	if len(openOrders) != 0 {
		return nil, fmt.Errorf("one-lot UAT requires zero open orders")
	}
	eastmoneyCode := security.EastmoneySymbol
	snapshot, err := s.freshBridgeSnapshot(ctx, "uat")
	if err != nil {
		return nil, err
	}
	health, positions := snapshot.Health, snapshot.Positions
	if health.Status != "READY" {
		return nil, fmt.Errorf("Bridge must be READY before one-lot UAT")
	}
	position := findPosition(positions, eastmoneyCode)
	if request.Action == "BUY" && position != nil && position.Volume > 0 {
		return nil, fmt.Errorf("BUY UAT requires no existing position for the symbol")
	}
	var cycleID *int64
	if request.Action == "SELL" {
		if position == nil || position.Volume != volume || position.AvailableVolume < volume {
			return nil, fmt.Errorf("SELL UAT requires exactly %d held and provider-sellable shares", volume)
		}
		cycle, e := dal.TradingPositionCycles.QueryOpenBySymbol(ctx, s.db, cfg.Trading.Bridge.ExpectedAccountID, eastmoneyCode)
		if e != nil {
			return nil, fmt.Errorf("UAT position must have a tracked cycle: %w", e)
		}
		cycleID = &cycle.ID
	}
	quotes, err := s.refreshBridgeQuotes(ctx, []string{eastmoneyCode}, now)
	if err != nil {
		return nil, fmt.Errorf("fresh Bridge quote unavailable: %w", err)
	}
	if len(quotes) != 1 {
		return nil, fmt.Errorf("fresh Bridge quote unavailable: expected one quote, got %d", len(quotes))
	}
	limitPrice, err := rules.TradingPrice(request.Action, quotes[0], s.now(), time.Duration(cfg.Trading.Risk.MaxSnapshotAgeSeconds)*time.Second)
	if err != nil {
		return nil, err
	}
	validUntil := validUntilForWindow(now, 5*time.Minute, cfg.Trading.Scheduler)
	intentDomain := tradingdomain.TradeIntent{
		IntentKey: uatIntentKey(request.Action, request.UATKey, security.TSCode), Symbol: security.Symbol, TSCode: security.TSCode, Market: security.Market,
		AssetType: security.AssetType, BoardType: security.BoardType, Action: request.Action, ProposedOrderType: "LIMIT", ProposedLimitPrice: limitPrice, PositionCycleID: cycleID,
		ProposedPositionRatio: "0.10000000", ProposedVolume: &volume, ValidFrom: now.Add(-time.Minute),
		ValidUntil: validUntil, Confidence: "1.00000000", EvidenceRefs: []string{"uat:" + request.UATKey},
		Reason: "explicit one-lot Eastmoney simulation " + request.Action + " UAT",
	}
	requestJSON, _ := json.Marshal(request)
	rawOutputJSON, _ := json.Marshal(map[string]any{"schema_version": tradingdomain.SchemaVersionIntent, "intent": intentDomain})
	run := db_model.TradingAgentRun{
		RunKey: runKey, TriggerType: "UAT_MANUAL", Status: "RUNNING", DecisionProvider: "UAT_MANUAL",
		StrategyName: "one-lot-simulation-uat", StrategyVersion: cfg.Trading.Decision.StrategyVersion,
		SchemaVersion: cfg.Trading.Agent.SchemaVersion, PromptVersion: "uat-manual-v1",
		ToolContractVersion: cfg.Trading.Decision.ToolContractVersion, AsOfTime: now, InputCursor: "",
		InputSnapshotHash: utils.SHA256Hex(requestJSON), RequestJSON: requestJSON, RawOutputJSON: rawOutputJSON,
		CandidateCount: 1, IntentCount: 1, DryRun: false, WorkerID: "cmd/trading-uat", QueuedAt: now,
		StartedAt: &now, ErrorMessage: "", ConfigVersion: cfg.Meta.ConfigVersion,
	}
	evidenceJSON, _ := json.Marshal(intentDomain.EvidenceRefs)
	rawIntentJSON, _ := json.Marshal(intentDomain)
	intent := db_model.TradingIntent{
		IntentKey: intentDomain.IntentKey, Symbol: security.Symbol, TSCode: security.TSCode, Market: security.Market, AssetType: security.AssetType, PositionCycleID: cycleID,
		BoardType: security.BoardType, EastmoneySymbol: eastmoneyCode, TradeAction: request.Action, Status: "PROPOSED",
		ProposedOrderType: "LIMIT", ProposedLimitPrice: &limitPrice, ProposedPositionRatio: &intentDomain.ProposedPositionRatio,
		ProposedVolume: &volume, Confidence: intentDomain.Confidence, ValidFrom: intentDomain.ValidFrom, ValidUntil: validUntil,
		Reason: intentDomain.Reason, EvidenceRefsJSON: evidenceJSON, RawIntentJSON: rawIntentJSON,
		RejectionMessage: "", StrategyVersion: cfg.Trading.Decision.StrategyVersion, ConfigVersion: cfg.Meta.ConfigVersion,
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := dal.TradingAgentRuns.Create(ctx, tx, &run); err != nil {
			return err
		}
		intent.TradingAgentRunID = run.ID
		return dal.TradingIntents.Create(ctx, tx, &intent)
	}); err != nil {
		return nil, err
	}
	if err := s.evaluateIntentWithOptions(ctx, &intent, false, true); err != nil {
		finished := time.Now()
		_ = dal.TradingAgentRuns.Update(ctx, s.db, run.ID, map[string]any{"status": "FAILED", "finished_at": finished, "error_code": "UAT_EVALUATION_FAILED", "error_message": err.Error()})
		return nil, err
	}
	finished := time.Now()
	if err := dal.TradingAgentRuns.Update(ctx, s.db, run.ID, map[string]any{"status": "ORDER_CREATED", "finished_at": finished}); err != nil {
		return nil, err
	}
	view, err := s.GetRun(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	if len(view.Orders) != 1 || view.Orders[0].Volume != volume {
		_ = dal.TradingAgentRuns.Update(ctx, s.db, run.ID, map[string]any{"status": "FAILED", "error_code": "UAT_ORDER_NOT_CREATED", "error_message": "risk evaluation did not create exactly one minimum-unit order"})
		return view, fmt.Errorf("UAT did not create one %d-share order; intent_status=%s", volume, view.Intents[0].Status)
	}
	return view, nil
}

func isSixDigitSymbol(value string) bool {
	if len(value) != 6 {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func uatIntentKey(action, campaign, tsCode string) string {
	return hashParts("one-lot-uat-intent", action, campaign, tsCode)
}

func oneLotUATLimitPrice(latest, action string) (string, error) {
	price, ok := new(big.Rat).SetString(strings.TrimSpace(latest))
	if !ok || price.Sign() <= 0 {
		return "", fmt.Errorf("invalid latest quote")
	}
	if strings.EqualFold(action, "SELL") {
		price.Mul(price, big.NewRat(998, 1000))
	} else {
		price.Mul(price, big.NewRat(1002, 1000))
	}
	scaled := new(big.Rat).Mul(price, big.NewRat(100, 1))
	cents, remainder := new(big.Int), new(big.Int)
	cents.QuoRem(scaled.Num(), scaled.Denom(), remainder)
	if !strings.EqualFold(action, "SELL") && remainder.Sign() > 0 {
		cents.Add(cents, big.NewInt(1))
	}
	value := cents.Int64()
	return fmt.Sprintf("%d.%02d", value/100, value%100), nil
}
