package service

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/dal"
	"finance-sys/internal/domain/db_model"
	tradingdomain "finance-sys/internal/trading/domain"
	"finance-sys/internal/trading/instrument"
	"gorm.io/gorm"
)

type fixtureBridge struct {
	now         func() time.Time
	version     int
	positions   []tradingdomain.PositionSnapshot
	orders      []map[string]any
	executions  []map[string]any
	placed      []tradingdomain.BridgeOrderRequest
	cash        string
	fees        string
	marketValue string
	nav         string
	turnover    string
	kill        bool
}

func (b *fixtureBridge) Health(context.Context) (*tradingdomain.BridgeHealth, error) {
	return &tradingdomain.BridgeHealth{Status: "READY", Runner: "READY", Terminal: "CONNECTED", Account: "READY", AccountID: "fixture", AuthState: "AUTH_OK", KillSwitch: b.kill}, nil
}
func (b *fixtureBridge) Account(context.Context) (*tradingdomain.AccountSnapshot, error) {
	cash := b.cash
	if cash == "" {
		cash = "100000"
	}
	fees := b.fees
	if fees == "" {
		fees = "0"
	}
	marketValue := b.marketValue
	if marketValue == "" {
		marketValue = "0"
	}
	nav := b.nav
	if nav == "" {
		nav = cash
	}
	turnover := b.turnover
	if turnover == "" {
		turnover = "0"
	}
	return &tradingdomain.AccountSnapshot{SnapshotVersion: fmt.Sprint(b.version), AccountID: "fixture", Environment: "SIMULATION", NAV: nav, AvailableCash: cash, FrozenCash: "0", MarketValue: marketValue, FloatingPnL: "0", CumulativeCommission: fees, SnapshotAt: b.now(), CommissionDataStatus: "REPORTED", CumulativeTrade: turnover, LastTrade: turnover, LastCommission: fees}, nil
}
func (b *fixtureBridge) Positions(context.Context) ([]tradingdomain.PositionSnapshot, error) {
	return b.positions, nil
}
func (b *fixtureBridge) ReconciliationSnapshot(ctx context.Context, _ string) (*tradingdomain.ReconciliationSnapshot, error) {
	a, _ := b.Account(ctx)
	h, _ := b.Health(ctx)
	return &tradingdomain.ReconciliationSnapshot{SnapshotVersion: fmt.Sprint(b.version), Account: *a, Health: *h, Positions: b.positions, Orders: b.orders, Executions: b.executions}, nil
}
func (b *fixtureBridge) RefreshSnapshot(context.Context, string) (*tradingdomain.BridgeCommandResponse, error) {
	b.version++
	return &tradingdomain.BridgeCommandResponse{Status: "QUEUED"}, nil
}
func (b *fixtureBridge) RefreshQuotes(context.Context, []string, string) (*tradingdomain.BridgeCommandResponse, error) {
	return &tradingdomain.BridgeCommandResponse{Status: "QUEUED"}, nil
}
func (b *fixtureBridge) Quotes(_ context.Context, symbols []string) ([]tradingdomain.QuoteSnapshot, error) {
	items := []tradingdomain.QuoteSnapshot{}
	for _, sym := range symbols {
		id, e := instrument.Parse(sym, "", "")
		if e != nil {
			return nil, e
		}
		items = append(items, tradingdomain.QuoteSnapshot{Symbol: id.Symbol, TSCode: id.TSCode, EastmoneySymbol: sym, AssetType: id.AssetType, BoardType: id.BoardType, Price: "10", Ask1: "10", Bid1: "10", UpperLimit: "11", LowerLimit: "9", PriceTick: "0.01", ObservedAt: b.now(), MetadataDate: config.ChinaDate(b.now()), SecurityStatus: "TRADING", TradeN: 1})
	}
	return items, nil
}
func (b *fixtureBridge) PlaceOrder(_ context.Context, _ string, r tradingdomain.BridgeOrderRequest) (*tradingdomain.BridgeCommandResponse, error) {
	b.placed = append(b.placed, r)
	return &tradingdomain.BridgeCommandResponse{Status: "QUEUED", ClientOrderID: r.ClientOrderID}, nil
}
func (b *fixtureBridge) CancelOrder(context.Context, string, string) (*tradingdomain.BridgeCommandResponse, error) {
	return &tradingdomain.BridgeCommandResponse{}, nil
}
func (b *fixtureBridge) SetKillSwitch(_ context.Context, enabled bool, _, _ string) error {
	b.kill = enabled
	return nil
}

type fixtureAgent struct{ calls int }

func (a *fixtureAgent) Run(_ context.Context, r tradingdomain.AgentRunRequest) (*tradingdomain.AgentRunResponse, error) {
	a.calls++
	from := r.AsOfTime.In(config.ChinaLocation)
	from = time.Date(from.Year(), from.Month(), from.Day(), 9, 35, 0, 0, config.ChinaLocation)
	eventID := int64(1)
	i := tradingdomain.TradeIntent{RecommendationEventID: &eventID, Symbol: "600000", TSCode: "600000.SH", Market: "SH", AssetType: "STOCK", BoardType: "SH_MAIN", Action: "BUY", ProposedOrderType: "LIMIT", ProposedLimitPrice: "9.00", ProposedPositionRatio: "0.03", Confidence: "0.8", ValidFrom: from, ValidUntil: from.Add(25 * time.Minute), EvidenceRefs: []string{"recommendation_event:1"}}
	i.IntentKey = intentKey(r.StrategyVersion, r.AsOfTime, i)
	return &tradingdomain.AgentRunResponse{SchemaVersion: tradingdomain.SchemaVersionIntent, RunKey: r.RunKey, AsOfTime: r.AsOfTime, StrategyVersion: r.StrategyVersion, StrategyName: r.StrategyName, ToolContractVersion: "trading-tools/v2", CandidateCount: 1, Intents: []tradingdomain.TradeIntent{i}, InputSnapshot: map[string]any{"fixture": true}}, nil
}

func enableFixture(t *testing.T, s *Service) *fixtureBridge {
	cfg := s.runtime.Config()
	cfg.Meta.ConfigVersion = 1
	cfg.Trading.Enabled = true
	cfg.Trading.Environment = "SIMULATION"
	cfg.Trading.Provider = "EASTMONEY_GM"
	cfg.Trading.Bridge.SimulationOnly = true
	cfg.Trading.Decision = config.TradingDecisionConfig{Provider: "DETERMINISTIC", StrategyName: "fixture", StrategyVersion: "1", ToolContractVersion: "trading-tools/v2", MaxCandidatesPerRun: 20, MaxIntentsPerRun: 5}
	cfg.Trading.Agent = config.TradingAgentConfig{Enabled: true, SchemaVersion: tradingdomain.SchemaVersionIntent, TimeoutMS: 1000}
	cfg.Trading.Scheduler = config.TradingSchedulerConfig{Enabled: true, MorningWindow: config.TradingSessionWindow{Start: "09:35:00", End: "11:25:00"}, AfternoonWindow: config.TradingSessionWindow{Start: "13:05:00", End: "14:50:00"}}
	cfg.Trading.Risk = config.TradingRiskConfig{Version: "fixture", AllowedAssetTypes: []string{"STOCK", "ETF"}, AllowedMarkets: []string{"SH", "SZ"}, AllowedBoards: []string{"SH_MAIN"}, AllowedSides: []string{"BUY", "SELL"}, TradingRuleVersion: "fixture", MaxTotalPositionRatio: .6, MaxSymbolPositionRatio: .1, MaxSingleOrderRatio: .05, MaxPositionCount: 10, MaxNewOrdersPerDay: 20, MaxDailyTurnoverRatio: .3, DailyLossKillRatio: .03, MaxPriceDeviationRatio: .02, MinCashReserveRatio: .2, IntentCooldownTradeDays: 1, MaxSnapshotAgeSeconds: 15}
	cfg.Trading.Eastmoney.AccountPolicy.VerifiedBoards = []string{"SH_MAIN"}
	s.db.Create(&db_model.TradingBoardCapability{CapabilityKey: "fixture", AccountID: "fixture", Environment: "SIMULATION", BoardType: "SH_MAIN", AssetType: "STOCK", VerificationStatus: "VERIFIED", TradingRuleVersion: "fixture", BuyEnabled: true, SellEnabled: true})
	cfg.Trading.Reconciliation = config.TradingReconciliationConfig{AutoRepairMissingEvents: true, AutoRepairMissingFills: true}
	cfg.Trading.Calendar.OpenDates = []string{"2026-09-08", "2026-09-09", "2026-09-10", "2026-09-11"}
	if err := s.db.Model(&db_model.TradingRuntimeControl{}).Where("control_key='KILL_SWITCH'").Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	b := &fixtureBridge{now: s.now}
	s.bridge = b
	return b
}

func TestClaimRecoveryAndFencing(t *testing.T) {
	s := ledgerService(t)
	ctx := context.Background()
	now := s.now()
	i := db_model.TradingIntent{IntentKey: "lease", Status: "READY_FOR_EXECUTION", ExecutionStatus: "READY", TradeAction: "BUY", ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour)}
	if err := s.db.Create(&i).Error; err != nil {
		t.Fatal(err)
	}
	claim := func(token string, at time.Time) (*db_model.TradingIntent, error) {
		var v *db_model.TradingIntent
		e := s.db.Transaction(func(tx *gorm.DB) error {
			if e := dal.TradingRuntimeControls.Lock(ctx, tx); e != nil {
				return e
			}
			var e error
			v, e = dal.TradingIntents.ClaimNextReady(ctx, tx, token, token, at, at.Add(time.Minute))
			return e
		})
		return v, e
	}
	if _, e := claim("first", now); e != nil {
		t.Fatal(e)
	}
	if _, e := claim("second", now); e != dal.ErrNotFound {
		t.Fatalf("double claim: %v", e)
	}
	if _, e := claim("recovered", now.Add(61*time.Second)); e != nil {
		t.Fatal(e)
	}
	if ok, e := dal.TradingIntents.FinishClaim(ctx, s.db, i.ID, "first", now.Add(62*time.Second), map[string]any{"execution_status": "COMPLETED"}); e != nil || ok {
		t.Fatalf("stale worker fenced incorrectly %v %v", ok, e)
	}
	if ok, e := dal.TradingIntents.FinishClaim(ctx, s.db, i.ID, "recovered", now.Add(62*time.Second), map[string]any{"execution_status": "COMPLETED"}); e != nil || !ok {
		t.Fatalf("new owner unable to finish %v %v", ok, e)
	}
}

func TestConcurrentClaimOnlyOneWinner(t *testing.T) {
	s := ledgerService(t)
	ctx := context.Background()
	now := s.now()
	s.db.Create(&db_model.TradingIntent{IntentKey: "concurrent", Status: "READY_FOR_EXECUTION", ExecutionStatus: "READY", TradeAction: "BUY", ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour)})
	var wg sync.WaitGroup
	wins := make(chan bool, 8)
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			e := s.db.Transaction(func(tx *gorm.DB) error {
				if e := dal.TradingRuntimeControls.Lock(ctx, tx); e != nil {
					return e
				}
				_, e := dal.TradingIntents.ClaimNextReady(ctx, tx, fmt.Sprint(n), fmt.Sprint(n), now, now.Add(time.Minute))
				return e
			})
			wins <- e == nil
		}(n)
	}
	wg.Wait()
	close(wins)
	count := 0
	for v := range wins {
		if v {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("winners=%d", count)
	}
}

func TestGoExitSurvivesAbsentAgentAndPersistsT1(t *testing.T) {
	s := ledgerService(t)
	b := enableFixture(t, s)
	s.agent = nil
	ctx := context.Background()
	b.positions = []tradingdomain.PositionSnapshot{{AccountID: "fixture", Symbol: "600000", EastmoneySymbol: "SHSE.600000", Volume: 100, AvailableVolume: 0}}
	c := db_model.TradingPositionCycle{CycleKey: "exit", AccountID: "fixture", Status: "OPEN", Symbol: "600000", TSCode: "600000.SH", Market: "SH", AssetType: "STOCK", BoardType: "SH_MAIN", EastmoneySymbol: "SHSE.600000", EntryTradeDate: tradingDay(s.now().AddDate(0, 0, -1)), EntryPrice: "11", CurrentVolume: 100, InitialVolume: 100, StopLossPrice: "10.67", TakeProfitPrice: "11.66", MaxHoldingTradeDays: 20}
	s.db.Create(&c)
	result, e := s.ScanExits(ctx, s.now())
	if e != nil {
		t.Fatal(e)
	}
	if result["intents"] != 0 || result["deferred"] != 1 {
		t.Fatalf("missing T1 pending %+v", result)
	}
	var audits []db_model.TradingSkillDecision
	s.db.Find(&audits)
	if len(audits) != 1 || audits[0].Reason != "T1_PENDING" {
		t.Fatalf("bad pending audit %+v", audits)
	}
	oldNow := s.now()
	s.now = func() time.Time { return oldNow.Add(time.Minute) }
	b.now = s.now
	b.positions[0].AvailableVolume = 100
	result, e = s.ScanExits(ctx, s.now())
	if e != nil {
		t.Fatal(e)
	}
	if result["intents"] != 1 {
		t.Fatalf("exit did not progress %+v", result)
	}
	s.now = func() time.Time { return oldNow.Add(2 * time.Minute) }
	b.now = s.now
	result, e = s.ScanExits(ctx, s.now())
	if e != nil {
		t.Fatal(e)
	}
	if result["intents"] != 0 {
		t.Fatalf("duplicate SELL %+v", result)
	}
	if len(b.placed) != 0 {
		t.Fatal("exit decision must not synchronously dispatch")
	}
}

func TestPreopenQueueExecutionRepricingAndReplay(t *testing.T) {
	s := ledgerService(t)
	b := enableFixture(t, s)
	a := &fixtureAgent{}
	s.agent = a
	ctx := context.Background()
	clock := time.Date(2026, 9, 9, 9, 20, 0, 0, config.ChinaLocation)
	s.now = func() time.Time { return clock }
	b.now = s.now
	request := RunRequest{TriggerType: "PRE_OPEN", AsOfTime: clock}
	view, err := s.StartRun(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if a.calls != 0 || view.Run.Status != "QUEUED" {
		t.Fatal("StartRun blocked on Agent")
	}
	second, err := s.StartRun(ctx, request)
	if err != nil || view.Run.ID != second.Run.ID {
		t.Fatalf("run dedupe failed %v", err)
	}
	if err := s.ProcessNextDecision(ctx, "test-decision"); err != nil {
		t.Fatal(err)
	}
	view, err = s.GetRun(ctx, view.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Intents) != 1 || view.Intents[0].Status != "READY_FOR_EXECUTION" || len(b.placed) != 0 {
		t.Fatalf("bad preopen result %+v", view)
	}
	if err := s.ExecuteNextReadyIntent(ctx, "test-executor", clock); err != dal.ErrNotFound {
		t.Fatalf("preopen must not execute: %v", err)
	}
	clock = clock.Add(15 * time.Minute)
	if _, err := s.Preflight(ctx, clock); err != nil {
		t.Fatal(err)
	}
	if err := s.ExecuteNextReadyIntent(ctx, "test-executor", clock); err != nil {
		t.Fatal(err)
	}
	if len(b.placed) != 1 || b.placed[0].Price != "10.00" || b.placed[0].Volume != 300 {
		t.Fatalf("not repriced/resized from fresh data: %+v", b.placed)
	}
	if err := s.ExecuteNextReadyIntent(ctx, "test-executor", clock); err != dal.ErrNotFound {
		t.Fatalf("unexpected repeat execution: %v", err)
	}
	if len(b.placed) != 1 {
		t.Fatal("duplicate SDK dispatch")
	}
}

func TestShadowQueueHasZeroOrdersAndHolidayHasZeroAgentCalls(t *testing.T) {
	s := ledgerService(t)
	b := enableFixture(t, s)
	a := &fixtureAgent{}
	s.agent = a
	ctx := context.Background()
	s.runtime.Config().Trading.KillSwitch = true
	clock := time.Date(2026, 9, 9, 9, 20, 0, 0, config.ChinaLocation)
	s.now = func() time.Time { return clock }
	b.now = s.now
	view, err := s.StartRun(ctx, RunRequest{TriggerType: "PRE_OPEN", AsOfTime: clock})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ProcessNextDecision(ctx, "shadow"); err != nil {
		t.Fatal(err)
	}
	view, err = s.GetRun(ctx, view.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Intents) != 1 || view.Intents[0].ExecutionStatus != "SHADOW_READY" {
		t.Fatalf("bad shadow output %+v", view)
	}
	if len(b.placed) != 0 {
		t.Fatal("shadow sent an order")
	}
	clock = clock.AddDate(0, 0, 3)
	if _, err := s.StartRun(ctx, RunRequest{TriggerType: "PRE_OPEN", AsOfTime: clock}); err == nil {
		t.Fatal("holiday enqueued a decision")
	}
	if a.calls != 1 {
		t.Fatal("holiday called Agent")
	}
}

func TestIndependentCashAndPositionLedgerDetectsMismatch(t *testing.T) {
	start := time.Date(2026, 9, 9, 9, 0, 0, 0, config.ChinaLocation)
	base := db_model.TradingAccountSnapshot{AvailableCash: "10000", FrozenCash: "0", MarketValue: "0", SnapshotAt: start}
	fills := []db_model.TradingFill{{TradingOrderID: 1, ExecID: "b1", EastmoneySymbol: "SHSE.600000", Side: "BUY", Price: "10", Volume: 200, Amount: "2000", Commission: "5", CommissionStatus: "VERIFIED", TradedAt: start.Add(time.Hour)}, {TradingOrderID: 2, ExecID: "s1", EastmoneySymbol: "SHSE.600000", Side: "SELL", Price: "11", Volume: 100, Amount: "1100", Commission: "5", CommissionStatus: "VERIFIED", TradedAt: start.Add(2 * time.Hour)}}
	orders := []db_model.TradingOrder{{ID: 1, FilledVolume: 200}, {ID: 2, FilledVolume: 100}}
	account := tradingdomain.AccountSnapshot{AccountID: "fixture", AvailableCash: "9090", FrozenCash: "0", MarketValue: "1100", NAV: "10190", CumulativeCommission: "10", SnapshotAt: start.Add(3 * time.Hour)}
	positions := []tradingdomain.PositionSnapshot{{EastmoneySymbol: "SHSE.600000", Volume: 100, AvailableVolume: 100, MarketValue: "1100"}}
	diffs, err := compareLedger(base, fills, orders, account, positions)
	if err != nil || len(diffs) != 0 {
		t.Fatalf("matched ledger rejected: %+v %v", diffs, err)
	}
	account.AvailableCash = "9190"
	positions[0].Volume = 200
	diffs, err = compareLedger(base, fills, orders, account, positions)
	if err != nil {
		t.Fatal(err)
	}
	cashDiff, posDiff := false, false
	for _, d := range diffs {
		cashDiff = cashDiff || d.DiffType == "CASH_DIFF"
		posDiff = posDiff || d.DiffType == "POSITION_DIFF"
	}
	if !cashDiff || !posDiff {
		t.Fatalf("ledger adopted provider values: %+v", diffs)
	}
}

func TestReconcileRecoversMissingFillWithoutDuplication(t *testing.T) {
	s := ledgerService(t)
	b := enableFixture(t, s)
	ctx := context.Background()
	base := db_model.TradingAccountSnapshot{SnapshotKey: "baseline", AccountID: "fixture", Environment: "SIMULATION", AvailableCash: "100000", FrozenCash: "0", MarketValue: "0", Nav: "100000", SnapshotAt: s.now().Add(-time.Hour)}
	if e := s.db.Create(&base).Error; e != nil {
		t.Fatal(e)
	}
	o := seedOrder(t, s, "BUY", 100, nil)
	event := tradingdomain.BridgeEvent{SchemaVersion: tradingdomain.SchemaVersionBridgeEvent, EventHash: hashParts("lost-fill"), ClientOrderID: o.ClientOrderID, AccountID: "fixture", ClOrdID: "broker-1", ExecID: "fill-1", EventType: "EXECUTION_REPORT", NormalizedStatus: "UNKNOWN", FillPrice: "10", FillVolume: 100, Commission: "5", EventAt: s.now().Add(-time.Minute), RawPayload: map[string]any{"commission": "5"}}
	b.executions = []map[string]any{{"client_order_id": o.ClientOrderID, "bridge_event": event}}
	b.positions = []tradingdomain.PositionSnapshot{{AccountID: "fixture", Symbol: "600000", EastmoneySymbol: "SHSE.600000", Volume: 100, AvailableVolume: 0, TodayVolume: 100, VWAP: "10", MarketValue: "1000", LastPrice: "10", FloatingPnL: "0"}}
	b.cash = "98995"
	b.nav = "99995"
	b.marketValue = "1000"
	b.fees = "5"
	b.turnover = "1000"
	run, e := s.Reconcile(ctx, "recovery")
	if e != nil {
		t.Fatal(e)
	}
	if run.Status != "SUCCEEDED" || run.DiffCount != 0 {
		t.Fatalf("recovery failed: %+v", run)
	}
	stored, e := s.GetReconciliation(ctx, run.ID)
	if e != nil {
		t.Fatal(e)
	}
	if stored.RepairedFillCount != 1 {
		t.Fatalf("incorrect repair counter: %+v", stored)
	}
	clock := s.now()
	s.now = func() time.Time { return clock.Add(time.Minute) }
	b.now = s.now
	run, e = s.Reconcile(ctx, "recovery-again")
	if e != nil {
		t.Fatal(e)
	}
	stored, e = s.GetReconciliation(ctx, run.ID)
	if e != nil {
		t.Fatal(e)
	}
	if stored.RepairedFillCount != 0 {
		t.Fatal("counted replay as repaired fill")
	}
	var n int64
	s.db.Model(&db_model.TradingFill{}).Count(&n)
	if n != 1 {
		t.Fatal("duplicate execution")
	}
}
