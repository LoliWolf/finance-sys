<script setup lang="ts">
import { LoaderCircle, Telescope } from 'lucide-vue-next'

withDefaults(defineProps<{
  loading?: boolean
  title?: string
  description?: string
}>(), {
  loading: false,
  title: '暂时没有数据',
  description: '调整筛选条件，或先在分析工作台创建评价任务。',
})
</script>

<template>
  <div class="state-panel">
    <LoaderCircle v-if="loading" class="spin" :size="28" />
    <Telescope v-else :size="30" />
    <strong>{{ loading ? '正在汇总真实数据' : title }}</strong>
    <p>{{ loading ? '评价窗口较多时需要一点时间。' : description }}</p>
  </div>
</template>
