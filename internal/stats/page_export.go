package stats

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"finance-sys/internal/config"
	"gorm.io/gorm"
)

const MaxPageExportItems = 2000

type PageExportLimitError struct{ Total, Limit int }

func (e *PageExportLimitError) Error() string {
	return fmt.Sprintf("当前报告包含 %d 条记录，单次最多导出 %d 条；请缩小筛选范围", e.Total, e.Limit)
}
func checkPageExportLimit(total, limit int) error {
	if total > limit {
		return &PageExportLimitError{Total: total, Limit: limit}
	}
	return nil
}

type PageExportRequest struct {
	Kind           string
	Filter         Filter
	Documents      DocumentReportListFilter
	EventID        int64
	SelectedTSCode string
	Search         string
}

// Only whitelisted task fields belong in a portable report, never raw request JSON.
type PageExportRun struct {
	ID                  int64     `json:"id"`
	RunType             string    `json:"run_type"`
	Status              string    `json:"status"`
	TargetEventCount    int64     `json:"target_event_count"`
	EvaluatedEventCount int64     `json:"evaluated_event_count"`
	WindowMetricCount   int64     `json:"window_metric_count"`
	PendingCount        int64     `json:"pending_count"`
	IncompleteCount     int64     `json:"incomplete_count"`
	FailedCount         int64     `json:"failed_count"`
	QueuedAt            time.Time `json:"queued_at"`
}

type PageExportReport struct {
	Kind             string                         `json:"kind"`
	GeneratedAt      time.Time                      `json:"generated_at"`
	DataAsOf         *time.Time                     `json:"data_as_of"`
	QuoteSource      string                         `json:"quote_source"`
	CalcVersion      string                         `json:"calc_version"`
	Filters          map[string]string              `json:"filters"`
	MaxItems         int                            `json:"max_items"`
	Dashboard        *BloggerRankingResponse        `json:"dashboard,omitempty"`
	Blogger          *BloggerSummaryResponse        `json:"blogger,omitempty"`
	Timeseries       *BloggerTimeseriesResponse     `json:"timeseries,omitempty"`
	Recommendations  *RecommendationPerformanceList `json:"recommendations,omitempty"`
	Securities       *SecurityRankingResponse       `json:"securities,omitempty"`
	SelectedSecurity *SecurityRankingItem           `json:"selected_security,omitempty"`
	Recommendation   *RecommendationDetail          `json:"recommendation,omitempty"`
	Prices           *PriceSeriesResponse           `json:"prices,omitempty"`
	Documents        *DocumentReportList            `json:"documents,omitempty"`
	RecentDocuments  []DocumentReportDocument       `json:"recent_documents"`
	Runs             []PageExportRun                `json:"runs"`
}

func (s *Service) PageExport(ctx context.Context, request PageExportRequest) (*PageExportReport, error) {
	frozen := config.NewRuntime(s.runtime.Current())
	options := &sql.TxOptions{ReadOnly: true}
	if s.db.Dialector.Name() == "mysql" {
		options.Isolation = sql.LevelRepeatableRead
	}
	var report *PageExportReport
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		reader := NewService(tx, frozen)
		filter := reader.normalizeFilter(request.Filter)
		result := &PageExportReport{Kind: request.Kind, GeneratedAt: time.Now().UTC(), MaxItems: MaxPageExportItems,
			QuoteSource: configuredQuoteSource(frozen), CalcVersion: configuredCalcVersion(frozen), Filters: recommendationExportFilters(filter)}
		result.Filters["window_days"] = fmt.Sprint(filter.WindowDays)
		result.Filters["min_sample_count"] = fmt.Sprint(filter.MinSampleCount)
		result.Filters["sort"] = filter.Sort
		var err error
		switch request.Kind {
		case "dashboard":
			result.Dashboard, err = reader.bloggerRankings(ctx, request.Filter, MaxPageExportItems)
		case "blogger":
			if filter.BloggerID <= 0 {
				return fmt.Errorf("blogger_id is required")
			}
			result.Blogger, err = reader.BloggerSummary(ctx, filter.BloggerID, filter)
			if err == nil {
				result.Timeseries, err = reader.BloggerTimeseries(ctx, filter.BloggerID, filter)
			}
			if err == nil {
				result.Recommendations, err = reader.recommendationPerformanceList(ctx, filter, MaxPageExportItems)
			}
			delete(result.Filters, "min_sample_count")
			delete(result.Filters, "sort")
		case "securities":
			result.Securities, err = reader.securityRankings(ctx, request.Filter, MaxPageExportItems, request.Search)
			if request.Search != "" {
				result.Filters["search"] = request.Search
			}
			if err == nil && request.SelectedTSCode != "" {
				// An old selection outside the new filter must not leak into this report.
				for _, item := range result.Securities.Items {
					if item.TSCode == strings.ToUpper(request.SelectedTSCode) {
						selected := item
						result.SelectedSecurity = &selected
						break
					}
				}
				if result.SelectedSecurity != nil {
					filter.TSCode = result.SelectedSecurity.TSCode
					result.Filters["selected_ts_code"] = filter.TSCode
					result.Recommendations, err = reader.recommendationPerformanceList(ctx, filter, MaxPageExportItems)
				}
			}
		case "recommendation":
			if request.EventID <= 0 {
				return fmt.Errorf("event_id is required")
			}
			result.Filters = map[string]string{"event_id": fmt.Sprint(request.EventID)}
			result.Recommendation, err = reader.RecommendationDetail(ctx, request.EventID)
			if err == nil {
				result.Prices, err = reader.PriceSeries(ctx, request.EventID, 5, 90)
			}
		case "documents":
			result.Filters = map[string]string{}
			f := request.Documents
			if f.Query != "" {
				result.Filters["query"] = f.Query
			}
			if f.Status != "" {
				result.Filters["status"] = f.Status
			}
			location := time.FixedZone("Asia/Shanghai", 8*60*60)
			if f.CreatedFrom != nil {
				result.Filters["date_from"] = f.CreatedFrom.In(location).Format(time.DateOnly)
			}
			if f.CreatedBefore != nil {
				result.Filters["date_to"] = f.CreatedBefore.In(location).AddDate(0, 0, -1).Format(time.DateOnly)
			}
			result.Documents, err = reader.documentReports(ctx, f, MaxPageExportItems)
		case "workbench":
			result.Runs = []PageExportRun{}
			result.RecentDocuments = []DocumentReportDocument{}
			result.Filters = map[string]string{"scope": "最近 30 条评价任务与最近 20 篇文档"}
			err = tx.Table("recommendation_evaluation_runs").Select("id, run_type, status, target_event_count, evaluated_event_count, window_metric_count, pending_count, incomplete_count, failed_count, queued_at").Order("id DESC").Limit(30).Scan(&result.Runs).Error
			if err == nil {
				err = tx.Table("documents").Select("id AS document_id, title, file_name, author, institution, status, created_at, updated_at, config_version").Order("id DESC").Limit(20).Scan(&result.RecentDocuments).Error
			}
		default:
			return fmt.Errorf("unsupported report kind")
		}
		if err != nil {
			return err
		}
		if request.Kind != "workbench" {
			result.DataAsOf, err = reader.documentReportDataAsOf(ctx, result.QuoteSource)
			if err != nil {
				return err
			}
		}
		report = result
		return nil
	}, options)
	return report, err
}
