<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ArrowLeft, BookOpenText, Calculator, Download, FileWarning, RefreshCw, UsersRound } from 'lucide-vue-next'
import { api } from '../api/client'
import type { DocumentReport, DocumentReportBloggerGroup, DocumentReportRecommendation, DocumentReportWindowMetric, EvaluationRun } from '../api/types'
import MetricCard from '../components/MetricCard.vue'
import StatePanel from '../components/StatePanel.vue'
import StatusBadge from '../components/StatusBadge.vue'
import { assetTypeLabel, formatDate, formatNumber, formatPercent, marketLabel, returnTone, sectorTypeLabel } from '../utils/format'

const route = useRoute()
const router = useRouter()
const documentID = computed(() => Number(route.params.id))
const loading = ref(true)
const evaluating = ref(false)
const exporting = ref(false)
const error = ref('')
const notice = ref('')
const report = ref<DocumentReport | null>(null)
const activeRun = ref<EvaluationRun | null>(null)
const viewMode = ref<'recommendations' | 'bloggers'>('recommendations')
let pollTimer: number | undefined
let disposed = false

async function load(showLoading = true) {
  if (showLoading) loading.value = true
  error.value = ''
  try {
    report.value = await api.documentReport(documentID.value)
    return true
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : '文档报告加载失败'
    return false
  } finally {
    if (showLoading) loading.value = false
  }
}

async function evaluateDocument() {
  if (evaluating.value) return
  evaluating.value = true
  error.value = ''
  notice.value = ''
  try {
    const response = await api.createDocumentEvaluationRun([documentID.value])
    notice.value = `追踪补算任务 #${response.run_id} 已提交。`
    await watchEvaluationRun(response.run_id)
  } catch (cause) {
    evaluating.value = false
    error.value = cause instanceof Error ? cause.message : '追踪补算提交失败'
  }
}

async function watchEvaluationRun(runID: number) {
  if (disposed) return
  try {
    const run = await api.evaluationRun(runID)
    activeRun.value = run
    if (run.status === 'QUEUED' || run.status === 'RUNNING') {
      pollTimer = window.setTimeout(() => watchEvaluationRun(runID), 1800)
      return
    }
    evaluating.value = false
    notice.value = run.status === 'SUCCEEDED'
      ? `补算任务 #${runID} 已完成，报告已更新。`
      : `补算任务 #${runID} 已结束，状态：${run.status}。`
    await load(false)
  } catch (cause) {
    evaluating.value = false
    error.value = cause instanceof Error ? cause.message : '补算任务状态查询失败'
  }
}

async function exportCurrentReport() {
  if (exporting.value) return
  exporting.value = true
  error.value = ''
  const previousMode = viewMode.value
  const previousTitle = document.title
  let evidenceStates: Array<{ element: HTMLDetailsElement; open: boolean }> = []
  try {
    const refreshed = await load(false)
    if (!refreshed || !report.value) return
    viewMode.value = 'recommendations'
    await nextTick()
    evidenceStates = Array.from(document.querySelectorAll<HTMLDetailsElement>('.document-report-detail details')).map((element) => ({ element, open: element.open }))
    evidenceStates.forEach(({ element }) => { element.open = true })
    const safeTitle = documentTitle.value.replace(/[\\/:*?"<>|]/g, '-').slice(0, 80)
    document.title = `研迹_${safeTitle}_截至${formatDate(report.value.data_as_of)}`
    document.body.classList.add('document-report-printing')
    window.print()
  } finally {
    window.setTimeout(() => {
      document.title = previousTitle
      document.body.classList.remove('document-report-printing')
      evidenceStates.forEach(({ element, open }) => { element.open = open })
      viewMode.value = previousMode
      exporting.value = false
    }, 0)
  }
}

function metricFor(item: DocumentReportRecommendation, windowDays: number) {
  return item.windows.find((metric) => metric.window_days === windowDays)
}

function metricReturn(metric: DocumentReportWindowMetric | undefined) {
  return metric?.status === 'READY' ? metric.direction_return_ratio : null
}

function recommendationsForGroup(group: DocumentReportBloggerGroup) {
  const eventIDs = new Set(group.recommendation_event_ids)
  return report.value?.recommendations.filter((item) => eventIDs.has(item.recommendation_event_id)) || []
}

function instrumentLabel(item: DocumentReportRecommendation) {
  return item.asset_type === 'SECTOR' ? sectorTypeLabel(item.sector_type) : assetTypeLabel(item.asset_type)
}

function fullDateTime(value: string | null | undefined) {
  if (!value) return '—'
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit',
  }).format(new Date(value))
}

function evaluatedValue(value: number | null | undefined, evaluatedCount: number | null | undefined) {
  return (evaluatedCount || 0) > 0 ? value : null
}

const documentTitle = computed(() => report.value?.document.title || report.value?.document.file_name || `文档 #${documentID.value}`)
const current = computed(() => report.value?.summary.current)
const currentUnreadyCount = computed(() => (current.value?.pending_count || 0) + (current.value?.incomplete_count || 0))

onMounted(() => load())
onBeforeUnmount(() => {
  disposed = true
  if (pollTimer !== undefined) window.clearTimeout(pollTimer)
  document.body.classList.remove('document-report-printing')
})
</script>

<template>
  <div class="document-report-detail">
    <div class="report-toolbar no-print">
      <button class="button" type="button" @click="router.push('/document-reports')"><ArrowLeft :size="15" />返回文档列表</button>
      <div>
        <button class="button" type="button" :disabled="loading" @click="load()"><RefreshCw :size="15" />刷新</button>
        <button class="button" type="button" :disabled="evaluating || !report?.summary.recommendation_count" @click="evaluateDocument"><Calculator :size="15" />{{ evaluating ? '正在补算…' : '补算追踪' }}</button>
        <button class="button primary" type="button" :disabled="exporting || !report" @click="exportCurrentReport"><Download :size="15" />{{ exporting ? '准备报告…' : '导出当前版' }}</button>
      </div>
    </div>

    <div v-if="error" class="error-banner no-print">{{ error }}</div>
    <div v-if="notice" class="success-banner no-print">{{ notice }}<span v-if="activeRun && evaluating"> 已处理 {{ activeRun.evaluated_event_count }} / {{ activeRun.target_event_count }} 条推荐。</span></div>
    <StatePanel v-if="loading" loading />

    <template v-if="report">
      <section class="detail-hero report-cover">
        <span class="kicker">Document report #{{ report.document.document_id }}</span>
        <h1>{{ documentTitle }}</h1>
        <p>对指定文档中的推荐事实进行结构化整理，并使用本地日线行情按统一、可复现的规则追踪后续表现。</p>
        <div class="detail-meta">
          <span>{{ report.document.author || '作者未标注' }}</span>
          <span>{{ report.document.institution || '机构未标注' }}</span>
          <span>上传于 {{ fullDateTime(report.document.created_at) }}</span>
          <span>行情截至 {{ formatDate(report.data_as_of) }}</span>
          <span>{{ report.quote_source }} · {{ report.calc_version }}</span>
        </div>
      </section>

      <section class="metric-grid report-summary-grid">
        <MetricCard label="推荐事实" :value="formatNumber(report.summary.recommendation_count, 0)" note="不含已被替代的旧事件" :icon="BookOpenText" tone="accent" />
        <MetricCard label="涉及博主" :value="formatNumber(report.summary.blogger_count, 0)" note="按推荐事件博主分组" :icon="UsersRound" />
        <MetricCard label="当前可评估" :value="formatNumber(current?.evaluated_count, 0)" :note="`共 ${current?.sample_count || 0} 条推荐`" />
        <MetricCard label="截至目前胜率" :value="formatPercent(evaluatedValue(current?.win_rate, current?.evaluated_count))" :note="`${current?.win_count || 0} 条方向收益超过阈值`" :tone="(current?.win_rate || 0) >= .5 ? 'positive' : 'negative'" />
        <MetricCard label="截至目前平均收益" :value="formatPercent(evaluatedValue(current?.avg_return_ratio, current?.evaluated_count))" :note="`${currentUnreadyCount} 条等待或不可评估`" :tone="(current?.avg_return_ratio || 0) >= 0 ? 'positive' : 'negative'" />
      </section>

      <article class="panel report-window-summary">
        <header class="panel-header"><div><h2>本文固定窗口表现</h2><p>每个窗口独立统计，只将 READY 样本纳入胜率和收益率。</p></div><span class="muted">T+1 开盘入场</span></header>
        <div class="table-wrap">
          <table class="data-table">
            <thead><tr><th>窗口</th><th>样本</th><th>可评估</th><th>未到期</th><th>不可评估</th><th>胜率</th><th>平均收益</th><th>中位收益</th><th>最佳 / 最差</th></tr></thead>
            <tbody><tr v-for="item in report.summary.windows" :key="item.window_days"><td><strong>{{ item.window_days }} 个交易日</strong></td><td>{{ item.sample_count }}</td><td>{{ item.evaluated_count }}</td><td>{{ item.pending_count }}</td><td>{{ item.incomplete_count }}</td><td>{{ formatPercent(evaluatedValue(item.win_rate, item.evaluated_count)) }}</td><td><span class="return-value" :class="returnTone(evaluatedValue(item.avg_return_ratio, item.evaluated_count))">{{ formatPercent(evaluatedValue(item.avg_return_ratio, item.evaluated_count)) }}</span></td><td>{{ formatPercent(evaluatedValue(item.median_return_ratio, item.evaluated_count)) }}</td><td><span class="positive">{{ formatPercent(evaluatedValue(item.best_return_ratio, item.evaluated_count)) }}</span> / <span class="negative">{{ formatPercent(evaluatedValue(item.worst_return_ratio, item.evaluated_count)) }}</span></td></tr></tbody>
          </table>
        </div>
      </article>

      <section class="report-view-switch no-print">
        <div class="segmented"><button type="button" :class="{ active: viewMode === 'recommendations' }" @click="viewMode = 'recommendations'">按推荐查看</button><button type="button" :class="{ active: viewMode === 'bloggers' }" @click="viewMode = 'bloggers'">按博主汇总</button></div>
        <span>报告生成于 {{ fullDateTime(report.generated_at) }}</span>
      </section>

      <section v-if="viewMode === 'bloggers'" class="blogger-report-grid no-print-view">
        <article v-for="group in report.bloggers" :key="group.blogger_id" class="panel blogger-report-card">
          <header class="panel-header"><div><h2>{{ group.blogger_name }}</h2><p>{{ group.institution || '独立研究者' }} · 仅统计本文推荐</p></div><strong>{{ group.recommendation_count }} 条</strong></header>
          <div class="blogger-current-strip"><div><span>截至目前胜率</span><strong>{{ formatPercent(evaluatedValue(group.current.win_rate, group.current.evaluated_count)) }}</strong></div><div><span>平均收益</span><strong :class="returnTone(evaluatedValue(group.current.avg_return_ratio, group.current.evaluated_count))">{{ formatPercent(evaluatedValue(group.current.avg_return_ratio, group.current.evaluated_count)) }}</strong></div><div><span>可评估</span><strong>{{ group.current.evaluated_count }} / {{ group.current.sample_count }}</strong></div></div>
          <div class="table-wrap"><table class="data-table"><thead><tr><th>窗口</th><th>可评估</th><th>胜率</th><th>平均收益</th><th>等待 / 缺口</th></tr></thead><tbody><tr v-for="metric in group.windows" :key="metric.window_days"><td>{{ metric.window_days }} 日</td><td>{{ metric.evaluated_count }} / {{ metric.sample_count }}</td><td>{{ formatPercent(evaluatedValue(metric.win_rate, metric.evaluated_count)) }}</td><td><span :class="returnTone(evaluatedValue(metric.avg_return_ratio, metric.evaluated_count))">{{ formatPercent(evaluatedValue(metric.avg_return_ratio, metric.evaluated_count)) }}</span></td><td>{{ metric.pending_count }} / {{ metric.incomplete_count }}</td></tr></tbody></table></div>
          <div class="blogger-recommendation-links"><button v-for="item in recommendationsForGroup(group)" :key="item.recommendation_event_id" type="button" @click="router.push(`/recommendations/${item.recommendation_event_id}`)"><span>{{ item.security_name || item.symbol }}</span><small>{{ formatDate(item.recommend_date) }} · {{ formatPercent(item.current.direction_return_ratio) }}</small></button></div>
        </article>
      </section>

      <section v-if="viewMode === 'recommendations'" class="report-recommendation-list">
        <article v-for="item in report.recommendations" :key="item.recommendation_event_id" class="panel report-recommendation-card">
          <header class="report-recommendation-header">
            <div class="identity-cell"><span class="avatar">{{ item.blogger_name.slice(0, 1) }}</span><span><strong>{{ item.security_name || item.symbol }}</strong><small>{{ item.blogger_name }} · {{ item.institution || '独立研究者' }}</small></span></div>
            <div class="report-recommendation-meta"><span>{{ formatDate(item.recommend_date) }}</span><span class="direction-pill" :class="item.direction.toLowerCase()">{{ item.direction === 'SHORT' ? '看空' : '看多' }}</span><span>{{ marketLabel(item.market) }} · {{ instrumentLabel(item) }}</span><span class="mono">{{ item.ts_code || item.symbol }}</span></div>
            <button class="icon-button no-print" type="button" title="查看单条推荐价格路径" @click="router.push(`/recommendations/${item.recommendation_event_id}`)"><BookOpenText :size="16" /></button>
          </header>

          <div class="report-current-line">
            <div><span>当前状态</span><StatusBadge :status="item.current.status" /></div>
            <div><span>入场</span><strong>{{ formatDate(item.current.entry_date) }} <small>{{ item.current.entry_price == null ? '' : `@ ${formatNumber(item.current.entry_price)}` }}</small></strong></div>
            <div><span>最新收盘</span><strong>{{ formatDate(item.current.latest_trade_date) }} <small>{{ item.current.latest_close_price == null ? '' : `@ ${formatNumber(item.current.latest_close_price)}` }}</small></strong></div>
            <div><span>截至目前方向收益</span><strong class="return-value" :class="returnTone(item.current.direction_return_ratio)">{{ formatPercent(item.current.direction_return_ratio) }}</strong></div>
          </div>
          <p v-if="item.current.reason_message && item.current.status !== 'READY'" class="report-reason">{{ item.current.reason_message }}</p>

          <div class="table-wrap">
            <table class="data-table report-metric-table">
              <thead><tr><th>窗口</th><th>状态</th><th>入场</th><th>窗口终点</th><th>方向收益</th><th>最大有利</th><th>最大不利</th><th>最大回撤</th><th>行情覆盖</th></tr></thead>
              <tbody><tr v-for="metric in item.windows" :key="metric.window_days"><td><strong>{{ metric.window_days }} 日</strong></td><td><StatusBadge :status="metric.status" /></td><td>{{ formatDate(metric.entry_date) }}<small>{{ metric.entry_price == null ? '' : ` @ ${formatNumber(metric.entry_price)}` }}</small></td><td>{{ formatDate(metric.exit_date) }}<small>{{ metric.exit_close_price == null ? '' : ` @ ${formatNumber(metric.exit_close_price)}` }}</small></td><td><span class="return-value" :class="returnTone(metricReturn(metric))">{{ formatPercent(metricReturn(metric)) }}</span></td><td class="positive">{{ formatPercent(metric.max_favorable_return_ratio) }}</td><td class="negative">{{ formatPercent(metric.max_adverse_return_ratio) }}</td><td class="negative">{{ formatPercent(metric.max_drawdown_ratio) }}</td><td>{{ metric.actual_quote_count }} / {{ metric.expected_quote_count }}</td></tr></tbody>
            </table>
          </div>

          <div class="report-evidence-grid">
            <div><span class="report-section-label">观点摘要</span><p>{{ item.thesis || '该条推荐未记录观点摘要。' }}</p></div>
            <details v-if="item.evidence.length"><summary>查看 {{ item.evidence.length }} 条原文证据</summary><div class="evidence-list"><div v-for="evidence in item.evidence" :key="`${evidence.chunk_index}-${evidence.text}`" class="evidence-item"><small>Evidence chunk {{ evidence.chunk_index }}</small>{{ evidence.text }}</div></div></details>
            <p v-else class="muted">没有单独存储的证据片段。</p>
          </div>
        </article>
        <StatePanel v-if="!report.recommendations.length" title="本文没有可展示的推荐事件" description="如果文档仍在分析中，请稍后刷新；无法追踪的目标会列在下方附录。" />
      </section>

      <article v-if="report.bloggers.length" class="panel print-only print-blogger-summary">
        <header class="panel-header"><div><h2>按博主汇总</h2><p>以下结果仅统计当前文档，不与博主全库历史样本混合。</p></div></header>
        <div class="table-wrap"><table class="data-table"><thead><tr><th>博主</th><th>推荐数</th><th>当前可评估</th><th>当前胜率</th><th>当前平均收益</th><th v-for="windowDays in report.windows" :key="windowDays">{{ windowDays }} 日胜率</th></tr></thead><tbody><tr v-for="group in report.bloggers" :key="group.blogger_id"><td><strong>{{ group.blogger_name }}</strong><br/><small>{{ group.institution || '独立研究者' }}</small></td><td>{{ group.recommendation_count }}</td><td>{{ group.current.evaluated_count }} / {{ group.current.sample_count }}</td><td>{{ formatPercent(evaluatedValue(group.current.win_rate, group.current.evaluated_count)) }}</td><td>{{ formatPercent(evaluatedValue(group.current.avg_return_ratio, group.current.evaluated_count)) }}</td><td v-for="metric in group.windows" :key="metric.window_days">{{ formatPercent(evaluatedValue(metric.win_rate, metric.evaluated_count)) }}<br/><small>{{ metric.evaluated_count }} 样本</small></td></tr></tbody></table></div>
      </article>

      <article v-if="report.untrackable_targets.length" class="panel report-untrackable">
        <header class="panel-header"><div><h2>不可追踪目标附录</h2><p>这些目标来自本文，但因无法识别或缺少市场数据而未进入收益统计。</p></div><FileWarning :size="18" /></header>
        <div class="table-wrap"><table class="data-table"><thead><tr><th>原始目标</th><th>标准化目标</th><th>类型</th><th>原因</th><th>来源</th></tr></thead><tbody><tr v-for="item in report.untrackable_targets" :key="item.id"><td><strong>{{ item.raw_target }}</strong></td><td>{{ item.normalized_target || '—' }}</td><td>{{ item.target_kind }}</td><td>{{ item.reason_message || item.reason_code }}</td><td>{{ item.source }}</td></tr></tbody></table></div>
      </article>

      <footer class="report-disclaimer">
        <strong>统计口径与声明</strong>
        <p>入场规则：{{ report.methodology.entry_price_rule || '推荐后首个交易日开盘价' }}；胜率阈值：方向收益高于 {{ formatPercent(report.methodology.win_threshold_ratio) }}。行情来源 {{ report.quote_source }}，数据截至 {{ formatDate(report.data_as_of) }}，计算版本 {{ report.calc_version }}。行情缺失、标的无法识别与未到期样本均不计入胜率和平均收益。本报告仅整理指定文档中的推荐事实及其历史市场表现，不构成投资建议。</p>
        <small>当前版生成时间：{{ fullDateTime(report.generated_at) }}</small>
      </footer>
    </template>
  </div>
</template>
