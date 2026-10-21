package service

// This is an explicitly seeded MECHANICS scenario, not a natural strategy win.
// Real historical daily opens drive simulated fills. No network broker exists.
import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/domain/db_model"
	tradingdomain "finance-sys/internal/trading/domain"
)

type historicalBridge struct {
	*fixtureBridge
	price string
}

func (b *historicalBridge) Quotes(ctx context.Context, symbols []string) ([]tradingdomain.QuoteSnapshot, error) {
	rows, err := b.fixtureBridge.Quotes(ctx, symbols)
	p, _ := strconv.ParseFloat(b.price, 64)
	for i := range rows {
		rows[i].Price = b.price
		rows[i].Bid1 = b.price
		rows[i].Ask1 = b.price
		rows[i].UpperLimit = fmt.Sprintf("%.2f", p*1.1)
		rows[i].LowerLimit = fmt.Sprintf("%.2f", p*.9)
	}
	return rows, err
}

func TestHistoricalDailyBarsGoLifecycle(t *testing.T) {
	path := os.Getenv("FINANCE_OFFLINE_REPLAY_DATASET")
	if path == "" {
		t.Skip("set FINANCE_OFFLINE_REPLAY_DATASET to the frozen read-only export")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var data struct {
		Start    string                       `json:"start"`
		End      string                       `json:"end"`
		Calendar config.TradingCalendarConfig `json:"calendar"`
		Quotes   []struct {
			Symbol string          `json:"symbol"`
			Date   string          `json:"trade_date"`
			Open   json.RawMessage `json:"open_price"`
		} `json:"quotes"`
	}
	if err = json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	dates := []string{}
	for _, d := range data.Calendar.OpenDates {
		if d >= data.Start && d <= data.End {
			dates = append(dates, d)
		}
	}
	if len(dates) < 2 {
		t.Fatal("two trading sessions required")
	}
	// Deterministically choose a main-board symbol with both real daily opens.
	prices := map[string]map[string]string{}
	for _, q := range data.Quotes {
		if len(q.Symbol) != 6 || q.Symbol[:2] != "60" {
			continue
		}
		if prices[q.Symbol] == nil {
			prices[q.Symbol] = map[string]string{}
		}
		var p string
		if json.Unmarshal(q.Open, &p) != nil {
			p = string(q.Open)
		}
		prices[q.Symbol][q.Date] = p
	}
	symbol := ""
	for code, p := range prices {
		if p[dates[0]] != "" && p[dates[1]] != "" && (symbol == "" || code < symbol) {
			symbol = code
		}
	}
	if symbol == "" {
		t.Fatal("no SH main-board pair")
	}
	ctx := context.Background()
	s := ledgerService(t)
	fixture := enableFixture(t, s)
	s.agent = nil
	cfg := s.runtime.Config()
	cfg.Trading.Calendar = data.Calendar
	cfg.Trading.Exit.MaxHoldingTradeDays = 1 // forced lifecycle branch only
	clock, _ := time.ParseInLocation("2006-01-02 15:00:00", dates[0]+" 15:00:00", config.ChinaLocation)
	// Use regular session clock, independent of real wall time.
	clock = time.Date(clock.Year(), clock.Month(), clock.Day(), 10, 0, 0, 0, config.ChinaLocation)
	s.now = func() time.Time { return clock }
	fixture.now = s.now
	b := &historicalBridge{fixtureBridge: fixture, price: prices[symbol][dates[0]]}
	s.bridge = b
	baseline := db_model.TradingAccountSnapshot{SnapshotKey: "historical-baseline", AccountID: "fixture", Environment: "SIMULATION", AvailableCash: "100000", FrozenCash: "0", MarketValue: "0", Nav: "100000", SnapshotAt: clock.Add(-time.Hour)}
	if err = s.db.Create(&baseline).Error; err != nil {
		t.Fatal(err)
	}
	buy := seedOrder(t, s, "BUY", 200, nil)
	for _, table := range []string{"trading_intents", "trading_orders"} {
		if err = s.db.Table(table).Where("id = ?", int64(1)).Updates(map[string]any{"symbol": symbol, "eastmoney_symbol": "SHSE." + symbol}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err = s.db.Model(&db_model.TradingIntent{}).Where("id = ?", buy.TradingIntentID).Update("ts_code", symbol+".SH").Error; err != nil {
		t.Fatal(err)
	}
	if err = s.db.First(&buy, buy.ID).Error; err != nil {
		t.Fatal(err)
	}
	fill := func(o db_model.TradingOrder, key, price, fee string, volume int64) {
		t.Helper()
		e := tradingdomain.BridgeEvent{SchemaVersion: tradingdomain.SchemaVersionBridgeEvent, EventHash: hashParts(key), ClientOrderID: o.ClientOrderID, AccountID: "fixture", ClOrdID: key + "-order", ExecID: key, EventType: "EXECUTION_REPORT", NormalizedStatus: "UNKNOWN", FillPrice: price, FillVolume: volume, Commission: fee, EventAt: clock, RawPayload: map[string]any{"fixture": "historical-mechanics", "commission": fee}}
		if e1 := s.HandleBridgeEvent(ctx, e); e1 != nil {
			t.Fatal(e1)
		}
		if e1 := s.HandleBridgeEvent(ctx, e); e1 != nil {
			t.Fatal(e1)
		}
	}
	fill(buy, "historical-buy", b.price, "5", 100)
	// Partial fill followed by cancellation: only the filled 100 shares become a position.
	cancel := tradingdomain.BridgeEvent{SchemaVersion: tradingdomain.SchemaVersionBridgeEvent, EventHash: hashParts("historical-cancel"), ClientOrderID: buy.ClientOrderID, AccountID: "fixture", NormalizedStatus: "CANCELED", FilledVolume: 100, FilledVWAP: b.price, EventAt: clock.Add(time.Second), RawPayload: map[string]any{}}
	if err = s.HandleBridgeEvent(ctx, cancel); err != nil {
		t.Fatal(err)
	}
	var cycle db_model.TradingPositionCycle
	if err = s.db.First(&cycle).Error; err != nil {
		t.Fatal(err)
	}
	if cycle.CurrentVolume != 100 || cycle.SellableTradeDate == nil || cycle.SellableTradeDate.Format(time.DateOnly) != dates[1] {
		t.Fatalf("bad partial/T+1 cycle %+v", cycle)
	}
	entry, _ := strconv.ParseFloat(b.price, 64)
	next, _ := time.ParseInLocation(time.DateOnly, dates[1], config.ChinaLocation)
	clock = next.Add(10 * time.Hour)
	b.price = prices[symbol][dates[1]]
	exit, _ := strconv.ParseFloat(b.price, 64)
	b.positions = []tradingdomain.PositionSnapshot{{AccountID: "fixture", Symbol: symbol, EastmoneySymbol: "SHSE." + symbol, Volume: 100, AvailableVolume: 100, VWAP: fmt.Sprint(entry), LastPrice: b.price, MarketValue: fmt.Sprintf("%.2f", exit*100)}}
	b.cash = fmt.Sprintf("%.2f", 100000-entry*100-5)
	b.marketValue = fmt.Sprintf("%.2f", exit*100)
	b.nav = fmt.Sprintf("%.2f", 100000-entry*100-5+exit*100)
	b.fees = "5"
	b.turnover = fmt.Sprintf("%.2f", entry*100)
	result, err := s.ScanExits(ctx, clock)
	if err != nil || result["intents"] != 1 {
		t.Fatalf("independent Go exit %+v %v", result, err)
	}
	var intent db_model.TradingIntent
	if err = s.db.Where("trade_action = ?", "SELL").First(&intent).Error; err != nil {
		t.Fatal(err)
	}
	sell := db_model.TradingOrder{TradingIntentID: intent.ID, ClientOrderID: "historical-go-exit", AccountID: "fixture", Provider: "EASTMONEY_GM", Symbol: symbol, EastmoneySymbol: "SHSE." + symbol, Side: "SELL", Volume: 100, Status: "BRIDGE_QUEUED", RequestJSON: []byte(`{}`), FilledAmount: "0"}
	if err = s.db.Create(&sell).Error; err != nil {
		t.Fatal(err)
	}
	fee := 5 + exit*100*.0005
	fee, _ = strconv.ParseFloat(fmt.Sprintf("%.2f", fee), 64)
	fill(sell, "historical-sell", b.price, fmt.Sprintf("%.2f", fee), 100)
	if err = s.db.First(&cycle, cycle.ID).Error; err != nil {
		t.Fatal(err)
	}
	if cycle.Status != "CLOSED" || cycle.CurrentVolume != 0 || cycle.OpenSymbolKey != nil {
		t.Fatalf("not closed %+v", cycle)
	}
	b.positions = nil
	b.cash = fmt.Sprintf("%.2f", 100000-entry*100-5+exit*100-fee)
	b.nav = b.cash
	b.marketValue = "0"
	b.fees = fmt.Sprintf("%.2f", 5+fee)
	b.turnover = fmt.Sprintf("%.2f", (entry+exit)*100)
	rec, err := s.Reconcile(ctx, "OFFLINE_HISTORICAL_EOD")
	if err != nil || rec.Status != "SUCCEEDED" || rec.DiffCount != 0 {
		t.Fatalf("strict reconciliation %+v %v", rec, err)
	}
	var count int64
	s.db.Model(&db_model.TradingFill{}).Count(&count)
	if count != 2 || len(b.placed) != 0 {
		t.Fatalf("duplicate fills or external submission count=%d placed=%d", count, len(b.placed))
	}
	session := db_model.TradingDailySession{SessionKey: "historical-session", AccountID: "fixture", TradeDate: tradingDay(clock), Status: "OPEN"}
	if err = s.db.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	if err = s.closeTradingDay(ctx, clock, true, ""); err != nil {
		t.Fatal(err)
	}
	if err = s.db.First(&session, session.ID).Error; err != nil {
		t.Fatal(err)
	}
	if session.Status != "CLOSED" {
		t.Fatalf("day not closed %+v", session)
	}
	t.Logf("SEEDED_MECHANICS_ONLY symbol=%s dates=%s/%s entry=%s exit=%s fills=2 cycle=CLOSED reconciliation=%s fees=%s final_cash=%s EOD=%s", symbol, dates[0], dates[1], prices[symbol][dates[0]], b.price, rec.Status, b.fees, b.cash, session.Status)
}
