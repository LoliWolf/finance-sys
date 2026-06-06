package httpapi

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeRecommendationLimit(t *testing.T) {
	limit, err := normalizeRecommendationLimit("")
	require.NoError(t, err)
	require.Equal(t, 100, limit)

	limit, err = normalizeRecommendationLimit("600")
	require.NoError(t, err)
	require.Equal(t, 500, limit)

	_, err = normalizeRecommendationLimit("bad")
	require.Error(t, err)
}

func TestValidateRecommendationFilters(t *testing.T) {
	require.NoError(t, validateRecommendationDirection(""))
	require.NoError(t, validateRecommendationDirection("LONG"))
	require.Error(t, validateRecommendationDirection("BUY"))

	require.NoError(t, validateRecommendationStatus(""))
	require.NoError(t, validateRecommendationStatus("ACTIVE"))
	require.Error(t, validateRecommendationStatus("READY"))
}
