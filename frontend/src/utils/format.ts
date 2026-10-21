export function formatPercent(value: number | null | undefined, digits = 1) {
  if (value === null || value === undefined || Number.isNaN(value)) return '—'
  return `${(value * 100).toFixed(digits)}%`
}

export function formatNumber(value: number | null | undefined, digits = 2) {
  if (value === null || value === undefined || Number.isNaN(value)) return '—'
  return new Intl.NumberFormat('zh-CN', { maximumFractionDigits: digits }).format(value)
}

export function formatCurrency(value: string | number | null | undefined, digits = 2) {
  const numeric = typeof value === 'string' ? Number(value) : value
  if (numeric === null || numeric === undefined || Number.isNaN(numeric)) return '—'
  return new Intl.NumberFormat('zh-CN', {
    style: 'currency',
    currency: 'CNY',
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  }).format(numeric)
}

export function formatDate(value: string | null | undefined) {
  if (!value) return '—'
  return value.slice(0, 10)
}

export function formatDateTime(value: string | null | undefined) {
  if (!value) return '—'
  return new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(new Date(value))
}

export function returnTone(value: number | null | undefined) {
  if (value === null || value === undefined) return 'muted'
  if (value > 0) return 'positive'
  if (value < 0) return 'negative'
  return 'neutral'
}

export function truncate(value: string, length = 48) {
  if (!value) return '—'
  return value.length > length ? `${value.slice(0, length)}…` : value
}

export function assetTypeLabel(value: string | null | undefined) {
  const labels: Record<string, string> = { STOCK: 'A 股', A_SHARE: 'A 股', ETF: 'ETF', SECTOR: '板块指数' }
  return labels[(value || '').toUpperCase()] || value || '未分类'
}

export function marketLabel(value: string | null | undefined) {
  const labels: Record<string, string> = { SH: '沪市', SZ: '深市', BJ: '北交所', DC: '东方财富板块' }
  return labels[(value || '').toUpperCase()] || value || '未知市场'
}

export function sectorTypeLabel(value: string | null | undefined) {
  const labels: Record<string, string> = { CONCEPT: '概念板块', INDUSTRY: '行业板块', REGION: '地域板块' }
  return labels[(value || '').toUpperCase()] || value || '板块指数'
}
