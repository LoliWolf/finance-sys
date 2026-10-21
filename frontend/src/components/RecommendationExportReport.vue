<script setup lang="ts">
import { computed } from 'vue'
import type { RecommendationExportReport, RecommendationWindowReturn } from '../api/types'
import MetricCard from './MetricCard.vue'
import { assetTypeLabel, marketLabel, formatDate, formatNumber, formatPercent, returnTone } from '../utils/format'

const props = defineProps<{ report: RecommendationExportReport }>()
const stateLabels: Record<string, string> = { READY: '已有结果', PENDING: '尚未到期', INCOMPLETE: '行情不全', NO_SECURITY: '未识别', UNSUPPORTED: '暂不支持', FAILED: '评估失败', NOT_EVALUATED: '尚未评估', OUTDATED: '版本过期' }
const filterLabels: Record<string, string> = { blogger_name: '博主姓名', blogger_id: '博主 ID', date_from: '推荐开始', date_to: '推荐结束', market: '市场', asset_type: '资产', direction: '方向', status: '窗口状态（任一）', symbol: '标的代码', ts_code: '证券代码' }
function filterValue(key: string, value: string) {
  if (key === 'market') return marketLabel(value)
  if (key === 'asset_type') return assetTypeLabel(value)
  if (key === 'direction') return value === 'SHORT' ? '看空' : '看多'
  if (key === 'status') return stateLabels[value] || value
  return value
}
const filters = computed(() => Object.keys(filterLabels).filter((key) => props.report.filters[key]).map((key) => ({ label: filterLabels[key], value: filterValue(key, props.report.filters[key]) })))
const generatedAt = computed(() => new Intl.DateTimeFormat('zh-CN', { timeZone: 'Asia/Shanghai', dateStyle: 'medium', timeStyle: 'medium' }).format(new Date(props.report.generated_at)))
function visibleReturn(metric?: RecommendationWindowReturn) {
  return metric?.status === 'READY' ? metric.return_ratio : null
}
function stateLabel(metric: RecommendationWindowReturn) {
  return stateLabels[metric.status] || '尚未评估'
}
</script>

<template>
  <section class="print-only document-report-detail recommendation-export-report" aria-label="推荐明细导出报告">
    <section class="detail-hero report-cover">
      <span class="kicker">研迹 · Evidence ledger report</span>
      <h1>推荐表现追踪报告</h1>
      <p>将当前筛选范围内的推荐事实与后续市场表现汇集成册。本报告包含全部 {{ report.total }} 条匹配推荐，不限于列表当前页。</p>
      <div class="detail-meta"><span>行情截至 {{ formatDate(report.data_as_of) }}</span><span>{{ report.quote_source }}</span><span>生成于 {{ generatedAt }}（北京时间）</span></div>
    </section>

    <section class="report-export-filters">
      <h2>本次筛选范围</h2>
      <dl v-if="filters.length"><div v-for="entry in filters" :key="entry.label"><dt>{{ entry.label }}</dt><dd>{{ entry.value }}</dd></div></dl>
      <p v-else>未设置筛选条件：全部推荐。</p>
      <p>各窗口展示与推荐明细列表一致的标的原始涨跌幅。看空方向不会将涨跌幅反转，也不代表真实账户收益。</p>
    </section>

    <section class="metric-grid report-summary-grid">
      <MetricCard label="推荐事实" :value="formatNumber(report.total, 0)" note="本次筛选全部结果" tone="accent" />
      <MetricCard label="涉及博主" :value="formatNumber(report.blogger_count, 0)" note="按博主 ID 去重" />
      <MetricCard label="涉及标的" :value="formatNumber(report.security_count, 0)" note="按代码与市场去重" />
      <MetricCard label="追踪窗口" :value="`${report.windows.length} 组`" :note="report.windows.join(' / ') + ' 交易日'" />
      <MetricCard label="行情截止" :value="formatDate(report.data_as_of)" note="本地已有行情" />
    </section>

    <article class="panel report-window-summary">
      <header class="panel-header"><div><h2>筛选范围内的窗口概览</h2><p>上涨占比与平均涨跌幅只统计 READY 且具有有效数值的样本；不是方向胜率或资金收益率。</p></div></header>
      <table class="data-table"><thead><tr><th>窗口</th><th>样本</th><th>可评估</th><th>未到期</th><th>暂无结果</th><th>上涨占比</th><th>平均涨跌幅</th></tr></thead>
        <tbody><tr v-for="window in report.summary" :key="window.window_days"><td>{{ window.window_days }} 交易日</td><td>{{ window.sample_count }}</td><td>{{ window.evaluated_count }}</td><td>{{ window.pending_count }}</td><td>{{ window.unavailable_count }}</td><td>{{ formatPercent(window.rise_rate) }}</td><td :class="returnTone(window.avg_return_ratio)">{{ formatPercent(window.avg_return_ratio) }}</td></tr></tbody>
      </table>
    </article>

    <article class="panel report-export-bloggers">
      <header class="panel-header"><div><h2>按博主汇总</h2><p>只统计本次筛选内的推荐，不混入筛选范围外的历史记录。各窗口列为平均涨跌幅。</p></div></header>
      <table class="data-table"><thead><tr><th>博主</th><th>推荐数</th><th v-for="window in report.windows" :key="window">{{ window }} 交易日</th></tr></thead>
        <tbody><tr v-for="blogger in report.bloggers" :key="blogger.blogger_id"><td>{{ blogger.blogger_name }}<br/><small>{{ blogger.institution || '独立研究者' }}</small></td><td>{{ blogger.recommendation_count }}</td><td v-for="window in blogger.windows" :key="window.window_days"><span :class="returnTone(window.avg_return_ratio)">{{ formatPercent(window.avg_return_ratio) }}</span><br/><small>{{ window.evaluated_count }} 条可评估</small></td></tr></tbody>
      </table>
    </article>

    <article class="panel report-export-details">
      <header class="panel-header"><div><h2>推荐表现明细 · 共 {{ report.total }} 条</h2><p>按推荐日期倒序排列；观点摘要完整保留。缺失、未到期与不可评估值均不填造收益。</p></div></header>
      <table class="data-table report-export-table">
        <colgroup><col style="width: 10%"/><col style="width: 15%"/><col style="width: 14%"/><col style="width: 6%"/><col v-for="window in report.windows" :key="window" :style="{ width: `${26 / report.windows.length}%` }"/><col style="width: 29%"/></colgroup>
        <thead><tr><th>推荐日</th><th>博主 / 机构</th><th>标的</th><th>方向</th><th v-for="window in report.windows" :key="window">{{ window }} 交易日</th><th>观点摘要</th></tr></thead>
        <tbody><tr v-for="item in report.items" :key="item.recommendation_event_id"><td>{{ formatDate(item.recommend_date) }}<br/><small>#{{ item.recommendation_event_id }}</small></td><td>{{ item.blogger_name }}<br/><small>{{ item.institution || '独立研究者' }}</small></td><td>{{ item.security_name || item.symbol }}<br/><small>{{ item.ts_code || item.symbol }}<br/>{{ assetTypeLabel(item.asset_type) }} · {{ marketLabel(item.market) }}</small></td><td>{{ item.direction === 'SHORT' ? '看空' : '看多' }}</td><td v-for="metric in item.windows" :key="metric.window_days"><span :class="returnTone(visibleReturn(metric))">{{ formatPercent(visibleReturn(metric)) }}</span><br/><small v-if="metric.status !== 'READY'">{{ stateLabel(metric) }}</small></td><td class="report-export-thesis">{{ item.thesis || '未记录观点摘要' }}</td></tr></tbody>
      </table>
    </article>

    <footer class="report-disclaimer">
      <strong>统计口径与声明</strong>
      <p>标的原始涨跌幅使用已保存的窗口评估结果，通常以推荐后的首个交易日开盘价与窗口终点收盘价比较。上涨占比为涨跌幅大于零的样本数 ÷ 可评估样本数，未到期或缺失样本不进入分母。本次导出未执行模型分析、行情补采或指标重算。当前计算配置：{{ report.calc_version || '未标注' }}；行情来源：{{ report.quote_source }}。</p>
      <p>本报告仅整理推荐事实和历史市场表现，不构成投资建议，不代表作者或模拟账户的实际收益，历史表现不代表未来结果。</p>
      <small>研迹 · 当前版生成于 {{ generatedAt }} · 共 {{ report.total }} 条推荐</small>
    </footer>
  </section>
</template>
