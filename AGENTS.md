# AGENTS.md

本仓库是面向中国市场研究场景的 Go 1.22 后端系统。系统把可提取文本的专家文档、研报摘录或研究线索转换为可审计的结构化交易候选计划，并记录标的解析、不可追踪目标和推荐事件等过程数据。

## 项目边界

- 主系统使用 Go 1.22。
- HTTP Router 使用 `chi/v5`。
- 日志基础使用 `slog`。
- 数据库访问使用 `gorm.io/gorm` + `gorm.io/driver/mysql`。
- 数据库模型生成使用 `gorm.io/gen`。
- 业务配置来自 Nacos 单个 JSON 文档；`configs/example_nacos_config.json` 只作为本地开发兜底。
- 文档解析层只负责提取纯文本，不承担业务推理。
- LLM 或 Agent 只做结构化抽取、标的解析辅助和工具调用，不直接生成最终交易计划。
- 入场价、止损价、止盈价、仓位等交易参数必须由 `internal/rules` 的确定性规则生成。
- 模型输出必须先做结构化校验，再进入候选计划装配和规则层。
- DAL 只接收和返回 `internal/domain/db_model` 中的数据库模型或基础值；事务编排和领域转换放在 `internal/service`。

## 当前主链路

1. 上传文档。
2. 计算 SHA256 去重并把原始文件写入 MySQL。
3. 提取纯文本并切分 chunk。
4. 通过 LLM 或 Agent 抽取结构化 `PlanIntent`。
5. 通过本地证券主数据、标的解析和候选计划装配器收敛为可追踪交易意图。
6. 由 `internal/rules` 生成确定性的 `CandidatePlan`。
7. 持久化候选计划、推荐事件、标的解析观测和不可追踪目标。
8. 通过 HTTP API 查询结果。

## 后续规划链路

当前系统已落地到推荐事件事实层。后续明确规划是把推荐事件扩展成“博主荐股表现评估系统”，用于追踪博主推荐之后一段时间内的股票走势，并生成统计和可视化报表。

规划链路如下：

```text
RecommendationEvent 推荐事实
-> Tushare / skill 同步证券基础信息、交易日历、日线行情、复权数据
-> 按 T+1 / T+5 / T+10 / T+20 等窗口评估推荐后走势
-> 计算收益率、是否盈利、止盈止损命中、最大浮盈、最大回撤
-> 按博主、标的、方向、时间区间聚合胜率、平均收益率、累计收益率、样本数
-> 生成博主排行榜、标的排行榜、推荐明细、可视化分析报表和运营看板
```

这部分尚未完整实现，但属于项目方向，不应在文档中描述为“不做”。实现时必须保持以下边界：

- Tushare、行情接口和 skill 只提供市场数据，不参与交易参数生成。
- 行情同步、窗口评估、胜率统计和排行榜必须由确定性代码完成。
- 第一阶段优先使用日线、交易日历和前复权数据；不默认引入分钟级实时行情。
- 行情缺失、停牌、接口权限不足、标的无法识别时，记录为不可评估，不得伪造收益率。
- 胜率、收益率和排行榜只统计可评估样本；不可评估样本单独计数。
- 评估和统计主键应优先使用稳定的 `recommendation_events`，不要直接依赖会随重分析变化的 `trade_candidate_plans.plan_id`。
- 后续新增目录优先按职责拆到 `internal/marketdata`、`internal/evaluation`、`internal/stats`；后台任务再考虑 worker / scheduler，不要阻塞 HTTP 请求链路。

## 目录约定

- `cmd/api`: API 入口。
- `cmd/init-tushare-security`: Tushare 证券主数据初始化工具。
- `agent`: Python Agent sidecar。
- `internal/bootstrap`: 启动装配。
- `internal/config`: 配置结构、校验和运行时快照。
- `internal/nacoscfg`: Nacos 加载、热更新和重载。
- `internal/httpapi`: HTTP handler 与中间件。
- `internal/domain`: 领域模型。
- `internal/domain/db_model`: GORM 数据库模型。
- `internal/dal`: 按数据库模型拆分的 DML 封装。
- `internal/parser`: 文档解析和文本清洗。
- `internal/llm`: OpenAI 兼容模型调用、结构化输出校验和重试。
- `internal/agentclient`: Go 主系统调用 Agent sidecar 的客户端。
- `internal/rules`: 确定性规则引擎。
- `internal/service`: 业务编排、事务控制和领域转换。
- `migrations`: 数据库迁移。
- `configs`: Nacos JSON 配置示例。

## 开发规则

- 修改 Go 代码后执行 `gofmt -w`。
- 修改表结构后执行 `go run generate.go` 同步 GORM 模型。
- 修改配置项时同步更新 `internal/config/types.go`、`internal/config/validate.go`、`configs/example_nacos_config.json` 和 `configs/example_nacos_config.annotated.jsonc`。
- 修改持久化字段时同步更新 `migrations/`、`internal/domain/db_model/` 和 `internal/dal/`。
- 修改模型输出结构时同步更新 `internal/domain/`、`internal/llm/`、`internal/rules/`、Agent 契约和相关测试。
- 修改规则生成逻辑时必须保持同样输入得到同样输出，不允许引入随机性或远程依赖。
- 提交前至少执行：

```bash
env GOTOOLCHAIN=local go test ./...
env GOTOOLCHAIN=local go build ./...
```

## 飞书知识库目录

项目详细方案和阶段文档统一维护在飞书知识库：

<https://my.feishu.cn/wiki/DAUVw7Lr6i7L3XkulELcWmoCnLh>

当前目录：

- 01 项目概览
- 02 业务介绍与核心流程
- 03 系统架构与模块边界
- 04 数据模型与 DAL 规范
- 05 LLM、Agent 与标的解析方案
- 06 配置、部署与运行
- 07 API 说明
- 08 测试与质量保障
- 09 方案调研与演进路线
- 10 推荐事件沉淀落地技术方案
- 11 股票日行情同步与 Tushare 多 Token 技术方案
- 12 推荐后动态分析与博主胜率排行榜技术方案
- 13 OpenList 外部文档自动摄取技术方案
- Agent 标的解析层与 CPA 稳定输入技术方案

仓库内只保留必要的入口说明、局部运行说明和代码旁文档；完整 PRD、技术方案、阶段复盘和演进路线以飞书知识库为准。
