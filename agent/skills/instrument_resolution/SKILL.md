---
name: instrument_resolution
version: instrument-resolution-sector-v1
description: Rules for Chinese A-share, ETF, and Eastmoney sector resolution.
---

# Instrument Resolution Rules 标的解析规则

## Hard Constraints 强约束

1. 不允许编造 `ts_code`。
2. 只有本地主数据验证通过的 A 股、ETF 或 `BKxxxx.DC` 板块指数才能进入 `candidate_plan_inputs`。
3. 板块只形成推荐事实和行情评估，不进入 Go 交易规则层，不生成交易参数。
4. 交易价格和仓位只能由 Go `internal/rules` 生成。
5. 原文没有价格时，`reference_price=0` 且 `reference_price_note="price_missing_in_text"`。
6. 证据必须来自原文块，不允许凭空生成。
7. 不允许输出 `entry_price`、`stop_loss`、`take_profit`、`position_pct`。

## Trackable Targets 可追踪目标

1. A 股股票。
2. A 股 ETF。
3. 东方财富板块指数，例如 `BK1128.DC`、`CPO概念`。
4. 明确证券代码，例如 `300502.SZ`。
5. 明确公司、ETF 或板块简称及常用别名。

## Untrackable Targets 不可追踪目标

1. 无法唯一映射到本地 `BKxxxx.DC` 的板块、行业、主题或概念。
2. 非东方财富 BK 体系且当前没有行情链路的指数，例如 `沪深300指数`。
3. 泛称，例如 `A股贵金属个股`、`龙头股`、`相关标的`。

## Ambiguity 多候选和歧义

1. 不能确定唯一证券时，不要编造证券代码。
2. 多候选不确定时保留原始标的，让 Go 主系统继续校验。
3. MCP 返回候选必须回本地 `security_master` 校验，M5 不直接调用 MCP。

## Evidence 证据

1. 每个原始意图必须至少包含一条证据。
2. 证据文本必须来自用户文档原文。
3. 证据的 `chunk_index` 必须对应输入文本块。

## Output JSON 输出要求

1. 只返回 JSON。
2. `direction` 只能是 `LONG` 或 `SHORT`。
3. `confidence` 必须在 `(0,1]`。
4. 不能输出第三方模型供应商的原始字段。
