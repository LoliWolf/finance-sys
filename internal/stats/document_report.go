package stats

import (
	"context"
	"errors"
	"math"
	"sort"
	"strings"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/dal"
	"finance-sys/internal/domain"
	"finance-sys/internal/domain/db_model"
	"finance-sys/internal/evaluation"

	"gorm.io/gorm"
)

const (
	documentReportStatusEmpty           = "EMPTY"
	documentReportStatusNeedsEvaluation = "NEEDS_EVALUATION"
	documentReportStatusPartial         = "PARTIAL"
	documentMetricStatusNotEvaluated    = "NOT_EVALUATED"
	documentMetricStatusOutdated        = "OUTDATED"
)

type documentReportAggregateRow struct {
	DocumentID            int64
	RecommendDateFrom     *time.Time
	RecommendDateTo       *time.Time
	RecommendationCount   int64
	BloggerCount          int64
	MetricCount           int64
	ReadyMetricCount      int64
	PendingMetricCount    int64
	IncompleteMetricCount int64
}

type documentUntrackableCountRow struct {
	DocumentID int64
	Count      int64
}

type documentRecommendationRow struct {
	RecommendationEventID int64
	BloggerID             int64
	BloggerName           string
	Institution           string
	TSCode                string
	Symbol                string
	SecurityName          string
	AssetType             string
	Market                string
	SectorType            string
	Direction             string
	RecommendDate         time.Time
	ReferencePrice        float64
	Confidence            float64
	RecommendationStatus  string
	Thesis                string
}

type documentReportGroupBuilder struct {
	item       DocumentReportBloggerGroup
	windowData map[int]*aggregate
	current    aggregate
}

func (s *Service) DocumentReports(ctx context.Context, filter DocumentReportListFilter) (*DocumentReportList, error) {
	filter = normalizeDocumentReportListFilter(filter)
	base := func() *gorm.DB {
		tx := s.db.WithContext(ctx).Table("documents AS d")
		if filter.Query != "" {
			search := "%" + filter.Query + "%"
			tx = tx.Where("d.title LIKE ? OR d.file_name LIKE ? OR d.author LIKE ? OR d.institution LIKE ?", search, search, search, search)
		}
		if filter.Status != "" {
			tx = tx.Where("d.status = ?", filter.Status)
		}
		if filter.CreatedFrom != nil {
			tx = tx.Where("d.created_at >= ?", *filter.CreatedFrom)
		}
		if filter.CreatedBefore != nil {
			tx = tx.Where("d.created_at < ?", *filter.CreatedBefore)
		}
		return tx
	}

	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, err
	}
	items := make([]DocumentReportListItem, 0, filter.Limit)
	if err := base().
		Select(`d.id AS document_id, d.author, d.institution, d.title, d.file_name, d.status,
			d.config_version, d.created_at, d.updated_at`).
		Order("d.created_at DESC, d.id DESC").
		Limit(filter.Limit).
		Offset(filter.Offset).
		Scan(&items).Error; err != nil {
		return nil, err
	}

	windows := configuredWindows(s.runtime)
	quoteSource := configuredQuoteSource(s.runtime)
	calcVersion := configuredCalcVersion(s.runtime)
	if len(items) > 0 {
		documentIDs := make([]int64, 0, len(items))
		for _, item := range items {
			documentIDs = append(documentIDs, item.DocumentID)
		}
		aggregates, err := s.documentReportAggregates(ctx, documentIDs, windows, quoteSource, calcVersion)
		if err != nil {
			return nil, err
		}
		untrackableCounts, err := s.documentUntrackableCounts(ctx, documentIDs)
		if err != nil {
			return nil, err
		}
		for index := range items {
			item := &items[index]
			row := aggregates[item.DocumentID]
			item.RecommendDateFrom = row.RecommendDateFrom
			item.RecommendDateTo = row.RecommendDateTo
			item.RecommendationCount = int(row.RecommendationCount)
			item.BloggerCount = int(row.BloggerCount)
			item.UntrackableCount = untrackableCounts[item.DocumentID]
			item.ExpectedMetricCount = item.RecommendationCount * len(windows)
			item.ReadyMetricCount = int(row.ReadyMetricCount)
			item.PendingMetricCount = int(row.PendingMetricCount)
			item.IncompleteMetricCount = int(row.IncompleteMetricCount)
			item.MissingMetricCount = maxInt(item.ExpectedMetricCount-int(row.MetricCount), 0)
			item.ReportStatus = documentReportStatus(*item)
		}
	}

	dataAsOf, err := s.documentReportDataAsOf(ctx, quoteSource)
	if err != nil {
		return nil, err
	}
	page, totalPages := paginationMetadata(total, filter.Offset, filter.Limit)
	return &DocumentReportList{
		Total:      total,
		Page:       page,
		PageSize:   filter.Limit,
		TotalPages: totalPages,
		DataAsOf:   dataAsOf,
		Items:      items,
	}, nil
}

func (s *Service) DocumentReport(ctx context.Context, documentID int64) (*DocumentReport, error) {
	var document DocumentReportDocument
	err := s.db.WithContext(ctx).
		Table("documents AS d").
		Select(`d.id AS document_id, d.author, d.institution, d.title, d.file_name, d.status,
			d.config_version, d.created_at, d.updated_at`).
		Where("d.id = ?", documentID).
		Take(&document).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, dal.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	var rows []documentRecommendationRow
	err = s.db.WithContext(ctx).
		Table("recommendation_events AS re").
		Select(`re.id AS recommendation_event_id, re.blogger_id, b.name AS blogger_name, b.institution,
			COALESCE(sm.ts_code, '') AS ts_code, re.symbol, COALESCE(sm.name, '') AS security_name,
			re.asset_type, re.market, COALESCE(sm.sector_type, '') AS sector_type, re.direction,
			re.recommend_date, re.reference_price, re.confidence, re.status AS recommendation_status, re.thesis`).
		Joins("JOIN bloggers AS b ON b.id = re.blogger_id").
		Joins("LEFT JOIN security_master AS sm ON sm.ts_code = CASE WHEN re.symbol LIKE '%.%' THEN re.symbol ELSE CONCAT(re.symbol, '.', re.market) END").
		Where("re.source_document_id = ?", documentID).
		Where("re.status <> ?", string(domain.RecommendationEventStatusSuperseded)).
		Order("re.recommend_date DESC, re.id DESC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	eventIDs := make([]int64, 0, len(rows))
	for _, row := range rows {
		eventIDs = append(eventIDs, row.RecommendationEventID)
	}
	quoteSource := configuredQuoteSource(s.runtime)
	calcVersion := configuredCalcVersion(s.runtime)
	windows := configuredWindows(s.runtime)
	metrics, err := dal.RecommendationEventWindowMetrics.QueryByEventIDsAndSource(ctx, s.db, eventIDs, quoteSource)
	if err != nil {
		return nil, err
	}
	evidences, err := dal.RecommendationEventEvidences.QueryByEventIDs(ctx, s.db, eventIDs)
	if err != nil {
		return nil, err
	}
	untrackableRows, err := dal.UntrackableTargets.QueryActiveByDocumentID(ctx, s.db, documentID)
	if err != nil {
		return nil, err
	}
	dataAsOf, err := s.documentReportDataAsOf(ctx, quoteSource)
	if err != nil {
		return nil, err
	}

	metricsByEvent := make(map[int64]map[int]db_model.RecommendationEventWindowMetric, len(rows))
	for _, metric := range metrics {
		byWindow := metricsByEvent[metric.RecommendationEventID]
		if byWindow == nil {
			byWindow = make(map[int]db_model.RecommendationEventWindowMetric, len(windows))
			metricsByEvent[metric.RecommendationEventID] = byWindow
		}
		byWindow[int(metric.WindowDays)] = metric
	}
	evidenceByEvent := make(map[int64][]Evidence, len(rows))
	for _, evidence := range evidences {
		evidenceByEvent[evidence.RecommendationEventID] = append(evidenceByEvent[evidence.RecommendationEventID], Evidence{
			ChunkIndex: int(evidence.ChunkIndex),
			Text:       evidence.EvidenceText,
		})
	}

	recommendations := make([]DocumentReportRecommendation, 0, len(rows))
	tsCodeSet := make(map[string]struct{})
	for _, row := range rows {
		item := DocumentReportRecommendation{
			RecommendationEventID: row.RecommendationEventID,
			BloggerID:             row.BloggerID,
			BloggerName:           row.BloggerName,
			Institution:           row.Institution,
			TSCode:                row.TSCode,
			Symbol:                row.Symbol,
			SecurityName:          row.SecurityName,
			AssetType:             row.AssetType,
			Market:                row.Market,
			SectorType:            row.SectorType,
			Direction:             row.Direction,
			RecommendDate:         row.RecommendDate,
			ReferencePrice:        row.ReferencePrice,
			Confidence:            row.Confidence,
			RecommendationStatus:  row.RecommendationStatus,
			Thesis:                row.Thesis,
			Evidence:              evidenceByEvent[row.RecommendationEventID],
			Windows:               make([]DocumentReportWindowMetric, 0, len(windows)),
		}
		if item.Evidence == nil {
			item.Evidence = []Evidence{}
		}
		byWindow := metricsByEvent[row.RecommendationEventID]
		for _, window := range windows {
			metric, exists := byWindow[window]
			item.Windows = append(item.Windows, reportWindowMetric(window, metric, exists, calcVersion))
			if exists {
				fillRecommendationIdentityFromMetric(&item, metric)
			}
		}
		if item.TSCode != "" {
			tsCodeSet[item.TSCode] = struct{}{}
		}
		recommendations = append(recommendations, item)
	}

	latestQuotes := make(map[string]db_model.StockDailyQuote, len(tsCodeSet))
	if dataAsOf != nil && len(tsCodeSet) > 0 {
		tsCodes := make([]string, 0, len(tsCodeSet))
		for tsCode := range tsCodeSet {
			tsCodes = append(tsCodes, tsCode)
		}
		sort.Strings(tsCodes)
		quotes, queryErr := dal.StockDailyQuotes.QueryLatestByTSCodesAt(ctx, s.db, tsCodes, quoteSource, *dataAsOf)
		if queryErr != nil {
			return nil, queryErr
		}
		for _, quote := range quotes {
			latestQuotes[quote.TSCode] = quote
		}
	}
	winThreshold := configuredWinThreshold(s.runtime)
	for index := range recommendations {
		item := &recommendations[index]
		item.Current = reportCurrentMetric(*item, dataAsOf, latestQuotes[item.TSCode], metricsByEvent[item.RecommendationEventID], windows, calcVersion, winThreshold)
	}

	summary, bloggers := aggregateDocumentReport(recommendations, windows)
	untrackables := make([]DocumentReportUntrackableTarget, 0, len(untrackableRows))
	for _, row := range untrackableRows {
		untrackables = append(untrackables, DocumentReportUntrackableTarget{
			ID:               row.ID,
			RawTarget:        row.RawTarget,
			NormalizedTarget: row.NormalizedTarget,
			TargetKind:       row.TargetKind,
			ReasonCode:       row.ReasonCode,
			ReasonMessage:    row.ReasonMessage,
			Source:           row.Source,
		})
	}
	summary.UntrackableCount = len(untrackables)
	entryPriceRule, basePriceRule, minCoverage := configuredReportMethodology(s.runtime)
	return &DocumentReport{
		GeneratedAt:        time.Now().UTC(),
		DataAsOf:           dataAsOf,
		QuoteSource:        quoteSource,
		CalcVersion:        calcVersion,
		Windows:            windows,
		Document:           document,
		Summary:            summary,
		Bloggers:           bloggers,
		Recommendations:    recommendations,
		UntrackableTargets: untrackables,
		Methodology: DocumentReportMethodology{
			EntryPriceRule:        entryPriceRule,
			BasePriceRule:         basePriceRule,
			WinThresholdRatio:     winThreshold,
			MinQuoteCoverageRatio: minCoverage,
		},
	}, nil
}

func (s *Service) documentReportAggregates(ctx context.Context, documentIDs []int64, windows []int, quoteSource string, calcVersion string) (map[int64]documentReportAggregateRow, error) {
	var rows []documentReportAggregateRow
	err := s.db.WithContext(ctx).
		Table("recommendation_events AS re").
		Select(`re.source_document_id AS document_id, MIN(re.recommend_date) AS recommend_date_from,
			MAX(re.recommend_date) AS recommend_date_to, COUNT(DISTINCT re.id) AS recommendation_count,
			COUNT(DISTINCT re.blogger_id) AS blogger_count, COUNT(m.id) AS metric_count,
			SUM(CASE WHEN m.status = 'READY' AND m.calc_version = ? THEN 1 ELSE 0 END) AS ready_metric_count,
			SUM(CASE WHEN m.status = 'PENDING' AND m.calc_version = ? THEN 1 ELSE 0 END) AS pending_metric_count,
			SUM(CASE WHEN m.id IS NOT NULL AND NOT (m.calc_version = ? AND m.status IN ('READY', 'PENDING')) THEN 1 ELSE 0 END) AS incomplete_metric_count`, calcVersion, calcVersion, calcVersion).
		Joins("LEFT JOIN recommendation_event_window_metrics AS m ON m.recommendation_event_id = re.id AND m.quote_source = ? AND m.window_days IN ?", quoteSource, windows).
		Where("re.source_document_id IN ?", documentIDs).
		Where("re.status <> ?", string(domain.RecommendationEventStatusSuperseded)).
		Group("re.source_document_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make(map[int64]documentReportAggregateRow, len(rows))
	for _, row := range rows {
		result[row.DocumentID] = row
	}
	return result, nil
}

func (s *Service) documentUntrackableCounts(ctx context.Context, documentIDs []int64) (map[int64]int, error) {
	var rows []documentUntrackableCountRow
	err := s.db.WithContext(ctx).
		Table("untrackable_targets AS u").
		Select("u.document_id, COUNT(*) AS count").
		Where("u.document_id IN ?", documentIDs).
		Where("u.is_active = ?", true).
		Group("u.document_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make(map[int64]int, len(rows))
	for _, row := range rows {
		result[row.DocumentID] = int(row.Count)
	}
	return result, nil
}

func (s *Service) documentReportDataAsOf(ctx context.Context, quoteSource string) (*time.Time, error) {
	value, err := dal.StockDailyQuotes.QueryLatestTradeDate(ctx, s.db, quoteSource)
	if errors.Is(err, dal.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	value = value.UTC()
	return &value, nil
}

func normalizeDocumentReportListFilter(filter DocumentReportListFilter) DocumentReportListFilter {
	filter.Query = strings.TrimSpace(filter.Query)
	filter.Status = strings.ToUpper(strings.TrimSpace(filter.Status))
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	return filter
}

func documentReportStatus(item DocumentReportListItem) string {
	if item.RecommendationCount == 0 {
		if item.Status != string(domain.DocumentStatusPlanned) {
			return item.Status
		}
		return documentReportStatusEmpty
	}
	if item.ExpectedMetricCount > 0 && item.ReadyMetricCount == item.ExpectedMetricCount {
		return string(evaluation.StatusReady)
	}
	if item.ReadyMetricCount+item.PendingMetricCount+item.IncompleteMetricCount == 0 {
		return documentReportStatusNeedsEvaluation
	}
	return documentReportStatusPartial
}

func reportWindowMetric(window int, metric db_model.RecommendationEventWindowMetric, exists bool, calcVersion string) DocumentReportWindowMetric {
	if !exists {
		return DocumentReportWindowMetric{
			WindowDays:    window,
			Status:        documentMetricStatusNotEvaluated,
			ReasonCode:    "METRIC_NOT_GENERATED",
			ReasonMessage: "该窗口尚未执行评价，可使用按文档补算生成",
		}
	}
	calculatedAt := metric.CalculatedAt.UTC()
	item := DocumentReportWindowMetric{
		WindowDays:              window,
		Status:                  metric.Status,
		ReasonCode:              metric.ReasonCode,
		ReasonMessage:           metric.ReasonMessage,
		EntryDate:               metric.EntryDate,
		EntryPrice:              metric.EntryPrice,
		ExitDate:                metric.ExitDate,
		ExitClosePrice:          metric.ExitClosePrice,
		DirectionReturnRatio:    metric.DirectionReturnRatio,
		MaxFavorableReturnRatio: metric.MaxFavorableReturnRatio,
		MaxAdverseReturnRatio:   metric.MaxAdverseReturnRatio,
		MaxDrawdownRatio:        metric.MaxDrawdownRatio,
		WinFlag:                 metric.WinFlag,
		ExpectedQuoteCount:      int(metric.ExpectedQuoteCount),
		ActualQuoteCount:        int(metric.ActualQuoteCount),
		MissingQuoteCount:       int(metric.MissingQuoteCount),
		CalcVersion:             metric.CalcVersion,
		CalculatedAt:            &calculatedAt,
	}
	if calcVersion != "" && metric.CalcVersion != calcVersion {
		item.Outdated = true
		item.Status = documentMetricStatusOutdated
		item.ReasonCode = "CALC_VERSION_OUTDATED"
		item.ReasonMessage = "评价指标使用旧计算版本，请按文档补算"
	}
	return item
}

func fillRecommendationIdentityFromMetric(item *DocumentReportRecommendation, metric db_model.RecommendationEventWindowMetric) {
	if item.TSCode == "" && metric.TSCode != "" {
		item.TSCode = metric.TSCode
	}
	if item.SecurityName == "" && metric.SecurityName != "" {
		item.SecurityName = metric.SecurityName
	}
	if item.Symbol == "" && metric.Symbol != "" {
		item.Symbol = metric.Symbol
	}
	if item.AssetType == "" && metric.AssetType != "" {
		item.AssetType = metric.AssetType
	}
	if item.Market == "" && metric.Market != "" {
		item.Market = metric.Market
	}
	if item.SectorType == "" && metric.SectorType != "" {
		item.SectorType = metric.SectorType
	}
}

func reportCurrentMetric(item DocumentReportRecommendation, dataAsOf *time.Time, latestQuote db_model.StockDailyQuote, metrics map[int]db_model.RecommendationEventWindowMetric, windows []int, calcVersion string, winThreshold float64) DocumentReportCurrentMetric {
	result := DocumentReportCurrentMetric{}
	var sourceMetric *db_model.RecommendationEventWindowMetric
	for _, window := range windows {
		metric, ok := metrics[window]
		if !ok || metric.EntryDate == nil || metric.EntryPrice == nil {
			continue
		}
		if calcVersion != "" && metric.CalcVersion != calcVersion {
			continue
		}
		copy := metric
		sourceMetric = &copy
		break
	}
	if sourceMetric == nil {
		if !item.RecommendDate.IsZero() && dataAsOf != nil && !item.RecommendDate.Before(*dataAsOf) {
			result.Status = string(evaluation.StatusPending)
			result.ReasonCode = "ENTRY_NOT_AVAILABLE"
			result.ReasonMessage = "推荐后的首个交易日尚未到达"
			return result
		}
		for _, metric := range item.Windows {
			if metric.Status != documentMetricStatusNotEvaluated {
				result.Status = metric.Status
				result.ReasonCode = metric.ReasonCode
				result.ReasonMessage = metric.ReasonMessage
				return result
			}
		}
		result.Status = documentMetricStatusNotEvaluated
		result.ReasonCode = "ENTRY_METRIC_NOT_GENERATED"
		result.ReasonMessage = "尚未生成 T+1 入场数据，请按文档补算"
		return result
	}
	result.EntryDate = sourceMetric.EntryDate
	result.EntryPrice = sourceMetric.EntryPrice
	if dataAsOf == nil {
		result.Status = string(evaluation.StatusIncomplete)
		result.ReasonCode = "QUOTE_DATA_UNAVAILABLE"
		result.ReasonMessage = "本地行情库没有可用行情"
		return result
	}
	if item.TSCode == "" || latestQuote.TSCode == "" {
		result.Status = string(evaluation.StatusIncomplete)
		result.ReasonCode = "LATEST_QUOTE_MISSING"
		result.ReasonMessage = "未找到该标的截至数据日的行情"
		return result
	}
	latestDate := latestQuote.TradeDate.UTC()
	latestClose := latestQuote.ClosePrice
	result.LatestTradeDate = &latestDate
	result.LatestClosePrice = &latestClose
	if latestDate.Format(time.DateOnly) != dataAsOf.UTC().Format(time.DateOnly) {
		result.Status = string(evaluation.StatusIncomplete)
		result.ReasonCode = "LATEST_QUOTE_STALE"
		result.ReasonMessage = "该标的最新行情早于全局数据截止日"
		return result
	}
	entryPrice := *sourceMetric.EntryPrice
	if entryPrice <= 0 || latestClose <= 0 || math.IsNaN(entryPrice) || math.IsNaN(latestClose) || math.IsInf(entryPrice, 0) || math.IsInf(latestClose, 0) {
		result.Status = string(evaluation.StatusFailed)
		result.ReasonCode = "INVALID_PRICE"
		result.ReasonMessage = "入场价或最新收盘价无效"
		return result
	}
	directionReturn := latestClose/entryPrice - 1
	if strings.EqualFold(item.Direction, string(domain.TradeDirectionShort)) {
		directionReturn = entryPrice/latestClose - 1
	} else if !strings.EqualFold(item.Direction, string(domain.TradeDirectionLong)) {
		result.Status = string(evaluation.StatusUnsupported)
		result.ReasonCode = "UNSUPPORTED_DIRECTION"
		result.ReasonMessage = "当前方向不支持收益计算"
		return result
	}
	win := directionReturn > winThreshold
	result.Status = string(evaluation.StatusReady)
	result.DirectionReturnRatio = &directionReturn
	result.WinFlag = &win
	return result
}

func aggregateDocumentReport(recommendations []DocumentReportRecommendation, windows []int) (DocumentReportSummary, []DocumentReportBloggerGroup) {
	overallWindows := make(map[int]*aggregate, len(windows))
	for _, window := range windows {
		overallWindows[window] = &aggregate{}
	}
	overallCurrent := aggregate{}
	grouped := make(map[int64]*documentReportGroupBuilder)
	for _, recommendation := range recommendations {
		group := grouped[recommendation.BloggerID]
		if group == nil {
			group = &documentReportGroupBuilder{
				item: DocumentReportBloggerGroup{
					BloggerID:              recommendation.BloggerID,
					BloggerName:            recommendation.BloggerName,
					Institution:            recommendation.Institution,
					RecommendationEventIDs: []int64{},
				},
				windowData: make(map[int]*aggregate, len(windows)),
			}
			for _, window := range windows {
				group.windowData[window] = &aggregate{}
			}
			grouped[recommendation.BloggerID] = group
		}
		group.item.RecommendationEventIDs = append(group.item.RecommendationEventIDs, recommendation.RecommendationEventID)
		for _, metric := range recommendation.Windows {
			addDocumentWindowMetric(overallWindows[metric.WindowDays], metric)
			addDocumentWindowMetric(group.windowData[metric.WindowDays], metric)
		}
		addDocumentCurrentMetric(&overallCurrent, recommendation.Current)
		addDocumentCurrentMetric(&group.current, recommendation.Current)
	}

	overallWindowSummaries := make([]WindowSummary, 0, len(windows))
	for _, window := range windows {
		overallWindowSummaries = append(overallWindowSummaries, windowSummary(window, *overallWindows[window]))
	}
	groups := make([]DocumentReportBloggerGroup, 0, len(grouped))
	for _, group := range grouped {
		group.item.RecommendationCount = len(group.item.RecommendationEventIDs)
		group.item.Windows = make([]WindowSummary, 0, len(windows))
		for _, window := range windows {
			group.item.Windows = append(group.item.Windows, windowSummary(window, *group.windowData[window]))
		}
		group.item.Current = documentCurrentSummary(group.current)
		groups = append(groups, group.item)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].RecommendationCount != groups[j].RecommendationCount {
			return groups[i].RecommendationCount > groups[j].RecommendationCount
		}
		return groups[i].BloggerID < groups[j].BloggerID
	})
	return DocumentReportSummary{
		RecommendationCount: len(recommendations),
		BloggerCount:        len(groups),
		Windows:             overallWindowSummaries,
		Current:             documentCurrentSummary(overallCurrent),
	}, groups
}

func addDocumentWindowMetric(values *aggregate, metric DocumentReportWindowMetric) {
	if values == nil {
		return
	}
	values.sampleCount++
	switch metric.Status {
	case string(evaluation.StatusReady):
		values.evaluatedCount++
		if metric.WinFlag != nil && *metric.WinFlag {
			values.winCount++
		}
		if metric.DirectionReturnRatio != nil {
			values.returns = append(values.returns, *metric.DirectionReturnRatio)
		}
		if metric.MaxFavorableReturnRatio != nil {
			values.favorableTotal += *metric.MaxFavorableReturnRatio
		}
		if metric.MaxAdverseReturnRatio != nil {
			values.adverseTotal += *metric.MaxAdverseReturnRatio
		}
		if metric.MaxDrawdownRatio != nil {
			values.drawdownTotal += *metric.MaxDrawdownRatio
		}
	case string(evaluation.StatusPending):
		values.pendingCount++
	default:
		values.incompleteCount++
	}
}

func addDocumentCurrentMetric(values *aggregate, metric DocumentReportCurrentMetric) {
	values.sampleCount++
	switch metric.Status {
	case string(evaluation.StatusReady):
		values.evaluatedCount++
		if metric.WinFlag != nil && *metric.WinFlag {
			values.winCount++
		}
		if metric.DirectionReturnRatio != nil {
			values.returns = append(values.returns, *metric.DirectionReturnRatio)
		}
	case string(evaluation.StatusPending):
		values.pendingCount++
	default:
		values.incompleteCount++
	}
}

func documentCurrentSummary(values aggregate) DocumentReportCurrentSummary {
	return DocumentReportCurrentSummary{
		SampleCount:       values.sampleCount,
		EvaluatedCount:    values.evaluatedCount,
		PendingCount:      values.pendingCount,
		IncompleteCount:   values.incompleteCount,
		WinCount:          values.winCount,
		WinRate:           ratio(values.winCount, values.evaluatedCount),
		AvgReturnRatio:    average(values.returns),
		MedianReturnRatio: median(values.returns),
		BestReturnRatio:   maxFloat(values.returns),
		WorstReturnRatio:  minFloat(values.returns),
	}
}

func configuredCalcVersion(runtime *config.Runtime) string {
	if runtime != nil {
		if cfg := runtime.Config(); cfg != nil {
			return strings.TrimSpace(cfg.Evaluation.RecommendationPerformance.CalcVersion)
		}
	}
	return ""
}

func configuredWinThreshold(runtime *config.Runtime) float64 {
	if runtime != nil {
		if cfg := runtime.Config(); cfg != nil {
			return cfg.Evaluation.RecommendationPerformance.WinThresholdRatio
		}
	}
	return 0
}

func configuredReportMethodology(runtime *config.Runtime) (string, string, float64) {
	if runtime != nil {
		if cfg := runtime.Config(); cfg != nil {
			performance := cfg.Evaluation.RecommendationPerformance
			return performance.EntryPriceRule, performance.BasePriceRule, performance.MinQuoteCoverageRatio
		}
	}
	return "", "", 0
}
