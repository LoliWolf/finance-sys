<script setup lang="ts">
import type { PrintablePageReport } from '../reports/types'
import MetricCard from './MetricCard.vue'
import ReportVectorChart from './ReportVectorChart.vue'
defineProps<{ report: PrintablePageReport }>()
</script>
<template>
  <section class="print-only document-report-detail page-export-report" aria-label="页面导出报告">
    <header class="detail-hero report-cover"><span class="kicker">研迹 · Research dossier</span><h1>{{ report.title }}</h1><p>{{ report.subtitle }}</p><div class="detail-meta"><span v-for="item in report.meta" :key="item">{{ item }}</span><span>生成于 {{ report.generatedAt }}（北京时间）</span></div></header>
    <section class="report-export-filters"><h2>本次报告范围</h2><dl v-if="report.filters.length"><div v-for="item in report.filters" :key="item.label"><dt>{{ item.label }}</dt><dd>{{ item.value }}</dd></div></dl><p v-else>未额外限定筛选条件。</p></section>
    <section v-if="report.metrics.length" class="metric-grid report-summary-grid"><MetricCard v-for="(m,i) in report.metrics" :key="m.label" :label="m.label" :value="m.value" :note="m.note || ''" :tone="i===0?'accent':'default'" /></section>
    <article v-for="(section,i) in report.sections" :key="i" class="panel page-report-section">
      <header class="panel-header"><div><h2>{{ section.title }}</h2><p v-if="section.description">{{ section.description }}</p></div></header>
      <ReportVectorChart v-if="section.chart" :chart="section.chart" />
      <div v-if="section.paragraphs" class="page-report-prose"><p v-for="(p,j) in section.paragraphs" :key="j">{{ p }}</p></div>
      <template v-if="section.columns"><table v-if="section.rows?.length" class="data-table page-report-table"><colgroup v-if="section.widths"><col v-for="(w,j) in section.widths" :key="j" :style="{width:`${w}%`}" /></colgroup><thead><tr><th v-for="(c,j) in section.columns" :key="j">{{ c }}</th></tr></thead><tbody><tr v-for="(row,j) in section.rows" :key="j"><td v-for="(cell,k) in row" :key="k">{{ cell }}</td></tr></tbody></table><p v-else class="muted">此范围内没有记录。</p></template>
    </article>
    <footer class="report-disclaimer"><strong>统计口径与声明</strong><p v-for="item in report.statements" :key="item">{{ item }}</p><small>研迹 · {{ report.title }} · {{ report.generatedAt }}</small></footer>
  </section>
</template>
