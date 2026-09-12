package rules

import (
	"finance-sys/internal/config"
	tradingdomain "finance-sys/internal/trading/domain"
	"testing"
	"time"
)

func TestTradingExitPriorityAndMissingPrices(t *testing.T) {
	for _, tc := range []struct {
		price  string
		days   int
		reason string
	}{{"9", 21, "STOP_LOSS"}, {"12", 21, "TAKE_PROFIT"}, {"10", 20, "MAX_HOLDING_DAYS"}, {"10", 1, ""}} {
		reason, err := TradingExitReason(tc.price, "9.7", "10.6", tc.days, 20)
		if err != nil || reason != tc.reason {
			t.Fatalf("%+v: %s %v", tc, reason, err)
		}
	}
	if _, err := TradingExitReason("", "9.7", "10.6", 1, 20); err == nil {
		t.Fatal("missing price accepted")
	}
	s, take, err := TradingExitLevels("10", .03, .06)
	if err != nil || s != "9.700000" || take != "10.600000" {
		t.Fatalf("bad levels %s %s %v", s, take, err)
	}
}

func TestTradingPriceUsesETFPrecisionAndVerifiedBook(t *testing.T) {
	now := time.Date(2026, 9, 9, 10, 0, 0, 0, config.ChinaLocation)
	quote := tradingdomain.QuoteSnapshot{AssetType: "ETF", Ask1: "4.632", Bid1: "4.631", UpperLimit: "5.086", LowerLimit: "4.162", PriceTick: "0.001", ObservedAt: now, MetadataDate: "2026-09-09", SecurityStatus: "TRADING"}
	price, err := TradingPrice("BUY", quote, now, 15*time.Second)
	if err != nil || price != "4.632" {
		t.Fatalf("ETF price %s %v", price, err)
	}
	quote.Ask1 = "0"
	if _, err := TradingPrice("BUY", quote, now, 15*time.Second); err == nil {
		t.Fatal("book missing but fabricated price used")
	}
}
