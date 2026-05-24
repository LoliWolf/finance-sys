package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"finance-sys/internal/agentclient"
	"finance-sys/internal/config"
	"finance-sys/internal/dal"
	"finance-sys/internal/domain"
	"finance-sys/internal/domain/db_model"

	"gorm.io/gorm"
)

type ResolutionObserver struct {
	runtime *config.Runtime
	logger  *slog.Logger
}

func NewResolutionObserver(runtime *config.Runtime, logger *slog.Logger) *ResolutionObserver {
	return &ResolutionObserver{runtime: runtime, logger: logger}
}

func (o *ResolutionObserver) Start(ctx context.Context, db *gorm.DB, document *domain.Document, parseRun *domain.ParseRun) (*db_model.InstrumentResolutionRun, error) {
	cfg := o.runtime.Config()
	if cfg == nil || document == nil || parseRun == nil || !cfg.Agent.Observation.Enabled {
		return nil, nil
	}
	if !cfg.Agent.Observation.PersistSuccess && !cfg.Agent.Observation.PersistFailure {
		return nil, nil
	}
	parseRunID := parseRun.ID
	model := &db_model.InstrumentResolutionRun{
		DocumentID:              document.ID,
		ParseRunID:              &parseRunID,
		ConfigVersion:           cfg.Meta.ConfigVersion,
		AgentMode:               observationAgentMode(cfg),
		Route:                   string(domain.ResolutionRouteLocalOnly),
		Status:                  string(domain.ResolutionRunStatusRunning),
		SchemaVersion:           cfg.Agent.SchemaVersion,
		ErrorMessage:            "",
		StartedAt:               time.Now(),
		TargetsJSON:             []byte("[]"),
		ToolTracesJSON:          []byte("[]"),
		ShadowCompareJSON:       []byte("{}"),
		RawMetadataJSON:         []byte("{}"),
		RawTargetCount:          0,
		CandidatePlanInputCount: 0,
		CandidatePlanCount:      0,
		UntrackableCount:        0,
		ToolCallCount:           0,
	}
	if err := dal.InstrumentResolutionRuns.Create(ctx, db, model); err != nil {
		return nil, err
	}
	return model, nil
}

func (o *ResolutionObserver) FinishSucceeded(ctx context.Context, tx *gorm.DB, run *db_model.InstrumentResolutionRun, analysis AnalysisObservation, intents []domain.PlanIntent, resolutions []domain.InstrumentResolution, plans []domain.CandidatePlan) error {
	if run == nil {
		return nil
	}
	cfg := o.runtime.Config()
	if cfg == nil || !cfg.Agent.Observation.Enabled {
		return nil
	}
	if !cfg.Agent.Observation.PersistSuccess {
		if err := dal.UntrackableTargets.DeactivateByDocumentID(ctx, tx, run.DocumentID); err != nil {
			return err
		}
		return dal.InstrumentResolutionRuns.DeleteByID(ctx, tx, run.ID)
	}
	targets := buildResolutionTargets(analysis, intents, resolutions)
	untrackables := buildUntrackableTargetModels(run, cfg, targets)
	toolTraces := buildToolTraces(analysis, cfg.Agent.Observation.PersistToolTraces)
	shadowCompare := buildShadowCompare(analysis, intents, plans, nil)
	rawMetadata := buildRawMetadata(analysis, nil)
	finishedAt := time.Now()
	values, err := o.finishValues(cfg, analysis, domain.ResolutionRunStatusSucceeded, "", "", len(intents), len(plans), len(untrackables), targets, toolTraces, shadowCompare, rawMetadata, run.StartedAt, finishedAt)
	if err != nil {
		return err
	}
	if err := dal.InstrumentResolutionRuns.UpdateByID(ctx, tx, run.ID, values); err != nil {
		return err
	}
	if err := dal.UntrackableTargets.DeactivateByDocumentID(ctx, tx, run.DocumentID); err != nil {
		return err
	}
	if err := dal.UntrackableTargets.CreateBatch(ctx, tx, untrackables); err != nil {
		return err
	}
	o.logFinish(ctx, run.ID, values)
	return nil
}

func (o *ResolutionObserver) FinishFailed(ctx context.Context, db *gorm.DB, run *db_model.InstrumentResolutionRun, analysis AnalysisObservation, intents []domain.PlanIntent, resolutions []domain.InstrumentResolution, cause error, errorCode string) error {
	if run == nil {
		return nil
	}
	cfg := o.runtime.Config()
	if cfg == nil || !cfg.Agent.Observation.Enabled {
		return nil
	}
	if !cfg.Agent.Observation.PersistFailure {
		return dal.InstrumentResolutionRuns.DeleteByID(ctx, db, run.ID)
	}
	if errorCode == "" {
		errorCode = classifyResolutionError(cause)
	}
	targets := buildResolutionTargets(analysis, intents, resolutions)
	untrackables := buildUntrackableTargetModels(run, cfg, targets)
	toolTraces := buildToolTraces(analysis, cfg.Agent.Observation.PersistToolTraces)
	shadowCompare := buildShadowCompare(analysis, intents, nil, cause)
	rawMetadata := buildRawMetadata(analysis, cause)
	finishedAt := time.Now()
	values, err := o.finishValues(cfg, analysis, domain.ResolutionRunStatusFailed, errorCode, safeError(cause), len(intents), 0, len(untrackables), targets, toolTraces, shadowCompare, rawMetadata, run.StartedAt, finishedAt)
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := dal.InstrumentResolutionRuns.UpdateByID(ctx, tx, run.ID, values); err != nil {
			return err
		}
		if err := dal.UntrackableTargets.DeactivateByDocumentID(ctx, tx, run.DocumentID); err != nil {
			return err
		}
		if err := dal.UntrackableTargets.CreateBatch(ctx, tx, untrackables); err != nil {
			return err
		}
		o.logFinish(ctx, run.ID, values)
		return nil
	})
}

func (o *ResolutionObserver) ListRunsByDocumentID(ctx context.Context, db *gorm.DB, documentID int64) ([]domain.ResolutionRun, error) {
	rows, err := dal.InstrumentResolutionRuns.QueryByDocumentID(ctx, db, documentID)
	if err != nil {
		return nil, err
	}
	items := make([]domain.ResolutionRun, 0, len(rows))
	for i := range rows {
		item, err := mapResolutionRun(&rows[i])
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, nil
}

func (o *ResolutionObserver) GetRunByID(ctx context.Context, db *gorm.DB, id int64) (*domain.ResolutionRun, error) {
	row, err := dal.InstrumentResolutionRuns.QueryByID(ctx, db, id)
	if err != nil {
		return nil, err
	}
	return mapResolutionRun(row)
}

func (o *ResolutionObserver) ListActiveUntrackableTargetsByDocumentID(ctx context.Context, db *gorm.DB, documentID int64) ([]domain.UntrackableTarget, error) {
	rows, err := dal.UntrackableTargets.QueryActiveByDocumentID(ctx, db, documentID)
	if err != nil {
		return nil, err
	}
	return mapUntrackableTargetRows(rows)
}

func (o *ResolutionObserver) finishValues(cfg *config.Config, analysis AnalysisObservation, status domain.ResolutionRunStatus, errorCode, errorMessage string, rawTargetCount int, planCount int, untrackableCount int, targets []domain.ResolutionTarget, toolTraces []domain.ResolutionToolTrace, shadowCompare map[string]any, rawMetadata map[string]any, startedAt time.Time, finishedAt time.Time) (map[string]any, error) {
	response := primaryObservationResponse(analysis)
	targetsJSON, err := marshalLimitedJSON(targets, cfg.Agent.Observation.MaxJSONBytes)
	if err != nil {
		return nil, err
	}
	toolTracesJSON, err := marshalLimitedJSON(toolTraces, cfg.Agent.Observation.MaxJSONBytes)
	if err != nil {
		return nil, err
	}
	shadowJSON, err := marshalLimitedJSON(shadowCompare, cfg.Agent.Observation.MaxJSONBytes)
	if err != nil {
		return nil, err
	}
	rawMetadataJSON, err := marshalLimitedJSON(rawMetadata, cfg.Agent.Observation.MaxJSONBytes)
	if err != nil {
		return nil, err
	}
	if startedAt.IsZero() || startedAt.After(finishedAt) {
		startedAt = finishedAt
	}
	values := map[string]any{
		"agent_mode":                 nonEmpty(analysis.AgentMode, observationAgentMode(cfg)),
		"route":                      nonEmpty(analysis.Route, string(domain.ResolutionRouteLocalOnly)),
		"status":                     string(status),
		"schema_version":             responseSchemaVersion(analysis, cfg),
		"agent_version":              responseAgentVersion(response),
		"skill_name":                 responseSkillName(response),
		"skill_version":              responseSkillVersion(response),
		"skill_hash":                 responseSkillHash(response),
		"fallback_used":              analysis.FallbackUsed,
		"raw_target_count":           int32(rawTargetCountValue(response, rawTargetCount)),
		"candidate_plan_input_count": int32(candidatePlanInputCount(response, rawTargetCount)),
		"candidate_plan_count":       int32(planCount),
		"untrackable_count":          int32(untrackableCount),
		"tool_call_count":            int32(len(toolTraces)),
		"error_code":                 errorCode,
		"error_message":              truncateString(errorMessage, 2000),
		"finished_at":                finishedAt,
		"duration_ms":                int32(maxInt64(0, finishedAt.Sub(startedAt).Milliseconds())),
		"targets_json":               targetsJSON,
		"tool_traces_json":           toolTracesJSON,
		"shadow_compare_json":        shadowJSON,
		"raw_metadata_json":          rawMetadataJSON,
	}
	return values, nil
}

func (o *ResolutionObserver) logFinish(ctx context.Context, runID int64, values map[string]any) {
	if o.logger == nil {
		return
	}
	o.logger.InfoContext(ctx, "instrument resolution observation finished",
		"resolution_run_id", runID,
		"agent_mode", values["agent_mode"],
		"route", values["route"],
		"status", values["status"],
		"skill_hash", values["skill_hash"],
		"raw_target_count", values["raw_target_count"],
		"candidate_plan_input_count", values["candidate_plan_input_count"],
		"candidate_plan_count", values["candidate_plan_count"],
		"untrackable_count", values["untrackable_count"],
		"tool_call_count", values["tool_call_count"],
		"duration_ms", values["duration_ms"],
		"error_code", values["error_code"],
	)
}

func buildResolutionTargets(analysis AnalysisObservation, intents []domain.PlanIntent, resolutions []domain.InstrumentResolution) []domain.ResolutionTarget {
	targets := make([]domain.ResolutionTarget, 0, len(intents)+len(resolutions))
	seen := make(map[string]struct{})
	add := func(target domain.ResolutionTarget) {
		key := strings.ToLower(strings.TrimSpace(target.RawTarget)) + "|" + string(target.Decision) + "|" + target.ReasonCode
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		targets = append(targets, target)
	}
	if response := primaryObservationResponse(analysis); response != nil {
		for _, item := range response.CandidatePlanInput {
			security := domain.InstrumentResolutionCandidate{
				TSCode:      strings.ToUpper(strings.TrimSpace(item.Security.TSCode)),
				Symbol:      strings.TrimSpace(item.Security.Symbol),
				Name:        strings.TrimSpace(item.Security.Name),
				AssetType:   mapAgentAssetTypeBestEffort(item.Security.AssetType),
				Market:      domain.Market(strings.ToUpper(strings.TrimSpace(item.Security.Market))),
				MatchSource: "agent",
			}
			add(domain.ResolutionTarget{
				RawTarget:        item.RawSymbol,
				NormalizedTarget: NormalizeSecurityAlias(item.RawSymbol),
				Decision:         domain.ResolutionTargetAccepted,
				Security:         &security,
				TargetKind:       targetKindForAsset(security.AssetType),
				MatchSource:      security.MatchSource,
				Source:           "agent",
				Evidence:         item.Evidence,
			})
		}
		for _, item := range response.UntrackableTargets {
			kind := normalizeTargetKind(item.TargetKind)
			add(domain.ResolutionTarget{
				RawTarget:        item.RawSymbol,
				NormalizedTarget: NormalizeSecurityAlias(item.RawSymbol),
				Decision:         domain.ResolutionTargetUntrackable,
				ReasonCode:       string(reasonCodeForTargetKind(kind, item.Reason)),
				ReasonMessage:    item.Reason,
				TargetKind:       kind,
				Source:           "agent",
				Evidence:         item.Evidence,
			})
		}
	}
	hasAgentAcceptedTargets := primaryObservationResponse(analysis) != nil && len(primaryObservationResponse(analysis).CandidatePlanInput) > 0
	for _, resolution := range resolutions {
		if hasAgentAcceptedTargets && resolution.Status == domain.InstrumentResolutionStatusResolved {
			continue
		}
		target := resolutionTargetFromAssembler(resolution)
		add(target)
	}
	if len(targets) == 0 {
		for _, intent := range intents {
			add(domain.ResolutionTarget{
				RawTarget:        intent.Symbol,
				NormalizedTarget: NormalizeSecurityAlias(intent.Symbol),
				Decision:         domain.ResolutionTargetRejected,
				ReasonCode:       string(domain.UntrackableReasonUnknown),
				TargetKind:       domain.InstrumentTargetKindUnknown,
				Source:           "llm",
				Evidence:         intent.Evidence,
			})
		}
	}
	return targets
}

func resolutionTargetFromAssembler(resolution domain.InstrumentResolution) domain.ResolutionTarget {
	target := domain.ResolutionTarget{
		RawTarget:        resolution.RawSymbol,
		NormalizedTarget: resolution.NormalizedQuery,
		ReasonMessage:    resolution.Reason,
		TargetKind:       resolution.TargetKind,
		Candidates:       resolution.Candidates,
		Source:           "local_security",
	}
	switch resolution.Status {
	case domain.InstrumentResolutionStatusResolved:
		target.Decision = domain.ResolutionTargetAccepted
		if len(resolution.Candidates) == 1 {
			security := resolution.Candidates[0]
			target.Security = &security
			target.MatchSource = security.MatchSource
		}
	case domain.InstrumentResolutionStatusAmbiguous:
		target.Decision = domain.ResolutionTargetAmbiguous
		target.ReasonCode = string(domain.UntrackableReasonAmbiguousSecurity)
	case domain.InstrumentResolutionStatusUntrackable:
		target.Decision = domain.ResolutionTargetUntrackable
		target.ReasonCode = string(reasonCodeForTargetKind(resolution.TargetKind, resolution.Reason))
	case domain.InstrumentResolutionStatusNotFound:
		target.Decision = domain.ResolutionTargetRejected
		target.ReasonCode = string(domain.UntrackableReasonSecurityNotFound)
	default:
		target.Decision = domain.ResolutionTargetRejected
		target.ReasonCode = string(domain.UntrackableReasonUnknown)
	}
	return target
}

func buildUntrackableTargetModels(run *db_model.InstrumentResolutionRun, cfg *config.Config, targets []domain.ResolutionTarget) []db_model.UntrackableTarget {
	limit := cfg.Agent.Observation.MaxTargetsPerRun
	models := make([]db_model.UntrackableTarget, 0)
	seen := make(map[string]struct{})
	for _, target := range targets {
		if target.Decision == domain.ResolutionTargetAccepted {
			continue
		}
		if limit > 0 && len(models) >= limit {
			break
		}
		key := strings.ToLower(strings.TrimSpace(target.RawTarget)) + "|" + target.ReasonCode
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		evidence, _ := marshalLimitedJSON(target.Evidence, cfg.Agent.Observation.MaxJSONBytes)
		candidates, _ := marshalLimitedJSON(target.Candidates, cfg.Agent.Observation.MaxJSONBytes)
		models = append(models, db_model.UntrackableTarget{
			ResolutionRunID:  run.ID,
			DocumentID:       run.DocumentID,
			ParseRunID:       run.ParseRunID,
			RawTarget:        truncateString(target.RawTarget, 255),
			NormalizedTarget: truncateString(nonEmpty(target.NormalizedTarget, NormalizeSecurityAlias(target.RawTarget)), 255),
			TargetKind:       nonEmpty(string(target.TargetKind), string(domain.InstrumentTargetKindUnknown)),
			ReasonCode:       nonEmpty(target.ReasonCode, string(domain.UntrackableReasonUnknown)),
			ReasonMessage:    truncateString(target.ReasonMessage, 2000),
			Source:           nonEmpty(target.Source, "local_security"),
			EvidenceJSON:     evidence,
			CandidatesJSON:   candidates,
			ConfigVersion:    cfg.Meta.ConfigVersion,
			IsActive:         true,
		})
	}
	return models
}

func buildToolTraces(analysis AnalysisObservation, enabled bool) []domain.ResolutionToolTrace {
	if !enabled {
		return nil
	}
	response := primaryObservationResponse(analysis)
	if response == nil {
		return nil
	}
	traces := make([]domain.ResolutionToolTrace, 0, len(response.Debug.ToolsUsed))
	for _, toolName := range response.Debug.ToolsUsed {
		toolName = strings.TrimSpace(toolName)
		if toolName == "" {
			continue
		}
		traces = append(traces, domain.ResolutionToolTrace{
			ToolName:   toolName,
			ToolSource: "agent_debug",
			Status:     "SUCCEEDED",
			DurationMS: int(response.Debug.DurationMS),
		})
	}
	return traces
}

func buildShadowCompare(analysis AnalysisObservation, intents []domain.PlanIntent, plans []domain.CandidatePlan, cause error) map[string]any {
	compare := map[string]any{}
	if analysis.ShadowResponse != nil {
		compare["agent_status"] = analysis.ShadowResponse.Status
		compare["agent_candidate_plan_input_count"] = len(analysis.ShadowResponse.CandidatePlanInput)
		compare["agent_untrackable_count"] = len(analysis.ShadowResponse.UntrackableTargets)
		compare["legacy_intent_count"] = len(intents)
		compare["candidate_plan_count"] = len(plans)
		compare["agent_symbols"] = agentResponseSymbols(analysis.ShadowResponse)
		compare["final_plan_symbols"] = planSymbols(plans)
	}
	if analysis.AgentError != "" {
		compare["agent_error"] = truncateString(analysis.AgentError, 1000)
	}
	if cause != nil {
		compare["final_error"] = truncateString(cause.Error(), 1000)
	}
	return compare
}

func buildRawMetadata(analysis AnalysisObservation, cause error) map[string]any {
	metadata := map[string]any{
		"fallback_used": analysis.FallbackUsed,
	}
	if analysis.AgentError != "" {
		metadata["agent_error"] = truncateString(analysis.AgentError, 1000)
	}
	if analysis.LegacyError != "" {
		metadata["legacy_error"] = truncateString(analysis.LegacyError, 1000)
	}
	if cause != nil {
		metadata["error"] = truncateString(cause.Error(), 1000)
	}
	if response := primaryObservationResponse(analysis); response != nil {
		metadata["warnings"] = response.Warnings
		metadata["graph_run_id"] = response.Debug.GraphRunID
		metadata["nodes"] = response.Debug.Nodes
	}
	return metadata
}

func mapResolutionRun(row *db_model.InstrumentResolutionRun) (*domain.ResolutionRun, error) {
	targets := make([]domain.ResolutionTarget, 0)
	toolTraces := make([]domain.ResolutionToolTrace, 0)
	shadowCompare := make(map[string]any)
	rawMetadata := make(map[string]any)
	if err := json.Unmarshal(row.TargetsJSON, &targets); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(row.ToolTracesJSON, &toolTraces); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(row.ShadowCompareJSON, &shadowCompare); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(row.RawMetadataJSON, &rawMetadata); err != nil {
		return nil, err
	}
	var parseRunID int64
	if row.ParseRunID != nil {
		parseRunID = *row.ParseRunID
	}
	return &domain.ResolutionRun{
		ID:                      row.ID,
		DocumentID:              row.DocumentID,
		ParseRunID:              parseRunID,
		ConfigVersion:           row.ConfigVersion,
		AgentMode:               row.AgentMode,
		Route:                   row.Route,
		Status:                  domain.ResolutionRunStatus(row.Status),
		SchemaVersion:           row.SchemaVersion,
		AgentVersion:            row.AgentVersion,
		SkillName:               row.SkillName,
		SkillVersion:            row.SkillVersion,
		SkillHash:               row.SkillHash,
		FallbackUsed:            row.FallbackUsed,
		RawTargetCount:          int(row.RawTargetCount),
		CandidatePlanInputCount: int(row.CandidatePlanInputCount),
		CandidatePlanCount:      int(row.CandidatePlanCount),
		UntrackableCount:        int(row.UntrackableCount),
		ToolCallCount:           int(row.ToolCallCount),
		ErrorCode:               row.ErrorCode,
		ErrorMessage:            row.ErrorMessage,
		StartedAt:               row.StartedAt.UTC(),
		FinishedAt:              row.FinishedAt,
		DurationMS:              int(row.DurationMs),
		Targets:                 targets,
		ToolTraces:              toolTraces,
		ShadowCompare:           shadowCompare,
		RawMetadata:             rawMetadata,
		CreatedAt:               row.CreatedAt.UTC(),
		UpdatedAt:               row.UpdatedAt.UTC(),
	}, nil
}

func mapUntrackableTargetRows(rows []db_model.UntrackableTarget) ([]domain.UntrackableTarget, error) {
	items := make([]domain.UntrackableTarget, 0, len(rows))
	for i := range rows {
		var evidence []domain.EvidenceSpan
		var candidates []domain.InstrumentResolutionCandidate
		if err := json.Unmarshal(rows[i].EvidenceJSON, &evidence); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(rows[i].CandidatesJSON, &candidates); err != nil {
			return nil, err
		}
		var parseRunID int64
		if rows[i].ParseRunID != nil {
			parseRunID = *rows[i].ParseRunID
		}
		items = append(items, domain.UntrackableTarget{
			ID:               rows[i].ID,
			ResolutionRunID:  rows[i].ResolutionRunID,
			DocumentID:       rows[i].DocumentID,
			ParseRunID:       parseRunID,
			RawTarget:        rows[i].RawTarget,
			NormalizedTarget: rows[i].NormalizedTarget,
			TargetKind:       domain.InstrumentTargetKind(rows[i].TargetKind),
			ReasonCode:       rows[i].ReasonCode,
			ReasonMessage:    rows[i].ReasonMessage,
			Source:           rows[i].Source,
			Evidence:         evidence,
			Candidates:       candidates,
			ConfigVersion:    rows[i].ConfigVersion,
			IsActive:         rows[i].IsActive,
			CreatedAt:        rows[i].CreatedAt.UTC(),
			UpdatedAt:        rows[i].UpdatedAt.UTC(),
		})
	}
	return items, nil
}

func primaryObservationResponse(analysis AnalysisObservation) *agentclient.ResolveDocumentResponse {
	if analysis.AgentResponse != nil {
		return analysis.AgentResponse
	}
	if analysis.ShadowResponse != nil {
		return analysis.ShadowResponse
	}
	return nil
}

func observationAgentMode(cfg *config.Config) string {
	if cfg.Agent.Enabled {
		return string(cfg.Agent.Mode)
	}
	return "disabled"
}

func responseSchemaVersion(analysis AnalysisObservation, cfg *config.Config) string {
	if response := primaryObservationResponse(analysis); response != nil && response.SchemaVersion != "" {
		return response.SchemaVersion
	}
	return cfg.Agent.SchemaVersion
}

func responseAgentVersion(response *agentclient.ResolveDocumentResponse) string {
	if response == nil {
		return ""
	}
	return response.AgentVersion
}

func responseSkillName(response *agentclient.ResolveDocumentResponse) string {
	if response == nil {
		return ""
	}
	return response.Debug.SkillName
}

func responseSkillVersion(response *agentclient.ResolveDocumentResponse) string {
	if response == nil {
		return ""
	}
	return response.Debug.SkillVersion
}

func responseSkillHash(response *agentclient.ResolveDocumentResponse) string {
	if response == nil {
		return ""
	}
	return response.Debug.SkillHash
}

func candidatePlanInputCount(response *agentclient.ResolveDocumentResponse, fallback int) int {
	if response == nil {
		return fallback
	}
	return len(response.CandidatePlanInput)
}

func rawTargetCountValue(response *agentclient.ResolveDocumentResponse, fallback int) int {
	if response == nil || len(response.RawIntents) == 0 {
		return fallback
	}
	return len(response.RawIntents)
}

func reasonCodeForTargetKind(kind domain.InstrumentTargetKind, reason string) domain.UntrackableReasonCode {
	switch kind {
	case domain.InstrumentTargetKindSector:
		return domain.UntrackableReasonSectorNotTradable
	case domain.InstrumentTargetKindTheme, domain.InstrumentTargetKindBroadPhrase:
		return domain.UntrackableReasonThemeNotTradable
	case domain.InstrumentTargetKindIndex:
		return domain.UntrackableReasonIndexNotSupported
	}
	lower := strings.ToLower(reason)
	switch {
	case strings.Contains(lower, "ambiguous"):
		return domain.UntrackableReasonAmbiguousSecurity
	case strings.Contains(lower, "not found"), strings.Contains(lower, "no active security"):
		return domain.UntrackableReasonSecurityNotFound
	case strings.Contains(lower, "timeout"):
		return domain.UntrackableReasonToolTimeout
	default:
		return domain.UntrackableReasonUnknown
	}
}

func normalizeTargetKind(value string) domain.InstrumentTargetKind {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "STOCK":
		return domain.InstrumentTargetKindStock
	case "ETF":
		return domain.InstrumentTargetKindETF
	case "SECTOR":
		return domain.InstrumentTargetKindSector
	case "THEME":
		return domain.InstrumentTargetKindTheme
	case "INDUSTRY":
		return domain.InstrumentTargetKindIndustry
	case "INDEX":
		return domain.InstrumentTargetKindIndex
	case "BROAD_PHRASE":
		return domain.InstrumentTargetKindBroadPhrase
	default:
		return domain.InstrumentTargetKindUnknown
	}
}

func mapAgentAssetTypeBestEffort(value string) domain.AssetType {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "STOCK", "A_SHARE", "ASHARE":
		return domain.AssetTypeAShare
	case "ETF":
		return domain.AssetTypeETF
	default:
		return domain.AssetType(strings.ToUpper(strings.TrimSpace(value)))
	}
}

func marshalLimitedJSON(value any, limit int) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || len(raw) <= limit {
		return raw, nil
	}
	return json.Marshal(map[string]any{
		"truncated":      true,
		"original_bytes": len(raw),
	})
}

func classifyResolutionError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "schema_version"), strings.Contains(msg, "schema"), strings.Contains(msg, "debug.skill_hash"):
		return string(domain.UntrackableReasonSchemaInvalid)
	case strings.Contains(msg, "ambiguous"):
		return string(domain.UntrackableReasonAmbiguousSecurity)
	case strings.Contains(msg, "not found"):
		return string(domain.UntrackableReasonSecurityNotFound)
	case strings.Contains(msg, "timeout"):
		return string(domain.UntrackableReasonToolTimeout)
	default:
		return string(domain.UntrackableReasonUnknown)
	}
}

func safeError(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func truncateString(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	end := 0
	for end < limit {
		_, size := utf8.DecodeRuneInString(value[end:])
		if size == 0 || end+size > limit {
			break
		}
		end += size
	}
	return value[:end]
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func agentResponseSymbols(response *agentclient.ResolveDocumentResponse) []string {
	if response == nil {
		return nil
	}
	symbols := make([]string, 0, len(response.CandidatePlanInput))
	for _, item := range response.CandidatePlanInput {
		symbols = append(symbols, item.Security.TSCode)
	}
	return symbols
}

func planSymbols(plans []domain.CandidatePlan) []string {
	symbols := make([]string, 0, len(plans))
	for _, plan := range plans {
		symbols = append(symbols, plan.Symbol)
	}
	return symbols
}
