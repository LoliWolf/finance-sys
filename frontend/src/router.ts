import { createRouter, createWebHistory } from 'vue-router'

const router = createRouter({
  history: createWebHistory(),
  scrollBehavior: () => ({ top: 0 }),
  routes: [
    {
      path: '/',
      name: 'dashboard',
      component: () => import('./views/DashboardView.vue'),
      meta: { eyebrow: 'Market intelligence', title: '博主表现总览' },
    },
    {
      path: '/bloggers/:id',
      name: 'blogger-detail',
      component: () => import('./views/BloggerDetailView.vue'),
      meta: { eyebrow: 'Blogger profile', title: '博主研究档案' },
    },
    {
      path: '/recommendations',
      name: 'recommendations',
      component: () => import('./views/RecommendationsView.vue'),
      meta: { eyebrow: 'Evidence ledger', title: '推荐表现明细' },
    },
    {
      path: '/recommendations/:id',
      name: 'recommendation-detail',
      component: () => import('./views/RecommendationDetailView.vue'),
      meta: { eyebrow: 'Recommendation trace', title: '单条推荐复盘' },
    },
    {
      path: '/securities',
      name: 'securities',
      component: () => import('./views/SecuritiesView.vue'),
      meta: { eyebrow: 'Security lens', title: '标的热度与表现' },
    },
    {
      path: '/workbench',
      name: 'workbench',
      component: () => import('./views/WorkbenchView.vue'),
      meta: { eyebrow: 'Research operations', title: '分析工作台' },
    },
    {
      path: '/simulation-trading',
      name: 'simulation-trading',
      component: () => import('./views/SimulationTradingView.vue'),
      meta: { eyebrow: 'Paper trading ledger', title: '模拟交易' },
    },
  ],
})

export default router
