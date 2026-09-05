package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"finance-sys/internal/dal"
	"finance-sys/internal/domain"
	"finance-sys/internal/domain/db_model"
	"finance-sys/internal/llm"
	"finance-sys/internal/utils"

	"gorm.io/gorm"
)

var (
	ErrInvalidTargetRecovery  = errors.New("invalid target recovery")
	ErrTargetRecoveryConflict = errors.New("target recovery conflict")
)

// Apply is opt-in. This input is a new source extraction, not a reconstruction
// of the lost original model output. It cannot supply executable order prices.
type TargetRecoveryRequest struct {
	ParseRunID    int64             `json:"parse_run_id"`
	RecommendDate string            `json:"recommend_date"`
	Intent        domain.PlanIntent `json:"intent"`
	Note          string            `json:"note"`
	Apply         bool              `json:"apply"`
}

type TargetRecoveryResult struct {
	Applied               bool                 `json:"applied"`
	Deduped               bool                 `json:"deduped"`
	ResolutionRunID       int64                `json:"resolution_run_id"`
	RecommendationEventID int64                `json:"recommendation_event_id"`
	Plan                  domain.CandidatePlan `json:"plan"`
}

type targetRecoveryMetadata struct {
	Operation                 string                `json:"operation"`
	InputOrigin               string                `json:"input_origin"`
	SourceUntrackableTargetID int64                 `json:"source_untrackable_target_id"`
	SourceResolutionRunID     int64                 `json:"source_resolution_run_id"`
	InputSHA256               string                `json:"input_sha256"`
	Request                   TargetRecoveryRequest `json:"request"`
	Result                    TargetRecoveryResult  `json:"result"`
}

func (s *DocumentService) RecoverUntrackableTarget(ctx context.Context, documentID, targetID int64, request TargetRecoveryRequest) (*TargetRecoveryResult, error) {
	if documentID <= 0 || targetID <= 0 {
		return nil, fmt.Errorf("%w: positive document and target IDs are required", ErrInvalidTargetRecovery)
	}
	cfg := s.currentConfig()
	if cfg == nil || s.rules == nil {
		return nil, errors.New("target recovery service unavailable")
	}
	apply := request.Apply
	var result TargetRecoveryResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		target, err := dal.UntrackableTargets.QueryForRecovery(ctx, tx, documentID, targetID)
		if err != nil {
			return err
		}
		docModel, err := dal.Documents.QueryByID(ctx, tx, documentID)
		if err != nil {
			return err
		}
		document := mapDocument(docModel)
		parsedModel, err := dal.ParseRuns.QueryByID(ctx, tx, request.ParseRunID)
		if err != nil {
			return err
		}
		parsed, err := mapParseRun(parsedModel)
		if err != nil {
			return err
		}
		date, intent, err := validateTargetRecoveryInput(*target, *document, *parsed, request)
		if err != nil {
			return err
		}
		request.Intent = intent
		request.Note = strings.TrimSpace(request.Note)
		request.Apply = false
		fingerprint, err := targetRecoveryFingerprint(documentID, targetID, request)
		if err != nil {
			return err
		}
		runs, err := dal.InstrumentResolutionRuns.QueryByDocumentID(ctx, tx, documentID)
		if err != nil {
			return err
		}
		if !target.IsActive {
			previous, err := previousTargetRecovery(runs, targetID, fingerprint)
			if err != nil {
				return err
			}
			plan, err := dal.TradeCandidatePlans.QueryByID(ctx, tx, previous.Plan.ID)
			if err != nil {
				return err
			}
			event, err := dal.RecommendationEvents.QueryByID(ctx, tx, previous.RecommendationEventID)
			if err != nil {
				return err
			}
			if plan.DocumentID != documentID || event.SourceDocumentID != documentID || event.PlanID == nil || *event.PlanID != plan.ID || event.Status == string(domain.RecommendationEventStatusSuperseded) {
				return fmt.Errorf("%w: recovered records changed; review before replay", ErrTargetRecoveryConflict)
			}
			result = *previous
			result.Deduped = true
			return nil
		}
		for _, run := range runs {
			if run.Status == string(domain.ResolutionRunStatusRunning) {
				return fmt.Errorf("%w: document analysis is running", ErrTargetRecoveryConflict)
			}
		}
		events, err := dal.RecommendationEvents.QueryByDocumentID(ctx, tx, documentID)
		if err != nil {
			return err
		}
		if err := validateTargetRecoveryDate(events, date); err != nil {
			return err
		}
		// Keep lookup on the same connection as the locked target, including when
		// the database pool permits only one open connection.
		assembler := NewCandidateAssembler(NewSecurityService(tx, s.logger), s.logger)
		trackable, resolutions, err := assembler.Assemble(ctx, []domain.PlanIntent{intent})
		if err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidTargetRecovery, err)
		}
		if len(trackable) != 1 || (trackable[0].AssetType != domain.AssetTypeAShare && trackable[0].AssetType != domain.AssetTypeETF) {
			return fmt.Errorf("%w: one stock or ETF must resolve", ErrInvalidTargetRecovery)
		}
		plan := s.rules.Generate(trackable[0], cfg.Rules, date, cfg.Meta.ConfigVersion)
		plan.DocumentID, plan.ParseRunID = documentID, parsed.ID
		plan.Status = domain.CandidatePlanStatusNeedsReview
		blogger, err := dal.Bloggers.QueryByNormalizedNameAndInstitution(ctx, tx, normalizeBloggerName(intent.Analyst), intent.Institution)
		if err != nil {
			return err
		}
		eventModel := recommendationEventFromPlan(*document, plan, blogger)
		if _, err := dal.RecommendationEvents.QueryByDedupeKey(ctx, tx, eventModel.DedupeKey); err == nil {
			return fmt.Errorf("%w: recommendation already exists; no existing event was overwritten", ErrTargetRecoveryConflict)
		} else if !errors.Is(err, dal.ErrNotFound) {
			return err
		}
		result = TargetRecoveryResult{Plan: plan}
		if !apply {
			return nil
		}
		startedAt := time.Now().UTC()
		run := &db_model.InstrumentResolutionRun{
			DocumentID: documentID, ParseRunID: &parsed.ID, ConfigVersion: cfg.Meta.ConfigVersion,
			AgentMode: "source_recovery", Route: string(domain.ResolutionRouteLocalOnly),
			Status: string(domain.ResolutionRunStatusRunning), SchemaVersion: cfg.Agent.SchemaVersion,
			StartedAt: startedAt, TargetsJSON: []byte("[]"), ToolTracesJSON: []byte("[]"),
			ShadowCompareJSON: []byte("{}"), RawMetadataJSON: []byte("{}"),
		}
		if err := dal.InstrumentResolutionRuns.Create(ctx, tx, run); err != nil {
			return err
		}
		planModel, err := candidatePlanToModel(plan)
		if err != nil {
			return err
		}
		if err := dal.TradeCandidatePlans.Create(ctx, tx, planModel); err != nil {
			return err
		}
		savedPlan, err := mapPlan(planModel)
		if err != nil {
			return err
		}
		eventModel = recommendationEventFromPlan(*document, *savedPlan, blogger)
		if err := dal.RecommendationEvents.Create(ctx, tx, eventModel); err != nil {
			return err
		}
		evidence := recommendationEvidenceModels(documentID, &savedPlan.ID, savedPlan.Evidence, eventModel.ID)
		if err := dal.RecommendationEventEvidences.CreateBatch(ctx, tx, evidence); err != nil {
			return err
		}
		result = TargetRecoveryResult{Applied: true, ResolutionRunID: run.ID, RecommendationEventID: eventModel.ID, Plan: *savedPlan}
		metadata, err := json.Marshal(targetRecoveryMetadata{
			Operation: "target_recovery", InputOrigin: "source_reextraction",
			SourceUntrackableTargetID: targetID, SourceResolutionRunID: target.ResolutionRunID,
			InputSHA256: fingerprint, Request: request, Result: result,
		})
		if err != nil {
			return err
		}
		resolved := resolutionTargetFromAssembler(resolutions[0])
		resolved.Source = "source_reextraction"
		resolved.Evidence = intent.Evidence
		targets, err := json.Marshal([]domain.ResolutionTarget{resolved})
		if err != nil {
			return err
		}
		finishedAt := time.Now().UTC()
		if err := dal.InstrumentResolutionRuns.UpdateByID(ctx, tx, run.ID, map[string]any{
			"status": string(domain.ResolutionRunStatusSucceeded), "raw_target_count": 1,
			"candidate_plan_input_count": 1, "candidate_plan_count": 1, "untrackable_count": 0,
			"finished_at": finishedAt, "duration_ms": finishedAt.Sub(startedAt).Milliseconds(),
			"targets_json": targets, "raw_metadata_json": metadata,
		}); err != nil {
			return err
		}
		if err := dal.UntrackableTargets.DeactivateByID(ctx, tx, documentID, targetID); err != nil {
			return err
		}
		if err := dal.Documents.UpdateStatusByID(ctx, tx, documentID, string(domain.DocumentStatusPlanned)); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func validateTargetRecoveryInput(target db_model.UntrackableTarget, document domain.Document, parsed domain.ParseRun, request TargetRecoveryRequest) (time.Time, domain.PlanIntent, error) {
	invalid := func(message string) (time.Time, domain.PlanIntent, error) {
		return time.Time{}, domain.PlanIntent{}, fmt.Errorf("%w: %s", ErrInvalidTargetRecovery, message)
	}
	if parsed.ID <= 0 || request.ParseRunID != parsed.ID || parsed.DocumentID != document.ID || target.DocumentID != document.ID || parsed.Status != domain.ParseRunStatusParsed {
		return invalid("parse run and target must belong to this document and text must be parsed")
	}
	if target.ReasonCode != string(domain.UntrackableReasonSecurityNotFound) {
		return invalid("only SECURITY_NOT_FOUND targets can use this recovery path")
	}
	date, err := time.Parse(time.DateOnly, request.RecommendDate)
	if err != nil {
		return invalid("recommend_date must be YYYY-MM-DD")
	}
	if strings.TrimSpace(request.Note) == "" || len(request.Note) > 4000 {
		return invalid("a recovery note is required (at most 4000 bytes)")
	}
	intent := request.Intent
	intent.Evidence = append([]domain.EvidenceSpan(nil), intent.Evidence...)
	intent.Symbol = strings.TrimSpace(intent.Symbol)
	intent.Thesis = strings.TrimSpace(intent.Thesis)
	if NormalizeSecurityAlias(intent.Symbol) != NormalizeSecurityAlias(target.RawTarget) {
		return invalid("intent symbol must match the original unresolved target")
	}
	if strings.TrimSpace(document.Author) == "" || (intent.Analyst != "" && intent.Analyst != document.Author) || (intent.Institution != "" && intent.Institution != document.Institution) {
		return invalid("author and institution must match the document")
	}
	intent.Analyst, intent.Institution = document.Author, document.Institution
	if intent.ReferencePrice != 0 || intent.ReferencePriceNote != domain.ReferencePriceNotePriceMissingInText {
		return invalid("source recovery cannot supply trade prices; use missing source price and review")
	}
	if math.IsNaN(intent.Confidence) || math.IsInf(intent.Confidence, 0) {
		return invalid("confidence must be finite")
	}
	if err := llm.ValidateIntent(intent); err != nil {
		return invalid(err.Error())
	}
	if len(intent.Evidence) == 0 || len(intent.Evidence) > 4 || len(intent.Risks) > 5 {
		return invalid("provide 1-4 evidence spans and at most 5 risks")
	}
	chunks := make(map[int]string, len(parsed.Chunks))
	for _, chunk := range parsed.Chunks {
		chunks[chunk.Index] = chunk.Text
	}
	if len(chunks) == 0 {
		chunks[0] = parsed.CleanedText
	}
	hasTarget := false
	for index := range intent.Evidence {
		span := &intent.Evidence[index]
		span.Text = strings.TrimSpace(span.Text)
		text, ok := chunks[span.ChunkIndex]
		if !ok || span.Text == "" || !strings.Contains(text, span.Text) {
			return invalid("each evidence span must occur verbatim in its saved source chunk")
		}
		hasTarget = hasTarget || strings.Contains(span.Text, target.RawTarget)
	}
	if !hasTarget {
		return invalid("evidence must include the unresolved target name")
	}
	return date, intent, nil
}

func validateTargetRecoveryDate(events []db_model.RecommendationEvent, date time.Time) error {
	count := 0
	for _, event := range events {
		if event.Status == string(domain.RecommendationEventStatusSuperseded) {
			continue
		}
		count++
		if event.RecommendDate.Format(time.DateOnly) != date.Format(time.DateOnly) {
			return fmt.Errorf("%w: recommend_date must match existing document recommendations", ErrTargetRecoveryConflict)
		}
	}
	if count == 0 {
		return fmt.Errorf("%w: no established document recommendation date", ErrTargetRecoveryConflict)
	}
	return nil
}

func targetRecoveryFingerprint(documentID, targetID int64, request TargetRecoveryRequest) (string, error) {
	request.Apply = false
	raw, err := json.Marshal(struct {
		DocumentID int64                 `json:"document_id"`
		TargetID   int64                 `json:"target_id"`
		Request    TargetRecoveryRequest `json:"request"`
	}{documentID, targetID, request})
	return utils.SHA256Hex(raw), err
}

func previousTargetRecovery(runs []db_model.InstrumentResolutionRun, targetID int64, fingerprint string) (*TargetRecoveryResult, error) {
	for _, run := range runs {
		var metadata targetRecoveryMetadata
		if json.Unmarshal(run.RawMetadataJSON, &metadata) != nil || run.Status != string(domain.ResolutionRunStatusSucceeded) || metadata.Operation != "target_recovery" || metadata.SourceUntrackableTargetID != targetID {
			continue
		}
		if metadata.InputSHA256 != fingerprint {
			return nil, fmt.Errorf("%w: this target was recovered with different input", ErrTargetRecoveryConflict)
		}
		if metadata.Result.Plan.ID <= 0 || metadata.Result.RecommendationEventID <= 0 {
			break
		}
		return &metadata.Result, nil
	}
	return nil, fmt.Errorf("%w: target is inactive without a matching recovery record", ErrTargetRecoveryConflict)
}
