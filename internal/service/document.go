package service

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/domain"
	"finance-sys/internal/repository"
	"finance-sys/internal/utils"
)

type documentParser interface {
	Parse(ctx context.Context, fileName string, content []byte, cfg config.DocumentConfig) (domain.ParseRun, error)
}

type planAnalyzer interface {
	Analyze(ctx context.Context, document domain.Document, parsed domain.ParseRun) ([]domain.PlanIntent, error)
}

type ruleEngine interface {
	Generate(intent domain.PlanIntent, cfg config.RulesConfig, tradeDate time.Time, configVersion int64) domain.CandidatePlan
}

type DocumentService struct {
	repo     *repository.Repository
	runtime  *config.Runtime
	parser   documentParser
	analyzer planAnalyzer
	rules    ruleEngine
	logger   *slog.Logger
}

func NewDocumentService(
	repo *repository.Repository,
	runtime *config.Runtime,
	parser documentParser,
	analyzer planAnalyzer,
	rules ruleEngine,
	logger *slog.Logger,
) *DocumentService {
	return &DocumentService{
		repo:     repo,
		runtime:  runtime,
		parser:   parser,
		analyzer: analyzer,
		rules:    rules,
		logger:   logger,
	}
}

func (s *DocumentService) IngestDocument(ctx context.Context, request domain.DocumentIngestRequest) (*domain.Document, bool, error) {
	cfg := s.currentConfig()
	if err := s.validateUpload(request.FileName, cfg.Document); err != nil {
		return nil, false, err
	}

	sha := utils.SHA256Hex(request.Content)
	if cfg.Document.SHA256Dedup {
		existing, err := s.repo.GetDocumentBySHA(ctx, sha)
		switch err {
		case nil:
			return existing, true, nil
		case repository.ErrNotFound:
		default:
			return nil, false, err
		}
	}

	request = s.applyDefaults(request, cfg)
	document, err := s.repo.CreateDocument(ctx, request, sha, cfg.Meta.ConfigVersion)
	if err != nil {
		return nil, false, err
	}
	return document, false, nil
}

func (s *DocumentService) AnalyzeDocument(ctx context.Context, documentID int64) ([]domain.CandidatePlan, error) {
	cfg := s.currentConfig()
	document, err := s.repo.GetDocumentByID(ctx, documentID)
	if err != nil {
		return nil, err
	}

	content, err := s.repo.GetDocumentContent(ctx, documentID)
	if err != nil {
		return nil, err
	}

	parsed, parseErr := s.parser.Parse(ctx, document.FileName, content, cfg.Document)
	parsed.DocumentID = document.ID
	parseRun, err := s.repo.CreateParseRun(ctx, parsed)
	if err != nil {
		return nil, err
	}
	if parseErr != nil || parseRun.Status == "FAILED" {
		_ = s.repo.UpdateDocumentStatus(ctx, document.ID, "FAILED")
		return nil, parseErr
	}
	if err := s.repo.UpdateDocumentStatus(ctx, document.ID, "PARSED"); err != nil {
		return nil, err
	}

	intents, err := s.analyzer.Analyze(ctx, *document, *parseRun)
	if err != nil {
		_ = s.repo.UpdateDocumentStatus(ctx, document.ID, "FAILED")
		return nil, err
	}

	tradeDate := s.tradeDate(cfg)
	plans := make([]domain.CandidatePlan, 0, len(intents))
	for _, intent := range intents {
		plan := s.rules.Generate(intent, cfg.Rules, tradeDate, cfg.Meta.ConfigVersion)
		plan.DocumentID = document.ID
		plan.ParseRunID = parseRun.ID
		plans = append(plans, plan)
	}

	savedPlans, err := s.repo.ReplacePlansByDocumentID(ctx, document.ID, plans)
	if err != nil {
		return nil, err
	}
	if err := s.repo.UpdateDocumentStatus(ctx, document.ID, "PLANNED"); err != nil {
		return nil, err
	}
	return savedPlans, nil
}

func (s *DocumentService) ListPlansByDocumentID(ctx context.Context, documentID int64) ([]domain.CandidatePlan, error) {
	return s.repo.ListPlansByDocumentID(ctx, documentID)
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
	if request.SourceType == "" {
		request.SourceType = cfg.Document.SourceDefaults.SourceType
	}
	if request.SourceName == "" {
		request.SourceName = cfg.Document.SourceDefaults.SourceName
	}
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
			return nil
		}
	}
	return fmt.Errorf("unsupported file extension %s", ext)
}
