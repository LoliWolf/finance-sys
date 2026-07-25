export function formatPercent(value: number | null | undefined, digits = 1) {
  if (value === null || value === undefined || Number.isNaN(value)) return '—'
  return `${(value * 100).toFixed(digits)}%`
}

export function formatNumber(value: number | null | undefined, digits = 2) {
  if (value === null || value === undefined || Number.isNaN(value)) return '—'
  return new Intl.NumberFormat('zh-CN', { maximumFractionDigits: digits }).format(value)
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
