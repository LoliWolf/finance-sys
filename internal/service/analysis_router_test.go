package service_test

import (
	"context"
	"fmt"
	"testing"

	"finance-sys/internal/config"
	"finance-sys/internal/domain"
	"finance-sys/internal/service"

	"github.com/stretchr/testify/require"
)

type fakePlanAnalyzer struct {
	intents []domain.PlanIntent
	err     error
	calls   int
}

func (f *fakePlanAnalyzer) Analyze(context.Context, domain.Document, domain.ParseRun) ([]domain.PlanIntent, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.intents, nil
}

func TestAnalysisRouterUsesLegacyWhenAgentDisabled(t *testing.T) {
	legacy := &fakePlanAnalyzer{intents: []domain.PlanIntent{testRouterIntent("legacy")}}
	agent := &fakePlanAnalyzer{intents: []domain.PlanIntent{testRouterIntent("agent")}}
	router := service.NewAnalysisRouter(testRouterRuntime(false, config.AgentModePrimary, false), legacy, agent, nil)

	intents, err := router.Analyze(context.Background(), domain.Document{}, domain.ParseRun{})
	require.NoError(t, err)
	require.Equal(t, "legacy", intents[0].Symbol)
	require.Equal(t, 1, legacy.calls)
	require.Zero(t, agent.calls)
}

func TestAnalysisRouterUsesAgentInPrimaryMode(t *testing.T) {
	legacy := &fakePlanAnalyzer{intents: []domain.PlanIntent{testRouterIntent("legacy")}}
	agent := &fakePlanAnalyzer{intents: []domain.PlanIntent{testRouterIntent("agent")}}
	router := service.NewAnalysisRouter(testRouterRuntime(true, config.AgentModePrimary, false), legacy, agent, nil)

	intents, err := router.Analyze(context.Background(), domain.Document{}, domain.ParseRun{})
	require.NoError(t, err)
	require.Equal(t, "agent", intents[0].Symbol)
	require.Zero(t, legacy.calls)
	require.Equal(t, 1, agent.calls)
}

func TestAnalysisRouterFallsBackWhenConfigured(t *testing.T) {
	legacy := &fakePlanAnalyzer{intents: []domain.PlanIntent{testRouterIntent("legacy")}}
	agent := &fakePlanAnalyzer{err: fmt.Errorf("agent down")}
	router := service.NewAnalysisRouter(testRouterRuntime(true, config.AgentModePrimary, true), legacy, agent, nil)

	intents, err := router.Analyze(context.Background(), domain.Document{}, domain.ParseRun{})
	require.NoError(t, err)
	require.Equal(t, "legacy", intents[0].Symbol)
	require.Equal(t, 1, legacy.calls)
	require.Equal(t, 1, agent.calls)
}

func TestAnalysisRouterFailsWithoutFallback(t *testing.T) {
	legacy := &fakePlanAnalyzer{intents: []domain.PlanIntent{testRouterIntent("legacy")}}
	agent := &fakePlanAnalyzer{err: fmt.Errorf("agent down")}
	router := service.NewAnalysisRouter(testRouterRuntime(true, config.AgentModePrimary, false), legacy, agent, nil)

	_, err := router.Analyze(context.Background(), domain.Document{}, domain.ParseRun{})
	require.ErrorContains(t, err, "agent down")
	require.Zero(t, legacy.calls)
	require.Equal(t, 1, agent.calls)
}

func TestAnalysisRouterUsesLegacyInShadowMode(t *testing.T) {
	legacy := &fakePlanAnalyzer{intents: []domain.PlanIntent{testRouterIntent("legacy")}}
	agent := &fakePlanAnalyzer{intents: []domain.PlanIntent{testRouterIntent("agent")}}
	router := service.NewAnalysisRouter(testRouterRuntime(true, config.AgentModeShadow, false), legacy, agent, nil)

	intents, err := router.Analyze(context.Background(), domain.Document{}, domain.ParseRun{})
	require.NoError(t, err)
	require.Equal(t, "legacy", intents[0].Symbol)
	require.Equal(t, 1, legacy.calls)
	require.Equal(t, 1, agent.calls)
}

func testRouterRuntime(agentEnabled bool, mode config.AgentMode, fallback bool) *config.Runtime {
	return config.NewRuntime(&config.Snapshot{
		Config: &config.Config{
			Agent: config.AgentConfig{
				Enabled:                agentEnabled,
				Mode:                   mode,
				AllowLegacyLLMFallback: fallback,
			},
		},
	})
}

func testRouterIntent(symbol string) domain.PlanIntent {
	return domain.PlanIntent{
		Symbol:         symbol,
		Direction:      domain.TradeDirectionLong,
		ReferencePrice: 1,
		Thesis:         "supported by text",
		Confidence:     0.8,
	}
}
