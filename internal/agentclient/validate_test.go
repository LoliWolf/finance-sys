package agentclient_test

import (
	"testing"

	"finance-sys/internal/agentclient"

	"github.com/stretchr/testify/require"
)

func TestValidateResponseAcceptsValidSkillHash(t *testing.T) {
	response := &agentclient.ResolveDocumentResponse{
		SchemaVersion: agentclient.ResponseSchemaVersion,
		AgentVersion:  "test-agent",
		Status:        agentclient.AgentStatusResolved,
		RawIntents:    []agentclient.AgentRawIntent{testRawIntent("新易盛")},
		Debug: agentclient.AgentDebug{
			SkillName:    "instrument_resolution",
			SkillVersion: "instrument-resolution-m5-v1",
			SkillHash:    "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		},
	}

	require.NoError(t, agentclient.ValidateResponse(response, agentclient.ResponseSchemaVersion))
}

func TestValidateResponseRejectsInvalidSkillHash(t *testing.T) {
	response := &agentclient.ResolveDocumentResponse{
		SchemaVersion: agentclient.ResponseSchemaVersion,
		AgentVersion:  "test-agent",
		Status:        agentclient.AgentStatusResolved,
		RawIntents:    []agentclient.AgentRawIntent{testRawIntent("新易盛")},
		Debug: agentclient.AgentDebug{
			SkillName:    "instrument_resolution",
			SkillVersion: "instrument-resolution-m5-v1",
			SkillHash:    "sha256:NOT-LOWERCASE-HEX",
		},
	}

	require.ErrorContains(t, agentclient.ValidateResponse(response, agentclient.ResponseSchemaVersion), "agent debug.skill_hash")
}
