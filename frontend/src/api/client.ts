import type {
  BloggerRankingResponse,
  BloggerSummaryResponse,
  BloggerTimeseriesResponse,
  DocumentEvaluationRunResponse,
  DocumentRecord,
  DocumentReport,
  DocumentReportList,
  EvaluationRun,
  PriceSeriesResponse,
  RecommendationDetail,
  RecommendationLedgerList,
  RecommendationExportReport,
  RecommendationPerformanceList,
  SecurityRankingItem,
  SecurityRankingResponse,
  TradingDashboard,
} from './types'
import type { PageExportSnapshot, PageReportKind } from '../reports/types'

const API_BASE = import.meta.env.VITE_API_BASE || '/api/v1'

export type Query = Record<string, string | number | boolean | null | undefined>

function queryString(query: Query = {}) {
  const params = new URLSearchParams()
  Object.entries(query).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== '') params.set(key, String(value))
  })
  const value = params.toString()
  return value ? `?${value}` : ''
}

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const response = await fetch(`${API_BASE}${path}`, options)
  if (!response.ok) {
    const payload = await response.json().catch(() => ({ error: response.statusText }))
    throw Object.assign(new Error(payload.error || payload.message || `请求失败 (${response.status})`), { status: response.status, code: payload.code })
  }
  return response.json() as Promise<T>
}

export const api = {
  pageExport: (kind: PageReportKind, query: Query = {}, signal?: AbortSignal) => request<PageExportSnapshot>(`${kind === 'trading' ? '/trading/dashboard/report' : `/page-reports/${kind}`}${queryString(query)}`, { signal, cache: 'no-store' }),
  health: async () => {
    const response = await fetch('/healthz')
    if (!response.ok) throw new Error('服务不可用')
    return response.json() as Promise<{ status: string }>
  },
  bloggerRankings: (query: Query) => request<BloggerRankingResponse>(`/bloggers/rankings${queryString(query)}`),
  bloggerSummary: (id: number, query: Query = {}) => request<BloggerSummaryResponse>(`/bloggers/${id}/performance/summary${queryString(query)}`),
  bloggerTimeseries: (id: number, query: Query = {}) => request<BloggerTimeseriesResponse>(`/bloggers/${id}/performance/timeseries${queryString(query)}`),
  bloggerRecommendations: (id: number, query: Query = {}) => request<RecommendationPerformanceList>(`/bloggers/${id}/recommendations/performance${queryString(query)}`),
  recommendations: (query: Query = {}) => request<RecommendationLedgerList>(`/recommendation-performance${queryString(query)}`),
  recommendationExport: (query: Query = {}, signal?: AbortSignal) => request<RecommendationExportReport>(`/recommendation-performance/report${queryString(query)}`, { signal, cache: 'no-store' }),
  recommendationDetail: (id: number) => request<RecommendationDetail>(`/recommendations/${id}/performance`),
  priceSeries: (id: number, query: Query = {}) => request<PriceSeriesResponse>(`/recommendations/${id}/price-series${queryString(query)}`),
  securityRankings: (query: Query = {}) => request<SecurityRankingResponse>(`/securities/rankings${queryString(query)}`),
  securitySummary: (tsCode: string, query: Query = {}) => request<SecurityRankingItem>(`/securities/${encodeURIComponent(tsCode)}/performance/summary${queryString(query)}`),
  securityRecommendations: (tsCode: string, query: Query = {}) => request<RecommendationPerformanceList>(`/securities/${encodeURIComponent(tsCode)}/recommendations/performance${queryString(query)}`),
  tradingDashboard: (query: Query = {}) => request<TradingDashboard>(`/trading/dashboard${queryString(query)}`),
  refreshTradingDashboard: (query: Query = {}) => request<TradingDashboard>(`/trading/dashboard/refresh${queryString(query)}`, { method: 'POST' }),
  evaluationRuns: (query: Query = {}) => request<{ items: EvaluationRun[] }>(`/admin/evaluations/recommendations/runs${queryString(query)}`),
  createEvaluationRun: (payload: Record<string, unknown>) => request<{ run_id: number; status: string; message: string }>('/admin/evaluations/recommendations/runs', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  }),
  documents: () => request<DocumentRecord[]>('/documents'),
  documentReports: (query: Query = {}) => request<DocumentReportList>(`/document-reports${queryString(query)}`),
  documentReport: (id: number) => request<DocumentReport>(`/documents/${id}/report`),
  retryDocumentAnalysis: (id: number, tradeDate = '') => request<{ document_id: number; status: string; plan_count: number; trade_date: string; warning?: string }>(`/documents/${id}/retry-analysis`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(tradeDate ? { trade_date: tradeDate } : {}),
  }),
  createDocumentEvaluationRun: (documentIds: number[], forceRebuild = false) => request<DocumentEvaluationRunResponse>('/documents/evaluation-runs', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ document_ids: documentIds, force_rebuild: forceRebuild }),
  }),
  evaluationRun: (id: number) => request<EvaluationRun>(`/admin/evaluations/recommendations/runs/${id}`),
  uploadDocument: (form: FormData) => request<Record<string, unknown>>('/documents/upload', { method: 'POST', body: form }),
}
