package service

import (
	"context"
	"fmt"
	"math/big"

	"finance-sys/internal/dal"
	"finance-sys/internal/domain/db_model"
	tradingdomain "finance-sys/internal/trading/domain"
)

func (s *Service) ledgerDiffs(ctx context.Context, account tradingdomain.AccountSnapshot, positions []tradingdomain.PositionSnapshot) ([]db_model.TradingReconciliationDiff, error) {
	base, err := dal.TradingAccountSnapshots.Earliest(ctx, s.db, account.AccountID)
	if err != nil {
		return nil, err
	}
	basePositions, err := dal.TradingPositionSnapshots.ByAccountSnapshot(ctx, s.db, base.ID)
	if err != nil {
		return nil, err
	}
	if len(basePositions) > 0 || !decimalWithin(base.MarketValue, "0", "0.01") {
		return nil, fmt.Errorf("verified flat ledger baseline unavailable")
	}
	fills, err := dal.TradingFills.ListByAccount(ctx, s.db, account.AccountID)
	if err != nil {
		return nil, err
	}
	orders, err := dal.TradingOrders.ByAccount(ctx, s.db, account.AccountID)
	if err != nil {
		return nil, err
	}
	return compareLedger(*base, fills, orders, account, positions)
}

func strictDecimal(value string) (*big.Rat, error) {
	r, ok := new(big.Rat).SetString(value)
	if !ok {
		return nil, fmt.Errorf("missing or invalid ledger decimal")
	}
	return r, nil
}

// Cash is reconstructed from the pre-trade flat snapshot and confirmed fills.
// External deposits, manual trades and corporate actions are discrepancies that
// need explicit attribution; provider cash never silently becomes the baseline.
func compareLedger(base db_model.TradingAccountSnapshot, fills []db_model.TradingFill, orders []db_model.TradingOrder, account tradingdomain.AccountSnapshot, positions []tradingdomain.PositionSnapshot) ([]db_model.TradingReconciliationDiff, error) {
	cash, err := strictDecimal(base.AvailableCash)
	if err != nil {
		return nil, err
	}
	frozen, err := strictDecimal(base.FrozenCash)
	if err != nil {
		return nil, err
	}
	cash.Add(cash, frozen)
	qty := map[string]int64{}
	byOrder := map[int64]int64{}
	fees := new(big.Rat)
	diffs := []db_model.TradingReconciliationDiff{}
	add := func(kind, key, field string, local, provider any) {
		diffs = append(diffs, db_model.TradingReconciliationDiff{DiffType: kind, Severity: "P0", EntityType: "LEDGER", EntityKey: key, FieldName: field, LocalValueJSON: marshalJSON(local), ProviderValueJSON: marshalJSON(provider), Resolution: "OPEN", ResolutionMessage: "independent local ledger differs from provider; do not auto-adopt"})
	}
	for _, f := range fills {
		if f.TradedAt.After(account.SnapshotAt) {
			continue
		}
		byOrder[f.TradingOrderID] += f.Volume
		if !f.TradedAt.After(base.SnapshotAt) {
			return nil, fmt.Errorf("baseline does not precede all known fills")
		}
		amount, e := strictDecimal(f.Amount)
		if e != nil || amount.Sign() <= 0 || f.Volume <= 0 {
			return nil, fmt.Errorf("invalid fill amount/volume")
		}
		commission, e := strictDecimal(f.Commission)
		if e != nil || commission.Sign() < 0 {
			return nil, fmt.Errorf("invalid fill commission")
		}
		if f.CommissionStatus != "VERIFIED" {
			add("FEE_UNVERIFIED", f.ExecID, "commission_status", f.CommissionStatus, "VERIFIED")
		}
		if f.Side == "BUY" {
			cash.Sub(cash, amount)
			qty[f.EastmoneySymbol] += f.Volume
		} else if f.Side == "SELL" {
			cash.Add(cash, amount)
			qty[f.EastmoneySymbol] -= f.Volume
		} else {
			return nil, fmt.Errorf("unsupported ledger side")
		}
		cash.Sub(cash, commission)
		fees.Add(fees, commission)
	}
	actual, err := strictDecimal(account.AvailableCash)
	if err != nil {
		return nil, err
	}
	actualFrozen, err := strictDecimal(account.FrozenCash)
	if err != nil {
		return nil, err
	}
	actual.Add(actual, actualFrozen)
	if !decimalWithin(cash.FloatString(6), actual.FloatString(6), "0.02") {
		add("CASH_DIFF", account.AccountID, "total_cash", cash.FloatString(6), actual.FloatString(6))
	}
	if !decimalWithin(fees.FloatString(6), account.CumulativeCommission, "0.02") {
		add("FEE_DIFF", account.AccountID, "cumulative_commission", fees.FloatString(6), account.CumulativeCommission)
	}
	marketValue := new(big.Rat)
	seen := map[string]bool{}
	for _, p := range positions {
		if p.Volume < 0 || p.AvailableVolume < 0 || p.AvailableVolume > p.Volume {
			return nil, fmt.Errorf("invalid provider position")
		}
		if p.Volume != qty[p.EastmoneySymbol] {
			add("POSITION_DIFF", p.EastmoneySymbol, "volume", qty[p.EastmoneySymbol], p.Volume)
		}
		seen[p.EastmoneySymbol] = true
		value, e := strictDecimal(p.MarketValue)
		if e != nil {
			return nil, e
		}
		marketValue.Add(marketValue, value)
	}
	for symbol, volume := range qty {
		if !seen[symbol] && volume != 0 {
			add("POSITION_DIFF", symbol, "volume", volume, 0)
		}
	}
	if !decimalWithin(marketValue.FloatString(6), account.MarketValue, "0.02") {
		add("MARKET_VALUE_DIFF", account.AccountID, "market_value", marketValue.FloatString(6), account.MarketValue)
	}
	nav := new(big.Rat).Add(cash, marketValue)
	if !decimalWithin(nav.FloatString(6), account.NAV, "0.02") {
		add("NAV_DIFF", account.AccountID, "nav", nav.FloatString(6), account.NAV)
	}
	for _, o := range orders {
		if o.Status != "DRY_RUN" && o.FilledVolume != byOrder[o.ID] {
			add("MISSING_EXECUTION", o.ClientOrderID, "filled_volume", byOrder[o.ID], o.FilledVolume)
		}
	}
	return diffs, nil
}
