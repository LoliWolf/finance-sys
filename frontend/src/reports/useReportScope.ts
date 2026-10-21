import { computed, ref, toValue, type MaybeRefOrGetter } from 'vue'
import type { Query } from '../api/client'

export function queryKey(query: Query) {
  return JSON.stringify(Object.entries(query).filter(([,v]) => v !== '' && v !== null && v !== undefined).sort(([a],[b]) => a.localeCompare(b)).map(([k,v]) => [k,String(v)]))
}

// Call accept with the exact query that produced the displayed data, not draft inputs.
export function useReportScope(query: MaybeRefOrGetter<Query>) {
  const applied = ref<Query | null>(null)
  const dirty = computed(() => applied.value === null || queryKey(toValue(query)) !== queryKey(applied.value))
  function accept(value: Query) { applied.value = { ...value } }
  return { applied, dirty, accept }
}
