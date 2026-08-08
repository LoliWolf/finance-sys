package main

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFilterDocumentsSelectsRequestedIDsInSourceOrder(t *testing.T) {
	rows := []documentRow{{ID: 3}, {ID: 5}, {ID: 8}}

	selected, err := filterDocuments(rows, "8,3")

	require.NoError(t, err)
	require.Equal(t, []documentRow{{ID: 3}, {ID: 8}}, selected)
}

func TestFilterDocumentsRejectsMissingID(t *testing.T) {
	_, err := filterDocuments([]documentRow{{ID: 3}}, "3,4")

	require.EqualError(t, err, "documents not found or not eligible: 4")
}

func TestBackfillWithRetryReturnsSuccessfulAttempt(t *testing.T) {
	attempts := 0
	count, err := backfillWithRetry(context.Background(), 3, func() (int, error) {
		attempts++
		if attempts < 3 {
			return 0, errors.New("temporary failure")
		}
		return 2, nil
	})

	require.NoError(t, err)
	require.Equal(t, 2, count)
	require.Equal(t, 3, attempts)
}

func TestBackfillWithRetryStopsOnModerationBlock(t *testing.T) {
	attempts := 0
	_, err := backfillWithRetry(context.Background(), 3, func() (int, error) {
		attempts++
		return 0, errors.New("llm http 500: MODERATION_BLOCKED")
	})

	require.EqualError(t, err, "llm http 500: MODERATION_BLOCKED")
	require.Equal(t, 1, attempts)
}
