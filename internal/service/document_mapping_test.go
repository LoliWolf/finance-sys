package service

import (
	"errors"
	"testing"
	"time"

	"finance-sys/internal/agentclient"
	"finance-sys/internal/domain"

	"github.com/stretchr/testify/require"
)

func TestCandidatePlanToModelNormalizesTradeDateToDateOnlyUTC(t *testing.T) {
	loc := time.FixedZone("Asia/Hong_Kong", 8*60*60)
	plan := domain.CandidatePlan{
		DocumentID:     1,
		ParseRunID:     2,
		Analyst:        "tester",
		Institution:    "integration",
		Symbol:         "300502.SZ",
		AssetType:      domain.AssetTypeAShare,
		Market:         domain.MarketSZ,
		Strategy:       domain.RuleStrategyTextReferencePrice,
		Direction:      domain.TradeDirectionLong,
		TradeDate:      time.Date(2026, 2, 3, 0, 0, 0, 0, loc),
		ReferencePrice: 88.8,
		Confidence:     0.82,
		Status:         domain.CandidatePlanStatusReady,
		Thesis:         "source text supports the recommendation",
		Risks:          []string{"risk"},
		Evidence:       []domain.EvidenceSpan{{ChunkIndex: 0, Text: "evidence"}},
		PricingNote:    string(domain.ReferencePriceNoteExplicitPriceMention),
		ConfigVersion:  1,
		RuleVersion:    "rules-v2",
	}

	model, err := candidatePlanToModel(plan)

	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC), model.TradeDate)
}

func TestDocumentStatusAfterAnalysisFailureMarksInvalidForTerminalUntrackableTargets(t *testing.T) {
	status := documentStatusAfterAnalysisFailure([]domain.InstrumentResolution{
		{
			RawSymbol:  "AI应用",
			Status:     domain.InstrumentResolutionStatusNotFound,
			TargetKind: domain.InstrumentTargetKindUnknown,
			Reason:     "no active security matched",
		},
		{
			RawSymbol:  "CPO板块",
			Status:     domain.InstrumentResolutionStatusUntrackable,
			TargetKind: domain.InstrumentTargetKindSector,
			Reason:     "sector is not directly tradable",
		},
	}, errors.New("security not found for instrument"))

	require.Equal(t, domain.DocumentStatusInvalid, status)
}

func TestDocumentStatusAfterAnalysisFailureKeepsFailedForAmbiguousOrTransientFailures(t *testing.T) {
	ambiguous := documentStatusAfterAnalysisFailure([]domain.InstrumentResolution{
		{
			RawSymbol:  "重名标的",
			Status:     domain.InstrumentResolutionStatusAmbiguous,
			TargetKind: domain.InstrumentTargetKindUnknown,
			Reason:     "matched 2 active securities",
		},
	}, errors.New("ambiguous instrument"))
	require.Equal(t, domain.DocumentStatusFailed, ambiguous)

	noResolution := documentStatusAfterAnalysisFailure(nil, errors.New("agent returned FAILED status"))
	require.Equal(t, domain.DocumentStatusFailed, noResolution)
}

func TestDocumentStatusAfterParseFailureMarksInvalidForTerminalPDFPageCount(t *testing.T) {
	err := errors.New("pdftotext failed for article.pdf: exit status 99; ocr failed: RuntimeError: pdftoppm failed\nSyntax Error: Invalid page count 0\nWrong page range given: the first page (1) can not be after the last page (0).")

	require.Equal(t, domain.DocumentStatusInvalid, documentStatusAfterParseFailure(err))
}

func TestDocumentStatusAfterParseFailureKeepsFailedForOCRResourceFailures(t *testing.T) {
	err := errors.New("ocr failed for article.pdf: Windows OCR failed: OutOfMemoryException")

	require.Equal(t, domain.DocumentStatusFailed, documentStatusAfterParseFailure(err))
}

func TestDocumentStatusAfterAnalyzerFailureMarksInvalidWhenAgentExtractedNoIntents(t *testing.T) {
	status := documentStatusAfterAnalyzerFailure(AnalysisObservation{
		AgentResponse: &agentclient.ResolveDocumentResponse{
			Status:   agentclient.AgentStatusFailed,
			Warnings: []string{"no instrument intent extracted"},
		},
	}, errors.New("agent returned FAILED status"))

	require.Equal(t, domain.DocumentStatusInvalid, status)
}

func TestDocumentStatusAfterAnalyzerFailureMarksInvalidWhenProviderModerationBlocksContent(t *testing.T) {
	status := documentStatusAfterAnalyzerFailure(AnalysisObservation{
		AgentResponse: &agentclient.ResolveDocumentResponse{
			Status: agentclient.AgentStatusFailed,
			Warnings: []string{
				`llm extraction failed after 1 attempts: llm http 400: {"error":{"message":"Smart moderation blocked by ai"}}`,
			},
		},
	}, errors.New("agent returned FAILED status"))

	require.Equal(t, domain.DocumentStatusInvalid, status)
}

func TestDocumentStatusAfterAnalyzerFailureKeepsFailedForTransientAgentFailures(t *testing.T) {
	status := documentStatusAfterAnalyzerFailure(AnalysisObservation{
		AgentResponse: &agentclient.ResolveDocumentResponse{
			Status:   agentclient.AgentStatusFailed,
			Warnings: []string{"llm extraction failed after 4 attempts: llm http 500"},
		},
	}, errors.New("agent returned FAILED status"))

	require.Equal(t, domain.DocumentStatusFailed, status)
}

func TestParseRunHasAnalyzableTextRequiresNonBlankText(t *testing.T) {
	require.False(t, parseRunHasAnalyzableText(domain.ParseRun{}))
	require.False(t, parseRunHasAnalyzableText(domain.ParseRun{
		CleanedText: " \n\t ",
		Chunks:      []domain.Chunk{{Index: 0, Text: " "}},
	}))
	require.True(t, parseRunHasAnalyzableText(domain.ParseRun{
		CleanedText: "推荐新易盛",
	}))
	require.True(t, parseRunHasAnalyzableText(domain.ParseRun{
		Chunks: []domain.Chunk{{Index: 0, Text: "推荐新易盛"}},
	}))
}
