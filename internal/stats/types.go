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
