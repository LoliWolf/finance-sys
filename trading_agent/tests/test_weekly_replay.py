import copy
import datetime as dt
from decimal import Decimal
import pytest
from trading_agent.app.selection import scan_candidates, technical_selection, bars_before
from trading_agent.replay import OfflineBroker, eligible, ReplayClient
from trading_agent.tests.test_strategy import FakeClient, config
from trading_agent.app.strategy import run_deterministic


def test_pagination_scans_empty_filtered_page_and_late_ingestion():
    class Pages:
        def get(self,path,params):
            cursor=params.get('after_id','')
            return {'':{'items':[], 'next_cursor':'31'},'31':{'items':[{'recommendation_event_id':30}], 'next_cursor':'30'},'30':{'items':[],'next_cursor':''}}[cursor]
    assert scan_candidates(Pages(),'date',20,100)==[{'recommendation_event_id':30}]
    class Loop:
        def get(self,*a):return {'items':[],'next_cursor':'3'}
    with pytest.raises(ValueError):scan_candidates(Loop(),'date',20,100)
    class Overflow:
        def get(self,*a):return {'items':[{'recommendation_event_id':i} for i in range(21)]}
    with pytest.raises(ValueError,match='INCOMPLETE'):scan_candidates(Overflow(),'date',20,20)


def test_market_skill_rejects_chasing_risk_off_missing_and_future_bars():
    rows=FakeClient().get('/daily-history')['items']
    h=[r for r in rows if r['symbol']=='600000'];b=[r for r in rows if r['symbol']=='510300']
    assert technical_selection(h,b,'10.23',.05,.08)[0]
    assert technical_selection(h,b,'15',.05,.08)[2]=='WATCH_ENTRY_PRICE'
    assert not technical_selection(h[:5],b,'10',.05,.08)[0]
    assert not technical_selection(h,[],'10',.05,.08)[0]
    assert not technical_selection(h,b,'NaN',.05,.08)[0]
    assert len(bars_before(h+[dict(h[0],trade_date='2026-08-24')],'2026-08-24'))==20


def test_duplicate_symbol_is_one_intent_with_all_evidence():
    class Duplicate(FakeClient):
        def get(self,path,params=None):
            value=super().get(path,params)
            if path=='/recommendation-candidates':
                other=copy.deepcopy(value['items'][0]);other.update(recommendation_event_id=13,evidence_refs=['recommendation_event:13'])
                value['items'].append(other)
            return value
    result=run_deterministic(dict(run_key='test',as_of_time='2026-08-24T09:30:00+08:00',strategy_name='weekly',strategy_version='1'),config(),Duplicate())
    assert len(result['intents'])==1
    assert result['intents'][0]['evidence_refs']==['recommendation_event:12','recommendation_event:13']


def test_offline_ledger_partial_cancel_idempotent_t1_exit_and_corruption():
    b=OfflineBroker('100000',{})
    first=b.submit('buy','600000','BUY','10',300,'2026-09-17')
    assert b.submit('buy','600000','BUY','10',300,'2026-09-17') is first
    assert b.fill('buy','exec1',100,'10','2026-09-17')
    assert not b.fill('buy','exec1',100,'10','2026-09-17')
    b.cancel('buy');assert first['status']=='CANCELED'
    assert b.positions['600000']['volume']==100
    b.submit('sell','600000','SELL','9',100,'2026-09-17')
    with pytest.raises(ValueError,match='T1'):b.fill('sell','exec2',100,'9','2026-09-17')
    assert b.fill('sell','exec2',100,'9','2026-09-18')
    assert not b.positions and len(b.closed)==1
    assert b.reconcile()['diff_count']==0
    assert Decimal(b.closed[0]['net_pnl'])<Decimal('-100')
    b.cash+=1
    with pytest.raises(AssertionError,match='LEDGER_DIFF'):b.reconcile()


def test_week_window_and_actual_availability():
    at=dt.datetime.fromisoformat('2026-09-30T09:25:00+08:00')
    row=dict(recommend_date='2026-09-24',created_at='2026-09-29 00:00:00',updated_at='2026-09-29 00:00:00',asset_type='A_SHARE',market='SH',direction='LONG',confidence='.8',status='NEEDS_REVIEW',reference_price='0',plan_status='NEEDS_REVIEW',plan_reference_price='0',pricing_note='missing explicit price in source text')
    assert eligible(row,at)
    assert not eligible(dict(row,recommend_date='2026-09-23'),at)
    assert not eligible(dict(row,created_at='2026-09-30 09:26:00'),at)
    assert not eligible(dict(row,updated_at='2026-10-01 00:00:00'),at)
    assert not eligible(dict(row,pricing_note='unknown'),at)


def test_no_future_blogger_statistics():
    data={'quotes':[],'metrics':[dict(blogger_id='1',status='READY',exit_date='2026-09-01',win_flag=1,calculated_at='2026-10-01 00:00:00',updated_at='2026-10-01 00:00:00')]}
    c=ReplayClient(data,dt.datetime.fromisoformat('2026-09-30T09:25:00+08:00'),OfflineBroker(100000,{}))
    assert c.get('/blogger-performance/1')['evaluable_count']==0


def test_replay_strategy_roundtrip_on_explicit_synthetic_fixture():
    """Mechanics coverage only. This fixture is not part of historical performance."""
    from trading_agent.replay import replay
    calendar=[]; day=dt.date(2026,8,1)
    while day <= dt.date(2026,9,30):
        if day.weekday()<5: calendar.append(day.isoformat())
        day+=dt.timedelta(days=1)
    quotes=[]
    for date in calendar:
        for symbol in ['600000','510300']:
            quotes.append(dict(symbol=symbol,trade_date=date,open_price='10',high_price='10.1',low_price='9.9',close_price='10',volume='1000'))
    data=dict(start='2026-09-17',end='2026-09-18',calendar={'open_dates':calendar},risk=dict(allowed_boards=['SH_MAIN'],max_single_order_ratio=.05,max_symbol_position_ratio=.1,max_total_position_ratio=.6,min_cash_reserve_ratio=.2,max_position_count=10,max_new_orders_per_day=20,max_daily_turnover_ratio=.3,daily_loss_kill_ratio=.03),exit=dict(stop_loss_ratio=.03,take_profit_ratio=.06,max_holding_trade_days=1),quotes=quotes,
        events=[dict(id=1,blogger_id=1,symbol='600000',market='SH',asset_type='STOCK',recommend_date='2026-09-16',created_at='2026-09-16 12:00:00',updated_at='2026-09-16 12:00:00',direction='LONG',confidence='.8',status='ACTIVE',reference_price=0)],
        metrics=[dict(blogger_id=1,status='READY',exit_date='2026-09-01',calculated_at='2026-09-02 00:00:00',updated_at='2026-09-02 00:00:00',win_flag=1) for _ in range(10)])
    result=replay(data)
    assert result==replay(data)
    assert len(result['fills'])==2 and len(result['closed_cycles'])==1
    assert result['closed_cycles'][0]['net_pnl'].startswith('-')
    assert result['daily'][-1]['ledger']['diff_count']==0
    assert result['broker_acceptance'] is False


def test_corporate_action_history_not_treated_as_momentum():
    rows=FakeClient().get('/daily-history')['items']
    h=[r for r in rows if r['symbol']=='600000'];b=[r for r in rows if r['symbol']=='510300']
    h[-1]['pre_close_price']='5'
    assert technical_selection(h,b,'10.23',.05,.08)[2]=='CORPORATE_ACTION_OR_INCONSISTENT_HISTORY'
