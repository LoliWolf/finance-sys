你是这个仓库的首席工程师与交付负责人。不要停留在讨论层，直接把系统落地为可运行代码、测试、数据库迁移、Docker 运行环境、样例数据和文档。你需要在当前仓库中从零实现一个“专家每日文档 / PDF / 研报 / 邮件 -> 次日交易操作建议 -> 次日价格与走势跟踪 -> 有效性评估 -> 专家评分”的完整系统。

这次实现的硬要求是：**后端主系统必须使用 Go 1.22**。你可以为了 OCR 或桥接免费行情源而引入独立 sidecar / bridge 服务，但核心 API、调度、规则、配置加载、数据落库、审批、评估、报表都必须是 Go 1.22 工程。

不要先反问。先给出：
1. 完整文件树；
2. 技术决策说明；
3. 分阶段提交计划；
4. 然后开始直接写代码。

--------------------------------
# 1. 项目目标
--------------------------------

构建一个面向中国市场的研究工作流平台，优先覆盖：
- A 股
- ETF
- 沪深主要指数

每日输入是 PDF、DOCX、HTML、TXT、EML 等文档；系统自动完成：
1. 文档摄取与去重；
2. 文档解析与结构化；
3. LLM 抽取专家观点与证据片段；
4. 标的标准化；
5. 拉取 T 日收盘后的市场上下文；
6. 用确定性规则生成 T+1 候选交易计划；
7. 支持人工审批后发布；
8. T+1 抓价格与走势，评估是否触发、是否成功；
9. 形成专家胜率、收益、MFE、MAE、超额收益、主题命中率等面板。

系统只做研究、建议、跟踪、评估，不接券商实盘，不自动下单。

--------------------------------
# 2. 强约束
--------------------------------

## 2.1 后端技术栈必须为 Go 1.22

必须使用：
- Go 1.22
- `chi/v5` 作为 HTTP Router
- `slog` 作为日志基础，输出 JSON 日志
- `pgx/v5` 作为 PostgreSQL 连接池
- `sqlc` 生成类型安全查询
- `golang-migrate` 管理数据库迁移
- `redis/go-redis/v9`
- `minio-go/v7`
- `nacos-sdk-go/v2`
- `robfig/cron/v3`
- 标准 `net/http` 或 `resty` 封装外部调用
- `testing` + `testify` 做单元/集成测试

优先目录结构：
- `cmd/api`
- `cmd/worker`
- `internal/bootstrap`
- `internal/config`
- `internal/nacoscfg`
- `internal/httpapi`
- `internal/domain`
- `internal/repository`
- `internal/service`
- `internal/market`
- `internal/parser`
- `internal/llm`
- `internal/rules`
- `internal/evaluation`
- `internal/report`
- `internal/approval`
- `internal/scheduler`
- `internal/storage`
- `internal/telemetry`
- `internal/utils`
- `migrations`
- `sqlc`
- `deploy`

不要使用 GORM。数据库访问必须基于 `pgx + sqlc`。
不要把所有逻辑堆到 `main.go`。
不要把 provider 返回的第三方字段名泄漏到业务层。

## 2.2 配置中心：全部业务配置只能来自 Nacos 单个 JSON 文档

应用启动时，只有以下引导参数允许来自环境变量：
- `NACOS_SERVER_ADDR`
- `NACOS_NAMESPACE`
- `NACOS_GROUP`
- `NACOS_DATA_ID`
- `NACOS_USERNAME`
- `NACOS_PASSWORD`

除此之外，不允许把业务配置散落在：
- yaml
- ini
- toml
- `.env`
- 本地 hardcode 常量

必须实现：
- `internal/config/types.go`：配置结构体
- `internal/config/validate.go`：配置合法性校验
- `internal/nacoscfg/loader.go`：启动拉取 Nacos JSON
- `internal/nacoscfg/watcher.go`：轮询或监听变更
- `internal/config/runtime.go`：原子热刷新与只读快照
- `configs/example_nacos_config.json`：完整样例
- `internal/config/snapshot.go`：把当前生效配置版本落表，便于审计

要求：
- 启动拉取失败时支持 fail-fast 和 last-good-cache 两种模式；
- 配置刷新后必须校验 schema；
- 不合法配置不能覆盖当前生效配置；
- 当前配置版本号必须体现在日志、任务、计划、评估记录里。

## 2.3 行情数据层必须支持“国内可用、免费、可替换”

由于核心后端是 Go 1.22，不能直接依赖 Python 包作为强运行时前提，因此行情层必须抽象为统一 provider interface，并支持两类实现：

### A. 直接 HTTP 免费源（默认主链路）
- `EastmoneyHTTPProvider`
- `SinaHTTPProvider`

### B. 可选桥接 / MCP 链路（增强能力）
- `AkshareBridgeProvider`
- `BaostockBridgeProvider`
- `MCPMarketProvider`

说明：
- 直接 HTTP provider 必须能在不依赖 Python 的情况下运行；
- `AkshareBridgeProvider` / `BaostockBridgeProvider` 不作为默认强依赖，只实现客户端、mock 和接口契约；
- `MCPMarketProvider` 也不作为默认强依赖，只实现接口与 mock server/client；
- 不要把 ChatGPT skills 设计成生产运行时依赖；如果开发环境里有 skills，只能当开发辅助，运行时仍然统一走 provider / HTTP bridge / MCP。

统一 interface 至少包含：
- `GetSecurityMaster(ctx, market, kind)`
- `GetRealtimeQuotes(ctx, symbols)`
- `GetDailyBars(ctx, symbol, start, end, adjust)`
- `GetMinuteBars(ctx, symbol, start, end, interval, adjust)`
- `GetTradingCalendar(ctx, start, end)`
- `GetCorporateActions(ctx, symbol, start, end)`
- `GetSuspensionStatus(ctx, symbols, tradeDate)`
- `HealthCheck(ctx)`

必须实现：
- provider chain
- 熔断
- 指数退避重试
- fallback
- 原始响应归档到 MinIO
- 健康检查
- 缓存
- 限流
- provider 级指标

默认可运行路径：
1. `EastmoneyHTTPProvider` 主用；
2. `SinaHTTPProvider` 兜底；
3. 其余桥接 provider 可选启用。

## 2.4 文档解析必须以 Go 为主，但允许 sidecar

因为后端核心必须是 Go 1.22，所以文档解析遵循：
- API / Worker / 状态机 / 调度 / 落库：Go 实现
- PDF 文本提取：优先调用本地 CLI，例如 `pdftotext`
- OCR：允许调用外部 PaddleOCR HTTP sidecar
- DOCX / HTML / TXT / EML：优先原生 Go 解析
- 表格抽取：v1 做 best-effort，支持 CLI / bridge / layout heuristic；要保留结构化表格和原始块

必须做：
- 文档 sha256 去重
- 页级文本保留
- clean text
- sections
- tables
- chunks
- parser metrics
- 扫描件与文本件判别
- 页眉页脚/免责声明/版权页噪音清洗
- OCR fallback 阈值控制
- parser failure dead-letter 处理

## 2.5 LLM 只允许做抽取，不允许直接定价

LLM 只允许做：
- 文档分类
- 摘要
- 专家观点抽取
- 风险点抽取
- 专家标签归纳
- 自然语言日报/周报生成

LLM 禁止直接输出：
- 最终入场价
- 止损价
- 止盈价
- 仓位比例
- 最终下单指令

这些交易参数必须由**确定性规则引擎**根据以下输入生成：
- T 日收盘价
- ATR / 波动率
- 最近 N 日涨跌幅
- 流动性
- ST / 停牌 / 退市风险
- 次日重大事件
- 板块/指数相对强弱
- 配置中心中的风险参数

LLM 输出必须是结构化 JSON，并严格经过 schema 校验。

## 2.6 全链路可审计、可复现、幂等

任何一次建议评估，都必须能回溯到：
- 原始文档
- 文档 hash
- 解析文本
- chunks / evidence spans
- 提取信号
- 生成计划时的市场快照
- 使用的配置版本
- 使用的规则版本
- T+1 行情数据
- 评估结果

所有任务都要幂等：
- 重复 ingest 不得重复建单；
- 重复评估不得生成多份冲突结果；
- 所有 cron 任务需要 job_run 记录和分布式锁。

--------------------------------
# 3. 功能范围
--------------------------------

## 3.1 文档摄取

支持三种入口：
1. 监听本地目录；
2. 监听 MinIO bucket 前缀；
3. API 上传。

支持文件类型：
- `.pdf`
- `.docx`
- `.txt`
- `.md`
- `.html`
- `.eml`

要求：
- 计算 sha256 去重；
- 记录 `source_type / source_name / author / institution / publish_time / title / file_path / object_key / mime_type`；
- 文档状态机：
  - `RECEIVED`
  - `STORED`
  - `PARSED`
  - `SIGNAL_EXTRACTED`
  - `PLANNED`
  - `APPROVED`
  - `EVALUATED`
  - `FAILED`

## 3.2 文档解析

输出至少包括：
- `page_texts`
- `clean_text`
- `sections`
- `tables`
- `chunks`
- `parser_metrics`

建议解析链：
1. PDF：
   - 先调用 `pdftotext`；
   - 若文本密度过低或乱码率过高，则走 OCR sidecar；
   - 表格抽取 best-effort；
2. DOCX：
   - 原生 Go 解析 OOXML；
3. HTML：
   - `goquery` 或等价方案清洗正文；
4. TXT / MD：
   - 直接读取；
5. EML：
   - 解析 subject、from、date、正文和附件元数据。

必须实现：
- chunk 切分；
- 标题层级尽力识别；
- 免责声明识别与剔除；
- 扫描件识别；
- parser metrics 持久化。

## 3.3 文档分类与观点抽取

设计严格 schema，至少包含：
- `document_type`
- `expert_name`
- `institution`
- `publish_time`
- `asset_universe`
- `symbols`
- `themes`
- `direction`
- `thesis`
- `catalysts`
- `risks`
- `time_horizon`
- `confidence`
- `evidence_spans`
- `disclaimers`

要求：
- 文档没有明确可执行标的时，`symbols` 可以为空；
- 若只有行业/主题观点，则允许生成 theme-level signal；
- symbol extraction 必须附 evidence spans；
- 禁止输出文档中未出现的具体股票。

## 3.4 标的标准化

必须做 symbol registry，覆盖：
- A 股主板 / 创业板 / 科创板 / 北交所
- ETF
- 沪深主要指数

统一代码格式：
- `600000.SH`
- `000001.SZ`
- `430047.BJ`
- `510300.SH`
- `000300.SH`

保留：
- `source_symbol`
- `source_name`
- `canonical_symbol`
- `canonical_name`
- `match_confidence`

必须实现：
- 中文简称映射；
- 同义词/别名映射；
- 模糊匹配；
- 人工修正接口；
- 交易所、证券类别、是否可交易标记。

## 3.5 交易计划生成

生成的是“次日候选交易计划”，不是投资建议口号。

计划字段至少包括：
- `plan_id`
- `signal_id`
- `trade_date`
- `symbol`
- `side`
- `setup_type`
- `entry_rule`
- `entry_price_low`
- `entry_price_high`
- `stop_loss`
- `take_profit_low`
- `take_profit_high`
- `max_holding_days`
- `invalid_if`
- `position_risk_pct`
- `confidence`
- `benchmark_symbol`
- `rule_version`
- `plan_status`

支持的 `setup_type`：
- `OPEN_FOLLOW`
- `OPEN_GAP_FILTER`
- `PULLBACK_TO_RANGE`
- `BREAKOUT_ABOVE_PREV_HIGH`
- `WATCHLIST_ONLY`

规则要求：
- 若流动性不足、不适合交易、或高开过度，则输出 `WATCHLIST_ONLY`；
- 默认 `max_holding_days = 1`；
- 单笔风险上限、单日风险上限由配置驱动；
- 计划生成必须记录使用的 market snapshot；
- 计划生成逻辑必须是纯函数化 + 规则版本化，便于复现。

## 3.6 人工审批

实现 plan approval：
- `DRAFT`
- `APPROVED`
- `REJECTED`
- `AUTO_APPROVED`

支持：
- API 审批；
- Feishu webhook 通知；
- 审批意见记录；
- 若配置关闭人工审批，则满足阈值的计划自动审批。

## 3.7 T+1 行情抓取与走势跟踪

为已审批计划抓取 T+1 行情：
- `pre_close`
- `open`
- `high`
- `low`
- `close`
- `volume`
- 分钟线或更高频快照（若分钟线可用）

要求：
- 优先 minute bars 判定触发；
- 没有 minute bars 时，必须降级并打标，不能静默成功；
- 记录使用了哪个 provider、哪个降级路径、数据完整性如何；
- 原始返回必须可归档。

## 3.8 评估逻辑

状态至少包括：
- `INVALIDATED`
- `NOT_TRIGGERED`
- `OPEN`
- `SUCCESS`
- `WEAK_SUCCESS`
- `FAIL`
- `DATA_INSUFFICIENT`

评估逻辑要求：
1. 先判断计划是否因 `invalid_if` 失效；
2. 再判断是否触发 entry rule；
3. 若触发，判断先到止盈还是先到止损；
4. 若都没到，则用收盘结果和阈值判断 `WEAK_SUCCESS` / `FAIL`；
5. 记录：
   - `entry_price`
   - `exit_price`
   - `close_price`
   - `pnl_pct`
   - `mfe_pct`
   - `mae_pct`
   - `benchmark_return_pct`
   - `excess_return_pct`
   - `evaluation_reason`
   - `data_quality_flag`

评估必须引用固定版本的规则，且可重复执行。

## 3.9 专家评分与报表

至少生成：
- 专家总文章数
- 生成计划数
- 触发率
- 成功率
- 平均收益
- 平均超额收益
- Profit Factor
- 平均 MFE / MAE
- 主题命中率
- 最近 20 笔表现

输出：
- JSON
- Markdown

支持：
- 每日晚报
- 每周复盘
- 专家排行榜

--------------------------------
# 4. 工程设计要求
--------------------------------

## 4.1 推荐文件树

请按下面思路创建仓库结构，并在生成代码时保持职责清晰：

```text
.
├── cmd/
│   ├── api/
│   │   └── main.go
│   └── worker/
│       └── main.go
├── internal/
│   ├── bootstrap/
│   ├── config/
│   ├── nacoscfg/
│   ├── httpapi/
│   │   ├── middleware/
│   │   ├── handlers/
│   │   └── dto/
│   ├── domain/
│   ├── repository/
│   ├── service/
│   ├── parser/
│   ├── llm/
│   ├── market/
│   │   ├── provider/
│   │   ├── bridge/
│   │   ├── cache/
│   │   └── symbol/
│   ├── rules/
│   ├── evaluation/
│   ├── approval/
│   ├── report/
│   ├── scheduler/
│   ├── storage/
│   ├── telemetry/
│   └── utils/
├── migrations/
├── sqlc/
│   ├── query/
│   └── sqlc.yaml
├── configs/
│   └── example_nacos_config.json
├── deploy/
│   ├── Dockerfile.api
│   ├── Dockerfile.worker
│   ├── docker-compose.yml
│   └── docker-compose.ocr.yml
├── testdata/
│   ├── sample_docs/
│   ├── market/
│   └── fixtures/
├── Makefile
├── go.mod
├── go.sum
└── README.md
```

## 4.2 数据库表

至少实现以下表及迁移：
- `documents`
- `document_blobs`
- `document_pages`
- `document_chunks`
- `document_tables`
- `parse_runs`
- `signals`
- `signal_evidence`
- `symbol_registry`
- `symbol_aliases`
- `market_snapshots`
- `market_daily_bars`
- `market_minute_bars`
- `trade_plans`
- `plan_approvals`
- `plan_evaluations`
- `expert_scorecards`
- `job_runs`
- `config_snapshots`
- `raw_provider_payloads`
- `dead_letters`

需要：
- 主键、外键、唯一索引、幂等约束、必要的 JSONB 字段；
- 常用查询索引；
- 配置版本、规则版本、provider 名称字段。

## 4.3 API 设计

至少提供以下接口：
- `POST /api/v1/documents/upload`
- `POST /api/v1/documents/ingest/scan-local`
- `POST /api/v1/documents/ingest/scan-object-storage`
- `GET /api/v1/documents`
- `GET /api/v1/documents/{id}`
- `GET /api/v1/signals`
- `GET /api/v1/plans`
- `GET /api/v1/plans/{id}`
- `POST /api/v1/plans/{id}/approve`
- `POST /api/v1/plans/{id}/reject`
- `GET /api/v1/evaluations`
- `GET /api/v1/experts/scorecards`
- `POST /api/v1/admin/reload-config`
- `GET /health/live`
- `GET /health/ready`
- `GET /metrics`

需要：
- 认证中间件（静态 token 即可）；
- request id；
- audit log；
- 分页；
- 基础错误码；
- OpenAPI 文档。

## 4.4 调度任务

至少实现以下 cron / worker 任务：
- 扫描本地目录；
- 扫描 MinIO 前缀；
- 解析待处理文档；
- 运行 LLM 抽取；
- 生成 T+1 计划；
- 发送审批通知；
- 拉取 T+1 开盘/分钟线/收盘数据；
- 运行计划评估；
- 刷新 symbol registry；
- 计算 scorecard；
- 生成日报/周报；
- 归档 provider 原始响应；
- 清理过期锁和临时文件。

要求：
- 每个任务都要有 `job_run`；
- 同一任务实例必须有分布式锁；
- 支持 dry-run；
- 支持按 document / date 重放。

## 4.5 文档解析服务边界

核心编排在 Go，解析策略如下：
- `pdftotext` CLI：默认 PDF 文本提取器；
- `mutool` CLI：可选 fallback；
- PaddleOCR HTTP sidecar：可选 OCR；
- 表格抽取：v1 可以 best-effort，不要求完美识别，但必须保留原始块与可用结构。

不要因为 OCR sidecar 不可用就让整个系统崩掉：
- 文本型 PDF 正常处理；
- 扫描型 PDF 进入 `FAILED` 或 `NEEDS_OCR` 队列，并写明原因；
- 若启用 OCR，则自动重试。

## 4.6 行情 provider 设计

### 默认必须实现
- `EastmoneyHTTPProvider`
- `SinaHTTPProvider`

### 必须预留接口与 mock
- `AkshareBridgeProvider`
- `BaostockBridgeProvider`
- `MCPMarketProvider`

要求：
- provider 返回统一领域模型；
- 每次调用打 provider 名、延迟、重试次数；
- 支持 Redis / 内存双层缓存；
- quote、daily bars、minute bars、calendar 分别有独立缓存 key；
- provider 原始响应异步归档到 MinIO；
- 数据缺口要落 `data_quality_flag`。

--------------------------------
# 5. 规则引擎要求
--------------------------------

不要让 LLM 决定价格。规则引擎必须是可测试的纯逻辑。

至少实现：
- `OPEN_FOLLOW`
- `OPEN_GAP_FILTER`
- `PULLBACK_TO_RANGE`
- `BREAKOUT_ABOVE_PREV_HIGH`
- `WATCHLIST_ONLY`

规则输入：
- 信号方向
- T 日收盘
- ATR
- N 日波动
- 近 5 日涨跌幅
- 成交额/流动性
- 次日事件风险
- 指数/板块相对强弱
- 风险参数配置

规则输出：
- 入场区间
- 止损
- 止盈区间
- 失效条件
- 风险比例
- 是否仅观察

同时实现：
- 规则版本号；
- 每个规则独立单测；
- 固定输入 -> 固定输出；
- 市场快照持久化。

--------------------------------
# 6. 验收标准
--------------------------------

最终仓库必须满足：
1. `go test ./...` 通过；
2. `docker compose -f deploy/docker-compose.yml up --build` 可启动；
3. API / Worker 能读取 Nacos 配置；
4. 用 `testdata/sample_docs` 中的样例 PDF 或 HTML 能跑通：
   - ingest
   - parse
   - signal extract
   - plan generate
5. 用样例行情 fixture 能跑通一次 T+1 评估；
6. 数据库迁移可执行；
7. README 写清：
   - 启动步骤
   - Nacos 配置说明
   - 示例 API 调用
   - 如何启用 OCR sidecar
   - 如何启用 bridge / MCP provider

--------------------------------
# 7. 代码质量要求
--------------------------------

必须补齐：
- 单元测试
- repository 测试
- rules 测试
- evaluation 测试
- config validate 测试
- 至少一组 API 集成测试
- 样例 fixture

代码要求：
- 明确的 interface
- 避免循环依赖
- 错误包装
- ctx 透传
- slog 结构化日志
- 领域层不依赖 HTTP DTO
- provider 层不依赖数据库实现

--------------------------------
# 8. 实施顺序
--------------------------------

按下面顺序推进，并在每一阶段提交可运行产物：

阶段 1：
- 项目骨架
- Go module
- Dockerfile
- chi API skeleton
- Nacos loader
- config schema/validate
- health endpoints

阶段 2：
- PostgreSQL + migrations
- sqlc
- 基础 repository
- document ingest
- MinIO 存储

阶段 3：
- parser pipeline
- chunking
- parse persistence
- OCR sidecar client

阶段 4：
- LLM extraction
- schema validation
- signal persistence
- symbol registry

阶段 5：
- market provider chain
- Eastmoney/Sina direct providers
- bridge/MCP client interfaces and mocks
- market snapshot persistence

阶段 6：
- deterministic rule engine
- plan generation
- approval flow
- Feishu webhook

阶段 7：
- T+1 market tracking
- evaluation engine
- scorecard
- reports

阶段 8：
- tests
- fixtures
- README
- OpenAPI
- docker compose

--------------------------------
# 9. 关键禁止项
--------------------------------

严禁：
- 用 LLM 直接给出 entry / stop / take profit；
- 把业务配置拆到多个 yaml；
- 把 Python 包作为 Go 主系统的强依赖；
- 将 provider 原始字段直接暴露给业务实体；
- 在没有 minute bars 的情况下默默伪造触发逻辑；
- 把没有明确标的的行业评论强行变成具体股票计划。

--------------------------------
# 10. 现在立即开始
--------------------------------

请直接输出：
1. 文件树；
2. 关键设计决策；
3. 第一个提交批次需要创建的文件；
4. 然后开始逐文件实现。

如果遇到桥接 provider 的外部依赖不可用：
- 保持默认系统仍可通过 direct HTTP provider 运行；
- 同时把 bridge provider 的接口、mock、配置和文档补齐。
