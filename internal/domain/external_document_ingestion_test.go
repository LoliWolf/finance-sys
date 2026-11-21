package domain

import "testing"

func TestExternalDocumentEnumValuesAreStable(t *testing.T) {
	if ExternalDocumentSourceTypeOpenList != 1 {
		t.Fatalf("OpenList source type changed: %d", ExternalDocumentSourceTypeOpenList)
	}
	if ExternalDocumentSourceTypeXueqiu != 2 || ExternalDocumentSourceTypeTHS != 3 {
		t.Fatalf("wire desk source types changed: xueqiu=%d ths=%d", ExternalDocumentSourceTypeXueqiu, ExternalDocumentSourceTypeTHS)
	}
	if !ExternalDocumentSourceTypeTHS.Valid() || ExternalDocumentSourceTypeTHS.String() != "THS" || ExternalDocumentSourceType(4).Valid() {
		t.Fatal("THS must be the only valid source type after Xueqiu")
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
