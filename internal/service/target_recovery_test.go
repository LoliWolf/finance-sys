package service

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"finance-sys/internal/domain"
	"finance-sys/internal/domain/db_model"

	"github.com/stretchr/testify/require"
)

func targetRecoveryFixture() (db_model.UntrackableTarget, domain.Document, domain.ParseRun, TargetRecoveryRequest) {
	return db_model.UntrackableTarget{ID: 4, DocumentID: 7, RawTarget: "Example stock", ReasonCode: "SECURITY_NOT_FOUND", IsActive: true},
		domain.Document{ID: 7, Author: "Author", Institution: "Research"},
		domain.ParseRun{ID: 9, DocumentID: 7, Status: domain.ParseRunStatusParsed, Chunks: []domain.Chunk{{Index: 0, Text: "Watch Example stock in this sector."}}},
		TargetRecoveryRequest{ParseRunID: 9, RecommendDate: "2026-07-01", Note: "Approved source re-extraction; the original intent was not saved.", Intent: domain.PlanIntent{
			Symbol: "Example stock", Direction: domain.TradeDirectionLong, ReferencePriceNote: domain.ReferencePriceNotePriceMissingInText,
			Thesis: "Source lists this stock in the watched sector.", Confidence: 0.6, Evidence: []domain.EvidenceSpan{{ChunkIndex: 0, Text: "Watch Example stock"}},
		}}
}

func TestTargetRecoveryValidatesSavedSourceAndAuthor(t *testing.T) {
	target, doc, parsed, request := targetRecoveryFixture()
	date, intent, err := validateTargetRecoveryInput(target, doc, parsed, request)
	require.NoError(t, err)
	require.Equal(t, "2026-07-01", date.Format(time.DateOnly))
	require.Equal(t, doc.Author, intent.Analyst)
	require.Equal(t, doc.Institution, intent.Institution)
	require.Zero(t, intent.ReferencePrice)
}

func TestTargetRecoveryRejectsUnsafeInput(t *testing.T) {
	for _, name := range []string{"wrong document", "wrong parse", "failed parse", "wrong target", "different reason", "wrong author", "wrong institution", "missing evidence", "invented evidence", "wrong chunk", "unrelated evidence", "supplied price", "wrong price note", "missing note", "missing direction", "invalid date", "non-finite confidence"} {
		t.Run(name, func(t *testing.T) {
			target, doc, parsed, request := targetRecoveryFixture()
			switch name {
			case "wrong document":
				parsed.DocumentID = 99
			case "wrong parse":
				request.ParseRunID = 8
			case "failed parse":
				parsed.Status = domain.ParseRunStatusFailed
			case "wrong target":
				request.Intent.Symbol = "Another stock"
			case "different reason":
				target.ReasonCode = "SECTOR_NOT_TRADABLE"
			case "wrong author":
				request.Intent.Analyst = "Another author"
			case "wrong institution":
				request.Intent.Institution = "Another institution"
			case "missing evidence":
				request.Intent.Evidence = nil
			case "invented evidence":
				request.Intent.Evidence[0].Text = "Buy Example stock at 100"
			case "wrong chunk":
				request.Intent.Evidence[0].ChunkIndex = 99
			case "unrelated evidence":
				request.Intent.Evidence[0].Text = "in this sector."
			case "supplied price":
				request.Intent.ReferencePrice = 100
			case "wrong price note":
				request.Intent.ReferencePriceNote = domain.ReferencePriceNoteExplicitPriceMention
			case "missing note":
				request.Note = " "
			case "missing direction":
				request.Intent.Direction = ""
			case "invalid date":
				request.RecommendDate = "2026-99-01"
			case "non-finite confidence":
				request.Intent.Confidence = math.NaN()
			}
			_, _, err := validateTargetRecoveryInput(target, doc, parsed, request)
			require.ErrorIs(t, err, ErrInvalidTargetRecovery)
		})
	}
}

func TestTargetRecoveryPreservesEstablishedRecommendationDate(t *testing.T) {
	date := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	events := []db_model.RecommendationEvent{{Status: "NEEDS_REVIEW", RecommendDate: date}}
	require.NoError(t, validateTargetRecoveryDate(events, date))
	require.ErrorIs(t, validateTargetRecoveryDate(events, date.AddDate(0, 0, 1)), ErrTargetRecoveryConflict)
	require.ErrorIs(t, validateTargetRecoveryDate(nil, date), ErrTargetRecoveryConflict)
}

func TestTargetRecoveryFingerprintAndReplayAreIdempotent(t *testing.T) {
	_, _, _, request := targetRecoveryFixture()
	fingerprint, err := targetRecoveryFingerprint(7, 4, request)
	require.NoError(t, err)
	request.Apply = true
	same, err := targetRecoveryFingerprint(7, 4, request)
	require.NoError(t, err)
	require.Equal(t, fingerprint, same)
	metadata, err := json.Marshal(targetRecoveryMetadata{
		Operation: "target_recovery", SourceUntrackableTargetID: 4, InputSHA256: fingerprint,
		Result: TargetRecoveryResult{Applied: true, ResolutionRunID: 20, RecommendationEventID: 30, Plan: domain.CandidatePlan{ID: 10}},
	})
	require.NoError(t, err)
	runs := []db_model.InstrumentResolutionRun{{ID: 20, Status: "SUCCEEDED", RawMetadataJSON: metadata}}
	result, err := previousTargetRecovery(runs, 4, fingerprint)
	require.NoError(t, err)
	require.Equal(t, int64(30), result.RecommendationEventID)
	_, err = previousTargetRecovery(runs, 4, "different input")
	require.ErrorIs(t, err, ErrTargetRecoveryConflict)
	_, err = previousTargetRecovery(runs, 5, fingerprint)
	require.ErrorIs(t, err, ErrTargetRecoveryConflict)
	runs[0].Status = "FAILED"
	_, err = previousTargetRecovery(runs, 4, fingerprint)
	require.ErrorIs(t, err, ErrTargetRecoveryConflict)
}
