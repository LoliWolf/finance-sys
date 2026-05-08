# 剩余部分技术方案

## 当前 MVP 基线

现有系统已经形成以下链路：

1. HTTP 上传文档。
2. SHA256 去重。
3. MySQL 保存原始文件字节。
4. `internal/parser` 提取纯文本并切 chunk。
5. `internal/llm` 调用 OpenAI 兼容接口抽取 `PlanIntent`。
6. `internal/rules` 生成 `CandidatePlan`。
7. `trade_candidate_plans` 持久化并支持查询。

剩余技术方案只在该基线上扩展，不改变现有边界：AI 仍只做结构化抽取，行情和收益计算必须走确定性代码。

## 模块设计

新增目录建议：

- `internal/marketdata`：Tushare client、请求签名、限速、响应解析。
- `internal/evaluation`：推荐事件评估、收益率计算、窗口计算。
- `internal/stats`：聚合统计查询与指标计算。

继续使用：

- `internal/repository`：封装所有数据库读写。
- `sqlc/query`：维护 SQL 源。
- `migrations`：维护表结构。
- `internal/config`：增加 Tushare 和评估配置。
- `internal/httpapi`：暴露管理与查询 API。

## 配置扩展

新增配置块：

```json
{
  "market_data": {
    "provider": "tushare",
    "tushare_endpoint": "https://api.tushare.pro",
    "tushare_token": "",
    "timeout_ms": 10000,
    "rate_limit_per_minute": 180
  },
  "evaluation": {
    "enabled": true,
    "windows": [1, 5, 10, 20],
    "entry_price_policy": "T_PLUS_1_OPEN",
    "exit_price_policy": "WINDOW_CLOSE",
    "adjustment": "QFQ"
  }
}
```

同步更新：

- `internal/config/types.go`
- `internal/config/validate.go`
- `configs/example_nacos_config.json`
- `configs/example_nacos_config.annotated.jsonc`

Token 不应提交真实值，生产环境继续由 Nacos 配置。

## 数据库设计

### bloggers

用于归一化博主身份。

字段：

- `id`
- `display_name`
- `normalized_name`
- `source_type`
- `source_name`
- `aliases_json`
- `created_at`
- `updated_at`

唯一键：

- `uk_bloggers_normalized_source(normalized_name, source_type, source_name)`

### recommendation_events

从候选计划派生的标准推荐事实。

字段：

- `id`
- `plan_id`
- `document_id`
- `parse_run_id`
- `blogger_id`
- `blogger_name`
- `symbol`
- `direction`
- `recommend_date`
- `reference_price`
- `confidence`
- `status`
- `thesis`
- `evidence_json`
- `dedup_key`
- `created_at`
- `updated_at`

唯一键：

- `uk_recommendation_events_dedup(dedup_key)`

### stock_basic

Tushare 股票基础信息。

字段：

- `ts_code`
- `symbol`
- `name`
- `area`
- `industry`
- `market`
- `list_status`
- `list_date`
- `delist_date`
- `updated_at`

### trade_calendars

交易日历。

字段：

- `exchange`
- `cal_date`
- `is_open`
- `pretrade_date`

唯一键：

- `uk_trade_calendars_exchange_date(exchange, cal_date)`

### stock_daily_bars

日线行情。

字段：

- `ts_code`
- `trade_date`
- `open`
- `high`
- `low`
- `close`
- `pre_close`
- `vol`
- `amount`
- `adj_factor`
- `created_at`
- `updated_at`

唯一键：

- `uk_stock_daily_bars_code_date(ts_code, trade_date)`

### recommendation_evaluations

单条推荐在某窗口下的评估结果。

字段：

- `id`
- `recommendation_id`
- `window_days`
- `entry_date`
- `exit_date`
- `entry_price`
- `exit_price`
- `return_pct`
- `is_win`
- `hit_take_profit`
- `hit_stop_loss`
- `max_favorable_pct`
- `max_drawdown_pct`
- `status`
- `error_message`
- `created_at`
- `updated_at`

唯一键：

- `uk_recommendation_evaluations_event_window(recommendation_id, window_days)`

## 处理流程

### 1. 候选计划派生推荐事件

触发点：

- `AnalyzeDocument` 保存候选计划后立即派生。
- 或提供管理接口对历史计划补派生。

规则：

- `blogger_name` 优先使用 `CandidatePlan.Analyst`。
- 如果为空，回退到 `Document.Author`。
- 如果仍为空，状态标记为 `NEEDS_REVIEW`。
- `recommend_date` 优先使用计划 `TradeDate` 的前一自然日或文档创建日；第一版建议使用文档创建日归一到配置时区。

### 2. Tushare 同步

Tushare client 封装在 `internal/marketdata`，业务层只接收领域结构，不暴露 Tushare 原始字段。

同步接口：

- `SyncStockBasic`
- `SyncTradeCalendar`
- `SyncDailyBars(symbols, from, to)`

要求：

- 请求有超时。
- 响应字段做结构化校验。
- Upsert 写入，重复执行幂等。
- 记录同步范围、状态、错误消息。

### 3. 推荐评估

输入：

- `RecommendationEvent`
- 评估配置
- 交易日历
- 日线行情

步骤：

1. 找到推荐日之后第一个开放交易日作为入场日。
2. 根据窗口找到退出交易日。
3. 读取入场到退出期间的日线。
4. 根据方向计算收益率。
5. 根据候选计划中的止盈/止损判断是否命中。
6. 写入 `recommendation_evaluations`。

不可评估状态：

- `MISSING_MARKET_DATA`
- `SUSPENDED`
- `INVALID_SYMBOL`
- `WINDOW_NOT_COMPLETE`
- `MISSING_ENTRY_PRICE`

### 4. 聚合统计

第一版直接 SQL 聚合，不新增缓存。

聚合维度：

- 博主
- 股票
- 方向
- 窗口
- 日期区间

核心 SQL 从 `recommendation_events` join `recommendation_evaluations`，只统计 `evaluation.status = 'EVALUATED'`。

## API 设计

### 推荐事件

`GET /api/v1/recommendations`

返回字段：

- 推荐 ID
- 博主
- 股票
- 方向
- 推荐日期
- 参考价
- 置信度
- 状态

`GET /api/v1/recommendations/{id}`

返回推荐详情、证据、候选计划和各窗口评估。

### 行情同步

`POST /api/v1/admin/market/sync-daily`

请求：

```json
{
  "from": "2026-01-01",
  "to": "2026-03-31",
  "symbols": ["600519.SH", "000001.SZ"]
}
```

### 评估刷新

`POST /api/v1/recommendations/{id}/refresh-evaluation`

根据当前行情重新计算所有窗口。

### 统计

`GET /api/v1/stats/bloggers?from=2026-01-01&to=2026-03-31&window=5&min_samples=3`

返回：

- `blogger_name`
- `sample_count`
- `evaluated_count`
- `win_rate`
- `avg_return_pct`
- `median_return_pct`
- `take_profit_hit_rate`
- `stop_loss_hit_rate`

## 实现顺序

### 阶段 1：表结构与推荐事件

1. 新增 migrations。
2. 新增 sqlc query。
3. 生成 sqlc。
4. 在 repository 增加推荐事件读写。
5. 在 service 中从候选计划派生推荐事件。
6. 增加单元测试。

### 阶段 2：Tushare client

1. 增加配置。
2. 实现 client 和响应解析。
3. 实现股票基础信息、交易日历、日线同步。
4. 增加 httptest 覆盖异常响应和字段缺失。

### 阶段 3：评估引擎

1. 实现交易窗口计算。
2. 实现收益率、最大浮盈、最大回撤。
3. 写入评估结果。
4. 增加固定行情样例测试，确保确定性。

### 阶段 4：统计 API

1. 增加聚合 SQL。
2. 增加 stats service。
3. 暴露查询 API。
4. 增加集成级 repository 测试或 SQL 回归样例。

## 测试策略

- `internal/marketdata`：使用 `httptest` 模拟 Tushare 响应。
- `internal/evaluation`：使用固定交易日历和行情数组测试确定性结果。
- `internal/stats`：使用最小数据集验证胜率和收益率口径。
- API 层：覆盖参数校验、空结果、非法窗口。

提交前继续执行：

```bash
env GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go test ./...
env GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go build ./...
```

## 风险与处理

- Tushare 限频：client 内置简单限速，批量同步按日期和股票分批。
- 停牌或行情缺失：评估结果状态化，不写假收益。
- 博主名称不一致：第一版用归一化名称和 alias JSON，后续再加人工合并接口。
- 复权口径变更：评估结果记录配置版本，必要时批量重算。
- 样本偏差：统计接口同时返回样本数和可评估数，避免小样本误导。

