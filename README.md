# finance-sys

`finance-sys` 是一个面向中国市场研究场景的 Go 后端系统。它把可提取文本的专家文档、研报摘录或研究线索转换成可审计、可追踪、由确定性规则生成的 T+1 交易候选计划。

系统的核心原则是：LLM 和 Agent 只负责结构化抽取、标的解析辅助和工具调用；真正的交易参数由 Go 主系统内的规则层生成。

## 当前定位

当前系统聚焦研究文档到候选计划的主链路：

1. 上传 `pdf`、`doc`、`docx`、`txt`、`md`、`csv` 等可提取文本的文件。
2. 计算 SHA256 去重，并把原始文件字节写入 MySQL。
3. 提取纯文本，必要时对 PDF 走 OCR 兜底。
4. 通过 LLM 或 Agent sidecar 抽取结构化交易意图。
5. 使用本地证券主数据做标的解析和复核，过滤板块、主题、歧义标的和不可追踪目标。
6. 由候选计划装配器生成稳定的规则输入。
7. 由 `internal/rules` 确定性生成入场价、止损价、止盈价和仓位。
8. 持久化候选计划、推荐事件、标的解析观测和不可追踪目标，供 API 查询。

当前已经落地到推荐事件事实层；行情同步、推荐后走势评估、统计排行榜和可视化报表属于后续规划链路，尚未作为当前 HTTP 主链路实现。

## 后续规划链路

后续目标不是只生成候选计划，而是形成“博主推荐表现评估系统”：

```text
RecommendationEvent 推荐事实
-> Tushare / skill 同步证券基础信息、交易日历、日线行情和复权数据
-> 按 T+1 / T+5 / T+10 / T+20 等窗口评估推荐后走势
-> 计算收益率、胜负、止盈止损命中、最大浮盈、最大回撤
-> 按博主、标的、方向、时间区间聚合胜率和收益率
-> 输出博主排行榜、标的表现榜、推荐明细、可视化分析报表和运营看板
```

这部分的工程边界：

- 行情和收益计算必须使用确定性代码，不能由 LLM 生成。
- Tushare token 只能来自 Nacos 或显式安全参数，不能提交到仓库。
- 第一阶段只使用日线和交易日历，不做分钟级实时交易。
- 行情缺失、停牌、权限不足、代码无法识别时必须记录为不可评估状态，不混入胜率分母。
- 评估结果和统计结果应挂在稳定的 `recommendation_events` 上，而不是易变的 `trade_candidate_plans.plan_id`。

建议后续模块：

- `internal/marketdata`: Tushare client、交易日历、日线行情、复权数据同步。
- `internal/evaluation`: 推荐事件窗口评估、收益率、最大浮盈和最大回撤计算。
- `internal/stats`: 博主、标的、方向、窗口维度的聚合统计和排行榜查询。
- worker / scheduler: 后台同步行情、刷新评估、生成报表和清理观测数据。
- dashboard / report: 可视化分析报表、博主胜率榜、收益榜、样本分布和异常数据视图。

## 技术栈

- Go 1.22
- `chi/v5`
- `slog`
- MySQL
- `gorm.io/gorm` + `gorm.io/driver/mysql`
- `gorm.io/gen`
- Nacos 单 JSON 配置
- OpenAI 兼容 LLM 接口
- 可选 Python Agent sidecar

## 关键目录

- `cmd/api`: HTTP API 服务入口。
- `cmd/init-tushare-security`: Tushare 证券主数据初始化工具。
- `agent`: Python Agent sidecar 和标的解析技能。
- `internal/bootstrap`: 启动装配。
- `internal/config`: 配置结构、校验和运行时快照。
- `internal/nacoscfg`: Nacos 配置加载、热更新和重载。
- `internal/httpapi`: HTTP handler、中间件和上传页。
- `internal/parser`: 文档解析、文本清洗和 chunk 切分。
- `internal/llm`: OpenAI 兼容模型调用、JSON 输出校验和重试。
- `internal/agentclient`: Go 主系统调用 Agent sidecar 的客户端。
- `internal/service`: 业务编排、事务控制、候选计划装配和观测记录。
- `internal/rules`: 确定性交易参数生成规则。
- `internal/dal`: GORM DML 封装。
- `internal/domain`: 领域模型。
- `internal/domain/db_model`: `gorm.io/gen` 生成或同步的数据库模型。
- `migrations`: 数据库迁移。
- `configs`: 本地示例 Nacos 配置。

## HTTP API

默认 API 前缀为 `/api/v1`，可通过 Nacos 配置调整。

- `GET /healthz`
- `GET /`
- `GET /upload`
- `GET /api/v1/documents`
- `POST /api/v1/documents/upload`
- `POST /api/v1/documents/{id}/analyze`
- `GET /api/v1/documents/{id}/plans`
- `GET /api/v1/documents/{id}/recommendations`
- `GET /api/v1/documents/{id}/resolution-runs`
- `GET /api/v1/documents/{id}/untrackable-targets`
- `GET /api/v1/resolution-runs/{id}`
- `GET /api/v1/plans`
- `GET /api/v1/recommendations`
- `GET /api/v1/recommendations/{id}`
- `GET /api/v1/admin/security/lookup`
- `POST /api/v1/internal/security/resolve`
- `POST /api/v1/internal/security/verify`
- `POST /api/v1/admin/config/reload`

## 配置

生产和正式联调使用 Nacos 中的单个 JSON 配置文档。API 启动和 `generate.go` 使用同一套配置加载路径：

1. 优先读取 Nacos bootstrap 环境变量并加载 Nacos 配置。
2. 本地开发未配置 Nacos 时，回退到 `configs/example_nacos_config.json`。

不要把真实生产 DSN、模型 API Key、Tushare token 或其他私密凭据提交到仓库。

## 生成数据库模型

修改表结构后执行：

```bash
go run generate.go
```

该命令会按启动路径读取配置中的 MySQL DSN，并基于当前数据库结构同步 `internal/domain/db_model`。

## 运行

```bash
go run ./cmd/api
```

如需初始化 Tushare 证券主数据：

```bash
go run ./cmd/init-tushare-security
```

## 验证

```bash
env GOTOOLCHAIN=local go test ./...
env GOTOOLCHAIN=local go build ./...
```

部分集成测试会写入配置指向的 MySQL，默认通过环境变量门禁跳过。运行前先阅读对应测试文件中的说明。

## 项目文档

完整 PRD、技术方案、阶段复盘和演进路线统一维护在飞书知识库：

<https://my.feishu.cn/wiki/DAUVw7Lr6i7L3XkulELcWmoCnLh>

仓库内只保留必要入口说明、子模块运行说明和代码旁文档。
