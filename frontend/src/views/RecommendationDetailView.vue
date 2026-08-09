<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ArrowLeft, BookText, CalendarClock, CandlestickChart, ExternalLink, Target } from 'lucide-vue-next'
import { api } from '../api/client'
import type { PriceSeriesResponse, RecommendationDetail } from '../api/types'
import ChartPanel from '../components/ChartPanel.vue'
import MetricCard from '../components/MetricCard.vue'
import StatePanel from '../components/StatePanel.vue'
import StatusBadge from '../components/StatusBadge.vue'
import { assetTypeLabel, formatDate, formatNumber, formatPercent, marketLabel, returnTone, sectorTypeLabel } from '../utils/format'

const route = useRoute()
const router = useRouter()
const eventID = computed(() => Number(route.params.id))
const loading = ref(true)
const error = ref('')
const detail = ref<RecommendationDetail | null>(null)
const series = ref<PriceSeriesResponse | null>(null)

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [detailData, seriesData] = await Promise.all([
      api.recommendationDetail(eventID.value),
      api.priceSeries(eventID.value, { days_before: 5, days_after: 90 }),
    ])
    detail.value = detailData
    series.value = seriesData
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : '推荐详情加载失败'
  } finally {
    loading.value = false
  }
}

const sortedMetrics = computed(() => [...(detail.value?.metrics || [])].sort((a, b) => a.window_days - b.window_days))
const readyMetrics = computed(() => sortedMetrics.value.filter((item) => item.status === 'READY'))
const headlineMetric = computed(() => readyMetrics.value.find((item) => item.window_days === 30) || readyMetrics.value[0])
const isSector = computed(() => detail.value?.recommendation.asset_type === 'SECTOR')
const headlineMetricLabel = computed(() => headlineMetric.value ? `${headlineMetric.value.window_days} 个交易日方向${isSector.value ? '表现' : '收益'}` : `方向${isSector.value ? '表现' : '收益'}`)
const instrumentTypeLabel = computed(() => isSector.value ? sectorTypeLabel(detail.value?.recommendation.sector_type) : assetTypeLabel(detail.value?.recommendation.asset_type))

const priceOption = computed(() => {
  const points = series.value?.items || []
  const dates = points.map((item) => item.trade_date.slice(0, 10))
  const candles = points.map((item) => [item.open_price, item.close_price, item.low_price, item.high_price])
  const closes = points.map((item) => item.close_price)
  const scatter = (series.value?.markers || []).flatMap((marker) => {
    const date = marker.trade_date?.slice(0, 10)
    const index = dates.indexOf(date || '')
    if (index < 0) return []
    const point = points[index]
    const y = marker.type === 'worst' ? point.low_price : marker.type === 'best' ? point.high_price : point.close_price
    const colors: Record<string, string> = { recommend: '#c8583d', entry: '#3f6f5a', exit: '#7558a1', best: '#b68632', worst: '#9a443f' }
    return [{
      value: [date, y],
      name: marker.label,
      itemStyle: { color: colors[marker.type] },
      label: { show: marker.type === 'recommend' || marker.type === 'exit', formatter: marker.label, position: 'top', color: colors[marker.type], fontSize: 9 },
    }]
  })
  return {
    animationDuration: 500,
    tooltip: {
      trigger: 'axis',
      axisPointer: { type: 'cross' },
      backgroundColor: '#2e2b26',
      borderWidth: 0,
      textStyle: { color: '#fff8ef', fontSize: 11 },
    },
    legend: { top: 4, right: 12, data: ['日 K', '收盘价', '评价标记'], textStyle: { color: '#746c62', fontSize: 10 } },
    grid: { left: 52, right: 22, top: 44, bottom: 66 },
    xAxis: { type: 'category', data: dates, boundaryGap: true, axisLine: { lineStyle: { color: '#d4cabd' } }, axisLabel: { color: '#756d63', fontSize: 9 } },
    yAxis: { scale: true, axisLabel: { color: '#756d63' }, splitLine: { lineStyle: { color: 'rgba(65,52,39,.08)' } } },
    dataZoom: [{ type: 'inside', start: 0, end: 100 }, { type: 'slider', height: 18, bottom: 18, borderColor: 'transparent', backgroundColor: '#eee7dc', fillerColor: 'rgba(185,94,67,.16)' }],
    series: [
      { name: '日 K', type: 'candlestick', data: candles, itemStyle: { color: '#b75545', color0: '#4a7a64', borderColor: '#b75545', borderColor0: '#4a7a64' } },
      { name: '收盘价', type: 'line', data: closes, showSymbol: false, smooth: .15, lineStyle: { color: '#37332e', width: 1.2, opacity: .65 } },
      { name: '评价标记', type: 'scatter', data: scatter, symbol: 'pin', symbolSize: 26, z: 20 },
    ],
  }
})

onMounted(load)
</script>

<template>
  <div>
    <button class="button" type="button" style="margin-bottom: 22px" @click="router.back()"><ArrowLeft :size="15" />返回上一页</button>
    <div v-if="error" class="error-banner">{{ error }}</div>
    <StatePanel v-if="loading" loading />

    <template v-if="detail">
      <section class="detail-hero">
        <span class="kicker">Recommendation #{{ detail.recommendation.recommendation_event_id }}</span>
        <h1>{{ series?.security_name || detail.recommendation.symbol }} · {{ detail.recommendation.blogger_name }}</h1>
        <p>{{ detail.recommendation.thesis || '该条推荐未记录观点摘要。' }}</p>
        <div class="detail-meta">
          <span>{{ formatDate(detail.recommendation.recommend_date) }} 推荐</span>
          <span>{{ detail.recommendation.direction }}</span>
		  <span>{{ marketLabel(detail.recommendation.market) }} · {{ instrumentTypeLabel }}</span>
          <span>置信度 {{ formatPercent(detail.recommendation.confidence) }}</span>
          <span>{{ detail.recommendation.institution || '独立研究者' }}</span>
        </div>
      </section>

      <section class="metric-grid">
		<MetricCard :label="headlineMetricLabel" :value="formatPercent(headlineMetric?.direction_return_ratio)" :note="isSector ? '下一交易日开盘至窗口终点' : 'T+1 开盘至窗口退出'" :icon="Target" tone="accent" />
        <MetricCard label="最大浮盈" :value="formatPercent(headlineMetric?.max_favorable_return_ratio)" note="窗口内最有利价格" :tone="(headlineMetric?.max_favorable_return_ratio || 0) >= 0 ? 'positive' : 'negative'" />
        <MetricCard label="最大不利波动" :value="formatPercent(headlineMetric?.max_adverse_return_ratio)" note="窗口内最不利价格" tone="negative" />
        <MetricCard label="最大回撤" :value="formatPercent(headlineMetric?.max_drawdown_ratio)" note="按方向净值序列" :icon="CandlestickChart" tone="negative" />
        <MetricCard label="可用窗口" :value="`${readyMetrics.length} / ${sortedMetrics.length}`" :note="`${sortedMetrics.filter((item) => item.status === 'PENDING').length} 个窗口未到期`" :icon="CalendarClock" />
      </section>

      <section class="content-grid">
		<ChartPanel class="span-12" :title="isSector ? '板块指数推荐前后路径' : '推荐前后价格路径'" :description="isSector ? '板块指数 K 线叠加推荐日、观察起点、各窗口终点及最佳与最不利点。' : 'K 线叠加推荐日、T+1 入场、各窗口退出、最佳与最不利点。'" :option="priceOption" :height="490" />

        <article class="panel span-12">
          <header class="panel-header"><div><h2>窗口指标明细</h2><p>同一推荐在 5 / 10 / 30 / 90 个交易日的确定性计算结果。</p></div><span class="mono">{{ series?.ts_code || detail.recommendation.symbol }}</span></header>
          <div class="table-wrap">
            <table class="data-table">
			<thead><tr><th>窗口</th><th>状态</th><th>{{ isSector ? '观察起点' : '入场' }}</th><th>{{ isSector ? '窗口终点' : '退出' }}</th><th>方向{{ isSector ? '表现' : '收益' }}</th><th>最大有利</th><th>最大不利</th><th>最大回撤</th><th>结果</th><th>行情覆盖</th></tr></thead>
              <tbody>
                <tr v-for="metric in sortedMetrics" :key="metric.window_days">
                  <td><strong>{{ metric.window_days }} 个交易日</strong></td>
                  <td><StatusBadge :status="metric.status" /></td>
                  <td>{{ formatDate(metric.entry_date) }} <small class="muted">{{ metric.entry_price == null ? '' : `@ ${formatNumber(metric.entry_price)}` }}</small></td>
                  <td>{{ formatDate(metric.exit_date) }} <small class="muted">{{ metric.exit_close_price == null ? '' : `@ ${formatNumber(metric.exit_close_price)}` }}</small></td>
                  <td><span class="return-value" :class="returnTone(metric.direction_return_ratio)">{{ formatPercent(metric.direction_return_ratio) }}</span></td>
                  <td><span class="return-value positive">{{ formatPercent(metric.max_favorable_return_ratio) }}</span></td>
                  <td><span class="return-value negative">{{ formatPercent(metric.max_adverse_return_ratio) }}</span></td>
                  <td><span class="return-value negative">{{ formatPercent(metric.max_drawdown_ratio) }}</span></td>
                  <td>{{ metric.win_flag == null ? '—' : metric.win_flag ? '胜' : '负' }}</td>
                  <td>{{ metric.actual_quote_count }} / {{ metric.expected_quote_count }} <small v-if="metric.reason_message" class="muted">{{ metric.reason_message }}</small></td>
                </tr>
              </tbody>
            </table>
          </div>
        </article>

        <article class="panel span-7">
          <header class="panel-header"><div><h2>原始观点与证据</h2><p>评价结果始终回到推荐事实，不脱离上下文解读。</p></div><BookText :size="18" /></header>
          <div class="prose">
            <h3>观点摘要</h3>
            <p>{{ detail.recommendation.thesis || '未记录' }}</p>
            <div class="evidence-list">
              <div v-for="evidence in detail.recommendation.evidence" :key="`${evidence.chunk_index}-${evidence.text}`" class="evidence-item">
                <small>Evidence chunk {{ evidence.chunk_index }}</small>{{ evidence.text }}
              </div>
              <p v-if="!detail.recommendation.evidence.length" class="muted">没有单独存储的证据片段。</p>
            </div>
          </div>
        </article>

        <article class="panel span-5">
          <header class="panel-header"><div><h2>来源文档</h2><p>用于审计推荐事实的原始载体。</p></div><ExternalLink :size="18" /></header>
          <div class="prose">
            <h3>{{ detail.recommendation.document_title || detail.recommendation.document_file_name || '未命名文档' }}</h3>
            <p>文件：{{ detail.recommendation.document_file_name || '—' }}<br />文档 ID：{{ detail.recommendation.source_document_id }}<br />推荐状态：{{ detail.recommendation.recommendation_status }}</p>
            <div class="insight-note" style="margin-top: 20px">结果只描述推荐后的历史市场表现，不构成任何投资建议。</div>
          </div>
        </article>
      </section>
    </template>
  </div>
</template>
