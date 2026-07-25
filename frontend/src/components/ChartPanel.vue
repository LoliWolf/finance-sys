<script setup lang="ts">
import { BarChart, CandlestickChart, LineChart, ScatterChart } from 'echarts/charts'
import {
  DataZoomComponent,
  DatasetComponent,
  GridComponent,
  LegendComponent,
  MarkLineComponent,
  MarkPointComponent,
  TitleComponent,
  ToolboxComponent,
  TooltipComponent,
} from 'echarts/components'
import { use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import VChart from 'vue-echarts'

withDefaults(defineProps<{
  title: string
  description?: string
  option: Record<string, unknown>
  height?: number
}>(), {
  description: '',
  height: 320,
})

use([
  CanvasRenderer,
  BarChart,
  CandlestickChart,
  LineChart,
  ScatterChart,
  DataZoomComponent,
  DatasetComponent,
  GridComponent,
  LegendComponent,
  MarkLineComponent,
  MarkPointComponent,
  TitleComponent,
  ToolboxComponent,
  TooltipComponent,
])
</script>

<template>
  <article class="panel chart-panel">
    <header class="panel-header">
      <div>
        <h2>{{ title }}</h2>
        <p v-if="description">{{ description }}</p>
      </div>
      <slot name="actions" />
    </header>
    <VChart class="chart" :option="option" :style="{ height: `${height}px` }" autoresize />
  </article>
</template>
