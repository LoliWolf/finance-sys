package domain

import "testing"

func TestExternalDocumentEnumValuesAreStable(t *testing.T) {
	if ExternalDocumentSourceTypeOpenList != 1 {
		t.Fatalf("OpenList source type changed: %d", ExternalDocumentSourceTypeOpenList)
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
