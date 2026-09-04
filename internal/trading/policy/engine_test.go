package policy

import (
	"testing"
	"time"

	"finance-sys/internal/config"
	tradingdomain "finance-sys/internal/trading/domain"
	"finance-sys/internal/trading/instrument"
)

func baseInput(now time.Time) Input {
	return Input{
		Trading: config.TradingConfig{
			Enabled: true, Environment: "SIMULATION", Provider: "EASTMONEY_GM", AllowLive: false,
			Bridge: config.TradingBridgeConfig{ExpectedAccountID: "sim-1", SimulationOnly: true},
			Risk: config.TradingRiskConfig{
				AllowedAssetTypes: []string{"STOCK", "ETF"}, AllowedMarkets: []string{"SH", "SZ"}, AllowedBoards: []string{instrument.BoardSHMain}, AllowedSides: []string{"BUY", "SELL"},
				MaxTotalPositionRatio: .60, MaxSymbolPositionRatio: .10, MaxSingleOrderRatio: .05, MaxPositionCount: 10,
				MaxNewOrdersPerDay: 20, MaxDailyTurnoverRatio: .30, DailyLossKillRatio: .03, MaxPriceDeviationRatio: .02,
				MinCashReserveRatio: .20, IntentCooldownTradeDays: 5, MaxSnapshotAgeSeconds: 15,
			},
			Eastmoney: config.TradingEastmoneyConfig{AccountPolicy: config.TradingAccountPolicyConfig{VerifiedBoards: []string{instrument.BoardSHMain}}},
		},
		BridgeHealth: tradingdomain.BridgeHealth{Status: "READY", Runner: "READY", Terminal: "CONNECTED", Account: "READY", AuthState: "AUTH_OK"},
		Intent: tradingdomain.TradeIntent{
			Symbol: "600000", TSCode: "600000.SH", Market: "SH", AssetType: "STOCK", BoardType: instrument.BoardSHMain, Action: "BUY", ProposedOrderType: "LIMIT",
			ProposedLimitPrice: "10.234", ProposedPositionRatio: "0.03", ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Minute),
		},
		Account:     tradingdomain.AccountSnapshot{Environment: "SIMULATION", AccountID: "sim-1", NAV: "1000000", AvailableCash: "1000000", SnapshotAt: now.Add(-time.Second)},
		LatestPrice: "10.23", QuoteObservedAt: now.Add(-time.Second), CurrentTotalPositionRatio: "0", CurrentSymbolRatio: "0", DailyTurnoverRatio: "0", DailyLossRatio: "0",
		SessionOpen: true, Now: now,
	}
}

func TestEvaluateBuyDeterministically(t *testing.T) {
	now := time.Date(2026, 8, 24, 9, 40, 0, 0, time.FixedZone("CST", 8*3600))
	engine := New()
	first := engine.Evaluate(baseInput(now))
	second := engine.Evaluate(baseInput(now))
	if !first.Approved || first.FinalPrice != "10.23" || first.FinalVolume != 2900 {
		t.Fatalf("unexpected decision: %+v", first)
	}
	if first.FinalPrice != second.FinalPrice || first.FinalVolume != second.FinalVolume {
		t.Fatal("same input must produce the same order")
	}
	if len(first.Checks) != 12 {
		t.Fatalf("expected all risk checks, got %d", len(first.Checks))
	}
}

func TestEvaluateRejectsNoPriceLimitListingPeriod(t *testing.T) {
	now := time.Now()
	input := baseInput(now)
	input.Trading.Risk.ExcludeNoPriceLimit = true
	input.NoPriceLimitPeriod = true
	result := New().Evaluate(input)
	if result.RejectionCode != "NO_PRICE_LIMIT_PERIOD" {
		t.Fatalf("expected no-price-limit-period rejection, got %+v", result)
	}
}

func TestEvaluateSTARTradingUnit(t *testing.T) {
	now := time.Now()
	input := baseInput(now)
	input.Intent.Symbol = "688002"
	input.Intent.TSCode = "688002.SH"
	input.Intent.BoardType = instrument.BoardSTAR
	input.Trading.Risk.AllowedBoards = append(input.Trading.Risk.AllowedBoards, instrument.BoardSTAR)
	input.Trading.Eastmoney.AccountPolicy.VerifiedBoards = append(input.Trading.Eastmoney.AccountPolicy.VerifiedBoards, instrument.BoardSTAR)
	volume := int64(100)
	input.Intent.ProposedVolume = &volume
	result := New().Evaluate(input)
	if result.RejectionCode != "POSITION_LIMIT" {
		t.Fatalf("expected STAR 100-share order to fail, got %+v", result)
	}
	volume = 201
	input.Intent.ProposedVolume = &volume
	result = New().Evaluate(input)
	if !result.Approved || result.FinalVolume != 201 {
		t.Fatalf("expected STAR 201-share order to pass, got %+v", result)
	}
}

func TestEvaluateFailsClosedForSectorAndKillSwitch(t *testing.T) {
	now := time.Now()
	input := baseInput(now)
	input.Trading.KillSwitch = true
	result := New().Evaluate(input)
	if result.RejectionCode != "TRADING_ENABLED" {
		t.Fatalf("expected kill switch rejection, got %+v", result)
	}

	input = baseInput(now)
	input.Intent.AssetType = "SECTOR"
	result = New().Evaluate(input)
	if result.RejectionCode != "ASSET_WHITELIST" {
		t.Fatalf("expected sector rejection, got %+v", result)
	}
}

func TestEvaluateRejectsStaleSnapshotAndT1Sell(t *testing.T) {
	now := time.Now()
	input := baseInput(now)
	input.Account.SnapshotAt = now.Add(-16 * time.Second)
	result := New().Evaluate(input)
	if result.RejectionCode != "SNAPSHOT_FRESH" {
		t.Fatalf("expected stale snapshot rejection, got %+v", result)
	}

	volume := int64(200)
	input = baseInput(now)
	input.Intent.Action = "SELL"
	input.Intent.ProposedVolume = &volume
	input.Position = &tradingdomain.PositionSnapshot{AvailableVolume: 100}
	result = New().Evaluate(input)
	if result.RejectionCode != "T1_SELLABLE" {
		t.Fatalf("expected T+1 rejection, got %+v", result)
	}
}

func TestEvaluateRejectsStaleQuote(t *testing.T) {
	now := time.Now()
	input := baseInput(now)
	input.QuoteObservedAt = now.Add(-16 * time.Second)
	result := New().Evaluate(input)
	if result.RejectionCode != "PRICE_BAND" {
		t.Fatalf("expected stale quote rejection, got %+v", result)
	}
}

func TestEvaluateCapsBuyAtProposedVolume(t *testing.T) {
	now := time.Now()
	input := baseInput(now)
	volume := int64(100)
	input.Intent.ProposedVolume = &volume
	result := New().Evaluate(input)
	if !result.Approved || result.FinalVolume != 100 {
		t.Fatalf("expected one-lot approved order, got %+v", result)
	}
}
