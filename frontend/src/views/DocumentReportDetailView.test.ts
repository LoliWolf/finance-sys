import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import DocumentReportDetailView from './DocumentReportDetailView.vue'

const routerPush = vi.hoisted(() => vi.fn())
const apiMocks = vi.hoisted(() => ({
  documentReport: vi.fn(),
  createDocumentEvaluationRun: vi.fn(),
  evaluationRun: vi.fn(),
}))

vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { id: '7' } }),
  useRouter: () => ({ push: routerPush }),
}))
vi.mock('../api/client', () => ({ api: apiMocks }))

const readyWindow = (windowDays: number) => ({
  window_days: windowDays,
  status: 'READY',
  reason_code: '',
  reason_message: '',
  entry_date: '2024-01-03T00:00:00Z',
  entry_price: 10,
  exit_date: '2024-01-10T00:00:00Z',
  exit_close_price: 11,
  direction_return_ratio: 0.1,
  max_favorable_return_ratio: 0.15,
  max_adverse_return_ratio: -0.03,
  max_drawdown_ratio: -0.04,
  win_flag: true,
  expected_quote_count: windowDays,
  actual_quote_count: windowDays,
  missing_quote_count: 0,
  calc_version: 'v2',
  calculated_at: '2026-09-04T04:00:00Z',
  outdated: false,
})

function reportFixture() {
  const windows = [5, 10, 30, 90]
  const windowSummaries = windows.map((windowDays) => ({
    window_days: windowDays,
    sample_count: 1,
    evaluated_count: 1,
    pending_count: 0,
    incomplete_count: 0,
    win_count: 1,
    win_rate: 1,
    avg_return_ratio: 0.1,
    median_return_ratio: 0.1,
    best_return_ratio: 0.1,
    worst_return_ratio: 0.1,
    avg_max_favorable_return_ratio: 0.15,
    avg_max_adverse_return_ratio: -0.03,
    avg_max_drawdown_ratio: -0.04,
  }))
  const current = { sample_count: 1, evaluated_count: 1, pending_count: 0, incomplete_count: 0, win_count: 1, win_rate: 1, avg_return_ratio: 0.2, median_return_ratio: 0.2, best_return_ratio: 0.2, worst_return_ratio: 0.2 }
  return {
    generated_at: '2026-09-05T02:00:00Z',
    data_as_of: '2026-09-04T00:00:00Z',
    quote_source: 'TUSHARE',
    calc_version: 'v2',
    windows,
    document: { document_id: 7, author: '研究员甲', institution: '示例机构', title: '历史研报', file_name: 'report.pdf', status: 'PLANNED', config_version: 21, created_at: '2026-09-05T01:00:00Z', updated_at: '2026-09-05T01:00:00Z' },
    summary: { recommendation_count: 1, blogger_count: 1, untrackable_count: 0, windows: windowSummaries, current },
    bloggers: [{ blogger_id: 3, blogger_name: '研究员甲', institution: '示例机构', recommendation_count: 1, recommendation_event_ids: [99], windows: windowSummaries, current }],
    recommendations: [{
      recommendation_event_id: 99,
      blogger_id: 3,
      blogger_name: '研究员甲',
      institution: '示例机构',
      ts_code: '600000.SH',
      symbol: '600000',
      security_name: '浦发银行',
      asset_type: 'A_SHARE',
      market: 'SH',
      sector_type: '',
      direction: 'LONG',
      recommend_date: '2024-01-02T00:00:00Z',
      reference_price: 10,
      confidence: 0.9,
      recommendation_status: 'ACTIVE',
      thesis: '基本面改善。',
      evidence: [{ chunk_index: 1, text: '原文证据' }],
      windows: windows.map(readyWindow),
      current: { status: 'READY', reason_code: '', reason_message: '', entry_date: '2024-01-03T00:00:00Z', entry_price: 10, latest_trade_date: '2026-09-04T00:00:00Z', latest_close_price: 12, direction_return_ratio: 0.2, win_flag: true },
    }],
    untrackable_targets: [],
    methodology: { entry_price_rule: 'NEXT_TRADING_DAY_OPEN', base_price_rule: 'RECOMMEND_DATE_CLOSE', win_threshold_ratio: 0, min_quote_coverage_ratio: 1 },
  }
}

describe('DocumentReportDetailView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.documentReport.mockResolvedValue(reportFixture())
    Object.defineProperty(window, 'print', { configurable: true, value: vi.fn() })
  })

  it('renders recommendation details and blogger grouping', async () => {
    const wrapper = mount(DocumentReportDetailView)
    await flushPromises()

    expect(wrapper.text()).toContain('历史研报')
    expect(wrapper.text()).toContain('浦发银行')
    expect(wrapper.text()).toContain('原文证据')

    const groupButton = wrapper.findAll('.report-view-switch button')[1]
    await groupButton.trigger('click')
    expect(wrapper.text()).toContain('仅统计本文推荐')
  })

  it('reloads the current report before opening the print dialog', async () => {
    const wrapper = mount(DocumentReportDetailView)
    await flushPromises()

    const exportButton = wrapper.findAll('.report-toolbar .button').find((button) => button.text().includes('导出当前版'))
    expect(exportButton).toBeTruthy()
    await exportButton!.trigger('click')
    await flushPromises()

    expect(apiMocks.documentReport).toHaveBeenCalledTimes(2)
    expect(window.print).toHaveBeenCalledOnce()
  })

  it('does not print stale data when the pre-export refresh fails', async () => {
    const wrapper = mount(DocumentReportDetailView)
    await flushPromises()
    apiMocks.documentReport.mockRejectedValueOnce(new Error('刷新失败'))

    const exportButton = wrapper.findAll('.report-toolbar .button').find((button) => button.text().includes('导出当前版'))
    await exportButton!.trigger('click')
    await flushPromises()

    expect(window.print).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('刷新失败')
  })
})
