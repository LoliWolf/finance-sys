package service

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/dal"
	"finance-sys/internal/domain/db_model"
	tradingdomain "finance-sys/internal/trading/domain"
	"finance-sys/internal/trading/policy"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// In-memory SQLite only: no middleware, Nacos, live MySQL or broker is involved.
// Strip MySQL-specific DDL tags, retaining all modeled columns and key guards.
func memoryTradingDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	models := []any{db_model.TradingAgentRun{}, db_model.TradingIntent{}, db_model.TradingOrder{}, db_model.TradingFill{}, db_model.TradingOrderEvent{}, db_model.TradingPositionCycle{}, db_model.TradingRiskCheck{}, db_model.TradingRuntimeControl{}, db_model.TradingDailySession{}, db_model.TradingSkillDecision{}, db_model.TradingAccountSnapshot{}, db_model.TradingPositionSnapshot{}, db_model.TradingReconciliationRun{}, db_model.TradingReconciliationDiff{}, db_model.TradingBoardCapability{}}
	for _, model := range models {
		typ := reflect.TypeOf(model)
		statement := &gorm.Statement{DB: db}
		if err := statement.Parse(reflect.New(typ).Interface()); err != nil {
			t.Fatal(err)
		}
		for _, f := range statement.Schema.Fields {
			if f.DBName != "id" {
				f.HasDefaultValue = false
				f.DefaultValue = ""
				f.DefaultValueInterface = nil
			}
		}
		defaults := statement.Schema.FieldsWithDefaultDBValue[:0]
		for _, f := range statement.Schema.FieldsWithDefaultDBValue {
			if f.DBName == "id" {
				defaults = append(defaults, f)
			}
		}
		statement.Schema.FieldsWithDefaultDBValue = defaults
		ptr := reflect.New(typ).Interface().(interface{ TableName() string })
		columns := []string{}
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			col := strings.Split(strings.TrimPrefix(f.Tag.Get("gorm"), "column:"), ";")[0]
			kind := "TEXT"
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft == reflect.TypeOf(time.Time{}) {
				kind = "datetime"
			} else if ft.Kind() == reflect.Int || ft.Kind() == reflect.Int64 || ft.Kind() == reflect.Int32 || ft.Kind() == reflect.Bool {
				kind = "INTEGER"
			} else if ft.Kind() == reflect.Slice {
				kind = "BLOB"
			}
			if col == "id" {
				kind = "INTEGER PRIMARY KEY AUTOINCREMENT"
			}
			columns = append(columns, col+" "+kind)
		}
		if err := db.Exec("CREATE TABLE " + ptr.TableName() + " (" + strings.Join(columns, ",") + ")").Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, spec := range []struct{ table, field string }{{"trading_order_events", "event_hash"}, {"trading_fills", "account_id,exec_id"}, {"trading_orders", "trading_intent_id"}, {"trading_orders", "client_order_id"}, {"trading_intents", "intent_key"}, {"trading_agent_runs", "run_key"}, {"trading_position_cycles", "cycle_key"}, {"trading_position_cycles", "open_symbol_key"}, {"trading_runtime_controls", "control_key"}, {"trading_skill_decisions", "decision_key"}, {"trading_daily_sessions", "session_key"}, {"trading_board_capabilities", "capability_key"}, {"trading_account_snapshots", "snapshot_key"}, {"trading_reconciliation_runs", "run_key"}} {
		if err := db.Exec("CREATE UNIQUE INDEX ix_" + spec.table + "_" + strings.ReplaceAll(spec.field, ",", "_") + " ON " + spec.table + " (" + spec.field + ")").Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&db_model.TradingRuntimeControl{ControlKey: "KILL_SWITCH", Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func ledgerService(t *testing.T) *Service {
	cfg := &config.Config{Meta: config.MetaConfig{Timezone: "Asia/Shanghai", ConfigVersion: 1}}
	cfg.Trading.Bridge.ExpectedAccountID = "fixture"
	cfg.Trading.Risk.TradingRuleVersion = "fixture-v1"
	cfg.Trading.Exit = config.TradingExitConfig{StopLossRatio: .03, TakeProfitRatio: .06, MaxHoldingTradeDays: 20}
	cfg.Trading.Calendar = config.TradingCalendarConfig{Source: "fixture", Version: "1", CoverageFrom: "2026-09-01", CoverageTo: "2026-09-30", OpenDates: []string{"2026-09-09", "2026-09-10", "2026-09-11"}}
	snap, err := config.NewSnapshot(cfg, []byte(`{}`), "test", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	s := New(memoryTradingDB(t), config.NewRuntime(snap), nil, nil, policy.New(), nil)
	s.now = func() time.Time { return time.Date(2026, 9, 10, 9, 40, 0, 0, config.ChinaLocation) }
	s.db.Config.NowFunc = func() time.Time { return s.now() }
	return s
}

func seedOrder(t *testing.T, s *Service, side string, volume int64, cycle *int64) db_model.TradingOrder {
	t.Helper()
	i := db_model.TradingIntent{IntentKey: fmt.Sprintf("intent-%s-%d", side, volume), TradeAction: side, Symbol: "600000", TSCode: "600000.SH", Market: "SH", AssetType: "STOCK", BoardType: "SH_MAIN", EastmoneySymbol: "SHSE.600000", PositionCycleID: cycle, StrategyVersion: "test", EvidenceRefsJSON: []byte(`[]`)}
	if err := s.db.Create(&i).Error; err != nil {
		t.Fatal(err)
	}
	o := db_model.TradingOrder{TradingIntentID: i.ID, ClientOrderID: fmt.Sprintf("order-%d", i.ID), AccountID: "fixture", Provider: "EASTMONEY_GM", Symbol: i.Symbol, EastmoneySymbol: i.EastmoneySymbol, Side: side, Volume: volume, Status: "BRIDGE_QUEUED", RequestJSON: []byte(`{}`), FilledAmount: "0"}
	if err := s.db.Create(&o).Error; err != nil {
		t.Fatal(err)
	}
	return o
}

func sendStatus(t *testing.T, s *Service, o db_model.TradingOrder, status string, filled int64, at time.Time) tradingdomain.BridgeEvent {
	t.Helper()
	e := tradingdomain.BridgeEvent{SchemaVersion: tradingdomain.SchemaVersionBridgeEvent, EventHash: hashParts(o.ClientOrderID, status, fmt.Sprint(filled)), ClientOrderID: o.ClientOrderID, AccountID: o.AccountID, NormalizedStatus: status, FilledVolume: filled, FilledVWAP: "10", EventAt: at, RawPayload: map[string]any{}}
	if err := s.HandleBridgeEvent(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestPartialBuyCancelAndMultipleSellLedger(t *testing.T) {
	s := ledgerService(t)
	buy := seedOrder(t, s, "BUY", 300, nil)
	sendStatus(t, s, buy, "PARTIALLY_FILLED", 200, s.now().AddDate(0, 0, -1))
	sendStatus(t, s, buy, "CANCELED", 200, s.now().AddDate(0, 0, -1).Add(time.Minute))
	var cycle db_model.TradingPositionCycle
	if err := s.db.First(&cycle).Error; err != nil {
		t.Fatal(err)
	}
	if cycle.CurrentVolume != 200 || cycle.Status != "OPEN" {
		t.Fatalf("partial buy untracked: %+v", cycle)
	}
	sell1 := seedOrder(t, s, "SELL", 100, &cycle.ID)
	e := sendStatus(t, s, sell1, "FILLED", 100, s.now())
	s.db.First(&cycle, cycle.ID)
	if cycle.CurrentVolume != 100 || cycle.Status == "CLOSED" {
		t.Fatalf("partial exit closed cycle: %+v", cycle)
	}
	if err := s.HandleBridgeEvent(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	sendStatus(t, s, sell1, "PARTIALLY_FILLED", 50, s.now().Add(-time.Minute))
	s.db.First(&cycle, cycle.ID)
	if cycle.CurrentVolume != 100 {
		t.Fatalf("duplicate/out-of-order changed volume: %d", cycle.CurrentVolume)
	}
	sell2 := seedOrder(t, s, "SELL", 200, &cycle.ID)
	sendStatus(t, s, sell2, "PARTIALLY_FILLED", 100, s.now().Add(time.Minute))
	sendStatus(t, s, sell2, "CANCELED", 100, s.now().Add(2*time.Minute))
	s.db.First(&cycle, cycle.ID)
	if cycle.CurrentVolume != 0 || cycle.Status != "CLOSED" || cycle.OpenSymbolKey != nil {
		t.Fatalf("cycle not fully closed: %+v", cycle)
	}
	sendStatus(t, s, buy, "CANCELED", 200, s.now().AddDate(0, 0, -1).Add(time.Minute))
	var count int64
	s.db.Model(&db_model.TradingPositionCycle{}).Count(&count)
	if count != 1 {
		t.Fatal("replayed old BUY created a second cycle")
	}
}

func TestMergeExecutionBeforeOrderStatus(t *testing.T) {
	o := db_model.TradingOrder{Volume: 200, Status: "BRIDGE_QUEUED", FilledAmount: "0"}
	fills := []db_model.TradingFill{{Volume: 100, Amount: "1000"}, {Volume: 100, Amount: "1100"}}
	if err := mergeOrderEvent(&o, tradingdomain.BridgeEvent{NormalizedStatus: "UNKNOWN"}, fills); err != nil {
		t.Fatal(err)
	}
	if o.Status != "FILLED" || o.FilledVolume != 200 || o.FilledVwap == nil || *o.FilledVwap != "10.500000" {
		t.Fatalf("bad merge %+v", o)
	}
}

func TestPendingProviderIDsUseNullNotEmptyUniqueKey(t *testing.T) {
	db := memoryTradingDB(t)
	if err := db.Exec("CREATE UNIQUE INDEX provider_unique ON trading_orders(provider,account_id,cl_ord_id)").Error; err != nil {
		t.Fatal(err)
	}
	for n := int64(1); n <= 2; n++ {
		o := db_model.TradingOrder{TradingIntentID: n, ClientOrderID: fmt.Sprint(n), Provider: "EASTMONEY_GM", AccountID: "fixture", Status: "DISPATCH_PENDING"}
		if err := dal.TradingOrders.Create(context.Background(), db, &o); err != nil {
			t.Fatal(err)
		}
	}
	var count int64
	db.Model(&db_model.TradingOrder{}).Where("cl_ord_id IS NULL").Count(&count)
	if count != 2 {
		t.Fatalf("nullable provider IDs count=%d", count)
	}
}
