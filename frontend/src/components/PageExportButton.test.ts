import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import PageExportButton from './PageExportButton.vue'

const mocks=vi.hoisted(()=>({pageExport:vi.fn()}))
vi.mock('../api/client',()=>({api:mocks}))
const response=()=>({kind:'workbench',generated_at:'2026-09-12T00:00:00Z',max_items:2000,filters:{scope:'最近记录'},runs:[],recent_documents:[{document_id:1,title:'<img src=x onerror=alert(1)>',file_name:'test.pdf',status:'FAILED',created_at:'2026-09-01T00:00:00Z'}]})

describe('shared page export lifecycle',()=>{
  let wrapper:VueWrapper|undefined
  beforeEach(()=>{vi.resetAllMocks();document.title='研迹';mocks.pageExport.mockResolvedValue(response());Object.defineProperty(window,'print',{configurable:true,value:vi.fn()})})
  afterEach(()=>{wrapper?.unmount();wrapper=undefined;window.dispatchEvent(new Event('afterprint'));document.body.classList.remove('document-report-printing','page-report-printing')})
  const render=(props={})=>wrapper=mount(PageExportButton,{props:{kind:'workbench',...props},attachTo:document.body})
  it('prints an isolated full report and cleans up on afterprint',async()=>{
    const w=render();await w.get('[data-testid="page-export"]').trigger('click');await flushPromises()
    expect(window.print).toHaveBeenCalledOnce();expect(document.body.classList.contains('page-report-printing')).toBe(true)
    const report=document.querySelector('.page-export-report')!
    expect(report.textContent).toContain('<img src=x onerror=alert(1)>');expect(report.querySelector('img')).toBeNull()
    window.dispatchEvent(new Event('afterprint'));await flushPromises()
    expect(document.title).toBe('研迹');expect(document.querySelector('.page-export-report')).toBeNull();expect(document.body.classList.contains('page-report-printing')).toBe(false)
  })
  it.each(['cancel','filter','unmount'])('does not open a late report after %s',async(action)=>{
    let resolve!:(value:unknown)=>void;mocks.pageExport.mockImplementation(()=>new Promise(r=>resolve=r))
    const w=render({query:{window_days:30}});await w.get('[data-testid="page-export"]').trigger('click')
    const signal=mocks.pageExport.mock.calls[0][2] as AbortSignal
    if(action==='cancel')await w.get('[data-testid="page-export-cancel"]').trigger('click')
    if(action==='filter')await w.setProps({query:{window_days:10}})
    if(action==='unmount'){w.unmount();wrapper=undefined}
    expect(signal.aborted).toBe(true);resolve(response());await flushPromises();expect(window.print).not.toHaveBeenCalled()
  })
  it('blocks dirty scopes and shows errors without printing cached data',async()=>{
    const w=render({dirty:true});expect(w.get('button').attributes('disabled')).toBeDefined()
    await w.setProps({dirty:false});mocks.pageExport.mockRejectedValueOnce(new Error('超过 2000 条'))
    await w.get('[data-testid="page-export"]').trigger('click');await flushPromises()
    expect(w.text()).toContain('超过 2000 条');expect(window.print).not.toHaveBeenCalled()
  })
  it('restores the page when print is unsupported',async()=>{
    vi.mocked(window.print).mockImplementation(()=>{throw new Error('打印不可用')})
    const w=render();await w.get('[data-testid="page-export"]').trigger('click');await flushPromises()
    expect(w.text()).toContain('打印不可用');expect(document.title).toBe('研迹');expect(document.body.classList.contains('page-report-printing')).toBe(false)
  })
  it('uses only the read-only report API for trading',async()=>{
    mocks.pageExport.mockRejectedValue(new Error('test-stop'))
    const w=render({kind:'trading',query:{trade_date:'2026-09-11'}});await w.get('[data-testid="page-export"]').trigger('click');await flushPromises()
    expect(mocks.pageExport).toHaveBeenCalledWith('trading',{trade_date:'2026-09-11'},expect.any(AbortSignal))
  })
  it('keeps the report during polling rerenders with equivalent filter values',async()=>{
    const w=render({query:{trade_date:'2026-09-11'}})
    await w.get('[data-testid="page-export"]').trigger('click');await flushPromises()
    await w.setProps({query:{trade_date:'2026-09-11'},disabled:true});await flushPromises()
    expect(document.querySelector('.page-export-report')).not.toBeNull()
    expect(document.body.classList.contains('page-report-printing')).toBe(true)
    expect(w.find('[data-testid="page-export-cancel"]').exists()).toBe(true)
  })
})
