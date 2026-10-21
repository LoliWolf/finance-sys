import { describe, expect, it } from 'vitest'
import { buildPageReport, beijingTime } from './buildPageReport'
import type { PageExportSnapshot } from './types'

const base = { generated_at:'2026-09-12T02:00:00Z',max_items:2000,data_as_of:'2026-09-11',quote_source:'TUSHARE',filters:{window_days:'30'} }
const ranking = {rank:1,blogger_id:1,blogger_name:'研究员甲',institution:'机构',sample_count:3,evaluated_count:0,pending_count:3,incomplete_count:0,win_count:0,win_rate:0,avg_return_ratio:0,median_return_ratio:0,best_return_ratio:0,worst_return_ratio:0,avg_max_drawdown_ratio:0,performance_score:0}
const summary = {window_days:30,...ranking}
export function snapshot(kind:PageExportSnapshot['kind']):PageExportSnapshot {
  return {...base,kind,
    dashboard:{window_days:30,overview:{total_bloggers:1,evaluated_recommendations:0,pending_recommendations:3,incomplete_recommendations:0,average_win_rate:0,average_return_ratio:0},items:[ranking]},
    blogger:{blogger_id:1,blogger_name:'研究员甲',institution:'机构',windows:[summary]},
    timeseries:{blogger_id:1,window_days:30,items:[{...summary,period:'2026-09'}]},
    recommendations:{total:1,window_days:30,items:[{recommendation_event_id:1,blogger_id:1,blogger_name:'研究员甲',security_name:'测试证券',symbol:'600000.SH',ts_code:'600000.SH',direction:'SHORT',status:'READY',recommend_date:'2026-09-01',entry_date:'2026-09-02',exit_date:'2026-09-10',entry_price:10,exit_close_price:9,thesis:'完整观点'.repeat(100),direction_return_ratio:0.1,max_favorable_return_ratio:0.2,max_adverse_return_ratio:-0.05,max_drawdown_ratio:-0.1}]},
    securities:{window_days:30,items:[{...ranking,ts_code:'600000.SH',symbol:'600000',security_name:'测试证券',asset_type:'A_SHARE',market:'SH',recommendation_count:3,blogger_count:1}]},
    recommendation:{recommendation:{recommendation_event_id:1,blogger_name:'研究员甲',symbol:'600000.SH',market:'SH',asset_type:'A_SHARE',direction:'SHORT',recommend_date:'2026-09-01',thesis:'完整观点',confidence:0.8,evidence:[{chunk_index:1,text:'<img src=x onerror=alert(1)>'}],document_title:'原始文章',document_file_name:'test.pdf',source_document_id:2},metrics:[]},
    prices:{security_name:'测试证券',ts_code:'600000.SH',items:[{trade_date:'2026-09-01',open_price:10,close_price:11,high_price:12,low_price:9,volume:100}],markers:[]},
    documents:{total:1,items:[{document_id:1,title:'完整文档标题',file_name:'test.pdf',author:'作者',institution:'机构',status:'FAILED',report_status:'FAILED',recommendation_count:0,blogger_count:0,untrackable_count:0,ready_metric_count:0,expected_metric_count:0,pending_metric_count:0,missing_metric_count:0,incomplete_metric_count:0,created_at:'2026-09-01T00:00:00Z'}]},
    recent_documents:[],runs:[],
    trading:{trade_date:'2026-09-11',runtime:{environment:'SIMULATION',config_version:1},account:null,positions:[],fills:[],orders:[],position_cycles:[],daily_summary:{fill_count:0,buy_amount:'0',buy_count:0,buy_volume:0,sell_amount:'0',sell_count:0,sell_volume:0,net_cash_flow:'0',commission:'0'}},
  // Deliberately omit unrelated API fields; these are the report builder's inputs.
  } as unknown as PageExportSnapshot
}

describe('portable page report builders',()=>{
  it.each(['dashboard','blogger','recommendation','securities','documents','workbench','trading'] as const)('builds a %s report with scope and methodology',(kind)=>{
    const report=buildPageReport(snapshot(kind))
    expect(report.title).toBeTruthy();expect(report.generatedAt).toContain('10:00:00');expect(report.sections.length).toBeGreaterThan(0)
    expect(report.statements.join('')).toContain('不执行模型分析')
  })
  it('does not invent a zero return for unevaluable summaries',()=>{
    const report=buildPageReport(snapshot('dashboard'))
    expect(report.metrics.find(m=>m.label==='总体胜率')?.value).toBe('—')
    expect(report.sections[0].chart?.points[0].value).toBeNull()
    expect(report.sections[1].rows?.[0][4]).toBe('—')
  })
  it('keeps full thesis and direction-return semantics',()=>{
    const source=snapshot('blogger'),report=buildPageReport(source)
    const rows=report.sections.at(-1)?.rows
    expect(rows?.[0][4]).toBe('10.0%')
    expect(rows?.[0][7]).toContain(source.recommendations!.items[0].thesis)
  })
  it('rejects partial lists rather than printing incomplete reports',()=>{
    const source=snapshot('blogger');source.recommendations!.total=2
    expect(()=>buildPageReport(source)).toThrow('不完整')
    const documents=snapshot('documents');documents.documents!.total=2
    expect(()=>buildPageReport(documents)).toThrow('不完整')
  })
  it('distinguishes current account snapshots from the selected historical trade date',()=>{
    const source=snapshot('trading')
    source.trading!.account={account_id:'secret-12345678-account',account_name:'账户甲',nav:'1234.56',balance:'1234.56',available_cash:'1000',frozen_cash:'0',market_value:'234.56',position_ratio:'0.19',floating_pnl:'-20',cumulative_pnl:'',cumulative_commission:'3',snapshot_at:'2026-09-12T01:00:00Z',snapshot_stale:true,snapshot_max_age_seconds:30} as NonNullable<PageExportSnapshot['trading']>['account']
    const result=buildPageReport(source),text=JSON.stringify(result)
    expect(result.subtitle).toContain('不代表该历史日')
    expect(text).toContain('2026-09-11');expect(text).toContain('09:00:00');expect(text).toContain('快照已过期')
    expect(result.metrics.find(i=>i.label==='累计盈亏')?.value).toBe('—')
    expect(text).not.toContain('secret-12345678-account')
    expect(text).toContain('净现金流不等于收益')
  })
  it('uses explicit Beijing timestamps',()=>{
    expect(beijingTime('2026-09-11T23:00:00Z')).toContain('2026/09/12 07:00:00')
    expect(beijingTime('bad')).toBe('—')
  })
})
