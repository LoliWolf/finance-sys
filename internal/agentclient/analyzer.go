package agentclient

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/domain"
	"finance-sys/internal/utils"
)

type Analyzer struct {
	runtime *config.Runtime
	client  *Client
	logger  *slog.Logger
}

func NewAnalyzer(runtime *config.Runtime, logger *slog.Logger) *Analyzer {
	return &Analyzer{
		runtime: runtime,
		client:  NewClient(nil, logger),
		logger:  logger,
	}
}

func NewAnalyzerWithClient(runtime *config.Runtime, client *Client, logger *slog.Logger) *Analyzer {
	if client == nil {
		client = NewClient(nil, logger)
	}
	return &Analyzer{runtime: runtime, client: client, logger: logger}
}

func (a *Analyzer) Analyze(ctx context.Context, document domain.Document, parsed domain.ParseRun) ([]domain.PlanIntent, error) {
	cfg := a.runtime.Config()
	if cfg == nil {
		return nil, fmt.Errorf("config runtime unavailable")
	}
	if !cfg.Agent.Enabled {
		return nil, fmt.Errorf("agent analyzer disabled")
	}
	request := buildResolveDocumentRequest(document, parsed, cfg)
	if a.logger != nil {
		a.logger.InfoContext(ctx, "agent analyze start", "document_id", document.ID, "parse_run_id", parsed.ID, "chunk_count", len(request.Chunks), "endpoint", cfg.Agent.Endpoint)
	}

	response, err := a.client.ResolveDocument(ctx, cfg.Agent, request)
	if err != nil {
		return nil, err
	}
	if err := ValidateResponse(response, cfg.Agent.SchemaVersion); err != nil {
		return nil, err
	}
	intents, err := responseToPlanIntents(document, response)
	if err != nil {
		return nil, err
	}
	if a.logger != nil {
		a.logger.InfoContext(ctx, "agent analyze completed", "document_id", document.ID, "parse_run_id", parsed.ID, "agent_status", response.Status, "agent_version", response.AgentVersion, "raw_intent_count", len(response.RawIntents), "candidate_plan_input_count", len(response.CandidatePlanInput), "untrackable_count", len(response.UntrackableTargets), "intent_count", len(intents))
	}
	return intents, nil
}

func buildResolveDocumentRequest(document domain.Document, parsed domain.ParseRun, cfg *config.Config) ResolveDocumentRequest {
	chunks := parsed.Chunks
	if len(chunks) == 0 && strings.TrimSpace(parsed.CleanedText) != "" {
		chunks = []domain.Chunk{{Index: 0, Text: parsed.CleanedText}}
	}
	agentChunks := make([]AgentDocumentChunk, 0, len(chunks))
	for _, chunk := range chunks {
		agentChunks = append(agentChunks, AgentDocumentChunk{
			ChunkIndex: chunk.Index,
			Text:       chunk.Text,
		})
	}
	tradeDate := agentTradeDate(cfg)
	return ResolveDocumentRequest{
		SchemaVersion: RequestSchemaVersion,
		RequestID:     fmt.Sprintf("doc-%d-parse-%d-%d", document.ID, parsed.ID, time.Now().UnixNano()),
		Document: AgentDocument{
			DocumentID:  document.ID,
			ParseRunID:  parsed.ID,
			Title:       document.Title,
			Author:      document.Author,
			Institution: document.Institution,
		},
		TradeDate: tradeDate.Format(time.DateOnly),
		Chunks:    agentChunks,
		Limits: AgentLimits{
			MaxIntents:            20,
			MaxEvidencePerIntent:  4,
			MaxRisksPerIntent:     5,
			MaxUntrackableTargets: 20,
		},
	}
}

func agentTradeDate(cfg *config.Config) time.Time {
	loc := utils.MustLocation(cfg.Meta.Timezone)
	base := time.Now().In(loc)
	return time.Date(base.Year(), base.Month(), base.Day()+cfg.Rules.TradeDateOffsetDays, 0, 0, 0, 0, loc)
}

func responseToPlanIntents(document domain.Document, response *ResolveDocumentResponse) ([]domain.PlanIntent, error) {
	if len(response.CandidatePlanInput) > 0 {
		intents := make([]domain.PlanIntent, 0, len(response.CandidatePlanInput))
		for _, item := range response.CandidatePlanInput {
			assetType, err := mapAgentAssetType(item.Security.AssetType)
			if err != nil {
				return nil, err
			}
			market, err := mapAgentMarket(item.Security.Market)
			if err != nil {
				return nil, err
			}
			intents = append(intents, domain.PlanIntent{
				Analyst:            document.Author,
				Institution:        document.Institution,
				Symbol:             strings.ToUpper(strings.TrimSpace(item.Security.TSCode)),
				AssetType:          assetType,
				Market:             market,
				Direction:          item.Direction,
				ReferencePrice:     item.ReferencePrice,
				ReferencePriceNote: item.ReferencePriceNote,
				Thesis:             item.Thesis,
				Evidence:           item.Evidence,
				Risks:              item.Risks,
				Confidence:         item.Confidence,
			})
		}
		return intents, nil
	}

	if len(response.RawIntents) > 0 {
		intents := make([]domain.PlanIntent, 0, len(response.RawIntents))
		for _, item := range response.RawIntents {
			intents = append(intents, domain.PlanIntent{
				Analyst:            document.Author,
				Institution:        document.Institution,
				Symbol:             strings.TrimSpace(item.RawSymbol),
				Direction:          item.Direction,
				ReferencePrice:     item.ReferencePrice,
				ReferencePriceNote: item.ReferencePriceNote,
				Thesis:             item.Thesis,
				Evidence:           item.Evidence,
				Risks:              item.Risks,
				Confidence:         item.Confidence,
			})
		}
		return intents, nil
	}
	return nil, fmt.Errorf("agent returned no plan intents")
}

func mapAgentAssetType(value string) (domain.AssetType, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "STOCK", "A_SHARE", "ASHARE":
		return domain.AssetTypeAShare, nil
	case "ETF":
		return domain.AssetTypeETF, nil
	default:
		return "", fmt.Errorf("unsupported agent asset_type %q", value)
	}
}

func mapAgentMarket(value string) (domain.Market, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "SH":
		return domain.MarketSH, nil
	case "SZ":
		return domain.MarketSZ, nil
	case "BJ":
		return domain.MarketBJ, nil
	default:
		return "", fmt.Errorf("unsupported agent market %q", value)
	}
}
