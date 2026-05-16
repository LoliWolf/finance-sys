# AGENTS.md

本文件定义本仓库内协作代理的工作约束。目标是让后续自动化代理、脚本代理或协作成员，在这个仓库里沿着同一套工程边界继续开发。

## 项目目标

这是一个面向中国市场研究场景的 Go 1.22 后端系统，负责把可解析出文字的专家文档输入，转换成稳定结构化的 T+1 交易候选计划。

当前系统只保留一条窄链路：

- 文档上传与去重
- 文本提取与清洗
- LLM 结构化抽取交易意图
- 确定性规则生成候选计划
- 结果查询

当前不在范围内：

- 行情 provider chain
- 人工审批
- T+1 评估
- scorecard / report
- worker / scheduler
- Redis 运行时依赖
- MinIO / 对象存储运行时依赖
- HTML / email / OCR / 表格抽取兼容链路

## 强约束

- 后端主系统必须保持 Go 1.22
- HTTP Router 使用 `chi/v5`
- 日志基础使用 `slog`
- 数据库访问使用 `gorm.io/gorm` + `gorm.io/driver/mysql`
- 数据库模型生成使用 `gorm.io/gen`
- 不再使用 `sqlc`，不再维护 `internal/repository/sqlc` 或 `sqlc/query`
- 业务配置只能来自 Nacos 单个 JSON 文档；本地示例配置只作为开发兜底
- 输入格式只支持可稳定提取纯文本的文件：`pdf`、`doc`、`docx`、`txt`、`md`、`csv`
- 解析层只负责把文件转成纯文本，不承担业务推理
- LLM 只允许做结构化抽取，不允许直接生成入场价、止损价、止盈价、仓位
- 交易参数必须由 `internal/rules` 中的确定性规则生成
- 模型输出必须先做结构化校验，再进入规则层
- 如果模型输出不合法，必须按配置重试；重试后仍失败则整个分析失败
- 不要把第三方模型 provider 的原始字段直接泄漏到业务层

## 当前处理链路

标准链路如下：

1. 上传文档
2. 计算 SHA256 去重
3. 将原始文件字节写入 MySQL
4. 解析出纯文本并切 chunk
5. 调用 LLM 抽取 `PlanIntent`
6. 校验并归一化结构化结果
7. 通过确定性规则生成 `CandidatePlan`
8. 持久化候选计划并返回

中间层边界必须保持：

- `internal/parser` 输出 `ParseRun`
- `internal/llm` 输出 `[]domain.PlanIntent`
- `internal/rules` 输出 `domain.CandidatePlan`
- `internal/domain/db_model` 放置由 `gorm.io/gen` 生成或同步的数据库模型
- `internal/dal` 放置数据库 DML 封装
- `internal/dal` 直接提供具体 DML 方法，例如 `QueryByID`、`UpdateStatusByID`
- 事务编排和 `domain` / `db_model` 转换放在 `internal/service`，不下沉到 DAL

不要让模型直接输出最终候选计划，也不要让规则层直接消费原始文本。

## 目录约定

- `cmd/api`: API 入口
- `internal/bootstrap`: 启动装配
- `internal/config`: 配置结构、校验、运行时快照
- `internal/nacoscfg`: Nacos 加载、热更新、重载
- `internal/httpapi`: HTTP handler 与中间件
- `internal/domain`: 领域模型
- `internal/domain/db_model`: GORM 数据库模型
- `internal/dal`: 按数据模型拆分的 DML 单例封装
- `internal/parser`: 文档解析，只做文本提取与清洗
- `internal/llm`: 模型调用、结构化输出校验、重试与归一化
- `internal/rules`: 确定性规则引擎
- `internal/service`: 业务编排
- `internal/telemetry`: 日志等基础设施
- `internal/utils`: 通用工具
- `migrations`: 数据库迁移
- `generate.go`: 读取 Nacos 配置中的 MySQL DSN，并基于数据库结构执行 `gorm.io/gen`

## DAL 规则

- `internal/dal` 中每个文件对应一个主要数据模型，例如 `document.go`、`parse_run.go`
- 每个 DML 类型使用包级单例变量暴露，例如 `dal.Documents`
- 所有 DML 方法必须显式传入 `ctx context.Context` 和 `db *gorm.DB`
- `db` 可以是普通连接，也可以是事务句柄；事务控制由调用方负责
- 涉及查询的方法必须使用通用 `QueryParam`
- 涉及更新的方法必须使用通用 `UpdateParam`
- 对外方法必须按业务语义命名，例如 `QueryByID`、`QueryLatestByDocumentID`、`UpdateStatusByID`
- 联表或复杂查询按业务相关性放在最相关的数据模型文件中
- DAL 只接收和返回 `internal/domain/db_model` 中的数据库模型或基础值
- DAL 不返回 HTTP 响应结构，不调用 LLM，不承载业务推理，不做领域模型转换，不开启事务

## 开发规则

- 修改表结构后，必须执行 `go run generate.go` 重新生成或同步 GORM 模型
- 修改 Go 代码后，必须执行 `gofmt -w`
- 提交前至少执行：

```bash
env GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go test ./...
env GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go build ./...
```

- 如果新增配置项，必须同时更新：

```text
internal/config/types.go
internal/config/validate.go
configs/example_nacos_config.json
configs/example_nacos_config.annotated.jsonc
```

- 如果新增持久化字段，必须同时更新：

```text
migrations/
internal/domain/db_model/
internal/dal/
```

- 如果修改了模型输出结构，必须同时更新：

```text
internal/domain/signal.go
internal/llm/
internal/rules/
相关测试
```

- 如果修改了规则生成逻辑，必须保持“同样输入得到同样输出”的确定性，不允许引入随机性或远程依赖

## 模型调用规则

- `internal/llm` 默认走 OpenAI 兼容接口风格
- 模型请求必须显式要求返回 JSON
- 模型返回内容必须反序列化到领域结构后再使用
- 必须校验：
  - `symbol` 非空
  - `direction` 只能是 `LONG` / `SHORT`
  - `reference_price` 不能为负数
  - `thesis` 非空
  - `confidence` 必须在 `(0,1]`
- 校验失败必须视为模型调用失败，而不是“尽量容错后继续”
- 重试次数只能来自配置 `llm.max_retries`
- 不要在代码里写死模型 endpoint、api key、model name

## 建议工作方式

1. 先读 `README.md` 了解当前实现范围。
2. 再看 `internal/bootstrap/app.go` 理解启动装配。
3. 修改功能时优先保持现有目录边界，不要把逻辑堆到 `main.go`。
4. 文档解析扩展优先走 `internal/parser`，不要在 handler 中直接解析文件。
5. 模型分析能力扩展优先走 `internal/llm`，不要把 prompt、HTTP 调用、JSON 校验散落到 service 层。
6. 交易参数生成扩展优先走 `internal/rules`，不要让模型越权生成价格或仓位。
7. 数据库读写扩展走 `internal/dal`，领域转换和事务编排放在 `internal/service`，不再引入 repository 层。

## 发布规则

- 默认使用 `main` 分支
- 发布 GitHub 前先确认 `README.md`、`AGENTS.md`、`configs/example_nacos_config.json` 已同步
- 不要把私密 token、真实生产 DSN、真实模型 API Key 提交进仓库
- 如果当前环境 `gh auth status` 失败，不要伪造发布成功，应明确提示需要重新登录 GitHub

## 当前发布阻塞条件

如果要真正发布到 GitHub，当前机器需要满足以下条件：

- `git` 仓库已初始化
- `git config user.name` 和 `git config user.email` 可用
- `gh auth status` 通过，或者已配置有效远端认证
- 远端仓库名称、可见性、组织归属明确
