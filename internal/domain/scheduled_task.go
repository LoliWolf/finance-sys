package domain

import "strconv"

// ScheduledTaskType is persisted as its numeric value in scheduled_task_runs.
// Existing values must never be renumbered; append new task types instead.
type ScheduledTaskType uint16

const (
	ScheduledTaskTypeStockDailyPreviousDay ScheduledTaskType = 1
	ScheduledTaskTypeEvaluationRecent      ScheduledTaskType = 2
)

func (t ScheduledTaskType) Valid() bool {
	switch t {
	case ScheduledTaskTypeStockDailyPreviousDay, ScheduledTaskTypeEvaluationRecent:
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
	default:
		return "UNKNOWN_" + strconv.FormatUint(uint64(t), 10)
	}
}
