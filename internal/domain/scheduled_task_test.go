package domain

import "testing"

func TestScheduledTaskTypeValuesAreStable(t *testing.T) {
	if ScheduledTaskTypeStockDailyPreviousDay != 1 {
		t.Fatalf("stock daily task type changed: %d", ScheduledTaskTypeStockDailyPreviousDay)
	}
	if ScheduledTaskTypeEvaluationRecent != 2 {
		t.Fatalf("evaluation task type changed: %d", ScheduledTaskTypeEvaluationRecent)
	}
	if ScheduledTaskTypeOpenListDocuments != 3 {
		t.Fatalf("OpenList document task type changed: %d", ScheduledTaskTypeOpenListDocuments)
	}
	if ScheduledTaskTypeSecurityMasterRefresh != 4 {
		t.Fatalf("security master refresh task type changed: %d", ScheduledTaskTypeSecurityMasterRefresh)
	}
	if !ScheduledTaskTypeStockDailyPreviousDay.Valid() || !ScheduledTaskTypeEvaluationRecent.Valid() || !ScheduledTaskTypeOpenListDocuments.Valid() || !ScheduledTaskTypeSecurityMasterRefresh.Valid() {
		t.Fatal("known scheduled task types must be valid")
	}
	if ScheduledTaskType(99).Valid() {
		t.Fatal("unknown scheduled task type must be invalid")
	}
}
