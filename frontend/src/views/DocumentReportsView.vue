<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Calculator, ChevronLeft, ChevronRight, FileArchive, Filter, RefreshCw, Search, UsersRound } from 'lucide-vue-next'
import { api } from '../api/client'
import type { DocumentReportList, DocumentReportListItem, EvaluationRun } from '../api/types'
import MetricCard from '../components/MetricCard.vue'
import PageHeader from '../components/PageHeader.vue'
import StatePanel from '../components/StatePanel.vue'
import StatusBadge from '../components/StatusBadge.vue'
import { formatDate, formatDateTime, formatNumber } from '../utils/format'
import { paginationItems } from '../utils/pagination'

const router = useRouter()
const pageSizeOptions = [20, 50, 100]
const loading = ref(true)
const evaluating = ref(false)
const error = ref('')
const notice = ref('')
const data = ref<DocumentReportList | null>(null)
const query = ref('')
const status = ref('')
const dateFrom = ref('')
const dateTo = ref('')
const page = ref(1)
const pageSize = ref(50)
const selectedIDs = ref<Set<number>>(new Set())
const forceRebuild = ref(false)
const activeRun = ref<EvaluationRun | null>(null)
let runPollTimer: number | undefined
let disposed = false

async function load(reset = false) {
  if (reset) page.value = 1
  loading.value = true
  error.value = ''
  try {
    const result = await api.documentReports({
      query: query.value.trim(),
      status: status.value,
      date_from: dateFrom.value,
      date_to: dateTo.value,
      offset: (page.value - 1) * pageSize.value,
      limit: pageSize.value,
    })
    if (result.total_pages > 0 && page.value > result.total_pages) {
      page.value = result.total_pages
      await load()
      return
    }
    data.value = result
    page.value = result.page
    selectedIDs.value = new Set()
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : '文档报告列表加载失败'
  } finally {
    loading.value = false
  }
}

function goToPage(target: number) {
  const maximum = Math.max(1, data.value?.total_pages || 1)
  const next = Math.min(maximum, Math.max(1, target))
  if (next === page.value || loading.value) return
  page.value = next
  load()
}

function changePageSize() {
  page.value = 1
  load()
}

function clearFilters() {
  query.value = ''
  status.value = ''
  dateFrom.value = ''
  dateTo.value = ''
  load(true)
}

function toggleSelection(documentID: number) {
  const next = new Set(selectedIDs.value)
  if (next.has(documentID)) next.delete(documentID)
  else next.add(documentID)
  selectedIDs.value = next
}

function toggleCurrentPage() {
  const ids = data.value?.items.map((item) => item.document_id) || []
  const allSelected = ids.length > 0 && ids.every((id) => selectedIDs.value.has(id))
  selectedIDs.value = allSelected ? new Set() : new Set(ids)
}

async function evaluateSelected() {
  const ids = Array.from(selectedIDs.value)
  if (!ids.length || evaluating.value) return
  evaluating.value = true
  error.value = ''
  notice.value = ''
  try {
    const response = await api.createDocumentEvaluationRun(ids, forceRebuild.value)
    selectedIDs.value = new Set()
    notice.value = `已提交 ${response.document_count} 篇文档的追踪补算，任务 #${response.run_id} 正在排队。`
    await watchEvaluationRun(response.run_id)
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : '文档追踪补算提交失败'
    evaluating.value = false
  }
}

async function watchEvaluationRun(runID: number) {
  if (disposed) return
  try {
    const run = await api.evaluationRun(runID)
    activeRun.value = run
    if (run.status === 'QUEUED' || run.status === 'RUNNING') {
      runPollTimer = window.setTimeout(() => watchEvaluationRun(runID), 1800)
      return
    }
    evaluating.value = false
    notice.value = run.status === 'SUCCEEDED'
      ? `补算任务 #${runID} 已完成，文档状态已刷新。`
      : `补算任务 #${runID} 已结束，状态：${run.status}。`
    await load()
  } catch (cause) {
    evaluating.value = false
    error.value = cause instanceof Error ? cause.message : '补算任务状态查询失败'
  }
}

function reportProgress(item: DocumentReportListItem) {
  if (!item.expected_metric_count) return '—'
  return `${item.ready_metric_count} / ${item.expected_metric_count}`
}

function reportProgressNote(item: DocumentReportListItem) {
  const missing = item.missing_metric_count + item.incomplete_metric_count
  return `${item.pending_metric_count} 未到期 · ${missing} 待补齐`
}

const totalPages = computed(() => Math.max(1, data.value?.total_pages || 1))
const pagerItems = computed(() => paginationItems(page.value, data.value?.total_pages || 0))
const rangeStart = computed(() => data.value?.total ? (page.value - 1) * pageSize.value + 1 : 0)
const rangeEnd = computed(() => Math.min(page.value * pageSize.value, data.value?.total || 0))
const currentPageSelected = computed(() => {
  const ids = data.value?.items.map((item) => item.document_id) || []
  return ids.length > 0 && ids.every((id) => selectedIDs.value.has(id))
})
const currentPagePartiallySelected = computed(() => {
  const ids = data.value?.items.map((item) => item.document_id) || []
  return !currentPageSelected.value && ids.some((id) => selectedIDs.value.has(id))
})
const pageRecommendationCount = computed(() => data.value?.items.reduce((total, item) => total + item.recommendation_count, 0) || 0)
const pageNeedsEvaluationCount = computed(() => data.value?.items.filter((item) => item.report_status === 'NEEDS_EVALUATION' || item.report_status === 'PARTIAL').length || 0)

onMounted(() => load())
onBeforeUnmount(() => {
  disposed = true
  if (runPollTimer !== undefined) window.clearTimeout(runPollTimer)
})
</script>

<template>
  <div>
    <PageHeader
      eyebrow="Document archive"
      title="从一篇研报，回到每一条可检验的推荐。"
      description="浏览历史上传文档，查看文档内全部推荐与固定交易日窗口表现；老研报可按文档补算，不受每日近期任务范围限制。"
    >
      <template #actions><button class="button" type="button" :disabled="loading" @click="load()"><RefreshCw :size="15" />刷新</button></template>
    </PageHeader>

    <section class="filter-bar">
      <div class="field grow"><label>文档关键词</label><div class="search-input"><Search :size="15" /><input v-model="query" placeholder="标题、文件名、作者或机构" @keyup.enter="load(true)" /></div></div>
      <div class="field"><label>分析状态</label><select v-model="status"><option value="">全部</option><option value="PLANNED">已完成分析</option><option value="INGESTED">已入库</option><option value="PARSED">已解析</option><option value="INVALID">无有效推荐</option><option value="FAILED">分析失败</option></select></div>
      <div class="field"><label>上传开始</label><input v-model="dateFrom" type="date" /></div>
      <div class="field"><label>上传结束</label><input v-model="dateTo" type="date" /></div>
      <button class="button" type="button" :disabled="loading" @click="clearFilters">清除</button>
      <button class="button primary" type="button" :disabled="loading" @click="load(true)"><Filter :size="15" />筛选</button>
    </section>

    <div v-if="error" class="error-banner">{{ error }}</div>
    <div v-if="notice" class="success-banner">{{ notice }}<span v-if="activeRun && evaluating"> 已处理 {{ activeRun.evaluated_event_count }} / {{ activeRun.target_event_count }} 条推荐。</span></div>

    <template v-if="data">
      <section class="metric-grid">
        <MetricCard label="历史文档" :value="formatNumber(data.total, 0)" note="符合当前筛选" :icon="FileArchive" tone="accent" />
        <MetricCard label="本页推荐" :value="formatNumber(pageRecommendationCount, 0)" note="由当前页文档产生" />
        <MetricCard label="需要补算" :value="formatNumber(pageNeedsEvaluationCount, 0)" note="待补算或部分可用" :icon="Calculator" />
        <MetricCard label="已选择" :value="formatNumber(selectedIDs.size, 0)" note="最多批量选择 100 篇" :icon="UsersRound" />
        <MetricCard label="行情截止" :value="formatDate(data.data_as_of)" :note="`第 ${page} / ${totalPages} 页`" />
      </section>

      <section v-if="selectedIDs.size" class="bulk-action-bar">
        <div><strong>已选择 {{ selectedIDs.size }} 篇文档</strong><span>补算会复用已有指标，只计算缺失、未成熟或旧版本窗口。</span></div>
        <label class="toggle-row"><input v-model="forceRebuild" type="checkbox" />强制重算已有 READY 指标</label>
        <button class="button primary" type="button" :disabled="evaluating" @click="evaluateSelected"><Calculator :size="15" />{{ evaluating ? '正在补算…' : '批量补算追踪' }}</button>
      </section>

      <article class="panel">
        <header class="panel-header"><div><h2>历史上传文档</h2><p>点击文档进入单篇报告；列表只读取元数据，不加载原始文件内容。</p></div><span class="muted">按上传时间倒序</span></header>
        <div v-if="data.items.length" class="table-wrap">
          <table class="data-table document-report-list-table">
            <thead><tr><th class="selection-cell"><input type="checkbox" :checked="currentPageSelected" :indeterminate.prop="currentPagePartiallySelected" aria-label="选择当前页" @click.stop="toggleCurrentPage" /></th><th>文档</th><th>作者 / 机构</th><th>分析状态</th><th>推荐概况</th><th>推荐日期</th><th>窗口进度</th><th>报告状态</th><th>上传时间</th></tr></thead>
            <tbody>
              <tr v-for="item in data.items" :key="item.document_id" class="clickable" @click="router.push(`/document-reports/${item.document_id}`)">
                <td class="selection-cell" @click.stop><input type="checkbox" :checked="selectedIDs.has(item.document_id)" :aria-label="`选择文档 ${item.document_id}`" @change="toggleSelection(item.document_id)" /></td>
                <td class="document-title-cell"><strong>{{ item.title || item.file_name }}</strong><small>#{{ item.document_id }} · {{ item.file_name }}</small></td>
                <td><strong>{{ item.author || '未标注' }}</strong><br/><small class="muted">{{ item.institution || '—' }}</small></td>
                <td><StatusBadge :status="item.status" /></td>
                <td>{{ item.recommendation_count }} 条推荐<br/><small class="muted">{{ item.blogger_count }} 位博主 · {{ item.untrackable_count }} 条不可追踪</small></td>
                <td>{{ formatDate(item.recommend_date_from) }}<br/><small class="muted">至 {{ formatDate(item.recommend_date_to) }}</small></td>
                <td><strong>{{ reportProgress(item) }}</strong><br/><small class="muted">{{ reportProgressNote(item) }}</small></td>
                <td><StatusBadge :status="item.report_status" /></td>
                <td>{{ formatDateTime(item.created_at) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <StatePanel v-else :loading="loading" title="没有符合条件的文档" description="可以调整关键词、上传日期或分析状态。" />
        <div class="pager">
          <div class="pager-summary"><strong>共 {{ formatNumber(data.total, 0) }} 篇</strong><span>第 {{ page }} / {{ totalPages }} 页 · 当前显示 {{ rangeStart }}–{{ rangeEnd }}</span></div>
          <div class="pager-controls">
            <label class="page-size-select"><span>每页</span><select v-model.number="pageSize" :disabled="loading" @change="changePageSize"><option v-for="value in pageSizeOptions" :key="value" :value="value">{{ value }} 篇</option></select></label>
            <nav class="page-buttons" aria-label="文档报告分页">
              <button type="button" class="page-button arrow" :disabled="page <= 1 || loading" aria-label="上一页" @click="goToPage(page - 1)"><ChevronLeft :size="16" /></button>
              <template v-for="(entry, index) in pagerItems" :key="`${entry}-${index}`">
                <span v-if="entry === 'ellipsis'" class="page-ellipsis">…</span>
                <button v-else type="button" class="page-button" :class="{ active: entry === page }" :disabled="loading" @click="goToPage(entry)">{{ entry }}</button>
              </template>
              <button type="button" class="page-button arrow" :disabled="page >= totalPages || loading" aria-label="下一页" @click="goToPage(page + 1)"><ChevronRight :size="16" /></button>
            </nav>
          </div>
        </div>
      </article>
    </template>
    <StatePanel v-else :loading="loading" />
  </div>
</template>
