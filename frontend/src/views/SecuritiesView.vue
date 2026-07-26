<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { Flame, Layers3, RefreshCw, Search, UsersRound } from 'lucide-vue-next'
import { api } from '../api/client'
import type { RecommendationPerformanceList, SecurityRankingItem, SecurityRankingResponse } from '../api/types'
import ChartPanel from '../components/ChartPanel.vue'
import MetricCard from '../components/MetricCard.vue'
import PageHeader from '../components/PageHeader.vue'
import StatePanel from '../components/StatePanel.vue'
import StatusBadge from '../components/StatusBadge.vue'
import { formatDate, formatNumber, formatPercent, returnTone, truncate } from '../utils/format'

const router = useRouter()
const loading = ref(true)
const detailLoading = ref(false)
const error = ref('')
const data = ref<SecurityRankingResponse | null>(null)
const selected = ref<SecurityRankingItem | null>(null)
const selectedRecommendations = ref<RecommendationPerformanceList | null>(null)
const windowDays = ref(30)
const market = ref('')
const assetType = ref('')
const minSampleCount = ref(3)
const sort = ref('sample_count')
const search = ref('')

async function load() {
  loading.value = true
  error.value = ''
  try {
    data.value = await api.securityRankings({
      window_days: windowDays.value,
      market: market.value,
      asset_type: assetType.value,
      min_sample_count: minSampleCount.value,
      sort: sort.value,
      limit: 100,
    })
    const first = filteredItems.value[0] || data.value.items[0]
    if (first && (!selected.value || !data.value.items.some((item) => item.ts_code === selected.value?.ts_code))) await selectSecurity(first)
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : '标的表现加载失败'
  } finally {
    loading.value = false
  }
}

async function selectSecurity(item: SecurityRankingItem) {
  selected.value = item
  detailLoading.value = true
  try {
    selectedRecommendations.value = await api.securityRecommendations(item.ts_code, { window_days: windowDays.value, limit: 30 })
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : '标的推荐明细加载失败'
  } finally {
    detailLoading.value = false
  }
}

const filteredItems = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  if (!keyword) return data.value?.items || []
  return (data.value?.items || []).filter((item) => `${item.security_name}${item.ts_code}${item.industry}`.toLowerCase().includes(keyword))
})

const topItems = computed(() => filteredItems.value.slice(0, 12))
const heatOption = computed(() => ({
  tooltip: { trigger: 'axis', backgroundColor: '#2e2b26', borderWidth: 0, textStyle: { color: '#fff8ef' } },
  grid: { left: 90, right: 28, top: 18, bottom: 28 },
  xAxis: { type: 'value', axisLabel: { color: '#756d63' }, splitLine: { lineStyle: { color: 'rgba(65,52,39,.08)' } } },
  yAxis: { type: 'category', inverse: true, data: topItems.value.map((item) => item.security_name || item.ts_code), axisLine: { show: false }, axisTick: { show: false }, axisLabel: { color: '#514b43', width: 78, overflow: 'truncate' } },
  series: [{ type: 'bar', data: topItems.value.map((item) => item.recommendation_count), barWidth: 13, itemStyle: { color: '#b95e43', borderRadius: [0, 5, 5, 0] } }],
}))

const distributionOption = computed(() => ({
  tooltip: { trigger: 'item', backgroundColor: '#2e2b26', borderWidth: 0, textStyle: { color: '#fff8ef' }, formatter: (params: { data: { name: string; value: number[] } }) => `${params.data.name}<br/>推荐 ${params.data.value[0]} 次 · 收益 ${formatPercent(params.data.value[1])}<br/>胜率 ${formatPercent(params.data.value[2])}` },
  grid: { left: 48, right: 24, top: 20, bottom: 38 },
  xAxis: { type: 'value', name: '推荐次数', nameTextStyle: { color: '#8a8278' }, axisLabel: { color: '#756d63' }, splitLine: { lineStyle: { color: 'rgba(65,52,39,.08)' } } },
  yAxis: { type: 'value', name: '平均收益', axisLabel: { formatter: (value: number) => `${Math.round(value * 100)}%`, color: '#756d63' }, splitLine: { lineStyle: { color: 'rgba(65,52,39,.08)' } } },
  series: [{ type: 'scatter', symbolSize: (value: number[]) => Math.max(10, Math.min(28, 8 + value[2] * 20)), data: filteredItems.value.map((item) => ({ name: item.security_name || item.ts_code, value: [item.recommendation_count, item.avg_return_ratio, item.win_rate], itemStyle: { color: item.avg_return_ratio >= 0 ? '#4f7160' : '#b95e43', opacity: .75 } })) }],
}))

watch(windowDays, load)
onMounted(load)
</script>

<template>
  <div>
    <PageHeader
      eyebrow="Security lens"
      title="辨认能力，也辨认行情。"
      description="同一标的可能被多位博主在相近阶段推荐。标的视角把推荐热度、参与博主与后续收益并列，帮助区分判断能力和市场贝塔。"
    >
      <template #actions><button class="button" type="button" @click="load"><RefreshCw :size="15" />刷新</button></template>
    </PageHeader>

    <section class="filter-bar">
      <div class="field"><label>观察窗口</label><div class="segmented"><button v-for="value in [5, 10, 30, 90]" :key="value" type="button" :class="{ active: windowDays === value }" @click="windowDays = value">{{ value }} 日</button></div></div>
      <div class="field grow"><label>页内搜索</label><div style="position:relative"><Search :size="15" style="position:absolute;left:11px;top:12px;color:#8e867c"/><input v-model="search" style="padding-left:34px" placeholder="名称、代码或行业" /></div></div>
      <div class="field"><label>市场</label><select v-model="market"><option value="">全部</option><option value="SH">沪市</option><option value="SZ">深市</option><option value="BJ">北交所</option></select></div>
      <div class="field"><label>资产</label><select v-model="assetType"><option value="">全部</option><option value="STOCK">A 股</option><option value="ETF">ETF</option></select></div>
      <div class="field"><label>最少样本</label><input v-model.number="minSampleCount" type="number" min="0" /></div>
      <div class="field"><label>排序</label><select v-model="sort"><option value="sample_count">热度</option><option value="win_rate">胜率</option><option value="avg_return">平均收益</option></select></div>
      <button class="button primary" type="button" @click="load">应用筛选</button>
    </section>

    <div v-if="error" class="error-banner">{{ error }}</div>
    <template v-if="data?.items.length">
      <section class="metric-grid">
        <MetricCard label="标的样本" :value="formatNumber(data.items.length, 0)" note="达到最少样本门槛" :icon="Layers3" tone="accent" />
        <MetricCard label="最高热度" :value="formatNumber(data.items[0]?.recommendation_count, 0)" :note="data.items[0]?.security_name || data.items[0]?.ts_code" :icon="Flame" />
        <MetricCard label="最多参与博主" :value="formatNumber(Math.max(...data.items.map((item) => item.blogger_count)), 0)" note="同一标的交叉覆盖" :icon="UsersRound" />
        <MetricCard label="选中标的胜率" :value="formatPercent(selected?.win_rate)" :note="selected?.security_name || '点击列表选择'" :tone="(selected?.win_rate || 0) >= .5 ? 'positive' : 'negative'" />
        <MetricCard label="选中标的平均收益" :value="formatPercent(selected?.avg_return_ratio)" :note="`${windowDays} 日方向收益`" :tone="(selected?.avg_return_ratio || 0) >= 0 ? 'positive' : 'negative'" />
      </section>

      <section class="content-grid">
        <ChartPanel class="span-6" title="推荐热度 Top 12" description="被推荐次数反映关注度，不直接代表收益。" :option="heatOption" />
        <ChartPanel class="span-6" title="热度与收益分布" description="点面积映射胜率，识别高热度低收益标的。" :option="distributionOption" />

        <article class="panel span-7">
          <header class="panel-header"><div><h2>标的表现榜</h2><p>点击标的，在右侧查看推荐者与具体事件。</p></div><span class="muted">{{ filteredItems.length }} 个标的</span></header>
          <div class="table-wrap" style="max-height:640px;overflow:auto">
            <table class="data-table">
              <thead><tr><th>排名</th><th>标的</th><th>推荐</th><th>博主</th><th>胜率</th><th>平均收益</th><th>最佳 / 最差</th></tr></thead>
              <tbody>
                <tr v-for="item in filteredItems" :key="item.ts_code" class="clickable" :style="selected?.ts_code === item.ts_code ? 'background:rgba(207,107,77,.07)' : ''" @click="selectSecurity(item)">
                  <td><span class="rank-number" :class="{ top: item.rank <= 3 }">{{ item.rank }}</span></td>
                  <td><div class="identity-cell"><span class="avatar">{{ item.security_name?.slice(0, 1) || '?' }}</span><span><strong>{{ item.security_name || item.symbol }}</strong><small>{{ item.ts_code }} · {{ item.industry || '未分类' }}</small></span></div></td>
                  <td>{{ item.recommendation_count }}</td><td>{{ item.blogger_count }}</td>
                  <td><span class="return-value" :class="item.win_rate >= .5 ? 'positive' : 'negative'">{{ formatPercent(item.win_rate) }}</span></td>
                  <td><span class="return-value" :class="returnTone(item.avg_return_ratio)">{{ formatPercent(item.avg_return_ratio) }}</span></td>
                  <td><span class="return-value positive">{{ formatPercent(item.best_return_ratio) }}</span> <span class="muted">/</span> <span class="return-value negative">{{ formatPercent(item.worst_return_ratio) }}</span></td>
                </tr>
              </tbody>
            </table>
          </div>
        </article>

        <article class="panel span-5">
          <header class="panel-header"><div><h2>{{ selected?.security_name || '选择标的' }}</h2><p class="mono">{{ selected?.ts_code || '—' }} · {{ selected?.industry || '—' }}</p></div><StatusBadge v-if="selected" status="READY" /></header>
          <StatePanel v-if="detailLoading" loading />
          <div v-else-if="selectedRecommendations?.items.length" style="max-height:640px;overflow:auto">
            <div v-for="item in selectedRecommendations.items" :key="item.recommendation_event_id" class="prose" style="border-bottom:1px solid rgba(56,48,39,.09);cursor:pointer" @click="router.push(`/recommendations/${item.recommendation_event_id}`)">
              <div style="display:flex;justify-content:space-between;gap:12px"><h3>{{ item.blogger_name }}</h3><span class="return-value" :class="returnTone(item.direction_return_ratio)">{{ formatPercent(item.direction_return_ratio) }}</span></div>
              <p>{{ formatDate(item.recommend_date) }} · {{ truncate(item.thesis, 74) }}</p>
              <div style="margin-top:10px"><StatusBadge :status="item.status" /></div>
            </div>
          </div>
          <StatePanel v-else title="暂无推荐明细" description="选择左侧其他标的继续查看。" />
        </article>
      </section>
    </template>
    <StatePanel v-else :loading="loading" />
  </div>
</template>
