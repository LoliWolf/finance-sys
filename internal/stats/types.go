package stats

import (
	"time"

	"finance-sys/internal/domain/db_model"
)

type Filter struct {
	WindowDays     int
	DateFrom       *time.Time
	DateTo         *time.Time
	Market         string
	AssetType      string
	Direction      string
	Status         string
	BloggerID      int64
	BloggerName    string
	TSCode         string
	Symbol         string
	MinSampleCount int
	Sort           string
	Limit          int
	Offset         int
}

type DocumentReportListFilter struct {
	Query         string
	Status        string
	CreatedFrom   *time.Time
	CreatedBefore *time.Time
	Limit         int
	Offset        int
}

type DocumentReportListItem struct {
	DocumentID            int64      `json:"document_id"`
	Author                string     `json:"author"`
	Institution           string     `json:"institution"`
	Title                 string     `json:"title"`
	FileName              string     `json:"file_name"`
	Status                string     `json:"status"`
	ConfigVersion         int64      `json:"config_version"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
	RecommendDateFrom     *time.Time `json:"recommend_date_from"`
	RecommendDateTo       *time.Time `json:"recommend_date_to"`
	RecommendationCount   int        `json:"recommendation_count"`
	BloggerCount          int        `json:"blogger_count"`
	UntrackableCount      int        `json:"untrackable_count"`
	ExpectedMetricCount   int        `json:"expected_metric_count"`
	ReadyMetricCount      int        `json:"ready_metric_count"`
	PendingMetricCount    int        `json:"pending_metric_count"`
	IncompleteMetricCount int        `json:"incomplete_metric_count"`
	MissingMetricCount    int        `json:"missing_metric_count"`
	ReportStatus          string     `json:"report_status"`
}

type DocumentReportList struct {
	Total      int64                    `json:"total"`
	Page       int                      `json:"page"`
	PageSize   int                      `json:"page_size"`
	TotalPages int                      `json:"total_pages"`
	DataAsOf   *time.Time               `json:"data_as_of"`
	Items      []DocumentReportListItem `json:"items"`
}

type DocumentReportDocument struct {
	DocumentID    int64     `json:"document_id"`
	Author        string    `json:"author"`
	Institution   string    `json:"institution"`
	Title         string    `json:"title"`
	FileName      string    `json:"file_name"`
	Status        string    `json:"status"`
	ConfigVersion int64     `json:"config_version"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type DocumentReportWindowMetric struct {
	WindowDays              int        `json:"window_days"`
	Status                  string     `json:"status"`
	ReasonCode              string     `json:"reason_code"`
	ReasonMessage           string     `json:"reason_message"`
	EntryDate               *time.Time `json:"entry_date"`
	EntryPrice              *float64   `json:"entry_price"`
	ExitDate                *time.Time `json:"exit_date"`
	ExitClosePrice          *float64   `json:"exit_close_price"`
	DirectionReturnRatio    *float64   `json:"direction_return_ratio"`
	MaxFavorableReturnRatio *float64   `json:"max_favorable_return_ratio"`
	MaxAdverseReturnRatio   *float64   `json:"max_adverse_return_ratio"`
	MaxDrawdownRatio        *float64   `json:"max_drawdown_ratio"`
	WinFlag                 *bool      `json:"win_flag"`
	ExpectedQuoteCount      int        `json:"expected_quote_count"`
	ActualQuoteCount        int        `json:"actual_quote_count"`
	MissingQuoteCount       int        `json:"missing_quote_count"`
	CalcVersion             string     `json:"calc_version"`
	CalculatedAt            *time.Time `json:"calculated_at"`
	Outdated                bool       `json:"outdated"`
}

type DocumentReportCurrentMetric struct {
	Status               string     `json:"status"`
	ReasonCode           string     `json:"reason_code"`
	ReasonMessage        string     `json:"reason_message"`
	EntryDate            *time.Time `json:"entry_date"`
	EntryPrice           *float64   `json:"entry_price"`
	LatestTradeDate      *time.Time `json:"latest_trade_date"`
	LatestClosePrice     *float64   `json:"latest_close_price"`
	DirectionReturnRatio *float64   `json:"direction_return_ratio"`
	WinFlag              *bool      `json:"win_flag"`
}

type DocumentReportRecommendation struct {
	RecommendationEventID int64                        `json:"recommendation_event_id"`
	BloggerID             int64                        `json:"blogger_id"`
	BloggerName           string                       `json:"blogger_name"`
	Institution           string                       `json:"institution"`
	TSCode                string                       `json:"ts_code"`
	Symbol                string                       `json:"symbol"`
	SecurityName          string                       `json:"security_name"`
	AssetType             string                       `json:"asset_type"`
	Market                string                       `json:"market"`
	SectorType            string                       `json:"sector_type"`
	Direction             string                       `json:"direction"`
	RecommendDate         time.Time                    `json:"recommend_date"`
	ReferencePrice        float64                      `json:"reference_price"`
	Confidence            float64                      `json:"confidence"`
	RecommendationStatus  string                       `json:"recommendation_status"`
	Thesis                string                       `json:"thesis"`
	Evidence              []Evidence                   `json:"evidence"`
	Windows               []DocumentReportWindowMetric `json:"windows"`
	Current               DocumentReportCurrentMetric  `json:"current"`
}

type DocumentReportCurrentSummary struct {
	SampleCount       int     `json:"sample_count"`
	EvaluatedCount    int     `json:"evaluated_count"`
	PendingCount      int     `json:"pending_count"`
	IncompleteCount   int     `json:"incomplete_count"`
	WinCount          int     `json:"win_count"`
	WinRate           float64 `json:"win_rate"`
	AvgReturnRatio    float64 `json:"avg_return_ratio"`
	MedianReturnRatio float64 `json:"median_return_ratio"`
	BestReturnRatio   float64 `json:"best_return_ratio"`
	WorstReturnRatio  float64 `json:"worst_return_ratio"`
}

type DocumentReportBloggerGroup struct {
	BloggerID              int64                        `json:"blogger_id"`
	BloggerName            string                       `json:"blogger_name"`
	Institution            string                       `json:"institution"`
	RecommendationCount    int                          `json:"recommendation_count"`
	RecommendationEventIDs []int64                      `json:"recommendation_event_ids"`
	Windows                []WindowSummary              `json:"windows"`
	Current                DocumentReportCurrentSummary `json:"current"`
}

type DocumentReportUntrackableTarget struct {
	ID               int64  `json:"id"`
	RawTarget        string `json:"raw_target"`
	NormalizedTarget string `json:"normalized_target"`
	TargetKind       string `json:"target_kind"`
	ReasonCode       string `json:"reason_code"`
	ReasonMessage    string `json:"reason_message"`
	Source           string `json:"source"`
}

type DocumentReportSummary struct {
	RecommendationCount int                          `json:"recommendation_count"`
	BloggerCount        int                          `json:"blogger_count"`
	UntrackableCount    int                          `json:"untrackable_count"`
	Windows             []WindowSummary              `json:"windows"`
	Current             DocumentReportCurrentSummary `json:"current"`
}

type DocumentReportMethodology struct {
	EntryPriceRule        string  `json:"entry_price_rule"`
	BasePriceRule         string  `json:"base_price_rule"`
	WinThresholdRatio     float64 `json:"win_threshold_ratio"`
	MinQuoteCoverageRatio float64 `json:"min_quote_coverage_ratio"`
}

type DocumentReport struct {
	GeneratedAt        time.Time                         `json:"generated_at"`
	DataAsOf           *time.Time                        `json:"data_as_of"`
	QuoteSource        string                            `json:"quote_source"`
	CalcVersion        string                            `json:"calc_version"`
	Windows            []int                             `json:"windows"`
	Document           DocumentReportDocument            `json:"document"`
	Summary            DocumentReportSummary             `json:"summary"`
	Bloggers           []DocumentReportBloggerGroup      `json:"bloggers"`
	Recommendations    []DocumentReportRecommendation    `json:"recommendations"`
	UntrackableTargets []DocumentReportUntrackableTarget `json:"untrackable_targets"`
	Methodology        DocumentReportMethodology         `json:"methodology"`
}

type Overview struct {
	TotalBloggers             int     `json:"total_bloggers"`
	EvaluatedRecommendations  int     `json:"evaluated_recommendations"`
	PendingRecommendations    int     `json:"pending_recommendations"`
	IncompleteRecommendations int     `json:"incomplete_recommendations"`
	AverageWinRate            float64 `json:"average_win_rate"`
	AverageReturnRatio        float64 `json:"average_return_ratio"`
}

type BloggerRankingItem struct {
	Rank                       int     `json:"rank"`
	BloggerID                  int64   `json:"blogger_id"`
	BloggerName                string  `json:"blogger_name"`
	Institution                string  `json:"institution"`
	SampleCount                int     `json:"sample_count"`
	EvaluatedCount             int     `json:"evaluated_count"`
	PendingCount               int     `json:"pending_count"`
	IncompleteCount            int     `json:"incomplete_count"`
	WinCount                   int     `json:"win_count"`
	WinRate                    float64 `json:"win_rate"`
	AvgReturnRatio             float64 `json:"avg_return_ratio"`
	MedianReturnRatio          float64 `json:"median_return_ratio"`
	BestReturnRatio            float64 `json:"best_return_ratio"`
	WorstReturnRatio           float64 `json:"worst_return_ratio"`
	AvgMaxFavorableReturnRatio float64 `json:"avg_max_favorable_return_ratio"`
	AvgMaxAdverseReturnRatio   float64 `json:"avg_max_adverse_return_ratio"`
	AvgMaxDrawdownRatio        float64 `json:"avg_max_drawdown_ratio"`
	PerformanceScore           float64 `json:"performance_score"`
}

type BloggerRankingResponse struct {
	WindowDays int                  `json:"window_days"`
	DateFrom   string               `json:"date_from,omitempty"`
	DateTo     string               `json:"date_to,omitempty"`
	Overview   Overview             `json:"overview"`
	Items      []BloggerRankingItem `json:"items"`
}

type WindowSummary struct {
	WindowDays                 int     `json:"window_days"`
	SampleCount                int     `json:"sample_count"`
	EvaluatedCount             int     `json:"evaluated_count"`
	PendingCount               int     `json:"pending_count"`
	IncompleteCount            int     `json:"incomplete_count"`
	WinCount                   int     `json:"win_count"`
	WinRate                    float64 `json:"win_rate"`
	AvgReturnRatio             float64 `json:"avg_return_ratio"`
	MedianReturnRatio          float64 `json:"median_return_ratio"`
	BestReturnRatio            float64 `json:"best_return_ratio"`
	WorstReturnRatio           float64 `json:"worst_return_ratio"`
	AvgMaxFavorableReturnRatio float64 `json:"avg_max_favorable_return_ratio"`
	AvgMaxAdverseReturnRatio   float64 `json:"avg_max_adverse_return_ratio"`
	AvgMaxDrawdownRatio        float64 `json:"avg_max_drawdown_ratio"`
}

type BloggerSummaryResponse struct {
	BloggerID   int64           `json:"blogger_id"`
	BloggerName string          `json:"blogger_name"`
	Institution string          `json:"institution"`
	Windows     []WindowSummary `json:"windows"`
}

type TimeseriesPoint struct {
	Period         string  `json:"period"`
	WindowDays     int     `json:"window_days"`
	SampleCount    int     `json:"sample_count"`
	EvaluatedCount int     `json:"evaluated_count"`
	PendingCount   int     `json:"pending_count"`
	WinCount       int     `json:"win_count"`
	WinRate        float64 `json:"win_rate"`
	AvgReturnRatio float64 `json:"avg_return_ratio"`
}

type BloggerTimeseriesResponse struct {
	BloggerID  int64             `json:"blogger_id"`
	WindowDays int               `json:"window_days"`
	Items      []TimeseriesPoint `json:"items"`
}

type RecommendationPerformanceItem struct {
	RecommendationEventID   int64      `json:"recommendation_event_id"`
	BloggerID               int64      `json:"blogger_id"`
	BloggerName             string     `json:"blogger_name"`
	Institution             string     `json:"institution"`
	SourceDocumentID        int64      `json:"source_document_id"`
	TSCode                  string     `json:"ts_code"`
	Symbol                  string     `json:"symbol"`
	SecurityName            string     `json:"security_name"`
	AssetType               string     `json:"asset_type"`
	Market                  string     `json:"market"`
	Industry                string     `json:"industry"`
	SectorType              string     `json:"sector_type"`
	Direction               string     `json:"direction"`
	RecommendDate           time.Time  `json:"recommend_date"`
	Thesis                  string     `json:"thesis"`
	WindowDays              int        `json:"window_days"`
	Status                  string     `json:"status"`
	ReasonCode              string     `json:"reason_code"`
	ReasonMessage           string     `json:"reason_message"`
	EntryDate               *time.Time `json:"entry_date"`
	EntryPrice              *float64   `json:"entry_price"`
	ExitDate                *time.Time `json:"exit_date"`
	ExitClosePrice          *float64   `json:"exit_close_price"`
	DirectionReturnRatio    *float64   `json:"direction_return_ratio"`
	MaxFavorableReturnRatio *float64   `json:"max_favorable_return_ratio"`
	MaxAdverseReturnRatio   *float64   `json:"max_adverse_return_ratio"`
	MaxDrawdownRatio        *float64   `json:"max_drawdown_ratio"`
	WinFlag                 *bool      `json:"win_flag"`
}

type RecommendationPerformanceList struct {
	Total      int64                           `json:"total"`
	WindowDays int                             `json:"window_days"`
	Items      []RecommendationPerformanceItem `json:"items"`
}

type RecommendationWindowReturn struct {
	WindowDays    int      `json:"window_days"`
	Status        string   `json:"status"`
	ReasonCode    string   `json:"reason_code"`
	ReasonMessage string   `json:"reason_message"`
	ReturnRatio   *float64 `json:"return_ratio"`
}

type RecommendationLedgerItem struct {
	RecommendationEventID int64                        `json:"recommendation_event_id"`
	BloggerID             int64                        `json:"blogger_id"`
	BloggerName           string                       `json:"blogger_name"`
	Institution           string                       `json:"institution"`
	TSCode                string                       `json:"ts_code"`
	Symbol                string                       `json:"symbol"`
	SecurityName          string                       `json:"security_name"`
	AssetType             string                       `json:"asset_type"`
	Market                string                       `json:"market"`
	SectorType            string                       `json:"sector_type"`
	Direction             string                       `json:"direction"`
	RecommendDate         time.Time                    `json:"recommend_date"`
	Thesis                string                       `json:"thesis"`
	Windows               []RecommendationWindowReturn `gorm:"-" json:"windows"`
}

type RecommendationLedgerList struct {
	Total      int64                      `json:"total"`
	Page       int                        `json:"page"`
	PageSize   int                        `json:"page_size"`
	TotalPages int                        `json:"total_pages"`
	Items      []RecommendationLedgerItem `json:"items"`
}

type Evidence struct {
	ChunkIndex int    `json:"chunk_index"`
	Text       string `json:"text"`
}

type RecommendationContext struct {
	RecommendationEventID int64      `json:"recommendation_event_id"`
	BloggerID             int64      `json:"blogger_id"`
	BloggerName           string     `json:"blogger_name"`
	Institution           string     `json:"institution"`
	SourceDocumentID      int64      `json:"source_document_id"`
	DocumentTitle         string     `json:"document_title"`
	DocumentFileName      string     `json:"document_file_name"`
	Symbol                string     `json:"symbol"`
	AssetType             string     `json:"asset_type"`
	Market                string     `json:"market"`
	SectorType            string     `json:"sector_type"`
	Direction             string     `json:"direction"`
	RecommendDate         time.Time  `json:"recommend_date"`
	ReferencePrice        float64    `json:"reference_price"`
	Confidence            float64    `json:"confidence"`
	RecommendationStatus  string     `json:"recommendation_status"`
	Thesis                string     `json:"thesis"`
	Evidence              []Evidence `gorm:"-" json:"evidence"`
}

type RecommendationDetail struct {
	Recommendation RecommendationContext                      `json:"recommendation"`
	Metrics        []db_model.RecommendationEventWindowMetric `json:"metrics"`
}

type PricePoint struct {
	TradeDate  time.Time `json:"trade_date"`
	OpenPrice  float64   `json:"open_price"`
	HighPrice  float64   `json:"high_price"`
	LowPrice   float64   `json:"low_price"`
	ClosePrice float64   `json:"close_price"`
	Volume     float64   `json:"volume"`
	PctChg     float64   `json:"pct_chg"`
}

type PriceMarker struct {
	Type       string     `json:"type"`
	Label      string     `json:"label"`
	TradeDate  *time.Time `json:"trade_date"`
	WindowDays int        `json:"window_days,omitempty"`
}

type PriceSeriesResponse struct {
	RecommendationEventID int64         `json:"recommendation_event_id"`
	TSCode                string        `json:"ts_code"`
	SecurityName          string        `json:"security_name"`
	RecommendDate         time.Time     `json:"recommend_date"`
	Items                 []PricePoint  `json:"items"`
	Markers               []PriceMarker `json:"markers"`
}

type SecurityRankingItem struct {
	Rank                int     `json:"rank"`
	SecurityMasterID    int64   `json:"security_master_id"`
	TSCode              string  `json:"ts_code"`
	Symbol              string  `json:"symbol"`
	SecurityName        string  `json:"security_name"`
	AssetType           string  `json:"asset_type"`
	Market              string  `json:"market"`
	Industry            string  `json:"industry"`
	SectorType          string  `json:"sector_type"`
	RecommendationCount int     `json:"recommendation_count"`
	BloggerCount        int     `json:"blogger_count"`
	EvaluatedCount      int     `json:"evaluated_count"`
	PendingCount        int     `json:"pending_count"`
	WinCount            int     `json:"win_count"`
	WinRate             float64 `json:"win_rate"`
	AvgReturnRatio      float64 `json:"avg_return_ratio"`
	MedianReturnRatio   float64 `json:"median_return_ratio"`
	BestReturnRatio     float64 `json:"best_return_ratio"`
	WorstReturnRatio    float64 `json:"worst_return_ratio"`
}

type SecurityRankingResponse struct {
	WindowDays int                   `json:"window_days"`
	Items      []SecurityRankingItem `json:"items"`
}
