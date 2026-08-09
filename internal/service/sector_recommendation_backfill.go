package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"finance-sys/internal/agentclient"
	"finance-sys/internal/dal"
	"finance-sys/internal/domain"
	"finance-sys/internal/llm"

	"gorm.io/gorm"
)

func (s *DocumentService) BackfillSectorRecommendationsFromLatestParseRun(ctx context.Context, documentID int64, recommendDate time.Time) (int, error) {
	documentModel, err := dal.Documents.QueryByID(ctx, s.db, documentID)
	if err != nil {
		return 0, err
	}
	parseRunModel, err := dal.ParseRuns.QueryLatestParsedByDocumentID(ctx, s.db, documentID)
	if err != nil {
		return 0, err
	}
	document := mapDocument(documentModel)
	parseRun, err := mapParseRun(parseRunModel)
	if err != nil {
		return 0, err
	}
	if !parseRunHasAnalyzableText(*parseRun) {
		return 0, fmt.Errorf("document %d latest parsed run has no analyzable text", documentID)
	}

	releaseLLM, err := s.processing.AcquireLLM(ctx)
	if err != nil {
		return 0, err
	}
	analysis, analyzeErr := s.analyzeWithObservation(ctx, *document, *parseRun)
	releaseLLM()
	if analyzeErr != nil && !agentResponseRepresentsNoPlanIntents(analysis) {
		return 0, analyzeErr
	}
	for _, intent := range analysis.Intents {
		if err := llm.ValidateIntent(intent); err != nil {
			return 0, fmt.Errorf("invalid plan intent: %w", err)
		}
	}
	if len(analysis.Intents) == 0 {
		return 0, s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return dal.RecommendationEvents.DeleteDirectSectorEventsByDocumentID(ctx, tx, documentID)
		})
	}
	trackable, resolutions, assembleErr := s.assembler.Assemble(ctx, analysis.Intents)
	sectorIntents := make([]domain.TrackablePlanIntent, 0)
	for _, intent := range trackable {
		if intent.AssetType == domain.AssetTypeSector {
			sectorIntents = append(sectorIntents, intent)
		}
	}
	if assembleErr != nil && !onlyExpectedResolutionFailures(resolutions) {
		return 0, assembleErr
	}

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := dal.RecommendationEvents.DeleteDirectSectorEventsByDocumentID(ctx, tx, documentID); err != nil {
			return err
		}
		for _, intent := range sectorIntents {
			if _, err := s.upsertRecommendationEventForSector(ctx, tx, *document, parseRun.ID, intent, recommendDate); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(sectorIntents), nil
}

func agentResponseRepresentsNoPlanIntents(analysis AnalysisObservation) bool {
	response := analysis.AgentResponse
	if response == nil || len(response.RawIntents) > 0 || len(response.CandidatePlanInput) > 0 {
		return false
	}
	if response.Status != agentclient.AgentStatusFailed && len(response.UntrackableTargets) > 0 {
		return true
	}
	return response.Status == agentclient.AgentStatusFailed &&
		len(response.Warnings) == 1 &&
		strings.EqualFold(strings.TrimSpace(response.Warnings[0]), "no instrument intent extracted")
}

func onlyExpectedResolutionFailures(resolutions []domain.InstrumentResolution) bool {
	if len(resolutions) == 0 {
		return false
	}
	for _, resolution := range resolutions {
		switch resolution.Status {
		case domain.InstrumentResolutionStatusResolved,
			domain.InstrumentResolutionStatusAmbiguous,
			domain.InstrumentResolutionStatusUntrackable:
			continue
		case domain.InstrumentResolutionStatusNotFound:
			if resolution.Reason == "no active security matched" {
				continue
			}
		}
		return false
	}
	return true
}
