package stats_test

import (
	"context"
	"os"
	"testing"
	"time"

	"finance-sys/internal/bootstrap"
	"finance-sys/internal/stats"

	"github.com/stretchr/testify/require"
)

const runRealDocumentReportEnv = "FINANCE_SYS_RUN_REAL_DOCUMENT_REPORT"

func TestRealDocumentReportReadModel(t *testing.T) {
	if os.Getenv(runRealDocumentReportEnv) != "1" {
		t.Skipf("set %s=1 to run the real test-database document report check", runRealDocumentReportEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	app, err := bootstrap.Build(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, app.Close()) })

	list, err := app.StatsService.DocumentReports(ctx, stats.DocumentReportListFilter{Limit: 100})
	require.NoError(t, err)
	require.NotEmpty(t, list.Items)

	var documentID int64
	for _, item := range list.Items {
		if item.RecommendationCount > 0 {
			documentID = item.DocumentID
			break
		}
	}
	require.NotZero(t, documentID, "latest 100 test documents should include one recommendation-bearing document")

	report, err := app.StatsService.DocumentReport(ctx, documentID)
	require.NoError(t, err)
	require.Equal(t, documentID, report.Document.DocumentID)
	require.Equal(t, report.Summary.RecommendationCount, len(report.Recommendations))
	require.Len(t, report.Summary.Windows, len(report.Windows))
}
