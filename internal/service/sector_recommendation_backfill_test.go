package service

import (
	"testing"

	"finance-sys/internal/agentclient"

	"github.com/stretchr/testify/require"
)

func TestAgentResponseRepresentsUntrackableOnlyResult(t *testing.T) {
	analysis := AnalysisObservation{AgentResponse: &agentclient.ResolveDocumentResponse{
		Status: agentclient.AgentStatusResolved,
		UntrackableTargets: []agentclient.AgentUntrackableTarget{{
			RawSymbol:  "宽泛主题",
			TargetKind: "THEME",
			Reason:     "not a trackable instrument",
		}},
	}}

	require.True(t, agentResponseRepresentsNoPlanIntents(analysis))
}

func TestAgentResponseRepresentsNoExtractedIntent(t *testing.T) {
	analysis := AnalysisObservation{AgentResponse: &agentclient.ResolveDocumentResponse{
		Status:   agentclient.AgentStatusFailed,
		Warnings: []string{"no instrument intent extracted"},
	}}

	require.True(t, agentResponseRepresentsNoPlanIntents(analysis))
}

func TestAgentResponseWithCandidateIsNotNoPlanResult(t *testing.T) {
	analysis := AnalysisObservation{AgentResponse: &agentclient.ResolveDocumentResponse{
		Status: agentclient.AgentStatusResolved,
		CandidatePlanInput: []agentclient.AgentCandidatePlanInput{{
			RawSymbol: "CPO板块",
		}},
	}}

	require.False(t, agentResponseRepresentsNoPlanIntents(analysis))
}

func TestAgentFailureIsNotNoPlanResult(t *testing.T) {
	analysis := AnalysisObservation{AgentResponse: &agentclient.ResolveDocumentResponse{
		Status:   agentclient.AgentStatusFailed,
		Warnings: []string{"upstream model timeout"},
	}}

	require.False(t, agentResponseRepresentsNoPlanIntents(analysis))
}
