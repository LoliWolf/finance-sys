<script setup lang="ts">
import type { Component } from 'vue'

withDefaults(defineProps<{
  label: string
  value: string
  note?: string
  tone?: 'default' | 'accent' | 'positive' | 'negative'
  icon?: Component
}>(), {
  note: '',
  tone: 'default',
})
</script>

<template>
  <article class="metric-card" :class="`tone-${tone}`">
    <div class="metric-label">
      <component :is="icon" v-if="icon" :size="17" stroke-width="1.7" />
      <span>{{ label }}</span>
    </div>
    <strong>{{ value }}</strong>
    <p>{{ note }}</p>
  </article>
</template>
