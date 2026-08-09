package stats

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/dal"
	"finance-sys/internal/evaluation"

	"gorm.io/gorm"
)

type Service struct {
	db      *gorm.DB
	runtime *config.Runtime
}

type metricRow struct {
	RecommendationEventID   int64
	BloggerID               int64
	BloggerName             string
	Institution             string
	SecurityMasterID        int64
	TSCode                  string
	Symbol                  string
	SecurityName            string
	AssetType               string
	Market                  string
	Industry                string
	SectorType              string
	Direction               string
	RecommendDate           time.Time
	WindowDays              int
	Status                  string
	DirectionReturnRatio    *float64
	MaxFavorableReturnRatio *float64
	MaxAdverseReturnRatio   *float64
	MaxDrawdownRatio        *float64
	WinFlag                 *bool
}

type recommendationLedgerMetricRow struct {
	RecommendationEventID int64
	TSCode                string
	Symbol                string
	SecurityName          string
	AssetType             string
	Market                string
	SectorType            string
	WindowDays            int
	Status                string
	ReasonCode            string
	ReasonMessage         string
	RawReturnRatio        *float64
}

type aggregate struct {
	sampleCount     int
	evaluatedCount  int
	pendingCount    int
	incompleteCount int
	winCount        int
	returns         []float64
	favorableTotal  float64
	adverseTotal    float64
	drawdownTotal   float64
}

func NewService(db *gorm.DB, runtime *config.Runtime) *Service {
	return &Service{db: db, runtime: runtime}
}

func (s *Service) BloggerRankings(ctx context.Context, filter Filter) (*BloggerRankingResponse, error) {
	filter = s.normalizeFilter(filter)
	rows, err := s.metricRows(ctx, filter)
	if err != nil {
		return nil, err
	}
	type bloggerAggregate struct {
		name        string
		institution string
		values      aggregate
	}
	grouped := make(map[int64]*bloggerAggregate)
	overall := aggregate{}
	for _, row := range rows {
		entry := grouped[row.BloggerID]
		if entry == nil {
			entry = &bloggerAggregate{name: row.BloggerName, institution: row.Institution}
			grouped[row.BloggerID] = entry
		}
		addMetric(&entry.values, row)
		addMetric(&overall, row)
	}
	items := make([]BloggerRankingItem, 0, len(grouped))
	for bloggerID, entry := range grouped {
		if entry.values.evaluatedCount < filter.MinSampleCount {
			continue
		}
		items = append(items, bloggerRankingItem(bloggerID, entry.name, entry.institution, entry.values))
	}
	sortBloggerRankings(items, filter.Sort)
	items = applyRankingLimit(items, filter.Offset, filter.Limit)
	for index := range items {
		items[index].Rank = filter.Offset + index + 1
	}
	return &BloggerRankingResponse{
		WindowDays: filter.WindowDays,
		DateFrom:   formatOptionalDate(filter.DateFrom),
		DateTo:     formatOptionalDate(filter.DateTo),
		Overview: Overview{
			TotalBloggers:             len(grouped),
			EvaluatedRecommendations:  overall.evaluatedCount,
			PendingRecommendations:    overall.pendingCount,
			IncompleteRecommendations: overall.incompleteCount,
			AverageWinRate:            ratio(overall.winCount, overall.evaluatedCount),
			AverageReturnRatio:        average(overall.returns),
		},
		Items: items,
	}, nil
}

func (s *Service) BloggerSummary(ctx context.Context, bloggerID int64, filter Filter) (*BloggerSummaryResponse, error) {
	blogger, err := dal.Bloggers.QueryByID(ctx, s.db, bloggerID)
	if err != nil {
		return nil, err
	}
	filter.BloggerID = bloggerID
	filter.WindowDays = 0
	filter.MinSampleCount = 0
	rows, err := s.metricRows(ctx, filter)
	if err != nil {
		return nil, err
	}
	grouped := make(map[int]*aggregate)
	for _, row := range rows {
		entry := grouped[row.WindowDays]
		if entry == nil {
			entry = &aggregate{}
			grouped[row.WindowDays] = entry
		}
		addMetric(entry, row)
	}
	windows := configuredWindows(s.runtime)
	items := make([]WindowSummary, 0, len(windows))
	for _, window := range windows {
		values := aggregate{}
		if existing := grouped[window]; existing != nil {
			values = *existing
		}
		items = append(items, windowSummary(window, values))
	}
	return &BloggerSummaryResponse{
		BloggerID:   blogger.ID,
		BloggerName: blogger.Name,
		Institution: blogger.Institution,
		Windows:     items,
	}, nil
}

func (s *Service) BloggerTimeseries(ctx context.Context, bloggerID int64, filter Filter) (*BloggerTimeseriesResponse, error) {
	if _, err := dal.Bloggers.QueryByID(ctx, s.db, bloggerID); err != nil {
		return nil, err
	}
	filter = s.normalizeFilter(filter)
	filter.BloggerID = bloggerID
	rows, err := s.metricRows(ctx, filter)
	if err != nil {
		return nil, err
	}
	grouped := make(map[string]*aggregate)
	for _, row := range rows {
		period := row.RecommendDate.Format("2006-01")
		entry := grouped[period]
		if entry == nil {
			entry = &aggregate{}
			grouped[period] = entry
		}
		addMetric(entry, row)
	}
	periods := make([]string, 0, len(grouped))
	for period := range grouped {
		periods = append(periods, period)
	}
	sort.Strings(periods)
	items := make([]TimeseriesPoint, 0, len(periods))
	for _, period := range periods {
		values := grouped[period]
		items = append(items, TimeseriesPoint{
			Period:         period,
			WindowDays:     filter.WindowDays,
			SampleCount:    values.sampleCount,
			EvaluatedCount: values.evaluatedCount,
			PendingCount:   values.pendingCount,
			WinCount:       values.winCount,
			WinRate:        ratio(values.winCount, values.evaluatedCount),
			AvgReturnRatio: average(values.returns),
		})
	}
	return &BloggerTimeseriesResponse{BloggerID: bloggerID, WindowDays: filter.WindowDays, Items: items}, nil
}

func (s *Service) RecommendationPerformanceList(ctx context.Context, filter Filter) (*RecommendationPerformanceList, error) {
	filter = s.normalizeFilter(filter)
	tx := s.db.WithContext(ctx).
		Table("recommendation_event_window_metrics AS m").
		Joins("JOIN recommendation_events AS re ON re.id = m.recommendation_event_id").
		Joins("JOIN bloggers AS b ON b.id = m.blogger_id")
	tx = applyMetricFilters(tx, filter)
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, err
	}
	var items []RecommendationPerformanceItem
	selectSQL := `
		m.recommendation_event_id, m.blogger_id, b.name AS blogger_name, b.institution,
			re.source_document_id, m.ts_code, m.symbol, m.security_name, m.asset_type, m.market, m.industry, m.sector_type,
		m.direction, m.recommend_date, re.thesis, m.window_days, m.status, m.reason_code, m.reason_message,
		m.entry_date, m.entry_price, m.exit_date, m.exit_close_price, m.direction_return_ratio,
		m.max_favorable_return_ratio, m.max_adverse_return_ratio, m.max_drawdown_ratio, m.win_flag`
	query := tx.Select(selectSQL).
		Order("m.recommend_date DESC, m.recommendation_event_id DESC").
		Limit(filter.Limit).
		Offset(filter.Offset)
	if err := query.Scan(&items).Error; err != nil {
		return nil, err
	}
	return &RecommendationPerformanceList{Total: total, WindowDays: filter.WindowDays, Items: items}, nil
}

// RecommendationLedger returns one row per recommendation event and attaches
// every configured evaluation window to that row. The event table is the
// pagination source so a missing or not-yet-created metric never removes the
// underlying recommendation from the ledger.
func (s *Service) RecommendationLedger(ctx context.Context, filter Filter) (*RecommendationLedgerList, error) {
	filter.AssetType = normalizeAssetTypeFilter(filter.AssetType)
	filter = normalizePagination(filter)
	quoteSource := configuredQuoteSource(s.runtime)
	base := s.db.WithContext(ctx).
		Table("recommendation_events AS re").
		Joins("JOIN bloggers AS b ON b.id = re.blogger_id").
		Joins("LEFT JOIN security_master AS sm ON sm.ts_code = CASE WHEN re.symbol LIKE '%.%' THEN re.symbol ELSE CONCAT(re.symbol, '.', re.market) END")
	base = applyRecommendationLedgerFilters(base, filter, quoteSource)

	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, err
	}

	items := make([]RecommendationLedgerItem, 0, filter.Limit)
	selectSQL := `
		re.id AS recommendation_event_id, re.blogger_id, b.name AS blogger_name, b.institution,
		COALESCE(sm.ts_code, '') AS ts_code, re.symbol, COALESCE(sm.name, '') AS security_name,
			re.asset_type, re.market, COALESCE(sm.sector_type, '') AS sector_type, re.direction, re.recommend_date, re.thesis`
	if err := base.Select(selectSQL).
		Order("re.recommend_date DESC, re.id DESC").
		Limit(filter.Limit).
		Offset(filter.Offset).
		Scan(&items).Error; err != nil {
		return nil, err
	}

	windows := configuredWindows(s.runtime)
	if len(items) > 0 {
		eventIDs := make([]int64, 0, len(items))
		for _, item := range items {
			eventIDs = append(eventIDs, item.RecommendationEventID)
		}
		var metrics []recommendationLedgerMetricRow
		err := s.db.WithContext(ctx).
			Table("recommendation_event_window_metrics AS m").
			Select(`m.recommendation_event_id, m.ts_code, m.symbol, m.security_name, m.asset_type, m.market, m.sector_type,
				m.window_days, m.status, m.reason_code, m.reason_message, m.raw_return_ratio`).
			Where("m.recommendation_event_id IN ?", eventIDs).
			Where("m.window_days IN ?", windows).
			Where("m.quote_source = ?", quoteSource).
			Order("m.recommendation_event_id ASC, m.window_days ASC").
			Scan(&metrics).Error
		if err != nil {
			return nil, err
		}
		mergeRecommendationLedgerMetrics(items, metrics, windows)
	}

	page, totalPages := paginationMetadata(total, filter.Offset, filter.Limit)
	return &RecommendationLedgerList{
		Total:      total,
		Page:       page,
		PageSize:   filter.Limit,
		TotalPages: totalPages,
		Items:      items,
	}, nil
}

func (s *Service) RecommendationDetail(ctx context.Context, eventID int64) (*RecommendationDetail, error) {
	var item RecommendationContext
	err := s.db.WithContext(ctx).
		Table("recommendation_events AS re").
		Select(`re.id AS recommendation_event_id, re.blogger_id, b.name AS blogger_name, b.institution,
			re.source_document_id, COALESCE(d.title, '') AS document_title, COALESCE(d.file_name, '') AS document_file_name,
			re.symbol, re.asset_type, re.market, COALESCE(sm.sector_type, '') AS sector_type, re.direction, re.recommend_date, re.reference_price,
			re.confidence, re.status AS recommendation_status, re.thesis`).
		Joins("JOIN bloggers AS b ON b.id = re.blogger_id").
		Joins("LEFT JOIN documents AS d ON d.id = re.source_document_id").
		Joins("LEFT JOIN security_master AS sm ON sm.ts_code = CASE WHEN re.symbol LIKE '%.%' THEN re.symbol ELSE CONCAT(re.symbol, '.', re.market) END").
		Where("re.id = ?", eventID).
		Take(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, dal.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	evidences, err := dal.RecommendationEventEvidences.QueryByEventID(ctx, s.db, eventID)
	if err != nil {
		return nil, err
	}
	item.Evidence = make([]Evidence, 0, len(evidences))
	for _, evidence := range evidences {
		item.Evidence = append(item.Evidence, Evidence{ChunkIndex: int(evidence.ChunkIndex), Text: evidence.EvidenceText})
	}
	metrics, err := dal.RecommendationEventWindowMetrics.QueryByEventID(ctx, s.db, eventID)
	if err != nil {
		return nil, err
	}
	return &RecommendationDetail{Recommendation: item, Metrics: metrics}, nil
}

func (s *Service) PriceSeries(ctx context.Context, eventID int64, daysBefore int, daysAfter int) (*PriceSeriesResponse, error) {
	detail, err := s.RecommendationDetail(ctx, eventID)
	if err != nil {
		return nil, err
	}
	if daysBefore < 0 {
		daysBefore = 0
	}
	if daysBefore > 60 {
		daysBefore = 60
	}
	if daysAfter <= 0 {
		daysAfter = 90
	}
	if daysAfter > 250 {
		daysAfter = 250
	}
	metrics := detail.Metrics
	tsCode := strings.ToUpper(strings.TrimSpace(detail.Recommendation.Symbol))
	securityName := ""
	for _, metric := range metrics {
		if metric.TSCode != "" {
			tsCode = metric.TSCode
			securityName = metric.SecurityName
			break
		}
	}
	if !strings.Contains(tsCode, ".") {
		market := strings.ToUpper(strings.TrimSpace(detail.Recommendation.Market))
		if market != "" {
			tsCode += "." + market
		}
	}
	quoteSource := "TUSHARE"
	if cfg := s.runtime.Config(); cfg != nil && cfg.Evaluation.RecommendationPerformance.QuoteSource != "" {
		quoteSource = cfg.Evaluation.RecommendationPerformance.QuoteSource
	}
	quotes, err := dal.StockDailyQuotes.QueryByTSCodeRange(ctx, s.db, tsCode, quoteSource, time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC), time.Time{})
	if err != nil {
		return nil, err
	}
	anchor := 0
	for index, quote := range quotes {
		anchor = index
		if !quote.TradeDate.Before(detail.Recommendation.RecommendDate) {
			break
		}
	}
	start := anchor - daysBefore
	if start < 0 {
		start = 0
	}
	end := anchor + daysAfter + 1
	if end > len(quotes) {
		end = len(quotes)
	}
	points := make([]PricePoint, 0, maxInt(end-start, 0))
	if start < end {
		for _, quote := range quotes[start:end] {
			points = append(points, PricePoint{
				TradeDate:  quote.TradeDate,
				OpenPrice:  quote.OpenPrice,
				HighPrice:  quote.HighPrice,
				LowPrice:   quote.LowPrice,
				ClosePrice: quote.ClosePrice,
				Volume:     quote.Volume,
				PctChg:     quote.PctChg,
			})
		}
	}
	recommendDate := detail.Recommendation.RecommendDate
	markers := []PriceMarker{{Type: "recommend", Label: "推荐日", TradeDate: &recommendDate}}
	for _, metric := range metrics {
		if metric.EntryDate != nil {
			markers = append(markers, PriceMarker{Type: "entry", Label: "T+1 入场", TradeDate: metric.EntryDate, WindowDays: int(metric.WindowDays)})
		}
		if metric.ExitDate != nil {
			markers = append(markers, PriceMarker{Type: "exit", Label: fmt.Sprintf("%d 日退出", metric.WindowDays), TradeDate: metric.ExitDate, WindowDays: int(metric.WindowDays)})
		}
		if metric.BestTradeDate != nil {
			markers = append(markers, PriceMarker{Type: "best", Label: fmt.Sprintf("%d 日最佳点", metric.WindowDays), TradeDate: metric.BestTradeDate, WindowDays: int(metric.WindowDays)})
		}
		if metric.WorstTradeDate != nil {
			markers = append(markers, PriceMarker{Type: "worst", Label: fmt.Sprintf("%d 日最不利点", metric.WindowDays), TradeDate: metric.WorstTradeDate, WindowDays: int(metric.WindowDays)})
		}
	}
	return &PriceSeriesResponse{
		RecommendationEventID: eventID,
		TSCode:                tsCode,
		SecurityName:          securityName,
		RecommendDate:         recommendDate,
		Items:                 points,
		Markers:               markers,
	}, nil
}

func (s *Service) SecurityRankings(ctx context.Context, filter Filter) (*SecurityRankingResponse, error) {
	filter = s.normalizeFilter(filter)
	rows, err := s.metricRows(ctx, filter)
	if err != nil {
		return nil, err
	}
	type securityAggregate struct {
		securityMasterID int64
		symbol           string
		name             string
		assetType        string
		market           string
		industry         string
		sectorType       string
		bloggers         map[int64]struct{}
		values           aggregate
	}
	grouped := make(map[string]*securityAggregate)
	for _, row := range rows {
		key := row.TSCode
		if key == "" {
			key = row.Symbol
		}
		entry := grouped[key]
		if entry == nil {
			entry = &securityAggregate{
				securityMasterID: row.SecurityMasterID,
				symbol:           row.Symbol,
				name:             row.SecurityName,
				assetType:        row.AssetType,
				market:           row.Market,
				industry:         row.Industry,
				sectorType:       row.SectorType,
				bloggers:         make(map[int64]struct{}),
			}
			grouped[key] = entry
		}
		entry.bloggers[row.BloggerID] = struct{}{}
		addMetric(&entry.values, row)
	}
	items := make([]SecurityRankingItem, 0, len(grouped))
	for tsCode, entry := range grouped {
		if entry.values.evaluatedCount < filter.MinSampleCount {
			continue
		}
		items = append(items, SecurityRankingItem{
			SecurityMasterID:    entry.securityMasterID,
			TSCode:              tsCode,
			Symbol:              entry.symbol,
			SecurityName:        entry.name,
			AssetType:           entry.assetType,
			Market:              entry.market,
			Industry:            entry.industry,
			SectorType:          entry.sectorType,
			RecommendationCount: entry.values.sampleCount,
			BloggerCount:        len(entry.bloggers),
			EvaluatedCount:      entry.values.evaluatedCount,
			PendingCount:        entry.values.pendingCount,
			WinCount:            entry.values.winCount,
			WinRate:             ratio(entry.values.winCount, entry.values.evaluatedCount),
			AvgReturnRatio:      average(entry.values.returns),
			MedianReturnRatio:   median(entry.values.returns),
			BestReturnRatio:     maxFloat(entry.values.returns),
			WorstReturnRatio:    minFloat(entry.values.returns),
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		switch filter.Sort {
		case "avg_return":
			if items[i].AvgReturnRatio != items[j].AvgReturnRatio {
				return items[i].AvgReturnRatio > items[j].AvgReturnRatio
			}
		case "sample_count":
			if items[i].EvaluatedCount != items[j].EvaluatedCount {
				return items[i].EvaluatedCount > items[j].EvaluatedCount
			}
		default:
			if items[i].WinRate != items[j].WinRate {
				return items[i].WinRate > items[j].WinRate
			}
		}
		return items[i].EvaluatedCount > items[j].EvaluatedCount
	})
	items = applySecurityRankingLimit(items, filter.Offset, filter.Limit)
	for index := range items {
		items[index].Rank = filter.Offset + index + 1
	}
	return &SecurityRankingResponse{WindowDays: filter.WindowDays, Items: items}, nil
}

func (s *Service) SecuritySummary(ctx context.Context, tsCode string, filter Filter) (*SecurityRankingItem, error) {
	filter.TSCode = strings.ToUpper(strings.TrimSpace(tsCode))
	filter.MinSampleCount = -1
	filter.Limit = 1
	response, err := s.SecurityRankings(ctx, filter)
	if err != nil {
		return nil, err
	}
	if len(response.Items) == 0 {
		return nil, dal.ErrNotFound
	}
	return &response.Items[0], nil
}

func (s *Service) metricRows(ctx context.Context, filter Filter) ([]metricRow, error) {
	var rows []metricRow
	tx := s.db.WithContext(ctx).
		Table("recommendation_event_window_metrics AS m").
		Select(`m.recommendation_event_id, m.blogger_id, b.name AS blogger_name, b.institution,
			m.security_master_id, m.ts_code, m.symbol, m.security_name, m.asset_type, m.market, m.industry, m.sector_type,
			m.direction, m.recommend_date, m.window_days, m.status, m.direction_return_ratio,
			m.max_favorable_return_ratio, m.max_adverse_return_ratio, m.max_drawdown_ratio, m.win_flag`).
		Joins("JOIN bloggers AS b ON b.id = m.blogger_id")
	tx = applyMetricFilters(tx, filter)
	if err := tx.Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func applyMetricFilters(tx *gorm.DB, filter Filter) *gorm.DB {
	if filter.WindowDays > 0 {
		tx = tx.Where("m.window_days = ?", filter.WindowDays)
	}
	if filter.DateFrom != nil {
		tx = tx.Where("m.recommend_date >= ?", *filter.DateFrom)
	}
	if filter.DateTo != nil {
		tx = tx.Where("m.recommend_date <= ?", *filter.DateTo)
	}
	if filter.Market != "" {
		tx = tx.Where("m.market = ?", strings.ToUpper(filter.Market))
	}
	if filter.AssetType != "" {
		tx = tx.Where("m.asset_type = ?", strings.ToUpper(filter.AssetType))
	}
	if filter.Direction != "" {
		tx = tx.Where("m.direction = ?", strings.ToUpper(filter.Direction))
	}
	if filter.Status != "" {
		tx = tx.Where("m.status = ?", strings.ToUpper(filter.Status))
	}
	if filter.BloggerID > 0 {
		tx = tx.Where("m.blogger_id = ?", filter.BloggerID)
	}
	if filter.TSCode != "" {
		tx = tx.Where("m.ts_code = ?", strings.ToUpper(filter.TSCode))
	}
	if filter.Symbol != "" {
		value := strings.ToUpper(filter.Symbol)
		tx = tx.Where("m.symbol = ? OR m.ts_code = ?", value, value)
	}
	return tx
}

func applyRecommendationLedgerFilters(tx *gorm.DB, filter Filter, quoteSource string) *gorm.DB {
	if filter.DateFrom != nil {
		tx = tx.Where("re.recommend_date >= ?", *filter.DateFrom)
	}
	if filter.DateTo != nil {
		tx = tx.Where("re.recommend_date <= ?", *filter.DateTo)
	}
	if filter.Market != "" {
		tx = tx.Where("re.market = ?", strings.ToUpper(filter.Market))
	}
	if filter.AssetType != "" {
		tx = tx.Where("re.asset_type = ?", strings.ToUpper(filter.AssetType))
	}
	if filter.Direction != "" {
		tx = tx.Where("re.direction = ?", strings.ToUpper(filter.Direction))
	}
	if filter.BloggerID > 0 {
		tx = tx.Where("re.blogger_id = ?", filter.BloggerID)
	}
	if filter.BloggerName != "" {
		search := "%" + filter.BloggerName + "%"
		tx = tx.Where("b.name LIKE ? OR b.normalized_name LIKE ?", search, search)
	}
	if filter.Status != "" {
		tx = tx.Where(`EXISTS (
			SELECT 1 FROM recommendation_event_window_metrics AS fm
			WHERE fm.recommendation_event_id = re.id AND fm.quote_source = ? AND fm.status = ?
		)`, quoteSource, strings.ToUpper(filter.Status))
	}
	if filter.TSCode != "" {
		tx = tx.Where(`EXISTS (
			SELECT 1 FROM recommendation_event_window_metrics AS fm
			WHERE fm.recommendation_event_id = re.id AND fm.quote_source = ? AND fm.ts_code = ?
		)`, quoteSource, strings.ToUpper(filter.TSCode))
	}
	if filter.Symbol != "" {
		value := strings.ToUpper(filter.Symbol)
		tx = tx.Where(`re.symbol = ? OR EXISTS (
			SELECT 1 FROM recommendation_event_window_metrics AS fm
			WHERE fm.recommendation_event_id = re.id AND fm.quote_source = ?
				AND (fm.symbol = ? OR fm.ts_code = ?)
		)`, value, quoteSource, value, value)
	}
	return tx
}

func (s *Service) normalizeFilter(filter Filter) Filter {
	cfg := s.runtime.Config()
	filter.AssetType = normalizeAssetTypeFilter(filter.AssetType)
	if filter.WindowDays <= 0 {
		filter.WindowDays = 30
		if cfg != nil && cfg.Evaluation.RecommendationPerformance.Ranking.DefaultWindowDays > 0 {
			filter.WindowDays = cfg.Evaluation.RecommendationPerformance.Ranking.DefaultWindowDays
		}
	}
	if filter.MinSampleCount < 0 {
		filter.MinSampleCount = 0
	} else if filter.MinSampleCount == 0 && cfg != nil {
		filter.MinSampleCount = cfg.Evaluation.RecommendationPerformance.Ranking.DefaultMinSampleCount
	}
	if filter.Sort == "" {
		filter.Sort = "win_rate"
		if cfg != nil && cfg.Evaluation.RecommendationPerformance.Ranking.DefaultSort != "" {
			filter.Sort = cfg.Evaluation.RecommendationPerformance.Ranking.DefaultSort
		}
	}
	return normalizePagination(filter)
}

func normalizeAssetTypeFilter(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "STOCK", "ASHARE", "A_SHARE":
		return "A_SHARE"
	case "ETF":
		return "ETF"
	case "SECTOR":
		return "SECTOR"
	default:
		return strings.ToUpper(strings.TrimSpace(value))
	}
}

func normalizePagination(filter Filter) Filter {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Limit > 200 {
		filter.Limit = 200
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	return filter
}

func addMetric(values *aggregate, row metricRow) {
	values.sampleCount++
	switch evaluation.Status(row.Status) {
	case evaluation.StatusReady:
		values.evaluatedCount++
		if row.WinFlag != nil && *row.WinFlag {
			values.winCount++
		}
		if row.DirectionReturnRatio != nil {
			values.returns = append(values.returns, *row.DirectionReturnRatio)
		}
		if row.MaxFavorableReturnRatio != nil {
			values.favorableTotal += *row.MaxFavorableReturnRatio
		}
		if row.MaxAdverseReturnRatio != nil {
			values.adverseTotal += *row.MaxAdverseReturnRatio
		}
		if row.MaxDrawdownRatio != nil {
			values.drawdownTotal += *row.MaxDrawdownRatio
		}
	case evaluation.StatusPending:
		values.pendingCount++
	default:
		values.incompleteCount++
	}
}

func bloggerRankingItem(bloggerID int64, name string, institution string, values aggregate) BloggerRankingItem {
	return BloggerRankingItem{
		BloggerID:                  bloggerID,
		BloggerName:                name,
		Institution:                institution,
		SampleCount:                values.sampleCount,
		EvaluatedCount:             values.evaluatedCount,
		PendingCount:               values.pendingCount,
		IncompleteCount:            values.incompleteCount,
		WinCount:                   values.winCount,
		WinRate:                    ratio(values.winCount, values.evaluatedCount),
		AvgReturnRatio:             average(values.returns),
		MedianReturnRatio:          median(values.returns),
		BestReturnRatio:            maxFloat(values.returns),
		WorstReturnRatio:           minFloat(values.returns),
		AvgMaxFavorableReturnRatio: safeDivide(values.favorableTotal, values.evaluatedCount),
		AvgMaxAdverseReturnRatio:   safeDivide(values.adverseTotal, values.evaluatedCount),
		AvgMaxDrawdownRatio:        safeDivide(values.drawdownTotal, values.evaluatedCount),
		PerformanceScore:           performanceScore(values),
	}
}

func windowSummary(window int, values aggregate) WindowSummary {
	return WindowSummary{
		WindowDays:                 window,
		SampleCount:                values.sampleCount,
		EvaluatedCount:             values.evaluatedCount,
		PendingCount:               values.pendingCount,
		IncompleteCount:            values.incompleteCount,
		WinCount:                   values.winCount,
		WinRate:                    ratio(values.winCount, values.evaluatedCount),
		AvgReturnRatio:             average(values.returns),
		MedianReturnRatio:          median(values.returns),
		BestReturnRatio:            maxFloat(values.returns),
		WorstReturnRatio:           minFloat(values.returns),
		AvgMaxFavorableReturnRatio: safeDivide(values.favorableTotal, values.evaluatedCount),
		AvgMaxAdverseReturnRatio:   safeDivide(values.adverseTotal, values.evaluatedCount),
		AvgMaxDrawdownRatio:        safeDivide(values.drawdownTotal, values.evaluatedCount),
	}
}

// The score is intentionally deterministic and transparent: 60% win rate,
// 30% average directional return normalized to [-10%, +10%], and 10% sample
// reliability saturated at 20 evaluated recommendations.
func performanceScore(values aggregate) float64 {
	winComponent := clamp(ratio(values.winCount, values.evaluatedCount), 0, 1) * 60
	returnComponent := clamp((average(values.returns)+0.1)/0.2, 0, 1) * 30
	reliabilityComponent := clamp(float64(values.evaluatedCount)/20, 0, 1) * 10
	return round(winComponent+returnComponent+reliabilityComponent, 4)
}

func sortBloggerRankings(items []BloggerRankingItem, field string) {
	sort.SliceStable(items, func(i, j int) bool {
		switch field {
		case "avg_return":
			if items[i].AvgReturnRatio != items[j].AvgReturnRatio {
				return items[i].AvgReturnRatio > items[j].AvgReturnRatio
			}
		case "sample_count":
			if items[i].EvaluatedCount != items[j].EvaluatedCount {
				return items[i].EvaluatedCount > items[j].EvaluatedCount
			}
		case "performance_score":
			if items[i].PerformanceScore != items[j].PerformanceScore {
				return items[i].PerformanceScore > items[j].PerformanceScore
			}
		default:
			if items[i].WinRate != items[j].WinRate {
				return items[i].WinRate > items[j].WinRate
			}
		}
		if items[i].EvaluatedCount != items[j].EvaluatedCount {
			return items[i].EvaluatedCount > items[j].EvaluatedCount
		}
		return items[i].BloggerID < items[j].BloggerID
	})
}

func configuredWindows(runtime *config.Runtime) []int {
	if runtime != nil {
		if cfg := runtime.Config(); cfg != nil && len(cfg.Evaluation.RecommendationPerformance.Windows) > 0 {
			result := append([]int(nil), cfg.Evaluation.RecommendationPerformance.Windows...)
			sort.Ints(result)
			return result
		}
	}
	return []int{5, 10, 30, 90}
}

func configuredQuoteSource(runtime *config.Runtime) string {
	if runtime != nil {
		if cfg := runtime.Config(); cfg != nil {
			if value := strings.TrimSpace(cfg.Evaluation.RecommendationPerformance.QuoteSource); value != "" {
				return strings.ToUpper(value)
			}
		}
	}
	return "TUSHARE"
}

func mergeRecommendationLedgerMetrics(items []RecommendationLedgerItem, metrics []recommendationLedgerMetricRow, windows []int) {
	metricsByEvent := make(map[int64]map[int]recommendationLedgerMetricRow, len(items))
	for _, metric := range metrics {
		byWindow := metricsByEvent[metric.RecommendationEventID]
		if byWindow == nil {
			byWindow = make(map[int]recommendationLedgerMetricRow, len(windows))
			metricsByEvent[metric.RecommendationEventID] = byWindow
		}
		byWindow[metric.WindowDays] = metric
	}

	for index := range items {
		item := &items[index]
		byWindow := metricsByEvent[item.RecommendationEventID]
		item.Windows = make([]RecommendationWindowReturn, 0, len(windows))
		for _, window := range windows {
			metric, ok := byWindow[window]
			if !ok {
				item.Windows = append(item.Windows, RecommendationWindowReturn{WindowDays: window})
				continue
			}
			if item.TSCode == "" && metric.TSCode != "" {
				item.TSCode = metric.TSCode
			}
			if item.SecurityName == "" && metric.SecurityName != "" {
				item.SecurityName = metric.SecurityName
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
			if item.Symbol == "" && metric.Symbol != "" {
				item.Symbol = metric.Symbol
			}
			item.Windows = append(item.Windows, RecommendationWindowReturn{
				WindowDays:    window,
				Status:        metric.Status,
				ReasonCode:    metric.ReasonCode,
				ReasonMessage: metric.ReasonMessage,
				ReturnRatio:   metric.RawReturnRatio,
			})
		}
	}
}

func paginationMetadata(total int64, offset int, limit int) (int, int) {
	if limit <= 0 {
		limit = 50
	}
	totalPages := 0
	if total > 0 {
		totalPages = int((total + int64(limit) - 1) / int64(limit))
	}
	return offset/limit + 1, totalPages
}

func applyRankingLimit(items []BloggerRankingItem, offset int, limit int) []BloggerRankingItem {
	if offset >= len(items) {
		return []BloggerRankingItem{}
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}

func applySecurityRankingLimit(items []SecurityRankingItem, offset int, limit int) []SecurityRankingItem {
	if offset >= len(items) {
		return []SecurityRankingItem{}
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}

func ratio(numerator int, denominator int) float64 {
	return safeDivide(float64(numerator), denominator)
}

func safeDivide(numerator float64, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return round(numerator/float64(denominator), 8)
}

func average(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	total := 0.0
	for _, value := range values {
		total += value
	}
	return round(total/float64(len(values)), 8)
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	copyValues := append([]float64(nil), values...)
	sort.Float64s(copyValues)
	middle := len(copyValues) / 2
	if len(copyValues)%2 == 1 {
		return round(copyValues[middle], 8)
	}
	return round((copyValues[middle-1]+copyValues[middle])/2, 8)
}

func maxFloat(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	result := values[0]
	for _, value := range values[1:] {
		if value > result {
			result = value
		}
	}
	return result
}

func minFloat(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	result := values[0]
	for _, value := range values[1:] {
		if value < result {
			result = value
		}
	}
	return result
}

func clamp(value, lower, upper float64) float64 {
	return math.Max(lower, math.Min(upper, value))
}

func round(value float64, precision int) float64 {
	power := math.Pow10(precision)
	return math.Round(value*power) / power
}

func formatOptionalDate(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Format(time.DateOnly)
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
