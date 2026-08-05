export interface Overview {
  total_bloggers: number
  evaluated_recommendations: number
  pending_recommendations: number
  incomplete_recommendations: number
  average_win_rate: number
  average_return_ratio: number
}

export interface BloggerRankingItem {
  rank: number
  blogger_id: number
  blogger_name: string
  institution: string
  sample_count: number
  evaluated_count: number
  pending_count: number
  incomplete_count: number
  win_count: number
  win_rate: number
  avg_return_ratio: number
  median_return_ratio: number
  best_return_ratio: number
  worst_return_ratio: number
  avg_max_favorable_return_ratio: number
  avg_max_adverse_return_ratio: number
  avg_max_drawdown_ratio: number
  performance_score: number
}

export interface BloggerRankingResponse {
  window_days: number
  date_from?: string
  date_to?: string
  overview: Overview
  items: BloggerRankingItem[]
}

export interface WindowSummary {
  window_days: number
  sample_count: number
  evaluated_count: number
  pending_count: number
  incomplete_count: number
  win_count: number
  win_rate: number
  avg_return_ratio: number
  median_return_ratio: number
  best_return_ratio: number
  worst_return_ratio: number
  avg_max_favorable_return_ratio: number
  avg_max_adverse_return_ratio: number
  avg_max_drawdown_ratio: number
}

export interface BloggerSummaryResponse {
  blogger_id: number
  blogger_name: string
  institution: string
  windows: WindowSummary[]
}

export interface TimeseriesPoint {
  period: string
  window_days: number
  sample_count: number
  evaluated_count: number
  pending_count: number
  win_count: number
  win_rate: number
  avg_return_ratio: number
}

export interface BloggerTimeseriesResponse {
  blogger_id: number
  window_days: number
  items: TimeseriesPoint[]
}

export interface RecommendationPerformanceItem {
  recommendation_event_id: number
  blogger_id: number
  blogger_name: string
  institution: string
  source_document_id: number
  ts_code: string
  symbol: string
  security_name: string
  asset_type: string
  market: string
  industry: string
  direction: string
  recommend_date: string
  thesis: string
  window_days: number
  status: MetricStatus
  reason_code: string
  reason_message: string
  entry_date: string | null
  entry_price: number | null
  exit_date: string | null
  exit_close_price: number | null
  direction_return_ratio: number | null
  max_favorable_return_ratio: number | null
  max_adverse_return_ratio: number | null
  max_drawdown_ratio: number | null
  win_flag: boolean | null
}

export interface RecommendationPerformanceList {
  total: number
  window_days: number
  items: RecommendationPerformanceItem[]
}

export interface RecommendationWindowReturn {
  window_days: number
  status: MetricStatus | ''
  reason_code: string
  reason_message: string
  return_ratio: number | null
}

export interface RecommendationLedgerItem {
  recommendation_event_id: number
  blogger_id: number
  blogger_name: string
  institution: string
  ts_code: string
  symbol: string
  security_name: string
  asset_type: string
  market: string
  direction: string
  recommend_date: string
  thesis: string
  windows: RecommendationWindowReturn[]
}

export interface RecommendationLedgerList {
  total: number
  page: number
  page_size: number
  total_pages: number
  items: RecommendationLedgerItem[]
}

export type MetricStatus = 'READY' | 'PENDING' | 'INCOMPLETE' | 'NO_SECURITY' | 'UNSUPPORTED' | 'FAILED'

export interface WindowMetric {
  id: number
  recommendation_event_id: number
  blogger_id: number
  security_master_id: number
  ts_code: string
  symbol: string
  security_name: string
  asset_type: string
  market: string
  industry: string
  direction: string
  recommend_date: string
  window_days: number
  quote_source: string
  status: MetricStatus
  reason_code: string
  reason_message: string
  base_date: string | null
  base_close_price: number | null
  entry_date: string | null
  entry_price: number | null
  exit_date: string | null
  exit_close_price: number | null
  expected_quote_count: number
  actual_quote_count: number
  missing_quote_count: number
  raw_return_ratio: number | null
  direction_return_ratio: number | null
  max_favorable_return_ratio: number | null
  max_adverse_return_ratio: number | null
  max_drawdown_ratio: number | null
  win_flag: boolean | null
  best_trade_date: string | null
  worst_trade_date: string | null
}

export interface RecommendationContext {
  recommendation_event_id: number
  blogger_id: number
  blogger_name: string
  institution: string
  source_document_id: number
  document_title: string
  document_file_name: string
  symbol: string
  asset_type: string
  market: string
  direction: string
  recommend_date: string
  reference_price: number
  confidence: number
  recommendation_status: string
  thesis: string
  evidence: Array<{ chunk_index: number; text: string }>
}

export interface RecommendationDetail {
  recommendation: RecommendationContext
  metrics: WindowMetric[]
}

export interface PricePoint {
  trade_date: string
  open_price: number
  high_price: number
  low_price: number
  close_price: number
  volume: number
  pct_chg: number
}

export interface PriceMarker {
  type: 'recommend' | 'entry' | 'exit' | 'best' | 'worst'
  label: string
  trade_date: string | null
  window_days?: number
}

export interface PriceSeriesResponse {
  recommendation_event_id: number
  ts_code: string
  security_name: string
  recommend_date: string
  items: PricePoint[]
  markers: PriceMarker[]
}

export interface SecurityRankingItem {
  rank: number
  security_master_id: number
  ts_code: string
  symbol: string
  security_name: string
  asset_type: string
  market: string
  industry: string
  recommendation_count: number
  blogger_count: number
  evaluated_count: number
  pending_count: number
  win_count: number
  win_rate: number
  avg_return_ratio: number
  median_return_ratio: number
  best_return_ratio: number
  worst_return_ratio: number
}

export interface SecurityRankingResponse {
  window_days: number
  items: SecurityRankingItem[]
}

export interface EvaluationRun {
  id: number
  run_type: string
  status: string
  request_params: Record<string, unknown>
  target_event_count: number
  evaluated_event_count: number
  window_metric_count: number
  pending_count: number
  incomplete_count: number
  failed_count: number
  worker_id: string
  queued_at: string
  started_at: string | null
  finished_at: string | null
  error_code: string
  error_message: string
}

export interface DocumentRecord {
  id: number
  title: string
  file_name: string
  status: string
  created_at: string
}
