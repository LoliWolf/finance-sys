package service

import (
	"testing"
	"time"

	"finance-sys/internal/domain"
	"finance-sys/internal/domain/db_model"

	"github.com/stretchr/testify/require"
)

func TestNormalizeBloggerName(t *testing.T) {
	require.Equal(t, "alice research", normalizeBloggerName("  Alice \t Research  "))
	require.Equal(t, "张三", normalizeBloggerName("  张三  "))
}

func TestRecommendationEventDedupeKeyIsStableAndSensitive(t *testing.T) {
	blogger := db_model.Blogger{
		NormalizedName: "alice",
		Institution:    "Research",
	}
	plan := recommendationTestPlan()

	key := recommendationEventDedupeKey(blogger, plan)
	require.Len(t, key, 64)
	require.Equal(t, key, recommendationEventDedupeKey(blogger, plan))

	changed := plan
	changed.Direction = domain.TradeDirectionShort
	require.NotEqual(t, key, recommendationEventDedupeKey(blogger, changed))
	require.Equal(t, key, recommendationEventDedupeKey(blogger, plan))
}

func TestRecommendationStatusFromPlan(t *testing.T) {
	ready := recommendationTestPlan()
	ready.Status = domain.CandidatePlanStatusReady
	require.Equal(t, domain.RecommendationEventStatusActive, recommendationStatusFromPlan(ready))

	review := recommendationTestPlan()
	review.Status = domain.CandidatePlanStatusNeedsReview
	require.Equal(t, domain.RecommendationEventStatusNeedsReview, recommendationStatusFromPlan(review))
}

func TestRecommendationEventFromPlanMapsFields(t *testing.T) {
	blogger := &db_model.Blogger{
		ID:             3,
		Name:           "Alice",
		NormalizedName: "alice",
		Institution:    "Research",
	}
	document := domain.Document{ID: 7, Author: "Fallback", Institution: "DocInst"}
	plan := recommendationTestPlan()

	event := recommendationEventFromPlan(document, plan, blogger)

	require.Equal(t, int64(3), event.BloggerID)
	require.Equal(t, int64(7), event.SourceDocumentID)
	require.NotNil(t, event.PlanID)
	require.Equal(t, int64(11), *event.PlanID)
	require.Equal(t, int64(13), event.ParseRunID)
	require.Equal(t, "300502.SZ", event.Symbol)
	require.Equal(t, "A_SHARE", event.AssetType)
	require.Equal(t, "SZ", event.Market)
	require.Equal(t, "LONG", event.Direction)
	require.Equal(t, time.Date(2026, 6, 7, 0, 0, 0, 0, time.UTC), event.RecommendDate)
	require.Equal(t, 88.8, event.ReferencePrice)
	require.Equal(t, 0.81, event.Confidence)
	require.Equal(t, "ACTIVE", event.Status)
	require.Equal(t, "source text supports the recommendation", event.Thesis)
	require.Equal(t, int64(2), event.ConfigVersion)
	require.Equal(t, "rules-v2", event.RuleVersion)
	require.Len(t, event.DedupeKey, 64)
}

func TestRecommendationEventFromPlanNormalizesRecommendDateToDateOnlyUTC(t *testing.T) {
	loc := time.FixedZone("Asia/Hong_Kong", 8*60*60)
	blogger := &db_model.Blogger{ID: 3, Name: "Alice", NormalizedName: "alice", Institution: "Research"}
	document := domain.Document{ID: 7}
	plan := recommendationTestPlan()
	plan.TradeDate = time.Date(2026, 2, 3, 0, 0, 0, 0, loc)

	event := recommendationEventFromPlan(document, plan, blogger)

	require.Equal(t, time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC), event.RecommendDate)
}

func TestRecommendationEventFromSectorIntentUsesActualParseRun(t *testing.T) {
	loc := time.FixedZone("Asia/Shanghai", 8*60*60)
	document := domain.Document{ID: 7}
	blogger := db_model.Blogger{ID: 3, NormalizedName: "alice", Institution: "Research"}
	intent := domain.TrackablePlanIntent{
		TSCode:     "BK1128.DC",
		AssetType:  domain.AssetTypeSector,
		Market:     domain.MarketDC,
		Direction:  domain.TradeDirectionLong,
		Confidence: 0.82,
		Thesis:     "CPO 板块景气度提升",
	}

	event := recommendationEventFromSectorIntent(
		document,
		blogger,
		913,
		intent,
		time.Date(2026, 8, 8, 15, 0, 0, 0, loc),
		16,
	)

	require.Equal(t, int64(913), event.ParseRunID)
	require.Equal(t, "BK1128.DC", event.Symbol)
	require.Equal(t, "SECTOR", event.AssetType)
	require.Equal(t, "DC", event.Market)
	require.Nil(t, event.PlanID)
	require.Equal(t, time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC), event.RecommendDate)
	require.Equal(t, int64(16), event.ConfigVersion)
	require.Equal(t, sectorRecommendationRuleVersion, event.RuleVersion)
}

func recommendationTestPlan() domain.CandidatePlan {
	return domain.CandidatePlan{
		ID:             11,
		DocumentID:     7,
		ParseRunID:     13,
		Analyst:        "Alice",
		Institution:    "Research",
		Symbol:         "300502.SZ",
		AssetType:      domain.AssetTypeAShare,
		Market:         domain.MarketSZ,
		Direction:      domain.TradeDirectionLong,
		TradeDate:      time.Date(2026, 6, 7, 0, 0, 0, 0, time.UTC),
		ReferencePrice: 88.8,
		Confidence:     0.81,
		Status:         domain.CandidatePlanStatusReady,
		Thesis:         "source text supports the recommendation",
		Evidence:       []domain.EvidenceSpan{{ChunkIndex: 0, Text: "source evidence"}},
		ConfigVersion:  2,
		RuleVersion:    "rules-v2",
	}
}
