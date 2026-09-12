package rules

import (
	"fmt"
	"math"
	"math/big"
	"strconv"

	"finance-sys/internal/config"
	"finance-sys/internal/trading/instrument"
)

type TradingSizingInput struct {
	Side, Price, NAV, AvailableCash, RequestedRatio, CurrentTotalRatio, CurrentSymbolRatio string
	Available                                                                              int64
	ProposedVolume                                                                         *int64
	Unit                                                                                   instrument.TradingUnitRule
	Risk                                                                                   config.TradingRiskConfig
}

func TradingVolume(in TradingSizingInput) (int64, error) {
	if in.Side == "SELL" {
		volume := in.Available
		if in.ProposedVolume != nil && *in.ProposedVolume < volume {
			volume = *in.ProposedVolume
		}
		if volume <= 0 {
			return 0, fmt.Errorf("sell volume must be positive")
		}
		return instrument.RoundSellVolume(volume, in.Available, in.Unit)
	}
	if in.Side != "BUY" {
		return 0, fmt.Errorf("unsupported side")
	}
	for _, v := range []float64{in.Risk.MaxSymbolPositionRatio, in.Risk.MaxTotalPositionRatio, in.Risk.MaxSingleOrderRatio, in.Risk.MinCashReserveRatio} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1 {
			return 0, fmt.Errorf("invalid risk ratio")
		}
	}
	nav, err := tradingPositive(in.NAV)
	if err != nil {
		return 0, err
	}
	cash, err := tradingPositive(in.AvailableCash)
	if err != nil {
		return 0, err
	}
	price, err := tradingPositive(in.Price)
	if err != nil {
		return 0, err
	}
	requested, err := tradingPositive(in.RequestedRatio)
	if err != nil {
		return 0, err
	}
	total, ok := new(big.Rat).SetString(in.CurrentTotalRatio)
	if !ok || total.Sign() < 0 {
		return 0, fmt.Errorf("invalid total ratio")
	}
	symbol, ok := new(big.Rat).SetString(in.CurrentSymbolRatio)
	if !ok || symbol.Sign() < 0 {
		return 0, fmt.Errorf("invalid symbol ratio")
	}
	fraction := func(v float64) *big.Rat { r, _ := new(big.Rat).SetString(strconv.FormatFloat(v, 'f', 8, 64)); return r }
	minimum := func(values ...*big.Rat) *big.Rat {
		v := new(big.Rat).Set(values[0])
		for _, r := range values[1:] {
			if r.Cmp(v) < 0 {
				v.Set(r)
			}
		}
		return v
	}
	effective := minimum(requested, fraction(in.Risk.MaxSingleOrderRatio), new(big.Rat).Sub(fraction(in.Risk.MaxSymbolPositionRatio), symbol), new(big.Rat).Sub(fraction(in.Risk.MaxTotalPositionRatio), total))
	if effective.Sign() <= 0 {
		return 0, fmt.Errorf("position budget exhausted")
	}
	budget := minimum(new(big.Rat).Mul(nav, effective), new(big.Rat).Sub(cash, new(big.Rat).Mul(nav, fraction(in.Risk.MinCashReserveRatio))))
	if budget.Sign() <= 0 {
		return 0, fmt.Errorf("cash reserve leaves no budget")
	}
	raw := new(big.Rat).Quo(budget, price)
	units := new(big.Int).Quo(raw.Num(), raw.Denom())
	if !units.IsInt64() {
		return 0, fmt.Errorf("volume overflows int64")
	}
	volume := units.Int64()
	if in.ProposedVolume != nil {
		v, e := instrument.RoundBuyVolume(*in.ProposedVolume, in.Unit)
		if e != nil || v != *in.ProposedVolume {
			return 0, fmt.Errorf("proposed volume violates board unit")
		}
		if v < volume {
			volume = v
		}
	}
	return instrument.RoundBuyVolume(volume, in.Unit)
}
