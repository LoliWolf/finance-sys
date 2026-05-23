package service

import (
	"context"
	"fmt"
	"log/slog"

	"finance-sys/internal/config"
	"finance-sys/internal/domain"
)

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
	cfg := r.runtime.Config()
	if cfg == nil {
		return nil, fmt.Errorf("config runtime unavailable")
	}
	if !cfg.Agent.Enabled {
		return r.legacy.Analyze(ctx, document, parsed)
	}

	switch cfg.Agent.Mode {
	case config.AgentModePrimary:
		intents, err := r.agent.Analyze(ctx, document, parsed)
		if err == nil {
			return intents, nil
		}
		if cfg.Agent.AllowLegacyLLMFallback {
			if r.logger != nil {
				r.logger.WarnContext(ctx, "agent analyzer failed; falling back to llm analyzer", "document_id", document.ID, "parse_run_id", parsed.ID, "error", err.Error())
			}
			return r.legacy.Analyze(ctx, document, parsed)
		}
		return nil, err
	case config.AgentModeShadow:
		if r.logger != nil {
			r.logger.InfoContext(ctx, "analysis router shadow mode invoking agent analyzer", "document_id", document.ID, "parse_run_id", parsed.ID)
		}
		if _, err := r.agent.Analyze(ctx, document, parsed); err != nil && r.logger != nil {
			r.logger.WarnContext(ctx, "analysis router shadow agent analyzer failed", "document_id", document.ID, "parse_run_id", parsed.ID, "error", err.Error())
		}
		return r.legacy.Analyze(ctx, document, parsed)
	default:
		return nil, fmt.Errorf("unsupported agent mode %q", cfg.Agent.Mode)
	}
}
