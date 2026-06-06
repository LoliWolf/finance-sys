package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/dal"
	"finance-sys/internal/domain"
	"finance-sys/internal/domain/db_model"
	"finance-sys/internal/llm"
	"finance-sys/internal/utils"

	"gorm.io/gorm"
)

type documentParser interface {
	Parse(ctx context.Context, fileName string, content []byte, cfg config.DocumentConfig) (domain.ParseRun, error)
}

type planAnalyzer interface {
	Analyze(ctx context.Context, document domain.Document, parsed domain.ParseRun) ([]domain.PlanIntent, error)
}

type ruleEngine interface {
	Generate(intent domain.TrackablePlanIntent, cfg config.RulesConfig, tradeDate time.Time, configVersion int64) domain.CandidatePlan
}

type DocumentService struct {
	db        *gorm.DB
	runtime   *config.Runtime
	parser    documentParser
	analyzer  planAnalyzer
	assembler *CandidateAssembler
	rules     ruleEngine
	observer  *ResolutionObserver
	logger    *slog.Logger
}

func NewDocumentService(
	db *gorm.DB,
	runtime *config.Runtime,
	parser documentParser,
	analyzer planAnalyzer,
	assembler *CandidateAssembler,
	rules ruleEngine,
	logger *slog.Logger,
) *DocumentService {
	return &DocumentService{
		db:        db,
		runtime:   runtime,
		parser:    parser,
		analyzer:  analyzer,
		assembler: assembler,
		rules:     rules,
		observer:  NewResolutionObserver(runtime, logger),
		logger:    logger,
	}
}

func (s *DocumentService) IngestDocument(ctx context.Context, request domain.DocumentIngestRequest) (*domain.Document, bool, error) {
	cfg := s.currentConfig()
	s.logger.InfoContext(ctx, "document service ingest start", "file_name", request.FileName, "size_bytes", len(request.Content))
	if err := s.validateUpload(request.FileName, cfg.Document); err != nil {
		s.logger.WarnContext(ctx, "document service ingest validation failed", "file_name", request.FileName, "error", err.Error())
		return nil, false, err
	}

	sha := utils.SHA256Hex(request.Content)
	s.logger.DebugContext(ctx, "document service ingest computed sha", "file_name", request.FileName, "sha256", sha)
	if cfg.Document.SHA256Dedup {
		existingModel, err := dal.Documents.QueryBySHA(ctx, s.db, sha)
		switch err {
		case nil:
			existing := mapDocument(existingModel)
			s.logger.InfoContext(ctx, "document service ingest duplicate hit", "file_name", request.FileName, "document_id", existing.ID)
			return existing, true, nil
		case dal.ErrNotFound:
		default:
			s.logger.ErrorContext(ctx, "document service ingest duplicate lookup failed", "file_name", request.FileName, "error", err.Error())
			return nil, false, err
		}
	}

	request = s.applyDefaults(request, cfg)
	documentModel := documentIngestToModel(request, sha, cfg.Meta.ConfigVersion)
	err := dal.Documents.Create(ctx, s.db, documentModel)
	if err != nil {
		s.logger.ErrorContext(ctx, "document service ingest create document failed", "file_name", request.FileName, "error", err.Error())
		return nil, false, err
	}
	document := mapDocument(documentModel)
	s.logger.InfoContext(ctx, "document service ingest success", "document_id", document.ID, "file_name", request.FileName)
	return document, false, nil
}

func (s *DocumentService) AnalyzeDocument(ctx context.Context, documentID int64) ([]domain.CandidatePlan, error) {
	cfg := s.currentConfig()
	s.logger.InfoContext(ctx, "document service analyze start", "document_id", documentID)
	documentModel, err := dal.Documents.QueryByID(ctx, s.db, documentID)
	if err != nil {
		s.logger.ErrorContext(ctx, "document service analyze load document failed", "document_id", documentID, "error", err.Error())
		return nil, err
	}
	document := mapDocument(documentModel)

	content, err := dal.Documents.QueryContentByID(ctx, s.db, documentID)
	if err != nil {
		s.logger.ErrorContext(ctx, "document service analyze load content failed", "document_id", documentID, "error", err.Error())
		return nil, err
	}
	s.logger.InfoContext(ctx, "document service analyze content loaded", "document_id", documentID, "file_name", document.FileName, "size_bytes", len(content))

	documentCfg := cfg.Document
	documentCfg.PDFOCR.Enabled = document.PDFOCREnabled
	parsed, parseErr := s.parser.Parse(ctx, document.FileName, content, documentCfg)
	parsed.DocumentID = document.ID
	parseRunModel, err := parseRunToModel(parsed)
	if err != nil {
		s.logger.ErrorContext(ctx, "document service analyze convert parse run failed", "document_id", documentID, "error", err.Error())
		return nil, err
	}
	if err := dal.ParseRuns.Create(ctx, s.db, parseRunModel); err != nil {
		s.logger.ErrorContext(ctx, "document service analyze create parse run failed", "document_id", documentID, "error", err.Error())
		return nil, err
	}
	parseRun, err := mapParseRun(parseRunModel)
	if err != nil {
		s.logger.ErrorContext(ctx, "document service analyze map parse run failed", "document_id", documentID, "error", err.Error())
		return nil, err
	}
	resolutionRun, err := s.observer.Start(ctx, s.db, document, parseRun)
	if err != nil {
		s.logger.ErrorContext(ctx, "document service analyze create resolution run failed", "document_id", documentID, "parse_run_id", parseRun.ID, "error", err.Error())
		return nil, err
	}
	if parseErr != nil || parseRun.Status == domain.ParseRunStatusFailed {
		if parseErr == nil {
			parseErr = errors.New(parseRun.ErrorMessage)
		}
		s.logger.ErrorContext(ctx, "document service analyze parse failed", "document_id", documentID, "parse_run_id", parseRun.ID, "error", parseRun.ErrorMessage)
		if obsErr := s.observer.FinishFailed(ctx, s.db, resolutionRun, AnalysisObservation{AgentMode: observationAgentMode(cfg), Route: string(domain.ResolutionRouteLocalOnly)}, nil, nil, parseErr, "PARSE_FAILED"); obsErr != nil {
			s.logger.ErrorContext(ctx, "document service analyze finish resolution failed after parse failure", "document_id", documentID, "parse_run_id", parseRun.ID, "error", obsErr.Error())
			return nil, obsErr
		}
		_ = dal.Documents.UpdateStatusByID(ctx, s.db, document.ID, string(domain.DocumentStatusFailed))
		return nil, parseErr
	}
	if err := dal.Documents.UpdateStatusByID(ctx, s.db, document.ID, string(domain.DocumentStatusParsed)); err != nil {
		s.logger.ErrorContext(ctx, "document service analyze update status parsed failed", "document_id", documentID, "error", err.Error())
		return nil, err
	}
	s.logger.InfoContext(ctx, "document service analyze parse success", "document_id", documentID, "parse_run_id", parseRun.ID, "chunk_count", len(parseRun.Chunks))

	analysis, err := s.analyzeWithObservation(ctx, *document, *parseRun)
	intents := analysis.Intents
	if err != nil {
		s.logger.ErrorContext(ctx, "document service analyze llm failed", "document_id", documentID, "parse_run_id", parseRun.ID, "error", err.Error())
		if obsErr := s.observer.FinishFailed(ctx, s.db, resolutionRun, analysis, intents, nil, err, ""); obsErr != nil {
			s.logger.ErrorContext(ctx, "document service analyze finish resolution failed after analyzer failure", "document_id", documentID, "parse_run_id", parseRun.ID, "error", obsErr.Error())
			return nil, obsErr
		}
		_ = dal.Documents.UpdateStatusByID(ctx, s.db, document.ID, string(domain.DocumentStatusFailed))
		return nil, err
	}
	s.logger.InfoContext(ctx, "document service analyze llm success", "document_id", documentID, "parse_run_id", parseRun.ID, "intent_count", len(intents))

	for _, intent := range intents {
		if err := llm.ValidateIntent(intent); err != nil {
			s.logger.ErrorContext(ctx, "document service analyze invalid intent", "document_id", documentID, "parse_run_id", parseRun.ID, "symbol", intent.Symbol, "error", err.Error())
			if obsErr := s.observer.FinishFailed(ctx, s.db, resolutionRun, analysis, intents, nil, err, "INVALID_PLAN_INTENT"); obsErr != nil {
				s.logger.ErrorContext(ctx, "document service analyze finish resolution failed after invalid intent", "document_id", documentID, "parse_run_id", parseRun.ID, "error", obsErr.Error())
				return nil, obsErr
			}
			_ = dal.Documents.UpdateStatusByID(ctx, s.db, document.ID, string(domain.DocumentStatusFailed))
			return nil, fmt.Errorf("invalid plan intent: %w", err)
		}
	}
	trackableIntents, resolutions, err := s.assembler.Assemble(ctx, intents)
	for _, resolution := range resolutions {
		s.logger.InfoContext(ctx, "document service analyze instrument resolved", "document_id", documentID, "parse_run_id", parseRun.ID, "raw_symbol", resolution.RawSymbol, "status", resolution.Status, "target_kind", resolution.TargetKind, "candidate_count", len(resolution.Candidates), "reason", resolution.Reason)
	}
	if err != nil {
		s.logger.ErrorContext(ctx, "document service analyze candidate assembly failed", "document_id", documentID, "parse_run_id", parseRun.ID, "error", err.Error())
		if obsErr := s.observer.FinishFailed(ctx, s.db, resolutionRun, analysis, intents, resolutions, err, ""); obsErr != nil {
			s.logger.ErrorContext(ctx, "document service analyze finish resolution failed after candidate assembly failure", "document_id", documentID, "parse_run_id", parseRun.ID, "error", obsErr.Error())
			return nil, obsErr
		}
		_ = dal.Documents.UpdateStatusByID(ctx, s.db, document.ID, string(domain.DocumentStatusFailed))
		return nil, err
	}
	s.logger.InfoContext(ctx, "document service analyze candidate assembly success", "document_id", documentID, "parse_run_id", parseRun.ID, "trackable_intent_count", len(trackableIntents), "raw_intent_count", len(intents))

	tradeDate := s.tradeDate(cfg)
	plans := make([]domain.CandidatePlan, 0, len(trackableIntents))
	for _, intent := range trackableIntents {
		s.logger.DebugContext(ctx, "document service analyze generate plan", "document_id", documentID, "ts_code", intent.TSCode, "raw_symbol", intent.RawSymbol, "direction", intent.Direction, "confidence", intent.Confidence)
		plan := s.rules.Generate(intent, cfg.Rules, tradeDate, cfg.Meta.ConfigVersion)
		plan.DocumentID = document.ID
		plan.ParseRunID = parseRun.ID
		plans = append(plans, plan)
	}

	savedPlans, err := s.replacePlansByDocumentID(ctx, *document, plans, resolutionRun, analysis, intents, resolutions)
	if err != nil {
		s.logger.ErrorContext(ctx, "document service analyze replace plans failed", "document_id", documentID, "error", err.Error())
		return nil, err
	}
	if err := dal.Documents.UpdateStatusByID(ctx, s.db, document.ID, string(domain.DocumentStatusPlanned)); err != nil {
		s.logger.ErrorContext(ctx, "document service analyze update status planned failed", "document_id", documentID, "error", err.Error())
		return nil, err
	}
	s.logger.InfoContext(ctx, "document service analyze completed", "document_id", documentID, "plan_count", len(savedPlans), "trade_date", tradeDate.Format(time.DateOnly))
	return savedPlans, nil
}

func (s *DocumentService) ListPlansByDocumentID(ctx context.Context, documentID int64) ([]domain.CandidatePlan, error) {
	s.logger.InfoContext(ctx, "document service list plans by document", "document_id", documentID)
	rows, err := dal.TradeCandidatePlans.QueryByDocumentID(ctx, s.db, documentID)
	if err != nil {
		return nil, err
	}
	return mapPlanRows(rows)
}

func (s *DocumentService) ListDocuments(ctx context.Context, limit int) ([]domain.Document, error) {
	rows, err := dal.Documents.QueryLatest(ctx, s.db, limit)
	if err != nil {
		return nil, err
	}
	items := make([]domain.Document, 0, len(rows))
	for i := range rows {
		items = append(items, *mapDocument(&rows[i]))
	}
	return items, nil
}

func (s *DocumentService) GetDocumentByID(ctx context.Context, documentID int64) (*domain.Document, error) {
	row, err := dal.Documents.QueryByID(ctx, s.db, documentID)
	if err != nil {
		return nil, err
	}
	return mapDocument(row), nil
}

func (s *DocumentService) ListPlans(ctx context.Context, limit int) ([]domain.CandidatePlan, error) {
	rows, err := dal.TradeCandidatePlans.QueryLatest(ctx, s.db, limit)
	if err != nil {
		return nil, err
	}
	return mapPlanRows(rows)
}

func (s *DocumentService) GetLatestParseRunByDocumentID(ctx context.Context, documentID int64) (*domain.ParseRun, error) {
	row, err := dal.ParseRuns.QueryLatestByDocumentID(ctx, s.db, documentID)
	if err != nil {
		return nil, err
	}
	return mapParseRun(row)
}

func (s *DocumentService) ListResolutionRunsByDocumentID(ctx context.Context, documentID int64) ([]domain.ResolutionRun, error) {
	return s.observer.ListRunsByDocumentID(ctx, s.db, documentID)
}

func (s *DocumentService) GetResolutionRunByID(ctx context.Context, id int64) (*domain.ResolutionRun, error) {
	return s.observer.GetRunByID(ctx, s.db, id)
}

func (s *DocumentService) ListActiveUntrackableTargetsByDocumentID(ctx context.Context, documentID int64) ([]domain.UntrackableTarget, error) {
	return s.observer.ListActiveUntrackableTargetsByDocumentID(ctx, s.db, documentID)
}

func (s *DocumentService) currentConfig() *config.Config {
	return s.runtime.Config()
}

func (s *DocumentService) tradeDate(cfg *config.Config) time.Time {
	loc := utils.MustLocation(cfg.Meta.Timezone)
	base := time.Now().In(loc)
	return time.Date(base.Year(), base.Month(), base.Day()+cfg.Rules.TradeDateOffsetDays, 0, 0, 0, 0, loc)
}

func (s *DocumentService) applyDefaults(request domain.DocumentIngestRequest, cfg *config.Config) domain.DocumentIngestRequest {
	if request.Author == "" {
		request.Author = cfg.Document.SourceDefaults.Author
	}
	if request.Institution == "" {
		request.Institution = cfg.Document.SourceDefaults.Institution
	}
	if request.Title == "" {
		request.Title = strings.TrimSuffix(request.FileName, filepath.Ext(request.FileName))
	}
	return request
}

func (s *DocumentService) validateUpload(fileName string, cfg config.DocumentConfig) error {
	ext := strings.ToLower(filepath.Ext(fileName))
	for _, allowed := range cfg.AllowedExtensions {
		if strings.EqualFold(allowed, ext) {
			s.logger.Debug("document service validate upload success", "file_name", fileName, "extension", ext)
			return nil
		}
	}
	s.logger.Warn("document service validate upload rejected", "file_name", fileName, "extension", ext)
	return fmt.Errorf("unsupported file extension %s", ext)
}

func (s *DocumentService) analyzeWithObservation(ctx context.Context, document domain.Document, parseRun domain.ParseRun) (AnalysisObservation, error) {
	if observed, ok := s.analyzer.(interface {
		AnalyzeWithObservation(context.Context, domain.Document, domain.ParseRun) (AnalysisObservation, error)
	}); ok {
		return observed.AnalyzeWithObservation(ctx, document, parseRun)
	}
	intents, err := s.analyzer.Analyze(ctx, document, parseRun)
	result := AnalysisObservation{Intents: intents, AgentMode: observationAgentMode(s.currentConfig()), Route: string(domain.ResolutionRouteLegacyLLM)}
	if err != nil {
		result.LegacyError = err.Error()
	}
	return result, err
}

func (s *DocumentService) replacePlansByDocumentID(ctx context.Context, document domain.Document, plans []domain.CandidatePlan, resolutionRun *db_model.InstrumentResolutionRun, analysis AnalysisObservation, intents []domain.PlanIntent, resolutions []domain.InstrumentResolution) ([]domain.CandidatePlan, error) {
	items := make([]domain.CandidatePlan, 0, len(plans))
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := dal.TradeCandidatePlans.DeleteByDocumentID(ctx, tx, document.ID); err != nil {
			return err
		}
		for _, plan := range plans {
			model, err := candidatePlanToModel(plan)
			if err != nil {
				return err
			}
			if err := dal.TradeCandidatePlans.Create(ctx, tx, model); err != nil {
				return err
			}
			item, err := mapPlan(model)
			if err != nil {
				return err
			}
			if _, err := s.upsertRecommendationEventForPlan(ctx, tx, document, *item); err != nil {
				return err
			}
			items = append(items, *item)
		}
		if err := s.observer.FinishSucceeded(ctx, tx, resolutionRun, analysis, intents, resolutions, items); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

func documentIngestToModel(request domain.DocumentIngestRequest, sha256Value string, configVersion int64) *db_model.Document {
	return &db_model.Document{
		Author:        request.Author,
		Institution:   request.Institution,
		Title:         request.Title,
		FileName:      request.FileName,
		Sha256:        sha256Value,
		PdfOcrEnabled: request.PDFUseOCR,
		Status:        string(domain.DocumentStatusIngested),
		ConfigVersion: configVersion,
		RawContent:    request.Content,
	}
}

func mapDocument(row *db_model.Document) *domain.Document {
	return &domain.Document{
		ID:            row.ID,
		Author:        row.Author,
		Institution:   row.Institution,
		Title:         row.Title,
		FileName:      row.FileName,
		SHA256:        row.Sha256,
		PDFOCREnabled: row.PdfOcrEnabled,
		Status:        domain.DocumentStatus(row.Status),
		ConfigVersion: row.ConfigVersion,
		CreatedAt:     row.CreatedAt.UTC(),
		UpdatedAt:     row.UpdatedAt.UTC(),
	}
}

func parseRunToModel(run domain.ParseRun) (*db_model.ParseRun, error) {
	chunks, err := json.Marshal(run.Chunks)
	if err != nil {
		return nil, err
	}
	rawMetadata, err := json.Marshal(run.RawMetadata)
	if err != nil {
		return nil, err
	}
	return &db_model.ParseRun{
		DocumentID:      run.DocumentID,
		Status:          string(run.Status),
		ParserName:      string(run.ParserName),
		ParserVersion:   run.ParserVersion,
		ErrorMessage:    run.ErrorMessage,
		CleanedText:     run.CleanedText,
		ChunksJSON:      chunks,
		RawMetadataJSON: rawMetadata,
	}, nil
}

func mapParseRun(row *db_model.ParseRun) (*domain.ParseRun, error) {
	var chunks []domain.Chunk
	rawMetadata := make(map[string]any)
	if err := json.Unmarshal(row.ChunksJSON, &chunks); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(row.RawMetadataJSON, &rawMetadata); err != nil {
		return nil, err
	}
	return &domain.ParseRun{
		ID:            row.ID,
		DocumentID:    row.DocumentID,
		Status:        domain.ParseRunStatus(row.Status),
		ParserName:    domain.ParserName(row.ParserName),
		ParserVersion: row.ParserVersion,
		ErrorMessage:  row.ErrorMessage,
		CleanedText:   row.CleanedText,
		Chunks:        chunks,
		RawMetadata:   rawMetadata,
		CreatedAt:     row.CreatedAt.UTC(),
		UpdatedAt:     row.UpdatedAt.UTC(),
	}, nil
}

func candidatePlanToModel(plan domain.CandidatePlan) (*db_model.TradeCandidatePlan, error) {
	risks, err := json.Marshal(plan.Risks)
	if err != nil {
		return nil, err
	}
	evidence, err := json.Marshal(plan.Evidence)
	if err != nil {
		return nil, err
	}
	return &db_model.TradeCandidatePlan{
		DocumentID:     plan.DocumentID,
		ParseRunID:     plan.ParseRunID,
		Analyst:        plan.Analyst,
		Institution:    plan.Institution,
		Symbol:         plan.Symbol,
		AssetType:      string(plan.AssetType),
		Market:         string(plan.Market),
		Strategy:       string(plan.Strategy),
		Direction:      string(plan.Direction),
		TradeDate:      plan.TradeDate,
		ReferencePrice: plan.ReferencePrice,
		EntryPrice:     plan.EntryPrice,
		StopLoss:       plan.StopLoss,
		TakeProfit:     plan.TakeProfit,
		PositionPct:    plan.PositionPct,
		Confidence:     plan.Confidence,
		Status:         string(plan.Status),
		Thesis:         plan.Thesis,
		RisksJSON:      risks,
		EvidenceJSON:   evidence,
		PricingNote:    plan.PricingNote,
		ConfigVersion:  plan.ConfigVersion,
		RuleVersion:    plan.RuleVersion,
	}, nil
}

func mapPlanRows(rows []db_model.TradeCandidatePlan) ([]domain.CandidatePlan, error) {
	items := make([]domain.CandidatePlan, 0, len(rows))
	for i := range rows {
		item, err := mapPlan(&rows[i])
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, nil
}

func mapPlan(row *db_model.TradeCandidatePlan) (*domain.CandidatePlan, error) {
	var risks []string
	var evidence []domain.EvidenceSpan
	if err := json.Unmarshal(row.RisksJSON, &risks); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(row.EvidenceJSON, &evidence); err != nil {
		return nil, err
	}
	return &domain.CandidatePlan{
		ID:             row.ID,
		DocumentID:     row.DocumentID,
		ParseRunID:     row.ParseRunID,
		Analyst:        row.Analyst,
		Institution:    row.Institution,
		Symbol:         row.Symbol,
		AssetType:      domain.AssetType(row.AssetType),
		Market:         domain.Market(row.Market),
		Strategy:       domain.RuleStrategy(row.Strategy),
		Direction:      domain.TradeDirection(row.Direction),
		TradeDate:      row.TradeDate.UTC(),
		ReferencePrice: row.ReferencePrice,
		EntryPrice:     row.EntryPrice,
		StopLoss:       row.StopLoss,
		TakeProfit:     row.TakeProfit,
		PositionPct:    row.PositionPct,
		Confidence:     row.Confidence,
		Status:         domain.CandidatePlanStatus(row.Status),
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
