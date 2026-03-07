package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/domain"
	"finance-sys/internal/repository"
	"finance-sys/internal/storage"
	"finance-sys/internal/utils"
)

type documentParser interface {
	Parse(ctx context.Context, fileName string, content []byte, cfg config.DocumentParsingConfig) (domain.ParseRun, error)
}

type signalExtractor interface {
	Extract(ctx context.Context, document domain.Document, parsed domain.ParseRun) ([]domain.ExtractedSignal, error)
}

type ruleEngine interface {
	Generate(signal domain.ExpertSignal, snapshot domain.MarketSnapshot, cfg config.RulesConfig, tradeDate time.Time) domain.TradePlan
}

type marketProvider interface {
	GetDailyBars(ctx context.Context, symbol string, start time.Time, end time.Time, adjust string) ([]domain.DailyBar, error)
	GetMinuteBars(ctx context.Context, symbol string, start time.Time, end time.Time, interval string, adjust string) ([]domain.MinuteBar, error)
	GetTradingCalendar(ctx context.Context, start time.Time, end time.Time) ([]domain.TradingDay, error)
}

type DocumentService struct {
	repo      *repository.Repository
	runtime   *config.Runtime
	parser    documentParser
	extractor signalExtractor
	market    marketProvider
	rules     ruleEngine
	store     storage.ObjectStorage
	logger    *slog.Logger
}

func NewDocumentService(
	repo *repository.Repository,
	runtime *config.Runtime,
	parser documentParser,
	extractor signalExtractor,
	marketProvider marketProvider,
	rules ruleEngine,
	store storage.ObjectStorage,
	logger *slog.Logger,
) *DocumentService {
	return &DocumentService{
		repo:      repo,
		runtime:   runtime,
		parser:    parser,
		extractor: extractor,
		market:    marketProvider,
		rules:     rules,
		store:     store,
		logger:    logger,
	}
}

func (s *DocumentService) IngestDocument(ctx context.Context, request domain.DocumentIngestRequest) (*domain.Document, bool, error) {
	cfg := s.currentConfig()
	sha := utils.SHA256Hex(request.Content)
	if cfg.DocumentIngestion.SHA256Dedup {
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
	objectKey := fmt.Sprintf("documents/%s/%s%s", time.Now().UTC().Format("2006/01/02"), sha, strings.ToLower(filepath.Ext(request.FileName)))
	if err := s.store.PutBytes(ctx, cfg.ObjectStorage.BucketDocuments, objectKey, request.ContentType, request.Content); err != nil {
		return nil, false, err
	}
	document, err := s.repo.CreateDocument(ctx, request, sha, objectKey, cfg.Meta.ConfigVersion)
	if err != nil {
		return nil, false, err
	}
	return document, false, nil
}

func (s *DocumentService) ProcessPendingDocuments(ctx context.Context, limit int32) error {
	documents, err := s.repo.ListDocumentsByStatus(ctx, "INGESTED", limit)
	if err != nil {
		return err
	}
	for _, document := range documents {
		if err := s.ProcessDocument(ctx, document.ID); err != nil {
			s.logger.Error("process document", "document_id", document.ID, "error", err.Error())
		}
	}
	return nil
}

func (s *DocumentService) ProcessDocument(ctx context.Context, documentID int64) error {
	cfg := s.currentConfig()
	document, err := s.repo.GetDocumentByID(ctx, documentID)
	if err != nil {
		return err
	}

	content, err := s.store.GetBytes(ctx, cfg.ObjectStorage.BucketDocuments, document.ObjectKey)
	if err != nil {
		return err
	}

	parsed, parseErr := s.parser.Parse(ctx, document.FileName, content, cfg.DocumentParsing)
	parsed.DocumentID = document.ID
	parseRun, err := s.repo.CreateParseRun(ctx, parsed)
	if err != nil {
		return err
	}
	if parseErr != nil || parseRun.Status == "FAILED" {
		_ = s.repo.UpdateDocumentStatus(ctx, document.ID, "FAILED")
		_ = s.writeDeadLetter(ctx, cfg, document, map[string]any{
			"document_id": document.ID,
			"error":       parseRun.ErrorMessage,
		})
		return parseErr
	}

	if parseRun.Status == "NEEDS_OCR" {
		if err := s.repo.UpdateDocumentStatus(ctx, document.ID, "NEEDS_OCR"); err != nil {
			return err
		}
		return nil
	}
	if err := s.repo.UpdateDocumentStatus(ctx, document.ID, "PARSED"); err != nil {
		return err
	}

	extracted, err := s.extractor.Extract(ctx, *document, *parseRun)
	if err != nil {
		_ = s.repo.UpdateDocumentStatus(ctx, document.ID, "FAILED")
		return err
	}

	tradeDate, err := s.nextTradeDate(ctx, time.Now().In(utils.MustLocation(cfg.Meta.Timezone)))
	if err != nil {
		return err
	}

	for _, item := range extracted {
		signal, err := s.repo.CreateSignal(ctx, domain.ExpertSignal{
			DocumentID:    document.ID,
			ParseRunID:    parseRun.ID,
			ExpertName:    item.ExpertName,
			ExpertOrg:     item.ExpertOrg,
			Symbol:        item.Symbol,
			AssetType:     item.AssetType,
			Market:        item.Market,
			Sentiment:     item.Sentiment,
			Thesis:        item.Thesis,
			Evidence:      item.Evidence,
			Risks:         item.Risks,
			Confidence:    item.Confidence,
			ConfigVersion: cfg.Meta.ConfigVersion,
			RuleVersion:   cfg.Rules.Version,
			SignalDate:    time.Now(),
		})
		if err != nil {
			return err
		}

		snapshot, err := s.buildMarketSnapshot(ctx, *signal)
		if err != nil {
			return err
		}
		savedSnapshot, err := s.repo.CreateMarketSnapshot(ctx, snapshot)
		if err != nil {
			return err
		}

		plan := s.rules.Generate(*signal, *savedSnapshot, cfg.Rules, tradeDate)
		if cfg.Runtime.AllowAutoApproval && !cfg.Approval.ManualRequired {
			plan.Status = "APPROVED"
			plan.ApprovedBy = cfg.Approval.DefaultApprover
			plan.ApprovedAt = time.Now().UTC()
		}
		savedPlan, err := s.repo.CreatePlan(ctx, plan)
		if err != nil {
			return err
		}
		if plan.Status == "APPROVED" {
			if _, err := s.repo.ApprovePlan(ctx, savedPlan.ID, cfg.Approval.DefaultApprover); err != nil {
				return err
			}
		}
	}

	return s.repo.UpdateDocumentStatus(ctx, document.ID, "PLAN_GENERATED")
}

func (s *DocumentService) buildMarketSnapshot(ctx context.Context, signal domain.ExpertSignal) (domain.MarketSnapshot, error) {
	cfg := s.currentConfig()
	end := time.Now()
	start := end.AddDate(0, 0, -30)
	bars, err := s.market.GetDailyBars(ctx, signal.Symbol, start, end, "qfq")
	if err != nil {
		return domain.MarketSnapshot{}, err
	}
	if len(bars) == 0 {
		return domain.MarketSnapshot{}, fmt.Errorf("no daily bars for %s", signal.Symbol)
	}
	last := bars[len(bars)-1]
	prevClose := last.Open
	if len(bars) > 1 {
		prevClose = bars[len(bars)-2].Close
	}
	benchmarkReturn := 0.0
	if cfg.Runtime.DefaultBenchmarkSymbol != "" {
		if benchmarkBars, err := s.market.GetDailyBars(ctx, cfg.Runtime.DefaultBenchmarkSymbol, start, end, "qfq"); err == nil && len(benchmarkBars) > 1 {
			lastBenchmark := benchmarkBars[len(benchmarkBars)-1]
			prevBenchmark := benchmarkBars[len(benchmarkBars)-2]
			if prevBenchmark.Close != 0 {
				benchmarkReturn = ((lastBenchmark.Close - prevBenchmark.Close) / prevBenchmark.Close) * 100
			}
		}
	}
	return domain.MarketSnapshot{
		Symbol:             signal.Symbol,
		TradeDate:          last.TradeDate,
		Provider:           "provider_chain",
		Open:               last.Open,
		High:               last.High,
		Low:                last.Low,
		Close:              last.Close,
		Volume:             last.Volume,
		Turnover:           last.Turnover,
		ATR:                averageATR(bars),
		PrevClose:          prevClose,
		BenchmarkReturnPct: benchmarkReturn,
		ConfigVersion:      cfg.Meta.ConfigVersion,
	}, nil
}

func (s *DocumentService) nextTradeDate(ctx context.Context, base time.Time) (time.Time, error) {
	days, err := s.market.GetTradingCalendar(ctx, base, base.AddDate(0, 0, 7))
	if err != nil {
		return time.Time{}, err
	}
	for _, day := range days {
		if day.Date.After(base) && day.IsTrading {
			return startOfDay(day.Date), nil
		}
	}
	return startOfDay(base.AddDate(0, 0, 1)), nil
}

func (s *DocumentService) applyDefaults(request domain.DocumentIngestRequest, cfg *config.Config) domain.DocumentIngestRequest {
	if request.SourceType == "" {
		request.SourceType = cfg.DocumentIngestion.SourceDefaults.SourceType
	}
	if request.SourceName == "" {
		request.SourceName = cfg.DocumentIngestion.SourceDefaults.SourceName
	}
	if request.Author == "" {
		request.Author = cfg.DocumentIngestion.SourceDefaults.Author
	}
	if request.Institution == "" {
		request.Institution = cfg.DocumentIngestion.SourceDefaults.Institution
	}
	if request.Title == "" {
		request.Title = strings.TrimSuffix(request.FileName, filepath.Ext(request.FileName))
	}
	return request
}

func (s *DocumentService) writeDeadLetter(ctx context.Context, cfg *config.Config, document *domain.Document, payload map[string]any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("%sdocument-%d.json", cfg.DocumentParsing.DeadLetter.Prefix, document.ID)
	return s.store.PutBytes(ctx, cfg.ObjectStorage.BucketDeadLetters, key, "application/json", raw)
}

func (s *DocumentService) currentConfig() *config.Config {
	return s.runtime.Config()
}

func averageATR(bars []domain.DailyBar) float64 {
	if len(bars) == 0 {
		return 0
	}
	window := 5
	if len(bars) < window {
		window = len(bars)
	}
	total := 0.0
	for _, bar := range bars[len(bars)-window:] {
		total += (bar.High - bar.Low)
	}
	return total / float64(window)
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
