import type { PageExportSnapshot, PrintablePageReport, ReportSection } from './types'
import type { RecommendationPerformanceList, WindowSummary } from '../api/types'
import { assetTypeLabel, marketLabel, formatDate, formatNumber, formatPercent, formatCurrency } from '../utils/format'

const statusLabels: Record<string,string> = { READY:'已有结果', PENDING:'未到期', INCOMPLETE:'行情不全', NO_SECURITY:'未识别', FAILED:'失败', UNSUPPORTED:'暂不支持', PLANNED:'分析完成', PARSED:'已解析', INGESTED:'已入库', ANALYZING:'分析中', INVALID:'无有效推荐', EMPTY:'无推荐', NEEDS_EVALUATION:'待补算', PARTIAL:'部分可用', QUEUED:'排队中', RUNNING:'运行中', SUCCEEDED:'成功', OPEN:'持有中', CLOSED:'已平仓', EXIT_PENDING:'待退出', BUY:'买入', SELL:'卖出', FILLED:'已成交', PARTIALLY_FILLED:'部分成交', REJECTED:'已拒绝', CANCELED:'已撤单' }
const state = (v: string | null | undefined) => v ? statusLabels[v] || v : '—'
const number = (v: unknown) => typeof v==='number' && Number.isFinite(v) ? v : null
const pct = (v: number | null | undefined, count = 1) => formatPercent(count>0 ? number(v) : null)
const num = (v: number | null | undefined) => formatNumber(number(v))
const currency = (v: string | number | null | undefined, digits=2) => v==='' || v==null || !Number.isFinite(Number(v)) ? '—' : formatCurrency(v,digits)
const decimalPct = (v: string | null | undefined) => pct(v==null || v==='' ? null : Number(v))
const direction = (v:string) => v==='SHORT'?'看空':v==='LONG'?'看多':state(v)
export const beijingTime = (v?:string|null) => v && Number.isFinite(Date.parse(v)) ? new Intl.DateTimeFormat('zh-CN',{timeZone:'Asia/Shanghai',year:'numeric',month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',second:'2-digit',hour12:false}).format(new Date(v)) : '—'
const requireData = <T>(v:T|null|undefined):T => { if(v==null) throw new Error('报告数据不完整，请刷新后重试'); return v }

function table(title:string,columns:string[],rows:string[][],description?:string):ReportSection { return {title,columns,rows,description} }
function summaryRows(items:WindowSummary[]) {
  return items.map(i=>[`${i.window_days} 交易日`,`${i.evaluated_count} / ${i.sample_count}`,pct(i.win_rate,i.evaluated_count),pct(i.avg_return_ratio,i.evaluated_count),pct(i.median_return_ratio,i.evaluated_count),pct(i.best_return_ratio,i.evaluated_count)+' / '+pct(i.worst_return_ratio,i.evaluated_count),pct(i.avg_max_drawdown_ratio,i.evaluated_count),`${i.pending_count} / ${i.incomplete_count}`])
}
function performanceSection(list:RecommendationPerformanceList):ReportSection {
  if((list.items || []).length !== list.total) throw new Error('推荐明细不完整，已停止导出')
  return { ...table(`推荐明细 · ${list.total} 条`,['推荐日 / ID','博主 / 标的','方向 / 状态','入场 → 退出','方向收益','最大有利 / 不利','最大回撤','完整观点摘要'],(list.items||[]).map(i=>[`${formatDate(i.recommend_date)}\n#${i.recommendation_event_id}`,`${i.blogger_name}\n${i.security_name || i.symbol}\n${i.ts_code || i.symbol}`,`${direction(i.direction)}\n${state(i.status)}`,`${formatDate(i.entry_date)} @ ${num(i.entry_price)}\n${formatDate(i.exit_date)} @ ${num(i.exit_close_price)}`,pct(i.direction_return_ratio,i.status==='READY'?1:0),`${pct(i.max_favorable_return_ratio,i.status==='READY'?1:0)} / ${pct(i.max_adverse_return_ratio,i.status==='READY'?1:0)}`,pct(i.max_drawdown_ratio,i.status==='READY'?1:0),`${i.thesis || '未记录'}${i.reason_message?'\n评估说明：'+i.reason_message:''}`]),`${list.window_days} 个交易日窗口的全部匹配指标；复用该页面的指标事实口径，不与“推荐明细”页原始涨跌幅混用。`),widths:[10,17,9,14,8,11,8,23] }
}
function filterEntries(snapshot:PageExportSnapshot) {
  const labels:Record<string,string>={window_days:'观察窗口（交易日）',date_from:snapshot.kind==='documents'?'上传开始':'推荐开始',date_to:snapshot.kind==='documents'?'上传结束':'推荐结束',market:'市场',asset_type:'资产',direction:'方向',status:'状态',blogger_id:'博主 ID',min_sample_count:'最少可评估样本',sort:'排序',search:'标的关键词（全量匹配）',selected_ts_code:'选中标的',query:'文档关键词',event_id:'推荐事件 ID',scope:'记录范围'}
  return Object.entries(snapshot.filters||{}).map(([key,value])=>({label:labels[key]||key,value:key==='market'?marketLabel(value):key==='asset_type'?assetTypeLabel(value):key==='direction'?direction(value):key==='status'?state(value):key==='sort'?({win_rate:'胜率',avg_return:'平均收益',sample_count:'可评估样本数',performance_score:'综合分'}[value]||value):value}))
}

export function buildPageReport(snapshot:PageExportSnapshot):PrintablePageReport {
  if(!Number.isFinite(Date.parse(snapshot.generated_at))) throw new Error('报告生成时间无效')
  const titles = {dashboard:'博主表现总览报告',blogger:'博主研究档案',recommendation:'单条推荐复盘报告',securities:'标的热度与表现报告',documents:'文档档案清单',workbench:'研究工作台运行报告',trading:'模拟交易账务报告'}
  const result:PrintablePageReport={title:titles[snapshot.kind],subtitle:'基于已有事实和确定性统计生成的本次独立报告。',generatedAt:beijingTime(snapshot.generated_at),meta:[],filters:filterEntries(snapshot),metrics:[],sections:[],statements:['本次导出只读取已保存数据，不执行模型分析、行情补采、指标重算或交易操作。','本报告整理研究事实与历史结果，不构成投资建议；历史表现不代表未来。']}
  if(snapshot.quote_source) result.meta.push(`行情来源 ${snapshot.quote_source}`)
  if(snapshot.kind!=='trading' && snapshot.kind!=='workbench') {
    result.meta.push(`本地行情截至 ${formatDate(snapshot.data_as_of)}`)
    result.statements.unshift('研究表现使用页面已有的方向收益与胜率口径；未到期、缺行情等不可评估样本不进入胜率分母，无有效样本显示“—”。方向收益不是账户实际收益。')
    result.statements.push(`当前计算配置：${snapshot.calc_version||'未标注'}。行情截止日是本地市场数据的最近日期，并不保证每个标的的行情或指标都已更新至此日。`)
  }
  switch(snapshot.kind) {
    case 'dashboard': {
      const data=requireData(snapshot.dashboard), o=data.overview
      result.subtitle=`${data.window_days} 交易日窗口 · 当前筛选下全部 ${data.items.length} 位达标博主，非仅页面前 100 位。`
      result.metrics=[{label:'覆盖博主',value:num(o.total_bloggers),note:'最少样本门槛前'},{label:'达标博主',value:num(data.items.length)},{label:'可评估推荐',value:num(o.evaluated_recommendations)},{label:'总体胜率',value:pct(o.average_win_rate,o.evaluated_recommendations)},{label:'平均方向收益',value:pct(o.average_return_ratio,o.evaluated_recommendations)}]
      result.sections.push({title:'领先博主比较',chart:{type:'bar',title:'当前排序前 10 位 · 胜率',percent:true,points:data.items.slice(0,10).map(i=>({label:i.blogger_name,value:i.evaluated_count?number(i.win_rate):null}))}})
      result.sections.push(table('完整博主表现榜',['排名 / 博主','机构','综合分','可评估 / 总样本','胜率','平均 / 中位收益','最佳 / 最差','平均回撤','未到期 / 缺口'],data.items.map(i=>[`${i.rank}. ${i.blogger_name}`,i.institution||'独立研究者',num(i.performance_score),`${i.evaluated_count} / ${i.sample_count}`,pct(i.win_rate,i.evaluated_count),`${pct(i.avg_return_ratio,i.evaluated_count)} / ${pct(i.median_return_ratio,i.evaluated_count)}`,`${pct(i.best_return_ratio,i.evaluated_count)} / ${pct(i.worst_return_ratio,i.evaluated_count)}`,pct(i.avg_max_drawdown_ratio,i.evaluated_count),`${i.pending_count} / ${i.incomplete_count}`]),`总览统计覆盖门槛前全部样本，榜单仅列达标博主。总计 ${o.pending_recommendations} 条未到期，${o.incomplete_recommendations} 条不完整。`))
      break
    }
    case 'blogger': {
      const data=requireData(snapshot.blogger), trend=requireData(snapshot.timeseries), list=requireData(snapshot.recommendations)
      result.title=`${data.blogger_name} · 博主研究档案`
      result.subtitle=`${data.institution || '独立研究者'} · 四窗口对照、月度趋势与所选窗口全部推荐。`
      result.sections.push(table('四窗口对照',['窗口','可评估 / 总样本','胜率','平均收益','中位收益','最佳 / 最差','平均回撤','未到期 / 缺口'],summaryRows(data.windows)))
      result.sections.push({title:'月度表现趋势',chart:{type:'line',title:`${trend.window_days} 交易日窗口 · 月均方向收益`,percent:true,points:trend.items.map(i=>({label:i.period,value:i.evaluated_count?number(i.avg_return_ratio):null}))}})
      result.sections.push(table('月度指标',['月份','总样本','可评估','未到期','胜率','平均方向收益'],trend.items.map(i=>[i.period,num(i.sample_count),num(i.evaluated_count),num(i.pending_count),pct(i.win_rate,i.evaluated_count),pct(i.avg_return_ratio,i.evaluated_count)])))
      result.sections.push(performanceSection(list)); break
    }
    case 'securities': {
      const data=requireData(snapshot.securities)
      result.subtitle=`${data.window_days} 交易日窗口 · 全部 ${data.items.length} 个达标标的；关键词在全量榜单匹配，不限页面前 100 条。`
      result.metrics=[{label:'达标标的',value:num(data.items.length)},{label:'观察窗口',value:`${data.window_days} 日`},{label:'选中标的',value:snapshot.selected_security?.security_name||'未选择'}]
      result.sections.push({title:'标的热度比较',chart:{type:'bar',title:'当前排序前 12 位 · 推荐次数',points:data.items.slice(0,12).map(i=>({label:i.security_name||i.ts_code,value:i.recommendation_count}))}})
      result.sections.push(table('完整标的榜单',['排名 / 标的','代码 / 类别','推荐 / 博主数','可评估 / 未到期','胜率','平均收益','中位收益','最佳 / 最差'],data.items.map(i=>[`${i.rank}. ${i.security_name||i.symbol}`,`${i.ts_code}\n${assetTypeLabel(i.asset_type)} · ${marketLabel(i.market)}`,`${i.recommendation_count} / ${i.blogger_count}`,`${i.evaluated_count} / ${i.pending_count}`,pct(i.win_rate,i.evaluated_count),pct(i.avg_return_ratio,i.evaluated_count),pct(i.median_return_ratio,i.evaluated_count),`${pct(i.best_return_ratio,i.evaluated_count)} / ${pct(i.worst_return_ratio,i.evaluated_count)}`])))
      if(snapshot.selected_security && snapshot.recommendations) result.sections.push(performanceSection(snapshot.recommendations))
      break
    }
    case 'recommendation': {
      const data=requireData(snapshot.recommendation), prices=requireData(snapshot.prices), r=data.recommendation
      const metrics=[...(data.metrics||[])].sort((a,b)=>a.window_days-b.window_days)
      result.title=`${prices.security_name||r.symbol} · 单条推荐复盘`
      result.subtitle=`${r.blogger_name} · ${formatDate(r.recommend_date)} · ${direction(r.direction)} · #${r.recommendation_event_id}`
      result.meta.push(`${prices.ts_code||r.symbol} · ${marketLabel(r.market)} · ${assetTypeLabel(r.asset_type)}`)
      result.sections.push({title:'原始观点',paragraphs:[r.thesis||'未记录观点',`博主：${r.blogger_name}；机构：${r.institution||'未标注'}；置信度：${pct(r.confidence)}；推荐状态：${state(r.recommendation_status)}`]})
      result.sections.push({title:'推荐前后价格路径',description:'推荐前 5 个、推荐后至多 90 个行情点的收盘价路径；不对缺失行情补零。完整 OHLC 数据见后附表。',chart:{type:'line',title:'收盘价路径',points:prices.items.map(i=>({label:formatDate(i.trade_date),value:number(i.close_price)}))}})
      result.sections.push(table('窗口指标',['窗口 / 来源','状态 / 说明','入场日期 / 价格','退出日期 / 价格','方向收益','最大有利 / 不利','最大回撤','胜负 / 行情覆盖'],metrics.map(m=>[`${m.window_days} 日\n${m.quote_source}`,`${state(m.status)}\n${m.reason_message||''}`,`${formatDate(m.entry_date)}\n${num(m.entry_price)}`,`${formatDate(m.exit_date)}\n${num(m.exit_close_price)}`,pct(m.direction_return_ratio,m.status==='READY'?1:0),`${pct(m.max_favorable_return_ratio,m.status==='READY'?1:0)} / ${pct(m.max_adverse_return_ratio,m.status==='READY'?1:0)}`,pct(m.max_drawdown_ratio,m.status==='READY'?1:0),`${m.status!=='READY'||m.win_flag==null?'—':m.win_flag?'胜':'负'}\n${m.actual_quote_count} / ${m.expected_quote_count}`])))
      result.sections.push({title:'原文证据',paragraphs:r.evidence.length?r.evidence.map(e=>`片段 ${e.chunk_index}\n${e.text}`):['没有单独存储的证据片段。']})
      result.sections.push({title:'来源文档',paragraphs:[r.document_title||r.document_file_name||'未命名文档',`文档 #${r.source_document_id} · 文件 ${r.document_file_name||'—'}`]})
      result.sections.push(table('价格路径标记',['标记','交易日','窗口'],prices.markers.map(m=>[m.label,formatDate(m.trade_date),m.window_days?`${m.window_days} 日`:'—'])))
      result.sections.push(table('价格路径数据',['交易日','开盘','最高','最低','收盘','成交量'],prices.items.map(p=>[formatDate(p.trade_date),num(p.open_price),num(p.high_price),num(p.low_price),num(p.close_price),num(p.volume)])))
      break
    }
    case 'documents': {
      const data=requireData(snapshot.documents)
      if(data.total!==(data.items||[]).length) throw new Error('文档清单不完整，已停止导出')
      result.subtitle=`当前筛选下全部 ${data.total} 篇文档的档案清单；单篇详细追踪报告仍在文档详情中导出。`
      result.metrics=[{label:'文档数',value:num(data.total)},{label:'推荐数',value:num(data.items.reduce((n,i)=>n+i.recommendation_count,0))},{label:'待补算 / 部分可用',value:num(data.items.filter(i=>['NEEDS_EVALUATION','PARTIAL'].includes(i.report_status)).length)}]
      result.sections.push(table('全部文档档案',['文档 ID / 标题 / 文件','作者 / 机构','分析 / 报告状态','推荐 / 博主 / 不可追踪','推荐日期范围','READY / 应有窗口','未到期 / 缺失或不完整','上传时间（北京）'],data.items.map(i=>[`#${i.document_id}\n${i.title||i.file_name}\n${i.file_name}`,`${i.author||'未标注'}\n${i.institution||'—'}`,`${state(i.status)}\n${state(i.report_status)}`,`${i.recommendation_count} / ${i.blogger_count} / ${i.untrackable_count}`,`${formatDate(i.recommend_date_from)}\n至 ${formatDate(i.recommend_date_to)}`,`${i.ready_metric_count} / ${i.expected_metric_count}`,`${i.pending_metric_count} / ${i.missing_metric_count+i.incomplete_metric_count}`,beijingTime(i.created_at)])))
      break
    }
    case 'workbench': {
      const runs=requireData(snapshot.runs),docs=requireData(snapshot.recent_documents)
      result.subtitle='最近 30 条评价任务与最近 20 篇文档的运行记录，不包含上传表单、未提交参数、原始文件或密钥。'
      result.metrics=[{label:'任务记录',value:num(runs.length)},{label:'文档记录',value:num(docs.length)},{label:'最近任务状态',value:state(runs[0]?.status)}]
      result.sections.push(table('最近评价任务',['任务 / 类型','状态','已处理 / 目标事件','窗口指标','未到期','缺口','失败','排队时间（北京）'],runs.map(i=>[`#${i.id}\n${i.run_type}`,state(i.status),`${i.evaluated_event_count} / ${i.target_event_count}`,num(i.window_metric_count),num(i.pending_count),num(i.incomplete_count),num(i.failed_count),beijingTime(i.queued_at)])))
      result.sections.push(table('最近文档',['ID / 标题','文件名','作者 / 机构','分析状态','上传时间（北京）'],docs.map(i=>[`#${i.document_id}\n${i.title||i.file_name}`,i.file_name,`${i.author||'未标注'}\n${i.institution||'—'}`,state(i.status),beijingTime(i.created_at)])))
      break
    }
    case 'trading': {
      const d=requireData(snapshot.trading),a=d.account,s=d.daily_summary
      if(s.fill_count!==d.fills.length) throw new Error('成交明细与汇总不一致，已停止导出')
      result.subtitle='账户及持仓来自最近一次本地账务快照；成交与订单按所选日期导出，不代表该历史日的账户资产快照。'
      result.filters=[{label:'成交 / 订单日期',value:d.trade_date},{label:'持仓周期',value:`最近 ${snapshot.cycle_limit||200} 条，实际 ${d.position_cycles.length} 条`},{label:'账户范围',value:'当前全部持仓与最新账务快照'}]
      result.meta=[`${d.runtime.environment} · 配置 v${d.runtime.config_version}`,`快照时间 ${beijingTime(a?.snapshot_at)}`,a?(a.snapshot_stale?'交易风控快照已过期':'账务快照在风控时效内'):'尚无账户快照']
      if(a) result.metrics=[{label:'账户总资产',value:currency(a.nav),note:`余额 ${currency(a.balance)}`},{label:'可用资金',value:currency(a.available_cash),note:`冻结 ${currency(a.frozen_cash)}`},{label:'持仓市值',value:currency(a.market_value),note:`仓位 ${decimalPct(a.position_ratio)}`},{label:'持仓浮盈亏',value:currency(a.floating_pnl)},{label:'累计盈亏',value:currency(a.cumulative_pnl),note:`累计佣金 ${currency(a.cumulative_commission)}`}]
      result.sections.push({title:'账户与运行状态',paragraphs:[a?`${a.account_name||'东方财富模拟账户'} · 账户 ${a.account_id.length>8?a.account_id.slice(0,4)+'…'+a.account_id.slice(-4):'已脱敏'}；账户状态 ${a.account_state}；佣金状态 ${a.commission_data_status}；风控快照阈值 ${a.snapshot_max_age_seconds} 秒`:'尚无账务快照，不将缺失资产或收益当作零。',`交易总开关：${d.runtime.trading_enabled?'开启':'关闭'}；调度：${d.runtime.scheduler_enabled?'开启':'关闭'}；自动退出：${d.runtime.exit_enabled?'开启':'关闭'}；对账：${d.runtime.reconciliation_enabled?'开启':'关闭'}；熔断：${d.runtime.nacos_kill_switch||d.runtime.runtime_kill_switch?'开启':'关闭'}`]})
      result.sections.push(table('当前全部持仓',['标的 / 代码','持仓 / 可卖','成本 / 最新价','市值','浮盈亏 / 比例','止损 / 止盈','持有日 / 上限','状态'],d.positions.map(i=>[`${i.security_name}\n${i.ts_code||i.eastmoney_symbol}`,`${i.volume} / ${i.available_volume}`,`${currency(i.vwap,3)} / ${currency(i.last_price,3)}`,currency(i.market_value),`${currency(i.floating_pnl)}\n${decimalPct(i.floating_pnl_ratio)}`,`${currency(i.stop_loss_price,3)} / ${currency(i.take_profit_price,3)}`,i.cycle_id?`${i.holding_trade_days} / ${i.max_holding_trade_days}`:'未关联周期',state(i.cycle_status)])))
      result.sections.push(table(`${d.trade_date} 成交汇总`,['买入额 / 笔数 / 数量','卖出额 / 笔数 / 数量','净现金流','佣金','成交回报数'],[[`${currency(s.buy_amount)} / ${s.buy_count} / ${s.buy_volume}`,`${currency(s.sell_amount)} / ${s.sell_count} / ${s.sell_volume}`,currency(s.net_cash_flow),currency(s.commission),num(s.fill_count)]],'仅按真实成交回报统计，净现金流 = 卖出 − 买入 − 佣金；净现金流不等于收益。'))
      result.sections.push(table('当日全部成交',['时间（北京）','方向 / 标的','代码','成交价','数量','成交额','佣金 / 状态','关联订单'],d.fills.map(i=>[beijingTime(i.traded_at),`${state(i.side)}\n${i.security_name}`,i.ts_code,currency(i.price,3),num(i.volume),currency(i.amount),`${currency(i.commission)}\n${state(i.commission_status)}`,`#${i.trading_order_id}\n${state(i.order_status)}`])))
      result.sections.push(table('当日全部订单',['创建时间（北京）','方向 / 标的 / 代码','委托价','委托 / 成交量','成交均价 / 金额','状态','原因'],d.orders.map(i=>[beijingTime(i.created_at),`${state(i.side)}\n${i.security_name}\n${i.ts_code}`,currency(i.limit_price,3),`${i.volume} / ${i.filled_volume}`,`${currency(i.filled_vwap,3)}\n${currency(i.filled_amount)}`,state(i.status),i.error_message||i.error_code||'—'])))
      result.sections.push(table(`最近 ${d.position_cycles.length} 条持仓周期`,['标的 / 状态','建仓日 / 初始数量','成本 / 退出价','止损 / 止盈','持有日 / 上限','退出原因','已实现盈亏 / 比例','来源推荐'],d.position_cycles.map(i=>[`${i.security_name}\n${i.ts_code}\n${state(i.status)}`,`${formatDate(i.entry_trade_date)}\n${i.initial_volume}`,`${currency(i.entry_price,3)} / ${currency(i.exit_price,3)}`,`${currency(i.stop_loss_price,3)} / ${currency(i.take_profit_price,3)}`,`${i.holding_trade_days} / ${i.max_holding_trade_days}`,i.exit_reason||'—',i.realized_pnl==null?'未产生已实现收益':`${currency(i.realized_pnl)}\n${decimalPct(i.realized_pnl_ratio)}`,i.source_recommendation_event_id?`#${i.source_recommendation_event_id}`:'—'])))
      result.statements.unshift('本报告仅反映模拟账户账务，不混入研究推荐的评价收益。已实现盈亏使用现有持仓周期结果；持仓浮盈亏与历史日成交属于不同时间口径。导出不会请求 Windows Bridge 刷新快照。')
      break
    }
  }
  return result
}
