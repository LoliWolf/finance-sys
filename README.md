# 专家文档到次日交易计划工作流

本项目是一个基于 Go 1.22 的研究工作流后端，用于把专家日报、研报、邮件、HTML、TXT、DOCX、PDF 等输入，转成 T+1 交易候选计划，并在次日完成价格跟踪、结果评估和专家评分统计。

系统定位是研究、建议、跟踪、评估平台，不接券商实盘，不自动下单。

## 已实现能力

- 文档上传、对象存储归档、SHA256 去重
- TXT、HTML、EML、DOCX 原生解析
- PDF 通过 `pdftotext` CLI 提取文本，并保留 OCR 兜底状态
- 结构化 parse run、chunks、sections、tables 持久化
- 抽取型信号识别，生成结构化专家观点
- 行情 provider chain：Eastmoney 主链路、Sina 兜底、桥接 provider 预留
- 确定性规则引擎生成交易计划
- 计划审批、次日评估、专家 scorecard 汇总
- Nacos 单 JSON 配置加载、热更新、last-good-cache、配置快照入库
- PostgreSQL + `pgx/v5` + `sqlc` 类型安全查询
- Redis、MinIO、Cron 调度、Docker 本地环境

## 目录结构

```text
.
├── cmd
│   ├── api
│   └── worker
├── configs
│   └── example_nacos_config.json
├── deploy
│   ├── Dockerfile.api
│   ├── Dockerfile.worker
│   ├── docker-compose.yml
│   └── init
├── internal
│   ├── approval
│   ├── bootstrap
│   ├── config
│   ├── domain
│   ├── evaluation
│   ├── httpapi
│   ├── llm
│   ├── market
│   ├── nacoscfg
│   ├── parser
│   ├── report
│   ├── repository
│   ├── rules
│   ├── scheduler
│   ├── service
│   ├── storage
│   ├── telemetry
│   └── utils
├── migrations
├── sqlc
│   ├── query
│   └── sqlc.yaml
├── testdata
│   └── sample_docs
├── architecture_overview_go122.md
├── bootstrap_go122.env.example
├── codex_master_prompt_go122.md
├── go.mod
├── go.sum
├── nacos_config_go122.example.json
├── AGENTS.md
└── README.md
```

## 技术决策

- 后端主系统统一为 Go 1.22
- HTTP 路由使用 `chi/v5`
- 日志使用 `slog`，输出 JSON
- 数据库访问使用 `pgx/v5` + `sqlc`
- 迁移目录使用 `migrations/`，兼容 `golang-migrate`
- Redis 使用 `redis/go-redis/v9`
- MinIO 使用 `minio-go/v7`
- Nacos 使用 `nacos-sdk-go/v2`
- 定时调度使用 `robfig/cron/v3`
- 外部 HTTP 调用统一封装在市场数据 provider 中
- 交易参数由规则引擎生成，LLM 只负责抽取，不负责定价

## 核心流程

1. 文档进入系统并计算 SHA256 去重。
2. 原始文件写入 MinIO，元信息写入 PostgreSQL。
3. Parser 解析正文、sections、chunks、tables。
4. 抽取模块生成结构化专家信号。
5. 市场数据 provider 拉取 T 日行情快照。
6. 规则引擎生成 T+1 候选计划。
7. 计划进入审批。
8. T+1 拉取行情并执行评估。
9. 输出计划结果与专家 scorecard。

## 当前接口

- `GET /healthz`
- `GET /metrics`
- `GET /api/v1/documents`
- `POST /api/v1/documents/upload`
- `POST /api/v1/documents/{id}/process`
- `POST /api/v1/jobs/process-documents`
- `GET /api/v1/plans`
- `POST /api/v1/plans/{id}/approve`
- `GET /api/v1/evaluations`
- `POST /api/v1/jobs/evaluate`
- `GET /api/v1/reports/scorecards`
- `POST /api/v1/admin/config/reload`

## 配置说明

业务配置只允许来自 Nacos 单个 JSON 文档。

启动时允许从环境变量读取的只有：

- `NACOS_SERVER_ADDR`
- `NACOS_NAMESPACE`
- `NACOS_GROUP`
- `NACOS_DATA_ID`
- `NACOS_USERNAME`
- `NACOS_PASSWORD`

如果本地没有可用 Nacos，服务会回退到 `configs/example_nacos_config.json`。

## 本地启动

依赖：

- PostgreSQL
- Redis
- MinIO
- 可选 Nacos

直接运行：

```bash
go run ./cmd/api
go run ./cmd/worker
```

使用 Docker Compose：

```bash
docker compose -f deploy/docker-compose.yml up --build
```

## 常用命令

运行测试：

```bash
env GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go test ./...
```

编译：

```bash
env GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go build ./...
```

生成 `sqlc` 代码：

```bash
sqlc generate -f sqlc/sqlc.yaml
```

执行数据库迁移：

```bash
migrate -path migrations -database "postgres://expert_trade:change_me@localhost:5432/expert_trade?sslmode=disable" up
```

## 测试状态

当前仓库已通过：

```bash
env GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go test ./...
env GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go build ./...
```

## 已知边界

- OCR sidecar 目前是接口和状态位预留，未完整接入 PaddleOCR HTTP 调用
- Akshare、Baostock、MCP provider 目前为桥接骨架与占位实现
- 分钟级行情评估在 provider 不支持时会回退到日线逻辑
- 目前没有前端界面，主要通过 API 和 worker 驱动
