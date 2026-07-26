<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Activity, ArrowRight, CircleGauge, Clock3, RefreshCw, UsersRound } from 'lucide-vue-next'
import { api } from '../api/client'
import type { BloggerRankingResponse } from '../api/types'
import ChartPanel from '../components/ChartPanel.vue'
import MetricCard from '../components/MetricCard.vue'
import PageHeader from '../components/PageHeader.vue'
import StatePanel from '../components/StatePanel.vue'
import { formatNumber, formatPercent, returnTone } from '../utils/format'

const router = useRouter()
const loading = ref(true)
const error = ref('')
const data = ref<BloggerRankingResponse | null>(null)
const windowDays = ref(30)
const dateFrom = ref('')
const dateTo = ref('')
const market = ref('')
const assetType = ref('')
const direction = ref('LONG')
const minSampleCount = ref(5)
const sort = ref('win_rate')

async function load() {
  loading.value = true
  error.value = ''
  try {
    data.value = await api.bloggerRankings({
      window_days: windowDays.value,
      date_from: dateFrom.value,
      date_to: dateTo.value,
      market: market.value,
      asset_type: assetType.value,
      direction: direction.value,
      min_sample_count: minSampleCount.value,
      sort: sort.value,
      limit: 100,
    })
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : '排行榜加载失败'
  } finally {
    loading.value = false
  }
}

const topItems = computed(() => data.value?.items.slice(0, 10) || [])
const axisStyle = { axisLine: { lineStyle: { color: '#d9d0c3' } }, axisLabel: { color: '#756d63', fontSize: 10 }, splitLine: { lineStyle: { color: 'rgba(68,55,43,.08)' } } }
const tooltip = { trigger: 'axis', backgroundColor: '#2e2b26', borderWidth: 0, textStyle: { color: '#fff8ef', fontSize: 11 } }

const winRateOption = computed(() => ({
  tooltip,
  grid: { left: 92, right: 30, top: 18, bottom: 28 },
  xAxis: { type: 'value', max: 1, axisLabel: { formatter: (value: number) => `${Math.round(value * 100)}%`, color: '#756d63' }, splitLine: axisStyle.splitLine },
  yAxis: { type: 'category', inverse: true, data: topItems.value.map((item) => item.blogger_name), axisTick: { show: false }, axisLine: { show: false }, axisLabel: { color: '#514b43', width: 78, overflow: 'truncate' } },
  series: [{ type: 'bar', data: topItems.value.map((item) => item.win_rate), barWidth: 13, itemStyle: { color: '#b95e43', borderRadius: [0, 5, 5, 0] } }],
}))

const returnOption = computed(() => ({
  tooltip,
  grid: { left: 92, right: 28, top: 18, bottom: 28 },
  xAxis: { type: 'value', axisLabel: { formatter: (value: number) => `${(value * 100).toFixed(0)}%`, color: '#756d63' }, splitLine: axisStyle.splitLine },
  yAxis: { type: 'category', inverse: true, data: topItems.value.map((item) => item.blogger_name), axisTick: { show: false }, axisLine: { show: false }, axisLabel: { color: '#514b43', width: 78, overflow: 'truncate' } },
  series: [{
    type: 'bar',
    data: topItems.value.map((item) => ({ value: item.avg_return_ratio, itemStyle: { color: item.avg_return_ratio >= 0 ? '#587765' : '#b45d52' } })),
    barWidth: 13,
    itemStyle: { borderRadius: 4 },
  }],
}))

const scatterOption = computed(() => ({
  tooltip: {
    trigger: 'item',
    backgroundColor: '#2e2b26',
    borderWidth: 0,
    textStyle: { color: '#fff8ef', fontSize: 11 },
    formatter: (params: { data: { value: [number, number, number]; name: string } }) => `${params.data.name}<br/>样本 ${params.data.value[0]} · 收益 ${formatPercent(params.data.value[1])}<br/>胜率 ${formatPercent(params.data.value[2])}`,
  },
  grid: { left: 48, right: 24, top: 20, bottom: 38 },
  xAxis: { type: 'value', name: '可评估样本', nameTextStyle: { color: '#8a8278' }, ...axisStyle },
  yAxis: { type: 'value', name: '平均收益', axisLabel: { formatter: (value: number) => `${(value * 100).toFixed(0)}%`, color: '#756d63' }, splitLine: axisStyle.splitLine },
  series: [{
    type: 'scatter',
    symbolSize: (value: [number, number, number]) => Math.max(10, Math.min(28, 9 + value[2] * 18)),
    data: (data.value?.items || []).map((item) => ({ name: item.blogger_name, value: [item.evaluated_count, item.avg_return_ratio, item.win_rate], itemStyle: { color: item.avg_return_ratio >= 0 ? '#52725f' : '#bd674f', opacity: .75 } })),
  }],
}))

onMounted(load)
</script>

<template>
  <div>
    <PageHeader
      eyebrow="Recommendation intelligence"
      title="谁的判断，经得起时间检验？"
      description="以真实推荐事件为起点，用 T+1 开盘入场和固定交易日窗口回看表现。未到期、缺行情与未识别标的不进入胜率分母。"
    >
      <template #actions>
        <button class="button" type="button" :disabled="loading" @click="load">
          <RefreshCw :size="16" :class="{ spin: loading }" />刷新数据
        </button>
      </template>
    </PageHeader>

    <section class="filter-bar">
      <div class="field">
        <label>评价窗口</label>
        <div class="segmented">
          <button v-for="value in [5, 10, 30, 90]" :key="value" type="button" :class="{ active: windowDays === value }" @click="windowDays = value; load()">{{ value }} 日</button>
        </div>
      </div>
      <div class="field"><label>推荐开始</label><input v-model="dateFrom" type="date" /></div>
      <div class="field"><label>推荐结束</label><input v-model="dateTo" type="date" /></div>
      <div class="field"><label>市场</label><select v-model="market"><option value="">全部</option><option value="SH">沪市</option><option value="SZ">深市</option><option value="BJ">北交所</option></select></div>
      <div class="field"><label>资产</label><select v-model="assetType"><option value="">全部</option><option value="STOCK">A 股</option><option value="ETF">ETF</option></select></div>
      <div class="field"><label>方向</label><select v-model="direction"><option value="">全部</option><option value="LONG">做多</option><option value="SHORT">做空</option></select></div>
      <div class="field"><label>最少样本</label><input v-model.number="minSampleCount" type="number" min="0" max="1000" /></div>
      <div class="field"><label>排序</label><select v-model="sort"><option value="win_rate">胜率</option><option value="avg_return">平均收益</option><option value="performance_score">综合分</option><option value="sample_count">样本数</option></select></div>
      <button class="button primary" type="button" @click="load">应用筛选</button>
    </section>

    <div v-if="error" class="error-banner">{{ error }}</div>

    <template v-if="data">
      <section class="metric-grid">
        <MetricCard label="覆盖博主" :value="formatNumber(data.overview.total_bloggers, 0)" note="当前筛选范围" :icon="UsersRound" tone="accent" />
        <MetricCard label="可评估推荐" :value="formatNumber(data.overview.evaluated_recommendations, 0)" note="只计 READY 窗口" :icon="Activity" />
        <MetricCard label="总体胜率" :value="formatPercent(data.overview.average_win_rate)" note="按全部可评估样本加权" :icon="CircleGauge" :tone="data.overview.average_win_rate >= .5 ? 'positive' : 'negative'" />
        <MetricCard label="平均方向收益" :value="formatPercent(data.overview.average_return_ratio)" note="LONG / SHORT 统一口径" :icon="ArrowRight" :tone="data.overview.average_return_ratio >= 0 ? 'positive' : 'negative'" />
        <MetricCard label="等待与缺口" :value="formatNumber(data.overview.pending_recommendations + data.overview.incomplete_recommendations, 0)" :note="`${data.overview.pending_recommendations} 未到期 · ${data.overview.incomplete_recommendations} 不完整`" :icon="Clock3" />
      </section>

      <section v-if="data.items.length" class="content-grid">
        <ChartPanel class="span-6" title="胜率领先者" description="横向比较 Top 10，样本数仍需结合表格判断。" :option="winRateOption" />
        <ChartPanel class="span-6" title="平均方向收益" description="正负收益分色，避免只看胜率忽略盈亏幅度。" :option="returnOption" />
        <ChartPanel class="span-12" title="样本可靠性地图" description="横轴为可评估样本，纵轴为平均收益，点面积映射胜率。" :option="scatterOption" :height="350" />

        <article class="panel span-12">
          <header class="panel-header"><div><h2>博主表现榜</h2><p>综合展示收益、胜率、样本与回撤，不把待评估样本混入分母。</p></div><span class="muted">{{ windowDays }} 个交易日</span></header>
          <div class="table-wrap">
            <table class="data-table">
              <thead><tr><th>排名</th><th>博主</th><th>综合分</th><th>样本</th><th>胜率</th><th>平均收益</th><th>中位收益</th><th>最佳 / 最差</th><th>平均回撤</th><th>待评估</th></tr></thead>
              <tbody>
                <tr v-for="item in data.items" :key="item.blogger_id" class="clickable" @click="router.push(`/bloggers/${item.blogger_id}`)">
                  <td><span class="rank-number" :class="{ top: item.rank <= 3 }">{{ item.rank }}</span></td>
                  <td><div class="identity-cell"><span class="avatar">{{ item.blogger_name.slice(0, 1) }}</span><span><strong>{{ item.blogger_name }}</strong><small>{{ item.institution || '独立研究者' }}</small></span></div></td>
                  <td><strong>{{ formatNumber(item.performance_score, 1) }}</strong></td>
                  <td>{{ item.evaluated_count }} <small class="muted">/ {{ item.sample_count }}</small></td>
                  <td><span class="return-value" :class="item.win_rate >= .5 ? 'positive' : 'negative'">{{ formatPercent(item.win_rate) }}</span></td>
                  <td><span class="return-value" :class="returnTone(item.avg_return_ratio)">{{ formatPercent(item.avg_return_ratio) }}</span></td>
                  <td><span class="return-value" :class="returnTone(item.median_return_ratio)">{{ formatPercent(item.median_return_ratio) }}</span></td>
                  <td><span class="return-value positive">{{ formatPercent(item.best_return_ratio) }}</span> <span class="muted">/</span> <span class="return-value negative">{{ formatPercent(item.worst_return_ratio) }}</span></td>
                  <td><span class="return-value negative">{{ formatPercent(item.avg_max_drawdown_ratio) }}</span></td>
                  <td>{{ item.pending_count + item.incomplete_count }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </article>
      </section>
      <StatePanel v-else :loading="loading" />
    </template>
    <StatePanel v-else :loading="loading" />
  </div>
</template>
