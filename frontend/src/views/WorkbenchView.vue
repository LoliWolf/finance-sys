<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Archive, FileUp, LoaderCircle, Play, RefreshCw, Settings2 } from 'lucide-vue-next'
import { api } from '../api/client'
import type { DocumentRecord, EvaluationRun } from '../api/types'
import PageHeader from '../components/PageHeader.vue'
import StatePanel from '../components/StatePanel.vue'
import StatusBadge from '../components/StatusBadge.vue'
import { formatDateTime, formatNumber } from '../utils/format'

const selectedFile = ref<File | null>(null)
const title = ref('')
const author = ref('')
const institution = ref('')
const reportDate = ref(new Date().toISOString().slice(0, 10))
const pdfUseOCR = ref(false)
const forceAnalyze = ref(false)
const uploading = ref(false)
const uploadResult = ref('')
const error = ref('')
const documents = ref<DocumentRecord[]>([])
const runs = ref<EvaluationRun[]>([])
const runsLoading = ref(true)
const creatingRun = ref(false)
const dateFrom = ref('2026-02-01')
const dateTo = ref('2026-07-22')
const includeNeedsReview = ref(true)
const forceRebuild = ref(false)

function chooseFile(event: Event) {
  const input = event.target as HTMLInputElement
  selectedFile.value = input.files?.[0] || null
  if (selectedFile.value && !title.value) title.value = selectedFile.value.name.replace(/\.[^.]+$/, '')
}

async function upload() {
  if (!selectedFile.value) {
    error.value = '请先选择一份可提取文本的研报或研究文档。'
    return
  }
  uploading.value = true
  error.value = ''
  uploadResult.value = ''
  try {
    const form = new FormData()
    form.append('file', selectedFile.value)
    form.append('title', title.value)
    form.append('author', author.value)
    form.append('institution', institution.value)
    form.append('trade_date', reportDate.value)
    form.append('pdf_use_ocr', String(pdfUseOCR.value))
    form.append('force_analyze', String(forceAnalyze.value))
    const result = await api.uploadDocument(form)
    uploadResult.value = JSON.stringify(result, null, 2)
    await loadDocuments()
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : '文档上传分析失败'
  } finally {
    uploading.value = false
  }
}

async function createRun() {
  creatingRun.value = true
  error.value = ''
  try {
    await api.createEvaluationRun({
      date_from: dateFrom.value,
      date_to: dateTo.value,
      windows: [5, 10, 30, 90],
      only_active: !includeNeedsReview.value,
      force_rebuild: forceRebuild.value,
    })
    await loadRuns()
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : '评价任务创建失败'
  } finally {
    creatingRun.value = false
  }
}

async function loadRuns() {
  runsLoading.value = true
  try {
    runs.value = (await api.evaluationRuns({ limit: 30 })).items
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : '任务列表加载失败'
  } finally {
    runsLoading.value = false
  }
}

async function loadDocuments() {
  try {
    documents.value = await api.documents()
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : '文档列表加载失败'
  }
}

async function refreshAll() {
	await Promise.all([loadRuns(), loadDocuments()])
}

const latestRun = computed(() => runs.value[0])
function progress(run: EvaluationRun) {
  if (!run.target_event_count) return run.status === 'SUCCEEDED' ? 100 : 0
  return Math.min(100, Math.round(run.evaluated_event_count / run.target_event_count * 100))
}

onMounted(async () => {
  await Promise.all([loadRuns(), loadDocuments()])
})
</script>

<template>
  <div>
    <PageHeader
      eyebrow="Research operations"
      title="从一篇文档，到一条可审计的表现链路。"
      description="上传研究材料会进入解析、结构化抽取、标的解析和推荐事实链路；评价任务再把历史行情转成固定窗口指标。所有中间件均使用 Nacos 中的远程配置。"
    >
      <template #actions><button class="button" type="button" @click="refreshAll"><RefreshCw :size="15" />刷新工作台</button></template>
    </PageHeader>

    <div v-if="error" class="error-banner">{{ error }}</div>

    <section class="content-grid">
      <article class="panel span-7">
        <header class="panel-header"><div><h2>上传并分析文档</h2><p>支持 PDF、Word、TXT、Markdown 与 CSV；重复文件按 SHA256 去重。</p></div><FileUp :size="19" /></header>
        <div class="prose">
          <label class="upload-drop">
            <FileUp :size="32" stroke-width="1.4" />
            <strong>{{ selectedFile?.name || '选择一份测试研报' }}</strong>
            <span>{{ selectedFile ? `${formatNumber(selectedFile.size / 1024, 1)} KB` : '点击选择文件；PDF 默认先提取文本，需要时开启 OCR' }}</span>
            <input type="file" accept=".pdf,.doc,.docx,.txt,.md,.csv" @change="chooseFile" />
          </label>
          <div class="form-grid" style="margin-top:16px">
            <div class="field full"><label>显示标题</label><input v-model="title" placeholder="默认使用文件名" /></div>
            <div class="field"><label>作者（可选）</label><input v-model="author" placeholder="分析层会从正文抽取" /></div>
            <div class="field"><label>机构（可选）</label><input v-model="institution" placeholder="分析层会从正文抽取" /></div>
            <div class="field full"><label>报告 / 推荐日期</label><input v-model="reportDate" type="date" /><small class="muted">用于确定推荐事实日期和后续 T+1 评价起点。</small></div>
            <label class="toggle-row full"><input v-model="pdfUseOCR" type="checkbox" />PDF 文本不足时启用 OCR</label>
            <label class="toggle-row full"><input v-model="forceAnalyze" type="checkbox" />重复文件也重新分析（调整日期或解析逻辑后使用）</label>
            <button class="button primary full" type="button" :disabled="uploading || !selectedFile" @click="upload">
              <LoaderCircle v-if="uploading" class="spin" :size="16" /><Play v-else :size="16" />{{ uploading ? '正在执行完整分析链路…' : '上传并开始分析' }}
            </button>
          </div>
          <pre v-if="uploadResult" class="result-box" style="margin-top:16px">{{ uploadResult }}</pre>
        </div>
      </article>

      <article class="panel span-5">
        <header class="panel-header"><div><h2>创建表现评价任务</h2><p>任务异步执行，不阻塞页面请求。</p></div><Settings2 :size="19" /></header>
        <div class="prose">
          <div class="form-grid">
            <div class="field"><label>推荐开始</label><input v-model="dateFrom" type="date" /></div>
            <div class="field"><label>推荐结束</label><input v-model="dateTo" type="date" /></div>
            <label class="toggle-row full"><input v-model="includeNeedsReview" type="checkbox" />包含历史待复核推荐（用于完整半年统计）</label>
            <label class="toggle-row full"><input v-model="forceRebuild" type="checkbox" />强制重算已有 READY 窗口</label>
            <button class="button primary full" type="button" :disabled="creatingRun" @click="createRun"><LoaderCircle v-if="creatingRun" class="spin" :size="16" /><Play v-else :size="16" />创建 5 / 10 / 30 / 90 日评价任务</button>
          </div>

          <div v-if="latestRun" style="margin-top:22px;padding:17px;border-radius:13px;background:#eee7dc">
            <div style="display:flex;justify-content:space-between;align-items:center;gap:12px"><strong>最近任务 #{{ latestRun.id }}</strong><StatusBadge :status="latestRun.status" /></div>
            <div style="height:7px;margin-top:14px;border-radius:999px;background:#d8cfc2;overflow:hidden"><div :style="{ width: `${progress(latestRun)}%`, height: '100%', background: '#b95e43', borderRadius: 'inherit', transition: 'width .3s ease' }" /></div>
            <p style="margin-top:9px">{{ latestRun.evaluated_event_count }} / {{ latestRun.target_event_count }} 条推荐 · {{ latestRun.window_metric_count }} 个窗口指标</p>
          </div>
        </div>
      </article>

      <article class="panel span-7">
        <header class="panel-header"><div><h2>评价任务记录</h2><p>任务进度、等待样本、行情缺口和错误摘要。</p></div><button class="icon-button" type="button" @click="loadRuns"><RefreshCw :size="15" /></button></header>
        <StatePanel v-if="runsLoading" loading />
        <div v-else class="table-wrap">
          <table class="data-table">
            <thead><tr><th>任务</th><th>状态</th><th>事件进度</th><th>窗口</th><th>未到期</th><th>缺口</th><th>失败</th><th>创建时间</th></tr></thead>
            <tbody>
              <tr v-for="run in runs" :key="run.id"><td><strong>#{{ run.id }}</strong><br/><small class="muted">{{ run.run_type }}</small></td><td><StatusBadge :status="run.status" /></td><td>{{ run.evaluated_event_count }} / {{ run.target_event_count }}</td><td>{{ run.window_metric_count }}</td><td>{{ run.pending_count }}</td><td>{{ run.incomplete_count }}</td><td>{{ run.failed_count }}</td><td>{{ formatDateTime(run.queued_at) }}</td></tr>
            </tbody>
          </table>
        </div>
      </article>

      <article class="panel span-5">
        <header class="panel-header"><div><h2>最近文档</h2><p>上传、去重与分析状态。</p></div><Archive :size="18" /></header>
        <div v-if="documents.length" style="max-height:430px;overflow:auto">
          <div v-for="document in documents.slice(0, 20)" :key="document.id" class="prose" style="padding:16px 20px;border-bottom:1px solid rgba(56,48,39,.09)">
            <div style="display:flex;justify-content:space-between;gap:12px"><h3 style="font-size:15px">{{ document.title || document.file_name }}</h3><StatusBadge :status="document.status" /></div>
            <p>#{{ document.id }} · {{ document.file_name }}<br/>{{ formatDateTime(document.created_at) }}</p>
          </div>
        </div>
        <StatePanel v-else title="尚无文档记录" />
      </article>
    </section>
  </div>
</template>
