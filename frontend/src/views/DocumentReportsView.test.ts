import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import DocumentReportsView from './DocumentReportsView.vue'

const routerPush = vi.hoisted(() => vi.fn())
const apiMocks = vi.hoisted(() => ({
  documentReports: vi.fn(),
  createDocumentEvaluationRun: vi.fn(),
  evaluationRun: vi.fn(),
}))

vi.mock('vue-router', () => ({ useRouter: () => ({ push: routerPush }) }))
vi.mock('../api/client', () => ({ api: apiMocks }))

describe('DocumentReportsView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.documentReports.mockResolvedValue({
      total: 1,
      page: 1,
      page_size: 50,
      total_pages: 1,
      data_as_of: '2026-09-04T00:00:00Z',
      items: [{
        document_id: 7,
        author: '研究员甲',
        institution: '示例机构',
        title: '历史研报',
        file_name: 'report.pdf',
        status: 'PLANNED',
        config_version: 21,
        created_at: '2026-09-05T01:00:00Z',
        updated_at: '2026-09-05T01:00:00Z',
        recommend_date_from: '2024-01-02T00:00:00Z',
        recommend_date_to: '2024-01-02T00:00:00Z',
        recommendation_count: 3,
        blogger_count: 1,
        untrackable_count: 0,
        expected_metric_count: 12,
        ready_metric_count: 0,
        pending_metric_count: 0,
        incomplete_metric_count: 0,
        missing_metric_count: 12,
        report_status: 'NEEDS_EVALUATION',
      }],
    })
    apiMocks.createDocumentEvaluationRun.mockResolvedValue({ run_id: 9, status: 'QUEUED', run_type: 'MANUAL', message: '', document_count: 1 })
    apiMocks.evaluationRun.mockResolvedValue({ id: 9, status: 'SUCCEEDED', target_event_count: 3, evaluated_event_count: 3 })
  })

  it('lists historical documents and submits selected documents for evaluation', async () => {
    const wrapper = mount(DocumentReportsView)
    await flushPromises()

    expect(wrapper.text()).toContain('历史研报')
    expect(wrapper.text()).toContain('待补算')

    await wrapper.get('input[aria-label="选择文档 7"]').trigger('change')
    await wrapper.get('.bulk-action-bar .button.primary').trigger('click')
    await flushPromises()

    expect(apiMocks.createDocumentEvaluationRun).toHaveBeenCalledWith([7], false)
    expect(apiMocks.evaluationRun).toHaveBeenCalledWith(9)
    expect(wrapper.text()).toContain('补算任务 #9 已完成')
  })

  it('opens the document report from its row', async () => {
    const wrapper = mount(DocumentReportsView)
    await flushPromises()

    await wrapper.get('tbody tr').trigger('click')

    expect(routerPush).toHaveBeenCalledWith('/document-reports/7')
  })
})
