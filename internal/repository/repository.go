package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"

	"finance-sys/internal/domain"
	reposqlc "finance-sys/internal/repository/sqlc"
)

var ErrNotFound = errors.New("repository: not found")

type Repository struct {
	db      *sql.DB
	queries *reposqlc.Queries
}

func New(db *sql.DB) *Repository {
	return &Repository{
		db:      db,
		queries: reposqlc.New(db),
	}
}

func (r *Repository) Ping(ctx context.Context) error {
	return r.db.PingContext(ctx)
}

func (r *Repository) InsertConfigSnapshot(ctx context.Context, snapshot *domain.ConfigSnapshot) (*domain.ConfigSnapshot, error) {
	result, err := r.queries.InsertConfigSnapshot(ctx, reposqlc.InsertConfigSnapshotParams{
		ConfigVersion: snapshot.ConfigVersion,
		Source:        snapshot.Source,
		Sha256:        snapshot.SHA256,
		RawJson:       json.RawMessage(snapshot.RawJSON),
	})
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	row, err := r.queries.GetConfigSnapshotByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return mapConfigSnapshot(row), nil
}

func (r *Repository) CreateDocument(ctx context.Context, request domain.DocumentIngestRequest, sha256Value string, configVersion int64) (*domain.Document, error) {
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
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	row, err := r.queries.GetDocumentByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return mapDocument(row), nil
}

func (r *Repository) GetDocumentByID(ctx context.Context, id int64) (*domain.Document, error) {
	row, err := r.queries.GetDocumentByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return mapDocument(row), nil
}

func (r *Repository) GetDocumentContent(ctx context.Context, id int64) ([]byte, error) {
	row, err := r.queries.GetDocumentByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return row.RawContent, nil
}

func (r *Repository) GetDocumentBySHA(ctx context.Context, sha string) (*domain.Document, error) {
	row, err := r.queries.GetDocumentBySHA(ctx, sha)
	if errors.Is(err, sql.ErrNoRows) {
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

func (r *Repository) UpdateDocumentStatus(ctx context.Context, id int64, status string) error {
	return r.queries.UpdateDocumentStatus(ctx, reposqlc.UpdateDocumentStatusParams{
		Status: status,
		ID:     id,
	})
}

func (r *Repository) CreateParseRun(ctx context.Context, run domain.ParseRun) (*domain.ParseRun, error) {
	chunks, err := json.Marshal(run.Chunks)
	if err != nil {
		return nil, err
	}
	rawMetadata, err := json.Marshal(run.RawMetadata)
	if err != nil {
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
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	row, err := r.queries.GetParseRunByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return mapParseRun(row)
}

func (r *Repository) GetLatestParseRunByDocumentID(ctx context.Context, documentID int64) (*domain.ParseRun, error) {
	row, err := r.queries.GetLatestParseRunByDocumentID(ctx, documentID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return mapParseRun(row)
}

func (r *Repository) ReplacePlansByDocumentID(ctx context.Context, documentID int64, plans []domain.CandidatePlan) ([]domain.CandidatePlan, error) {
	if err := r.queries.DeletePlansByDocumentID(ctx, documentID); err != nil {
		return nil, err
	}
	items := make([]domain.CandidatePlan, 0, len(plans))
	for _, plan := range plans {
		item, err := r.CreatePlan(ctx, plan)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, nil
}

func (r *Repository) CreatePlan(ctx context.Context, plan domain.CandidatePlan) (*domain.CandidatePlan, error) {
	risks, err := json.Marshal(plan.Risks)
	if err != nil {
		return nil, err
	}
	evidence, err := json.Marshal(plan.Evidence)
	if err != nil {
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
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	row, err := r.queries.GetPlanByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return mapPlan(row)
}

func (r *Repository) ListPlans(ctx context.Context, limit int32) ([]domain.CandidatePlan, error) {
	rows, err := r.queries.ListPlans(ctx, limit)
	if err != nil {
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
	return items, nil
}

func (r *Repository) ListPlansByDocumentID(ctx context.Context, documentID int64) ([]domain.CandidatePlan, error) {
	rows, err := r.queries.ListPlansByDocumentID(ctx, documentID)
	if err != nil {
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
	return items, nil
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
