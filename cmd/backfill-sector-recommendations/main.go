package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"finance-sys/internal/bootstrap"
	"finance-sys/internal/config"

	"gorm.io/gorm"
)

type options struct {
	apply           bool
	allowProduction bool
	concurrency     int
	documentIDs     string
	maxAttempts     int
	timeout         time.Duration
}

type documentRow struct {
	ID             int64
	Title          string
	CreatedAt      time.Time
	ArticleDate    *time.Time
	ExistingDate   *time.Time
	CandidateDate  *time.Time
	LatestParseRun int64
}

func main() {
	opts := parseFlags()
	if !opts.apply {
		fatal(errors.New("-apply is required because this command rewrites direct sector recommendation facts"))
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv(config.FinanceSysEnvironmentVariable)), "PROD") && !opts.allowProduction {
		fatal(errors.New("-allow-production is required when FINANCE_SYS_ENV=PROD"))
	}
	if err := bootstrap.LoadNacosServerAddressFromFiles("bootstrap_go122.env", "bootstrap_go122.env.example"); err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()
	app, err := bootstrap.Build(ctx)
	if err != nil {
		fatal(err)
	}
	defer app.Close()

	rows, err := loadDocuments(ctx, app.DB)
	if err != nil {
		fatal(err)
	}
	rows, err = filterDocuments(rows, opts.documentIDs)
	if err != nil {
		fatal(err)
	}
	jobs := make(chan documentRow)
	var processed atomic.Int64
	var sectorFacts atomic.Int64
	var skipped atomic.Int64
	var failuresMu sync.Mutex
	var failures []string
	var wg sync.WaitGroup
	workers := opts.concurrency
	if workers <= 0 {
		workers = 1
	}
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for row := range jobs {
				if row.LatestParseRun == 0 {
					skipped.Add(1)
					continue
				}
				count, err := backfillWithRetry(ctx, opts.maxAttempts, func() (int, error) {
					return app.DocumentService.BackfillSectorRecommendationsFromLatestParseRun(ctx, row.ID, recommendationDate(row))
				})
				if err != nil {
					failuresMu.Lock()
					failures = append(failures, fmt.Sprintf("document=%d title=%q: %v", row.ID, row.Title, err))
					failuresMu.Unlock()
					continue
				}
				processed.Add(1)
				sectorFacts.Add(int64(count))
			}
		}()
	}
	for _, row := range rows {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			fatal(ctx.Err())
		case jobs <- row:
		}
	}
	close(jobs)
	wg.Wait()
	if len(failures) > 0 {
		limit := len(failures)
		if limit > 20 {
			limit = 20
		}
		failedDocumentIDs := make([]int64, 0, len(failures))
		for _, failure := range failures {
			var documentID int64
			if _, scanErr := fmt.Sscanf(failure, "document=%d", &documentID); scanErr == nil {
				failedDocumentIDs = append(failedDocumentIDs, documentID)
			}
		}
		sort.Slice(failedDocumentIDs, func(i, j int) bool { return failedDocumentIDs[i] < failedDocumentIDs[j] })
		fatal(fmt.Errorf("sector recommendation backfill failed for %d documents; retry with -document-ids=%s; first failures:\n%s",
			len(failures), formatDocumentIDs(failedDocumentIDs), strings.Join(failures[:limit], "\n")))
	}
	fmt.Printf("sector_recommendation_backfill_completed documents=%d processed=%d skipped=%d sector_facts=%d\n", len(rows), processed.Load(), skipped.Load(), sectorFacts.Load())
}

func parseFlags() options {
	var opts options
	flag.BoolVar(&opts.apply, "apply", false, "apply the backfill")
	flag.BoolVar(&opts.allowProduction, "allow-production", false, "allow production writes")
	flag.IntVar(&opts.concurrency, "concurrency", 10, "document analysis concurrency")
	flag.StringVar(&opts.documentIDs, "document-ids", "", "optional comma-separated document IDs to backfill")
	flag.IntVar(&opts.maxAttempts, "max-attempts", 3, "maximum attempts for each document")
	flag.DurationVar(&opts.timeout, "timeout", 12*time.Hour, "overall timeout")
	flag.Parse()
	if opts.maxAttempts <= 0 {
		fatal(errors.New("-max-attempts must be greater than zero"))
	}
	return opts
}

func filterDocuments(rows []documentRow, rawIDs string) ([]documentRow, error) {
	rawIDs = strings.TrimSpace(rawIDs)
	if rawIDs == "" {
		return rows, nil
	}
	wanted := make(map[int64]struct{})
	for _, part := range strings.Split(rawIDs, ",") {
		id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("invalid document id %q", strings.TrimSpace(part))
		}
		wanted[id] = struct{}{}
	}
	selected := make([]documentRow, 0, len(wanted))
	for _, row := range rows {
		if _, ok := wanted[row.ID]; ok {
			selected = append(selected, row)
			delete(wanted, row.ID)
		}
	}
	if len(wanted) > 0 {
		missing := make([]int64, 0, len(wanted))
		for id := range wanted {
			missing = append(missing, id)
		}
		sort.Slice(missing, func(i, j int) bool { return missing[i] < missing[j] })
		return nil, fmt.Errorf("documents not found or not eligible: %s", formatDocumentIDs(missing))
	}
	return selected, nil
}

func backfillWithRetry(ctx context.Context, maxAttempts int, backfill func() (int, error)) (int, error) {
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		count, err := backfill()
		if err == nil {
			return count, nil
		}
		lastErr = err
		if ctx.Err() != nil || attempt == maxAttempts || !retryableSectorBackfillError(err) {
			break
		}
		timer := time.NewTimer(time.Duration(attempt) * 500 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return 0, ctx.Err()
		case <-timer.C:
		}
	}
	return 0, lastErr
}

func retryableSectorBackfillError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return !strings.Contains(message, "moderation_blocked") &&
		!strings.Contains(message, "smart moderation blocked by")
}

func formatDocumentIDs(ids []int64) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, strconv.FormatInt(id, 10))
	}
	return strings.Join(parts, ",")
}

func loadDocuments(ctx context.Context, db *gorm.DB) ([]documentRow, error) {
	var rows []documentRow
	err := db.WithContext(ctx).Raw(`
SELECT d.id, d.title, d.created_at,
       oi.article_date,
       re.recommend_date AS existing_date,
       cp.trade_date AS candidate_date,
       COALESCE(pr.id, 0) AS latest_parse_run
FROM documents d
LEFT JOIN (
  SELECT document_id, MIN(article_date) AS article_date
  FROM external_document_ingestions
  WHERE document_id IS NOT NULL
  GROUP BY document_id
) oi ON oi.document_id = d.id
LEFT JOIN (
  SELECT source_document_id, MIN(recommend_date) AS recommend_date
  FROM recommendation_events
  GROUP BY source_document_id
) re ON re.source_document_id = d.id
LEFT JOIN (
  SELECT document_id, MIN(trade_date) AS trade_date
  FROM trade_candidate_plans
  GROUP BY document_id
) cp ON cp.document_id = d.id
LEFT JOIN parse_runs pr ON pr.id = (
  SELECT pr2.id FROM parse_runs pr2
  WHERE pr2.document_id = d.id AND pr2.status = 'PARSED'
  ORDER BY pr2.created_at DESC, pr2.id DESC LIMIT 1
)
WHERE d.status IN ('PLANNED', 'INVALID', 'FAILED')
ORDER BY d.id ASC`).Scan(&rows).Error
	return rows, err
}

func recommendationDate(row documentRow) time.Time {
	for _, value := range []*time.Time{row.ArticleDate, row.ExistingDate, row.CandidateDate} {
		if value != nil && !value.IsZero() {
			return dateOnly(*value)
		}
	}
	parts := strings.SplitN(row.Title, "|", 3)
	if len(parts) == 3 && parts[0] == "M9" {
		if parsed, err := time.Parse("20060102", parts[1]); err == nil {
			return parsed
		}
	}
	return dateOnly(row.CreatedAt)
}

func dateOnly(value time.Time) time.Time {
	year, month, day := value.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func fatal(err error) {
	_, _ = fmt.Fprintln(os.Stderr, "backfill sector recommendations:", err)
	os.Exit(1)
}
