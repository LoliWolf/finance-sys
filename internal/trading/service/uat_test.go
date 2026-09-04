package service

import (
	"testing"
	"time"

	tradingdomain "finance-sys/internal/trading/domain"
)

func TestOneLotUATLimitPrice(t *testing.T) {
	price, err := oneLotUATLimitPrice("9.14", "BUY")
	if err != nil || price != "9.16" {
		t.Fatalf("unexpected UAT limit price %q, err=%v", price, err)
	}
	price, err = oneLotUATLimitPrice("8.99", "SELL")
	if err != nil || price != "8.97" {
		t.Fatalf("unexpected SELL UAT limit price %q, err=%v", price, err)
	}
}

func TestQuotesFreshForUsesConfiguredMinimum(t *testing.T) {
	now := time.Now()
	quotes := []tradingdomain.QuoteSnapshot{{Symbol: "600000", EastmoneySymbol: "SHSE.600000", Price: "9.18", ObservedAt: now.Add(-3 * time.Second)}}
	if !quotesFreshFor(quotes, []string{"SHSE.600000"}, now.Add(-15*time.Second)) {
		t.Fatal("quote inside configured freshness window should be accepted")
	}
	if quotesFreshFor(quotes, []string{"SHSE.600000"}, now.Add(-2*time.Second)) {
		t.Fatal("quote older than the minimum observation time should be rejected")
	}
}

func TestBridgeSnapshotWaitSettings(t *testing.T) {
	wait, maxAge := bridgeSnapshotWaitSettings(nil)
	if wait != 5*time.Second || maxAge != 15*time.Second {
		t.Fatalf("unexpected defaults: wait=%s maxAge=%s", wait, maxAge)
	}
}

func TestFixedDecimalRemovesProviderFloatTail(t *testing.T) {
	if got := fixedDecimal("9.180000305175781", 6); got != "9.180000" {
		t.Fatalf("unexpected fixed decimal %s", got)
	}
	if got := multiplyDecimal(fixedDecimal("9.180000305175781", 6), 100); got != "918.000000" {
		t.Fatalf("unexpected exact amount %s", got)
	}
}
