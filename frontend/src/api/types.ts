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
  sector_type: string
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
  sector_type: string
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

export type MetricStatus = 'READY' | 'PENDING' | 'INCOMPLETE' | 'NO_SECURITY' | 'UNSUPPORTED' | 'FAILED' | 'NOT_EVALUATED' | 'OUTDATED'

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
  sector_type: string
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
  sector_type: string
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
  sector_type: string
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

export interface DocumentReportListItem {
  document_id: number
  author: string
  institution: string
  title: string
  file_name: string
  status: string
  config_version: number
  created_at: string
  updated_at: string
  recommend_date_from: string | null
  recommend_date_to: string | null
  recommendation_count: number
  blogger_count: number
  untrackable_count: number
  expected_metric_count: number
  ready_metric_count: number
  pending_metric_count: number
  incomplete_metric_count: number
  missing_metric_count: number
  report_status: string
}

export interface DocumentReportList {
  total: number
  page: number
  page_size: number
  total_pages: number
  data_as_of: string | null
  items: DocumentReportListItem[]
}

export interface DocumentReportWindowMetric {
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
  expected_quote_count: number
  actual_quote_count: number
  missing_quote_count: number
  calc_version: string
  calculated_at: string | null
  outdated: boolean
}

export interface DocumentReportCurrentMetric {
  status: MetricStatus
  reason_code: string
  reason_message: string
  entry_date: string | null
  entry_price: number | null
  latest_trade_date: string | null
  latest_close_price: number | null
  direction_return_ratio: number | null
  win_flag: boolean | null
}

export interface DocumentReportRecommendation {
  recommendation_event_id: number
  blogger_id: number
  blogger_name: string
  institution: string
  ts_code: string
  symbol: string
  security_name: string
  asset_type: string
  market: string
  sector_type: string
  direction: string
  recommend_date: string
  reference_price: number
  confidence: number
  recommendation_status: string
  thesis: string
  evidence: Array<{ chunk_index: number; text: string }>
  windows: DocumentReportWindowMetric[]
  current: DocumentReportCurrentMetric
}

export interface DocumentReportCurrentSummary {
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
}

export interface DocumentReportBloggerGroup {
  blogger_id: number
  blogger_name: string
  institution: string
  recommendation_count: number
  recommendation_event_ids: number[]
  windows: WindowSummary[]
  current: DocumentReportCurrentSummary
}

export interface DocumentReportUntrackableTarget {
  id: number
  raw_target: string
  normalized_target: string
  target_kind: string
  reason_code: string
  reason_message: string
  source: string
}

export interface DocumentReport {
  generated_at: string
  data_as_of: string | null
  quote_source: string
  calc_version: string
  windows: number[]
  document: {
    document_id: number
    author: string
    institution: string
    title: string
    file_name: string
    status: string
    config_version: number
    created_at: string
    updated_at: string
  }
  summary: {
    recommendation_count: number
    blogger_count: number
    untrackable_count: number
    windows: WindowSummary[]
    current: DocumentReportCurrentSummary
  }
  bloggers: DocumentReportBloggerGroup[]
  recommendations: DocumentReportRecommendation[]
  untrackable_targets: DocumentReportUntrackableTarget[]
  methodology: {
    entry_price_rule: string
    base_price_rule: string
    win_threshold_ratio: number
    min_quote_coverage_ratio: number
  }
}

export interface DocumentEvaluationRunResponse {
  run_id: number
  status: string
  run_type: string
  message: string
  document_count: number
}

export interface TradingDashboardRuntime {
  trading_enabled: boolean
  nacos_kill_switch: boolean
  runtime_kill_switch: boolean
  scheduler_enabled: boolean
  exit_enabled: boolean
  reconciliation_enabled: boolean
  environment: string
  provider: string
  config_version: number
}

export interface TradingDashboardAccount {
  account_id: string
  account_name: string
  nav: string
  balance: string
  available_cash: string
  frozen_cash: string
  market_value: string
  position_ratio: string
  floating_pnl: string
  cumulative_pnl: string
  cumulative_commission: string
  commission_data_status: string
  terminal_state: string
  account_state: string
  snapshot_at: string
  snapshot_age_seconds: number
  snapshot_max_age_seconds: number
  snapshot_stale: boolean
}

export interface TradingDashboardPosition {
  id: number
  symbol: string
  ts_code: string
  security_name: string
  market: string
  asset_type: string
  eastmoney_symbol: string
  volume: number
  available_volume: number
  today_volume: number
  vwap: string
  last_price: string
  market_value: string
  floating_pnl: string
  floating_pnl_ratio: string
  cycle_id: number | null
  cycle_status: string
  stop_loss_price: string
  take_profit_price: string
  holding_trade_days: number
  max_holding_trade_days: number
  exit_reason: string
}

export interface TradingDashboardDailySummary {
  fill_count: number
  buy_count: number
  sell_count: number
  buy_volume: number
  sell_volume: number
  buy_amount: string
  sell_amount: string
  commission: string
  net_cash_flow: string
}

export interface TradingDashboardFill {
  id: number
  trading_order_id: number
  client_order_id: string
  symbol: string
  ts_code: string
  security_name: string
  side: string
  price: string
  volume: number
  amount: string
  commission: string
  commission_status: string
  order_status: string
  traded_at: string
}

export interface TradingDashboardOrder {
  id: number
  client_order_id: string
  symbol: string
  ts_code: string
  security_name: string
  side: string
  order_type: string
  limit_price: string | null
  volume: number
  filled_volume: number
  filled_vwap: string | null
  filled_amount: string
  filled_commission: string
  status: string
  provider_status: string
  error_code: string
  error_message: string
  created_at: string
  submitted_at: string | null
  finished_at: string | null
}

export interface TradingDashboardCycle {
  id: number
  source_recommendation_event_id: number | null
  symbol: string
  ts_code: string
  security_name: string
  status: string
  entry_trade_date: string
  sellable_trade_date: string
  entry_price: string
  initial_volume: number
  current_volume: number
  stop_loss_price: string
  take_profit_price: string
  holding_trade_days: number
  max_holding_trade_days: number
  exit_reason: string
  exit_price: string | null
  realized_pnl: string | null
  realized_pnl_ratio: string | null
  opened_at: string
  closed_at: string | null
}

export interface TradingDashboard {
  trade_date: string
  runtime: TradingDashboardRuntime
  account: TradingDashboardAccount | null
  positions: TradingDashboardPosition[]
  daily_summary: TradingDashboardDailySummary
  fills: TradingDashboardFill[]
  orders: TradingDashboardOrder[]
  position_cycles: TradingDashboardCycle[]
}
