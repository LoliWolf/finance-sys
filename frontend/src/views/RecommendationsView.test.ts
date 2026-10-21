import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import RecommendationsView from './RecommendationsView.vue'

const mocks = vi.hoisted(() => ({ recommendations: vi.fn(), recommendationExport: vi.fn(), push: vi.fn() }))
vi.mock('../api/client', () => ({ api: mocks }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: mocks.push }) }))

function item(id: number) {
  return { recommendation_event_id: id, blogger_id: 1, blogger_name: '研究员甲', institution: '机构', ts_code: '600001.SH', symbol: '600001', security_name: '示例标的', asset_type: 'A_SHARE', market: 'SH', sector_type: '', direction: 'SHORT', recommend_date: '2026-09-01T00:00:00Z', thesis: `完整观点 ${id} ` + '这是不能截断的原始观点摘要。'.repeat(12), windows: [5,10,30,90].map((days) => ({ window_days: days, status: days === 5 ? 'READY' : 'PENDING', return_ratio: days === 5 ? -0.2 : null, reason_code: '', reason_message: '' })) }
}
function report() {
  return { generated_at: '2026-09-12T03:00:00Z', data_as_of: '2026-09-11T00:00:00Z', quote_source: 'TUSHARE', calc_version: 'v3', filters: {}, total: 60, max_items: 2000, blogger_count: 1, security_count: 1, windows: [5,10,30,90], summary: [{window_days:5,sample_count:60,evaluated_count:60,pending_count:0,unavailable_count:0,rise_count:0,rise_rate:0,avg_return_ratio:-0.2}], bloggers: [], items: Array.from({length:60},(_,i)=>item(i+1)) }
}

describe('recommendation ledger export', () => {
  const wrappers: VueWrapper[] = []
  function render() {
    const wrapper = mount(RecommendationsView, { attachTo: document.body })
    wrappers.push(wrapper)
    return wrapper
  }
  beforeEach(() => {
    vi.resetAllMocks()
    document.title = '原页面标题'
    mocks.recommendations.mockResolvedValue({total:60,page:1,page_size:50,total_pages:2,items:Array.from({length:50},(_,i)=>item(i+1))})
    mocks.recommendationExport.mockResolvedValue(report())
    Object.defineProperty(window, 'print', { configurable: true, value: vi.fn() })
  })
  afterEach(() => {
    for (const wrapper of wrappers.splice(0)) wrapper.unmount()
    window.dispatchEvent(new Event('afterprint'))
    document.body.classList.remove('document-report-printing')
  })

  it('fetches every matched row, prints full thesis, then restores the screen', async () => {
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="export-recommendations"]').trigger('click')
    await flushPromises()
    expect(mocks.recommendationExport).toHaveBeenCalledOnce()
    expect(mocks.recommendationExport.mock.calls[0][0]).not.toHaveProperty('offset')
    expect(mocks.recommendationExport.mock.calls[0][0]).not.toHaveProperty('limit')
    expect(window.print).toHaveBeenCalledOnce()
    expect(wrapper.findAll('.report-export-table tbody tr')).toHaveLength(60)
    expect(wrapper.get('.recommendation-export-report').text()).toContain(item(60).thesis)
    expect(wrapper.get('.recommendation-export-report').text()).toContain('-20.0%')
    expect(wrapper.get('.recommendation-export-report').text()).toContain('上涨占比')
    expect(document.body.classList.contains('document-report-printing')).toBe(true)
    window.dispatchEvent(new Event('afterprint'))
    await flushPromises()
    expect(document.title).toBe('原页面标题')
    expect(document.body.classList.contains('document-report-printing')).toBe(false)
    expect(wrapper.find('.recommendation-export-report').exists()).toBe(false)
    expect(wrapper.get('[data-testid="export-recommendations"]').attributes('disabled')).toBeUndefined()
  })

  it('requires edited filters to be applied before exporting them', async () => {
    const wrapper = render()
    await flushPromises()
    await wrapper.get('input[placeholder="输入全部或部分名字"]').setValue(' Alice ')
    expect(wrapper.get('[data-testid="export-recommendations"]').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('请先点击“筛选”')
    await wrapper.get('.filter-bar .button.primary').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="export-recommendations"]').trigger('click')
    await flushPromises()
    expect(mocks.recommendationExport.mock.calls[0][0].blogger_name).toBe('Alice')
  })

  it('does not print cached or partial data after an export error', async () => {
    mocks.recommendationExport.mockRejectedValueOnce(new Error('超过单次 2000 条限制'))
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="export-recommendations"]').trigger('click')
    await flushPromises()
    expect(window.print).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('超过单次 2000 条限制')
    mocks.recommendationExport.mockResolvedValueOnce({...report(),items:[item(1)]})
    await wrapper.get('[data-testid="export-recommendations"]').trigger('click')
    await flushPromises()
    expect(window.print).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('不完整')
  })

  it('cancels an in-flight export without later opening print', async () => {
    let complete!: (value: unknown) => void
    mocks.recommendationExport.mockImplementation(() => new Promise((resolve) => { complete = resolve }))
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="export-recommendations"]').trigger('click')
    const signal = mocks.recommendationExport.mock.calls[0][1] as AbortSignal
    await wrapper.get('[data-testid="cancel-export"]').trigger('click')
    expect(signal.aborted).toBe(true)
    complete(report())
    await flushPromises()
    expect(window.print).not.toHaveBeenCalled()
    expect(wrapper.find('.error-banner').exists()).toBe(false)
  })

  it('aborts the request on navigation/unmount', async () => {
    mocks.recommendationExport.mockImplementation(() => new Promise(() => {}))
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="export-recommendations"]').trigger('click')
    const signal = mocks.recommendationExport.mock.calls[0][1] as AbortSignal
    wrapper.unmount()
    wrappers.splice(wrappers.indexOf(wrapper),1)
    expect(signal.aborted).toBe(true)
    expect(window.print).not.toHaveBeenCalled()
  })

  it('restores state if the print API throws', async () => {
    vi.mocked(window.print).mockImplementation(() => { throw new Error('打印不可用') })
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="export-recommendations"]').trigger('click')
    await flushPromises()
    expect(document.title).toBe('原页面标题')
    expect(document.body.classList.contains('document-report-printing')).toBe(false)
    expect(wrapper.text()).toContain('打印不可用')
  })

  it('does not turn source HTML into executable markup', async () => {
    const malicious = report()
    malicious.items[0].thesis = '<img src=x onerror="alert(1)">'
    mocks.recommendationExport.mockResolvedValue(malicious)
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="export-recommendations"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('.recommendation-export-report').text()).toContain(malicious.items[0].thesis)
    expect(wrapper.find('.recommendation-export-report img').exists()).toBe(false)
  })

  it('disables export for empty results', async () => {
    mocks.recommendations.mockResolvedValue({total:0,page:1,page_size:50,total_pages:0,items:[]})
    const wrapper = render()
    await flushPromises()
    expect(wrapper.get('[data-testid="export-recommendations"]').attributes('disabled')).toBeDefined()
  })
})
