package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strconv"
	"strings"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/dal"
	"finance-sys/internal/domain/db_model"
	tradingdomain "finance-sys/internal/trading/domain"
	"finance-sys/internal/trading/instrument"
	"finance-sys/internal/trading/policy"
	"finance-sys/internal/utils"

	"gorm.io/gorm"
)

type Service struct {
	db      *gorm.DB
	runtime *config.Runtime
	agent   AgentClient
	bridge  BridgeClient
	policy  *policy.Engine
	logger  *slog.Logger
}

func New(db *gorm.DB, runtime *config.Runtime, agent AgentClient, bridge BridgeClient, policyEngine *policy.Engine, logger *slog.Logger) *Service {
	return &Service{db: db, runtime: runtime, agent: agent, bridge: bridge, policy: policyEngine, logger: logger}
}

func (s *Service) StartRun(ctx context.Context, request RunRequest) (*RunView, error) {
	cfg := s.runtime.Config()
	if cfg == nil {
		return nil, fmt.Errorf("runtime config unavailable")
	}
	if request.TriggerType == "" {
		request.TriggerType = "MANUAL"
	}
	if request.AsOfTime.IsZero() {
		request.AsOfTime = time.Now()
	}
	request.AsOfTime = request.AsOfTime.Truncate(time.Millisecond)
	runKey := hashParts(cfg.Trading.Decision.StrategyVersion, request.TriggerType, request.AsOfTime.Format(time.RFC3339Nano), strconv.FormatBool(request.DryRun))
	if existing, err := dal.TradingAgentRuns.QueryByRunKey(ctx, s.db, runKey); err == nil {
		return s.GetRun(ctx, existing.ID)
	} else if !errors.Is(err, dal.ErrNotFound) {
		return nil, err
	}

	agentRequest := tradingdomain.AgentRunRequest{
		RunKey: runKey, AsOfTime: request.AsOfTime, TriggerType: request.TriggerType,
		StrategyName: cfg.Trading.Decision.StrategyName, StrategyVersion: cfg.Trading.Decision.StrategyVersion,
		DecisionProvider: cfg.Trading.Decision.Provider, ToolBaseURL: s.toolBaseURL(cfg),
		ConfigVersion: cfg.Meta.ConfigVersion, DryRun: request.DryRun,
	}
	requestJSON, _ := json.Marshal(agentRequest)
	now := time.Now()
	run := db_model.TradingAgentRun{
		RunKey: runKey, TriggerType: request.TriggerType, Status: "RUNNING", DecisionProvider: cfg.Trading.Decision.Provider,
		StrategyName: cfg.Trading.Decision.StrategyName, StrategyVersion: cfg.Trading.Decision.StrategyVersion,
		SchemaVersion: cfg.Trading.Agent.SchemaVersion, PromptVersion: "", ToolContractVersion: cfg.Trading.Decision.ToolContractVersion,
		AsOfTime: request.AsOfTime, InputCursor: "", InputSnapshotHash: utils.SHA256Hex(requestJSON),
		RequestJSON: requestJSON, RawOutputJSON: []byte(`{}`), DryRun: request.DryRun, WorkerID: "api",
		QueuedAt: now, StartedAt: &now, ErrorMessage: "", ConfigVersion: cfg.Meta.ConfigVersion,
	}
	if err := dal.TradingAgentRuns.Create(ctx, s.db, &run); err != nil {
		return nil, err
	}

	response, err := s.agent.Run(ctx, agentRequest)
	if err != nil {
		finished := time.Now()
		_ = dal.TradingAgentRuns.Update(ctx, s.db, run.ID, map[string]any{"status": "FAILED", "finished_at": finished, "error_code": "AGENT_CALL_FAILED", "error_message": err.Error()})
		return nil, fmt.Errorf("trading agent run %d failed: %w", run.ID, err)
	}
	rawOutput, _ := json.Marshal(response)
	if err := validateAgentResponse(cfg, agentRequest, response); err != nil {
		finished := time.Now()
		_ = dal.TradingAgentRuns.Update(ctx, s.db, run.ID, map[string]any{"status": "FAILED", "finished_at": finished, "raw_output_json": rawOutput, "error_code": "AGENT_SCHEMA_INVALID", "error_message": err.Error()})
		return nil, fmt.Errorf("trading agent run %d returned invalid output: %w", run.ID, err)
	}

	rejectedOutputs := 0
	createdIntents := 0
	intentIDs := make(map[string]int64, len(response.Intents))
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, intent := range response.Intents {
			security, securityErr := instrument.Parse(intent.Symbol, intent.Market, intent.AssetType)
			if securityErr != nil {
				rejectedOutputs++
				continue
			}
			intent.Symbol = security.Symbol
			intent.TSCode = security.TSCode
			intent.Market = security.Market
			intent.AssetType = security.AssetType
			intent.BoardType = security.BoardType
			if intentErr := validateIntent(cfg, response, intent); intentErr != nil {
				rejectedOutputs++
				continue
			}
			rawIntent, _ := json.Marshal(intent)
			evidence, _ := json.Marshal(intent.EvidenceRefs)
			executionStatus := "READY"
			if request.DryRun || cfg.Trading.Decision.Provider == "SHADOW" {
				executionStatus = "SHADOW_READY"
			}
			model := db_model.TradingIntent{
				TradingAgentRunID: run.ID, IntentKey: intent.IntentKey, RecommendationEventID: intent.RecommendationEventID,
				CandidatePlanID: intent.CandidatePlanID, PositionCycleID: intent.PositionCycleID, Symbol: security.Symbol, TSCode: security.TSCode, Market: security.Market,
				AssetType: security.AssetType, BoardType: security.BoardType, EastmoneySymbol: security.EastmoneySymbol,
				TradeAction: strings.ToUpper(intent.Action), Status: "READY_FOR_EXECUTION", ExecutionStatus: executionStatus, ProposedOrderType: "LIMIT",
				ProposedLimitPrice: stringPointer(intent.ProposedLimitPrice), ProposedPositionRatio: stringPointer(intent.ProposedPositionRatio),
				ProposedVolume: intent.ProposedVolume, Confidence: intent.Confidence, ValidFrom: intent.ValidFrom, ValidUntil: intent.ValidUntil,
				Reason: intent.Reason, EvidenceRefsJSON: evidence, RawIntentJSON: rawIntent, RejectionMessage: "",
				StrategyVersion: response.StrategyVersion, ConfigVersion: cfg.Meta.ConfigVersion,
			}
			if createErr := dal.TradingIntents.Create(ctx, tx, &model); createErr != nil {
				if strings.Contains(strings.ToLower(createErr.Error()), "duplicate") {
					continue
				}
				return createErr
			}
			intentIDs[intent.IntentKey] = model.ID
			createdIntents++
		}
		skillModels := make([]db_model.TradingSkillDecision, 0, len(response.SkillDecisions))
		for _, decision := range response.SkillDecisions {
			inputJSON, _ := json.Marshal(decision.Input)
			outputJSON, _ := json.Marshal(decision.Output)
			var intentID *int64
			if id, ok := intentIDs[decision.IntentKey]; ok {
				intentID = &id
			}
			skillModels = append(skillModels, db_model.TradingSkillDecision{
				DecisionKey: decision.DecisionKey, TradingAgentRunID: run.ID, TradingIntentID: intentID, PositionCycleID: decision.PositionCycleID,
				Stage: decision.Stage, SkillName: decision.SkillName, SkillVersion: decision.SkillVersion, Decision: decision.Decision,
				Score: decision.Score, Reason: decision.Reason, InputJSON: inputJSON, OutputJSON: outputJSON, EvaluatedAt: decision.EvaluatedAt,
			})
		}
		if err := dal.TradingSkillDecisions.CreateBatch(ctx, tx, skillModels); err != nil {
			return err
		}
		decisionCompleted := time.Now()
		return dal.TradingAgentRuns.Update(ctx, tx, run.ID, map[string]any{
			"raw_output_json": rawOutput, "candidate_count": response.CandidateCount,
			"intent_count": createdIntents, "rejected_output_count": rejectedOutputs,
			"decision_completed_at": decisionCompleted,
		})
	})
	if err != nil {
		finished := time.Now()
		_ = dal.TradingAgentRuns.Update(ctx, s.db, run.ID, map[string]any{"status": "FAILED", "finished_at": finished, "error_code": "PERSIST_INTENTS_FAILED", "error_message": err.Error()})
		return nil, fmt.Errorf("persist trading agent run %d intents: %w", run.ID, err)
	}

	finished := time.Now()
	status := "DECISION_SUCCEEDED"
	if rejectedOutputs > 0 {
		status = "DECISION_PARTIAL_SUCCEEDED"
	}
	if err := dal.TradingAgentRuns.Update(ctx, s.db, run.ID, map[string]any{"status": status, "finished_at": finished}); err != nil {
		return nil, err
	}
	return s.GetRun(ctx, run.ID)
}

func (s *Service) GetRun(ctx context.Context, id int64) (*RunView, error) {
	run, err := dal.TradingAgentRuns.QueryByID(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	intents, err := dal.TradingIntents.QueryByRunID(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	orders := make([]db_model.TradingOrder, 0)
	for _, intent := range intents {
		items, listErr := dal.TradingOrders.List(ctx, s.db, "", intent.Symbol, 100)
		if listErr != nil {
			return nil, listErr
		}
		for _, item := range items {
			if item.TradingIntentID == intent.ID {
				orders = append(orders, item)
			}
		}
	}
	return &RunView{Run: *run, Intents: intents, Orders: orders}, nil
}

func (s *Service) ListIntents(ctx context.Context, status, symbol string, limit int) ([]db_model.TradingIntent, error) {
	return dal.TradingIntents.List(ctx, s.db, status, symbol, boundedLimit(limit))
}

func (s *Service) ListOrders(ctx context.Context, status, symbol string, limit int) ([]db_model.TradingOrder, error) {
	return dal.TradingOrders.List(ctx, s.db, status, symbol, boundedLimit(limit))
}

func (s *Service) ListPositionCycles(ctx context.Context, status string, limit int) ([]db_model.TradingPositionCycle, error) {
	cfg := s.runtime.Config()
	if cfg == nil {
		return nil, fmt.Errorf("runtime config unavailable")
	}
	return dal.TradingPositionCycles.List(ctx, s.db, cfg.Trading.Bridge.ExpectedAccountID, status, boundedLimit(limit))
}

func (s *Service) ListDailySessions(ctx context.Context, limit int) ([]db_model.TradingDailySession, error) {
	cfg := s.runtime.Config()
	if cfg == nil {
		return nil, fmt.Errorf("runtime config unavailable")
	}
	return dal.TradingDailySessions.List(ctx, s.db, cfg.Trading.Bridge.ExpectedAccountID, boundedLimit(limit))
}

func (s *Service) ListSkillDecisions(ctx context.Context, runID int64) ([]db_model.TradingSkillDecision, error) {
	return dal.TradingSkillDecisions.ListByRunID(ctx, s.db, runID)
}

func (s *Service) CancelOrder(ctx context.Context, clientOrderID string) (*db_model.TradingOrder, error) {
	order, err := dal.TradingOrders.QueryByClientOrderID(ctx, s.db, clientOrderID)
	if err != nil {
		return nil, err
	}
	if tradingdomain.IsTerminalOrderStatus(order.Status) || order.Status == "DRY_RUN" {
		return order, nil
	}
	idempotencyKey := hashParts("cancel", clientOrderID)
	if _, err := s.bridge.CancelOrder(ctx, clientOrderID, idempotencyKey); err != nil {
		return nil, err
	}
	return order, nil
}

func (s *Service) SetKillSwitch(ctx context.Context, request KillSwitchRequest) error {
	cfg := s.runtime.Config()
	if cfg == nil {
		return fmt.Errorf("runtime config unavailable")
	}
	if strings.TrimSpace(request.Reason) == "" || strings.TrimSpace(request.ChangedBy) == "" {
		return fmt.Errorf("reason and changed_by are required")
	}
	if err := dal.TradingRuntimeControls.SetKillSwitch(ctx, s.db, request.Enabled, request.Reason, request.ChangedBy, cfg.Meta.ConfigVersion); err != nil {
		return err
	}
	if s.bridge != nil {
		if err := s.bridge.SetKillSwitch(ctx, request.Enabled, request.Reason, hashParts("kill-switch", strconv.FormatBool(request.Enabled), request.Reason)); err != nil {
			return fmt.Errorf("persisted FinanceSys kill switch but Bridge update failed: %w", err)
		}
	}
	if request.Enabled && request.CancelOpenOrders {
		orders, err := dal.TradingOrders.QueryOpen(ctx, s.db)
		if err != nil {
			return err
		}
		for _, order := range orders {
			_, _ = s.bridge.CancelOrder(ctx, order.ClientOrderID, hashParts("kill-switch-cancel", order.ClientOrderID))
		}
	}
	return nil
}

func (s *Service) LatestAccount(ctx context.Context) (*db_model.TradingAccountSnapshot, error) {
	cfg := s.runtime.Config()
	accountID := ""
	if cfg != nil {
		accountID = cfg.Trading.Bridge.ExpectedAccountID
	}
	return dal.TradingAccountSnapshots.Latest(ctx, s.db, accountID)
}

func (s *Service) LatestPositions(ctx context.Context) ([]db_model.TradingPositionSnapshot, error) {
	account, err := s.LatestAccount(ctx)
	if err != nil {
		return nil, err
	}
	return dal.TradingPositionSnapshots.ByAccountSnapshot(ctx, s.db, account.ID)
}

func (s *Service) evaluateIntent(ctx context.Context, intent *db_model.TradingIntent, dryRun bool) error {
	return s.evaluateIntentWithOptions(ctx, intent, dryRun, false)
}

func (s *Service) evaluateIntentWithOptions(ctx context.Context, intent *db_model.TradingIntent, dryRun, allowManualSession bool) error {
	cfg := s.runtime.Config()
	if cfg == nil {
		return fmt.Errorf("runtime config unavailable")
	}
	tradeIntent := dbIntentToDomain(intent)
	now := time.Now()
	health, account, positions := s.currentBridgeState(ctx, cfg)
	position := findPosition(positions, intent.EastmoneySymbol)
	latestPrice := ""
	quoteObservedAt := time.Time{}
	if quotes, quoteErr := s.refreshBridgeQuotes(ctx, []string{intent.EastmoneySymbol}, now); quoteErr == nil && len(quotes) > 0 {
		latestPrice = quotes[0].Price
		quoteObservedAt = quotes[0].ObservedAt
	}
	runtimeKill := true
	if control, controlErr := dal.TradingRuntimeControls.KillSwitch(ctx, s.db); controlErr == nil {
		runtimeKill = control.Enabled
	}
	startOfDay := startOfDayInLocation(now, cfg.Meta.Timezone)
	newOrders, _ := dal.TradingOrders.CountCreatedSince(ctx, s.db, startOfDay)
	turnoverAmount, _ := dal.TradingOrders.SumFilledAmountSince(ctx, s.db, startOfDay)
	cooldownSince := startOfDay.AddDate(0, 0, -max(cfg.Trading.Risk.IntentCooldownTradeDays*3, 15))
	duplicate := false
	if !strings.EqualFold(intent.TradeAction, "SELL") {
		duplicate, _ = dal.TradingOrders.ExistsRecentForIntent(ctx, s.db, intent.RecommendationEventID, intent.Symbol, cooldownSince)
	}
	noPriceLimitPeriod := false
	if strings.EqualFold(intent.TradeAction, "BUY") && cfg.Trading.Risk.ExcludeNoPriceLimit {
		if master, masterErr := dal.SecurityMasters.QueryByTSCode(ctx, s.db, intent.TSCode); masterErr == nil && master.ListDate != nil {
			if !master.ListDate.Before(now.AddDate(0, 0, -14)) {
				if dates, datesErr := dal.StockDailyQuotes.QueryTradingDates(ctx, s.db, "TUSHARE", *master.ListDate, now); datesErr == nil {
					noPriceLimitPeriod = len(dates) > 0 && len(dates) <= 5
				}
			}
		}
	}
	if strings.EqualFold(intent.TradeAction, "SELL") && intent.PositionCycleID != nil {
		cycle, cycleErr := dal.TradingPositionCycles.QueryByID(ctx, s.db, *intent.PositionCycleID)
		if cycleErr != nil || cycle.AccountID != cfg.Trading.Bridge.ExpectedAccountID || !strings.EqualFold(cycle.EastmoneySymbol, intent.EastmoneySymbol) || cycle.Status != "OPEN" {
			return fmt.Errorf("SELL intent position cycle is missing, closed, or mismatched")
		}
	}
	input := policy.Input{
		Trading: cfg.Trading, RuntimeKillSwitch: runtimeKill, BridgeHealth: health, Intent: tradeIntent,
		Account: account, Position: position, LatestPrice: latestPrice, QuoteObservedAt: quoteObservedAt,
		CurrentTotalPositionRatio: safeRatio(account.MarketValue, account.NAV),
		CurrentSymbolRatio:        positionRatio(position, account.NAV),
		DailyTurnoverRatio:        safeRatio(turnoverAmount, account.NAV),
		DailyLossRatio:            lossRatio(account.FloatingPnL, account.NAV),
		NewOrdersToday:            int(newOrders), PositionCount: len(positions), DuplicateInCooldown: duplicate,
		SessionOpen: sessionOpen(now, cfg.Trading.Scheduler) || (allowManualSession && sessionWindowOpen(now, cfg.Trading.Scheduler)), Now: now,
		NoPriceLimitPeriod: noPriceLimitPeriod,
	}
	result := s.policy.Evaluate(input)
	return s.persistDecision(ctx, cfg, intent, tradeIntent, result, dryRun)
}

func (s *Service) persistDecision(ctx context.Context, cfg *config.Config, intent *db_model.TradingIntent, tradeIntent tradingdomain.TradeIntent, result policy.Result, dryRun bool) error {
	checks := make([]db_model.TradingRiskCheck, 0, len(result.Checks))
	for _, item := range result.Checks {
		observed, _ := json.Marshal(item.Observed)
		limit, _ := json.Marshal(item.Limit)
		checks = append(checks, db_model.TradingRiskCheck{
			TradingIntentID: intent.ID, EvaluationNo: 1, CheckCode: item.Code, CheckOrder: int32(item.Order), Passed: item.Passed,
			Decision: item.Decision, ReasonMessage: item.Reason, ObservedJSON: observed, LimitJSON: limit,
			RiskVersion: cfg.Trading.Risk.Version, ConfigVersion: cfg.Meta.ConfigVersion,
		})
	}
	if !result.Approved {
		return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := dal.TradingRiskChecks.CreateBatch(ctx, tx, checks); err != nil {
				return err
			}
			return dal.TradingIntents.Update(ctx, tx, intent.ID, map[string]any{"status": "RISK_REJECTED", "rejection_code": result.RejectionCode, "rejection_message": result.RejectionMessage})
		})
	}

	clientOrderID, err := newUUIDv7(time.Now())
	if err != nil {
		return err
	}
	idempotencyKey := hashParts("place", clientOrderID, "1")
	request := tradingdomain.BridgeOrderRequest{
		ClientOrderID: clientOrderID, Environment: "SIMULATION", ExpectedAccountID: cfg.Trading.Bridge.ExpectedAccountID,
		StrategyID: cfg.Trading.Bridge.StrategyID, Symbol: intent.EastmoneySymbol, TSCode: intent.TSCode,
		AssetType: intent.AssetType, BoardType: intent.BoardType, TradingRuleVersion: cfg.Trading.Risk.TradingRuleVersion, Side: intent.TradeAction,
		OrderType: "LIMIT", PositionEffect: positionEffect(intent.TradeAction), Volume: result.FinalVolume,
		Price: result.FinalPrice, ValidUntil: intent.ValidUntil, SourceIntentID: intent.ID,
	}
	riskJSON, _ := json.Marshal(result)
	request.RiskSnapshotHash = utils.SHA256Hex(riskJSON)
	requestJSON, _ := json.Marshal(request)
	price := result.FinalPrice
	status := "DISPATCH_PENDING"
	if dryRun || cfg.Trading.Decision.Provider == "SHADOW" {
		status = "DRY_RUN"
	}
	order := db_model.TradingOrder{
		TradingIntentID: intent.ID, ClientOrderID: clientOrderID, IdempotencyKey: idempotencyKey, DispatchSeq: 1,
		Environment: "SIMULATION", Provider: "EASTMONEY_GM", AccountID: cfg.Trading.Bridge.ExpectedAccountID,
		StrategyID: cfg.Trading.Bridge.StrategyID, Symbol: intent.Symbol, EastmoneySymbol: intent.EastmoneySymbol,
		Side: intent.TradeAction, PositionEffect: request.PositionEffect, OrderType: "LIMIT", LimitPrice: &price,
		Volume: result.FinalVolume, Status: status, FilledAmount: "0", FilledCommission: "0", ValidUntil: intent.ValidUntil,
		RiskSnapshotHash: request.RiskSnapshotHash, RequestJSON: requestJSON, LatestProviderPayloadJSON: []byte(`{}`),
		RiskVersion: cfg.Trading.Risk.Version, ConfigVersion: cfg.Meta.ConfigVersion, ErrorMessage: "",
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := dal.TradingRiskChecks.CreateBatch(ctx, tx, checks); err != nil {
			return err
		}
		if err := dal.TradingOrders.Create(ctx, tx, &order); err != nil {
			return err
		}
		intentStatus := "ORDER_CREATED"
		if status == "DRY_RUN" {
			intentStatus = "DRY_RUN"
		}
		return dal.TradingIntents.Update(ctx, tx, intent.ID, map[string]any{"status": intentStatus, "rejection_code": "", "rejection_message": ""})
	}); err != nil {
		return err
	}
	if status == "DRY_RUN" {
		return nil
	}
	response, err := s.bridge.PlaceOrder(ctx, idempotencyKey, request)
	if err != nil {
		return dal.TradingOrders.Update(ctx, s.db, order.ID, map[string]any{
			"status":                 gorm.Expr("CASE WHEN status = 'DISPATCH_PENDING' THEN 'UNKNOWN' ELSE status END"),
			"dispatch_attempt_count": 1, "error_code": "BRIDGE_DISPATCH_FAILED", "error_message": err.Error(),
		})
	}
	payload, _ := json.Marshal(response)
	now := time.Now()
	return dal.TradingOrders.Update(ctx, s.db, order.ID, map[string]any{
		"status":                 gorm.Expr("CASE WHEN status = 'DISPATCH_PENDING' THEN 'BRIDGE_QUEUED' ELSE status END"),
		"dispatch_attempt_count": 1, "submitted_at": now, "latest_provider_payload_json": payload,
	})
}

func (s *Service) currentBridgeState(ctx context.Context, cfg *config.Config) (tradingdomain.BridgeHealth, tradingdomain.AccountSnapshot, []tradingdomain.PositionSnapshot) {
	health := tradingdomain.BridgeHealth{Status: "UNAVAILABLE", KillSwitch: true}
	account := tradingdomain.AccountSnapshot{Environment: "SIMULATION", AccountID: cfg.Trading.Bridge.ExpectedAccountID}
	if s.bridge == nil {
		return health, account, nil
	}
	if value, err := s.bridge.Health(ctx); err == nil && value != nil {
		health = *value
	}
	requestedAt := time.Now()
	if _, err := s.bridge.RefreshSnapshot(ctx, hashParts("risk-snapshot", requestedAt.Format(time.RFC3339Nano))); err != nil {
		return health, account, nil
	}
	wait, maxAge := bridgeSnapshotWaitSettings(cfg)
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		value, err := s.bridge.Account(ctx)
		if err == nil && value != nil {
			account = *value
			if !account.SnapshotAt.Before(requestedAt.Add(-maxAge)) {
				break
			}
		}
		select {
		case <-ctx.Done():
			return health, account, nil
		case <-deadline.C:
			return health, account, nil
		case <-ticker.C:
		}
	}
	positions, err := s.bridge.Positions(ctx)
	if err != nil {
		positions = nil
	}
	_ = s.persistSnapshot(ctx, account, positions)
	return health, account, positions
}

func bridgeSnapshotWaitSettings(cfg *config.Config) (time.Duration, time.Duration) {
	wait := 5 * time.Second
	maxAge := 15 * time.Second
	if cfg != nil {
		if cfg.Trading.Bridge.RequestTimeoutMS > 0 {
			wait = time.Duration(cfg.Trading.Bridge.RequestTimeoutMS) * time.Millisecond
		}
		if cfg.Trading.Risk.MaxSnapshotAgeSeconds > 0 {
			maxAge = time.Duration(cfg.Trading.Risk.MaxSnapshotAgeSeconds) * time.Second
		}
	}
	if wait < 500*time.Millisecond {
		wait = 500 * time.Millisecond
	}
	if wait > 10*time.Second {
		wait = 10 * time.Second
	}
	return wait, maxAge
}

func (s *Service) persistSnapshot(ctx context.Context, account tradingdomain.AccountSnapshot, positions []tradingdomain.PositionSnapshot) error {
	if account.AccountID == "" || account.SnapshotAt.IsZero() {
		return nil
	}
	raw, _ := json.Marshal(account)
	snapshotKey := hashParts(account.AccountID, account.SnapshotVersion, account.SnapshotAt.Format(time.RFC3339Nano))
	model := db_model.TradingAccountSnapshot{
		SnapshotKey: snapshotKey, SnapshotVersion: account.SnapshotVersion, Environment: account.Environment,
		Provider: "EASTMONEY_GM", AccountID: account.AccountID, AccountName: account.AccountName,
		Nav: zeroDefault(account.NAV), Balance: zeroDefault(account.Balance), AvailableCash: zeroDefault(account.AvailableCash),
		FrozenCash: zeroDefault(account.FrozenCash), MarketValue: zeroDefault(account.MarketValue), FloatingPnl: zeroDefault(account.FloatingPnL),
		CumulativeInout: zeroDefault(account.CumulativeInOut), CumulativeTrade: zeroDefault(account.CumulativeTrade),
		CumulativePnl: zeroDefault(account.CumulativePnL), CumulativeCommission: zeroDefault(account.CumulativeCommission),
		LastTrade: zeroDefault(account.LastTrade), LastPnl: zeroDefault(account.LastPnL), LastCommission: zeroDefault(account.LastCommission),
		CommissionDataStatus: account.CommissionDataStatus, TerminalState: account.TerminalState, AccountState: account.AccountState,
		SnapshotAt: account.SnapshotAt, RawPayloadJSON: raw,
	}
	if model.CommissionDataStatus == "" {
		model.CommissionDataStatus = "UNAVAILABLE"
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := dal.TradingAccountSnapshots.Create(ctx, tx, &model); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
				return nil
			}
			return err
		}
		positionModels := make([]db_model.TradingPositionSnapshot, 0, len(positions))
		for _, item := range positions {
			rawPosition, _ := json.Marshal(item)
			positionModels = append(positionModels, db_model.TradingPositionSnapshot{
				AccountSnapshotID: model.ID, AccountID: item.AccountID, Symbol: item.Symbol, EastmoneySymbol: item.EastmoneySymbol,
				PositionSide: item.PositionSide, Volume: item.Volume, AvailableVolume: item.AvailableVolume, TodayVolume: item.TodayVolume,
				Vwap: zeroDefault(item.VWAP), LastPrice: zeroDefault(item.LastPrice), MarketValue: zeroDefault(item.MarketValue),
				FloatingPnl: zeroDefault(item.FloatingPnL), RawPayloadJSON: rawPosition,
			})
		}
		return dal.TradingPositionSnapshots.CreateBatch(ctx, tx, positionModels)
	})
}

func validateAgentResponse(cfg *config.Config, request tradingdomain.AgentRunRequest, response *tradingdomain.AgentRunResponse) error {
	if response == nil || response.SchemaVersion != cfg.Trading.Agent.SchemaVersion || response.RunKey != request.RunKey {
		return fmt.Errorf("schema_version or run_key mismatch")
	}
	if response.StrategyVersion != cfg.Trading.Decision.StrategyVersion || response.ToolContractVersion != cfg.Trading.Decision.ToolContractVersion {
		return fmt.Errorf("strategy or tool contract version mismatch")
	}
	if len(response.Intents) > cfg.Trading.Decision.MaxIntentsPerRun {
		return fmt.Errorf("agent returned too many intents")
	}
	for _, decision := range response.SkillDecisions {
		if len(decision.DecisionKey) != 64 || decision.SkillName == "" || decision.SkillVersion == "" || decision.Stage == "" || decision.Decision == "" || decision.EvaluatedAt.IsZero() {
			return fmt.Errorf("agent skill decision is invalid")
		}
		if _, ok := new(big.Rat).SetString(decision.Score); !ok {
			return fmt.Errorf("agent skill decision score is invalid")
		}
	}
	return nil
}

func validateIntent(cfg *config.Config, response *tradingdomain.AgentRunResponse, intent tradingdomain.TradeIntent) error {
	if intent.IntentKey == "" || len(intent.IntentKey) != 64 {
		return fmt.Errorf("intent identity is invalid")
	}
	if strings.EqualFold(intent.Action, "BUY") && intent.RecommendationEventID == nil {
		return fmt.Errorf("BUY intent requires recommendation_event_id")
	}
	if strings.EqualFold(intent.Action, "SELL") && intent.PositionCycleID == nil {
		return fmt.Errorf("SELL intent requires position_cycle_id")
	}
	expected := intentKey(response.StrategyVersion, response.AsOfTime, intent)
	if !strings.EqualFold(expected, intent.IntentKey) {
		return fmt.Errorf("intent key mismatch")
	}
	if !containsFold(cfg.Trading.Risk.AllowedAssetTypes, intent.AssetType) || strings.EqualFold(intent.AssetType, "SECTOR") {
		return fmt.Errorf("asset type is not tradable")
	}
	if !containsFold(cfg.Trading.Risk.AllowedMarkets, intent.Market) || !containsFold(cfg.Trading.Risk.AllowedSides, intent.Action) {
		return fmt.Errorf("market or action is not allowed")
	}
	security, err := instrument.Parse(intent.Symbol, intent.Market, intent.AssetType)
	if err != nil || intent.TSCode != security.TSCode || intent.BoardType != security.BoardType {
		return fmt.Errorf("security identity is invalid")
	}
	if intent.ProposedOrderType != "LIMIT" || intent.ValidUntil.Before(intent.ValidFrom) || intent.ValidUntil.Before(response.AsOfTime) {
		return fmt.Errorf("order type or validity window is invalid")
	}
	if _, ok := new(big.Rat).SetString(intent.ProposedLimitPrice); !ok {
		return fmt.Errorf("limit price is invalid")
	}
	if _, ok := new(big.Rat).SetString(intent.ProposedPositionRatio); !ok {
		return fmt.Errorf("position ratio is invalid")
	}
	return nil
}

func intentKey(strategyVersion string, asOf time.Time, intent tradingdomain.TradeIntent) string {
	identity := "event:0"
	if intent.RecommendationEventID != nil {
		identity = "event:" + strconv.FormatInt(*intent.RecommendationEventID, 10)
	} else if intent.PositionCycleID != nil {
		identity = "cycle:" + strconv.FormatInt(*intent.PositionCycleID, 10)
	}
	return hashParts(strategyVersion, asOf.In(time.FixedZone("CST", 8*3600)).Format(time.DateOnly), identity, strings.ToUpper(intent.Symbol), strings.ToUpper(intent.Action), intent.ValidUntil.Format(time.RFC3339Nano))
}

func hashParts(parts ...string) string {
	hash := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(hash[:])
}

func newUUIDv7(now time.Time) (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	millis := uint64(now.UnixMilli())
	raw[0] = byte(millis >> 40)
	raw[1] = byte(millis >> 32)
	raw[2] = byte(millis >> 24)
	raw[3] = byte(millis >> 16)
	raw[4] = byte(millis >> 8)
	raw[5] = byte(millis)
	raw[6] = (raw[6] & 0x0f) | 0x70
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s", hex.EncodeToString(raw[0:4]), hex.EncodeToString(raw[4:6]), hex.EncodeToString(raw[6:8]), hex.EncodeToString(raw[8:10]), hex.EncodeToString(raw[10:16])), nil
}

func (s *Service) toolBaseURL(cfg *config.Config) string {
	return fmt.Sprintf("http://127.0.0.1:%d%s/internal/trading-tools", cfg.Service.HTTP.Port, strings.TrimRight(cfg.Service.HTTP.APIPrefix, "/"))
}

func dbIntentToDomain(intent *db_model.TradingIntent) tradingdomain.TradeIntent {
	value := tradingdomain.TradeIntent{
		IntentKey: intent.IntentKey, RecommendationEventID: intent.RecommendationEventID, CandidatePlanID: intent.CandidatePlanID, PositionCycleID: intent.PositionCycleID,
		Symbol: intent.Symbol, TSCode: intent.TSCode, Market: intent.Market, AssetType: intent.AssetType, BoardType: intent.BoardType, Action: intent.TradeAction,
		ProposedOrderType: intent.ProposedOrderType, ValidFrom: intent.ValidFrom, ValidUntil: intent.ValidUntil,
		Confidence: intent.Confidence, Reason: intent.Reason, ProposedVolume: intent.ProposedVolume,
	}
	if intent.ProposedLimitPrice != nil {
		value.ProposedLimitPrice = *intent.ProposedLimitPrice
	}
	if intent.ProposedPositionRatio != nil {
		value.ProposedPositionRatio = *intent.ProposedPositionRatio
	}
	_ = json.Unmarshal(intent.EvidenceRefsJSON, &value.EvidenceRefs)
	return value
}

func positionEffect(action string) string {
	if strings.EqualFold(action, "SELL") {
		return "CLOSE"
	}
	return "OPEN"
}

func findPosition(positions []tradingdomain.PositionSnapshot, eastmoneySymbol string) *tradingdomain.PositionSnapshot {
	for i := range positions {
		if strings.EqualFold(positions[i].EastmoneySymbol, eastmoneySymbol) {
			return &positions[i]
		}
	}
	return nil
}

func positionRatio(position *tradingdomain.PositionSnapshot, nav string) string {
	if position == nil {
		return "0"
	}
	return safeRatio(position.MarketValue, nav)
}

func safeRatio(numerator, denominator string) string {
	n, ok := new(big.Rat).SetString(zeroDefault(numerator))
	if !ok {
		return "0"
	}
	d, ok := new(big.Rat).SetString(zeroDefault(denominator))
	if !ok || d.Sign() <= 0 {
		return "0"
	}
	return new(big.Rat).Quo(n, d).FloatString(8)
}

func lossRatio(pnl, nav string) string {
	p, ok := new(big.Rat).SetString(zeroDefault(pnl))
	if !ok || p.Sign() >= 0 {
		return "0"
	}
	p.Neg(p)
	return safeRatio(p.FloatString(6), nav)
}

func sessionOpen(now time.Time, cfg config.TradingSchedulerConfig) bool {
	return cfg.Enabled && sessionWindowOpen(now, cfg)
}

func sessionWindowOpen(now time.Time, cfg config.TradingSchedulerConfig) bool {
	if now.Weekday() == time.Saturday || now.Weekday() == time.Sunday {
		return false
	}
	clock := now.Format("15:04:05")
	return (clock >= cfg.MorningWindow.Start && clock <= cfg.MorningWindow.End) || (clock >= cfg.AfternoonWindow.Start && clock <= cfg.AfternoonWindow.End)
}

func startOfDayInLocation(now time.Time, timezone string) time.Time {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		location = now.Location()
	}
	local := now.In(location)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
}

func zeroDefault(value string) string {
	if strings.TrimSpace(value) == "" {
		return "0"
	}
	return value
}

func stringPointer(value string) *string { return &value }

func boundedLimit(value int) int {
	if value <= 0 {
		return 100
	}
	if value > 500 {
		return 500
	}
	return value
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}
