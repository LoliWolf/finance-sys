"""Offline daily-bar validation, never connects to a broker or writes MySQL.

Inputs are frozen dbx exports. Matching is next-session OPEN at or better than
limit, not fake intraday ticks. Source data availability and unavailable historical
blogger statistics are explicit; zero trades is not successful trading acceptance.
"""
from __future__ import annotations
import argparse
import datetime as dt
import hashlib
import json
from collections import Counter
from decimal import Decimal, ROUND_FLOOR, ROUND_HALF_UP
from pathlib import Path
from .app.config import AgentConfig
from .app.strategy import run_deterministic
from .app.skills import exit_signal

CST = dt.timezone(dt.timedelta(hours=8))
D = lambda x: Decimal(str(x))


def stamp(value):
    parsed = dt.datetime.fromisoformat(str(value).replace("Z", "+00:00"))
    return parsed.replace(tzinfo=CST) if parsed.tzinfo is None else parsed.astimezone(CST)


def eligible(event, at):
    day = at.date()
    return ((day-dt.timedelta(days=6)).isoformat() <= event['recommend_date'] <= day.isoformat()
            and stamp(event['created_at']) <= at and stamp(event['updated_at']) <= at
            and event['asset_type'] in ('A_SHARE','STOCK','ETF') and event['market'] in ('SH','SZ')
            and event['direction'] in ('LONG','BUY') and D(event['confidence']) >= D('.65')
            and (event['status']=='ACTIVE' or (
                event['status']=='NEEDS_REVIEW' and D(event['reference_price'])==0
                and event.get('plan_status')=='NEEDS_REVIEW' and event.get('plan_reference_price') is not None and D(event['plan_reference_price'])==0
                and event.get('pricing_note')=='missing explicit price in source text')))


def board(code):
    if code.startswith(('688','689')): return 'STAR'
    if code.startswith(('300','301')): return 'CHINEXT'
    if code.startswith(('51','52','56','58','159')): return 'ETF'
    return 'SH_MAIN' if code.startswith('6') else 'SZ_MAIN'


class OfflineBroker:
    """Independent fill journal and account ledger with deterministic reconciliation."""
    def __init__(self, cash, risk):
        self.initial = D(cash)
        self.cash = self.initial
        self.risk = risk
        self.positions = {}
        self.orders = {}
        self.fills = []
        self.closed = []
        self.marks = {}
        self.fee_rate = D('.0003')
        self.min_fee = D('5')
        self.stamp_tax = D('.0005')

    def fee(self, side, amount, asset):
        return (max(self.min_fee,amount*self.fee_rate)+(amount*self.stamp_tax if side=='SELL' and asset=='STOCK' else 0)).quantize(D('.01'),rounding=ROUND_HALF_UP)

    def submit(self, key, symbol, side, limit, quantity, date, asset='STOCK'):
        if key in self.orders: return self.orders[key]
        order = dict(key=key,symbol=symbol,side=side,limit=str(limit),quantity=quantity,filled=0,status='SUBMITTED',date=date,asset=asset)
        self.orders[key]=order
        return order

    def fill(self, key, exec_id, quantity, price, date):
        if any(f['exec_id']==exec_id for f in self.fills): return False
        o=self.orders[key];p=D(price)
        if o['status'] not in ('SUBMITTED','PARTIALLY_FILLED'): raise ValueError('order not fillable')
        if quantity<=0 or quantity+o['filled']>o['quantity']: raise ValueError('invalid fill quantity')
        if p<=0 or not p.is_finite(): raise ValueError('invalid fill price')
        if (o['side']=='BUY' and p>D(o['limit'])) or (o['side']=='SELL' and p<D(o['limit'])): raise ValueError('limit violation')
        amount=p*quantity;fee=self.fee(o['side'],amount,o['asset']);symbol=o['symbol']
        if o['side']=='BUY':
            if amount+fee>self.cash: raise ValueError('insufficient cash including fee')
            pos=self.positions.get(symbol)
            if pos and pos['entry_date']!=date: raise ValueError('pyramiding disabled')
            if not pos:
                pos=dict(volume=0,cost=D(0),entry_date=date,asset=o['asset'],entry_price=p,buy_fees=D(0),sold_proceeds=D(0),initial_cost=D(0))
                self.positions[symbol]=pos
            pos['volume']+=quantity;pos['cost']+=amount;pos['initial_cost']+=amount;pos['buy_fees']+=fee
            pos['entry_price']=pos['cost']/pos['volume'];self.cash-=amount+fee
        else:
            pos=self.positions.get(symbol)
            if not pos or quantity>pos['volume']: raise ValueError('oversell')
            if date<=pos['entry_date']: raise ValueError('T1_PENDING')
            self.cash+=amount-fee;pos['sold_proceeds']+=amount-fee;pos['volume']-=quantity
            if pos['volume']==0:
                self.closed.append(dict(symbol=symbol,entry_date=pos['entry_date'],exit_date=date,net_pnl=str(pos['sold_proceeds']-pos['initial_cost']-pos['buy_fees'])))
                del self.positions[symbol]
        o['filled']+=quantity;o['status']='FILLED' if o['filled']==o['quantity'] else 'PARTIALLY_FILLED'
        self.fills.append(dict(exec_id=exec_id,order_key=key,symbol=symbol,side=o['side'],volume=quantity,price=str(p),amount=str(amount),commission=str(fee),date=date))
        return True

    def cancel(self,key):
        o=self.orders[key]
        if o['status'] in ('SUBMITTED','PARTIALLY_FILLED'):o['status']='CANCELED'

    def reconcile(self):
        cash=self.initial;volumes=Counter()
        for f in self.fills:
            cash += (-D(f['amount']) if f['side']=='BUY' else D(f['amount']))-D(f['commission'])
            volumes[f['symbol']] += f['volume'] if f['side']=='BUY' else -f['volume']
        actual={k:v['volume'] for k,v in self.positions.items()}
        expected={k:v for k,v in volumes.items() if v}
        if cash!=self.cash or actual!=expected:raise AssertionError('LEDGER_DIFF')
        return dict(cash=str(cash),positions=actual,diff_count=0)


class ReplayClient:
    def __init__(self,data,at,broker):
        self.data,self.at,self.broker=data,at,broker
        self.day=at.date().isoformat()
        self.bars={}
        for r in data['quotes']:
            self.bars.setdefault(r['symbol'],[]).append(r)
        for rows in self.bars.values():rows.sort(key=lambda r:r['trade_date'])

    def get(self,path,params=None):
        params=params or {}
        if path=='/recommendation-candidates':
            rows=[]
            for e in sorted(self.data['events'],key=lambda r:int(r['id']),reverse=True):
                if not eligible(e,self.at) or (params.get('after_id') and int(e['id'])>=int(params['after_id'])):continue
                code=e['symbol'].split('.')[0]
                rows.append(dict(recommendation_event_id=int(e['id']),candidate_plan_id=e.get('plan_id'),blogger_id=int(e['blogger_id']),
                    symbol=code,ts_code=code+'.'+e['market'],market=e['market'],asset_type='ETF' if e['asset_type']=='ETF' else 'STOCK',
                    board_type=board(code),direction=e['direction'],recommend_date=e['recommend_date'],confidence=str(e['confidence']),
                    reference_price=str(e['reference_price']),position_ratio='0.03',evidence_refs=['recommendation_event:'+str(e['id'])]))
            rows=rows[:int(params['limit'])]
            return dict(items=rows,next_cursor=str(rows[-1]['recommendation_event_id']) if rows else '')
        if path=='/portfolio':
            return dict(positions=[dict(symbol=k,volume=v['volume']) for k,v in self.broker.positions.items()],cycles=[],open_orders=[])
        if path in ('/market-snapshot','/daily-history'):
            result=[]
            for raw in params['symbols'].split(','):
                code=raw.split('.')[0]
                rows=[r for r in self.bars.get(code,[]) if r['trade_date']<self.day]
                previous = max((d for d in self.data['calendar']['open_dates'] if d < self.day), default=None)
                if path=='/daily-history':
                    if rows and rows[-1]['trade_date']==previous: result.extend(rows[-int(params['limit']):])
                elif rows:result.append(dict(symbol=code,price=rows[-1]['close_price'],trade_date=rows[-1]['trade_date'],source='HISTORICAL_PREVIOUS_CLOSE'))
            return dict(items=result)
        if path.startswith('/blogger-performance/'):
            blogger=int(path.rsplit('/',1)[-1])
            # Existing aggregates are overwritten: do NOT backdate today's metrics.
            rows=[r for r in self.data['metrics'] if int(r['blogger_id'])==blogger and r['status']=='READY'
                  and r.get('exit_date') and r['exit_date']<self.day
                  and stamp(r['calculated_at'])<=self.at and stamp(r['updated_at'])<=self.at]
            wins=sum(int(r.get('win_flag') or 0) for r in rows)
            return dict(evaluable_count=len(rows),win_rate=wins/len(rows) if rows else 0,win_count=wins,history_source='PERSISTED_POINT_IN_TIME')
        raise ValueError(path)


def replay(data):
    dates=[d for d in data['calendar']['open_dates'] if data['start']<=d<=data['end']]
    if not dates:raise ValueError('no covered trading dates')
    risk=data['risk'];broker=OfflineBroker('1000000',risk)
    cfg=AgentConfig('offline',0,'','trading-tools/v2',.65,10,.55,20,5)
    daily=[];audits=[];warnings=[];used=set()
    quote_map={(q['symbol'],q['trade_date']):q for q in data['quotes']}
    all_dates=data['calendar']['open_dates']
    exits=data['exit']
    for date in dates:
        at=dt.datetime.fromisoformat(date+'T09:25:00+08:00')
        start_nav = broker.cash+sum(v['volume']*broker.marks.get(k,v['entry_price']) for k,v in broker.positions.items())
        day_turnover = D(0)
        day_orders = 0
        # Prior close triggers next-session exits. No same-day high/low used for decisions.
        client=ReplayClient(data,at,broker)
        for symbol,pos in list(broker.positions.items()):
            previous=[q for q in client.bars.get(symbol,[]) if q['trade_date']<date]
            current=quote_map.get((symbol,date))
            if not previous or not current: warnings.append(dict(date=date,symbol=symbol,reason='MISSING_EXIT_BAR'));continue
            days=sum(pos['entry_date']<d<=date for d in all_dates)
            signal=dict(stop_loss_price=str(pos['entry_price']*(1-D(exits['stop_loss_ratio']))),take_profit_price=str(pos['entry_price']*(1+D(exits['take_profit_ratio']))),holding_trade_days=days,max_holding_trade_days=exits['max_holding_trade_days'])
            triggered,reason,_,_=exit_signal(signal,D(previous[-1]['close_price']))
            if triggered:
                p=D(current['open_price']);key=date+':SELL:'+symbol
                o=broker.submit(key,symbol,'SELL',p,pos['volume'],date,pos['asset'])
                day_orders += 1
                # Daily bars cannot prove queue priority; zero-volume/one-price bars are not filled.
                if D(current['volume'])<=0 or D(current['high_price'])==D(current['low_price']):broker.cancel(key);continue
                sold = pos['volume']
                broker.fill(key,key+':fill',sold,p,date);day_turnover += sold*p;audits.append(dict(date=date,symbol=symbol,exit_reason=reason))
        result=run_deterministic(dict(run_key='replay:'+date,as_of_time=at.isoformat(),strategy_name='weekly',strategy_version='weekly-pullback-v1',selection_only=True),cfg,client)
        audits.extend(result['skill_decisions'])
        buys=0
        for i in result['intents']:
            symbol=i['symbol'];event=i['recommendation_event_id'];q=quote_map.get((symbol,date))
            if event in used or symbol in broker.positions:continue
            if i['board_type'] not in risk['allowed_boards']:
                warnings.append(dict(date=date,symbol=symbol,reason='BOARD_WHITELIST'));continue
            if not q or D(q['volume'])<=0 or D(q['high_price'])==D(q['low_price']):
                warnings.append(dict(date=date,symbol=symbol,reason='NO_EXECUTABLE_DAILY_BAR'));continue
            p=D(q['open_price']);limit=D(i['proposed_limit_price'])
            nav=broker.cash+sum(v['volume']*broker.marks.get(k,v['entry_price']) for k,v in broker.positions.items())
            if day_orders >= int(risk['max_new_orders_per_day']) or nav <= start_nav*(1-D(risk['daily_loss_kill_ratio'])):
                warnings.append(dict(date=date,symbol=symbol,reason='DAILY_ORDER_OR_LOSS_LIMIT'));continue
            invested=nav-broker.cash
            ratio=min(D(i['proposed_position_ratio']),D(risk['max_single_order_ratio']),D(risk['max_symbol_position_ratio']))
            budget=min(nav*ratio,nav*D(risk['max_total_position_ratio'])-invested,broker.cash-nav*D(risk['min_cash_reserve_ratio']),start_nav*D(risk['max_daily_turnover_ratio'])-day_turnover)
            qty=int(max(D(0),budget-broker.min_fee)/(max(limit,p)*(1+broker.fee_rate)))//100*100
            if qty<100 or len(broker.positions)>=int(risk['max_position_count']):continue
            key=date+':BUY:'+symbol
            broker.submit(key,symbol,'BUY',limit,qty,date,i['asset_type'])
            day_orders += 1
            if p>limit:broker.cancel(key);continue
            broker.fill(key,key+':fill',qty,p,date);day_turnover += qty*p;used.add(event);buys+=1
        missing=[]
        for symbol,pos in broker.positions.items():
            q=quote_map.get((symbol,date))
            if not q or D(q['close_price'])<=0:missing.append(symbol)
            else:broker.marks[symbol]=D(q['close_price'])
        ledger=broker.reconcile()
        nav=None if missing else str(broker.cash+sum(v['volume']*broker.marks[k] for k,v in broker.positions.items()))
        daily.append(dict(date=date,candidates=result['candidate_count'],intents=len(result['intents']),buys=buys,nav=nav,missing_valuation=missing,ledger=ledger))
    return dict(schema='offline-replay/v1',start=data['start'],end=data['end'],trading_days=dates,
        assumptions=['7 natural dates including today','previous completed daily bars only; next open limit matching','no intraday order book or broker callbacks claimed','commission max(5,0.03%); stock sell tax 0.05%, conservative per-fill minimum; modeled, not broker verified','raw OHLC: corporate-action adjusted strategy equivalence not certified','current historical rows are conservatively excluded if updated after decision; no reconstructed metrics','no force liquidation at test end; open positions remain open'],
        limitations=['Daily open matching is a research approximation, NOT execution within production 09:35 valid_from; minute data required to certify intraday execution','Current board whitelist retained; historical board eligibility and price-limit queue metadata unavailable','Fees are an explicit model, not the Eastmoney account fee contract; transfer fees are not certified','Input data/market regimes may cause zero trades; seeded mechanics tests are separate'],
        input_hash=hashlib.sha256(json.dumps(data,sort_keys=True).encode()).hexdigest(),daily=daily,
        audits=audits,warnings=warnings,orders=list(broker.orders.values()),fills=broker.fills,closed_cycles=broker.closed,
        open_positions={k:{kk:str(vv) for kk,vv in v.items()} for k,v in broker.positions.items()},
        natural_roundtrip_pass=bool(broker.closed),broker_acceptance=False,
        conclusion='NATURAL_ROUNDTRIP_OBSERVED' if broker.closed else 'NO_NATURAL_ROUNDTRIP_NOT_ACCEPTED')


def main():
    p=argparse.ArgumentParser();p.add_argument('--input',type=Path,required=True);p.add_argument('--output',type=Path,required=True);args=p.parse_args()
    data=json.loads(args.input.read_text());result=replay(data)
    if result!=replay(data):raise AssertionError('non deterministic replay')
    args.output.parent.mkdir(parents=True,exist_ok=True);args.output.write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
    print(json.dumps({k:result[k] for k in ('start','end','trading_days','input_hash','conclusion')},ensure_ascii=False))
    print(json.dumps({'fills':len(result['fills']),'closed_cycles':len(result['closed_cycles']),'daily':result['daily']},ensure_ascii=False))

if __name__=='__main__':main()
