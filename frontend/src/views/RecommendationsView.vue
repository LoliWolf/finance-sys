<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ChevronLeft, ChevronRight, ClipboardList, Download, Filter, RefreshCw, Search, Target, UsersRound } from 'lucide-vue-next'
import { api } from '../api/client'
import type { RecommendationExportReport as ExportReport, RecommendationLedgerItem, RecommendationLedgerList, RecommendationWindowReturn } from '../api/types'
import RecommendationExportReport from '../components/RecommendationExportReport.vue'
import { beginReportPrint } from '../utils/printReport'
import MetricCard from '../components/MetricCard.vue'
import PageHeader from '../components/PageHeader.vue'
import StatePanel from '../components/StatePanel.vue'
import { assetTypeLabel, formatDate, formatNumber, formatPercent, returnTone, sectorTypeLabel, truncate } from '../utils/format'
import { paginationItems } from '../utils/pagination'

const router = useRouter()
const windows = [5, 10, 30, 90]
const pageSizeOptions = [20, 50, 100]
const loading = ref(true)
const error = ref('')
const data = ref<RecommendationLedgerList | null>(null)
const bloggerName = ref('')
const status = ref('')
const direction = ref('')
const market = ref('')
const assetType = ref('')
const symbol = ref('')
const dateFrom = ref('')
const dateTo = ref('')
const page = ref(1)
const pageSize = ref(50)
const exporting = ref(false)
const exportReport = ref<ExportReport | null>(null)
const appliedFilters = ref<Record<string, string>>({})
let loadVersion = 0
let disposed = false
let exportAbort: AbortController | undefined
let cleanupPrint: (() => void) | undefined

function filterQuery() {
  return { blogger_name: bloggerName.value.trim(), status: status.value, direction: direction.value,
    market: market.value, asset_type: assetType.value, symbol: symbol.value.trim(), date_from: dateFrom.value, date_to: dateTo.value }
}
const unappliedFilters = computed(() => JSON.stringify(filterQuery()) !== JSON.stringify(appliedFilters.value))

async function load(reset = false) {
  if (exporting.value) return
  const version = ++loadVersion
  const filters = filterQuery()
  if (reset) page.value = 1
  loading.value = true
  error.value = ''
  try {
    const result = await api.recommendations({
      ...filters,
      offset: (page.value - 1) * pageSize.value,
      limit: pageSize.value,
    })
    if (disposed || version !== loadVersion) return
    if (result.total_pages > 0 && page.value > result.total_pages) {
      page.value = result.total_pages
      await load()
      return
    }
    data.value = result
    appliedFilters.value = filters
    page.value = result.page
  } catch (cause) {
    if (disposed || version !== loadVersion) return
    error.value = cause instanceof Error ? cause.message : '推荐明细加载失败'
  } finally {
    if (version === loadVersion) loading.value = false
  }
}

async function exportCurrentFilter() {
  if (exporting.value || loading.value || unappliedFilters.value || !data.value?.total) return
  exporting.value = true
  error.value = ''
  exportReport.value = null
  const controller = new AbortController()
  exportAbort = controller
  try {
    const result = await api.recommendationExport({ ...appliedFilters.value }, controller.signal)
    if (disposed || controller.signal.aborted || exportAbort !== controller) return
    if (!result.total || result.items.length !== result.total) throw new Error('导出结果为空或不完整，请刷新筛选后重试')
    exportReport.value = result
    await nextTick()
    if (disposed) return
    cleanupPrint = beginReportPrint(`研迹_推荐明细_${result.filters.blogger_name || '筛选报告'}_截至${formatDate(result.data_as_of)}`, () => {
      exporting.value = false
      exportReport.value = null
      cleanupPrint = undefined
    })
  } catch (cause) {
    if (controller.signal.aborted || exportAbort !== controller) return
    if (!disposed) error.value = cause instanceof Error ? cause.message : '导出报告失败'
    exporting.value = false
    exportReport.value = null
  }
}

function cancelExport() {
  exportAbort?.abort()
  exportAbort = undefined
  cleanupPrint?.()
  exporting.value = false
  exportReport.value = null
}

function goToPage(target: number) {
  const maximum = Math.max(1, data.value?.total_pages || 1)
  const next = Math.min(maximum, Math.max(1, target))
  if (next === page.value || loading.value || exporting.value) return
  page.value = next
  load()
}

function changePageSize() {
  if (exporting.value) return
  page.value = 1
  load()
}

function clearFilters() {
  if (exporting.value) return
  bloggerName.value = ''
  status.value = ''
  direction.value = ''
  market.value = ''
  assetType.value = ''
  symbol.value = ''
  dateFrom.value = ''
  dateTo.value = ''
  load(true)
}

function metricFor(item: RecommendationLedgerItem, windowDays: number) {
  return item.windows.find((metric) => metric.window_days === windowDays)
}

function visibleReturn(metric: RecommendationWindowReturn | undefined) {
  return metric?.status === 'READY' ? metric.return_ratio : null
}

function metricTitle(item: RecommendationLedgerItem, metric: RecommendationWindowReturn | undefined, windowDays: number) {
	if (!metric?.status) return `${windowDays} 个交易日窗口暂未生成`
	if (metric.status === 'READY') return `第 ${windowDays} 个交易日${item.asset_type === 'SECTOR' ? '板块指数' : '标的'}涨跌幅`
  if (metric.status === 'PENDING') return metric.reason_message || `${windowDays} 个交易日窗口尚未到期`
  return metric.reason_message || `${windowDays} 个交易日窗口暂不可评估`
}

const totalPages = computed(() => Math.max(1, data.value?.total_pages || 1))
const pagerItems = computed(() => paginationItems(page.value, data.value?.total_pages || 0))
const rangeStart = computed(() => data.value?.total ? (page.value - 1) * pageSize.value + 1 : 0)
const rangeEnd = computed(() => Math.min(page.value * pageSize.value, data.value?.total || 0))
const readyWindowCount = computed(() => data.value?.items.reduce(
  (count, item) => count + item.windows.filter((metric) => metric.status === 'READY').length,
  0,
) || 0)
const pendingWindowCount = computed(() => data.value?.items.reduce(
  (count, item) => count + item.windows.filter((metric) => metric.status === 'PENDING' || !metric.status).length,
  0,
) || 0)

onMounted(() => load())
onBeforeUnmount(() => {
  disposed = true
  exportAbort?.abort()
  cleanupPrint?.()
})
</script>

<template>
  <div>
    <div class="recommendation-ledger-screen no-print">
    <PageHeader
      eyebrow="Evidence ledger"
      title="逐条看见推荐，逐条接受检验。"
		description="每条推荐只出现一次，同时呈现股票、ETF 与板块指数后续第 5、10、30、90 个交易日的涨跌；尚未到期的窗口暂不展示结果。"
    >
      <template #actions>
        <button class="button" type="button" :disabled="loading || exporting" @click="load()"><RefreshCw :size="15" />刷新</button>
        <button class="button primary" data-testid="export-recommendations" type="button" :disabled="loading || exporting || unappliedFilters || !data?.total" title="导出已应用筛选下的全部结果，单次最多 2000 条" @click="exportCurrentFilter"><Download :size="15" />{{ exporting ? '正在准备导出…' : '导出当前筛选' }}</button>
        <button v-if="exporting" class="button" data-testid="cancel-export" type="button" @click="cancelExport">取消导出</button>
      </template>
    </PageHeader>

    <fieldset class="filter-bar recommendation-filters" :disabled="exporting">
      <div class="field grow"><label>博主姓名</label><div class="search-input"><UsersRound :size="15" /><input v-model="bloggerName" placeholder="输入全部或部分名字" @keyup.enter="load(true)" /></div></div>
		<div class="field grow"><label>标的代码</label><div class="search-input"><Search :size="15" /><input v-model="symbol" placeholder="如 600519.SH 或 BK1128.DC" @keyup.enter="load(true)" /></div></div>
      <div class="field"><label>窗口状态（任一）</label><select v-model="status"><option value="">全部</option><option value="READY">已有结果</option><option value="PENDING">尚未到期</option><option value="INCOMPLETE">行情不全</option><option value="NO_SECURITY">未识别</option><option value="FAILED">失败</option></select></div>
      <div class="field"><label>方向</label><select v-model="direction"><option value="">全部</option><option value="LONG">看多</option><option value="SHORT">看空</option></select></div>
		<div class="field"><label>市场</label><select v-model="market"><option value="">全部</option><option value="SH">沪市</option><option value="SZ">深市</option><option value="BJ">北交所</option><option value="DC">东方财富板块</option></select></div>
		<div class="field"><label>资产</label><select v-model="assetType"><option value="">全部</option><option value="A_SHARE">A 股</option><option value="ETF">ETF</option><option value="SECTOR">板块指数</option></select></div>
      <div class="field"><label>推荐开始</label><input v-model="dateFrom" type="date" /></div>
      <div class="field"><label>推荐结束</label><input v-model="dateTo" type="date" /></div>
      <button class="button" type="button" :disabled="loading" @click="clearFilters">清除</button>
      <button class="button primary" type="button" :disabled="loading" @click="load(true)"><Filter :size="15" />筛选</button>
    </fieldset>
    <p class="muted recommendation-export-hint">{{ unappliedFilters && !loading ? '筛选条件已修改，请先点击“筛选”再导出。' : '导出当前筛选的全部推荐（非仅当前页），单次最多 2000 条；可在打印窗口保存为 PDF。' }}</p>

    <div v-if="error" class="error-banner">{{ error }}</div>
    <template v-if="data">
      <section class="metric-grid">
        <MetricCard label="符合条件" :value="formatNumber(data.total, 0)" note="推荐事件总数" :icon="ClipboardList" tone="accent" />
        <MetricCard label="本页推荐" :value="formatNumber(data.items.length, 0)" :note="`第 ${rangeStart}–${rangeEnd} 条`" :icon="UsersRound" />
        <MetricCard label="已有结果" :value="formatNumber(readyWindowCount, 0)" note="本页已完成窗口" :icon="Target" tone="positive" />
        <MetricCard label="暂未展示" :value="formatNumber(pendingWindowCount, 0)" note="未到期或尚未评估" />
        <MetricCard label="当前页" :value="`${page} / ${totalPages}`" :note="`每页 ${pageSize} 条`" />
      </section>

      <article class="panel">
        <header class="panel-header"><div><h2>推荐表现明细</h2><p>点击任意一行，查看完整价格路径、窗口指标和原始证据。</p></div><span class="muted">按推荐日倒序</span></header>
        <div v-if="data.items.length" class="table-wrap">
          <table class="data-table recommendation-ledger-table">
			<thead><tr><th>推荐日</th><th>博主</th><th>标的</th><th>方向</th><th v-for="windowDays in windows" :key="windowDays">{{ windowDays }} 个交易日</th><th>观点摘要</th></tr></thead>
            <tbody>
              <tr v-for="item in data.items" :key="item.recommendation_event_id" class="clickable" @click="router.push(`/recommendations/${item.recommendation_event_id}`)">
                <td>{{ formatDate(item.recommend_date) }}</td>
                <td><div class="identity-cell"><span class="avatar">{{ item.blogger_name.slice(0, 1) }}</span><span><strong>{{ item.blogger_name }}</strong><small>{{ item.institution || '独立研究者' }}</small></span></div></td>
				<td><strong>{{ item.security_name || item.symbol }}</strong><br/><small class="mono muted">{{ item.ts_code || item.symbol }} · {{ item.asset_type === 'SECTOR' ? sectorTypeLabel(item.sector_type) : assetTypeLabel(item.asset_type) }}</small></td>
                <td><span class="direction-pill" :class="item.direction.toLowerCase()">{{ item.direction === 'SHORT' ? '看空' : '看多' }}</span></td>
				<td v-for="windowDays in windows" :key="windowDays" class="window-return-cell" :title="metricTitle(item, metricFor(item, windowDays), windowDays)">
                  <span class="return-value" :class="returnTone(visibleReturn(metricFor(item, windowDays)))">{{ formatPercent(visibleReturn(metricFor(item, windowDays))) }}</span>
                </td>
                <td class="thesis-cell"><div class="thesis-copy" :title="item.thesis">{{ truncate(item.thesis, 56) }}</div></td>
              </tr>
            </tbody>
          </table>
        </div>
        <StatePanel v-else :loading="loading" title="没有符合条件的推荐" description="可以调整博主姓名、推荐日期或其他筛选条件。" />
        <div class="pager">
          <div class="pager-summary"><strong>共 {{ formatNumber(data.total, 0) }} 条</strong><span>第 {{ page }} / {{ totalPages }} 页 · 当前显示 {{ rangeStart }}–{{ rangeEnd }}</span></div>
          <div class="pager-controls">
            <label class="page-size-select"><span>每页</span><select v-model.number="pageSize" :disabled="loading || exporting" @change="changePageSize"><option v-for="value in pageSizeOptions" :key="value" :value="value">{{ value }} 条</option></select></label>
            <nav class="page-buttons" aria-label="推荐明细分页">
              <button type="button" class="page-button arrow" :disabled="page <= 1 || loading || exporting" aria-label="上一页" @click="goToPage(page - 1)"><ChevronLeft :size="16" /></button>
              <template v-for="(entry, index) in pagerItems" :key="`${entry}-${index}`">
                <span v-if="entry === 'ellipsis'" class="page-ellipsis">…</span>
                <button v-else type="button" class="page-button" :class="{ active: entry === page }" :disabled="loading || exporting" @click="goToPage(entry)">{{ entry }}</button>
              </template>
              <button type="button" class="page-button arrow" :disabled="page >= totalPages || loading || exporting" aria-label="下一页" @click="goToPage(page + 1)"><ChevronRight :size="16" /></button>
            </nav>
          </div>
        </div>
      </article>
    </template>
    <StatePanel v-else :loading="loading" />
    </div>
    <RecommendationExportReport v-if="exportReport" :report="exportReport" />
  </div>
</template>
