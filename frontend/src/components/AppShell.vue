<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink, RouterView, useRoute } from 'vue-router'
import {
  BarChart3,
  BookOpenCheck,
  BriefcaseBusiness,
  ChevronRight,
  FileSearch,
  Menu,
  Orbit,
  PanelLeftClose,
  SearchCode,
  X,
} from 'lucide-vue-next'
import { api } from '../api/client'

const route = useRoute()
const mobileOpen = ref(false)
const serviceOnline = ref(false)
const checking = ref(true)

const meta = computed(() => ({
  eyebrow: String(route.meta.eyebrow || 'Research intelligence'),
  title: String(route.meta.title || '研迹'),
}))

const navigation = [
  { to: '/', label: '表现总览', caption: '榜单与全局指标', icon: BarChart3 },
  { to: '/recommendations', label: '推荐明细', caption: '逐条检验事实', icon: FileSearch },
  { to: '/securities', label: '标的视角', caption: '热度与收益分布', icon: Orbit },
  { to: '/workbench', label: '分析工作台', caption: '文档与评价任务', icon: BriefcaseBusiness },
]

onMounted(async () => {
  try {
    await api.health()
    serviceOnline.value = true
  } catch {
    serviceOnline.value = false
  } finally {
    checking.value = false
  }
})
</script>

<template>
  <div class="app-shell">
    <div v-if="mobileOpen" class="mobile-scrim" @click="mobileOpen = false" />
    <aside class="sidebar" :class="{ 'is-open': mobileOpen }">
      <div class="brand-row">
        <RouterLink class="brand" to="/" @click="mobileOpen = false">
          <span class="brand-mark"><SearchCode :size="19" /></span>
          <span>
            <strong>研迹</strong>
            <small>Evidence, then returns.</small>
          </span>
        </RouterLink>
        <button class="icon-button mobile-only" type="button" aria-label="关闭导航" @click="mobileOpen = false">
          <X :size="20" />
        </button>
      </div>

      <div class="sidebar-intro">
        <span class="kicker">Research desk</span>
        <p>把推荐事实、后续行情和可解释统计放进同一条证据链。</p>
      </div>

      <nav class="navigation" aria-label="主导航">
        <RouterLink
          v-for="item in navigation"
          :key="item.to"
          :to="item.to"
          class="nav-item"
          @click="mobileOpen = false"
        >
          <component :is="item.icon" :size="19" stroke-width="1.7" />
          <span>
            <strong>{{ item.label }}</strong>
            <small>{{ item.caption }}</small>
          </span>
          <ChevronRight :size="16" class="nav-arrow" />
        </RouterLink>
      </nav>

      <div class="sidebar-note">
        <BookOpenCheck :size="18" />
        <div>
          <strong>确定性口径</strong>
          <p>T+1 开盘入场，5 / 10 / 30 / 90 个交易日窗口。</p>
        </div>
      </div>

      <div class="service-state">
        <span class="state-dot" :class="{ online: serviceOnline, checking }" />
        <span>{{ checking ? '正在连接服务' : serviceOnline ? '数据服务在线' : '数据服务离线' }}</span>
      </div>
    </aside>

    <main class="workspace">
      <header class="topbar">
        <button class="icon-button menu-button" type="button" aria-label="打开导航" @click="mobileOpen = true">
          <Menu :size="21" />
        </button>
        <div>
          <span>{{ meta.eyebrow }}</span>
          <strong>{{ meta.title }}</strong>
        </div>
        <div class="topbar-mark">
          <PanelLeftClose :size="17" />
          <span>基于可评估样本</span>
        </div>
      </header>
      <div class="page-frame">
        <RouterView />
      </div>
    </main>
  </div>
</template>
