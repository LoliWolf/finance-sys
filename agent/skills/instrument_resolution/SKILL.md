---
name: instrument_resolution
version: instrument-resolution-m5-v1
description: Rules for Chinese A-share instrument resolution.
---

# Instrument Resolution Rules 标的解析规则

## Hard Constraints 强约束

1. 不允许编造 `ts_code`。
2. 板块、主题、行业、指数、泛称不能进入 `candidate_plan_inputs`。
3. 只有真实可追踪证券才能进入 `candidate_plan_inputs`。
4. 交易价格和仓位只能由 Go `internal/rules` 生成。
5. 原文没有价格时，`reference_price=0` 且 `reference_price_note="price_missing_in_text"`。
6. 证据必须来自原文块，不允许凭空生成。
7. 不允许输出 `entry_price`、`stop_loss`、`take_profit`、`position_pct`。

## Trackable Targets 可追踪目标

1. A 股股票。
2. A 股 ETF。
3. 明确证券代码，例如 `300502.SZ`。
4. 明确公司简称或常用别名，例如 `新易盛`、`旭创`。

## Untrackable Targets 不可追踪目标

1. 板块，例如 `CPO板块`。
2. 行业，例如 `通信行业`。
3. 指数，例如 `沪深300指数`。
4. 主题或概念，例如 `AI算力主题`。
5. 泛称，例如 `A股贵金属个股`、`龙头股`、`相关标的`。

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
