<script setup lang="ts">
defineProps<{
  eyebrow: string
  title: string
  description: string
}>()
</script>

<template>
  <section class="page-heading">
    <div class="page-heading-copy">
      <span class="kicker">{{ eyebrow }}</span>
      <h1>{{ title }}</h1>
      <p>{{ description }}</p>
    </div>
    <div v-if="$slots.actions" class="page-heading-actions">
      <slot name="actions" />
    </div>
  </section>
</template>
