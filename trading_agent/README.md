# Trading decision Agent: weekly deterministic selection

This sidecar consumes recommendation facts; it does not modify document extraction or ask an LLM to set trade parameters. Final executable prices, lot sizing and risk limits remain in Go `internal/rules` / trading service.

## Strategy contract

- Default date mode `WEEK_NATURAL_DAYS`: seven Asia/Shanghai calendar dates including the decision date. `recommend_date` determines membership; `created_at` and `updated_at` must not exceed the decision time. Explicit legacy modes remain available for rollback.
- Missing source price alone is allowed by the existing exact review-reason check. No other review reason is bypassed.
- Fetch all candidate pages before ranking; ID keyset pagination supports late ingestion and empty filtered pages. Exceeding the configured scan budget is an error, never a silent first-page portfolio.
- `TREND_PULLBACK_V1`: 20 completed and aligned sessions, benchmark close and MA5 above/equal MA20; stock trend and relative strength; ATR14/close <= 8%; entry no more than 5% above MA5 and not below MA20; no new daily low-close trend and volume at least half the prior-five average. Watch/reject reasons are audited. These are initial research parameters, not a profitability guarantee.
- Preserve recommendation confidence 0.65, blogger minimum 10 samples and 0.55 win rate. The blogger gate uses historically available **30-trading-day evaluation outcomes**, not a one-week win-rate statistic. Its history is independent of the seven-date recommendation pool.
- Existing composite ranking: 40% extraction confidence + 35% blogger win rate + 25% technical score. Select at most one intent per symbol; preserve all recommendation evidence. Proposed 3% allocation is independent of source article sizing and is still capped by Go risk/sizing.
- Exit monitoring remains the independent Go service, not dependent on this sidecar or new recommendations.
- Daily features exclude the current incomplete session and require the latest completed exchange date. Missing benchmark/history or suspected corporate-action discontinuity fails closed.
- Intraday market snapshots preserve fresh quotes per symbol when a batch is only partially available. Stale, missing or invalid quotes return an empty price and `missing_reason=REALTIME_QUOTE_UNAVAILABLE`; the Agent must not turn these into entry prices or silently use yesterday's close. Explicitly historical/out-of-window lookups keep the daily-data path.
- Quote age is checked against retrieval time (including time spent waiting), not a minute-truncated decision timestamp. Execution, independent exits and UAT still require a complete fresh response; a partial response is selection evidence only, never order authorization.

Business configuration is in Nacos. Examples show strategy version `3.0.0`; editing examples does **not** publish it. Set `recommendation_date_mode=WEEK_NATURAL_DAYS` explicitly during a separately authorized deployment, together with the strategy identity and parameters. Existing explicit legacy modes do not automatically migrate. Production also needs the benchmark (default `510300.SH`) in maintained daily-market data; there is no remote-data fallback inside deterministic selection.

## Offline checks (no deployment / no broker)

From the repository root:

```sh
python3 -B scripts/export_trading_replay.py --end 2026-09-30 --output tmp/trading_replay_20261001/input.json
python3 -B -m trading_agent.replay --input tmp/trading_replay_20261001/input.json --output tmp/trading_replay_20261001/result.json
agent/.venv/bin/python -m pytest trading_agent/tests -q
```

The exporter reads Nacos by HTTP, MySQL by `dbx` SELECT only, and can download an absent benchmark / globally missing warm-up sessions from the existing Tushare subscription into the **local dataset only**. It does not write MySQL or Nacos. No credential is written into the dataset. Run replay from the frozen dataset to reproduce results; re-exporting is not an immutable snapshot of an earlier export.

The replay uses previous completed bars for decisions and daily-open limit matching as an explicitly simplified research model. It does not certify fills after production's 09:35 execution-window start. Daily bars cannot establish queue priority, partial-fill timing, exact slippage or real broker callbacks. Conservative commission/stamp-tax costs are modeled, not asserted to match the account fee contract; transfer fees are not certified. Historical rows revised after the decision are excluded, rather than backdated. Zero trades is a valid result but not natural roundtrip acceptance.

For the actual Go service's seeded historical-price mechanics check, set `FINANCE_OFFLINE_REPLAY_DATASET` to the absolute exported file path while using a clean TEST environment, then run:

```sh
go test ./internal/trading/service -run TestHistoricalDailyBarsGoLifecycle -count=1 -v
```

This test only instantiates an in-memory SQLite database and fake Bridge. It drives the real fill handler, partial-buy cancellation, holding cycle, independent Go exit, duplicate-event guard, strict reconciliation and day closure. It deliberately seeds an entry and a one-day holding limit **inside the fixture**: do not count its result as naturally selected trading, do not copy fixture parameters into production, and do not treat it as permission to disable production kill switches.

Use `python3 -B start_prod.py --build-only` for all standard build gates. Deploying / enabling simulation trading is a separate operation requiring authorization and safety checks.
