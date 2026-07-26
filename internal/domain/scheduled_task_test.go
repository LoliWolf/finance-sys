package domain

import "testing"

func TestScheduledTaskTypeValuesAreStable(t *testing.T) {
	if ScheduledTaskTypeStockDailyPreviousDay != 1 {
		t.Fatalf("stock daily task type changed: %d", ScheduledTaskTypeStockDailyPreviousDay)
	}
	if ScheduledTaskTypeEvaluationRecent != 2 {
		t.Fatalf("evaluation task type changed: %d", ScheduledTaskTypeEvaluationRecent)
	}
	if !ScheduledTaskTypeStockDailyPreviousDay.Valid() || !ScheduledTaskTypeEvaluationRecent.Valid() {
		t.Fatal("known scheduled task types must be valid")
	}
	if ScheduledTaskType(99).Valid() {
		t.Fatal("unknown scheduled task type must be invalid")
	}
}
