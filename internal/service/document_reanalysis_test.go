package service

import (
	"context"
	"testing"
	"time"

	"finance-sys/internal/domain/db_model"
	"github.com/stretchr/testify/require"
)

func TestReanalysisRequiresOriginalDateBeforeAccessingDatabase(t *testing.T) {
	_, err := (&DocumentService{}).ReanalyzeParsedDocumentForTradeDate(context.Background(), 1, time.Time{})
	require.ErrorIs(t, err, ErrDocumentRetryDateRequired)
}

func TestReusableParsedRunRejectsWrongDocumentAndEmptyOrFailedInput(t *testing.T) {
	model := &db_model.ParseRun{ID: 17, DocumentID: 9, Status: "PARSED", CleanedText: "推荐原文", ChunksJSON: []byte(`[{"index":0,"text":"推荐原文"}]`), RawMetadataJSON: []byte(`{}`)}
	run, err := reusableParsedRun(model, 9)
	require.NoError(t, err)
	require.Equal(t, int64(17), run.ID)
	require.Equal(t, "推荐原文", run.CleanedText)
	run, err = reusableParsedRun(model, 8)
	require.NoError(t, err)
	require.Nil(t, run)
	model.Status = "FAILED"
	run, err = reusableParsedRun(model, 9)
	require.NoError(t, err)
	require.Nil(t, run)
	model.Status = "PARSED"
	model.CleanedText = ""
	model.ChunksJSON = []byte(`[]`)
	run, err = reusableParsedRun(model, 9)
	require.NoError(t, err)
	require.Nil(t, run)
}
