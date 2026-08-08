import { describe, expect, it } from 'vitest'
import { assetTypeLabel, formatDate, formatPercent, marketLabel, returnTone, sectorTypeLabel, truncate } from './format'

describe('format utilities', () => {
  it('formats ratios as percentages', () => {
    expect(formatPercent(0.1234)).toBe('12.3%')
    expect(formatPercent(-0.05, 2)).toBe('-5.00%')
    expect(formatPercent(null)).toBe('—')
  })

  it('keeps API dates stable across timezones', () => {
    expect(formatDate('2026-02-03T00:00:00Z')).toBe('2026-02-03')
  })

  it('assigns deterministic return tones', () => {
    expect(returnTone(0.1)).toBe('positive')
    expect(returnTone(-0.1)).toBe('negative')
    expect(returnTone(null)).toBe('muted')
  })

  it('truncates long evidence without changing short text', () => {
    expect(truncate('简短观点', 10)).toBe('简短观点')
    expect(truncate('这是一段足够长的观点', 5)).toBe('这是一段足…')
  })

  it('labels A shares and Eastmoney sectors consistently', () => {
    expect(assetTypeLabel('A_SHARE')).toBe('A 股')
    expect(assetTypeLabel('SECTOR')).toBe('板块指数')
    expect(marketLabel('DC')).toBe('东方财富板块')
    expect(sectorTypeLabel('INDUSTRY')).toBe('行业板块')
  })
})
