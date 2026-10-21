import type { BloggerRankingResponse, BloggerSummaryResponse, BloggerTimeseriesResponse, RecommendationPerformanceList, SecurityRankingResponse, SecurityRankingItem, RecommendationDetail, PriceSeriesResponse, DocumentReportList, DocumentReport, EvaluationRun, TradingDashboard } from '../api/types'

export type PageReportKind = 'dashboard' | 'blogger' | 'recommendation' | 'securities' | 'documents' | 'workbench' | 'trading'
export interface PageExportSnapshot {
  kind: PageReportKind
  generated_at: string
  data_as_of?: string | null
  quote_source?: string
  calc_version?: string
  filters?: Record<string, string>
  max_items: number
  cycle_limit?: number
  dashboard?: BloggerRankingResponse
  blogger?: BloggerSummaryResponse
  timeseries?: BloggerTimeseriesResponse
  recommendations?: RecommendationPerformanceList
  securities?: SecurityRankingResponse
  selected_security?: SecurityRankingItem
  recommendation?: RecommendationDetail
  prices?: PriceSeriesResponse
  documents?: DocumentReportList
  recent_documents?: DocumentReport['document'][]
  runs?: Pick<EvaluationRun, 'id' | 'run_type' | 'status' | 'target_event_count' | 'evaluated_event_count' | 'window_metric_count' | 'pending_count' | 'incomplete_count' | 'failed_count' | 'queued_at'>[]
  trading?: TradingDashboard
}

export interface ReportChart {
  type: 'bar' | 'line'
  title: string
  percent?: boolean
  points: { label: string; value: number | null }[]
}
export interface ReportSection {
  title: string
  description?: string
  columns?: string[]
  rows?: string[][]
  paragraphs?: string[]
  chart?: ReportChart
  widths?: number[]
}
export interface PrintablePageReport {
  title: string
  subtitle: string
  generatedAt: string
  meta: string[]
  filters: { label: string; value: string }[]
  metrics: { label: string; value: string; note?: string }[]
  sections: ReportSection[]
  statements: string[]
}
