package service

import (
	"testing"
	"time"

	"finance-sys/internal/domain/db_model"
	tradingdomain "finance-sys/internal/trading/domain"
	"github.com/stretchr/testify/require"
)

func TestBuildCommissionReconciliationPlanAttributesVerifiedAccountFees(t *testing.T) {
	now := time.Now()
	account := tradingdomain.AccountSnapshot{
		SnapshotVersion: "snapshot-1", AccountID: "sim-1", CommissionDataStatus: "REPORTED",
		CumulativeTrade: "1814.000034", CumulativeCommission: "10.000000",
		LastTrade: "896.000004", LastCommission: "5.000000",
	}
	fills := []db_model.TradingFill{
		{ID: 1, TradingOrderID: 1, ExecID: "buy", Amount: "918.000000", Commission: "0", CommissionStatus: "PENDING", TradedAt: now.Add(-time.Hour), RawPayloadJSON: []byte(`{"cost":918.000030517578}`)},
		{ID: 3, TradingOrderID: 2, ExecID: "sell", Amount: "896.000000", Commission: "0", CommissionStatus: "PENDING", TradedAt: now, RawPayloadJSON: []byte(`{"cost":896.0000038146973}`)},
	}

	plan := buildCommissionReconciliationPlan(account, fills)

	require.Empty(t, plan.MismatchCode)
	require.Equal(t, "ACCOUNT_LAST_AND_CUMULATIVE", plan.Method)
	require.Equal(t, "1814.000034", plan.LocalTradeTotal)
	require.Equal(t, "10.000000", plan.AttributedTotal)
	require.Equal(t, 2, plan.VerifiedFillCount)
	require.Zero(t, plan.UnavailableFillCount)
	require.Len(t, plan.Updates, 2)
	updates := map[int64]commissionAttribution{}
	for _, update := range plan.Updates {
		updates[update.FillID] = update
	}
	require.Equal(t, "5.000000", updates[3].Commission)
	require.Equal(t, "ACCOUNT_LAST_TRANSACTION", updates[3].Source)
	require.Equal(t, "5.000000", updates[1].Commission)
	require.Equal(t, "ACCOUNT_CUMULATIVE_RESIDUAL", updates[1].Source)
}

func TestBuildCommissionReconciliationPlanDoesNotGuessWithoutCoverage(t *testing.T) {
	account := tradingdomain.AccountSnapshot{
		SnapshotVersion: "snapshot-2", AccountID: "sim-1", CommissionDataStatus: "REPORTED",
		CumulativeTrade: "2000", CumulativeCommission: "10", LastTrade: "896", LastCommission: "5",
	}
	fills := []db_model.TradingFill{
		{ID: 1, TradingOrderID: 1, ExecID: "sell", Amount: "896", Commission: "0", CommissionStatus: "PENDING", RawPayloadJSON: []byte(`{"cost":896}`)},
	}

	plan := buildCommissionReconciliationPlan(account, fills)

	require.Equal(t, "ACCOUNT_TRADE_COVERAGE_MISMATCH", plan.MismatchCode)
	require.Equal(t, "0.000000", plan.AttributedTotal)
	require.Equal(t, 1, plan.UnavailableFillCount)
	require.Len(t, plan.Updates, 1)
	require.Equal(t, "UNAVAILABLE", plan.Updates[0].Status)
}

func TestReportedCommissionState(t *testing.T) {
	require.Equal(t, []string{"VERIFIED", "PROVIDER_EXECUTION"}, pair(reportedCommissionState("0.01")))
	require.Equal(t, []string{"PENDING", ""}, pair(reportedCommissionState("0")))
}

func pair(first, second string) []string { return []string{first, second} }
