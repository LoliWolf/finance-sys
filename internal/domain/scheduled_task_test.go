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
	if ScheduledTaskTypeXueqiuDocuments != 11 || ScheduledTaskTypeTHSDocuments != 12 {
		t.Fatalf("wire desk task types changed: xueqiu=%d ths=%d", ScheduledTaskTypeXueqiuDocuments, ScheduledTaskTypeTHSDocuments)
	}
	if !ScheduledTaskTypeTHSDocuments.Valid() || ScheduledTaskTypeTHSDocuments.String() != "THS_DOCUMENTS" {
		t.Fatal("THS document task type must be valid")
	}
	if ScheduledTaskType(99).Valid() {
		t.Fatal("unknown scheduled task type must be invalid")
	}
}

func TestEastmoneyTaskTypeIsStable(t *testing.T) {
	if ScheduledTaskTypeEastmoneyDocuments != 13 || !ScheduledTaskTypeEastmoneyDocuments.Valid() || ScheduledTaskTypeEastmoneyDocuments.String() != "EASTMONEY_DOCUMENTS" {
		t.Fatal("East Money task type must be 13 / EASTMONEY_DOCUMENTS")
	}
}
