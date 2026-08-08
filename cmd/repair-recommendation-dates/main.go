package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"finance-sys/internal/bootstrap"
	"finance-sys/internal/config"
	"finance-sys/internal/domain/db_model"
	"finance-sys/internal/service"

	"gorm.io/gorm"
)

const (
	openListSourceType = 1
	repairBatchSize    = 500
	m9TitlePrefix      = "M9_REAL_HISTORY|"
)

type options struct {
	apply           bool
	allowProduction bool
	skipEvaluation  bool
	backupDir       string
	timeout         time.Duration
}

type planDateUpdate struct {
	ID         int64     `json:"id"`
	DocumentID int64     `json:"document_id"`
	OldDate    time.Time `json:"old_date"`
	NewDate    time.Time `json:"new_date"`
}

type eventDateUpdate struct {
	ID         int64     `json:"id"`
	DocumentID int64     `json:"document_id"`
	OldDate    time.Time `json:"old_date"`
	NewDate    time.Time `json:"new_date"`
	OldKey     string    `json:"old_key"`
	NewKey     string    `json:"new_key"`
	DateChange bool      `json:"date_change"`
}

type repairPlan struct {
	documentDates   map[int64]time.Time
	planUpdates     []planDateUpdate
	eventUpdates    []eventDateUpdate
	evaluationIDs   []int64
	metricsToDelete int64
	dateFrom        string
	dateTo          string
}

type repairSummary struct {
	DatabaseProfile    string             `json:"database_profile"`
	Applied            bool               `json:"applied"`
	SourceDocuments    int                `json:"source_documents"`
	PlansToUpdate      int                `json:"plans_to_update"`
	EventsToUpdate     int                `json:"events_to_update"`
	EventsToReevaluate int                `json:"events_to_reevaluate"`
	MetricsToDelete    int64              `json:"metrics_to_delete"`
	DateFrom           string             `json:"date_from,omitempty"`
	DateTo             string             `json:"date_to,omitempty"`
	BackupPath         string             `json:"backup_path,omitempty"`
	EvaluationRun      *evaluationSummary `json:"evaluation_run,omitempty"`
	Acceptance         *repairAcceptance  `json:"acceptance,omitempty"`
}

type repairAcceptance struct {
	RemainingPlanMismatches  int   `json:"remaining_plan_mismatches"`
	RemainingEventMismatches int   `json:"remaining_event_mismatches"`
	MetricDateMismatches     int64 `json:"metric_date_mismatches"`
}

type evaluationSummary struct {
	RunID           int64  `json:"run_id"`
	Status          string `json:"status"`
	TargetEvents    int32  `json:"target_events"`
	EvaluatedEvents int32  `json:"evaluated_events"`
	WindowMetrics   int32  `json:"window_metrics"`
	Pending         int32  `json:"pending"`
	Incomplete      int32  `json:"incomplete"`
	Failed          int32  `json:"failed"`
}

type repairBackup struct {
	CreatedAt       time.Time         `json:"created_at"`
	DatabaseProfile string            `json:"database_profile"`
	Plans           []planDateUpdate  `json:"plans"`
	Events          []eventDateUpdate `json:"events"`
}

type openListDateRow struct {
	DocumentID  int64
	ArticleDate time.Time
	DateCount   int64
}

type m9DocumentRow struct {
	ID    int64
	Title string
}

type planRow struct {
	ID         int64
	DocumentID int64
	TradeDate  time.Time
}

type eventRow struct {
	ID               int64
	SourceDocumentID int64
	BloggerID        int64
	Symbol           string
	Direction        string
	RecommendDate    time.Time
	DedupeKey        string
	NormalizedName   string
	Institution      string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "repair recommendation dates:", err)
		os.Exit(1)
	}
}

func run() error {
	opts := parseOptions()
	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()
	if err := bootstrap.LoadNacosServerAddressFromFiles(
		"bootstrap_go122.env",
		"bootstrap_go122.env.example",
	); err != nil {
		return err
	}

	app, err := bootstrap.Build(ctx)
	if err != nil {
		return err
	}
	defer app.Close()

	profile := app.Runtime.Config().SelectedDatabaseProfile
	if opts.apply && profile == config.DatabaseProfileProduction && !opts.allowProduction {
		return errors.New("refusing production writes without --allow-production")
	}

	plan, err := buildRepairPlan(ctx, app.DB)
	if err != nil {
		return err
	}
	summary := summaryForPlan(profile, plan)
	if !opts.apply {
		return writeJSON(os.Stdout, summary)
	}

	backupPath, err := writeBackup(opts.backupDir, profile, plan)
	if err != nil {
		return err
	}
	if err := applyRepairPlan(ctx, app.DB, plan); err != nil {
		return err
	}
	summary.Applied = true
	summary.BackupPath = backupPath

	if !opts.skipEvaluation && len(plan.evaluationIDs) > 0 {
		onlyActive := false
		created, err := app.EvaluationService.CreateRun(ctx, service.RecommendationEvaluationRequest{
			EventIDs:     plan.evaluationIDs,
			ForceRebuild: true,
			OnlyActive:   &onlyActive,
		})
		if err != nil {
			return fmt.Errorf("create evaluation run: %w", err)
		}
		if err := app.EvaluationService.ExecuteRun(ctx, created.RunID); err != nil {
			return fmt.Errorf("execute evaluation run %d: %w", created.RunID, err)
		}
		runView, err := app.EvaluationService.GetRun(ctx, created.RunID)
		if err != nil {
			return fmt.Errorf("load evaluation run %d: %w", created.RunID, err)
		}
		if runView.Status != service.RecommendationEvaluationRunStatusSucceeded ||
			runView.FailedCount != 0 ||
			int(runView.TargetEventCount) != len(plan.evaluationIDs) ||
			runView.EvaluatedEventCount != runView.TargetEventCount {
			return fmt.Errorf(
				"evaluation acceptance failed: run_id=%d status=%s target=%d expected=%d evaluated=%d failed=%d",
				runView.ID,
				runView.Status,
				runView.TargetEventCount,
				len(plan.evaluationIDs),
				runView.EvaluatedEventCount,
				runView.FailedCount,
			)
		}
		summary.EvaluationRun = &evaluationSummary{
			RunID:           runView.ID,
			Status:          runView.Status,
			TargetEvents:    runView.TargetEventCount,
			EvaluatedEvents: runView.EvaluatedEventCount,
			WindowMetrics:   runView.WindowMetricCount,
			Pending:         runView.PendingCount,
			Incomplete:      runView.IncompleteCount,
			Failed:          runView.FailedCount,
		}
	}

	remaining, err := buildRepairPlan(ctx, app.DB)
	if err != nil {
		return fmt.Errorf("post-repair validation: %w", err)
	}
	metricMismatches, err := countMetricDateMismatches(ctx, app.DB, plan.evaluationIDs)
	if err != nil {
		return fmt.Errorf("validate metric dates: %w", err)
	}
	summary.Acceptance = &repairAcceptance{
		RemainingPlanMismatches:  len(remaining.planUpdates),
		RemainingEventMismatches: len(remaining.eventUpdates),
		MetricDateMismatches:     metricMismatches,
	}
	if summary.Acceptance.RemainingPlanMismatches != 0 || summary.Acceptance.RemainingEventMismatches != 0 || metricMismatches != 0 {
		return fmt.Errorf(
			"acceptance failed: remaining_plan_mismatches=%d remaining_event_mismatches=%d metric_date_mismatches=%d",
			summary.Acceptance.RemainingPlanMismatches,
			summary.Acceptance.RemainingEventMismatches,
			metricMismatches,
		)
	}
	return writeJSON(os.Stdout, summary)
}

func parseOptions() options {
	var opts options
	flag.BoolVar(&opts.apply, "apply", false, "apply the repair; default is dry-run")
	flag.BoolVar(&opts.allowProduction, "allow-production", false, "allow writes when FINANCE_SYS_ENV=PROD")
	flag.BoolVar(&opts.skipEvaluation, "skip-evaluation", false, "skip synchronous evaluation rebuild")
	flag.StringVar(&opts.backupDir, "backup-dir", filepath.Join("tmp", "date-repair-backups"), "directory for repair manifests")
	flag.DurationVar(&opts.timeout, "timeout", 4*time.Hour, "overall command timeout")
	flag.Parse()
	return opts
}

func buildRepairPlan(ctx context.Context, db *gorm.DB) (*repairPlan, error) {
	documentDates, err := loadExpectedDocumentDates(ctx, db)
	if err != nil {
		return nil, err
	}
	documentIDs := sortedMapKeys(documentDates)
	plan := &repairPlan{documentDates: documentDates}
	if len(documentIDs) == 0 {
		return plan, nil
	}

	var plans []planRow
	if err := db.WithContext(ctx).
		Table("trade_candidate_plans").
		Select("id, document_id, trade_date").
		Where("document_id IN ?", documentIDs).
		Find(&plans).Error; err != nil {
		return nil, fmt.Errorf("load candidate plans: %w", err)
	}
	for _, row := range plans {
		expected := documentDates[row.DocumentID]
		if sameDate(row.TradeDate, expected) {
			continue
		}
		plan.planUpdates = append(plan.planUpdates, planDateUpdate{
			ID: row.ID, DocumentID: row.DocumentID, OldDate: dateOnly(row.TradeDate), NewDate: expected,
		})
	}

	var events []eventRow
	if err := db.WithContext(ctx).
		Table("recommendation_events AS re").
		Select("re.id, re.source_document_id, re.blogger_id, re.symbol, re.direction, re.recommend_date, re.dedupe_key, b.normalized_name, b.institution").
		Joins("JOIN bloggers AS b ON b.id = re.blogger_id").
		Find(&events).Error; err != nil {
		return nil, fmt.Errorf("load recommendation events: %w", err)
	}
	finalKeys := make(map[string]int64, len(events))
	for _, row := range events {
		finalDate := dateOnly(row.RecommendDate)
		if expected, ok := documentDates[row.SourceDocumentID]; ok {
			finalDate = expected
		}
		finalKey := service.BuildRecommendationEventDedupeKey(
			row.NormalizedName, row.Institution, row.Symbol, row.Direction, finalDate,
		)
		if existingID, exists := finalKeys[finalKey]; exists && existingID != row.ID {
			return nil, fmt.Errorf("final recommendation dedupe collision between event %d and event %d", existingID, row.ID)
		}
		finalKeys[finalKey] = row.ID
		expected, affected := documentDates[row.SourceDocumentID]
		if !affected {
			continue
		}
		dateChange := !sameDate(row.RecommendDate, expected)
		if !dateChange && row.DedupeKey == finalKey {
			continue
		}
		plan.eventUpdates = append(plan.eventUpdates, eventDateUpdate{
			ID: row.ID, DocumentID: row.SourceDocumentID,
			OldDate: dateOnly(row.RecommendDate), NewDate: expected,
			OldKey: row.DedupeKey, NewKey: finalKey, DateChange: dateChange,
		})
		if dateChange {
			plan.evaluationIDs = append(plan.evaluationIDs, row.ID)
		}
	}

	sort.Slice(plan.planUpdates, func(i, j int) bool { return plan.planUpdates[i].ID < plan.planUpdates[j].ID })
	sort.Slice(plan.eventUpdates, func(i, j int) bool { return plan.eventUpdates[i].ID < plan.eventUpdates[j].ID })
	sort.Slice(plan.evaluationIDs, func(i, j int) bool { return plan.evaluationIDs[i] < plan.evaluationIDs[j] })
	if len(plan.evaluationIDs) > 0 {
		if err := db.WithContext(ctx).
			Table("recommendation_event_window_metrics").
			Where("recommendation_event_id IN ?", plan.evaluationIDs).
			Count(&plan.metricsToDelete).Error; err != nil {
			return nil, fmt.Errorf("count stale metrics: %w", err)
		}
	}
	plan.dateFrom, plan.dateTo = dateBounds(documentDates)
	return plan, nil
}

func loadExpectedDocumentDates(ctx context.Context, db *gorm.DB) (map[int64]time.Time, error) {
	dates := make(map[int64]time.Time)
	var openListRows []openListDateRow
	if err := db.WithContext(ctx).
		Table("external_document_ingestions").
		Select("document_id, MIN(article_date) AS article_date, COUNT(DISTINCT article_date) AS date_count").
		Where("source_type = ? AND document_id IS NOT NULL", openListSourceType).
		Group("document_id").
		Find(&openListRows).Error; err != nil {
		return nil, fmt.Errorf("load OpenList article dates: %w", err)
	}
	for _, row := range openListRows {
		if row.DateCount != 1 {
			return nil, fmt.Errorf("OpenList document %d has %d article dates", row.DocumentID, row.DateCount)
		}
		dates[row.DocumentID] = dateOnly(row.ArticleDate)
	}

	var m9Rows []m9DocumentRow
	if err := db.WithContext(ctx).
		Table("documents").
		Select("id, title").
		Where("title LIKE ?", m9TitlePrefix+"%").
		Find(&m9Rows).Error; err != nil {
		return nil, fmt.Errorf("load M9 history document dates: %w", err)
	}
	for _, row := range m9Rows {
		articleDate, err := m9ArticleDateFromTitle(row.Title)
		if err != nil {
			return nil, fmt.Errorf("document %d: %w", row.ID, err)
		}
		if existing, ok := dates[row.ID]; ok && !sameDate(existing, articleDate) {
			return nil, fmt.Errorf(
				"document %d has conflicting source dates: OpenList=%s M9=%s",
				row.ID, existing.Format(time.DateOnly), articleDate.Format(time.DateOnly),
			)
		}
		dates[row.ID] = articleDate
	}
	return dates, nil
}

func m9ArticleDateFromTitle(title string) (time.Time, error) {
	parts := strings.SplitN(title, "|", 3)
	if len(parts) != 3 || parts[0] != strings.TrimSuffix(m9TitlePrefix, "|") {
		return time.Time{}, fmt.Errorf("invalid M9 history title %q", title)
	}
	parsed, err := time.Parse("20060102", parts[1])
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid M9 article date in title %q: %w", title, err)
	}
	return dateOnly(parsed), nil
}

func applyRepairPlan(ctx context.Context, db *gorm.DB, plan *repairPlan) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, batch := range eventUpdateBatches(plan.eventUpdates, repairBatchSize) {
			ids := make([]int64, 0, len(batch))
			for _, update := range batch {
				ids = append(ids, update.ID)
			}
			result := tx.Model(&db_model.RecommendationEvent{}).
				Where("id IN ?", ids).
				Update("dedupe_key", gorm.Expr("CONCAT('date-repair:', id)"))
			if result.Error != nil {
				return fmt.Errorf("assign temporary event dedupe keys: %w", result.Error)
			}
			if result.RowsAffected != int64(len(ids)) {
				return fmt.Errorf("assign temporary event dedupe keys: updated %d rows, expected %d", result.RowsAffected, len(ids))
			}
		}

		for dateText, ids := range groupPlanUpdatesByDate(plan.planUpdates) {
			dateValue, _ := time.Parse(time.DateOnly, dateText)
			for _, batch := range int64Batches(ids, repairBatchSize) {
				result := tx.Model(&db_model.TradeCandidatePlan{}).
					Where("id IN ?", batch).
					Update("trade_date", dateValue)
				if result.Error != nil {
					return fmt.Errorf("update candidate plan dates: %w", result.Error)
				}
				if result.RowsAffected != int64(len(batch)) {
					return fmt.Errorf("update candidate plan dates: updated %d rows, expected %d", result.RowsAffected, len(batch))
				}
			}
		}

		for dateText, ids := range groupEventUpdatesByDate(plan.eventUpdates) {
			dateValue, _ := time.Parse(time.DateOnly, dateText)
			for _, batch := range int64Batches(ids, repairBatchSize) {
				result := tx.Model(&db_model.RecommendationEvent{}).
					Where("id IN ?", batch).
					Update("recommend_date", dateValue)
				if result.Error != nil {
					return fmt.Errorf("update recommendation event dates: %w", result.Error)
				}
			}
		}
		for _, batch := range eventUpdateBatches(plan.eventUpdates, repairBatchSize) {
			if err := updateFinalEventKeys(tx, batch); err != nil {
				return err
			}
		}
		for _, batch := range int64Batches(plan.evaluationIDs, repairBatchSize) {
			if err := tx.Where("recommendation_event_id IN ?", batch).
				Delete(&db_model.RecommendationEventWindowMetric{}).Error; err != nil {
				return fmt.Errorf("delete stale recommendation metrics: %w", err)
			}
		}
		return nil
	})
}

func updateFinalEventKeys(tx *gorm.DB, updates []eventDateUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	var sql strings.Builder
	sql.WriteString("UPDATE recommendation_events SET dedupe_key = CASE id")
	args := make([]any, 0, len(updates)*3)
	for _, update := range updates {
		sql.WriteString(" WHEN ? THEN ?")
		args = append(args, update.ID, update.NewKey)
	}
	sql.WriteString(" END WHERE id IN (")
	for i, update := range updates {
		if i > 0 {
			sql.WriteByte(',')
		}
		sql.WriteByte('?')
		args = append(args, update.ID)
	}
	sql.WriteByte(')')
	result := tx.Exec(sql.String(), args...)
	if result.Error != nil {
		return fmt.Errorf("write final event dedupe keys: %w", result.Error)
	}
	if result.RowsAffected != int64(len(updates)) {
		return fmt.Errorf("write final event dedupe keys: updated %d rows, expected %d", result.RowsAffected, len(updates))
	}
	return nil
}

func countMetricDateMismatches(ctx context.Context, db *gorm.DB, eventIDs []int64) (int64, error) {
	if len(eventIDs) == 0 {
		return 0, nil
	}
	var count int64
	for _, batch := range int64Batches(eventIDs, repairBatchSize) {
		var batchCount int64
		if err := db.WithContext(ctx).
			Table("recommendation_event_window_metrics AS m").
			Joins("JOIN recommendation_events AS re ON re.id = m.recommendation_event_id").
			Where("m.recommendation_event_id IN ?", batch).
			Where("m.recommend_date <> re.recommend_date").
			Count(&batchCount).Error; err != nil {
			return 0, err
		}
		count += batchCount
	}
	return count, nil
}

func writeBackup(dir string, profile config.DatabaseProfile, plan *repairPlan) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create backup directory: %w", err)
	}
	backup := repairBackup{
		CreatedAt: time.Now().UTC(), DatabaseProfile: string(profile),
		Plans: plan.planUpdates, Events: plan.eventUpdates,
	}
	content, err := json.MarshalIndent(backup, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode backup manifest: %w", err)
	}
	path := filepath.Join(dir, fmt.Sprintf("recommendation-date-repair-%s-%s.json", profile, time.Now().UTC().Format("20060102T150405Z")))
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return "", fmt.Errorf("write backup manifest: %w", err)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return path, nil
	}
	return absPath, nil
}

func summaryForPlan(profile config.DatabaseProfile, plan *repairPlan) repairSummary {
	return repairSummary{
		DatabaseProfile: string(profile), SourceDocuments: len(plan.documentDates),
		PlansToUpdate: len(plan.planUpdates), EventsToUpdate: len(plan.eventUpdates),
		EventsToReevaluate: len(plan.evaluationIDs), MetricsToDelete: plan.metricsToDelete,
		DateFrom: plan.dateFrom, DateTo: plan.dateTo,
	}
}

func dateOnly(value time.Time) time.Time {
	if value.IsZero() {
		return time.Time{}
	}
	parsed, _ := time.Parse(time.DateOnly, value.Format(time.DateOnly))
	return parsed
}

func sameDate(left time.Time, right time.Time) bool {
	return left.Format(time.DateOnly) == right.Format(time.DateOnly)
}

func dateBounds(dates map[int64]time.Time) (string, string) {
	values := make([]string, 0, len(dates))
	for _, value := range dates {
		values = append(values, value.Format(time.DateOnly))
	}
	if len(values) == 0 {
		return "", ""
	}
	sort.Strings(values)
	return values[0], values[len(values)-1]
}

func sortedMapKeys(values map[int64]time.Time) []int64 {
	keys := make([]int64, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

func groupPlanUpdatesByDate(updates []planDateUpdate) map[string][]int64 {
	result := make(map[string][]int64)
	for _, update := range updates {
		dateText := update.NewDate.Format(time.DateOnly)
		result[dateText] = append(result[dateText], update.ID)
	}
	return result
}

func groupEventUpdatesByDate(updates []eventDateUpdate) map[string][]int64 {
	result := make(map[string][]int64)
	for _, update := range updates {
		dateText := update.NewDate.Format(time.DateOnly)
		result[dateText] = append(result[dateText], update.ID)
	}
	return result
}

func int64Batches(values []int64, size int) [][]int64 {
	var batches [][]int64
	for start := 0; start < len(values); start += size {
		end := start + size
		if end > len(values) {
			end = len(values)
		}
		batches = append(batches, values[start:end])
	}
	return batches
}

func eventUpdateBatches(values []eventDateUpdate, size int) [][]eventDateUpdate {
	var batches [][]eventDateUpdate
	for start := 0; start < len(values); start += size {
		end := start + size
		if end > len(values) {
			end = len(values)
		}
		batches = append(batches, values[start:end])
	}
	return batches
}

func writeJSON(output *os.File, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
