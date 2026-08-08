package dal

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStockDailyQuoteUpsertAssignmentsIncludesSectorType(t *testing.T) {
	assignments := stockDailyQuoteUpsertAssignments()

	require.Contains(t, assignments, "sector_type")
}
