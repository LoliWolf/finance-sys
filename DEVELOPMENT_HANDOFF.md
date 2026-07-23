# finance-sys 开发交接说明

> 更新日期：2026-07-22  
> 用途：从 Windows 切换到 macOS 后快速恢复开发，并明确当前实现边界和下一阶段工作。  
> 详细产品和技术方案仍以[飞书知识库](https://my.feishu.cn/wiki/DAUVw7Lr6i7L3XkulELcWmoCnLh)为准。

> 2026-07-23 运行环境更新：本文第 1 节的分支/提交状态是交接时快照。当前 macOS/Windows 安装、Nacos bootstrap、启动脚本和 OCR 命令以仓库 `README.md` 为准。

## 1. 当前代码状态

- 当前分支：`feat/track`
- 远端跟踪分支：`origin/feat/track`
- 基线：本地 `master` 与 `origin/master` 一致
- 相对 `master`：领先 3 个提交，无分叉
- 编写本交接文档前：业务代码工作区干净
- 当前未提交文件：`DEVELOPMENT_HANDOFF.md`
- 最近提交：
  - `578df65 feat: add market data sync and stock daily quote module`
  - `8d4a6f2 feat(market-data): add stock daily sync and tushare multi-token support`
  - `6154cab chore: 整理数据库迁移脚本，拆分独立迁移文件并更新主DDL`
- 最近验证：
  - `go test -count=1 ./...` 通过
  - `go build ./...` 通过
  - 真实 MySQL/Tushare 集成测试默认由环境变量门禁跳过

`feat/track` 的业务代码已经推送到远端；本交接文档需要提交并推送后才能直接在 Mac 上获取。Mac 上应先检出 `feat/track`，不要直接从 `master` 开始后续开发。

## 2. 项目当前定位

项目面向中国市场研究场景，目标是把博主文章、专家文档、研报摘录等研究线索转换为可审计的推荐事实，并进一步追踪推荐后的股票走势，形成博主胜率、收益率和排行榜。

当前整体链路：

```text
研究文档
-> 文本提取与 chunk
-> LLM / Agent 结构化抽取
-> 本地证券主数据解析
-> 确定性规则生成候选计划
-> RecommendationEvent 推荐事实
-> Tushare 日行情同步
-> [下一阶段] 推荐后 5/10/30/90 交易日评估
-> [下一阶段] 博主统计、排行榜和前端可视化
```

工程边界：LLM/Agent 只负责抽取和标的解析辅助；交易参数、行情同步、收益计算和排行榜必须由 Go 确定性代码完成。

## 3. 已实现功能

### 3.1 文档到推荐事件

- 上传 `pdf`、`doc`、`docx`、`txt`、`md`、`csv` 等文件。
- 计算 SHA256 去重，原始文件写入 MySQL。
- 提取纯文本并切分 chunk，PDF 可走 OCR 兜底。
- 支持 Go 侧 LLM 分析和 Python Agent sidecar 分析。
- 使用 `security_master`、证券别名和 Agent 工具结果解析标的。
- 不可识别、歧义、板块和主题类目标会记录为不可追踪目标。
- `internal/rules` 确定性生成入场、止损、止盈和仓位。
- 持久化 `trade_candidate_plans`、`recommendation_events`、解析观测和证据。
- 已有文档、计划、推荐事件、解析记录等查询 API。

### 3.2 证券主数据

- `security_master` 保存 A 股、ETF 等证券基础信息。
- `cmd/init-tushare-security` 可通过 Tushare 初始化或刷新证券主数据。
- 股票日行情关联使用 `ts_code`，并兼容本地 `STOCK` 与 provider `A_SHARE` 的资产类型差异。

### 3.3 股票日行情异步同步

已接入 `github.com/yushikuann/go-tushare-sdk`，实现由 HTTP 接口触发、worker 后台执行的单日行情同步。

创建任务：

```http
POST /api/v1/admin/market/stock-daily/sync
Content-Type: application/json

{
  "trade_date": "2026-06-28"
}
```

接口返回 `202 Accepted` 和 `QUEUED` 任务，不等待 Tushare 和数据库处理完成。

查询任务：

```http
GET /api/v1/admin/market/sync-runs?sync_type=stock_daily&trade_date=2026-06-28&limit=50
```

执行过程：

1. worker 认领最早的 `QUEUED` 任务并标记为 `RUNNING`。
2. 调用 Tushare `daily`，一次获取指定日期的全部 A 股行情。
3. 调用 Tushare `fund_daily`，一次获取指定日期的全部 ETF 行情。
4. 通过 `ts_code` 与本地 `security_master` 关联。
5. 只保存本地证券表中存在且资产类型匹配的证券。
6. 未返回、provider 空结果或 provider 错误写入缺失明细。
7. 更新任务为 `SUCCEEDED`、`PARTIAL_FAILED` 或 `FAILED`。

任务内部对 provider 结果的关联和转换使用 10 并发；A 股与 ETF 的两次 provider 请求当前顺序执行。

### 3.4 行情落库和幂等

新增表：

- `stock_daily_quotes`：每日行情事实表。
- `market_data_sync_runs`：异步同步任务及统计。
- `market_data_sync_missing_items`：本地证券未获得行情的缺失明细。

`stock_daily_quotes` 常用列包括：

- 证券属性：`security_master_id`、`ts_code`、`symbol`、名称、交易所、市场、资产类型、行业、上市状态。
- 行情属性：交易日、开盘、最高、最低、收盘、昨收、涨跌额、涨跌幅、成交量、成交额。
- 来源属性：`source`、`config_version`。
- 原始数据：完整 Tushare 行记录保存在 `tushare_content`。

唯一键为：

```text
(ts_code, trade_date, source)
```

重复同步同一证券、同一日期时执行 upsert，不会生成重复行情记录。成功任务本身可以再次创建，但行情事实仍保持幂等。

### 3.5 Nacos 市场数据配置

已经增加：

- `market_data.enabled`
- Tushare 开关、SDK 包名、超时和 Token 列表
- 每个 Token 的 `alias`、`enabled`、`weight`
- worker 的轮询、认领超时、并发任务数和批大小
- 股票/ETF 同步范围、字段列表、原始单位和原始内容保存开关
- 缺失明细开关

真实 Token 只能维护在 Nacos，不能写入仓库。仓库示例配置默认关闭市场数据功能。

### 3.6 历史回填测试

`internal/service/market_data_backfill_integration_test.go` 可以从 2026-01-01 扫到执行当天：

```bash
export FINANCE_SYS_RUN_REAL_STOCK_DAILY_BACKFILL=1
export FINANCE_SYS_STOCK_DAILY_BACKFILL_CONCURRENCY=1
go test -run TestRealBackfillStockDailyFrom20260101ToToday -v ./internal/service
```

该测试会连接 Nacos 配置中的真实 MySQL 和 Tushare，并写真实数据。历史回填曾执行过，但迁移到 Mac 后应查询数据库实际最大日期，不要假设已经追到当前日期：

```sql
SELECT asset_type, MIN(trade_date), MAX(trade_date), COUNT(*)
FROM stock_daily_quotes
GROUP BY asset_type;
```

## 4. 当前未闭环问题

以下配置已经定义和校验，但尚未完整参与运行逻辑：

- Token `weight` 尚未用于加权选择。
- `max_retries` 和 `token_cooldown_ms` 尚未形成真正的重试与冷却调度。
- worker 的 `max_concurrent_runs`、`claim_timeout_ms`、`batch_size` 尚未生效。
- worker 当前一次只消费一个同步任务，没有超时 `RUNNING` 任务回收机制。

行情语义方面：

- 尚未同步交易日历。
- 非交易日目前会表现为 `PROVIDER_EMPTY`，并可能给所有本地证券生成缺失明细。
- 尚未同步复权因子，当前价格和成交数据均保留 Tushare 原始单位。
- 停牌、非交易日、接口权限不足和真正漏数还不能被完全准确地区分。
- 当前只有任务列表接口，没有任务详情、缺失明细查询和失败任务重试接口。

仓库维护方面：

- `migrations/DDL.sql` 当前更接近数据库结构快照，包含已有库的 `AUTO_INCREMENT` 值。
- DDL 中部分外键引用表位于被引用表之前，不适合直接用于全新数据库初始化。
- 不应在新 Mac 的空数据库上盲目执行当前完整 DDL；应先整理成可重复执行的基线和增量迁移。
- README 仍把行情同步描述为“后续规划”，与当前分支实现不一致。
- `AGENTS.md` 的飞书目录尚未加入第 12 篇动态分析方案。

## 5. 下一阶段实现计划

完整设计见飞书：[12 推荐后动态分析与博主胜率排行榜技术方案](https://my.feishu.cn/wiki/Yu6sw4kXxiUcobksmvDcLiY0noe)。

### P0：收口现有行情模块

1. 整理数据库基线和增量迁移，移除结构快照中的自增值。
2. 增加交易日历，非交易日任务明确标记，不批量制造证券缺失明细。
3. 落实 Token 轮转、重试、冷却和 worker 配置语义。
4. 增加任务详情、缺失明细和失败任务重试接口。
5. 更新 README 和 `AGENTS.md` 的当前状态。

### P1：补齐动态评估所需市场数据

- 同步交易日历。
- 同步复权因子，原始日行情继续原单位保存。
- 收益计算使用可复现的复权价格或复权收益序列。
- 明确停牌、缺行情、窗口尚未结束等状态。

### P2：推荐事件窗口评估

新增：

- `recommendation_evaluation_runs`
- `recommendation_event_window_metrics`
- `internal/evaluation`

评估以稳定的 `recommendation_events.id` 为主键，不直接依赖可能随重分析变化的 `trade_candidate_plans.plan_id`。

第一版窗口：`5`、`10`、`30`、`90` 个交易日。

建议口径：

- 推荐后第一个可交易日开盘价作为 `entry_price`。
- 窗口最后一个交易日收盘价作为 `exit_close_price`。
- 多头方向收益：`exit / entry - 1`。
- 空头方向收益：对原始收益取反。
- 同时计算胜负、最大有利波动、最大不利波动、最大回撤、最佳/最差日期。
- 只有 `READY` 样本进入胜率分母。
- 窗口未结束、行情不完整、证券未解析和资产不支持分别记录状态，不伪造收益。

结果表唯一约束：

```text
(recommendation_event_id, window_days, quote_source)
```

### P3：异步评估和查询 API

计划实现：

- `POST /api/v1/admin/evaluations/recommendations/runs`
- `GET /api/v1/admin/evaluations/recommendations/runs`
- `GET /api/v1/admin/evaluations/recommendations/runs/{id}`
- `GET /api/v1/recommendations/{id}/performance`
- `GET /api/v1/recommendations/{id}/price-series`
- `GET /api/v1/recommendation-performance`
- `GET /api/v1/bloggers/rankings`
- `GET /api/v1/bloggers/{id}/performance/summary`
- `GET /api/v1/bloggers/{id}/performance/timeseries`
- `GET /api/v1/bloggers/{id}/recommendations/performance`

第一版排行榜可直接基于窗口指标表 SQL 聚合；数据量明显增长后再增加日级聚合表。

### P4：前端可视化

至少包含：

- 博主排行榜：样本数、可评估数、胜率、平均/中位收益、最大浮盈和最大不利波动。
- 博主详情：不同窗口指标、收益趋势、推荐列表和状态分布。
- 推荐详情：推荐信息、入场/退出点、K 线或价格曲线、5/10/30/90 日指标。
- 标的视角：同一股票被不同博主推荐后的表现对比。
- 数据质量视图：待评估、行情缺失、标的未识别和任务失败数量。

## 6. macOS 开发环境恢复

### 6.1 拉取代码

```bash
git clone <repository-url>
cd finance-sys
git checkout feat/track
git pull --ff-only origin feat/track
git status
```

### 6.2 安装基础环境

`go.mod` 声明 Go `1.22.11`，Python Agent 要求 Python `>=3.9`。Go 可用官方归档或版本管理器安装 1.22.x；本地命令不依赖固定 Homebrew 前缀。

```bash
go version
python3 --version
pdftoppm -v
```

MySQL、Nacos、LLM 和 Tushare 继续使用 Nacos 指向的远程服务，不在 Mac 另外安装中间件。macOS 解析旧 `.doc` 优先使用系统 `textutil`，不需要 antiword。Intel 和 Apple Silicon 的 Homebrew 前缀不同，脚本不写死前缀。

### 6.3 配置 Nacos

Go 服务和 Python Agent 读取同一组 bootstrap 环境变量。项目当前 Nacos 地址已在示例中；一般只需复制文件，切换网络时再改地址：

```bash
cp bootstrap_go122.env.example bootstrap_go122.env
# 必要时仅修改 NACOS_SERVER_ADDR=<reachable-host>:8848
```

代码固定使用 `public / DEFAULT_GROUP / expert_trade` 定位配置文档，bootstrap 文件只保留 Nacos 地址。MySQL DSN、HTTP 端口、LLM Key、Tushare Token 和业务开关只维护在 Nacos JSON，不要写入 Git。非地址 bootstrap 键会被启动脚本拒绝；启动脚本会从 Nacos 动态读取 HTTP 端口做健康检查。

Go 主程序优先通过环境变量中的 Nacos 地址读取配置；地址缺失或 Nacos 读取失败时，降级读取 `configs/example_nacos_config.json`，用于本地开发调试。

### 6.4 验证 Go 服务

当前 Nacos 开启 Agent 路由。先按 6.5 在一个终端启动 Agent，再在另一个终端启动 API：

```bash
go mod download
GOTOOLCHAIN=local go test -count=1 ./...
GOTOOLCHAIN=local go build ./...
./debug_api_nacos.sh
```

默认 HTTP 地址由 Nacos 决定，当前项目配置为：

```text
http://127.0.0.1:30005
```

健康检查：

```bash
curl http://127.0.0.1:30005/healthz
```

需要后台运行和自动打开上传页时使用 `./start_api_nacos.sh`。Windows 保留对等的 `debug_api_nacos.bat` 和 `start_api_nacos.bat`。

### 6.5 启动 Python Agent

```bash
cd agent
python3 -m venv .venv
source .venv/bin/activate
python -m pip install -e '.[test]'
python -m pytest -q
python -m app.runner
```

Python Agent 不是行情同步的依赖；只有文档分析配置为 Agent 路由时才需要启动。

### 6.6 数据库注意事项

- 优先连接已经执行过 DDL 和历史回填的现有 MySQL。
- 先确认 Nacos 中的 DSN 指向正确环境，再运行 API 或集成测试。
- `go run generate.go` 会连接配置中的真实数据库并更新生成模型，仅在表结构变更后运行。
- 带 `FINANCE_SYS_*_INTEGRATION`、`DML_ACK` 或真实回填开关的测试可能写库，不要在不了解目标数据库时启用。

建议首次启动后检查：

```sql
SELECT COUNT(*) FROM security_master WHERE is_active = 1;
SELECT asset_type, MAX(trade_date), COUNT(*)
FROM stock_daily_quotes
GROUP BY asset_type;
SELECT status, COUNT(*)
FROM market_data_sync_runs
GROUP BY status;
```

## 7. Mac 恢复后的首个开发任务

建议先完成 P0，不直接跳到排行榜。动态分析依赖正确的交易日和行情状态，如果继续把非交易日当作 `PROVIDER_EMPTY`，后续胜率分母、待评估状态和缺失率都会失真。

P0 合并后，再按以下顺序进入动态分析：

```text
DDL 与生成模型
-> 纯计算器及单元测试
-> DAL
-> 异步评估 service/worker
-> 推荐表现 API
-> 博主统计与排行榜 API
-> 前端可视化
```

每一步都应保持幂等，并保存 `calc_version`、`config_version` 和失败原因，确保以后调整口径时可以重算和审计。
