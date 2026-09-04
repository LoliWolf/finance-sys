package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	"finance-sys/internal/dal"
	"finance-sys/internal/domain/db_model"
	tradingdomain "finance-sys/internal/trading/domain"

	"gorm.io/gorm"
)

const commissionComparisonTolerance = "0.01"

type commissionAttribution struct {
	FillID     int64
	OrderID    int64
	Commission string
	Status     string
	Source     string
	Evidence   []byte
}

type commissionReconciliationPlan struct {
	DataStatus           string
	Method               string
	ProviderTradeTotal   string
	LocalTradeTotal      string
	ProviderCommission   string
	AttributedTotal      string
	VerifiedFillCount    int
	UnavailableFillCount int
	PendingFillCount     int
	MismatchCode         string
	MismatchMessage      string
	Updates              []commissionAttribution
}

func (s *Service) reconcileCommissions(ctx context.Context, account tradingdomain.AccountSnapshot) (map[string]any, *db_model.TradingReconciliationDiff, error) {
	fills, err := dal.TradingFills.ListByAccount(ctx, s.db, account.AccountID)
	if err != nil {
		return nil, nil, err
	}
	plan := buildCommissionReconciliationPlan(account, fills)
	reconciledAt := time.Now()
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		orderIDs := make(map[int64]struct{})
		for _, fill := range fills {
			orderIDs[fill.TradingOrderID] = struct{}{}
		}
		for _, update := range plan.Updates {
			if update.OrderID != 0 {
				orderIDs[update.OrderID] = struct{}{}
			}
			values := map[string]any{
				"commission_status":        update.Status,
				"commission_source":        update.Source,
				"commission_evidence_json": update.Evidence,
			}
			if update.Status == "VERIFIED" {
				values["commission"] = update.Commission
				values["commission_reconciled_at"] = reconciledAt
			} else {
				values["commission_reconciled_at"] = nil
			}
			if updateErr := dal.TradingFills.UpdateCommission(ctx, tx, update.FillID, values); updateErr != nil {
				return updateErr
			}
		}
		for orderID := range orderIDs {
			total, sumErr := dal.TradingFills.SumCommissionByOrder(ctx, tx, orderID)
			if sumErr != nil {
				return sumErr
			}
			if updateErr := dal.TradingOrders.Update(ctx, tx, orderID, map[string]any{"filled_commission": fixedDecimal(total, 6)}); updateErr != nil {
				return updateErr
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	summary := map[string]any{
		"data_status":                 plan.DataStatus,
		"method":                      plan.Method,
		"provider_trade_total":        plan.ProviderTradeTotal,
		"local_trade_total":           plan.LocalTradeTotal,
		"provider_commission_total":   plan.ProviderCommission,
		"attributed_commission_total": plan.AttributedTotal,
		"verified_fill_count":         plan.VerifiedFillCount,
		"unavailable_fill_count":      plan.UnavailableFillCount,
		"pending_fill_count":          plan.PendingFillCount,
	}
	if plan.MismatchCode == "" {
		return summary, nil, nil
	}
	diff := &db_model.TradingReconciliationDiff{
		DiffType: "COMMISSION_MISMATCH", Severity: "P1", EntityType: "ACCOUNT", EntityKey: account.AccountID,
		FieldName: "cumulative_commission", LocalValueJSON: marshalJSON(summary),
		ProviderValueJSON: marshalJSON(map[string]any{
			"cumulative_trade":      account.CumulativeTrade,
			"cumulative_commission": account.CumulativeCommission,
			"last_trade":            account.LastTrade,
			"last_commission":       account.LastCommission,
		}),
		Resolution: "OPEN", ResolutionMessage: plan.MismatchCode + ": " + plan.MismatchMessage,
	}
	return summary, diff, nil
}

func buildCommissionReconciliationPlan(account tradingdomain.AccountSnapshot, fills []db_model.TradingFill) commissionReconciliationPlan {
	plan := commissionReconciliationPlan{
		DataStatus:         account.CommissionDataStatus,
		ProviderTradeTotal: fixedDecimal(account.CumulativeTrade, 6),
		ProviderCommission: fixedDecimal(account.CumulativeCommission, 6),
		Method:             "UNAVAILABLE",
	}
	sorted := append([]db_model.TradingFill(nil), fills...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].TradedAt.Equal(sorted[j].TradedAt) {
			return sorted[i].ID < sorted[j].ID
		}
		return sorted[i].TradedAt.Before(sorted[j].TradedAt)
	})

	localTrade := new(big.Rat)
	for _, fill := range sorted {
		localTrade.Add(localTrade, decimalRat(fillProviderTradeAmount(fill)))
	}
	plan.LocalTradeTotal = localTrade.FloatString(6)

	if account.CommissionDataStatus != "REPORTED" {
		plan.Method = "PROVIDER_FIELDS_UNAVAILABLE"
		for _, fill := range sorted {
			if fill.CommissionStatus == "VERIFIED" {
				continue
			}
			plan.Updates = append(plan.Updates, unavailableCommissionUpdate(fill, "PROVIDER_FIELDS_UNAVAILABLE", account))
		}
		return finalizeCommissionPlan(plan, sorted)
	}

	if !decimalWithin(plan.LocalTradeTotal, plan.ProviderTradeTotal, commissionComparisonTolerance) {
		plan.Method = "ACCOUNT_TRADE_COVERAGE_MISMATCH"
		plan.MismatchCode = "ACCOUNT_TRADE_COVERAGE_MISMATCH"
		plan.MismatchMessage = fmt.Sprintf("local provider trade total %s does not cover account cumulative trade %s", plan.LocalTradeTotal, plan.ProviderTradeTotal)
		for _, fill := range sorted {
			if fill.CommissionStatus == "VERIFIED" {
				continue
			}
			plan.Updates = append(plan.Updates, unavailableCommissionUpdate(fill, plan.Method, account))
		}
		return finalizeCommissionPlan(plan, sorted)
	}

	plan.Method = "ACCOUNT_LAST_AND_CUMULATIVE"
	commissions := make(map[int64]*big.Rat, len(sorted))
	statuses := make(map[int64]string, len(sorted))
	for _, fill := range sorted {
		commissions[fill.ID] = decimalRat(fill.Commission)
		statuses[fill.ID] = fill.CommissionStatus
	}

	if len(sorted) > 0 {
		latest := sorted[len(sorted)-1]
		if decimalWithin(fillProviderTradeAmount(latest), account.LastTrade, commissionComparisonTolerance) {
			lastCommission := decimalRat(account.LastCommission)
			if latest.CommissionStatus == "VERIFIED" && commissions[latest.ID].Cmp(lastCommission) != 0 {
				plan.MismatchCode = "LAST_COMMISSION_MISMATCH"
				plan.MismatchMessage = fmt.Sprintf("verified latest fill commission %s differs from provider last commission %s", latest.Commission, account.LastCommission)
				return finalizeCommissionPlan(plan, sorted)
			}
			if latest.CommissionStatus != "VERIFIED" {
				commissions[latest.ID] = lastCommission
				statuses[latest.ID] = "VERIFIED"
				plan.Updates = append(plan.Updates, verifiedCommissionUpdate(latest, lastCommission.FloatString(6), "ACCOUNT_LAST_TRANSACTION", account))
			}
		}
	}

	known := new(big.Rat)
	unresolved := make([]db_model.TradingFill, 0)
	for _, fill := range sorted {
		if statuses[fill.ID] == "VERIFIED" {
			known.Add(known, commissions[fill.ID])
		} else {
			unresolved = append(unresolved, fill)
		}
	}
	providerTotal := decimalRat(account.CumulativeCommission)
	residual := new(big.Rat).Sub(providerTotal, known)
	if residual.Sign() < 0 {
		plan.MismatchCode = "COMMISSION_TOTAL_BELOW_VERIFIED"
		plan.MismatchMessage = fmt.Sprintf("provider cumulative commission %s is below already verified commission %s", account.CumulativeCommission, known.FloatString(6))
		return finalizeCommissionPlan(plan, sorted)
	}

	switch {
	case len(unresolved) == 0 && residual.Sign() != 0:
		plan.MismatchCode = "COMMISSION_TOTAL_UNATTRIBUTED"
		plan.MismatchMessage = fmt.Sprintf("provider cumulative commission has residual %s with no unresolved fills", residual.FloatString(6))
	case len(unresolved) == 1:
		fill := unresolved[0]
		commissions[fill.ID] = residual
		statuses[fill.ID] = "VERIFIED"
		plan.Updates = append(plan.Updates, verifiedCommissionUpdate(fill, residual.FloatString(6), "ACCOUNT_CUMULATIVE_RESIDUAL", account))
	case len(unresolved) > 1 && residual.Sign() == 0:
		for _, fill := range unresolved {
			commissions[fill.ID] = new(big.Rat)
			statuses[fill.ID] = "VERIFIED"
			plan.Updates = append(plan.Updates, verifiedCommissionUpdate(fill, "0.000000", "ACCOUNT_CUMULATIVE_EXHAUSTED", account))
		}
	case len(unresolved) > 1:
		plan.Method = "ACCOUNT_TOTAL_ONLY"
		for _, fill := range unresolved {
			statuses[fill.ID] = "UNAVAILABLE"
			plan.Updates = append(plan.Updates, unavailableCommissionUpdate(fill, "MULTIPLE_UNRESOLVED_FILLS", account))
		}
	}

	return finalizeCommissionPlanWithState(plan, sorted, commissions, statuses)
}

func finalizeCommissionPlan(plan commissionReconciliationPlan, fills []db_model.TradingFill) commissionReconciliationPlan {
	commissions := make(map[int64]*big.Rat, len(fills))
	statuses := make(map[int64]string, len(fills))
	for _, fill := range fills {
		commissions[fill.ID] = decimalRat(fill.Commission)
		statuses[fill.ID] = fill.CommissionStatus
	}
	for _, update := range plan.Updates {
		statuses[update.FillID] = update.Status
		if update.Status == "VERIFIED" {
			commissions[update.FillID] = decimalRat(update.Commission)
		}
	}
	return finalizeCommissionPlanWithState(plan, fills, commissions, statuses)
}

func finalizeCommissionPlanWithState(plan commissionReconciliationPlan, fills []db_model.TradingFill, commissions map[int64]*big.Rat, statuses map[int64]string) commissionReconciliationPlan {
	total := new(big.Rat)
	for _, fill := range fills {
		switch statuses[fill.ID] {
		case "VERIFIED":
			plan.VerifiedFillCount++
			total.Add(total, commissions[fill.ID])
		case "UNAVAILABLE":
			plan.UnavailableFillCount++
		default:
			plan.PendingFillCount++
		}
	}
	plan.AttributedTotal = total.FloatString(6)
	return plan
}

func verifiedCommissionUpdate(fill db_model.TradingFill, commission, source string, account tradingdomain.AccountSnapshot) commissionAttribution {
	return commissionAttribution{
		FillID: fill.ID, OrderID: fill.TradingOrderID, Commission: fixedDecimal(commission, 6), Status: "VERIFIED", Source: source,
		Evidence: marshalJSON(map[string]any{
			"method": source, "exec_id": fill.ExecID, "provider_trade_amount": fillProviderTradeAmount(fill),
			"account_cumulative_trade": account.CumulativeTrade, "account_cumulative_commission": account.CumulativeCommission,
			"account_last_trade": account.LastTrade, "account_last_commission": account.LastCommission,
			"account_snapshot_version": account.SnapshotVersion,
		}),
	}
}

func unavailableCommissionUpdate(fill db_model.TradingFill, reason string, account tradingdomain.AccountSnapshot) commissionAttribution {
	return commissionAttribution{
		FillID: fill.ID, OrderID: fill.TradingOrderID, Commission: fill.Commission, Status: "UNAVAILABLE", Source: "ACCOUNT_TOTAL_ONLY",
		Evidence: marshalJSON(map[string]any{
			"reason": reason, "exec_id": fill.ExecID, "provider_trade_amount": fillProviderTradeAmount(fill),
			"account_cumulative_trade": account.CumulativeTrade, "account_cumulative_commission": account.CumulativeCommission,
			"account_snapshot_version": account.SnapshotVersion,
		}),
	}
}

func fillProviderTradeAmount(fill db_model.TradingFill) string {
	decoder := json.NewDecoder(bytes.NewReader(fill.RawPayloadJSON))
	decoder.UseNumber()
	var raw map[string]any
	if err := decoder.Decode(&raw); err == nil {
		for _, key := range []string{"cost", "amount"} {
			if value, ok := raw[key]; ok {
				switch typed := value.(type) {
				case json.Number:
					if _, ok := new(big.Rat).SetString(typed.String()); ok {
						return typed.String()
					}
				case string:
					if _, ok := new(big.Rat).SetString(strings.TrimSpace(typed)); ok {
						return strings.TrimSpace(typed)
					}
				case float64:
					return strconv.FormatFloat(typed, 'f', -1, 64)
				}
			}
		}
	}
	return zeroDefault(fill.Amount)
}

func decimalWithin(left, right, tolerance string) bool {
	diff := new(big.Rat).Sub(decimalRat(left), decimalRat(right))
	if diff.Sign() < 0 {
		diff.Neg(diff)
	}
	return diff.Cmp(decimalRat(tolerance)) <= 0
}

func decimalRat(value string) *big.Rat {
	result, ok := new(big.Rat).SetString(strings.TrimSpace(zeroDefault(value)))
	if !ok {
		return new(big.Rat)
	}
	return result
}
