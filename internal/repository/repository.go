package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"finance-sys/internal/domain"
	reposqlc "finance-sys/internal/repository/sqlc"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("repository: not found")

type Repository struct {
	pool    *pgxpool.Pool
	queries *reposqlc.Queries
}

func New(pool *pgxpool.Pool) *Repository {
	return &Repository{
		pool:    pool,
		queries: reposqlc.New(pool),
	}
}

func (r *Repository) Ping(ctx context.Context) error {
	return r.pool.Ping(ctx)
}

func (r *Repository) InsertConfigSnapshot(ctx context.Context, snapshot *domain.ConfigSnapshot) (*domain.ConfigSnapshot, error) {
	row, err := r.queries.InsertConfigSnapshot(ctx, reposqlc.InsertConfigSnapshotParams{
		ConfigVersion: snapshot.ConfigVersion,
		Source:        snapshot.Source,
		Sha256:        snapshot.SHA256,
		RawJson:       []byte(snapshot.RawJSON),
	})
	if err != nil {
		return nil, err
	}
	return mapConfigSnapshot(row), nil
}

func (r *Repository) CreateDocument(ctx context.Context, request domain.DocumentIngestRequest, sha256Value string, objectKey string, configVersion int64) (*domain.Document, error) {
	row, err := r.queries.InsertDocument(ctx, reposqlc.InsertDocumentParams{
		SourceType:    request.SourceType,
		SourceName:    request.SourceName,
		Author:        request.Author,
		Institution:   request.Institution,
		Title:         request.Title,
		FileName:      request.FileName,
		Extension:     extFromFileName(request.FileName),
		ContentType:   request.ContentType,
		Sha256:        sha256Value,
		ObjectKey:     objectKey,
		Status:        "INGESTED",
		ConfigVersion: configVersion,
	})
	if err != nil {
		return nil, err
	}
	return mapDocument(row), nil
}

func (r *Repository) GetDocumentByID(ctx context.Context, id int64) (*domain.Document, error) {
	row, err := r.queries.GetDocumentByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return mapDocument(row), nil
}

func (r *Repository) GetDocumentBySHA(ctx context.Context, sha string) (*domain.Document, error) {
	row, err := r.queries.GetDocumentBySHA(ctx, sha)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return mapDocument(row), nil
}

func (r *Repository) ListDocuments(ctx context.Context, limit int32) ([]domain.Document, error) {
	rows, err := r.queries.ListDocuments(ctx, limit)
	if err != nil {
		return nil, err
	}
	items := make([]domain.Document, 0, len(rows))
	for _, row := range rows {
		items = append(items, *mapDocument(row))
	}
	return items, nil
}

func (r *Repository) ListDocumentsByStatus(ctx context.Context, status string, limit int32) ([]domain.Document, error) {
	rows, err := r.queries.ListDocumentsByStatus(ctx, reposqlc.ListDocumentsByStatusParams{
		Status: status,
		Limit:  limit,
	})
	if err != nil {
		return nil, err
	}
	items := make([]domain.Document, 0, len(rows))
	for _, row := range rows {
		items = append(items, *mapDocument(row))
	}
	return items, nil
}

func (r *Repository) UpdateDocumentStatus(ctx context.Context, id int64, status string) error {
	return r.queries.UpdateDocumentStatus(ctx, reposqlc.UpdateDocumentStatusParams{
		ID:     id,
		Status: status,
	})
}

func (r *Repository) CreateParseRun(ctx context.Context, run domain.ParseRun) (*domain.ParseRun, error) {
	sections, err := json.Marshal(run.Sections)
	if err != nil {
		return nil, err
	}
	chunks, err := json.Marshal(run.Chunks)
	if err != nil {
		return nil, err
	}
	tables, err := json.Marshal(run.Tables)
	if err != nil {
		return nil, err
	}
	rawMetadata, err := json.Marshal(run.RawMetadata)
	if err != nil {
		return nil, err
	}
	row, err := r.queries.InsertParseRun(ctx, reposqlc.InsertParseRunParams{
		DocumentID:      run.DocumentID,
		Status:          run.Status,
		ParserName:      run.ParserName,
		ParserVersion:   run.ParserVersion,
		RequiresOcr:     run.RequiresOCR,
		ErrorMessage:    run.ErrorMessage,
		PageCount:       int32(run.PageCount),
		TextDensity:     run.TextDensity,
		ContentText:     run.ContentText,
		CleanedText:     run.CleanedText,
		SectionsJson:    sections,
		ChunksJson:      chunks,
		TablesJson:      tables,
		RawMetadataJson: rawMetadata,
	})
	if err != nil {
		return nil, err
	}
	return mapParseRun(row)
}

func (r *Repository) GetLatestParseRunByDocumentID(ctx context.Context, documentID int64) (*domain.ParseRun, error) {
	row, err := r.queries.GetLatestParseRunByDocumentID(ctx, documentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return mapParseRun(row)
}

func (r *Repository) CreateSignal(ctx context.Context, signal domain.ExpertSignal) (*domain.ExpertSignal, error) {
	evidence, err := json.Marshal(signal.Evidence)
	if err != nil {
		return nil, err
	}
	risks, err := json.Marshal(signal.Risks)
	if err != nil {
		return nil, err
	}
	row, err := r.queries.InsertSignal(ctx, reposqlc.InsertSignalParams{
		DocumentID:    signal.DocumentID,
		ParseRunID:    signal.ParseRunID,
		ExpertName:    signal.ExpertName,
		ExpertOrg:     signal.ExpertOrg,
		Symbol:        signal.Symbol,
		AssetType:     signal.AssetType,
		Market:        signal.Market,
		Sentiment:     signal.Sentiment,
		Thesis:        signal.Thesis,
		EvidenceJson:  evidence,
		RisksJson:     risks,
		Confidence:    signal.Confidence,
		ConfigVersion: signal.ConfigVersion,
		RuleVersion:   signal.RuleVersion,
		SignalDate:    date(signal.SignalDate),
	})
	if err != nil {
		return nil, err
	}
	return mapSignal(row)
}

func (r *Repository) ListSignalsByDocumentID(ctx context.Context, documentID int64) ([]domain.ExpertSignal, error) {
	rows, err := r.queries.ListSignalsByDocumentID(ctx, documentID)
	if err != nil {
		return nil, err
	}
	items := make([]domain.ExpertSignal, 0, len(rows))
	for _, row := range rows {
		item, err := mapSignal(row)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, nil
}

func (r *Repository) GetSignalByID(ctx context.Context, id int64) (*domain.ExpertSignal, error) {
	row, err := r.queries.GetSignalByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return mapSignal(row)
}

func (r *Repository) CreateMarketSnapshot(ctx context.Context, snapshot domain.MarketSnapshot) (*domain.MarketSnapshot, error) {
	row, err := r.queries.InsertMarketSnapshot(ctx, reposqlc.InsertMarketSnapshotParams{
		Symbol:             snapshot.Symbol,
		TradeDate:          date(snapshot.TradeDate),
		Provider:           snapshot.Provider,
		Open:               snapshot.Open,
		High:               snapshot.High,
		Low:                snapshot.Low,
		Close:              snapshot.Close,
		Volume:             snapshot.Volume,
		Turnover:           snapshot.Turnover,
		Atr:                snapshot.ATR,
		PrevClose:          snapshot.PrevClose,
		BenchmarkReturnPct: snapshot.BenchmarkReturnPct,
		RawObjectKey:       snapshot.RawObjectKey,
		ConfigVersion:      snapshot.ConfigVersion,
	})
	if err != nil {
		return nil, err
	}
	return mapMarketSnapshot(row), nil
}

func (r *Repository) GetMarketSnapshotBySymbolDate(ctx context.Context, symbol string, tradeDate time.Time) (*domain.MarketSnapshot, error) {
	row, err := r.queries.GetMarketSnapshotBySymbolDate(ctx, reposqlc.GetMarketSnapshotBySymbolDateParams{
		Symbol:    symbol,
		TradeDate: date(tradeDate),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return mapMarketSnapshot(row), nil
}

func (r *Repository) CreatePlan(ctx context.Context, plan domain.TradePlan) (*domain.TradePlan, error) {
	row, err := r.queries.InsertPlan(ctx, reposqlc.InsertPlanParams{
		SignalID:          plan.SignalID,
		DocumentID:        plan.DocumentID,
		Symbol:            plan.Symbol,
		Strategy:          plan.Strategy,
		TradeDate:         date(plan.TradeDate),
		Direction:         plan.Direction,
		EntryPrice:        plan.EntryPrice,
		StopLoss:          plan.StopLoss,
		TakeProfit:        plan.TakeProfit,
		InvalidationPrice: plan.InvalidationPrice,
		PositionPct:       plan.PositionPct,
		Status:            plan.Status,
		Rationale:         plan.Rationale,
		ConfigVersion:     plan.ConfigVersion,
		RuleVersion:       plan.RuleVersion,
		MarketSnapshotID:  plan.MarketSnapshotID,
	})
	if err != nil {
		return nil, err
	}
	return mapPlan(row), nil
}

func (r *Repository) GetPlanByID(ctx context.Context, id int64) (*domain.TradePlan, error) {
	row, err := r.queries.GetPlanByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return mapPlan(row), nil
}

func (r *Repository) ListPlans(ctx context.Context, limit int32) ([]domain.TradePlan, error) {
	rows, err := r.queries.ListPlans(ctx, limit)
	if err != nil {
		return nil, err
	}
	items := make([]domain.TradePlan, 0, len(rows))
	for _, row := range rows {
		items = append(items, *mapPlan(row))
	}
	return items, nil
}

func (r *Repository) ApprovePlan(ctx context.Context, id int64, approvedBy string) (*domain.TradePlan, error) {
	row, err := r.queries.ApprovePlan(ctx, reposqlc.ApprovePlanParams{
		ID:         id,
		ApprovedBy: approvedBy,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return mapPlan(row), nil
}

func (r *Repository) ListApprovedPlansForTradeDateWithoutEvaluation(ctx context.Context, tradeDate time.Time) ([]domain.TradePlan, error) {
	rows, err := r.queries.ListApprovedPlansForTradeDateWithoutEvaluation(ctx, date(tradeDate))
	if err != nil {
		return nil, err
	}
	items := make([]domain.TradePlan, 0, len(rows))
	for _, row := range rows {
		items = append(items, *mapPlan(row))
	}
	return items, nil
}

func (r *Repository) CreateEvaluation(ctx context.Context, evaluation domain.PlanEvaluation) (*domain.PlanEvaluation, error) {
	row, err := r.queries.InsertEvaluation(ctx, reposqlc.InsertEvaluationParams{
		PlanID:             evaluation.PlanID,
		TradeDate:          date(evaluation.TradeDate),
		Status:             evaluation.Status,
		EntryPrice:         evaluation.EntryPrice,
		ExitPrice:          evaluation.ExitPrice,
		ClosePrice:         evaluation.ClosePrice,
		PnlPct:             evaluation.PNLPct,
		MfePct:             evaluation.MFEPct,
		MaePct:             evaluation.MAEPct,
		BenchmarkReturnPct: evaluation.BenchmarkReturnPct,
		ExcessReturnPct:    evaluation.ExcessReturnPct,
		Reason:             evaluation.EvaluationReason,
		DataQualityFlag:    evaluation.DataQualityFlag,
		ConfigVersion:      evaluation.ConfigVersion,
	})
	if err != nil {
		return nil, err
	}
	return mapEvaluation(row), nil
}

func (r *Repository) ListEvaluations(ctx context.Context, limit int32) ([]domain.PlanEvaluation, error) {
	rows, err := r.queries.ListEvaluations(ctx, limit)
	if err != nil {
		return nil, err
	}
	items := make([]domain.PlanEvaluation, 0, len(rows))
	for _, row := range rows {
		items = append(items, *mapEvaluation(row))
	}
	return items, nil
}

func date(t time.Time) pgtype.Date {
	return pgtype.Date{
		Time:  t.UTC(),
		Valid: true,
	}
}

func ts(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time.UTC()
}

func dateTime(value pgtype.Date) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time.UTC()
}

func mapConfigSnapshot(row reposqlc.ConfigSnapshot) *domain.ConfigSnapshot {
	return &domain.ConfigSnapshot{
		ID:            row.ID,
		ConfigVersion: row.ConfigVersion,
		Source:        row.Source,
		SHA256:        row.Sha256,
		RawJSON:       string(row.RawJson),
		CreatedAt:     ts(row.CreatedAt),
	}
}

func mapDocument(row reposqlc.Document) *domain.Document {
	return &domain.Document{
		ID:            row.ID,
		SourceType:    row.SourceType,
		SourceName:    row.SourceName,
		Author:        row.Author,
		Institution:   row.Institution,
		Title:         row.Title,
		FileName:      row.FileName,
		Extension:     row.Extension,
		ContentType:   row.ContentType,
		SHA256:        row.Sha256,
		ObjectKey:     row.ObjectKey,
		Status:        row.Status,
		ConfigVersion: row.ConfigVersion,
		CreatedAt:     ts(row.CreatedAt),
		UpdatedAt:     ts(row.UpdatedAt),
	}
}

func mapParseRun(row reposqlc.ParseRun) (*domain.ParseRun, error) {
	var sections []domain.Section
	var chunks []domain.Chunk
	var tables []domain.Table
	rawMetadata := make(map[string]any)
	if err := json.Unmarshal(row.SectionsJson, &sections); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(row.ChunksJson, &chunks); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(row.TablesJson, &tables); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(row.RawMetadataJson, &rawMetadata); err != nil {
		return nil, err
	}
	return &domain.ParseRun{
		ID:            row.ID,
		DocumentID:    row.DocumentID,
		Status:        row.Status,
		ParserName:    row.ParserName,
		ParserVersion: row.ParserVersion,
		RequiresOCR:   row.RequiresOcr,
		ErrorMessage:  row.ErrorMessage,
		PageCount:     int(row.PageCount),
		TextDensity:   row.TextDensity,
		ContentText:   row.ContentText,
		CleanedText:   row.CleanedText,
		Sections:      sections,
		Chunks:        chunks,
		Tables:        tables,
		RawMetadata:   rawMetadata,
		CreatedAt:     ts(row.CreatedAt),
		UpdatedAt:     ts(row.UpdatedAt),
	}, nil
}

func mapSignal(row reposqlc.Signal) (*domain.ExpertSignal, error) {
	var evidence []domain.EvidenceSpan
	var risks []string
	if err := json.Unmarshal(row.EvidenceJson, &evidence); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(row.RisksJson, &risks); err != nil {
		return nil, err
	}
	return &domain.ExpertSignal{
		ID:            row.ID,
		DocumentID:    row.DocumentID,
		ParseRunID:    row.ParseRunID,
		ExpertName:    row.ExpertName,
		ExpertOrg:     row.ExpertOrg,
		Symbol:        row.Symbol,
		AssetType:     row.AssetType,
		Market:        row.Market,
		Sentiment:     row.Sentiment,
		Thesis:        row.Thesis,
		Evidence:      evidence,
		Risks:         risks,
		Confidence:    row.Confidence,
		ConfigVersion: row.ConfigVersion,
		RuleVersion:   row.RuleVersion,
		SignalDate:    dateTime(row.SignalDate),
		CreatedAt:     ts(row.CreatedAt),
	}, nil
}

func mapMarketSnapshot(row reposqlc.MarketSnapshot) *domain.MarketSnapshot {
	return &domain.MarketSnapshot{
		ID:                 row.ID,
		Symbol:             row.Symbol,
		TradeDate:          dateTime(row.TradeDate),
		Provider:           row.Provider,
		Open:               row.Open,
		High:               row.High,
		Low:                row.Low,
		Close:              row.Close,
		Volume:             row.Volume,
		Turnover:           row.Turnover,
		ATR:                row.Atr,
		PrevClose:          row.PrevClose,
		BenchmarkReturnPct: row.BenchmarkReturnPct,
		RawObjectKey:       row.RawObjectKey,
		ConfigVersion:      row.ConfigVersion,
		CreatedAt:          ts(row.CreatedAt),
	}
}

func mapPlan(row reposqlc.Plan) *domain.TradePlan {
	return &domain.TradePlan{
		ID:                row.ID,
		SignalID:          row.SignalID,
		DocumentID:        row.DocumentID,
		Symbol:            row.Symbol,
		Strategy:          row.Strategy,
		TradeDate:         dateTime(row.TradeDate),
		Direction:         row.Direction,
		EntryPrice:        row.EntryPrice,
		StopLoss:          row.StopLoss,
		TakeProfit:        row.TakeProfit,
		InvalidationPrice: row.InvalidationPrice,
		PositionPct:       row.PositionPct,
		Status:            row.Status,
		Rationale:         row.Rationale,
		ConfigVersion:     row.ConfigVersion,
		RuleVersion:       row.RuleVersion,
		MarketSnapshotID:  row.MarketSnapshotID,
		ApprovedBy:        row.ApprovedBy,
		ApprovedAt:        ts(row.ApprovedAt),
		CreatedAt:         ts(row.CreatedAt),
		UpdatedAt:         ts(row.UpdatedAt),
	}
}

func mapEvaluation(row reposqlc.Evaluation) *domain.PlanEvaluation {
	return &domain.PlanEvaluation{
		ID:                 row.ID,
		PlanID:             row.PlanID,
		TradeDate:          dateTime(row.TradeDate),
		Status:             row.Status,
		EntryPrice:         row.EntryPrice,
		ExitPrice:          row.ExitPrice,
		ClosePrice:         row.ClosePrice,
		PNLPct:             row.PnlPct,
		MFEPct:             row.MfePct,
		MAEPct:             row.MaePct,
		BenchmarkReturnPct: row.BenchmarkReturnPct,
		ExcessReturnPct:    row.ExcessReturnPct,
		EvaluationReason:   row.Reason,
		DataQualityFlag:    row.DataQualityFlag,
		ConfigVersion:      row.ConfigVersion,
		CreatedAt:          ts(row.CreatedAt),
	}
}

func extFromFileName(name string) string {
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '.' {
			return name[i:]
		}
	}
	return ""
}
