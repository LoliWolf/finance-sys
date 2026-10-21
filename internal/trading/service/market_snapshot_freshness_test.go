package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"finance-sys/internal/config"
	tradingdomain "finance-sys/internal/trading/domain"
)

// No network, Nacos or MySQL: all scenarios use the existing memory ledger.
type selectionQuoteBridge struct {
	*fixtureBridge
	quotes       func(int) ([]tradingdomain.QuoteSnapshot, error)
	refreshErr   error
	refreshCalls int
	readCalls    int
}

func (b *selectionQuoteBridge) RefreshQuotes(context.Context, []string, string) (*tradingdomain.BridgeCommandResponse, error) {
	b.refreshCalls++
	return &tradingdomain.BridgeCommandResponse{Status: "QUEUED"}, b.refreshErr
}

func (b *selectionQuoteBridge) Quotes(context.Context, []string) ([]tradingdomain.QuoteSnapshot, error) {
	b.readCalls++
	return b.quotes(b.readCalls)
}

func selectionQuoteService(t *testing.T) (*Service, *selectionQuoteBridge) {
	t.Helper()
	s := ledgerService(t)
	b := &selectionQuoteBridge{fixtureBridge: enableFixture(t, s)}
	s.bridge = b
	s.runtime.Config().Trading.Bridge.RequestTimeoutMS = 1 // bounded minimum is 500ms
	// Seed a real-looking yesterday price. Intraday failures must not use it.
	if err := s.db.Exec(`CREATE TABLE stock_daily_quotes (id INTEGER PRIMARY KEY, symbol TEXT, ts_code TEXT, source TEXT, trade_date DATETIME, close_price REAL, updated_at DATETIME)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.db.Exec("INSERT INTO stock_daily_quotes VALUES (1, '600825', '600825.SH', 'TUSHARE', ?, 8, ?)", s.now().AddDate(0, 0, -1), s.now()).Error; err != nil {
		t.Fatal(err)
	}
	return s, b
}

func selectionQuote(symbol string, at time.Time) tradingdomain.QuoteSnapshot {
	return tradingdomain.QuoteSnapshot{Symbol: symbol, EastmoneySymbol: "SHSE." + symbol, Price: "10", Source: "EASTMONEY_GM_CURRENT_PRICE", ObservedAt: at}
}

func TestMarketSnapshotsPreserveNineteenFreshQuotesBesideOneStale(t *testing.T) {
	s, b := selectionQuoteService(t)
	symbols := []string{"600825"}
	quotes := []tradingdomain.QuoteSnapshot{selectionQuote("600825", s.now().AddDate(0, 0, -1))}
	for i := 1; i <= 19; i++ {
		code := fmt.Sprintf("600%03d", i)
		symbols = append(symbols, code)
		quotes = append(quotes, selectionQuote(code, s.now().Add(-time.Second)))
	}
	b.quotes = func(int) ([]tradingdomain.QuoteSnapshot, error) { return quotes, nil }
	rows, err := s.MarketSnapshots(context.Background(), symbols, s.now())
	if err != nil || len(rows) != 20 {
		t.Fatalf("snapshot failed: %+v %v", rows, err)
	}
	for i, row := range rows {
		if i == 0 {
			if row.Price != "" || row.TradeDate != "" || row.MissingReason != "REALTIME_QUOTE_UNAVAILABLE" || row.Source != "BRIDGE" {
				t.Fatalf("stale quote fell back to a usable daily price: %+v", row)
			}
		} else if row.Price != "10" || row.MissingReason != "" || row.Source != "EASTMONEY_GM_CURRENT_PRICE" {
			t.Fatalf("fresh peer was discarded: %+v", row)
		}
	}
	if b.refreshCalls != 1 || len(b.placed) != 0 {
		t.Fatalf("unexpected refresh or order: %+v", b)
	}
	// Existing execution/exit/UAT callers must still see incomplete batches as
	// errors, even though the selection tool can use the fresh subset.
	partial, err := s.refreshBridgeQuotes(context.Background(), symbols, s.now())
	if err == nil || len(partial) != 19 {
		t.Fatalf("strict completeness contract lost: %d %v", len(partial), err)
	}
}

func TestMarketSnapshotsMissingInvalidAndFailedQuotesNeverFallBackIntraday(t *testing.T) {
	for _, kind := range []string{"empty", "zero", "negative", "nan", "bad-number", "missing-time", "future-time", "refresh-error", "read-error", "no-bridge"} {
		t.Run(kind, func(t *testing.T) {
			s, b := selectionQuoteService(t)
			q := selectionQuote("600825", s.now())
			switch kind {
			case "zero":
				q.Price = "0"
			case "negative":
				q.Price = "-10"
			case "nan":
				q.Price = "NaN"
			case "bad-number":
				q.Price = "invalid"
			case "missing-time":
				q.ObservedAt = time.Time{}
			case "future-time":
				q.ObservedAt = s.now().Add(2 * time.Second)
			case "refresh-error":
				b.refreshErr = errors.New("fixture refresh unavailable")
			case "no-bridge":
				s.bridge = nil
			}
			b.quotes = func(int) ([]tradingdomain.QuoteSnapshot, error) {
				if kind == "empty" {
					return nil, nil
				}
				if kind == "read-error" {
					return nil, errors.New("fixture read unavailable")
				}
				return []tradingdomain.QuoteSnapshot{q}, nil
			}
			rows, err := s.MarketSnapshots(context.Background(), []string{"600825"}, s.now())
			if err != nil || len(rows) != 1 || rows[0].Price != "" || rows[0].MissingReason != "REALTIME_QUOTE_UNAVAILABLE" {
				t.Fatalf("unavailable quote became usable: %+v %v", rows, err)
			}
		})
	}
}

func TestMarketSnapshotsAgeUsesRetrievalClockNotTruncatedDecision(t *testing.T) {
	s, b := selectionQuoteService(t)
	clock := s.now().Add(50 * time.Second)
	s.now = func() time.Time { return clock }
	b.quotes = func(int) ([]tradingdomain.QuoteSnapshot, error) {
		return []tradingdomain.QuoteSnapshot{selectionQuote("600825", clock.Add(-30*time.Second)), selectionQuote("600001", clock.Add(-10*time.Second))}, nil
	}
	rows, err := s.MarketSnapshots(context.Background(), []string{"600825", "600001"}, clock.Truncate(time.Minute))
	if err != nil || len(rows) != 2 || rows[0].MissingReason == "" || rows[1].Price != "10" {
		t.Fatalf("truncated decision admitted an old quote: %+v %v", rows, err)
	}
}

func TestRefreshBridgeQuotesAccumulatesRotatingCacheWithoutUnrelatedSymbols(t *testing.T) {
	s, b := selectionQuoteService(t)
	b.quotes = func(call int) ([]tradingdomain.QuoteSnapshot, error) {
		if call == 1 {
			return []tradingdomain.QuoteSnapshot{selectionQuote("600001", s.now()), selectionQuote("600999", s.now())}, nil
		}
		return []tradingdomain.QuoteSnapshot{selectionQuote("600002", s.now())}, nil
	}
	rows, err := s.refreshBridgeQuotes(context.Background(), []string{"600002", "600001.SH", "SHSE.600001"}, s.now())
	if err != nil || len(rows) != 2 || rows[0].Symbol != "600001" || rows[1].Symbol != "600002" || b.readCalls != 2 {
		t.Fatalf("partial cache handling failed: %+v %v", rows, err)
	}
}

func TestRefreshBridgeQuotesRechecksAgeAtTimeout(t *testing.T) {
	s, b := selectionQuoteService(t)
	start := s.now()
	clock := start
	s.now = func() time.Time { return clock }
	b.quotes = func(call int) ([]tradingdomain.QuoteSnapshot, error) {
		if call > 1 {
			clock = start.Add(16 * time.Second)
		}
		return []tradingdomain.QuoteSnapshot{selectionQuote("600001", start)}, nil
	}
	rows, err := s.refreshBridgeQuotes(context.Background(), []string{"600001", "600002"}, start)
	if err == nil || len(rows) != 0 {
		t.Fatalf("quote expired while waiting but was returned: %+v %v", rows, err)
	}
}

func TestMarketSnapshotsCancelledRequestDoesNotReturnPartialOrDailyPrice(t *testing.T) {
	s, b := selectionQuoteService(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.quotes = func(int) ([]tradingdomain.QuoteSnapshot, error) {
		cancel()
		return []tradingdomain.QuoteSnapshot{selectionQuote("600825", s.now())}, nil
	}
	rows, err := s.MarketSnapshots(ctx, []string{"600825"}, s.now())
	if !errors.Is(err, context.Canceled) || len(rows) != 0 {
		t.Fatalf("cancelled request produced prices: %+v %v", rows, err)
	}
}

func TestMarketSnapshotsHistoricalDailyFallbackRemainsOffline(t *testing.T) {
	s, b := selectionQuoteService(t)
	b.quotes = func(int) ([]tradingdomain.QuoteSnapshot, error) { panic("historical lookup must not call Bridge") }
	// Same reference date but no longer near the wall clock, so this is an
	// explicitly historical lookup, not an intraday quote fallback.
	at := s.now()
	s.now = func() time.Time { return at.AddDate(0, 0, 1) }
	rows, err := s.MarketSnapshots(context.Background(), []string{"600825"}, at)
	if err != nil || len(rows) != 1 || rows[0].Price != "8.0000" || rows[0].Source != "TUSHARE" || b.refreshCalls != 0 {
		t.Fatalf("historical path changed: %+v %v", rows, err)
	}
}

func TestMarketSnapshotsNormalizesQuoteDateToShanghai(t *testing.T) {
	s, b := selectionQuoteService(t)
	q := selectionQuote("600825", s.now().In(time.FixedZone("test-west", -8*60*60)))
	b.quotes = func(int) ([]tradingdomain.QuoteSnapshot, error) { return []tradingdomain.QuoteSnapshot{q}, nil }
	rows, err := s.MarketSnapshots(context.Background(), []string{"600825", "bad-symbol"}, s.now())
	if err != nil || len(rows) != 2 || rows[0].TradeDate != config.ChinaDate(s.now()) || rows[1].MissingReason != "INVALID_SYMBOL" {
		t.Fatalf("date or invalid symbol mismatch: %+v %v", rows, err)
	}
}
