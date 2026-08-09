package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"finance-sys/internal/bootstrap"
	"finance-sys/internal/config"
	"finance-sys/internal/dal"
	"finance-sys/internal/domain/db_model"
	"finance-sys/internal/service"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type options struct {
	apply   bool
	dryRun  bool
	timeout time.Duration
}

type bloggerRow struct {
	ID             int64
	Name           string
	NormalizedName string
	Institution    string
	SourceType     string
}

type documentRow struct {
	ID     int64
	Sha256 string
}

type parseRunRow struct {
	ID            int64
	DocumentID    int64
	Status        string
	ParserName    string
	ParserVersion string
}

func main() {
	opts := parseFlags()
	if !opts.apply && !opts.dryRun {
		fatal(errors.New("-apply or -dry-run is required"))
	}
	if err := bootstrap.LoadNacosServerAddressFromFiles("bootstrap_go122.env", "bootstrap_go122.env.example"); err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	snapshot, _, err := bootstrap.LoadInitialSnapshot(ctx, logger)
	if err != nil {
		fatal(fmt.Errorf("load config snapshot: %w", err))
	}
	testDB, err := openDB(ctx, snapshot.Config.DatabaseTest)
	if err != nil {
		fatal(fmt.Errorf("open test database: %w", err))
	}
	defer testClose(testDB)
	prodDB, err := openDB(ctx, snapshot.Config.DatabaseProduction)
	if err != nil {
		fatal(fmt.Errorf("open production database: %w", err))
	}
	defer testClose(prodDB)

	summary, err := syncSectorEvents(ctx, logger, testDB, prodDB, opts.dryRun)
	if err != nil {
		fatal(err)
	}
	logger.Info("sector promotion completed",
		"source_events", summary.sourceEvents,
		"synced_events", summary.syncedEvents,
		"synced_evidence", summary.syncedEvidence,
		"missing_documents", summary.missingDocuments,
		"missing_bloggers", summary.missingBloggers,
		"parse_run_fallbacks", summary.parseRunFallbacks,
		"dry_run", opts.dryRun,
	)
}

type syncSummary struct {
	sourceEvents      int
	syncedEvents      int
	syncedEvidence    int
	missingDocuments  int
	missingBloggers   int
	parseRunFallbacks int
}

func syncSectorEvents(ctx context.Context, logger *slog.Logger, testDB, prodDB *gorm.DB, dryRun bool) (*syncSummary, error) {
	testBloggers, err := loadBloggers(ctx, testDB)
	if err != nil {
		return nil, fmt.Errorf("load test bloggers: %w", err)
	}
	prodBloggers, err := loadBloggers(ctx, prodDB)
	if err != nil {
		return nil, fmt.Errorf("load production bloggers: %w", err)
	}
	prodBloggerByKey := make(map[string]bloggerRow, len(prodBloggers))
	for _, blogger := range prodBloggers {
		key := bloggerBusinessKey(blogger.NormalizedName, blogger.Institution)
		if _, exists := prodBloggerByKey[key]; !exists {
			prodBloggerByKey[key] = blogger
		}
	}

	testDocs, err := loadDocuments(ctx, testDB)
	if err != nil {
		return nil, fmt.Errorf("load test documents: %w", err)
	}
	prodDocs, err := loadDocuments(ctx, prodDB)
	if err != nil {
		return nil, fmt.Errorf("load production documents: %w", err)
	}
	prodDocBySHA := make(map[string]int64, len(prodDocs))
	for _, doc := range prodDocs {
		prodDocBySHA[doc.Sha256] = doc.ID
	}
	testDocSHA := make(map[int64]string, len(testDocs))
	for _, doc := range testDocs {
		testDocSHA[doc.ID] = doc.Sha256
	}

	events, err := dal.RecommendationEvents.QueryByParam(ctx, testDB, dal.QueryParam{
		Where:  []dal.Condition{dal.Eq("asset_type", "SECTOR"), {Query: "plan_id IS NULL"}},
		Orders: []dal.OrderParam{dal.OrderBy("id", true)},
	})
	if err != nil {
		return nil, fmt.Errorf("query test sector events: %w", err)
	}
	evidence, err := loadEvidenceByEventIDs(ctx, testDB, eventIDs(events))
	if err != nil {
		return nil, fmt.Errorf("query test sector evidence: %w", err)
	}
	evidenceByEvent := make(map[int64][]db_model.RecommendationEventEvidence)
	for _, item := range evidence {
		evidenceByEvent[item.RecommendationEventID] = append(evidenceByEvent[item.RecommendationEventID], item)
	}

	testParseRuns, err := loadParseRunsByIDs(ctx, testDB, eventParseRunIDs(events))
	if err != nil {
		return nil, fmt.Errorf("query test parse runs: %w", err)
	}
	testParseRunByID := make(map[int64]parseRunRow, len(testParseRuns))
	for _, run := range testParseRuns {
		testParseRunByID[run.ID] = run
	}

	prodDocIDs := make(map[int64]struct{})
	for _, event := range events {
		sha := testDocSHA[event.SourceDocumentID]
		if prodID, ok := prodDocBySHA[sha]; ok {
			prodDocIDs[prodID] = struct{}{}
		}
	}
	prodParseRuns, err := loadParseRunsByDocumentIDs(ctx, prodDB, prodDocIDs)
	if err != nil {
		return nil, fmt.Errorf("query production parse runs: %w", err)
	}
	prodParseRunsByDoc := make(map[int64][]parseRunRow)
	for _, run := range prodParseRuns {
		prodParseRunsByDoc[run.DocumentID] = append(prodParseRunsByDoc[run.DocumentID], run)
	}

	summary := &syncSummary{sourceEvents: len(events)}
	for _, event := range events {
		sha := testDocSHA[event.SourceDocumentID]
		prodDocID, ok := prodDocBySHA[sha]
		if !ok {
			summary.missingDocuments++
			logger.Warn("skip event: production document missing", "test_document_id", event.SourceDocumentID, "event_id", event.ID, "symbol", event.Symbol)
			continue
		}
		testBlogger, ok := testBloggers[event.BloggerID]
		if !ok {
			summary.missingBloggers++
			logger.Warn("skip event: test blogger missing", "test_blogger_id", event.BloggerID, "event_id", event.ID)
			continue
		}
		prodBlogger, ok := prodBloggerByKey[bloggerBusinessKey(testBlogger.NormalizedName, testBlogger.Institution)]
		if !ok {
			if dryRun {
				summary.missingBloggers++
				logger.Warn("skip event: production blogger missing", "blogger", testBlogger.Name, "event_id", event.ID)
				continue
			}
			created := db_model.Blogger{
				Name:           testBlogger.Name,
				NormalizedName: testBlogger.NormalizedName,
				Institution:    testBlogger.Institution,
				SourceType:     testBlogger.SourceType,
			}
			if err := prodDB.WithContext(ctx).Create(&created).Error; err != nil {
				return nil, fmt.Errorf("create production blogger %q: %w", testBlogger.Name, err)
			}
			prodBlogger = bloggerRow{
				ID:             created.ID,
				Name:           created.Name,
				NormalizedName: created.NormalizedName,
				Institution:    created.Institution,
				SourceType:     created.SourceType,
			}
			prodBloggerByKey[bloggerBusinessKey(prodBlogger.NormalizedName, prodBlogger.Institution)] = prodBlogger
			logger.Info("created production blogger", "blogger", testBlogger.Name, "prod_blogger_id", prodBlogger.ID)
		}
		testRun := testParseRunByID[event.ParseRunID]
		prodParseRunID, usedFallback := resolveProductionParseRun(prodParseRunsByDoc[prodDocID], testRun)
		if usedFallback {
			summary.parseRunFallbacks++
			logger.Warn("parse run fallback", "test_document_id", event.SourceDocumentID, "prod_document_id", prodDocID, "test_parse_run_id", event.ParseRunID)
		}

		model := event
		model.ID = 0
		model.BloggerID = prodBlogger.ID
		model.SourceDocumentID = prodDocID
		model.PlanID = nil
		model.ParseRunID = prodParseRunID
		model.DedupeKey = service.BuildRecommendationEventDedupeKey(
			prodBlogger.NormalizedName,
			prodBlogger.Institution,
			event.Symbol,
			event.Direction,
			event.RecommendDate,
		)
		model.CreatedAt = time.Time{}
		model.UpdatedAt = time.Time{}

		eventEvidence := evidenceByEvent[event.ID]
		if err := prodDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if dryRun {
				return nil
			}
			if err := dal.RecommendationEvents.UpsertByDedupeKey(ctx, tx, &model); err != nil {
				return fmt.Errorf("upsert production event dedupe_key=%s: %w", model.DedupeKey, err)
			}
			persisted, err := dal.RecommendationEvents.QueryByDedupeKey(ctx, tx, model.DedupeKey)
			if err != nil {
				return fmt.Errorf("query production event by dedupe key: %w", err)
			}
			if err := dal.RecommendationEventEvidences.DeleteByEventID(ctx, tx, persisted.ID); err != nil {
				return fmt.Errorf("delete production evidence: %w", err)
			}
			evidenceModels := make([]db_model.RecommendationEventEvidence, 0, len(eventEvidence))
			for _, item := range eventEvidence {
				evidenceModels = append(evidenceModels, db_model.RecommendationEventEvidence{
					RecommendationEventID: persisted.ID,
					SourceDocumentID:      prodDocID,
					PlanID:                nil,
					ChunkIndex:            item.ChunkIndex,
					EvidenceText:          item.EvidenceText,
				})
			}
			if err := dal.RecommendationEventEvidences.CreateBatch(ctx, tx, evidenceModels); err != nil {
				return fmt.Errorf("create production evidence: %w", err)
			}
			summary.syncedEvidence += len(evidenceModels)
			return nil
		}); err != nil {
			return nil, fmt.Errorf("sync event %d (%s): %w", event.ID, event.Symbol, err)
		}
		summary.syncedEvents++
		summary.syncedEvidence += len(eventEvidence)
	}
	return summary, nil
}

func resolveProductionParseRun(runs []parseRunRow, testRun parseRunRow) (int64, bool) {
	if len(runs) == 0 {
		return 0, true
	}
	best := runs[0]
	for _, run := range runs {
		if run.ID > best.ID {
			best = run
		}
	}
	if testRun.ID == 0 || testRun.ParserName == "" {
		return best.ID, true
	}
	for _, run := range runs {
		if run.ParserName == testRun.ParserName && run.ParserVersion == testRun.ParserVersion {
			return run.ID, false
		}
	}
	return best.ID, true
}

func loadBloggers(ctx context.Context, db *gorm.DB) (map[int64]bloggerRow, error) {
	var rows []db_model.Blogger
	if err := db.WithContext(ctx).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make(map[int64]bloggerRow, len(rows))
	for _, row := range rows {
		result[row.ID] = bloggerRow{ID: row.ID, Name: row.Name, NormalizedName: row.NormalizedName, Institution: row.Institution, SourceType: row.SourceType}
	}
	return result, nil
}

func loadDocuments(ctx context.Context, db *gorm.DB) ([]documentRow, error) {
	var rows []documentRow
	err := db.WithContext(ctx).Raw("SELECT id, sha256 FROM documents ORDER BY id ASC").Scan(&rows).Error
	return rows, err
}

func loadEvidenceByEventIDs(ctx context.Context, db *gorm.DB, ids []int64) ([]db_model.RecommendationEventEvidence, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []db_model.RecommendationEventEvidence
	err := db.WithContext(ctx).Where("recommendation_event_id IN ?", ids).Order("id ASC").Find(&rows).Error
	return rows, err
}

func loadParseRunsByIDs(ctx context.Context, db *gorm.DB, ids []int64) ([]parseRunRow, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []parseRunRow
	err := db.WithContext(ctx).Raw("SELECT id, document_id, status, parser_name, parser_version FROM parse_runs WHERE id IN ? ORDER BY id ASC", ids).Scan(&rows).Error
	return rows, err
}

func loadParseRunsByDocumentIDs(ctx context.Context, db *gorm.DB, documentIDs map[int64]struct{}) ([]parseRunRow, error) {
	if len(documentIDs) == 0 {
		return nil, nil
	}
	ids := make([]int64, 0, len(documentIDs))
	for id := range documentIDs {
		ids = append(ids, id)
	}
	var rows []parseRunRow
	err := db.WithContext(ctx).Raw("SELECT id, document_id, status, parser_name, parser_version FROM parse_runs WHERE document_id IN ? AND status = 'PARSED' ORDER BY id ASC", ids).Scan(&rows).Error
	return rows, err
}

func eventIDs(events []db_model.RecommendationEvent) []int64 {
	result := make([]int64, 0, len(events))
	for _, event := range events {
		result = append(result, event.ID)
	}
	return result
}

func eventParseRunIDs(events []db_model.RecommendationEvent) []int64 {
	seen := make(map[int64]struct{})
	result := make([]int64, 0, len(events))
	for _, event := range events {
		if _, ok := seen[event.ParseRunID]; ok {
			continue
		}
		seen[event.ParseRunID] = struct{}{}
		result = append(result, event.ParseRunID)
	}
	return result
}

func bloggerBusinessKey(normalizedName, institution string) string {
	return normalizedName + "\x00" + institution
}

func openDB(ctx context.Context, cfg config.DatabaseConfig) (*gorm.DB, error) {
	db, err := gorm.Open(mysql.Open(cfg.DSN), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetimeMinutes) * time.Minute)
	sqlDB.SetConnMaxIdleTime(time.Duration(cfg.ConnMaxIdleTimeMinutes) * time.Minute)
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return db, nil
}

func testClose(db *gorm.DB) {
	sqlDB, err := db.DB()
	if err == nil {
		_ = sqlDB.Close()
	}
}

func parseFlags() options {
	var opts options
	flag.BoolVar(&opts.apply, "apply", false, "apply the promotion")
	flag.BoolVar(&opts.dryRun, "dry-run", false, "validate mappings without writing")
	flag.DurationVar(&opts.timeout, "timeout", 4*time.Hour, "overall timeout")
	flag.Parse()
	if opts.apply && opts.dryRun {
		fatal(errors.New("-apply and -dry-run cannot be combined"))
	}
	return opts
}

func fatal(err error) {
	_, _ = fmt.Fprintln(os.Stderr, "promote sector events:", err)
	os.Exit(1)
}
