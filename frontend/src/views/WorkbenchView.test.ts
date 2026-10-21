import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import WorkbenchView from './WorkbenchView.vue'

const apiMocks = vi.hoisted(() => ({
  uploadDocument: vi.fn(),
  documents: vi.fn(),
  evaluationRuns: vi.fn(),
  createEvaluationRun: vi.fn(),
}))

vi.mock('../api/client', () => ({ api: apiMocks }))

describe('WorkbenchView upload form', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.documents.mockResolvedValue([])
    apiMocks.evaluationRuns.mockResolvedValue({ items: [] })
    apiMocks.uploadDocument.mockResolvedValue({ document: { id: 1 }, plans: [] })
  })

  it('submits the explicit author and clears document-specific fields after success', async () => {
    const wrapper = mount(WorkbenchView)
    await flushPromises()

    const fileInput = wrapper.get<HTMLInputElement>('[data-testid="upload-file"]')
    const file = new File(['report'], 'second-report.pdf', { type: 'application/pdf' })
    Object.defineProperty(fileInput.element, 'files', {
      configurable: true,
      value: [file],
    })
    await fileInput.trigger('change')
    await wrapper.get('[data-testid="upload-title"]').setValue('Second report')
    await wrapper.get('[data-testid="upload-author"]').setValue('张豪杰')
    await wrapper.get('[data-testid="upload-institution"]').setValue('开源证券')

    await wrapper.get('button.button.primary.full').trigger('click')
    await flushPromises()

    const form = apiMocks.uploadDocument.mock.calls[0][0] as FormData
    expect(form.get('author')).toBe('张豪杰')
    expect(form.get('title')).toBe('Second report')
    expect(wrapper.get<HTMLInputElement>('[data-testid="upload-author"]').element.value).toBe('')
    expect(wrapper.get<HTMLInputElement>('[data-testid="upload-title"]').element.value).toBe('')
    expect(wrapper.get<HTMLInputElement>('[data-testid="upload-institution"]').element.value).toBe('')
    expect(fileInput.element.value).toBe('')
    expect(wrapper.text()).toContain('选择一份测试研报')
  })
})
