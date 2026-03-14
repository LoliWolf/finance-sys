package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"

	"finance-sys/internal/domain"
	reposqlc "finance-sys/internal/repository/sqlc"
)

var ErrNotFound = errors.New("repository: not found")

type Repository struct {
	db      *sql.DB
	queries *reposqlc.Queries
	logger  *slog.Logger
}

func New(db *sql.DB, logger *slog.Logger) *Repository {
	return &Repository{
		db:      db,
		queries: reposqlc.New(db),
		logger:  logger,
	}
}

func (r *Repository) Ping(ctx context.Context) error {
	r.logDebug(ctx, "repository ping start")
	return r.db.PingContext(ctx)
}

func (r *Repository) InsertConfigSnapshot(ctx context.Context, snapshot *domain.ConfigSnapshot) (*domain.ConfigSnapshot, error) {
	r.logInfo(ctx, "repository insert config snapshot start", "config_version", snapshot.ConfigVersion, "source", snapshot.Source)
	result, err := r.queries.InsertConfigSnapshot(ctx, reposqlc.InsertConfigSnapshotParams{
		ConfigVersion: snapshot.ConfigVersion,
		Source:        snapshot.Source,
		Sha256:        snapshot.SHA256,
		RawJson:       json.RawMessage(snapshot.RawJSON),
	})
	if err != nil {
		r.logError(ctx, "repository insert config snapshot failed", "config_version", snapshot.ConfigVersion, "error", err.Error())
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		r.logError(ctx, "repository insert config snapshot last insert id failed", "config_version", snapshot.ConfigVersion, "error", err.Error())
		return nil, err
	}
	row, err := r.queries.GetConfigSnapshotByID(ctx, id)
	if err != nil {
		r.logError(ctx, "repository load config snapshot by id failed", "config_snapshot_id", id, "error", err.Error())
		return nil, err
	}
	r.logInfo(ctx, "repository insert config snapshot success", "config_snapshot_id", id, "config_version", snapshot.ConfigVersion)
	return mapConfigSnapshot(row), nil
}

func (r *Repository) CreateDocument(ctx context.Context, request domain.DocumentIngestRequest, sha256Value string, configVersion int64) (*domain.Document, error) {
	r.logInfo(ctx, "repository create document start", "file_name", request.FileName, "sha256", sha256Value, "config_version", configVersion)
	result, err := r.queries.InsertDocument(ctx, reposqlc.InsertDocumentParams{
		SourceType:    request.SourceType,
		SourceName:    request.SourceName,
		Author:        request.Author,
		Institution:   request.Institution,
		Title:         request.Title,
		FileName:      request.FileName,
		Extension:     filepath.Ext(request.FileName),
		ContentType:   request.ContentType,
		Sha256:        sha256Value,
		Status:        "INGESTED",
		ConfigVersion: configVersion,
		RawContent:    request.Content,
	})
	if err != nil {
		r.logError(ctx, "repository create document failed", "file_name", request.FileName, "error", err.Error())
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		r.logError(ctx, "repository create document last insert id failed", "file_name", request.FileName, "error", err.Error())
		return nil, err
	}
	row, err := r.queries.GetDocumentByID(ctx, id)
	if err != nil {
		r.logError(ctx, "repository load document by id failed", "document_id", id, "error", err.Error())
		return nil, err
	}
	r.logInfo(ctx, "repository create document success", "document_id", id, "file_name", request.FileName)
	return mapDocument(row), nil
}

func (r *Repository) GetDocumentByID(ctx context.Context, id int64) (*domain.Document, error) {
	r.logDebug(ctx, "repository get document by id start", "document_id", id)
	row, err := r.queries.GetDocumentByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		r.logWarn(ctx, "repository get document by id not found", "document_id", id)
		return nil, ErrNotFound
	}
	if err != nil {
		r.logError(ctx, "repository get document by id failed", "document_id", id, "error", err.Error())
		return nil, err
	}
	r.logDebug(ctx, "repository get document by id success", "document_id", id, "status", row.Status)
	return mapDocument(row), nil
}

func (r *Repository) GetDocumentContent(ctx context.Context, id int64) ([]byte, error) {
	r.logDebug(ctx, "repository get document content start", "document_id", id)
	row, err := r.queries.GetDocumentByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		r.logWarn(ctx, "repository get document content not found", "document_id", id)
		return nil, ErrNotFound
	}
	if err != nil {
		r.logError(ctx, "repository get document content failed", "document_id", id, "error", err.Error())
		return nil, err
	}
	r.logDebug(ctx, "repository get document content success", "document_id", id, "size_bytes", len(row.RawContent))
	return row.RawContent, nil
}

func (r *Repository) GetDocumentBySHA(ctx context.Context, sha string) (*domain.Document, error) {
	r.logDebug(ctx, "repository get document by sha start", "sha256", sha)
	row, err := r.queries.GetDocumentBySHA(ctx, sha)
	if errors.Is(err, sql.ErrNoRows) {
		r.logDebug(ctx, "repository get document by sha not found", "sha256", sha)
		return nil, ErrNotFound
	}
	if err != nil {
		r.logError(ctx, "repository get document by sha failed", "sha256", sha, "error", err.Error())
		return nil, err
	}
	r.logDebug(ctx, "repository get document by sha success", "sha256", sha, "document_id", row.ID)
	return mapDocument(row), nil
}

func (r *Repository) ListDocuments(ctx context.Context, limit int32) ([]domain.Document, error) {
	r.logDebug(ctx, "repository list documents start", "limit", limit)
	rows, err := r.queries.ListDocuments(ctx, limit)
	if err != nil {
		r.logError(ctx, "repository list documents failed", "limit", limit, "error", err.Error())
		return nil, err
	}
	items := make([]domain.Document, 0, len(rows))
	for _, row := range rows {
		items = append(items, *mapDocument(row))
	}
	r.logDebug(ctx, "repository list documents success", "limit", limit, "count", len(items))
	return items, nil
}

func (r *Repository) UpdateDocumentStatus(ctx context.Context, id int64, status string) error {
	r.logInfo(ctx, "repository update document status start", "document_id", id, "status", status)
	err := r.queries.UpdateDocumentStatus(ctx, reposqlc.UpdateDocumentStatusParams{
		Status: status,
		ID:     id,
	})
	if err != nil {
		r.logError(ctx, "repository update document status failed", "document_id", id, "status", status, "error", err.Error())
		return err
	}
	r.logInfo(ctx, "repository update document status success", "document_id", id, "status", status)
	return nil
}

func (r *Repository) CreateParseRun(ctx context.Context, run domain.ParseRun) (*domain.ParseRun, error) {
	r.logInfo(ctx, "repository create parse run start", "document_id", run.DocumentID, "status", run.Status, "chunk_count", len(run.Chunks))
	chunks, err := json.Marshal(run.Chunks)
	if err != nil {
		r.logError(ctx, "repository marshal parse run chunks failed", "document_id", run.DocumentID, "error", err.Error())
		return nil, err
	}
	rawMetadata, err := json.Marshal(run.RawMetadata)
	if err != nil {
		r.logError(ctx, "repository marshal parse run metadata failed", "document_id", run.DocumentID, "error", err.Error())
		return nil, err
	}
	result, err := r.queries.InsertParseRun(ctx, reposqlc.InsertParseRunParams{
		DocumentID:      run.DocumentID,
		Status:          run.Status,
		ParserName:      run.ParserName,
		ParserVersion:   run.ParserVersion,
		ErrorMessage:    run.ErrorMessage,
		PageCount:       int32(run.PageCount),
		ContentText:     run.ContentText,
		CleanedText:     run.CleanedText,
		ChunksJson:      json.RawMessage(chunks),
		RawMetadataJson: json.RawMessage(rawMetadata),
	})
	if err != nil {
		r.logError(ctx, "repository create parse run failed", "document_id", run.DocumentID, "error", err.Error())
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		r.logError(ctx, "repository create parse run last insert id failed", "document_id", run.DocumentID, "error", err.Error())
		return nil, err
	}
	row, err := r.queries.GetParseRunByID(ctx, id)
	if err != nil {
		r.logError(ctx, "repository load parse run by id failed", "parse_run_id", id, "error", err.Error())
		return nil, err
	}
	r.logInfo(ctx, "repository create parse run success", "parse_run_id", id, "document_id", run.DocumentID)
	return mapParseRun(row)
}

func (r *Repository) GetLatestParseRunByDocumentID(ctx context.Context, documentID int64) (*domain.ParseRun, error) {
	r.logDebug(ctx, "repository get latest parse run start", "document_id", documentID)
	row, err := r.queries.GetLatestParseRunByDocumentID(ctx, documentID)
	if errors.Is(err, sql.ErrNoRows) {
		r.logWarn(ctx, "repository get latest parse run not found", "document_id", documentID)
		return nil, ErrNotFound
	}
	if err != nil {
		r.logError(ctx, "repository get latest parse run failed", "document_id", documentID, "error", err.Error())
		return nil, err
	}
	r.logDebug(ctx, "repository get latest parse run success", "document_id", documentID, "parse_run_id", row.ID)
	return mapParseRun(row)
}

func (r *Repository) ReplacePlansByDocumentID(ctx context.Context, documentID int64, plans []domain.CandidatePlan) ([]domain.CandidatePlan, error) {
	r.logInfo(ctx, "repository replace plans start", "document_id", documentID, "plan_count", len(plans))
	if err := r.queries.DeletePlansByDocumentID(ctx, documentID); err != nil {
		r.logError(ctx, "repository delete plans by document failed", "document_id", documentID, "error", err.Error())
		return nil, err
	}
	items := make([]domain.CandidatePlan, 0, len(plans))
	for _, plan := range plans {
		item, err := r.CreatePlan(ctx, plan)
		if err != nil {
			r.logError(ctx, "repository replace plans create plan failed", "document_id", documentID, "symbol", plan.Symbol, "error", err.Error())
			return nil, err
		}
		items = append(items, *item)
	}
	r.logInfo(ctx, "repository replace plans success", "document_id", documentID, "plan_count", len(items))
	return items, nil
}

func (r *Repository) CreatePlan(ctx context.Context, plan domain.CandidatePlan) (*domain.CandidatePlan, error) {
	r.logInfo(ctx, "repository create plan start", "document_id", plan.DocumentID, "symbol", plan.Symbol, "direction", plan.Direction, "status", plan.Status)
	risks, err := json.Marshal(plan.Risks)
	if err != nil {
		r.logError(ctx, "repository marshal plan risks failed", "document_id", plan.DocumentID, "symbol", plan.Symbol, "error", err.Error())
		return nil, err
	}
	evidence, err := json.Marshal(plan.Evidence)
	if err != nil {
		r.logError(ctx, "repository marshal plan evidence failed", "document_id", plan.DocumentID, "symbol", plan.Symbol, "error", err.Error())
		return nil, err
	}
	result, err := r.queries.InsertPlan(ctx, reposqlc.InsertPlanParams{
		DocumentID:     plan.DocumentID,
		ParseRunID:     plan.ParseRunID,
		Analyst:        plan.Analyst,
		Institution:    plan.Institution,
		Symbol:         plan.Symbol,
		AssetType:      plan.AssetType,
		Market:         plan.Market,
		Strategy:       plan.Strategy,
		Direction:      plan.Direction,
		TradeDate:      plan.TradeDate,
		ReferencePrice: plan.ReferencePrice,
		EntryPrice:     plan.EntryPrice,
		StopLoss:       plan.StopLoss,
		TakeProfit:     plan.TakeProfit,
		PositionPct:    plan.PositionPct,
		Confidence:     plan.Confidence,
		Status:         plan.Status,
		Thesis:         plan.Thesis,
		RisksJson:      json.RawMessage(risks),
		EvidenceJson:   json.RawMessage(evidence),
		PricingNote:    plan.PricingNote,
		ConfigVersion:  plan.ConfigVersion,
		RuleVersion:    plan.RuleVersion,
	})
	if err != nil {
		r.logError(ctx, "repository create plan failed", "document_id", plan.DocumentID, "symbol", plan.Symbol, "error", err.Error())
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		r.logError(ctx, "repository create plan last insert id failed", "document_id", plan.DocumentID, "symbol", plan.Symbol, "error", err.Error())
		return nil, err
	}
	row, err := r.queries.GetPlanByID(ctx, id)
	if err != nil {
		r.logError(ctx, "repository load plan by id failed", "plan_id", id, "error", err.Error())
		return nil, err
	}
	r.logInfo(ctx, "repository create plan success", "plan_id", id, "document_id", plan.DocumentID, "symbol", plan.Symbol)
	return mapPlan(row)
}

func (r *Repository) ListPlans(ctx context.Context, limit int32) ([]domain.CandidatePlan, error) {
	r.logDebug(ctx, "repository list plans start", "limit", limit)
	rows, err := r.queries.ListPlans(ctx, limit)
	if err != nil {
		r.logError(ctx, "repository list plans failed", "limit", limit, "error", err.Error())
		return nil, err
	}
	items := make([]domain.CandidatePlan, 0, len(rows))
	for _, row := range rows {
		item, err := mapPlan(row)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	r.logDebug(ctx, "repository list plans success", "limit", limit, "count", len(items))
	return items, nil
}

func (r *Repository) ListPlansByDocumentID(ctx context.Context, documentID int64) ([]domain.CandidatePlan, error) {
	r.logDebug(ctx, "repository list plans by document start", "document_id", documentID)
	rows, err := r.queries.ListPlansByDocumentID(ctx, documentID)
	if err != nil {
		r.logError(ctx, "repository list plans by document failed", "document_id", documentID, "error", err.Error())
		return nil, err
	}
	items := make([]domain.CandidatePlan, 0, len(rows))
	for _, row := range rows {
		item, err := mapPlan(row)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	r.logDebug(ctx, "repository list plans by document success", "document_id", documentID, "count", len(items))
	return items, nil
}

func (r *Repository) logDebug(ctx context.Context, msg string, args ...any) {
	if r.logger != nil {
		r.logger.DebugContext(ctx, msg, args...)
	}
}

func (r *Repository) logInfo(ctx context.Context, msg string, args ...any) {
	if r.logger != nil {
		r.logger.InfoContext(ctx, msg, args...)
	}
}

func (r *Repository) logWarn(ctx context.Context, msg string, args ...any) {
	if r.logger != nil {
		r.logger.WarnContext(ctx, msg, args...)
	}
}

func (r *Repository) logError(ctx context.Context, msg string, args ...any) {
	if r.logger != nil {
		r.logger.ErrorContext(ctx, msg, args...)
	}
}

func mapConfigSnapshot(row reposqlc.ConfigSnapshot) *domain.ConfigSnapshot {
	return &domain.ConfigSnapshot{
		ID:            row.ID,
		ConfigVersion: row.ConfigVersion,
		Source:        row.Source,
		SHA256:        row.Sha256,
		RawJSON:       string(row.RawJson),
		CreatedAt:     row.CreatedAt.UTC(),
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
		Status:        row.Status,
		ConfigVersion: row.ConfigVersion,
		CreatedAt:     row.CreatedAt.UTC(),
		UpdatedAt:     row.UpdatedAt.UTC(),
	}
}

func mapParseRun(row reposqlc.ParseRun) (*domain.ParseRun, error) {
	var chunks []domain.Chunk
	rawMetadata := make(map[string]any)
	if err := json.Unmarshal(row.ChunksJson, &chunks); err != nil {
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
		ErrorMessage:  row.ErrorMessage,
		PageCount:     int(row.PageCount),
		ContentText:   row.ContentText,
		CleanedText:   row.CleanedText,
		Chunks:        chunks,
		RawMetadata:   rawMetadata,
		CreatedAt:     row.CreatedAt.UTC(),
		UpdatedAt:     row.UpdatedAt.UTC(),
	}, nil
}

func mapPlan(row reposqlc.TradeCandidatePlan) (*domain.CandidatePlan, error) {
	var risks []string
	var evidence []domain.EvidenceSpan
	if err := json.Unmarshal(row.RisksJson, &risks); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(row.EvidenceJson, &evidence); err != nil {
		return nil, err
	}
	return &domain.CandidatePlan{
		ID:             row.ID,
		DocumentID:     row.DocumentID,
		ParseRunID:     row.ParseRunID,
		Analyst:        row.Analyst,
		Institution:    row.Institution,
		Symbol:         row.Symbol,
		AssetType:      row.AssetType,
		Market:         row.Market,
		Strategy:       row.Strategy,
		Direction:      row.Direction,
		TradeDate:      row.TradeDate.UTC(),
		ReferencePrice: row.ReferencePrice,
		EntryPrice:     row.EntryPrice,
		StopLoss:       row.StopLoss,
		TakeProfit:     row.TakeProfit,
		PositionPct:    row.PositionPct,
		Confidence:     row.Confidence,
		Status:         row.Status,
		Thesis:         row.Thesis,
		Risks:          risks,
		Evidence:       evidence,
		PricingNote:    row.PricingNote,
		ConfigVersion:  row.ConfigVersion,
		RuleVersion:    row.RuleVersion,
		CreatedAt:      row.CreatedAt.UTC(),
		UpdatedAt:      row.UpdatedAt.UTC(),
	}, nil
}
