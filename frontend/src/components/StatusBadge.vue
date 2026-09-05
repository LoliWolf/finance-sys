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
  FILLED: '全部成交',
  PARTIALLY_FILLED: '部分成交',
  SUBMITTED: '已报',
  DISPATCH_PENDING: '待发送',
  BRIDGE_QUEUED: '桥接排队',
  REJECTED: '已拒绝',
  CANCELED: '已撤单',
  CANCELLED: '已撤单',
  UNKNOWN: '待确认',
  OPEN: '持有中',
  EXIT_PENDING: '待退出',
  CLOSED: '已平仓',
  VERIFIED: '已核验',
  REPORTED: '已上报',
  UNAVAILABLE: '不可用',
  INGESTED: '已入库',
  PARSED: '已解析',
  PLANNED: '已完成分析',
  INVALID: '无有效推荐',
  EMPTY: '无推荐',
  NEEDS_EVALUATION: '待补算',
  PARTIAL: '部分可用',
  NOT_EVALUATED: '尚未补算',
  OUTDATED: '版本已过期',
}
</script>

<template>
  <span class="status-badge" :class="`status-${status.toLowerCase()}`">
    <i />{{ labels[status] || status }}
  </span>
</template>
