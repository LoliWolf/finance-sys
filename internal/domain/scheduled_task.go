package domain

import "strconv"

// ScheduledTaskType is persisted as its numeric value in scheduled_task_runs.
// Existing values must never be renumbered; append new task types instead.
type ScheduledTaskType uint16

const (
	ScheduledTaskTypeStockDailyPreviousDay  ScheduledTaskType = 1
	ScheduledTaskTypeEvaluationRecent       ScheduledTaskType = 2
	ScheduledTaskTypeOpenListDocuments      ScheduledTaskType = 3
	ScheduledTaskTypeSecurityMasterRefresh  ScheduledTaskType = 4
	ScheduledTaskTypeTradingPreopenDecision ScheduledTaskType = 5
	ScheduledTaskTypeTradingExitMonitor     ScheduledTaskType = 6
	ScheduledTaskTypeTradingReconciliation  ScheduledTaskType = 7
	ScheduledTaskTypeTradingEODSnapshot     ScheduledTaskType = 8
	ScheduledTaskTypeTradingPreflight       ScheduledTaskType = 9
	ScheduledTaskTypeTradingCancelOpen      ScheduledTaskType = 10
)

func (t ScheduledTaskType) Valid() bool {
	switch t {
	case ScheduledTaskTypeStockDailyPreviousDay, ScheduledTaskTypeEvaluationRecent, ScheduledTaskTypeOpenListDocuments, ScheduledTaskTypeSecurityMasterRefresh,
		ScheduledTaskTypeTradingPreopenDecision, ScheduledTaskTypeTradingExitMonitor, ScheduledTaskTypeTradingReconciliation, ScheduledTaskTypeTradingEODSnapshot:
		return true
	case ScheduledTaskTypeTradingPreflight, ScheduledTaskTypeTradingCancelOpen:
		return true
	default:
		return false
	}
}

func (t ScheduledTaskType) String() string {
	switch t {
	case ScheduledTaskTypeStockDailyPreviousDay:
		return "STOCK_DAILY_PREVIOUS_DAY"
	case ScheduledTaskTypeEvaluationRecent:
		return "EVALUATION_RECENT"
	case ScheduledTaskTypeOpenListDocuments:
		return "OPENLIST_DOCUMENTS"
	case ScheduledTaskTypeSecurityMasterRefresh:
		return "SECURITY_MASTER_REFRESH"
	case ScheduledTaskTypeTradingPreopenDecision:
		return "TRADING_PREOPEN_DECISION"
	case ScheduledTaskTypeTradingExitMonitor:
		return "TRADING_EXIT_MONITOR"
	case ScheduledTaskTypeTradingReconciliation:
		return "TRADING_RECONCILIATION"
	case ScheduledTaskTypeTradingEODSnapshot:
		return "TRADING_EOD_SNAPSHOT"
	case ScheduledTaskTypeTradingPreflight:
		return "TRADING_PREFLIGHT"
	case ScheduledTaskTypeTradingCancelOpen:
		return "TRADING_CANCEL_OPEN"
	default:
		return "UNKNOWN_" + strconv.FormatUint(uint64(t), 10)
	}
}
