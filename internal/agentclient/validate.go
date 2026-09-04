package agentclient

import (
	"fmt"
	"regexp"
	"strings"

	"finance-sys/internal/domain"
)

var (
	agentTSCodeRe    = regexp.MustCompile(`^(?:\d{6}\.(?:SH|SZ|BJ)|BK\d{4}\.DC)$`)
	agentSkillHashRe = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

func ValidateResponse(response *ResolveDocumentResponse, expectedSchemaVersion string) error {
	if response == nil {
		return fmt.Errorf("agent response is nil")
	}
	if response.SchemaVersion != expectedSchemaVersion {
		return fmt.Errorf("agent schema_version mismatch: got %q, want %q", response.SchemaVersion, expectedSchemaVersion)
	}
	switch response.Status {
	case AgentStatusResolved, AgentStatusPartial:
	case AgentStatusFailed:
		return fmt.Errorf("agent returned FAILED status")
	default:
		return fmt.Errorf("agent status must be RESOLVED, PARTIAL, or FAILED")
	}
	if err := validateDebug(response.Debug); err != nil {
		return err
	}
	if len([]rune(strings.TrimSpace(response.ExtractedAuthor))) > 128 {
		return fmt.Errorf("agent extracted_author must not exceed 128 characters")
	}
	for i := range response.RawIntents {
		if err := validateRawIntent(response.RawIntents[i]); err != nil {
			return fmt.Errorf("agent raw_intents[%d]: %w", i, err)
		}
	}
	for i := range response.CandidatePlanInput {
		if err := validateCandidatePlanInput(response.CandidatePlanInput[i]); err != nil {
			return fmt.Errorf("agent candidate_plan_inputs[%d]: %w", i, err)
		}
	}
	return nil
}

func validateDebug(debug AgentDebug) error {
	skillHash := strings.TrimSpace(debug.SkillHash)
	if skillHash == "" {
		return nil
	}
	if !agentSkillHashRe.MatchString(skillHash) {
		return fmt.Errorf("agent debug.skill_hash must match sha256:<64 lowercase hex>")
	}
	return nil
}

func validateRawIntent(intent AgentRawIntent) error {
	if strings.TrimSpace(intent.RawSymbol) == "" {
		return fmt.Errorf("raw_symbol is required")
	}
	if intent.Direction != domain.TradeDirectionLong && intent.Direction != domain.TradeDirectionShort {
		return fmt.Errorf("direction must be LONG or SHORT")
	}
	if intent.ReferencePrice < 0 {
		return fmt.Errorf("reference_price must be zero or positive")
	}
	if strings.TrimSpace(intent.Thesis) == "" {
		return fmt.Errorf("thesis is required")
	}
	if intent.Confidence <= 0 || intent.Confidence > 1 {
		return fmt.Errorf("confidence must be in (0,1]")
	}
	if len(intent.Evidence) == 0 {
		return fmt.Errorf("evidence is required")
	}
	return nil
}

func validateCandidatePlanInput(input AgentCandidatePlanInput) error {
	raw := AgentRawIntent{
		IntentID:           input.IntentID,
		RawSymbol:          input.RawSymbol,
		Direction:          input.Direction,
		ReferencePrice:     input.ReferencePrice,
		ReferencePriceNote: input.ReferencePriceNote,
		Thesis:             input.Thesis,
		Evidence:           input.Evidence,
		Risks:              input.Risks,
		Confidence:         input.Confidence,
	}
	if err := validateRawIntent(raw); err != nil {
		return err
	}
	security := input.Security
	if !agentTSCodeRe.MatchString(strings.ToUpper(strings.TrimSpace(security.TSCode))) {
		return fmt.Errorf("security.ts_code must be a valid security or BK sector ts_code")
	}
	if strings.TrimSpace(security.Symbol) == "" {
		return fmt.Errorf("security.symbol is required")
	}
	if strings.TrimSpace(security.Name) == "" {
		return fmt.Errorf("security.name is required")
	}
	switch strings.ToUpper(strings.TrimSpace(security.AssetType)) {
	case "STOCK", "ETF", "SECTOR":
	default:
		return fmt.Errorf("security.asset_type must be STOCK, ETF, or SECTOR")
	}
	switch strings.ToUpper(strings.TrimSpace(security.Market)) {
	case "SH", "SZ", "BJ", "DC":
	default:
		return fmt.Errorf("security.market must be SH, SZ, BJ, or DC")
	}
	return nil
}
