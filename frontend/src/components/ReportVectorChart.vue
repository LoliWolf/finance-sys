<script setup lang="ts">
import { computed } from 'vue'
import type { ReportChart } from '../reports/types'
import { formatNumber, formatPercent } from '../utils/format'
const props = defineProps<{ chart: ReportChart }>()
const points = computed(() => props.chart.points.map(p => ({ ...p, value: p.value != null && Number.isFinite(p.value) ? p.value : null })))
const bounds = computed(() => {
  const values = points.value.flatMap(p => p.value == null ? [] : [p.value])
  const min = props.chart.type === 'bar' ? Math.min(0,...values) : Math.min(...values)
  const max = props.chart.type === 'bar' ? Math.max(0,...values) : Math.max(...values)
  return { min: Number.isFinite(min) ? min : 0, max: Number.isFinite(max) && max !== min ? max : (Number.isFinite(min) ? min + 1 : 1) }
})
const x = (i: number) => 64 + i * 604 / Math.max(1, points.value.length - 1)
const y = (v: number) => 198 - (v - bounds.value.min) * 170 / (bounds.value.max - bounds.value.min)
const label = (v: number) => props.chart.percent ? formatPercent(v) : formatNumber(v)
// Null values break the line rather than creating an invented zero-price observation.
const segments = computed(() => {
  const result: string[] = []; let current: string[] = []
  points.value.forEach((p,i) => { if (p.value == null) { if(current.length) result.push(current.join(' ')); current=[] } else current.push(`${x(i)},${y(p.value)}`) })
  if(current.length) result.push(current.join(' '))
  return result
})
</script>
<template>
  <figure class="report-vector-chart">
    <figcaption>{{ chart.title }}</figcaption>
    <p v-if="!points.some(p => p.value !== null)" class="muted">暂无可绘制的有效数据。</p>
    <svg v-else-if="chart.type === 'line'" viewBox="0 0 720 240" role="img" :aria-label="chart.title">
      <g v-for="n in [0,1,2]" :key="n"><line x1="64" x2="668" :y1="28 + n*85" :y2="28 + n*85" stroke="#d9d0c3"/><text x="56" :y="32+n*85" text-anchor="end">{{ label(bounds.max - n*(bounds.max-bounds.min)/2) }}</text></g>
      <polyline v-for="(segment,i) in segments" :key="i" :points="segment" fill="none" stroke="#b95e43" stroke-width="2"/>
      <template v-for="(p,i) in points" :key="i"><circle v-if="p.value !== null" :cx="x(i)" :cy="y(p.value)" r="2.3" fill="#b95e43"><title>{{ p.label }}：{{ label(p.value) }}</title></circle><text v-if="i===0 || i===points.length-1 || (points.length>4 && i===Math.floor(points.length/2))" :x="x(i)" y="221" text-anchor="middle">{{ p.label }}</text></template>
    </svg>
    <svg v-else :viewBox="`0 0 720 ${Math.max(50,points.length*28+20)}`" role="img" :aria-label="chart.title">
      <g v-for="(p,i) in points" :key="i"><text x="158" :y="i*28+21" text-anchor="end">{{ p.label.length > 15 ? p.label.slice(0,15)+'…' : p.label }}</text><template v-if="p.value !== null"><rect :x="178+(Math.min(0,p.value)-bounds.min)*400/(bounds.max-bounds.min)" :y="i*28+8" :width="Math.max(1,Math.abs(p.value)*400/(bounds.max-bounds.min))" height="15" :fill="p.value>=0?'#587765':'#b95e43'"/><text x="604" :y="i*28+21">{{ label(p.value) }}</text></template><text v-else x="604" :y="i*28+21">—</text></g>
    </svg>
  </figure>
</template>
