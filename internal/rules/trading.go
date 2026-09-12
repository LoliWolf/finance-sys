package rules

import (
	"fmt"
	"math/big"
	"time"

	"finance-sys/internal/config"
	tradingdomain "finance-sys/internal/trading/domain"
)

func tradingPositive(value string) (*big.Rat, error) {
	r, ok := new(big.Rat).SetString(value)
	if !ok || r.Sign() <= 0 {
		return nil, fmt.Errorf("invalid positive decimal %q", value)
	}
	return r, nil
}

// TradingPrice chooses the opposite best price, not an Agent-proposed price.
// A zero/missing book is non-tradable, never synthesized from the last trade.
func TradingPrice(side string, quote tradingdomain.QuoteSnapshot, now time.Time, maxAge time.Duration) (string, error) {
	if quote.ObservedAt.IsZero() || now.Sub(quote.ObservedAt) > maxAge || quote.ObservedAt.After(now.Add(time.Second)) {
		return "", fmt.Errorf("QUOTE_STALE")
	}
	if quote.MetadataDate != config.ChinaDate(now) || quote.SecurityStatus != "TRADING" {
		return "", fmt.Errorf("SECURITY_METADATA_UNAVAILABLE")
	}
	value := quote.Ask1
	if side == "SELL" {
		value = quote.Bid1
	} else if side != "BUY" {
		return "", fmt.Errorf("unsupported side")
	}
	price, err := tradingPositive(value)
	if err != nil {
		return "", fmt.Errorf("ORDER_BOOK_UNAVAILABLE")
	}
	upper, err := tradingPositive(quote.UpperLimit)
	if err != nil {
		return "", fmt.Errorf("PRICE_LIMIT_UNAVAILABLE")
	}
	lower, err := tradingPositive(quote.LowerLimit)
	if err != nil || upper.Cmp(lower) <= 0 {
		return "", fmt.Errorf("PRICE_LIMIT_UNAVAILABLE")
	}
	if price.Cmp(upper) > 0 || price.Cmp(lower) < 0 {
		return "", fmt.Errorf("PRICE_OUTSIDE_DAILY_LIMIT")
	}
	tick := "0.01"
	precision := 2
	if quote.AssetType == "ETF" {
		tick = "0.001"
		precision = 3
	}
	if quote.PriceTick != "" {
		t, err := tradingPositive(quote.PriceTick)
		want, _ := tradingPositive(tick)
		if err != nil || t.Cmp(want) != 0 {
			return "", fmt.Errorf("UNSUPPORTED_PRICE_TICK")
		}
	}
	t, _ := tradingPositive(tick)
	units := new(big.Rat).Quo(price, t)
	if !units.IsInt() {
		return "", fmt.Errorf("PRICE_TICK_MISMATCH")
	}
	return price.FloatString(precision), nil
}

func TradingExitLevels(entry string, stopLoss, takeProfit float64) (string, string, error) {
	price, err := tradingPositive(entry)
	if err != nil {
		return "", "", err
	}
	if stopLoss <= 0 || stopLoss >= 1 || takeProfit <= 0 {
		return "", "", fmt.Errorf("invalid exit ratios")
	}
	stop, _ := new(big.Rat).SetString(fmt.Sprintf("%.8f", 1-stopLoss))
	take, _ := new(big.Rat).SetString(fmt.Sprintf("%.8f", 1+takeProfit))
	return new(big.Rat).Mul(price, stop).FloatString(6), new(big.Rat).Mul(price, take).FloatString(6), nil
}

// ExitReason is pure; the caller records T+1 pending when sellable is zero.
func TradingExitReason(price, stopLoss, takeProfit string, holdingDays, maxDays int) (string, error) {
	p, err := tradingPositive(price)
	if err != nil {
		return "", err
	}
	s, err := tradingPositive(stopLoss)
	if err != nil {
		return "", err
	}
	t, err := tradingPositive(takeProfit)
	if err != nil {
		return "", err
	}
	if p.Cmp(s) <= 0 {
		return "STOP_LOSS", nil
	}
	if p.Cmp(t) >= 0 {
		return "TAKE_PROFIT", nil
	}
	if maxDays > 0 && holdingDays >= maxDays {
		return "MAX_HOLDING_DAYS", nil
	}
	return "", nil
}
