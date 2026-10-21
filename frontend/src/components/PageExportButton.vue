<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { Download } from 'lucide-vue-next'
import { api, type Query } from '../api/client'
import { beginReportPrint } from '../utils/printReport'
import { buildPageReport } from '../reports/buildPageReport'
import { queryKey } from '../reports/useReportScope'
import type { PageReportKind, PrintablePageReport } from '../reports/types'
import PageExportReport from './PageExportReport.vue'

const props = withDefaults(defineProps<{ kind: PageReportKind; query?: Query; disabled?: boolean; dirty?: boolean; label?: string }>(), { query: () => ({}), label: '导出报告' })
const busy = ref(false)
const error = ref('')
const report = ref<PrintablePageReport | null>(null)
let controller: AbortController | undefined
let finishPrint: (()=>void) | undefined
let disposed = false
function clear() { document.body.classList.remove('page-report-printing'); report.value=null; busy.value=false; finishPrint=undefined }
function cancel() { controller?.abort(); controller=undefined; finishPrint?.(); clear() }
async function exportReport() {
  if (busy.value || props.disabled || props.dirty) return
  if (document.body.classList.contains('document-report-printing')) { error.value='请先关闭其他打印窗口'; return }
  const current = new AbortController(); controller=current
  busy.value=true; error.value=''
  try {
    const response = await api.pageExport(props.kind, { ...props.query }, current.signal)
    if (disposed || current.signal.aborted || controller!==current) return
    if (response.kind !== props.kind) throw new Error('报告类型不匹配，请刷新后重试')
    report.value=buildPageReport(response)
    await nextTick()
    if (disposed || current.signal.aborted || controller!==current) return
    document.body.classList.add('page-report-printing')
    finishPrint=beginReportPrint(`研迹_${report.value.title}_${response.generated_at.slice(0,10)}`, clear)
  } catch(cause) {
    if (disposed || current.signal.aborted || controller!==current) return
    clear(); error.value=cause instanceof Error ? cause.message : '导出失败，请重试'
  }
}
watch([()=>props.kind,()=>queryKey(props.query),()=>props.dirty],()=>{ if(busy.value) cancel(); error.value='' })
onBeforeUnmount(()=>{ disposed=true; cancel() })
</script>
<template>
  <div class="page-export-action">
    <button class="button primary" type="button" data-testid="page-export" :disabled="disabled || dirty || busy" :title="dirty ? '筛选已修改，请先应用筛选' : '生成与单篇文档同风格的报告，可在打印窗口保存 PDF'" @click="exportReport"><Download :size="15" />{{ busy ? '正在准备导出…' : label }}</button>
    <button v-if="busy" class="button" type="button" data-testid="page-export-cancel" @click="cancel">取消导出</button>
    <small v-if="dirty && !disabled" class="muted">请先应用筛选再导出</small>
    <small v-if="error" class="page-export-error" role="alert">{{ error }}</small>
    <Teleport to="body"><PageExportReport v-if="report" :report="report" /></Teleport>
  </div>
</template>
