<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import {
  ArrowDownToLine,
  ArrowUpFromLine,
  ChartCandlestick,
  CircleDollarSign,
  Landmark,
  RefreshCw,
  ShieldAlert,
  WalletCards,
} from 'lucide-vue-next'
import { api } from '../api/client'
import type { TradingDashboard } from '../api/types'
import MetricCard from '../components/MetricCard.vue'
import PageHeader from '../components/PageHeader.vue'
import StatePanel from '../components/StatePanel.vue'
import StatusBadge from '../components/StatusBadge.vue'
import { formatCurrency, formatDate, formatDateTime, formatNumber, formatPercent, returnTone } from '../utils/format'

const loading = ref(true)
const refreshing = ref(false)
const error = ref('')
const success = ref('')
const data = ref<TradingDashboard | null>(null)
const tradeDate = ref(localDate(new Date()))
let refreshTimer: ReturnType<typeof setInterval> | undefined

function localDate(value: Date) {
  const year = value.getFullYear()
  const month = String(value.getMonth() + 1).padStart(2, '0')
  const day = String(value.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

function numberValue(value: string | number | null | undefined) {
  if (value === null || value === undefined || value === '') return null
  const numeric = Number(value)
  return Number.isNaN(numeric) ? null : numeric
}

function decimalPercent(value: string | null | undefined) {
  return formatPercent(numberValue(value))
}

function sideLabel(side: string) {
  return side.toUpperCase() === 'BUY' ? '买入' : side.toUpperCase() === 'SELL' ? '卖出' : side
}

function maskAccount(value: string) {
  if (!value) return '账户待同步'
  if (value.length <= 8) return value
  return `${value.slice(0, 4)}…${value.slice(-4)}`
}

function snapshotAge(seconds: number) {
  if (seconds < 60) return `${Math.max(0, seconds)} 秒前`
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分钟前`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)} 小时前`
  return `${Math.floor(seconds / 86400)} 天前`
}

async function load(silent = false) {
  if (silent) refreshing.value = true
  else loading.value = true
  error.value = ''
  try {
    data.value = await api.tradingDashboard({ trade_date: tradeDate.value })
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : '模拟交易数据加载失败'
  } finally {
    loading.value = false
    refreshing.value = false
  }
}

async function refreshSnapshot() {
  refreshing.value = true
  error.value = ''
  success.value = ''
  try {
    data.value = await api.refreshTradingDashboard({ trade_date: tradeDate.value })
    success.value = `账户和持仓已同步，快照时间 ${formatDateTime(data.value.account?.snapshot_at)}`
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : '东方财富账户快照刷新失败'
  } finally {
    refreshing.value = false
  }
}

const automationRunning = computed(() => {
  const runtime = data.value?.runtime
  return Boolean(runtime?.trading_enabled && runtime.scheduler_enabled && runtime.exit_enabled
    && runtime.reconciliation_enabled && !runtime.nacos_kill_switch && !runtime.runtime_kill_switch)
})

const runtimeMessage = computed(() => {
  const runtime = data.value?.runtime
  if (!runtime) return ''
  const blockers: string[] = []
  if (!runtime.trading_enabled) blockers.push('交易总开关关闭')
  if (runtime.nacos_kill_switch) blockers.push('Nacos 熔断开启')
  if (runtime.runtime_kill_switch) blockers.push('运行时熔断开启')
  if (!runtime.scheduler_enabled) blockers.push('自动调度关闭')
  if (!runtime.exit_enabled) blockers.push('自动卖出关闭')
  if (!runtime.reconciliation_enabled) blockers.push('自动对账关闭')
  return blockers.length ? blockers.join(' · ') : '盘前决策、盘中执行、自动退出和对账均已开启'
})

onMounted(() => {
  load()
  refreshTimer = setInterval(() => load(true), 30_000)
})

onBeforeUnmount(() => {
  if (refreshTimer) clearInterval(refreshTimer)
})
</script>

<template>
  <div>
    <PageHeader
      eyebrow="Eastmoney simulation"
      title="模拟账户，每一笔都可追溯"
      description="账户与持仓来自东方财富最新对账快照；买卖明细来自真实成交回报。推荐评价收益不会混入模拟账户盈亏。"
    >
      <template #actions>
        <button class="button" type="button" :disabled="loading || refreshing" @click="refreshSnapshot">
          <RefreshCw :size="16" :class="{ spin: loading || refreshing }" />刷新快照
        </button>
      </template>
    </PageHeader>

    <div v-if="error" class="error-banner">{{ error }}</div>
    <div v-if="success" class="success-banner">{{ success }}</div>

    <template v-if="data">
      <section class="trading-runtime" :class="automationRunning ? 'is-running' : 'is-blocked'">
        <span class="runtime-icon"><ChartCandlestick v-if="automationRunning" :size="19" /><ShieldAlert v-else :size="19" /></span>
        <div>
          <strong>{{ automationRunning ? '自动模拟交易运行中' : '自动模拟交易当前受控停止' }}</strong>
          <p>{{ runtimeMessage }}</p>
        </div>
        <span class="runtime-meta">{{ data.runtime.environment }} · 配置 v{{ data.runtime.config_version }}</span>
      </section>

      <template v-if="data.account">
        <section class="account-caption">
          <div>
            <span>东方财富模拟账户</span>
            <strong>东方财富模拟账户</strong>
            <small>{{ data.account.account_name ? `${data.account.account_name} · ` : '' }}{{ maskAccount(data.account.account_id) }}</small>
          </div>
          <p :class="{ stale: data.account.snapshot_stale }">
            {{ data.account.snapshot_stale ? '交易风控快照已过期' : '最新账务快照' }} · {{ formatDateTime(data.account.snapshot_at) }}（{{ snapshotAge(data.account.snapshot_age_seconds) }}<template v-if="data.account.snapshot_stale">；风控阈值 {{ data.account.snapshot_max_age_seconds }} 秒</template>）
          </p>
        </section>

        <section class="metric-grid trading-metrics">
          <MetricCard label="账户总资产" :value="formatCurrency(data.account.nav)" :note="`资金余额 ${formatCurrency(data.account.balance)}`" :icon="Landmark" tone="accent" />
          <MetricCard label="可用资金" :value="formatCurrency(data.account.available_cash)" :note="`冻结 ${formatCurrency(data.account.frozen_cash)}`" :icon="WalletCards" />
          <MetricCard label="持仓市值" :value="formatCurrency(data.account.market_value)" :note="`仓位 ${decimalPercent(data.account.position_ratio)}`" :icon="ChartCandlestick" />
          <MetricCard label="持仓浮盈亏" :value="formatCurrency(data.account.floating_pnl)" note="按东方财富账户快照" :icon="CircleDollarSign" :tone="numberValue(data.account.floating_pnl)! >= 0 ? 'positive' : 'negative'" />
          <MetricCard label="账户累计盈亏" :value="formatCurrency(data.account.cumulative_pnl)" :note="`累计佣金 ${formatCurrency(data.account.cumulative_commission)}`" :icon="CircleDollarSign" :tone="numberValue(data.account.cumulative_pnl)! >= 0 ? 'positive' : 'negative'" />
        </section>
      </template>
      <article v-else class="panel account-empty">
        <StatePanel title="尚无账户快照" description="Windows Bridge 完成一次账户同步后，这里会显示模拟账户余额、持仓与盈亏。" />
      </article>

      <section class="content-grid">
        <article class="panel span-12">
          <header class="panel-header">
            <div><h2>当前持仓</h2><p>以最新账户快照为准；止损、止盈和持有天数来自 FinanceSys 持仓周期。</p></div>
            <span class="muted">{{ data.positions.length }} 个标的</span>
          </header>
          <div v-if="data.positions.length" class="table-wrap">
            <table class="data-table trading-position-table">
              <thead><tr><th>标的</th><th>持仓 / 可卖</th><th>成本价</th><th>最新价</th><th>市值</th><th>浮动盈亏</th><th>止损 / 止盈</th><th>持有进度</th><th>状态</th></tr></thead>
              <tbody>
                <tr v-for="item in data.positions" :key="item.id">
                  <td><div class="security-cell"><strong>{{ item.security_name }}</strong><small>{{ item.ts_code || item.eastmoney_symbol }}</small></div></td>
                  <td><strong>{{ formatNumber(item.volume, 0) }}</strong><small class="muted"> / {{ formatNumber(item.available_volume, 0) }}</small></td>
                  <td>{{ formatCurrency(item.vwap, 3) }}</td>
                  <td>{{ formatCurrency(item.last_price, 3) }}</td>
                  <td>{{ formatCurrency(item.market_value) }}</td>
                  <td><span class="return-value" :class="returnTone(numberValue(item.floating_pnl))">{{ formatCurrency(item.floating_pnl) }}</span><small class="pnl-ratio" :class="returnTone(numberValue(item.floating_pnl_ratio))">{{ decimalPercent(item.floating_pnl_ratio) }}</small></td>
                  <td><span class="limit-pair negative">{{ item.stop_loss_price ? formatCurrency(item.stop_loss_price, 3) : '—' }}</span><span class="muted"> / </span><span class="limit-pair positive">{{ item.take_profit_price ? formatCurrency(item.take_profit_price, 3) : '—' }}</span></td>
                  <td>{{ item.cycle_id ? `${item.holding_trade_days} / ${item.max_holding_trade_days} 日` : '未关联周期' }}</td>
                  <td><StatusBadge v-if="item.cycle_status" :status="item.cycle_status" /><span v-else class="muted">快照持仓</span></td>
                </tr>
              </tbody>
            </table>
          </div>
          <StatePanel v-else title="当前没有持仓" description="模拟账户为空仓，新的买入成交后会显示成本、最新市值和退出阈值。" />
        </article>

        <article class="panel span-12">
          <header class="panel-header trade-day-header">
            <div><h2>每日买卖与成交</h2><p>按成交时间统计，不把挂单、撤单或拒单计入买卖金额。</p></div>
            <div class="trade-date-control"><label for="trade-date">交易日</label><input id="trade-date" v-model="tradeDate" type="date" @change="load()" /></div>
          </header>
          <div class="daily-trade-summary">
            <div><ArrowDownToLine :size="16" /><span>买入</span><strong>{{ formatCurrency(data.daily_summary.buy_amount) }}</strong><small>{{ data.daily_summary.buy_count }} 条成交 · {{ formatNumber(data.daily_summary.buy_volume, 0) }} 股/份</small></div>
            <div><ArrowUpFromLine :size="16" /><span>卖出</span><strong>{{ formatCurrency(data.daily_summary.sell_amount) }}</strong><small>{{ data.daily_summary.sell_count }} 条成交 · {{ formatNumber(data.daily_summary.sell_volume, 0) }} 股/份</small></div>
            <div><CircleDollarSign :size="16" /><span>净现金流</span><strong :class="returnTone(numberValue(data.daily_summary.net_cash_flow))">{{ formatCurrency(data.daily_summary.net_cash_flow) }}</strong><small>卖出 − 买入 − 佣金</small></div>
            <div><WalletCards :size="16" /><span>手续费</span><strong>{{ formatCurrency(data.daily_summary.commission) }}</strong><small>共 {{ data.daily_summary.fill_count }} 条成交回报</small></div>
          </div>
          <div v-if="data.fills.length" class="table-wrap">
            <table class="data-table trading-fill-table">
              <thead><tr><th>成交时间</th><th>方向</th><th>标的</th><th>成交价</th><th>数量</th><th>成交额</th><th>佣金</th><th>佣金状态</th><th>订单</th></tr></thead>
              <tbody>
                <tr v-for="fill in data.fills" :key="fill.id">
                  <td>{{ formatDateTime(fill.traded_at) }}</td>
                  <td><span class="trade-side" :class="fill.side.toLowerCase()">{{ sideLabel(fill.side) }}</span></td>
                  <td><div class="security-cell"><strong>{{ fill.security_name }}</strong><small>{{ fill.ts_code }}</small></div></td>
                  <td>{{ formatCurrency(fill.price, 3) }}</td>
                  <td>{{ formatNumber(fill.volume, 0) }}</td>
                  <td>{{ formatCurrency(fill.amount) }}</td>
                  <td>{{ formatCurrency(fill.commission) }}</td>
                  <td><StatusBadge :status="fill.commission_status" /></td>
                  <td><StatusBadge :status="fill.order_status" /></td>
                </tr>
              </tbody>
            </table>
          </div>
          <StatePanel v-else title="该交易日没有成交" description="可以切换日期查看历史买卖；未成交委托请查看下方订单记录。" />
        </article>

        <article class="panel span-12">
          <header class="panel-header"><div><h2>当日订单</h2><p>包括待报、已报、成交、撤单和拒单，便于排查“为什么没有成交”。</p></div><span class="muted">{{ data.orders.length }} 笔</span></header>
          <div v-if="data.orders.length" class="table-wrap">
            <table class="data-table trading-order-table">
              <thead><tr><th>创建时间</th><th>方向</th><th>标的</th><th>委托价</th><th>委托 / 成交</th><th>成交均价</th><th>成交额</th><th>状态</th><th>原因</th></tr></thead>
              <tbody>
                <tr v-for="order in data.orders" :key="order.id">
                  <td>{{ formatDateTime(order.created_at) }}</td>
                  <td><span class="trade-side" :class="order.side.toLowerCase()">{{ sideLabel(order.side) }}</span></td>
                  <td><div class="security-cell"><strong>{{ order.security_name }}</strong><small>{{ order.ts_code }}</small></div></td>
                  <td>{{ order.limit_price ? formatCurrency(order.limit_price, 3) : '—' }}</td>
                  <td>{{ formatNumber(order.volume, 0) }} <span class="muted">/ {{ formatNumber(order.filled_volume, 0) }}</span></td>
                  <td>{{ order.filled_vwap ? formatCurrency(order.filled_vwap, 3) : '—' }}</td>
                  <td>{{ formatCurrency(order.filled_amount) }}</td>
                  <td><StatusBadge :status="order.status" /></td>
                  <td class="order-reason" :title="order.error_message">{{ order.error_message || order.error_code || '—' }}</td>
                </tr>
              </tbody>
            </table>
          </div>
          <StatePanel v-else title="该交易日没有订单" description="Agent 尚未生成并执行订单，或所选日期不是交易日。" />
        </article>

        <article class="panel span-12">
          <header class="panel-header"><div><h2>持仓周期与盈亏</h2><p>持有中展示退出规则；平仓后按实际买卖成交额扣除佣金计算已实现盈亏。</p></div><span class="muted">最近 {{ data.position_cycles.length }} 条</span></header>
          <div v-if="data.position_cycles.length" class="table-wrap">
            <table class="data-table trading-cycle-table">
              <thead><tr><th>标的</th><th>状态</th><th>建仓日</th><th>成本 / 数量</th><th>止损 / 止盈</th><th>持有日</th><th>退出原因</th><th>退出价</th><th>已实现盈亏</th><th>来源推荐</th></tr></thead>
              <tbody>
                <tr v-for="cycle in data.position_cycles" :key="cycle.id">
                  <td><div class="security-cell"><strong>{{ cycle.security_name }}</strong><small>{{ cycle.ts_code }}</small></div></td>
                  <td><StatusBadge :status="cycle.status" /></td>
                  <td>{{ formatDate(cycle.entry_trade_date) }}</td>
                  <td>{{ formatCurrency(cycle.entry_price, 3) }} <small class="muted">× {{ formatNumber(cycle.initial_volume, 0) }}</small></td>
                  <td><span class="negative">{{ formatCurrency(cycle.stop_loss_price, 3) }}</span><span class="muted"> / </span><span class="positive">{{ formatCurrency(cycle.take_profit_price, 3) }}</span></td>
                  <td>{{ cycle.holding_trade_days }} / {{ cycle.max_holding_trade_days }}</td>
                  <td>{{ cycle.exit_reason || '—' }}</td>
                  <td>{{ cycle.exit_price ? formatCurrency(cycle.exit_price, 3) : '—' }}</td>
                  <td><template v-if="cycle.realized_pnl !== null"><span class="return-value" :class="returnTone(numberValue(cycle.realized_pnl))">{{ formatCurrency(cycle.realized_pnl) }}</span><small class="pnl-ratio" :class="returnTone(numberValue(cycle.realized_pnl_ratio))">{{ decimalPercent(cycle.realized_pnl_ratio) }}</small></template><span v-else class="muted">持有中</span></td>
                  <td><RouterLink v-if="cycle.source_recommendation_event_id" class="recommendation-link" :to="`/recommendations/${cycle.source_recommendation_event_id}`">#{{ cycle.source_recommendation_event_id }}</RouterLink><span v-else>—</span></td>
                </tr>
              </tbody>
            </table>
          </div>
          <StatePanel v-else title="尚无自动持仓周期" description="自动策略完成一次 BUY 成交后，这里会记录来源推荐、退出阈值与最终实际收益。" />
        </article>
      </section>
    </template>
    <StatePanel v-else :loading="loading" title="模拟交易数据不可用" description="确认 FinanceSys 交易模块已启用并完成数据库迁移。" />
  </div>
</template>
