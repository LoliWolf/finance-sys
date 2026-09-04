import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import SimulationTradingView from './SimulationTradingView.vue'

const apiMocks = vi.hoisted(() => ({ tradingDashboard: vi.fn(), refreshTradingDashboard: vi.fn() }))

vi.mock('../api/client', () => ({ api: apiMocks }))

describe('SimulationTradingView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.tradingDashboard.mockResolvedValue({
      trade_date: '2026-09-04',
      runtime: {
        trading_enabled: true,
        nacos_kill_switch: false,
        runtime_kill_switch: false,
        scheduler_enabled: true,
        exit_enabled: true,
        reconciliation_enabled: true,
        environment: 'SIMULATION',
        provider: 'EASTMONEY_GM',
        config_version: 30,
      },
      account: {
        account_id: 'account-1', account_name: '东方财富模拟盘', nav: '100000.000000', balance: '100000.000000',
        available_cash: '90000.000000', frozen_cash: '0.000000', market_value: '10000.000000', position_ratio: '0.10000000',
        floating_pnl: '120.000000', cumulative_pnl: '220.000000', cumulative_commission: '10.000000',
        commission_data_status: 'REPORTED', terminal_state: 'CONNECTED', account_state: 'READY',
        snapshot_at: '2026-09-04T10:00:00+08:00', snapshot_age_seconds: 5, snapshot_max_age_seconds: 15, snapshot_stale: false,
      },
      positions: [{
        id: 1, symbol: '600000', ts_code: '600000.SH', security_name: '浦发银行', market: 'SH', asset_type: 'STOCK',
        eastmoney_symbol: 'SHSE.600000', volume: 100, available_volume: 100, today_volume: 0, vwap: '9.180000',
        last_price: '10.380000', market_value: '1038.000000', floating_pnl: '120.000000', floating_pnl_ratio: '0.13071895',
        cycle_id: 1, cycle_status: 'OPEN', stop_loss_price: '8.904600', take_profit_price: '9.730800',
        holding_trade_days: 2, max_holding_trade_days: 20, exit_reason: '',
      }],
      daily_summary: { fill_count: 1, buy_count: 1, sell_count: 0, buy_volume: 100, sell_volume: 0, buy_amount: '918.000000', sell_amount: '0.000000', commission: '5.000000', net_cash_flow: '-923.000000' },
      fills: [{ id: 1, trading_order_id: 1, client_order_id: 'order-1', symbol: '600000', ts_code: '600000.SH', security_name: '浦发银行', side: 'BUY', price: '9.180000', volume: 100, amount: '918.000000', commission: '5.000000', commission_status: 'VERIFIED', order_status: 'FILLED', traded_at: '2026-09-04T09:40:00+08:00' }],
      orders: [],
      position_cycles: [],
    })
    apiMocks.refreshTradingDashboard.mockImplementation(() => apiMocks.tradingDashboard.mock.results[0]?.value)
  })

  it('renders account, positions, and real fill data', async () => {
    const wrapper = mount(SimulationTradingView, {
      global: { stubs: { RouterLink: { template: '<a><slot /></a>' } } },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('自动模拟交易运行中')
    expect(wrapper.text()).toContain('东方财富模拟盘')
    expect(wrapper.text()).toContain('浦发银行')
    expect(wrapper.text()).toContain('买入')
    expect(apiMocks.tradingDashboard).toHaveBeenCalledOnce()
  })

  it('requests a real Bridge snapshot refresh from the refresh button', async () => {
    const wrapper = mount(SimulationTradingView, {
      global: { stubs: { RouterLink: { template: '<a><slot /></a>' } } },
    })
    await flushPromises()
    apiMocks.refreshTradingDashboard.mockResolvedValue(await apiMocks.tradingDashboard.mock.results[0].value)

    await wrapper.get('button.button').trigger('click')
    await flushPromises()

    expect(apiMocks.refreshTradingDashboard).toHaveBeenCalledOnce()
    expect(wrapper.text()).toContain('账户和持仓已同步')
  })
})
