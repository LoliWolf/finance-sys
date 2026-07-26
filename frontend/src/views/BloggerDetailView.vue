<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ArrowLeft, BarChart2, CalendarRange, Layers3, RefreshCw } from 'lucide-vue-next'
import { api } from '../api/client'
import type { BloggerSummaryResponse, BloggerTimeseriesResponse, RecommendationPerformanceList } from '../api/types'
import ChartPanel from '../components/ChartPanel.vue'
import MetricCard from '../components/MetricCard.vue'
import StatePanel from '../components/StatePanel.vue'
import StatusBadge from '../components/StatusBadge.vue'
import { formatDate, formatNumber, formatPercent, returnTone, truncate } from '../utils/format'

const route = useRoute()
const router = useRouter()
const bloggerID = computed(() => Number(route.params.id))
const loading = ref(true)
const error = ref('')
const summary = ref<BloggerSummaryResponse | null>(null)
const timeseries = ref<BloggerTimeseriesResponse | null>(null)
const recommendations = ref<RecommendationPerformanceList | null>(null)
const windowDays = ref(30)
const dateFrom = ref('')
const dateTo = ref('')

async function load() {
  loading.value = true
  error.value = ''
  const query = { date_from: dateFrom.value, date_to: dateTo.value }
  try {
    const [summaryData, timeseriesData, recommendationData] = await Promise.all([
      api.bloggerSummary(bloggerID.value, query),
      api.bloggerTimeseries(bloggerID.value, { ...query, window_days: windowDays.value }),
      api.bloggerRecommendations(bloggerID.value, { ...query, window_days: windowDays.value, limit: 100 }),
    ])
    summary.value = summaryData
    timeseries.value = timeseriesData
    recommendations.value = recommendationData
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : '博主详情加载失败'
  } finally {
    loading.value = false
  }
}

const currentWindow = computed(() => summary.value?.windows.find((item) => item.window_days === windowDays.value))
const windowOption = computed(() => ({
  tooltip: { trigger: 'axis', backgroundColor: '#2e2b26', borderWidth: 0, textStyle: { color: '#fff8ef' } },
  legend: { bottom: 5, textStyle: { color: '#766e64', fontSize: 10 } },
  grid: { left: 48, right: 48, top: 28, bottom: 54 },
  xAxis: { type: 'category', data: summary.value?.windows.map((item) => `${item.window_days}日`) || [], axisLine: { lineStyle: { color: '#d6cec2' } }, axisLabel: { color: '#6d655c' } },
  yAxis: [
    { type: 'value', max: 1, axisLabel: { formatter: (value: number) => `${Math.round(value * 100)}%`, color: '#776f65' }, splitLine: { lineStyle: { color: 'rgba(55,45,35,.08)' } } },
    { type: 'value', axisLabel: { formatter: (value: number) => `${Math.round(value * 100)}%`, color: '#776f65' }, splitLine: { show: false } },
  ],
  series: [
    { name: '胜率', type: 'bar', data: summary.value?.windows.map((item) => item.win_rate) || [], barWidth: 28, itemStyle: { color: '#b95e43', borderRadius: [5, 5, 0, 0] } },
    { name: '平均收益', type: 'line', yAxisIndex: 1, data: summary.value?.windows.map((item) => item.avg_return_ratio) || [], smooth: true, symbolSize: 8, lineStyle: { color: '#4f7160', width: 2 }, itemStyle: { color: '#4f7160' } },
  ],
}))

const trendOption = computed(() => ({
  tooltip: { trigger: 'axis', backgroundColor: '#2e2b26', borderWidth: 0, textStyle: { color: '#fff8ef' } },
  legend: { bottom: 4, textStyle: { color: '#766e64', fontSize: 10 } },
  grid: { left: 48, right: 48, top: 28, bottom: 52 },
  xAxis: { type: 'category', data: timeseries.value?.items.map((item) => item.period) || [], boundaryGap: false, axisLine: { lineStyle: { color: '#d6cec2' } }, axisLabel: { color: '#6d655c' } },
  yAxis: [
    { type: 'value', min: 0, max: 1, axisLabel: { formatter: (value: number) => `${Math.round(value * 100)}%`, color: '#776f65' }, splitLine: { lineStyle: { color: 'rgba(55,45,35,.08)' } } },
    { type: 'value', axisLabel: { formatter: (value: number) => `${Math.round(value * 100)}%`, color: '#776f65' }, splitLine: { show: false } },
  ],
  series: [
    { name: '月度胜率', type: 'line', data: timeseries.value?.items.map((item) => item.win_rate) || [], smooth: true, symbolSize: 7, areaStyle: { color: 'rgba(185,94,67,.08)' }, lineStyle: { color: '#b95e43', width: 2 }, itemStyle: { color: '#b95e43' } },
    { name: '月均收益', type: 'line', yAxisIndex: 1, data: timeseries.value?.items.map((item) => item.avg_return_ratio) || [], smooth: true, symbolSize: 7, lineStyle: { color: '#4f7160', width: 2 }, itemStyle: { color: '#4f7160' } },
  ],
}))

watch(windowDays, load)
onMounted(load)
</script>

<template>
  <div>
    <button class="button" type="button" style="margin-bottom: 22px" @click="router.push('/')"><ArrowLeft :size="15" />返回榜单</button>
    <section v-if="summary" class="detail-hero">
      <span class="kicker">Blogger performance dossier</span>
      <h1>{{ summary.blogger_name }}</h1>
      <p>{{ summary.institution || '独立研究者' }} · 这里呈现的是推荐后的真实窗口表现，不评价内容风格，只检验可复现的事实与结果。</p>
      <div class="detail-meta"><span>博主 ID {{ summary.blogger_id }}</span><span>四窗口横向对照</span><span>不完整样本单独计数</span></div>
    </section>
    <StatePanel v-else-if="loading" loading />

    <div v-if="error" class="error-banner">{{ error }}</div>

    <template v-if="summary && currentWindow">
      <section class="filter-bar">
        <div class="field"><label>观察窗口</label><div class="segmented"><button v-for="value in [5, 10, 30, 90]" :key="value" type="button" :class="{ active: windowDays === value }" @click="windowDays = value">{{ value }} 日</button></div></div>
        <div class="field"><label>推荐开始</label><input v-model="dateFrom" type="date" /></div>
        <div class="field"><label>推荐结束</label><input v-model="dateTo" type="date" /></div>
        <button class="button" type="button" @click="load"><RefreshCw :size="15" />应用日期</button>
      </section>

      <section class="metric-grid">
        <MetricCard label="可评估样本" :value="formatNumber(currentWindow.evaluated_count, 0)" :note="`总样本 ${currentWindow.sample_count}`" :icon="Layers3" tone="accent" />
        <MetricCard label="窗口胜率" :value="formatPercent(currentWindow.win_rate)" :note="`${currentWindow.win_count} 次正向`" :icon="BarChart2" :tone="currentWindow.win_rate >= .5 ? 'positive' : 'negative'" />
        <MetricCard label="平均收益" :value="formatPercent(currentWindow.avg_return_ratio)" note="方向收益均值" :tone="currentWindow.avg_return_ratio >= 0 ? 'positive' : 'negative'" />
        <MetricCard label="中位收益" :value="formatPercent(currentWindow.median_return_ratio)" note="降低极值干扰" :tone="currentWindow.median_return_ratio >= 0 ? 'positive' : 'negative'" />
        <MetricCard label="等待 / 缺口" :value="formatNumber(currentWindow.pending_count + currentWindow.incomplete_count, 0)" :note="`${currentWindow.pending_count} 未到期 · ${currentWindow.incomplete_count} 不完整`" :icon="CalendarRange" />
      </section>

      <section class="content-grid">
        <ChartPanel class="span-6" title="四窗口能力曲线" description="胜率与平均收益使用双轴，对比短中长期稳定性。" :option="windowOption" />
        <ChartPanel class="span-6" title="月度表现趋势" :description="`${windowDays} 日窗口，按推荐月份聚合。`" :option="trendOption" />

        <article class="panel span-12">
          <header class="panel-header"><div><h2>推荐明细</h2><p>点击任意一条，查看入场、退出、最佳点、最不利点和原始证据。</p></div><span class="muted">{{ recommendations?.total || 0 }} 条</span></header>
          <div v-if="recommendations?.items.length" class="table-wrap">
            <table class="data-table">
              <thead><tr><th>推荐日</th><th>标的</th><th>观点摘要</th><th>状态</th><th>收益</th><th>最大浮盈</th><th>最大不利</th><th>最大回撤</th><th>入场 / 退出</th></tr></thead>
              <tbody>
                <tr v-for="item in recommendations.items" :key="item.recommendation_event_id" class="clickable" @click="router.push(`/recommendations/${item.recommendation_event_id}`)">
                  <td>{{ formatDate(item.recommend_date) }}</td>
                  <td><div class="identity-cell"><span class="avatar">{{ item.security_name?.slice(0, 1) || '?' }}</span><span><strong>{{ item.security_name || item.symbol }}</strong><small class="mono">{{ item.ts_code || item.symbol }}</small></span></div></td>
                  <td style="max-width: 280px; white-space: normal">{{ truncate(item.thesis, 55) }}</td>
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
          <StatePanel v-else :loading="loading" title="当前窗口暂无推荐" />
        </article>
      </section>
    </template>
  </div>
</template>
