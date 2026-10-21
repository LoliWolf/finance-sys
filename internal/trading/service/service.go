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
	"sync"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/dal"
	"finance-sys/internal/domain/db_model"
	"finance-sys/internal/rules"
	tradingdomain "finance-sys/internal/trading/domain"
	"finance-sys/internal/trading/instrument"
	"finance-sys/internal/trading/policy"
	"finance-sys/internal/tradingbridgeclient"
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
	now     func() time.Time
	// Serialize local reconciliation with risk evaluation through Bridge dispatch.
	accountActivity sync.Mutex
}

func New(db *gorm.DB, runtime *config.Runtime, agent AgentClient, bridge BridgeClient, policyEngine *policy.Engine, logger *slog.Logger) *Service {
	return &Service{db: db, runtime: runtime, agent: agent, bridge: bridge, policy: policyEngine, logger: logger, now: time.Now}
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
		request.AsOfTime = s.now()
	}
	request.AsOfTime = request.AsOfTime.UTC().Truncate(time.Minute)
	request.DryRun = request.DryRun || cfg.Trading.KillSwitch || !cfg.Trading.Enabled || cfg.Trading.Decision.Provider == "SHADOW"
	if request.TriggerType == "PRE_OPEN" {
		open, err := cfg.Trading.Calendar.IsOpen(request.AsOfTime)
		if err != nil {
			return nil, err
		}
		if !open {
			return nil, fmt.Errorf("not a trading day")
		}
	}
	runKey := hashParts(cfg.Trading.Decision.StrategyVersion, request.TriggerType, request.AsOfTime.Format(time.RFC3339Nano), strconv.FormatBool(request.DryRun), s.toolBaseURL(cfg))
	if existing, err := dal.TradingAgentRuns.QueryByRunKey(ctx, s.db, runKey); err == nil {
		return s.GetRun(ctx, existing.ID)
	} else if !errors.Is(err, dal.ErrNotFound) {
		return nil, err
	}

	agentRequest := tradingdomain.AgentRunRequest{
		RunKey: runKey, AsOfTime: request.AsOfTime, TriggerType: request.TriggerType,
		StrategyName: cfg.Trading.Decision.StrategyName, StrategyVersion: cfg.Trading.Decision.StrategyVersion,
		DecisionProvider: cfg.Trading.Decision.Provider, ToolBaseURL: s.toolBaseURL(cfg),
		ConfigVersion: cfg.Meta.ConfigVersion, DryRun: request.DryRun, SelectionOnly: true,
	}
	requestJSON, _ := json.Marshal(agentRequest)
	now := s.now()
	run := db_model.TradingAgentRun{
		RunKey: runKey, TriggerType: request.TriggerType, Status: "QUEUED", DecisionProvider: cfg.Trading.Decision.Provider,
		StrategyName: cfg.Trading.Decision.StrategyName, StrategyVersion: cfg.Trading.Decision.StrategyVersion,
		SchemaVersion: cfg.Trading.Agent.SchemaVersion, PromptVersion: "", ToolContractVersion: cfg.Trading.Decision.ToolContractVersion,
		AsOfTime: request.AsOfTime, InputCursor: "", InputSnapshotHash: utils.SHA256Hex(requestJSON),
		RequestJSON: requestJSON, RawOutputJSON: []byte(`{}`), DryRun: request.DryRun,
		QueuedAt: now, ErrorMessage: "", ConfigVersion: cfg.Meta.ConfigVersion,
	}
	if err := dal.TradingAgentRuns.Create(ctx, s.db, &run); err != nil {
		if existing, e := dal.TradingAgentRuns.QueryByRunKey(ctx, s.db, runKey); e == nil {
			return s.GetRun(ctx, existing.ID)
		}
		return nil, err
	}
	return s.GetRun(ctx, run.ID)
}

func (s *Service) decideRun(ctx context.Context, run *db_model.TradingAgentRun) (*RunView, error) {
	cfg := s.runtime.Config()
	if cfg == nil {
		return nil, fmt.Errorf("config unavailable")
	}
	var agentRequest tradingdomain.AgentRunRequest
	if err := json.Unmarshal(run.RequestJSON, &agentRequest); err != nil {
		return nil, err
	}
	if run.ConfigVersion != cfg.Meta.ConfigVersion {
		return nil, fmt.Errorf("CONFIG_CHANGED: re-enqueue decision")
	}
	request := agentRequest
	response, err := s.agent.Run(ctx, agentRequest)
	if err != nil {
		finished := time.Now()
		_, _ = dal.TradingAgentRuns.FinishClaim(ctx, s.db, run.ID, run.ClaimToken, s.now(), map[string]any{"status": "FAILED", "finished_at": finished, "error_code": "AGENT_CALL_FAILED", "error_message": err.Error()})
		return nil, fmt.Errorf("trading agent run %d failed: %w", run.ID, err)
	}
	rawOutput, _ := json.Marshal(response)
	if err := validateAgentResponse(cfg, agentRequest, response); err != nil {
		finished := time.Now()
		_, _ = dal.TradingAgentRuns.FinishClaim(ctx, s.db, run.ID, run.ClaimToken, s.now(), map[string]any{"status": "FAILED", "finished_at": finished, "raw_output_json": rawOutput, "error_code": "AGENT_SCHEMA_INVALID", "error_message": err.Error()})
		return nil, fmt.Errorf("trading agent run %d returned invalid output: %w", run.ID, err)
	}

	rejectedOutputs := 0
	createdIntents := 0
	intentIDs := make(map[string]int64, len(response.Intents))
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := dal.TradingRuntimeControls.Lock(ctx, tx); err != nil {
			return err
		}
		stored, err := dal.TradingAgentRuns.QueryByID(ctx, tx, run.ID)
		if err != nil {
			return err
		}
		if stored.ClaimToken != run.ClaimToken || stored.ClaimDeadline == nil || !stored.ClaimDeadline.After(s.now()) {
			return fmt.Errorf("decision claim lost")
		}
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
		decisionCompleted := s.now()
		status := "DECISION_SUCCEEDED"
		if rejectedOutputs > 0 {
			status = "DECISION_PARTIAL_SUCCEEDED"
		}
		ok, err := dal.TradingAgentRuns.FinishClaim(ctx, tx, run.ID, run.ClaimToken, decisionCompleted, map[string]any{
			"raw_output_json": rawOutput, "candidate_count": response.CandidateCount,
			"intent_count": createdIntents, "rejected_output_count": rejectedOutputs,
			"decision_completed_at": decisionCompleted, "status": status, "finished_at": decisionCompleted,
			"input_snapshot_hash": utils.SHA256Hex(marshalJSON(response.InputSnapshot)),
		})
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("decision claim lost")
		}
		return nil
	})
	if err != nil {
		finished := time.Now()
		_, _ = dal.TradingAgentRuns.FinishClaim(ctx, s.db, run.ID, run.ClaimToken, s.now(), map[string]any{"status": "FAILED", "finished_at": finished, "error_code": "PERSIST_INTENTS_FAILED", "error_message": err.Error()})
		return nil, fmt.Errorf("persist trading agent run %d intents: %w", run.ID, err)
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
	if !s.accountActivity.TryLock() {
		return fmt.Errorf("%w: account operation in progress", ErrExecutionDeferred)
	}
	defer s.accountActivity.Unlock()
	cfg := s.runtime.Config()
	if cfg == nil {
		return fmt.Errorf("runtime config unavailable")
	}
	tradeIntent := dbIntentToDomain(intent)
	if err := s.requireNoRunningReconciliation(ctx, s.db, cfg.Trading.Bridge.ExpectedAccountID); err != nil {
		return err
	}
	snapshot, err := s.freshBridgeSnapshot(ctx, "risk")
	if err != nil {
		return fmt.Errorf("%w: %v", ErrExecutionDeferred, err)
	}
	health, account, positions := snapshot.Health, snapshot.Account, snapshot.Positions
	ledgerProblems, ledgerErr := s.ledgerDiffs(ctx, account, positions)
	if ledgerErr != nil {
		return fmt.Errorf("%w: ledger unavailable: %v", ErrExecutionDeferred, ledgerErr)
	}
	if len(ledgerProblems) > 0 {
		return fmt.Errorf("%w: ledger not reconciled (%s)", ErrExecutionDeferred, ledgerProblems[0].DiffType)
	}
	position := findPosition(positions, intent.EastmoneySymbol)
	quotes, err := s.refreshBridgeQuotes(ctx, []string{intent.EastmoneySymbol}, s.now())
	if err != nil || len(quotes) != 1 {
		return fmt.Errorf("%w: fresh quote unavailable", ErrExecutionDeferred)
	}
	quote := quotes[0]
	now := s.now()
	if quote.AssetType != intent.AssetType || quote.BoardType != intent.BoardType {
		return fmt.Errorf("provider identity differs from intent")
	}
	price, err := rules.TradingPrice(intent.TradeAction, quote, now, time.Duration(cfg.Trading.Risk.MaxSnapshotAgeSeconds)*time.Second)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrExecutionDeferred, err)
	}
	tradeIntent.ProposedLimitPrice = price
	if tradeIntent.Action == "BUY" && (position != nil && position.Volume > 0) {
		return fmt.Errorf("%w: pyramiding is disabled", ErrExecutionDeferred)
	}
	runtimeKill := true
	if control, controlErr := dal.TradingRuntimeControls.KillSwitch(ctx, s.db); controlErr == nil {
		runtimeKill = control.Enabled
	}
	startOfDay := startOfDayInLocation(now, cfg.Meta.Timezone)
	newOrders, err := dal.TradingOrders.CountCreatedSince(ctx, s.db, startOfDay)
	if err != nil {
		return err
	}
	turnoverAmount, err := dal.TradingOrders.SumFilledAmountSince(ctx, s.db, startOfDay)
	if err != nil {
		return err
	}
	duplicate := false
	if !strings.EqualFold(intent.TradeAction, "SELL") {
		cooldownSince, dateErr := cfg.Trading.Calendar.Previous(now, cfg.Trading.Risk.IntentCooldownTradeDays)
		if dateErr != nil {
			return dateErr
		}
		duplicate, err = dal.TradingOrders.ExistsRecentForIntent(ctx, s.db, intent.RecommendationEventID, intent.Symbol, cooldownSince)
		if err != nil {
			return err
		}
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
		if cycleErr != nil || cycle.AccountID != cfg.Trading.Bridge.ExpectedAccountID || !strings.EqualFold(cycle.EastmoneySymbol, intent.EastmoneySymbol) || cycle.Status == "CLOSED" {
			return fmt.Errorf("SELL intent position cycle is missing, closed, or mismatched")
		}
	}
	uat := uatKeyFromIntent(intent)
	uatOK := uatAllowed(cfg.Trading, uat, intent.EastmoneySymbol, now)
	if allowManualSession && !uatOK {
		return fmt.Errorf("scoped UAT authorization is missing")
	}
	if !uatOK {
		capability, e := dal.TradingBoardCapabilities.ByBoard(ctx, s.db, cfg.Trading.Bridge.ExpectedAccountID, intent.BoardType)
		if e != nil || capability.VerificationStatus != "VERIFIED" || capability.TradingRuleVersion != cfg.Trading.Risk.TradingRuleVersion || (intent.TradeAction == "BUY" && !capability.BuyEnabled) || (intent.TradeAction == "SELL" && !capability.SellEnabled) {
			return fmt.Errorf("BOARD_CAPABILITY_UNVERIFIED: settled UAT evidence required")
		}
	}
	input := policy.Input{
		Trading: cfg.Trading, RuntimeKillSwitch: runtimeKill, BridgeHealth: health, Intent: tradeIntent,
		Account: account, Position: position, LatestPrice: quote.Price, QuoteObservedAt: quote.ObservedAt,
		CurrentTotalPositionRatio: safeRatio(account.MarketValue, account.NAV),
		CurrentSymbolRatio:        positionRatio(position, account.NAV),
		DailyTurnoverRatio:        safeRatio(turnoverAmount, account.NAV),
		DailyLossRatio:            lossRatio(account.FloatingPnL, account.NAV),
		NewOrdersToday:            int(newOrders), PositionCount: len(positions), DuplicateInCooldown: duplicate,
		SessionOpen: (cfg.Trading.Scheduler.Enabled || allowManualSession) && tradingSessionOpen(now, cfg.Trading), Now: now,
		NoPriceLimitPeriod: noPriceLimitPeriod,
		UATAuthorized:      uatOK,
	}
	// Freeze the provider's settlement unit with the order, not in a guessed code map.
	tradeIntent.EvidenceRefs = append(tradeIntent.EvidenceRefs, fmt.Sprintf("trade_n:%d", quote.TradeN))
	result := s.policy.Evaluate(input)
	return s.persistDecision(ctx, cfg, intent, tradeIntent, result, dryRun)
}

func (s *Service) persistDecision(ctx context.Context, cfg *config.Config, intent *db_model.TradingIntent, tradeIntent tradingdomain.TradeIntent, result policy.Result, dryRun bool) error {
	checks := make([]db_model.TradingRiskCheck, 0, len(result.Checks))
	for _, item := range result.Checks {
		observed, _ := json.Marshal(item.Observed)
		limit, _ := json.Marshal(item.Limit)
		checks = append(checks, db_model.TradingRiskCheck{
			TradingIntentID: intent.ID, EvaluationNo: intent.ExecutionAttemptCount + 1, CheckCode: item.Code, CheckOrder: int32(item.Order), Passed: item.Passed,
			Decision: item.Decision, ReasonMessage: item.Reason, ObservedJSON: observed, LimitJSON: limit,
			RiskVersion: cfg.Trading.Risk.Version, ConfigVersion: cfg.Meta.ConfigVersion,
		})
	}
	if !result.Approved {
		return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := s.fenceIntent(ctx, tx, intent); err != nil {
				return err
			}
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
		UATKey: uatKeyFromIntent(intent), TradeN: 1,
		ExitPolicy: tradingdomain.ExitPolicy{StopLossRatio: cfg.Trading.Exit.StopLossRatio, TakeProfitRatio: cfg.Trading.Exit.TakeProfitRatio, MaxHoldingTradeDays: cfg.Trading.Exit.MaxHoldingTradeDays},
	}
	for _, ref := range tradeIntent.EvidenceRefs {
		if ref == "trade_n:0" && intent.AssetType == "ETF" {
			request.TradeN = 0
		}
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
		if err := s.fenceIntent(ctx, tx, intent); err != nil {
			return err
		}
		latest := s.runtime.Config()
		control, e := dal.TradingRuntimeControls.KillSwitch(ctx, tx)
		if e != nil {
			return e
		}
		if status != "DRY_RUN" && (latest == nil || latest.Meta.ConfigVersion != cfg.Meta.ConfigVersion || !latest.Trading.Enabled || latest.Trading.KillSwitch || control.Enabled) {
			return fmt.Errorf("%w: trading configuration or kill switch changed", ErrExecutionDeferred)
		}
		if existing, e := dal.TradingOrders.ByIntent(ctx, tx, intent.ID); e == nil && existing != nil {
			return fmt.Errorf("%w: order already exists for intent", ErrExecutionDeferred)
		} else if !errors.Is(e, dal.ErrNotFound) {
			return e
		}
		if status != "DRY_RUN" {
			if err := s.requireNoRunningReconciliation(ctx, tx, cfg.Trading.Bridge.ExpectedAccountID); err != nil {
				return err
			}
			open, e := dal.TradingOrders.QueryOpen(ctx, tx)
			if e != nil {
				return e
			}
			if len(open) > 0 {
				return fmt.Errorf("%w: account already has an in-flight order", ErrExecutionDeferred)
			}
		}
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
	// Persist uncertainty before crossing the external side-effect boundary.
	// Recovery must reconcile this same client ID; it may not create a new order.
	if err := dal.TradingOrders.Update(ctx, s.db, order.ID, map[string]any{"status": "UNKNOWN", "dispatch_attempt_count": 1}); err != nil {
		return err
	}
	response, err := s.bridge.PlaceOrder(ctx, idempotencyKey, request)
	if err != nil {
		if tradingbridgeclient.IsOrderRejected(err) {
			return dal.TradingOrders.Update(ctx, s.db, order.ID, map[string]any{"status": "REJECTED", "finished_at": s.now(), "dispatch_attempt_count": 1, "error_code": "BRIDGE_REJECTED_BEFORE_ENQUEUE", "error_message": err.Error()})
		}
		return dal.TradingOrders.Update(ctx, s.db, order.ID, map[string]any{
			"status":                 gorm.Expr("CASE WHEN status = 'DISPATCH_PENDING' THEN 'UNKNOWN' ELSE status END"),
			"dispatch_attempt_count": 1, "error_code": "BRIDGE_DISPATCH_FAILED", "error_message": err.Error(),
		})
	}
	payload, _ := json.Marshal(response)
	now := time.Now()
	return dal.TradingOrders.Update(ctx, s.db, order.ID, map[string]any{
		"status":                 gorm.Expr("CASE WHEN status IN ('DISPATCH_PENDING','UNKNOWN') THEN 'BRIDGE_QUEUED' ELSE status END"),
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
	if !response.AsOfTime.Equal(request.AsOfTime) {
		return fmt.Errorf("as_of_time mismatch")
	}
	if response.InputSnapshot == nil {
		return fmt.Errorf("auditable input snapshot is required")
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
	now = now.In(config.ChinaLocation)
	if now.Weekday() == time.Saturday || now.Weekday() == time.Sunday {
		return false
	}
	clock := now.Format("15:04:05")
	return (clock >= cfg.MorningWindow.Start && clock < cfg.MorningWindow.End) || (clock >= cfg.AfternoonWindow.Start && clock < cfg.AfternoonWindow.End)
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
