# 专家文档 -> 次日交易计划 -> 次日验证 系统设计（Go 1.22）

## 1. 设计原则

### 1.1 核心后端统一为 Go 1.22
API、Worker、配置加载、规则引擎、审批、评估、报表、持久化都使用 Go 1.22。这样部署面更收敛，内部服务交付也更标准。

### 1.2 配置单一来源
除了 Nacos bootstrap 参数来自环境变量，其他业务配置全部来自 **一个 Nacos JSON 文档**。应用启动和热刷新都基于这份 JSON。

### 1.3 LLM 只做抽取，不做定价
LLM 负责文档分类、观点抽取、风险点提取、报告生成。交易参数必须由规则引擎基于市场快照和风控配置生成。

### 1.4 行情源分层
由于 Go 后端不应强依赖 Python 运行时，因此默认链路采用 **直连免费 HTTP 源**，并预留桥接与 MCP：
- 主用：Eastmoney HTTP
- 兜底：Sina HTTP
- 可选：AKShare bridge
- 可选：BaoStock bridge
- 可选：MCP Market provider

### 1.5 文档解析 Go 主编排 + sidecar
PDF 文本优先通过本地 CLI 提取；OCR 通过 PaddleOCR HTTP sidecar；DOCX/HTML/TXT/EML 由 Go 原生解析。这样既满足 Go 主系统约束，也避免把 OCR 重逻辑塞进主服务。

### 1.6 全链路可审计
原始文档、解析结果、抽取信号、计划生成快照、评估输入、规则版本、配置版本、原始 provider 响应都需要可追溯。

## 2. 总体架构

```text
            +---------------------+
            |      Nacos JSON     |
            +----------+----------+
                       |
                config loader / watcher
                       |
        +--------------+---------------+
        |                              |
+-------v--------+             +-------v--------+
|   API Service  |             |  Worker/Cron   |
|   Go 1.22      |             |   Go 1.22      |
+-------+--------+             +-------+--------+
        |                              |
        +----------+-------------------+
                   |
             application services
                   |
   +---------------+------------------------------+
   |               |              |               |
   v               v              v               v
document       llm extract    market data      rules/eval
parser         & validation   provider chain   & reports
   |                               |
   |                               +-- Eastmoney HTTP
   |                               +-- Sina HTTP
   |                               +-- Akshare bridge (optional)
   |                               +-- Baostock bridge (optional)
   |                               +-- MCP market (optional)
   |
   +-- pdftotext CLI
   +-- mutool CLI (optional)
   +-- PaddleOCR sidecar (optional)
   +-- native Go parsers for docx/html/txt/eml

                   |
            PostgreSQL / Redis / MinIO
```

## 3. 处理链路

### 3.1 T 日晚间
1. 文档从本地目录、MinIO 或 API 进入系统。
2. 计算 SHA256 去重。
3. 文档元信息入库并上传对象存储。
4. 解析正文、页文本、表格块、chunks。
5. 调 LLM 做文档分类和观点抽取。
6. 标的标准化，生成 symbol mapping。
7. 拉 T 日市场快照。
8. 规则引擎生成 T+1 候选计划。
9. 发送审批通知或自动审批。

### 3.2 T+1
1. 抓开盘和分钟线。
2. 判断计划是否失效。
3. 判断是否触发 entry。
4. 判断先到止盈还是止损。
5. 记录 MFE / MAE / 收盘收益 / 超额收益。
6. 更新专家 scorecard。
7. 生成日报/周报。

## 4. 服务边界

### API Service
负责：
- 上传文档
- 扫描入口触发
- 查询 documents/signals/plans/evaluations
- 审批计划
- 配置重载
- 健康检查与 metrics

### Worker Service
负责：
- cron 调度
- 批处理状态机推进
- 解析任务
- LLM 抽取任务
- 计划生成
- 行情抓取
- 评估
- 报表计算

## 5. 目录设计

```text
cmd/api
cmd/worker
internal/bootstrap
internal/config
internal/nacoscfg
internal/httpapi
internal/domain
internal/repository
internal/service
internal/parser
internal/llm
internal/market
internal/rules
internal/evaluation
internal/approval
internal/report
internal/scheduler
internal/storage
internal/telemetry
migrations
sqlc
deploy
```

## 6. 文档解析策略

### PDF
- 主路径：`pdftotext` CLI
- 备选：`mutool` CLI
- OCR：PaddleOCR HTTP sidecar
- 表格：best-effort，保存结构化 rows 和原始块坐标

### 其他格式
- DOCX：解析 OOXML 文本块
- HTML：清理脚本/样式，提取正文
- TXT/MD：直接读取
- EML：解析邮件头、正文和附件元信息

### 失败处理
- 文本密度过低 -> `NEEDS_OCR`
- OCR 不可用 -> `FAILED`，并落 dead letter
- 解析失败要有 parse_run 和错误原因

## 7. 行情层设计

### 直接 HTTP provider
适合作为 Go 后端的默认运行方案，不依赖 Python 包：
- Eastmoney：主行情链路
- Sina：兜底链路

### Bridge / MCP
用于以后增强或内网统一接入：
- AKShare bridge：通过 HTTP 或 RPC 暴露 Python 行情能力
- BaoStock bridge：通过 HTTP 或 RPC 暴露 Python 历史行情能力
- MCP provider：对接企业内部统一市场数据 MCP

### 统一约束
- 统一领域模型
- provider chain
- 熔断与重试
- 双层缓存
- 原始响应归档
- 数据质量标记

## 8. 规则与评估

### 计划生成
支持：
- OPEN_FOLLOW
- OPEN_GAP_FILTER
- PULLBACK_TO_RANGE
- BREAKOUT_ABOVE_PREV_HIGH
- WATCHLIST_ONLY

### 评估输出
至少包括：
- INVALIDATED
- NOT_TRIGGERED
- OPEN
- SUCCESS
- WEAK_SUCCESS
- FAIL
- DATA_INSUFFICIENT

并记录：
- entry_price
- exit_price
- close_price
- pnl_pct
- mfe_pct
- mae_pct
- benchmark_return_pct
- excess_return_pct
- evaluation_reason
- data_quality_flag

## 9. 配置加载策略

1. 启动时从环境变量读取 Nacos bootstrap 参数。
2. 拉取单个 JSON 配置。
3. 校验结构和业务合法性。
4. 将生效配置放入原子快照。
5. Worker 和 API 从只读快照读取配置。
6. 周期轮询或监听 Nacos 变更。
7. 刷新成功时写 `config_snapshots` 表。

## 10. 关于 MCP 与 skills

- **MCP**：建议作为未来内网统一接入层来预留，运行时通过 `MCPMarketProvider` 接入。
- **skills**：不要把 skills 设计成生产依赖。开发时可以辅助生成代码或分析问题，但部署后的系统必须仅依赖 HTTP / CLI / MCP / 数据库 / Redis / MinIO / Nacos 等常规组件。

## 11. 本地启动建议

最小开发环境：
- PostgreSQL
- Redis
- MinIO
- API
- Worker

可选增强：
- Nacos
- PaddleOCR sidecar
- Akshare bridge
- Baostock bridge
- MCP mock server
