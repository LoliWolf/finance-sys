package service

import (
	"testing"

	"finance-sys/internal/config"

	"github.com/stretchr/testify/require"
)

func TestNormalizeRecommendationEvaluationRequestUsesSafeDefaults(t *testing.T) {
	cfg := config.RecommendationPerformanceConfig{Windows: []int{90, 5, 30, 10}}

	params, err := normalizeRecommendationEvaluationRequest(RecommendationEvaluationRequest{}, cfg)
	require.NoError(t, err)
	require.True(t, params.OnlyActive)
	require.Equal(t, []int{5, 10, 30, 90}, params.Windows)
}

func TestNormalizeRecommendationEvaluationRequestHonorsExplicitInactiveAndNormalizesTargets(t *testing.T) {
	onlyActive := false
	cfg := config.RecommendationPerformanceConfig{Windows: []int{5, 10, 30, 90}}
	request := RecommendationEvaluationRequest{
		DateFrom:   " 2026-01-01 ",
		DateTo:     " 2026-06-30 ",
		BloggerIDs: []int64{7, 7, -1, 0, 8},
		Symbols:    []string{" 600519 ", "000001.SZ", "600519"},
		EventIDs:   []int64{9, 9, 10},
		Windows:    []int{30, 5, 30},
		OnlyActive: &onlyActive,
	}

	params, err := normalizeRecommendationEvaluationRequest(request, cfg)
	require.NoError(t, err)
	require.False(t, params.OnlyActive)
	require.Equal(t, []int{5, 30}, params.Windows)
	require.Equal(t, []int64{7, 8}, params.BloggerIDs)
	require.Equal(t, []int64{9, 10}, params.EventIDs)
	require.Equal(t, []string{"600519", "600519.SH", "600519.SZ", "600519.BJ", "000001.SZ"}, params.Symbols)
}

func TestNormalizeRecommendationEvaluationRequestRejectsInvalidDateAndWindow(t *testing.T) {
	cfg := config.RecommendationPerformanceConfig{Windows: []int{5, 10, 30, 90}}

	_, err := normalizeRecommendationEvaluationRequest(RecommendationEvaluationRequest{DateFrom: "2026/01/01"}, cfg)
	require.ErrorContains(t, err, "invalid date_from")

	_, err = normalizeRecommendationEvaluationRequest(RecommendationEvaluationRequest{Windows: []int{20}}, cfg)
	require.ErrorContains(t, err, "window 20 is not configured")
}

func TestEvaluationTSCodesUsesMarketAndStableFallbackOrder(t *testing.T) {
	require.Equal(t, []string{"600519.SH"}, evaluationTSCodes("600519", "SSE"))
	require.Equal(t, []string{"000001.SZ"}, evaluationTSCodes("000001.sz", ""))
	require.Equal(t, []string{"430047.SH", "430047.SZ", "430047.BJ"}, evaluationTSCodes("430047", ""))
	require.Nil(t, evaluationTSCodes(" ", "SH"))
}
