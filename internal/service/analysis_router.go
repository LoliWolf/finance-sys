package service

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"

	"finance-sys/internal/agentclient"
	"finance-sys/internal/config"
	"finance-sys/internal/domain"
)

type AnalysisObservation struct {
	Intents        []domain.PlanIntent
	AgentMode      string
	Route          string
	AgentResponse  *agentclient.ResolveDocumentResponse
	ShadowResponse *agentclient.ResolveDocumentResponse
	AgentError     string
	LegacyError    string
	FallbackUsed   bool
}

type responsePlanAnalyzer interface {
	AnalyzeWithResponse(context.Context, domain.Document, domain.ParseRun) ([]domain.PlanIntent, *agentclient.ResolveDocumentResponse, error)
}

type AnalysisRouter struct {
	runtime *config.Runtime
	legacy  planAnalyzer
	agent   planAnalyzer
	logger  *slog.Logger
}

func NewAnalysisRouter(runtime *config.Runtime, legacy planAnalyzer, agent planAnalyzer, logger *slog.Logger) *AnalysisRouter {
	return &AnalysisRouter{
		runtime: runtime,
		legacy:  legacy,
		agent:   agent,
		logger:  logger,
	}
}

func (r *AnalysisRouter) Analyze(ctx context.Context, document domain.Document, parsed domain.ParseRun) ([]domain.PlanIntent, error) {
	result, err := r.AnalyzeWithObservation(ctx, document, parsed)
	if err != nil {
		return nil, err
	}
	return result.Intents, nil
}

func (r *AnalysisRouter) AnalyzeWithObservation(ctx context.Context, document domain.Document, parsed domain.ParseRun) (AnalysisObservation, error) {
	cfg := r.runtime.Config()
	if cfg == nil {
		return AnalysisObservation{}, fmt.Errorf("config runtime unavailable")
	}
	if !cfg.Agent.Enabled {
		intents, err := r.legacy.Analyze(ctx, document, parsed)
		if err != nil {
			return AnalysisObservation{AgentMode: "disabled", Route: string(domain.ResolutionRouteLegacyLLM), LegacyError: err.Error()}, err
		}
		return AnalysisObservation{Intents: intents, AgentMode: "disabled", Route: string(domain.ResolutionRouteLegacyLLM)}, nil
	}

	switch cfg.Agent.Mode {
	case config.AgentModePrimary:
		intents, response, err := analyzeAgentWithOptionalResponse(ctx, r.agent, document, parsed)
		if err == nil {
			return AnalysisObservation{Intents: intents, AgentMode: string(cfg.Agent.Mode), Route: string(domain.ResolutionRouteAgentPrimary), AgentResponse: response}, nil
		}
		if cfg.Agent.AllowLegacyLLMFallback {
			if r.logger != nil {
				r.logger.WarnContext(ctx, "agent analyzer failed; falling back to llm analyzer", "document_id", document.ID, "parse_run_id", parsed.ID, "error", err.Error())
			}
			legacyIntents, legacyErr := r.legacy.Analyze(ctx, document, parsed)
			result := AnalysisObservation{
				Intents:       legacyIntents,
				AgentMode:     string(cfg.Agent.Mode),
				Route:         string(domain.ResolutionRouteLegacyLLM),
				AgentResponse: response,
				AgentError:    err.Error(),
				FallbackUsed:  true,
			}
			if legacyErr != nil {
				result.LegacyError = legacyErr.Error()
				return result, legacyErr
			}
			return result, nil
		}
		return AnalysisObservation{AgentMode: string(cfg.Agent.Mode), Route: string(domain.ResolutionRouteAgentPrimary), AgentResponse: response, AgentError: err.Error()}, err
	case config.AgentModeShadow:
		if r.logger != nil {
			r.logger.InfoContext(ctx, "analysis router shadow mode evaluating agent analyzer", "document_id", document.ID, "parse_run_id", parsed.ID)
		}
		var agentError string
		var shadowResponse *agentclient.ResolveDocumentResponse
		if shouldRunShadowAgent(cfg, document, parsed) {
			var err error
			_, shadowResponse, err = analyzeAgentWithOptionalResponse(ctx, r.agent, document, parsed)
			if err != nil {
				agentError = err.Error()
				if r.logger != nil {
					r.logger.WarnContext(ctx, "analysis router shadow agent analyzer failed", "document_id", document.ID, "parse_run_id", parsed.ID, "error", err.Error())
				}
			}
		} else if r.logger != nil {
			r.logger.InfoContext(ctx, "analysis router shadow agent skipped by sample rate", "document_id", document.ID, "parse_run_id", parsed.ID, "sample_rate", cfg.Agent.Observation.ShadowSampleRate)
		}
		intents, legacyErr := r.legacy.Analyze(ctx, document, parsed)
		result := AnalysisObservation{
			Intents:        intents,
			AgentMode:      string(cfg.Agent.Mode),
			Route:          string(domain.ResolutionRouteAgentShadow),
			ShadowResponse: shadowResponse,
			AgentError:     agentError,
		}
		if legacyErr != nil {
			result.LegacyError = legacyErr.Error()
			return result, legacyErr
		}
		return result, nil
	default:
		return AnalysisObservation{}, fmt.Errorf("unsupported agent mode %q", cfg.Agent.Mode)
	}
}

func analyzeAgentWithOptionalResponse(ctx context.Context, analyzer planAnalyzer, document domain.Document, parsed domain.ParseRun) ([]domain.PlanIntent, *agentclient.ResolveDocumentResponse, error) {
	if detailed, ok := analyzer.(responsePlanAnalyzer); ok {
		return detailed.AnalyzeWithResponse(ctx, document, parsed)
	}
	intents, err := analyzer.Analyze(ctx, document, parsed)
	return intents, nil, err
}

func shouldRunShadowAgent(cfg *config.Config, document domain.Document, parsed domain.ParseRun) bool {
	if cfg == nil || !cfg.Agent.Observation.Enabled {
		return true
	}
	rate := cfg.Agent.Observation.ShadowSampleRate
	if rate <= 0 {
		return false
	}
	if rate >= 1 {
		return true
	}
	h := fnv.New64a()
	_, _ = fmt.Fprintf(h, "%d:%d", document.ID, parsed.ID)
	bucket := float64(h.Sum64()%10000) / 10000
	return bucket < rate
}
