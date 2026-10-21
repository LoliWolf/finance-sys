package stats

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"finance-sys/internal/config"
	"gorm.io/gorm"
)

// Synchronous print reports deliberately have a bounded size, not a silent truncation.
const MaxRecommendationExportItems = 2000

type RecommendationExportLimitError struct {
	Total int64
	Limit int
}

func (e *RecommendationExportLimitError) Error() string {
	return fmt.Sprintf("当前筛选共 %d 条，单次最多导出 %d 条；请缩小博主或日期范围", e.Total, e.Limit)
}

type RecommendationExportWindow struct {
	WindowDays       int      `json:"window_days"`
	SampleCount      int      `json:"sample_count"`
	EvaluatedCount   int      `json:"evaluated_count"`
	PendingCount     int      `json:"pending_count"`
	UnavailableCount int      `json:"unavailable_count"`
	RiseCount        int      `json:"rise_count"`
	RiseRate         *float64 `json:"rise_rate"`
	AvgReturnRatio   *float64 `json:"avg_return_ratio"`
}

type RecommendationExportBlogger struct {
	BloggerID           int64                        `json:"blogger_id"`
	BloggerName         string                       `json:"blogger_name"`
	Institution         string                       `json:"institution"`
	RecommendationCount int                          `json:"recommendation_count"`
	Windows             []RecommendationExportWindow `json:"windows"`
}

type RecommendationExportReport struct {
	GeneratedAt   time.Time                     `json:"generated_at"`
	DataAsOf      *time.Time                    `json:"data_as_of"`
	QuoteSource   string                        `json:"quote_source"`
	CalcVersion   string                        `json:"calc_version"`
	Filters       map[string]string             `json:"filters"`
	Total         int64                         `json:"total"`
	MaxItems      int                           `json:"max_items"`
	BloggerCount  int                           `json:"blogger_count"`
	SecurityCount int                           `json:"security_count"`
	Windows       []int                         `json:"windows"`
	Summary       []RecommendationExportWindow  `json:"summary"`
	Bloggers      []RecommendationExportBlogger `json:"bloggers"`
	Items         []RecommendationLedgerItem    `json:"items"`
}

func (s *Service) RecommendationExport(ctx context.Context, filter Filter) (*RecommendationExportReport, error) {
	filter.BloggerName = strings.TrimSpace(filter.BloggerName)
	filter.Symbol = strings.ToUpper(strings.TrimSpace(filter.Symbol))
	filter.TSCode = strings.ToUpper(strings.TrimSpace(filter.TSCode))
	filter.Market = strings.ToUpper(strings.TrimSpace(filter.Market))
	filter.Direction = strings.ToUpper(strings.TrimSpace(filter.Direction))
	filter.Status = strings.ToUpper(strings.TrimSpace(filter.Status))
	filter.AssetType = normalizeAssetTypeFilter(filter.AssetType)
	var frozen *config.Runtime
	if s.runtime != nil {
		frozen = config.NewRuntime(s.runtime.Current())
	}
	options := &sql.TxOptions{ReadOnly: true}
	if s.db.Dialector.Name() == "mysql" {
		options.Isolation = sql.LevelRepeatableRead
	}
	var report *RecommendationExportReport
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		reader := NewService(tx, frozen)
		// Reuse exactly the ledger's filtering, ordering and raw-return mapping.
		ledger, err := reader.recommendationLedger(ctx, filter, MaxRecommendationExportItems)
		if err != nil {
			return err
		}
		asOf, err := reader.documentReportDataAsOf(ctx, configuredQuoteSource(frozen))
		if err != nil {
			return err
		}
		windows := configuredWindows(frozen)
		summary, bloggers, securityCount := summarizeRecommendationExport(ledger.Items, windows)
		report = &RecommendationExportReport{
			GeneratedAt: time.Now().UTC(), DataAsOf: asOf, QuoteSource: configuredQuoteSource(frozen), CalcVersion: configuredCalcVersion(frozen),
			Filters: recommendationExportFilters(filter), Total: ledger.Total, MaxItems: MaxRecommendationExportItems,
			BloggerCount: len(bloggers), SecurityCount: securityCount, Windows: windows, Summary: summary, Bloggers: bloggers, Items: ledger.Items,
		}
		return nil
	}, options)
	return report, err
}

func recommendationExportFilters(filter Filter) map[string]string {
	result := map[string]string{}
	for key, value := range map[string]string{"blogger_name": filter.BloggerName, "symbol": filter.Symbol, "ts_code": filter.TSCode, "market": filter.Market, "asset_type": filter.AssetType, "direction": filter.Direction, "status": filter.Status} {
		if value != "" {
			result[key] = value
		}
	}
	if filter.DateFrom != nil {
		result["date_from"] = filter.DateFrom.Format(time.DateOnly)
	}
	if filter.DateTo != nil {
		result["date_to"] = filter.DateTo.Format(time.DateOnly)
	}
	if filter.BloggerID > 0 {
		result["blogger_id"] = fmt.Sprint(filter.BloggerID)
	}
	return result
}

func summarizeRecommendationExport(items []RecommendationLedgerItem, windows []int) ([]RecommendationExportWindow, []RecommendationExportBlogger, int) {
	groups := make(map[int64][]RecommendationLedgerItem)
	securities := make(map[string]struct{})
	for _, item := range items {
		groups[item.BloggerID] = append(groups[item.BloggerID], item)
		key := item.TSCode
		if key == "" {
			key = item.Symbol + "." + item.Market
		}
		securities[key] = struct{}{}
	}
	bloggers := make([]RecommendationExportBlogger, 0, len(groups))
	for id, rows := range groups {
		bloggers = append(bloggers, RecommendationExportBlogger{BloggerID: id, BloggerName: rows[0].BloggerName, Institution: rows[0].Institution, RecommendationCount: len(rows), Windows: exportWindowSummary(rows, windows)})
	}
	sort.Slice(bloggers, func(i, j int) bool {
		if bloggers[i].RecommendationCount != bloggers[j].RecommendationCount {
			return bloggers[i].RecommendationCount > bloggers[j].RecommendationCount
		}
		return bloggers[i].BloggerID < bloggers[j].BloggerID
	})
	return exportWindowSummary(items, windows), bloggers, len(securities)
}

func exportWindowSummary(items []RecommendationLedgerItem, windows []int) []RecommendationExportWindow {
	result := make([]RecommendationExportWindow, 0, len(windows))
	for _, window := range windows {
		summary := RecommendationExportWindow{WindowDays: window, SampleCount: len(items)}
		var returns []float64
		for _, item := range items {
			metric := RecommendationWindowReturn{}
			for _, candidate := range item.Windows {
				if candidate.WindowDays == window {
					metric = candidate
					break
				}
			}
			switch {
			case metric.Status == "READY" && metric.ReturnRatio != nil && !math.IsNaN(*metric.ReturnRatio) && !math.IsInf(*metric.ReturnRatio, 0):
				summary.EvaluatedCount++
				returns = append(returns, *metric.ReturnRatio)
				if *metric.ReturnRatio > 0 {
					summary.RiseCount++
				}
			case metric.Status == "PENDING":
				summary.PendingCount++
			default:
				summary.UnavailableCount++
			}
		}
		if summary.EvaluatedCount > 0 {
			rate, avg := float64(summary.RiseCount)/float64(summary.EvaluatedCount), average(returns)
			summary.RiseRate, summary.AvgReturnRatio = &rate, &avg
		}
		result = append(result, summary)
	}
	return result
}
