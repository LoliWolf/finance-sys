import type {
  BloggerRankingResponse,
  BloggerSummaryResponse,
  BloggerTimeseriesResponse,
  DocumentRecord,
  EvaluationRun,
  PriceSeriesResponse,
  RecommendationDetail,
  RecommendationLedgerList,
  RecommendationPerformanceList,
  SecurityRankingItem,
  SecurityRankingResponse,
} from './types'

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
    throw new Error(payload.error || payload.message || `请求失败 (${response.status})`)
  }
  return response.json() as Promise<T>
}

export const api = {
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
  recommendationDetail: (id: number) => request<RecommendationDetail>(`/recommendations/${id}/performance`),
  priceSeries: (id: number, query: Query = {}) => request<PriceSeriesResponse>(`/recommendations/${id}/price-series${queryString(query)}`),
  securityRankings: (query: Query = {}) => request<SecurityRankingResponse>(`/securities/rankings${queryString(query)}`),
  securitySummary: (tsCode: string, query: Query = {}) => request<SecurityRankingItem>(`/securities/${encodeURIComponent(tsCode)}/performance/summary${queryString(query)}`),
  securityRecommendations: (tsCode: string, query: Query = {}) => request<RecommendationPerformanceList>(`/securities/${encodeURIComponent(tsCode)}/recommendations/performance${queryString(query)}`),
  evaluationRuns: (query: Query = {}) => request<{ items: EvaluationRun[] }>(`/admin/evaluations/recommendations/runs${queryString(query)}`),
  createEvaluationRun: (payload: Record<string, unknown>) => request<{ run_id: number; status: string; message: string }>('/admin/evaluations/recommendations/runs', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  }),
  documents: () => request<DocumentRecord[]>('/documents'),
  uploadDocument: (form: FormData) => request<Record<string, unknown>>('/documents/upload', { method: 'POST', body: form }),
}
