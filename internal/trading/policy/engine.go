package policy

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"finance-sys/internal/config"
	tradingdomain "finance-sys/internal/trading/domain"
	"finance-sys/internal/trading/instrument"
)

type Check struct {
	Code     string         `json:"check_code"`
	Order    int            `json:"check_order"`
	Passed   bool           `json:"passed"`
	Decision string         `json:"decision"`
	Reason   string         `json:"reason"`
	Observed map[string]any `json:"observed"`
	Limit    map[string]any `json:"limit"`
}

type Input struct {
	Trading                   config.TradingConfig
	RuntimeKillSwitch         bool
	BridgeHealth              tradingdomain.BridgeHealth
	Intent                    tradingdomain.TradeIntent
	Account                   tradingdomain.AccountSnapshot
	Position                  *tradingdomain.PositionSnapshot
	LatestPrice               string
	QuoteObservedAt           time.Time
	CurrentTotalPositionRatio string
	CurrentSymbolRatio        string
	DailyTurnoverRatio        string
	DailyLossRatio            string
	NewOrdersToday            int
	PositionCount             int
	DuplicateInCooldown       bool
	SessionOpen               bool
	Now                       time.Time
	TradingUnit               instrument.TradingUnitRule
	NoPriceLimitPeriod        bool
}

type Result struct {
	Approved         bool    `json:"approved"`
	Decision         string  `json:"decision"`
	RejectionCode    string  `json:"rejection_code,omitempty"`
	RejectionMessage string  `json:"rejection_message,omitempty"`
	FinalPrice       string  `json:"final_price,omitempty"`
	FinalVolume      int64   `json:"final_volume,omitempty"`
	Checks           []Check `json:"checks"`
}

type Engine struct{}

func New() *Engine { return &Engine{} }

func (e *Engine) Evaluate(input Input) Result {
	if input.Now.IsZero() {
		input.Now = time.Now()
	}
	checks := make([]Check, 0, 10)
	reject := func(code, reason string, observed, limit map[string]any) Result {
		checks = append(checks, Check{Code: code, Order: len(checks) + 1, Passed: false, Decision: "REJECT", Reason: reason, Observed: observed, Limit: limit})
		return Result{Approved: false, Decision: "REJECTED", RejectionCode: code, RejectionMessage: reason, Checks: checks}
	}
	pass := func(code string, observed, limit map[string]any) {
		checks = append(checks, Check{Code: code, Order: len(checks) + 1, Passed: true, Decision: "PASS", Reason: "passed", Observed: observed, Limit: limit})
	}

	if !input.Trading.Enabled || input.Trading.KillSwitch || input.RuntimeKillSwitch || input.BridgeHealth.KillSwitch {
		return reject("TRADING_ENABLED", "one or more trading kill switches are enabled", map[string]any{
			"nacos_enabled": input.Trading.Enabled, "nacos_kill_switch": input.Trading.KillSwitch,
			"runtime_kill_switch": input.RuntimeKillSwitch, "bridge_kill_switch": input.BridgeHealth.KillSwitch,
		}, map[string]any{"all_layers_must_allow_new_orders": true})
	}
	pass("TRADING_ENABLED", map[string]any{"all_layers_allow": true}, map[string]any{"required": true})

	if input.Trading.Environment != "SIMULATION" || input.Trading.AllowLive || !input.Trading.Bridge.SimulationOnly || input.Account.Environment != "SIMULATION" || input.Account.AccountID == "" || input.Account.AccountID != input.Trading.Bridge.ExpectedAccountID {
		return reject("SIMULATION_ONLY", "simulation account identity does not match the configured allowlist", map[string]any{
			"environment": input.Trading.Environment, "allow_live": input.Trading.AllowLive,
			"account_environment": input.Account.Environment, "account_id_match": input.Account.AccountID == input.Trading.Bridge.ExpectedAccountID,
		}, map[string]any{"environment": "SIMULATION", "allow_live": false, "simulation_only": true})
	}
	pass("SIMULATION_ONLY", map[string]any{"account_id_match": true}, map[string]any{"environment": "SIMULATION"})

	if !containsFold(input.Trading.Risk.AllowedAssetTypes, input.Intent.AssetType) || !containsFold(input.Trading.Risk.AllowedMarkets, input.Intent.Market) || !containsFold(input.Trading.Risk.AllowedSides, input.Intent.Action) || strings.EqualFold(input.Intent.AssetType, "SECTOR") {
		return reject("ASSET_WHITELIST", "asset, market, or side is not allowed", map[string]any{"asset_type": input.Intent.AssetType, "market": input.Intent.Market, "side": input.Intent.Action}, map[string]any{"asset_types": input.Trading.Risk.AllowedAssetTypes, "markets": input.Trading.Risk.AllowedMarkets, "sides": input.Trading.Risk.AllowedSides})
	}
	pass("ASSET_WHITELIST", map[string]any{"asset_type": input.Intent.AssetType, "market": input.Intent.Market, "side": input.Intent.Action}, map[string]any{"sector_forbidden": true})

	if !instrument.ContainsBoard(input.Trading.Risk.AllowedBoards, input.Intent.BoardType) || !instrument.ContainsBoard(input.Trading.Eastmoney.AccountPolicy.VerifiedBoards, input.Intent.BoardType) {
		return reject("BOARD_WHITELIST", "board is not both allowed and verified for the simulation account", map[string]any{
			"board_type": input.Intent.BoardType, "allowed_boards": input.Trading.Risk.AllowedBoards,
			"verified_boards": input.Trading.Eastmoney.AccountPolicy.VerifiedBoards,
		}, map[string]any{"board_must_be_allowed_and_verified": true})
	}
	pass("BOARD_WHITELIST", map[string]any{"board_type": input.Intent.BoardType}, map[string]any{"account_verified": true})

	if input.Trading.Risk.ExcludeNoPriceLimit && input.NoPriceLimitPeriod && strings.EqualFold(input.Intent.Action, "BUY") {
		return reject("NO_PRICE_LIMIT_PERIOD", "new listings without a daily price limit are excluded", map[string]any{"no_price_limit_period": true, "board_type": input.Intent.BoardType}, map[string]any{"excluded": true})
	}
	pass("NO_PRICE_LIMIT_PERIOD", map[string]any{"no_price_limit_period": input.NoPriceLimitPeriod}, map[string]any{"buy_excluded_when_true": input.Trading.Risk.ExcludeNoPriceLimit})

	unit := input.TradingUnit
	if unit.MinimumBuyVolume <= 0 {
		var unitErr error
		unit, unitErr = instrument.UnitRule(input.Intent.BoardType)
		if unitErr != nil {
			return reject("TRADING_UNIT", unitErr.Error(), map[string]any{"board_type": input.Intent.BoardType}, map[string]any{"known_board_rule": true})
		}
	}

	age := input.Now.Sub(input.Account.SnapshotAt)
	bridgeReady := input.BridgeHealth.Status == "READY" && input.BridgeHealth.Runner == "READY" && input.BridgeHealth.Terminal == "CONNECTED" && input.BridgeHealth.Account == "READY" && input.BridgeHealth.AuthState == "AUTH_OK"
	if input.Account.SnapshotAt.IsZero() || age < 0 || age > time.Duration(input.Trading.Risk.MaxSnapshotAgeSeconds)*time.Second || !bridgeReady {
		return reject("SNAPSHOT_FRESH", "account snapshot or Bridge health is stale", map[string]any{"snapshot_age_seconds": age.Seconds(), "bridge_status": input.BridgeHealth.Status, "auth_state": input.BridgeHealth.AuthState}, map[string]any{"max_snapshot_age_seconds": input.Trading.Risk.MaxSnapshotAgeSeconds, "bridge_status": "READY"})
	}
	pass("SNAPSHOT_FRESH", map[string]any{"snapshot_age_seconds": age.Seconds()}, map[string]any{"max_snapshot_age_seconds": input.Trading.Risk.MaxSnapshotAgeSeconds})

	if !input.SessionOpen || input.Now.Before(input.Intent.ValidFrom) || input.Now.After(input.Intent.ValidUntil) {
		return reject("TRADING_SESSION", "intent is outside the configured trading session or validity window", map[string]any{"session_open": input.SessionOpen, "now": input.Now, "valid_from": input.Intent.ValidFrom, "valid_until": input.Intent.ValidUntil}, map[string]any{"must_be_open_and_valid": true})
	}
	pass("TRADING_SESSION", map[string]any{"session_open": true}, map[string]any{"must_be_open_and_valid": true})

	available := int64(0)
	if input.Position != nil {
		available = input.Position.AvailableVolume
	}
	if strings.EqualFold(input.Intent.Action, "SELL") && (available <= 0 || (input.Intent.ProposedVolume != nil && *input.Intent.ProposedVolume > available)) {
		return reject("T1_SELLABLE", "sell volume exceeds the T+1 available position", map[string]any{"available_volume": available, "proposed_volume": input.Intent.ProposedVolume}, map[string]any{"minimum_sell_volume": unit.MinimumSellVolume})
	}
	pass("T1_SELLABLE", map[string]any{"available_volume": available}, map[string]any{"minimum_sell_volume": unit.MinimumSellVolume})

	price, err := normalizePrice(input.Intent.ProposedLimitPrice)
	if err != nil {
		return reject("PRICE_BAND", "invalid proposed limit price", map[string]any{"price": input.Intent.ProposedLimitPrice}, map[string]any{"positive_price": true})
	}
	quoteAge := input.Now.Sub(input.QuoteObservedAt)
	if input.QuoteObservedAt.IsZero() || quoteAge < 0 || quoteAge > time.Duration(input.Trading.Risk.MaxSnapshotAgeSeconds)*time.Second {
		return reject("PRICE_BAND", "latest quote is unavailable or stale", map[string]any{"latest_price": input.LatestPrice, "quote_age_seconds": quoteAge.Seconds()}, map[string]any{"max_quote_age_seconds": input.Trading.Risk.MaxSnapshotAgeSeconds})
	}
	deviation, err := priceDeviation(price, input.LatestPrice)
	if err != nil || deviation.Cmp(ratFromFloat(input.Trading.Risk.MaxPriceDeviationRatio)) > 0 {
		return reject("PRICE_BAND", "limit price exceeds the configured deviation from the latest quote", map[string]any{"limit_price": price, "latest_price": input.LatestPrice, "deviation": ratString(deviation, 8)}, map[string]any{"max_price_deviation_ratio": input.Trading.Risk.MaxPriceDeviationRatio})
	}
	pass("PRICE_BAND", map[string]any{"limit_price": price, "latest_price": input.LatestPrice, "deviation": ratString(deviation, 8)}, map[string]any{"max_price_deviation_ratio": input.Trading.Risk.MaxPriceDeviationRatio})

	input.TradingUnit = unit
	volume, volumeErr := calculateVolume(input, price, available)
	if volumeErr != nil || volume <= 0 || (input.Position == nil && input.PositionCount >= input.Trading.Risk.MaxPositionCount) {
		reason := "position limit leaves less than one tradable lot"
		if volumeErr != nil {
			reason = volumeErr.Error()
		}
		return reject("POSITION_LIMIT", reason, map[string]any{"calculated_volume": volume, "position_count": input.PositionCount, "current_total_ratio": input.CurrentTotalPositionRatio, "current_symbol_ratio": input.CurrentSymbolRatio}, map[string]any{"minimum_buy_volume": unit.MinimumBuyVolume, "max_position_count": input.Trading.Risk.MaxPositionCount})
	}
	pass("POSITION_LIMIT", map[string]any{"calculated_volume": volume, "position_count": input.PositionCount}, map[string]any{"minimum_buy_volume": unit.MinimumBuyVolume, "max_position_count": input.Trading.Risk.MaxPositionCount})

	turnover, turnoverErr := parseRat(input.DailyTurnoverRatio)
	loss, lossErr := parseRat(input.DailyLossRatio)
	if turnoverErr != nil || lossErr != nil || input.NewOrdersToday >= input.Trading.Risk.MaxNewOrdersPerDay || turnover.Cmp(ratFromFloat(input.Trading.Risk.MaxDailyTurnoverRatio)) >= 0 || loss.Cmp(ratFromFloat(input.Trading.Risk.DailyLossKillRatio)) >= 0 {
		return reject("DAILY_LIMIT", "daily order, turnover, or loss limit has been reached", map[string]any{"new_orders": input.NewOrdersToday, "turnover_ratio": input.DailyTurnoverRatio, "loss_ratio": input.DailyLossRatio}, map[string]any{"max_new_orders": input.Trading.Risk.MaxNewOrdersPerDay, "max_turnover_ratio": input.Trading.Risk.MaxDailyTurnoverRatio, "daily_loss_kill_ratio": input.Trading.Risk.DailyLossKillRatio})
	}
	pass("DAILY_LIMIT", map[string]any{"new_orders": input.NewOrdersToday, "turnover_ratio": input.DailyTurnoverRatio, "loss_ratio": input.DailyLossRatio}, map[string]any{"within_limits": true})

	if input.DuplicateInCooldown {
		return reject("DUPLICATE_COOLDOWN", "the recommendation or symbol is still in its trade-day cooldown", map[string]any{"duplicate": true}, map[string]any{"cooldown_trade_days": input.Trading.Risk.IntentCooldownTradeDays})
	}
	pass("DUPLICATE_COOLDOWN", map[string]any{"duplicate": false}, map[string]any{"cooldown_trade_days": input.Trading.Risk.IntentCooldownTradeDays})

	return Result{Approved: true, Decision: "APPROVED", FinalPrice: price, FinalVolume: volume, Checks: checks}
}

func calculateVolume(input Input, price string, available int64) (int64, error) {
	if strings.EqualFold(input.Intent.Action, "SELL") {
		volume := available
		if input.Intent.ProposedVolume != nil && *input.Intent.ProposedVolume < volume {
			volume = *input.Intent.ProposedVolume
		}
		return instrument.RoundSellVolume(volume, available, input.TradingUnit)
	}

	nav, err := parsePositiveRat(input.Account.NAV)
	if err != nil {
		return 0, fmt.Errorf("invalid account nav")
	}
	availableCash, err := parsePositiveRat(input.Account.AvailableCash)
	if err != nil {
		return 0, fmt.Errorf("invalid available cash")
	}
	limitPrice, err := parsePositiveRat(price)
	if err != nil {
		return 0, fmt.Errorf("invalid limit price")
	}
	proposed, err := parsePositiveRat(input.Intent.ProposedPositionRatio)
	if err != nil {
		return 0, fmt.Errorf("invalid proposed position ratio")
	}
	currentTotal, err := parseNonNegativeRat(input.CurrentTotalPositionRatio)
	if err != nil {
		return 0, fmt.Errorf("invalid total position ratio")
	}
	currentSymbol, err := parseNonNegativeRat(input.CurrentSymbolRatio)
	if err != nil {
		return 0, fmt.Errorf("invalid symbol position ratio")
	}

	remainingSymbol := new(big.Rat).Sub(ratFromFloat(input.Trading.Risk.MaxSymbolPositionRatio), currentSymbol)
	remainingTotal := new(big.Rat).Sub(ratFromFloat(input.Trading.Risk.MaxTotalPositionRatio), currentTotal)
	effective := minRat(proposed, ratFromFloat(input.Trading.Risk.MaxSingleOrderRatio), remainingSymbol, remainingTotal)
	if effective.Sign() <= 0 {
		return 0, fmt.Errorf("position ratio budget is exhausted")
	}
	reserve := new(big.Rat).Mul(nav, ratFromFloat(input.Trading.Risk.MinCashReserveRatio))
	cashBudget := new(big.Rat).Sub(availableCash, reserve)
	ratioBudget := new(big.Rat).Mul(nav, effective)
	budget := minRat(cashBudget, ratioBudget)
	if budget.Sign() <= 0 {
		return 0, fmt.Errorf("cash reserve leaves no order budget")
	}
	raw := new(big.Rat).Quo(budget, limitPrice)
	units := new(big.Int).Quo(raw.Num(), raw.Denom())
	volume := units.Int64()
	if input.Intent.ProposedVolume != nil {
		proposed, proposedErr := instrument.RoundBuyVolume(*input.Intent.ProposedVolume, input.TradingUnit)
		if proposedErr != nil || proposed != *input.Intent.ProposedVolume {
			return 0, fmt.Errorf("proposed volume does not satisfy the board trading unit")
		}
		if *input.Intent.ProposedVolume < volume {
			volume = *input.Intent.ProposedVolume
		}
	}
	return instrument.RoundBuyVolume(volume, input.TradingUnit)
}

func normalizePrice(value string) (string, error) {
	r, err := parsePositiveRat(value)
	if err != nil {
		return "", err
	}
	scaled := new(big.Rat).Mul(r, big.NewRat(100, 1))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(scaled.Num(), scaled.Denom(), remainder)
	if new(big.Int).Mul(remainder, big.NewInt(2)).Cmp(scaled.Denom()) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	cents := quotient.Int64()
	if cents <= 0 {
		return "", fmt.Errorf("price must be positive")
	}
	return fmt.Sprintf("%d.%02d", cents/100, cents%100), nil
}

func priceDeviation(price, latest string) (*big.Rat, error) {
	p, err := parsePositiveRat(price)
	if err != nil {
		return new(big.Rat), err
	}
	l, err := parsePositiveRat(latest)
	if err != nil {
		return new(big.Rat), err
	}
	delta := new(big.Rat).Sub(p, l)
	if delta.Sign() < 0 {
		delta.Neg(delta)
	}
	return delta.Quo(delta, l), nil
}

func parseRat(value string) (*big.Rat, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "0"
	}
	r, ok := new(big.Rat).SetString(value)
	if !ok {
		return nil, fmt.Errorf("invalid decimal %q", value)
	}
	return r, nil
}

func parsePositiveRat(value string) (*big.Rat, error) {
	r, err := parseRat(value)
	if err != nil || r.Sign() <= 0 {
		return nil, fmt.Errorf("decimal must be positive")
	}
	return r, nil
}

func parseNonNegativeRat(value string) (*big.Rat, error) {
	r, err := parseRat(value)
	if err != nil || r.Sign() < 0 {
		return nil, fmt.Errorf("decimal must be non-negative")
	}
	return r, nil
}

func ratFromFloat(value float64) *big.Rat {
	r, _ := new(big.Rat).SetString(strconv.FormatFloat(value, 'f', 8, 64))
	return r
}

func minRat(values ...*big.Rat) *big.Rat {
	minimum := new(big.Rat).Set(values[0])
	for _, value := range values[1:] {
		if value.Cmp(minimum) < 0 {
			minimum.Set(value)
		}
	}
	return minimum
}

func ratString(value *big.Rat, precision int) string {
	if value == nil {
		return ""
	}
	return value.FloatString(precision)
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}
