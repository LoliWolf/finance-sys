package service_test

import (
	"context"
	"os"
	"testing"
	"time"

	"finance-sys/internal/bootstrap"
	"finance-sys/internal/service"

	"github.com/stretchr/testify/require"
)

const runRealRecentEvaluationBackfillEnv = "FINANCE_SYS_RUN_REAL_RECENT_EVALUATION_BACKFILL"

func TestRealForceRebuildRecent90DaysRecommendationEvaluation(t *testing.T) {
	if os.Getenv(runRealRecentEvaluationBackfillEnv) != "1" {
		t.Skipf("set %s=1 to run the real DB recommendation evaluation backfill", runRealRecentEvaluationBackfillEnv)
	}
	if testing.Short() {
		t.Skip("real DB recommendation evaluation backfill is disabled in short mode")
	}

	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	dateTo := dateOnlyInLocation(time.Now(), location)
	dateFrom := dateTo.AddDate(0, 0, -90)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Hour)
	defer cancel()
	app, err := bootstrap.Build(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, app.Close())
	})

	onlyActive := false
	response, err := app.EvaluationService.CreateRun(ctx, service.RecommendationEvaluationRequest{
		DateFrom:     dateFrom.Format(time.DateOnly),
		DateTo:       dateTo.Format(time.DateOnly),
		ForceRebuild: true,
		OnlyActive:   &onlyActive,
	})
	require.NoError(t, err)
	t.Logf("force rebuild recommendation evaluation from %s to %s, run_id=%d", dateFrom.Format(time.DateOnly), dateTo.Format(time.DateOnly), response.RunID)
	require.NoError(t, app.EvaluationService.ExecuteRun(ctx, response.RunID))

	run, err := app.EvaluationService.GetRun(ctx, response.RunID)
	require.NoError(t, err)
	require.Contains(t, []string{
		service.RecommendationEvaluationRunStatusSucceeded,
		service.RecommendationEvaluationRunStatusPartialFailed,
	}, run.Status)
	t.Logf("evaluation completed: status=%s target_events=%d evaluated_events=%d metrics=%d pending=%d incomplete=%d failed=%d",
		run.Status,
		run.TargetEventCount,
		run.EvaluatedEventCount,
		run.WindowMetricCount,
		run.PendingCount,
		run.IncompleteCount,
		run.FailedCount,
	)
}
