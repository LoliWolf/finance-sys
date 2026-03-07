# AGENTS.md

本文件定义本仓库内协作代理的工作约束。目标是让任何后续自动化代理、脚本代理或协作成员，在这个仓库里沿着同一套工程边界继续开发。

## 项目目标

这是一个面向中国市场研究场景的 Go 1.22 后端系统，负责把专家文档输入转成次日交易候选计划，并完成审批、评估和专家评分。

核心范围：

- 文档摄取与去重
- 文档解析与结构化
- 专家观点抽取
- 标的标准化
- 市场快照归档
- 确定性规则生成计划
- 人工审批
- T+1 评估
- 专家 scorecard 与报告

## 强约束

- 后端主系统必须保持 Go 1.22
- HTTP Router 使用 `chi/v5`
- 日志基础使用 `slog`
- 数据库访问必须使用 `pgx/v5` + `sqlc`
- 不允许引入 GORM
- 业务配置只能来自 Nacos 单个 JSON 文档
- LLM 只允许做抽取，不允许直接生成入场价、止损价、止盈价、仓位
- 交易参数必须由 `internal/rules` 中的确定性规则生成
- 不要把第三方 provider 字段直接泄漏到业务层

## 目录约定

- `cmd/api`: API 入口
- `cmd/worker`: Worker 入口
- `internal/bootstrap`: 启动装配
- `internal/config`: 配置结构、校验、运行时快照
- `internal/nacoscfg`: Nacos 加载、热更新、重载
- `internal/httpapi`: HTTP handler 与中间件
- `internal/domain`: 领域模型
- `internal/repository`: repository 封装
- `internal/repository/sqlc`: 生成代码，禁止手改
- `internal/parser`: 文档解析
- `internal/llm`: 抽取逻辑
- `internal/market`: 行情 provider chain
- `internal/rules`: 规则引擎
- `internal/evaluation`: 次日评估
- `internal/report`: scorecard 和报表
- `internal/approval`: 审批逻辑
- `internal/scheduler`: 定时任务
- `internal/storage`: MinIO 抽象
- `migrations`: 数据库迁移
- `sqlc/query`: SQL 源文件

## 开发规则

- 修改 SQL 后，必须重新执行 `sqlc generate -f sqlc/sqlc.yaml`
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
```

- 如果新增持久化字段，必须同时更新：

```text
migrations/
sqlc/query/
internal/domain/
internal/repository/
```

## 发布规则

- 默认使用 `main` 分支
- 发布 GitHub 前先确认 `README.md`、`AGENTS.md`、`configs/example_nacos_config.json` 已同步
- 不要把私密 token、真实生产 DSN、真实对象存储密钥提交进仓库
- 如果当前环境 `gh auth status` 失败，不要伪造发布成功，应明确提示需要重新登录 GitHub

## 建议工作方式

1. 先读 `README.md` 了解当前实现范围。
2. 再看 `internal/bootstrap/app.go` 理解启动装配。
3. 修改功能时优先保持现有目录边界，不要把逻辑堆到 `main.go`。
4. 行情能力扩展优先走 `internal/market` provider 接口，不要把 HTTP 调用散落到 service 层。
5. 文档解析扩展优先走 `internal/parser`，不要在 handler 中直接解析文件。

## 当前发布阻塞条件

如果要真正发布到 GitHub，当前机器需要满足以下条件：

- `git` 仓库已初始化
- `git config user.name` 和 `git config user.email` 可用
- `gh auth status` 通过，或者已配置有效远端认证
- 远端仓库名称、可见性、组织归属明确
