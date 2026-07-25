<script setup lang="ts">
defineProps<{ status: string }>()

const labels: Record<string, string> = {
  READY: '已评估',
  PENDING: '未到期',
  INCOMPLETE: '行情不全',
  NO_SECURITY: '未识别标的',
  UNSUPPORTED: '暂不支持',
  FAILED: '计算失败',
  QUEUED: '排队中',
  RUNNING: '计算中',
  SUCCEEDED: '已完成',
  PARTIAL_FAILED: '部分失败',
  ACTIVE: '有效',
  NEEDS_REVIEW: '待复核',
}
</script>

<template>
  <span class="status-badge" :class="`status-${status.toLowerCase()}`">
    <i />{{ labels[status] || status }}
  </span>
</template>
