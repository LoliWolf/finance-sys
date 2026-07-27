<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ChevronLeft, ChevronRight, ClipboardList, Filter, RefreshCw, Search, Target, UsersRound } from 'lucide-vue-next'
import { api } from '../api/client'
import type { RecommendationLedgerItem, RecommendationLedgerList, RecommendationWindowReturn } from '../api/types'
import MetricCard from '../components/MetricCard.vue'
import PageHeader from '../components/PageHeader.vue'
import StatePanel from '../components/StatePanel.vue'
import { formatDate, formatNumber, formatPercent, returnTone, truncate } from '../utils/format'
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

async function load(reset = false) {
  if (reset) page.value = 1
  loading.value = true
  error.value = ''
  try {
    const result = await api.recommendations({
      blogger_name: bloggerName.value.trim(),
      status: status.value,
      direction: direction.value,
      market: market.value,
      asset_type: assetType.value,
      symbol: symbol.value.trim(),
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
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : '推荐明细加载失败'
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

function metricTitle(metric: RecommendationWindowReturn | undefined, windowDays: number) {
  if (!metric?.status) return `${windowDays} 日窗口暂未生成`
  if (metric.status === 'READY') return `${windowDays} 日后股票涨跌幅`
  if (metric.status === 'PENDING') return metric.reason_message || `${windowDays} 日窗口尚未到期`
  return metric.reason_message || `${windowDays} 日窗口暂不可评估`
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
</script>

<template>
  <div>
    <PageHeader
      eyebrow="Evidence ledger"
      title="逐条看见推荐，逐条接受检验。"
      description="每条推荐只出现一次，同时呈现后续 5、10、30、90 日的股票涨跌；尚未到期的窗口暂不展示收益。"
    >
      <template #actions><button class="button" type="button" :disabled="loading" @click="load()"><RefreshCw :size="15" />刷新</button></template>
    </PageHeader>

    <section class="filter-bar">
      <div class="field grow"><label>博主姓名</label><div class="search-input"><UsersRound :size="15" /><input v-model="bloggerName" placeholder="输入全部或部分名字" @keyup.enter="load(true)" /></div></div>
      <div class="field grow"><label>股票代码</label><div class="search-input"><Search :size="15" /><input v-model="symbol" placeholder="如 600519 或 600519.SH" @keyup.enter="load(true)" /></div></div>
      <div class="field"><label>窗口状态（任一）</label><select v-model="status"><option value="">全部</option><option value="READY">已有结果</option><option value="PENDING">尚未到期</option><option value="INCOMPLETE">行情不全</option><option value="NO_SECURITY">未识别</option><option value="FAILED">失败</option></select></div>
      <div class="field"><label>方向</label><select v-model="direction"><option value="">全部</option><option value="LONG">看多</option><option value="SHORT">看空</option></select></div>
      <div class="field"><label>市场</label><select v-model="market"><option value="">全部</option><option value="SH">沪市</option><option value="SZ">深市</option><option value="BJ">北交所</option></select></div>
      <div class="field"><label>资产</label><select v-model="assetType"><option value="">全部</option><option value="STOCK">A 股</option><option value="ETF">ETF</option></select></div>
      <div class="field"><label>推荐开始</label><input v-model="dateFrom" type="date" /></div>
      <div class="field"><label>推荐结束</label><input v-model="dateTo" type="date" /></div>
      <button class="button" type="button" :disabled="loading" @click="clearFilters">清除</button>
      <button class="button primary" type="button" :disabled="loading" @click="load(true)"><Filter :size="15" />筛选</button>
    </section>

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
            <thead><tr><th>推荐日</th><th>博主</th><th>股票</th><th>方向</th><th v-for="windowDays in windows" :key="windowDays">{{ windowDays }} 日涨跌</th><th>观点摘要</th></tr></thead>
            <tbody>
              <tr v-for="item in data.items" :key="item.recommendation_event_id" class="clickable" @click="router.push(`/recommendations/${item.recommendation_event_id}`)">
                <td>{{ formatDate(item.recommend_date) }}</td>
                <td><div class="identity-cell"><span class="avatar">{{ item.blogger_name.slice(0, 1) }}</span><span><strong>{{ item.blogger_name }}</strong><small>{{ item.institution || '独立研究者' }}</small></span></div></td>
                <td><strong>{{ item.security_name || item.symbol }}</strong><br/><small class="mono muted">{{ item.ts_code || item.symbol }}</small></td>
                <td><span class="direction-pill" :class="item.direction.toLowerCase()">{{ item.direction === 'SHORT' ? '看空' : '看多' }}</span></td>
                <td v-for="windowDays in windows" :key="windowDays" class="window-return-cell" :title="metricTitle(metricFor(item, windowDays), windowDays)">
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
            <label class="page-size-select"><span>每页</span><select v-model.number="pageSize" :disabled="loading" @change="changePageSize"><option v-for="value in pageSizeOptions" :key="value" :value="value">{{ value }} 条</option></select></label>
            <nav class="page-buttons" aria-label="推荐明细分页">
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
