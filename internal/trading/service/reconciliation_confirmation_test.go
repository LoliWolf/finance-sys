package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/dal"
	"finance-sys/internal/domain/db_model"
	tradingdomain "finance-sys/internal/trading/domain"
	"finance-sys/internal/trading/policy"
)

type sequenceBridge struct {
	*fixtureBridge
	refresh          func(int) error
	calls            int
	freezeAfterFirst bool
	killError        error
}

func (b *sequenceBridge) SetKillSwitch(ctx context.Context, enabled bool, reason, key string) error {
	if b.killError != nil {
		return b.killError
	}
	return b.fixtureBridge.SetKillSwitch(ctx, enabled, reason, key)
}

func (b *sequenceBridge) RefreshSnapshot(ctx context.Context, key string) (*tradingdomain.BridgeCommandResponse, error) {
	b.calls++
	if b.refresh != nil {
		if err := b.refresh(b.calls); err != nil {
			return nil, err
		}
	}
	if b.freezeAfterFirst && b.calls > 1 {
		return &tradingdomain.BridgeCommandResponse{Status: "QUEUED"}, nil
	}
	return b.fixtureBridge.RefreshSnapshot(ctx, key)
}

func confirmationFixture(t *testing.T) (*Service, *sequenceBridge) {
	t.Helper()
	s := ledgerService(t)
	b := &sequenceBridge{fixtureBridge: enableFixture(t, s)}
	s.bridge = b
	s.runtime.Config().Trading.Reconciliation.ValuationRetryIntervalMS = 1 // accelerate only in isolated fixtures
	s.runtime.Config().Trading.Bridge.RequestTimeoutMS = 500
	base := db_model.TradingAccountSnapshot{SnapshotKey: "flat-baseline", SnapshotVersion: "baseline", AccountID: "fixture", Environment: "SIMULATION", AvailableCash: "100000", FrozenCash: "0", MarketValue: "0", Nav: "100000", SnapshotAt: s.now().Add(-time.Hour)}
	if err := s.db.Create(&base).Error; err != nil {
		t.Fatal(err)
	}
	return s, b
}

func readAttempts(t *testing.T, s *Service, run *db_model.TradingReconciliationRun) []reconciliationSnapshotAttempt {
	t.Helper()
	stored, err := s.GetReconciliation(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var summary struct {
		Attempts []reconciliationSnapshotAttempt `json:"snapshot_attempts"`
	}
	if err := json.Unmarshal(stored.SummaryJSON, &summary); err != nil {
		t.Fatal(err)
	}
	return summary.Attempts
}

func assertKill(t *testing.T, s *Service, b *sequenceBridge, want bool) {
	t.Helper()
	kill, err := dal.TradingRuntimeControls.KillSwitch(context.Background(), s.db)
	if err != nil || kill.Enabled != want || b.kill != want {
		t.Fatalf("kill DB=%+v Bridge=%v err=%v want=%v", kill, b.kill, err, want)
	}
}

func TestReconciliationValuationConfirmation(t *testing.T) {
	for _, kind := range []string{"sold-position-still-in-total", "buy-not-yet-in-total", "NAV-mark-timing"} {
		t.Run(kind, func(t *testing.T) {
			s, b := confirmationFixture(t)
			if kind == "buy-not-yet-in-total" {
				o := seedOrder(t, s, "BUY", 100, nil)
				event := tradingdomain.BridgeEvent{SchemaVersion: tradingdomain.SchemaVersionBridgeEvent, EventHash: hashParts("delayed-value-fill"), ClientOrderID: o.ClientOrderID, AccountID: "fixture", ClOrdID: "broker", ExecID: "fill", EventType: "EXECUTION_REPORT", NormalizedStatus: "UNKNOWN", FillPrice: "10", FillVolume: 100, Commission: "5", EventAt: s.now().Add(-time.Minute), RawPayload: map[string]any{"commission": "5"}}
				b.executions = []map[string]any{{"client_order_id": o.ClientOrderID, "bridge_event": event}}
				b.positions = []tradingdomain.PositionSnapshot{{AccountID: "fixture", Symbol: "600000", EastmoneySymbol: "SHSE.600000", Volume: 100, TodayVolume: 100, VWAP: "10", MarketValue: "1000", LastPrice: "10", FloatingPnL: "0"}}
				b.cash, b.fees, b.turnover = "98995", "5", "1000"
			}
			b.refresh = func(n int) error {
				if n == 1 {
					switch kind {
					case "sold-position-still-in-total":
						b.marketValue, b.nav = "332.70", "100000"
					case "buy-not-yet-in-total":
						b.marketValue, b.nav = "0", "99994"
					case "NAV-mark-timing":
						b.marketValue, b.nav = "0", "100002.10"
					}
				} else {
					// Evidence must already be durable before asking for another snapshot.
					var running db_model.TradingReconciliationRun
					if err := s.db.Where("status='RUNNING'").First(&running).Error; err != nil {
						t.Fatal(err)
					}
					if len(readAttempts(t, s, &running)) != 1 {
						t.Fatal("first mismatch evidence was lost")
					}
					if err := s.evaluateIntent(context.Background(), &db_model.TradingIntent{}, false); !errors.Is(err, ErrExecutionDeferred) {
						t.Fatalf("local execution not blocked: %v", err)
					}
					other := New(s.db, s.runtime, nil, b, policy.New(), nil)
					other.now = s.now
					if err := other.evaluateIntent(context.Background(), &db_model.TradingIntent{}, false); !errors.Is(err, ErrExecutionDeferred) {
						t.Fatalf("durable barrier not enforced: %v", err)
					}
					assertKill(t, s, b, false) // transient valuation alone must not latch kill
					b.marketValue, b.nav = "0", "100000"
					if kind == "buy-not-yet-in-total" {
						b.marketValue, b.nav = "1000", "99995"
					}
				}
				return nil
			}
			run, err := s.Reconcile(context.Background(), kind)
			if err != nil || run.Status != "SUCCEEDED" || run.DiffCount != 0 || b.calls != 2 {
				t.Fatalf("run=%+v calls=%d err=%v", run, b.calls, err)
			}
			attempts := readAttempts(t, s, run)
			if len(attempts) != 2 || len(attempts[0].Diffs) == 0 || len(attempts[1].Diffs) != 0 || attempts[0].SnapshotVersion == attempts[1].SnapshotVersion {
				t.Fatalf("bad evidence: %+v", attempts)
			}
			var rows int64
			s.db.Model(&db_model.TradingReconciliationDiff{}).Count(&rows)
			if rows != 0 || len(b.placed) != 0 {
				t.Fatal("transient difference promoted to P0 or order placed")
			}
			if kind == "buy-not-yet-in-total" {
				stored, _ := s.GetReconciliation(context.Background(), run.ID)
				if stored.RepairedFillCount != 1 || stored.RepairedEventCount != 1 {
					t.Fatalf("replayed fill counted twice: %+v", stored)
				}
				s.db.Model(&db_model.TradingFill{}).Count(&rows)
				if rows != 1 {
					t.Fatal("duplicate fill during confirmation")
				}
			}
			assertKill(t, s, b, false)
		})
	}
}

func TestReconciliationPersistentValuationAndHardFaults(t *testing.T) {
	for _, kind := range []string{"valuation", "cash", "quantity", "fees", "unknown-order", "missing-order", "mixed"} {
		t.Run(kind, func(t *testing.T) {
			s, b := confirmationFixture(t)
			want := 1
			switch kind {
			case "valuation":
				b.marketValue = "332.70"
				want = 3
			case "cash":
				b.cash = "100001"
			case "quantity":
				b.positions = []tradingdomain.PositionSnapshot{{EastmoneySymbol: "SHSE.600000", Volume: 100, AvailableVolume: 100, MarketValue: "0"}}
			case "fees":
				b.fees = "1"
			case "unknown-order":
				b.orders = []map[string]any{{"cl_ord_id": "foreign"}}
			case "missing-order":
				seedOrder(t, s, "BUY", 100, nil)
			case "mixed":
				b.marketValue, b.cash = "332.70", "100001"
			}
			run, err := s.Reconcile(context.Background(), kind)
			if err != nil || run.Status != "FAILED" || run.DiffCount == 0 || b.calls != want {
				t.Fatalf("run=%+v calls=%d err=%v", run, b.calls, err)
			}
			if len(readAttempts(t, s, run)) != want {
				t.Fatal("missing attempt audit")
			}
			assertKill(t, s, b, true)
			var rows []db_model.TradingReconciliationDiff
			s.db.Find(&rows)
			if len(rows) != int(run.DiffCount) {
				t.Fatal("persistent fault not recorded")
			}
			for _, d := range rows {
				if d.Resolution != "OPEN" {
					t.Fatal("auto-resolved a ledger difference")
				}
			}
		})
	}
}

func TestReconciliationConfirmationInterruptedFailsClosed(t *testing.T) {
	for _, kind := range []string{"cancel", "refresh-error", "stale-snapshot", "deadline", "regressed-snapshot"} {
		t.Run(kind, func(t *testing.T) {
			s, b := confirmationFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			b.marketValue = "332.70"
			b.refresh = func(n int) error {
				if n == 2 {
					switch kind {
					case "cancel":
						cancel()
					case "refresh-error":
						return fmt.Errorf("provider unavailable")
					case "regressed-snapshot":
						b.now = func() time.Time { return s.now().Add(-time.Second) }
					}
				}
				return nil
			}
			if kind == "stale-snapshot" {
				b.freezeAfterFirst = true
			}
			if kind == "deadline" {
				s.runtime.Config().Trading.Reconciliation.RefreshTimeoutSeconds = 1
				s.runtime.Config().Trading.Reconciliation.ValuationRetryIntervalMS = 2000
			}
			run, err := s.Reconcile(ctx, kind)
			if err == nil || run == nil || run.Status != "FAILED" {
				t.Fatalf("false acceptance: %+v %v", run, err)
			}
			stored, e := s.GetReconciliation(context.Background(), run.ID)
			if e != nil || stored.Status != "FAILED" || stored.ErrorCode != "SNAPSHOT_CONFIRMATION_FAILED" {
				t.Fatalf("cancellation not finalized: %+v %v", stored, e)
			}
			if len(readAttempts(t, s, run)) != 1 {
				t.Fatal("lost last failed observation")
			}
			assertKill(t, s, b, true)
		})
	}
}

func TestReconciliationKillFailureRetainsDurableBarrier(t *testing.T) {
	s, b := confirmationFixture(t)
	b.cash = "100001"
	b.killError = errors.New("Bridge offline")
	run, err := s.Reconcile(context.Background(), "kill-failure")
	if err == nil || run.Status != "RUNNING" {
		t.Fatalf("lost recovery barrier: %+v %v", run, err)
	}
	stored, e := s.GetReconciliation(context.Background(), run.ID)
	if e != nil || stored.Status != "RUNNING" || stored.ErrorCode != "KILL_SWITCH_FAILED" {
		t.Fatalf("fault not checkpointed: %+v %v", stored, e)
	}
	if err := s.requireNoRunningReconciliation(context.Background(), s.db, "fixture"); !errors.Is(err, ErrExecutionDeferred) {
		t.Fatal("barrier released before safety latch confirmed")
	}
}

func TestReconciliationReclaimedRunKeepsPriorAttemptEvidence(t *testing.T) {
	s, b := confirmationFixture(t)
	deadline := s.now().Add(time.Minute)
	prior := reconciliationSnapshotAttempt{SnapshotVersion: "old-observation", SnapshotAt: s.now().Add(-time.Minute), Diffs: []reconciliationDiffEvidence{{Type: "NAV_DIFF", Local: json.RawMessage(`"100000"`), Provider: json.RawMessage(`"100001"`)}}}
	run := db_model.TradingReconciliationRun{RunKey: "recovered", AccountID: "fixture", Status: "RUNNING", ClaimToken: "new-worker", ClaimDeadline: &deadline, ConfigVersion: 1, SummaryJSON: marshalJSON(map[string]any{"snapshot_attempts": []reconciliationSnapshotAttempt{prior}}), ErrorCode: "OLD_FAILURE"}
	if err := s.db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	completed, err := s.performReconciliation(context.Background(), &run)
	if err != nil || completed.Status != "SUCCEEDED" || b.calls != 1 {
		t.Fatalf("%+v %v", completed, err)
	}
	attempts := readAttempts(t, s, completed)
	if len(attempts) != 2 || attempts[0].SnapshotVersion != "old-observation" {
		t.Fatal("recovery overwrote prior audit")
	}
	stored, _ := s.GetReconciliation(context.Background(), run.ID)
	if stored.ErrorCode != "" {
		t.Fatal("stale recovery error on completed run")
	}
}

func TestReconciliationSuccessNeverClearsExistingKillOrHistory(t *testing.T) {
	s, b := confirmationFixture(t)
	b.kill = true
	if err := dal.TradingRuntimeControls.SetKillSwitch(context.Background(), s.db, true, "existing", "test", 1); err != nil {
		t.Fatal(err)
	}
	old := db_model.TradingReconciliationDiff{ReconciliationRunID: 999, DiffType: "MARKET_VALUE_DIFF", Severity: "P0", Resolution: "OPEN"}
	if err := s.db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	run, err := s.Reconcile(context.Background(), "clean-but-disabled")
	if err != nil || run.Status != "SUCCEEDED" || b.calls != 1 {
		t.Fatalf("%+v %v", run, err)
	}
	assertKill(t, s, b, true)
	s.db.First(&old, old.ID)
	if old.Resolution != "OPEN" {
		t.Fatal("historical fault rewritten")
	}
}

func TestExecutionFinalFenceHonorsReconciliationIncludingExpiredLease(t *testing.T) {
	s, b := confirmationFixture(t)
	for _, expired := range []bool{false, true} {
		deadline := s.now().Add(time.Minute)
		if expired {
			deadline = s.now().Add(-time.Minute)
		}
		run := db_model.TradingReconciliationRun{RunKey: fmt.Sprint(expired), AccountID: "fixture", Status: "RUNNING", ClaimDeadline: &deadline}
		if err := s.db.Create(&run).Error; err != nil {
			t.Fatal(err)
		}
		intent := db_model.TradingIntent{IntentKey: fmt.Sprint(expired), Status: "PROPOSED"}
		if err := s.db.Create(&intent).Error; err != nil {
			t.Fatal(err)
		}
		err := s.persistDecision(context.Background(), s.runtime.Config(), &intent, tradingdomain.TradeIntent{}, policy.Result{Approved: true, FinalVolume: 100, FinalPrice: "10"}, false)
		if !errors.Is(err, ErrExecutionDeferred) {
			t.Fatalf("running reconciliation bypassed final fence: %v", err)
		}
		var n int64
		s.db.Model(&db_model.TradingOrder{}).Count(&n)
		if n != 0 || len(b.placed) != 0 {
			t.Fatal("dispatched while reconciliation pending")
		}
	}
}

func TestValuationClassificationAndConfirmationLimits(t *testing.T) {
	for _, kind := range []string{"CASH_DIFF", "POSITION_DIFF", "FEE_DIFF", "FEE_UNVERIFIED", "MISSING_EXECUTION", "UNTRACKED_PROVIDER_ORDER", "COMMISSION_MISMATCH", "MISSING_PROVIDER_ORDER", "UNKNOWN_NEW_DIFF"} {
		if valuationOnlyDiffs([]db_model.TradingReconciliationDiff{{DiffType: "NAV_DIFF"}, {DiffType: kind}}) {
			t.Fatalf("unsafe retry of %s", kind)
		}
	}
	if valuationOnlyDiffs(nil) {
		t.Fatal("clean snapshots retried")
	}
	n, interval, budget := reconciliationConfirmationSettings(config.TradingReconciliationConfig{})
	if n != 3 || interval != time.Second || budget != 10*time.Second {
		t.Fatal("bad defaults")
	}
	n, interval, budget = reconciliationConfirmationSettings(config.TradingReconciliationConfig{ValuationMaxAttempts: 999, ValuationRetryIntervalMS: 999999, RefreshTimeoutSeconds: 999})
	if n != 5 || interval != 5*time.Second || budget != 40*time.Second {
		t.Fatal("unbounded confirmation")
	}
}
