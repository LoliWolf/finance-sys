package service

import (
	"context"
	"strings"
	"time"

	"finance-sys/internal/dal"
	"finance-sys/internal/domain"
	"finance-sys/internal/domain/db_model"
	"finance-sys/internal/utils"

	"gorm.io/gorm"
)

const unknownBloggerName = "UNKNOWN"

const sectorRecommendationRuleVersion = "sector-index-v1"

func normalizeBloggerName(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(name)), " "))
}

func recommendationEventDedupeKey(blogger db_model.Blogger, plan domain.CandidatePlan) string {
	return BuildRecommendationEventDedupeKey(
		blogger.NormalizedName,
		blogger.Institution,
		plan.Symbol,
		string(plan.Direction),
		plan.TradeDate,
	)
}

// BuildRecommendationEventDedupeKey is shared with audited data-repair tools
// so online writes and repairs use the exact same business key.
func BuildRecommendationEventDedupeKey(normalizedName string, institution string, symbol string, direction string, recommendDate time.Time) string {
	raw := strings.Join([]string{
		normalizedName,
		institution,
		symbol,
		direction,
		recommendDate.Format(time.DateOnly),
	}, "|")
	return utils.SHA256Hex([]byte(raw))
}

func recommendationStatusFromPlan(plan domain.CandidatePlan) domain.RecommendationEventStatus {
	if plan.Status == domain.CandidatePlanStatusReady {
		return domain.RecommendationEventStatusActive
	}
	return domain.RecommendationEventStatusNeedsReview
}

func recommendationEventFromPlan(document domain.Document, plan domain.CandidatePlan, blogger *db_model.Blogger) *db_model.RecommendationEvent {
	planID := plan.ID
	return &db_model.RecommendationEvent{
		BloggerID:        blogger.ID,
		SourceDocumentID: document.ID,
		PlanID:           &planID,
		ParseRunID:       plan.ParseRunID,
		Symbol:           plan.Symbol,
		AssetType:        string(plan.AssetType),
		Market:           string(plan.Market),
		Direction:        string(plan.Direction),
		RecommendDate:    dbDateOnly(plan.TradeDate),
		ReferencePrice:   plan.ReferencePrice,
		Confidence:       plan.Confidence,
		Status:           string(recommendationStatusFromPlan(plan)),
		Thesis:           plan.Thesis,
		DedupeKey:        recommendationEventDedupeKey(*blogger, plan),
		ConfigVersion:    plan.ConfigVersion,
		RuleVersion:      plan.RuleVersion,
	}
}

func (s *DocumentService) upsertRecommendationEventForPlan(ctx context.Context, db *gorm.DB, document domain.Document, plan domain.CandidatePlan) (*domain.RecommendationEvent, error) {
	blogger, err := s.resolveBloggerForPlan(ctx, db, document, plan)
	if err != nil {
		return nil, err
	}

	eventModel := recommendationEventFromPlan(document, plan, blogger)
	planID := plan.ID
	return s.upsertRecommendationEvent(ctx, db, document, blogger, eventModel, &planID, recommendationEvidenceModels(document.ID, &planID, plan.Evidence, 0))
}

func (s *DocumentService) upsertRecommendationEventForSector(ctx context.Context, db *gorm.DB, document domain.Document, parseRunID int64, intent domain.TrackablePlanIntent, recommendDate time.Time) (*domain.RecommendationEvent, error) {
	blogger, err := s.resolveBlogger(ctx, db, document, intent.Analyst, intent.Institution)
	if err != nil {
		return nil, err
	}
	eventModel := recommendationEventFromSectorIntent(document, *blogger, parseRunID, intent, recommendDate, s.currentConfig().Meta.ConfigVersion)
	return s.upsertRecommendationEvent(ctx, db, document, blogger, eventModel, nil, recommendationEvidenceModels(document.ID, nil, intent.Evidence, 0))
}

func recommendationEventFromSectorIntent(document domain.Document, blogger db_model.Blogger, parseRunID int64, intent domain.TrackablePlanIntent, recommendDate time.Time, configVersion int64) *db_model.RecommendationEvent {
	return &db_model.RecommendationEvent{
		BloggerID:        blogger.ID,
		SourceDocumentID: document.ID,
		PlanID:           nil,
		ParseRunID:       parseRunID,
		Symbol:           intent.TSCode,
		AssetType:        string(domain.AssetTypeSector),
		Market:           string(domain.MarketDC),
		Direction:        string(intent.Direction),
		RecommendDate:    dbDateOnly(recommendDate),
		ReferencePrice:   intent.ReferencePrice,
		Confidence:       intent.Confidence,
		Status:           string(domain.RecommendationEventStatusActive),
		Thesis:           intent.Thesis,
		DedupeKey: BuildRecommendationEventDedupeKey(
			blogger.NormalizedName,
			blogger.Institution,
			intent.TSCode,
			string(intent.Direction),
			recommendDate,
		),
		ConfigVersion: configVersion,
		RuleVersion:   sectorRecommendationRuleVersion,
	}
}

func (s *DocumentService) upsertRecommendationEvent(ctx context.Context, db *gorm.DB, document domain.Document, blogger *db_model.Blogger, eventModel *db_model.RecommendationEvent, expectedPlanID *int64, evidenceModels []db_model.RecommendationEventEvidence) (*domain.RecommendationEvent, error) {
	if err := dal.RecommendationEvents.UpsertByDedupeKey(ctx, db, eventModel); err != nil {
		return nil, err
	}
	eventModel, err := dal.RecommendationEvents.QueryByDedupeKey(ctx, db, eventModel.DedupeKey)
	if err != nil {
		return nil, err
	}
	if eventModel.SourceDocumentID == document.ID && sameOptionalInt64(eventModel.PlanID, expectedPlanID) {
		if err := dal.RecommendationEventEvidences.DeleteByEventID(ctx, db, eventModel.ID); err != nil {
			return nil, err
		}
		for index := range evidenceModels {
			evidenceModels[index].RecommendationEventID = eventModel.ID
		}
		if err := dal.RecommendationEventEvidences.CreateBatch(ctx, db, evidenceModels); err != nil {
			return nil, err
		}
	} else {
		evidenceModels, err = dal.RecommendationEventEvidences.QueryByEventID(ctx, db, eventModel.ID)
		if err != nil {
			return nil, err
		}
	}
	return mapRecommendationEvent(eventModel, blogger, evidenceModels), nil
}

func (s *DocumentService) ListRecommendationEvents(ctx context.Context, limit int) ([]domain.RecommendationEvent, error) {
	return s.ListRecommendationEventsByQuery(ctx, domain.RecommendationEventQuery{Limit: limit})
}

func (s *DocumentService) ListRecommendationEventsByQuery(ctx context.Context, query domain.RecommendationEventQuery) ([]domain.RecommendationEvent, error) {
	param := dal.QueryParam{
		Orders: []dal.OrderParam{dal.OrderBy("created_at", true), dal.OrderBy("id", true)},
		Limit:  query.Limit,
	}
	if query.Symbol != "" {
		param.Where = append(param.Where, dal.Eq("symbol", query.Symbol))
	}
	if query.Direction != "" {
		param.Where = append(param.Where, dal.Eq("direction", string(query.Direction)))
	}
	if query.Status != "" {
		param.Where = append(param.Where, dal.Eq("status", string(query.Status)))
	}
	rows, err := dal.RecommendationEvents.QueryByParam(ctx, s.db, param)
	if err != nil {
		return nil, err
	}
	return s.mapRecommendationEventRows(ctx, rows, false)
}

func (s *DocumentService) ListRecommendationEventsByDocumentID(ctx context.Context, documentID int64) ([]domain.RecommendationEvent, error) {
	rows, err := dal.RecommendationEvents.QueryByDocumentID(ctx, s.db, documentID)
	if err != nil {
		return nil, err
	}
	return s.mapRecommendationEventRows(ctx, rows, false)
}

func (s *DocumentService) GetRecommendationEventByID(ctx context.Context, id int64) (*domain.RecommendationEvent, error) {
	row, err := dal.RecommendationEvents.QueryByID(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	blogger, err := dal.Bloggers.QueryByID(ctx, s.db, row.BloggerID)
	if err != nil {
		return nil, err
	}
	evidenceRows, err := dal.RecommendationEventEvidences.QueryByEventID(ctx, s.db, row.ID)
	if err != nil {
		return nil, err
	}
	return mapRecommendationEvent(row, blogger, evidenceRows), nil
}

func (s *DocumentService) resolveBloggerForPlan(ctx context.Context, db *gorm.DB, document domain.Document, plan domain.CandidatePlan) (*db_model.Blogger, error) {
	return s.resolveBlogger(ctx, db, document, plan.Analyst, plan.Institution)
}

func (s *DocumentService) resolveBlogger(ctx context.Context, db *gorm.DB, document domain.Document, analyst string, planInstitution string) (*db_model.Blogger, error) {
	name := strings.TrimSpace(analyst)
	if name == "" {
		name = strings.TrimSpace(document.Author)
	}
	if name == "" {
		name = strings.TrimSpace(s.currentConfig().Document.SourceDefaults.Author)
	}
	if name == "" {
		name = unknownBloggerName
	}

	institution := strings.TrimSpace(planInstitution)
	if institution == "" {
		institution = strings.TrimSpace(document.Institution)
	}

	model := &db_model.Blogger{
		Name:           name,
		NormalizedName: normalizeBloggerName(name),
		Institution:    institution,
		SourceType:     "DOCUMENT",
	}
	if err := dal.Bloggers.UpsertByNormalizedNameAndInstitution(ctx, db, model); err != nil {
		return nil, err
	}
	return dal.Bloggers.QueryByNormalizedNameAndInstitution(ctx, db, model.NormalizedName, model.Institution)
}

func (s *DocumentService) mapRecommendationEventRows(ctx context.Context, rows []db_model.RecommendationEvent, includeEvidence bool) ([]domain.RecommendationEvent, error) {
	items := make([]domain.RecommendationEvent, 0, len(rows))
	bloggers := make(map[int64]*db_model.Blogger)
	for i := range rows {
		blogger, ok := bloggers[rows[i].BloggerID]
		if !ok {
			var err error
			blogger, err = dal.Bloggers.QueryByID(ctx, s.db, rows[i].BloggerID)
			if err != nil {
				return nil, err
			}
			bloggers[rows[i].BloggerID] = blogger
		}
		var evidenceRows []db_model.RecommendationEventEvidence
		if includeEvidence {
			var err error
			evidenceRows, err = dal.RecommendationEventEvidences.QueryByEventID(ctx, s.db, rows[i].ID)
			if err != nil {
				return nil, err
			}
		}
		items = append(items, *mapRecommendationEvent(&rows[i], blogger, evidenceRows))
	}
	return items, nil
}

func recommendationEvidenceModels(documentID int64, planID *int64, evidenceSpans []domain.EvidenceSpan, eventID int64) []db_model.RecommendationEventEvidence {
	items := make([]db_model.RecommendationEventEvidence, 0, len(evidenceSpans))
	for _, evidence := range evidenceSpans {
		items = append(items, db_model.RecommendationEventEvidence{
			RecommendationEventID: eventID,
			SourceDocumentID:      documentID,
			PlanID:                planID,
			ChunkIndex:            int32(evidence.ChunkIndex),
			EvidenceText:          evidence.Text,
		})
	}
	return items
}

func sameOptionalInt64(left *int64, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func mapRecommendationEvent(row *db_model.RecommendationEvent, blogger *db_model.Blogger, evidenceRows []db_model.RecommendationEventEvidence) *domain.RecommendationEvent {
	evidence := make([]domain.EvidenceSpan, 0, len(evidenceRows))
	for _, item := range evidenceRows {
		evidence = append(evidence, domain.EvidenceSpan{
			ChunkIndex: int(item.ChunkIndex),
			Text:       item.EvidenceText,
		})
	}
	bloggerName := ""
	if blogger != nil {
		bloggerName = blogger.Name
	}
	return &domain.RecommendationEvent{
		ID:               row.ID,
		BloggerID:        row.BloggerID,
		BloggerName:      bloggerName,
		SourceDocumentID: row.SourceDocumentID,
		PlanID:           row.PlanID,
		ParseRunID:       row.ParseRunID,
		Symbol:           row.Symbol,
		AssetType:        domain.AssetType(row.AssetType),
		Market:           domain.Market(row.Market),
		Direction:        domain.TradeDirection(row.Direction),
		RecommendDate:    row.RecommendDate.UTC(),
		ReferencePrice:   row.ReferencePrice,
		Confidence:       row.Confidence,
		Status:           domain.RecommendationEventStatus(row.Status),
		Thesis:           row.Thesis,
		Evidence:         evidence,
		DedupeKey:        row.DedupeKey,
		ConfigVersion:    row.ConfigVersion,
		RuleVersion:      row.RuleVersion,
		CreatedAt:        row.CreatedAt.UTC(),
		UpdatedAt:        row.UpdatedAt.UTC(),
	}
}
