"""Read-only export for offline validation. MySQL access exclusively through dbx.

Usage: python3 -B scripts/export_trading_replay.py --end 2026-09-30 --output tmp/.../input.json
No tokens/credentials/config documents are persisted. Business settings from Nacos HTTP.
"""
import argparse
import datetime as dt
import json
import subprocess
import sys
import urllib.parse
import urllib.request
from pathlib import Path

ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT))
import start_prod


def query(sql):
    if not sql.lstrip().upper().startswith('SELECT '):raise ValueError('read-only SELECT required')
    p=subprocess.run(['dbx','query','home',sql,'--json'],capture_output=True,text=True,timeout=120,check=True)
    rows = json.loads(p.stdout)['rows']
    if len(rows) >= 10000: raise ValueError('dbx row cap reached; paginate this query')
    return rows


def main():
    p=argparse.ArgumentParser();p.add_argument('--end',required=True);p.add_argument('--output',type=Path,required=True);a=p.parse_args()
    end=dt.date.fromisoformat(a.end);start=end-dt.timedelta(days=13);pool_start=start-dt.timedelta(days=6);warmup=start-dt.timedelta(days=80)
    address=start_prod.nacos_address(ROOT).split(',')[0].strip()
    url='http://'+address+'/nacos/v1/cs/configs?'+urllib.parse.urlencode(dict(dataId='expert_trade',group='DEFAULT_GROUP',tenant='public'))
    with start_prod.HTTP.open(url,timeout=10) as r:cfg=json.load(r)
    t=cfg['trading'];assert t['environment']=='SIMULATION' and not t['allow_live']
    events=query(f"SELECT e.id,e.plan_id,e.blogger_id,e.symbol,e.asset_type,e.market,e.direction,DATE_FORMAT(e.recommend_date,'%Y-%m-%d') AS recommend_date,e.reference_price,e.confidence,e.status,DATE_FORMAT(e.created_at,'%Y-%m-%d %H:%i:%s') AS created_at,DATE_FORMAT(e.updated_at,'%Y-%m-%d %H:%i:%s') AS updated_at,p.status AS plan_status,p.reference_price AS plan_reference_price,p.pricing_note FROM expert_trade.recommendation_events e LEFT JOIN expert_trade.trade_candidate_plans p ON p.id=e.plan_id WHERE e.recommend_date BETWEEN '{pool_start}' AND '{end}' AND e.asset_type IN ('A_SHARE','STOCK','ETF') AND e.market IN ('SH','SZ')")
    codes=sorted({e['symbol'].split('.')[0] for e in events}|{'510300'})
    if not all(c.isdigit() and len(c)==6 for c in codes):raise ValueError('noncanonical symbol')
    quotes=[]
    for n in range(0,len(codes),100):
        symbols=','.join("'"+c+"'" for c in codes[n:n+100])
        quotes.extend(query(f"SELECT symbol,ts_code,DATE_FORMAT(trade_date,'%Y-%m-%d') AS trade_date,open_price,high_price,low_price,close_price,pre_close_price,volume,source FROM expert_trade.stock_daily_quotes WHERE source='TUSHARE' AND trade_date BETWEEN '{warmup}' AND '{end}' AND symbol IN ({symbols}) ORDER BY symbol,trade_date"))
    ids=sorted({int(e['blogger_id']) for e in events})
    benchmark_source = 'dbx'
    token = next(t['token'] for t in cfg['market_data']['tushare']['tokens'] if t['enabled'])
    # Repair only globally absent warm-up dates in this LOCAL test dataset.
    coverage = {q['trade_date'] for q in quotes}
    repair_dates = [day for day in t['calendar']['open_dates'] if str(warmup)<=day<=str(end) and day not in coverage]
    for day in repair_dates:
        body=json.dumps(dict(api_name='daily',token=token,params=dict(trade_date=day.replace('-','')),fields='ts_code,trade_date,open,high,low,close,pre_close,vol')).encode()
        request=urllib.request.Request('https://api.tushare.pro',data=body,headers={'Content-Type':'application/json'})
        with urllib.request.urlopen(request,timeout=20) as response:payload=json.load(response)
        if payload.get('code')!=0: raise ValueError('historical gap download failed')
        for values in payload['data']['items']:
            row=dict(zip(payload['data']['fields'],values));code=row['ts_code'].split('.')[0]
            if code in codes: quotes.append(dict(symbol=code,ts_code=row['ts_code'],trade_date=day,open_price=row['open'],high_price=row['high'],low_price=row['low'],close_price=row['close'],pre_close_price=row['pre_close'],volume=row['vol'],source='TUSHARE'))
    if not any(q['symbol']=='510300' for q in quotes):
        # Read-only remote quotes; production DB and configuration remain unchanged.
        token = next(t['token'] for t in cfg['market_data']['tushare']['tokens'] if t['enabled'])
        body = json.dumps(dict(api_name='fund_daily', token=token, params=dict(ts_code='510300.SH', start_date=warmup.strftime('%Y%m%d'), end_date=end.strftime('%Y%m%d')), fields='ts_code,trade_date,open,high,low,close,pre_close,vol')).encode()
        request = urllib.request.Request('https://api.tushare.pro', data=body, headers={'Content-Type':'application/json'})
        with urllib.request.urlopen(request, timeout=20) as response: payload=json.load(response)
        if payload.get('code') != 0: raise ValueError('benchmark historical download failed')
        for values in payload['data']['items']:
            row=dict(zip(payload['data']['fields'],values));day=dt.datetime.strptime(row['trade_date'],'%Y%m%d').date().isoformat()
            quotes.append(dict(symbol='510300',ts_code='510300.SH',trade_date=day,open_price=row['open'],high_price=row['high'],low_price=row['low'],close_price=row['close'],pre_close_price=row['pre_close'],volume=row['vol'],source='TUSHARE'))
        benchmark_source='Tushare fund_daily read-only download, local dataset only'
    metrics=[]
    for n in range(0,len(ids),100):
        values=','.join(str(i) for i in ids[n:n+100])
        cursor = 0
        while True:
            page = query(f"SELECT id,blogger_id,status,win_flag,DATE_FORMAT(exit_date,'%Y-%m-%d') AS exit_date,DATE_FORMAT(calculated_at,'%Y-%m-%d %H:%i:%s') AS calculated_at,DATE_FORMAT(updated_at,'%Y-%m-%d %H:%i:%s') AS updated_at FROM expert_trade.recommendation_event_window_metrics WHERE id>{cursor} AND window_days=30 AND quote_source='TUSHARE' AND blogger_id IN ({values}) ORDER BY id LIMIT 5000")
            if not page: break
            metrics.extend(page)
            cursor = int(page[-1]['id'])
    risk_keys=['allowed_boards','max_single_order_ratio','max_symbol_position_ratio','max_total_position_ratio','min_cash_reserve_ratio','max_position_count','max_new_orders_per_day','max_daily_turnover_ratio','daily_loss_kill_ratio']
    data=dict(start=str(start),end=str(end),warmup_start=str(warmup),schema='offline-dataset/v1',calendar=t['calendar'],risk={k:t['risk'][k] for k in risk_keys},exit={k:t['exit'][k] for k in ['stop_loss_ratio','take_profit_ratio','max_holding_trade_days']},events=events,quotes=quotes,metrics=metrics,provenance=dict(schema='expert_trade',access='dbx SELECT only',nacos_config_version=cfg['meta']['config_version'],exported_at=dt.datetime.now(dt.timezone.utc).isoformat(),quote_availability='historical EOD market data; ingestion revisions are not versioned',recommendation_availability='created_at and updated_at bounded; cannot reconstruct overwritten historical rows'))
    data['provenance']['benchmark_source']=benchmark_source
    data['provenance']['local_only_gap_download_dates']=repair_dates
    a.output.parent.mkdir(parents=True,exist_ok=True);a.output.write_text(json.dumps(data,ensure_ascii=False,indent=2)+'\n')
    print(json.dumps(dict(start=str(start),end=str(end),events=len(events),symbols=len(codes),quotes=len(quotes),metrics=len(metrics),benchmark_source=benchmark_source,output=str(a.output)),ensure_ascii=False))

if __name__=='__main__':main()
