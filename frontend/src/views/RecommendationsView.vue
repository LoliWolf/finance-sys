<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ClipboardList, Filter, RefreshCw, Search, Target } from 'lucide-vue-next'
import { api } from '../api/client'
import type { RecommendationPerformanceList } from '../api/types'
import MetricCard from '../components/MetricCard.vue'
import PageHeader from '../components/PageHeader.vue'
import StatePanel from '../components/StatePanel.vue'
import StatusBadge from '../components/StatusBadge.vue'
import { formatDate, formatNumber, formatPercent, returnTone, truncate } from '../utils/format'

const router = useRouter()
const loading = ref(true)
const error = ref('')
const data = ref<RecommendationPerformanceList | null>(null)
const windowDays = ref(30)
const status = ref('')
const direction = ref('LONG')
const market = ref('')
const assetType = ref('')
const symbol = ref('')
const dateFrom = ref('')
const dateTo = ref('')
const offset = ref(0)
const limit = 50

async function load(reset = false) {
  if (reset) offset.value = 0
  loading.value = true
  error.value = ''
  try {
    data.value = await api.recommendations({
      window_days: windowDays.value,
      status: status.value,
      direction: direction.value,
      market: market.value,
      asset_type: assetType.value,
      symbol: symbol.value.trim(),
      date_from: dateFrom.value,
      date_to: dateTo.value,
      offset: offset.value,
      limit,
    })
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : '推荐明细加载失败'
  } finally {
    loading.value = false
  }
}

function nextPage() { offset.value += limit; load() }
function previousPage() { offset.value = Math.max(0, offset.value - limit); load() }

const pageReady = computed(() => data.value?.items.filter((item) => item.status === 'READY') || [])
const pageAverage = computed(() => {
  const values = pageReady.value.flatMap((item) => item.direction_return_ratio == null ? [] : [item.direction_return_ratio])
  return values.length ? values.reduce((sum, value) => sum + value, 0) / values.length : 0
})
const pageWinRate = computed(() => pageReady.value.length ? pageReady.value.filter((item) => item.win_flag).length / pageReady.value.length : 0)

onMounted(() => load())
</script>

<template>
  <div>
    <PageHeader
      eyebrow="Evidence ledger"
      title="逐条看见推荐，逐条接受检验。"
      description="这里不是观点信息流，而是一份可下钻的评价账本：推荐日、标的、入场与退出、收益、波动、证据都可追溯。"
    >
      <template #actions><button class="button" type="button" @click="load()"><RefreshCw :size="15" />刷新</button></template>
    </PageHeader>

    <section class="filter-bar">
      <div class="field"><label>窗口</label><div class="segmented"><button v-for="value in [5, 10, 30, 90]" :key="value" type="button" :class="{ active: windowDays === value }" @click="windowDays = value; load(true)">{{ value }} 日</button></div></div>
      <div class="field grow"><label>标的代码</label><div style="position: relative"><Search :size="15" style="position:absolute;left:11px;top:12px;color:#8e867c"/><input v-model="symbol" style="padding-left:34px" placeholder="如 600519.SH" @keyup.enter="load(true)" /></div></div>
      <div class="field"><label>状态</label><select v-model="status"><option value="">全部</option><option value="READY">已评估</option><option value="PENDING">未到期</option><option value="INCOMPLETE">行情不全</option><option value="NO_SECURITY">未识别</option><option value="FAILED">失败</option></select></div>
      <div class="field"><label>方向</label><select v-model="direction"><option value="">全部</option><option value="LONG">做多</option><option value="SHORT">做空</option></select></div>
      <div class="field"><label>市场</label><select v-model="market"><option value="">全部</option><option value="SH">沪市</option><option value="SZ">深市</option><option value="BJ">北交所</option></select></div>
      <div class="field"><label>资产</label><select v-model="assetType"><option value="">全部</option><option value="STOCK">A 股</option><option value="ETF">ETF</option></select></div>
      <div class="field"><label>开始</label><input v-model="dateFrom" type="date" /></div>
      <div class="field"><label>结束</label><input v-model="dateTo" type="date" /></div>
      <button class="button primary" type="button" @click="load(true)"><Filter :size="15" />筛选</button>
    </section>

    <div v-if="error" class="error-banner">{{ error }}</div>
    <template v-if="data">
      <section class="metric-grid">
        <MetricCard label="符合条件" :value="formatNumber(data.total, 0)" note="当前窗口全部状态" :icon="ClipboardList" tone="accent" />
        <MetricCard label="本页已评估" :value="formatNumber(pageReady.length, 0)" :note="`每页最多 ${limit} 条`" :icon="Target" />
        <MetricCard label="本页胜率" :value="formatPercent(pageWinRate)" note="只计 READY" :tone="pageWinRate >= .5 ? 'positive' : 'negative'" />
        <MetricCard label="本页平均收益" :value="formatPercent(pageAverage)" note="方向收益均值" :tone="pageAverage >= 0 ? 'positive' : 'negative'" />
        <MetricCard label="当前页码" :value="`${Math.floor(offset / limit) + 1}`" :note="`${offset + 1}–${Math.min(offset + limit, data.total)} 条`" />
      </section>

      <article class="panel">
        <header class="panel-header"><div><h2>推荐评价账本</h2><p>点击行进入单条推荐的价格路径与原始证据。</p></div><span class="muted">{{ windowDays }} 日窗口</span></header>
        <div v-if="data.items.length" class="table-wrap">
          <table class="data-table">
            <thead><tr><th>推荐日</th><th>博主</th><th>标的</th><th>观点摘要</th><th>状态</th><th>方向收益</th><th>最大浮盈</th><th>最大不利</th><th>最大回撤</th><th>入场 → 退出</th></tr></thead>
            <tbody>
              <tr v-for="item in data.items" :key="item.recommendation_event_id" class="clickable" @click="router.push(`/recommendations/${item.recommendation_event_id}`)">
                <td>{{ formatDate(item.recommend_date) }}</td>
                <td><div class="identity-cell"><span class="avatar">{{ item.blogger_name.slice(0, 1) }}</span><span><strong>{{ item.blogger_name }}</strong><small>{{ item.institution || '独立研究者' }}</small></span></div></td>
                <td><strong>{{ item.security_name || item.symbol }}</strong><br/><small class="mono muted">{{ item.ts_code || item.symbol }}</small></td>
                <td style="max-width:310px;white-space:normal">{{ truncate(item.thesis, 60) }}</td>
                <td><StatusBadge :status="item.status" /></td>
                <td><span class="return-value" :class="returnTone(item.direction_return_ratio)">{{ formatPercent(item.direction_return_ratio) }}</span></td>
                <td><span class="return-value positive">{{ formatPercent(item.max_favorable_return_ratio) }}</span></td>
                <td><span class="return-value negative">{{ formatPercent(item.max_adverse_return_ratio) }}</span></td>
                <td><span class="return-value negative">{{ formatPercent(item.max_drawdown_ratio) }}</span></td>
                <td>{{ formatDate(item.entry_date) }} <span class="muted">→</span> {{ formatDate(item.exit_date) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <StatePanel v-else :loading="loading" />
        <div class="pager">
          <span>共 {{ formatNumber(data.total, 0) }} 条 · 第 {{ Math.floor(offset / limit) + 1 }} 页</span>
          <div class="pager-actions"><button class="button" type="button" :disabled="offset === 0 || loading" @click="previousPage">上一页</button><button class="button" type="button" :disabled="offset + limit >= data.total || loading" @click="nextPage">下一页</button></div>
        </div>
      </article>
    </template>
    <StatePanel v-else :loading="loading" />
  </div>
</template>
