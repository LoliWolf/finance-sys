package domain

import "testing"

func TestExternalDocumentEnumValuesAreStable(t *testing.T) {
	if ExternalDocumentSourceTypeOpenList != 1 {
		t.Fatalf("OpenList source type changed: %d", ExternalDocumentSourceTypeOpenList)
	}
	if ExternalDocumentSourceTypeXueqiu != 2 || ExternalDocumentSourceTypeTHS != 3 {
		t.Fatalf("wire desk source types changed: xueqiu=%d ths=%d", ExternalDocumentSourceTypeXueqiu, ExternalDocumentSourceTypeTHS)
	}
	if !ExternalDocumentSourceTypeTHS.Valid() || ExternalDocumentSourceTypeTHS.String() != "THS" || ExternalDocumentSourceType(5).Valid() {
		t.Fatal("THS remains a valid source type")
	}
	wantStatuses := []ExternalDocumentIngestionStatus{
		ExternalDocumentIngestionStatusDiscovered,
		ExternalDocumentIngestionStatusDownloading,
		ExternalDocumentIngestionStatusIngested,
		ExternalDocumentIngestionStatusAnalyzing,
		ExternalDocumentIngestionStatusSucceeded,
		ExternalDocumentIngestionStatusFailed,
	}
	for index, status := range wantStatuses {
		want := ExternalDocumentIngestionStatus(index + 1)
		if status != want || !status.Valid() {
			t.Fatalf("external ingestion status %d = %d, valid=%v", index+1, status, status.Valid())
		}
	}
	if !ExternalDocumentIngestionStatusSucceeded.Terminal() || ExternalDocumentIngestionStatusFailed.Terminal() {
		t.Fatal("only succeeded ingestion rows should be terminal")
	}
}

func TestEastmoneySourceTypeIsStable(t *testing.T) {
	if ExternalDocumentSourceTypeEastmoney != 4 || !ExternalDocumentSourceTypeEastmoney.Valid() || ExternalDocumentSourceTypeEastmoney.String() != "EASTMONEY" {
		t.Fatal("East Money source type must be 4 / EASTMONEY")
	}
}
