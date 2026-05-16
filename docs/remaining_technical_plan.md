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

后续扩展只在该基线上演进：LLM 仍只做结构化抽取，行情和收益计算必须走确定性代码。

## 模块设计

继续使用：

- `internal/domain`：业务领域模型。
- `internal/domain/db_model`：由 `gorm.io/gen` 生成或同步的数据库模型。
- `internal/dal`：按模型拆分的 DML 单例封装，只接收和返回 `db_model` 或基础值。
- `internal/service`：业务编排、事务控制、领域模型与数据库模型转换。
- `migrations`：维护表结构。
- `internal/config`：维护配置结构与校验。
- `internal/httpapi`：暴露管理与查询 API。

后续新增目录建议：

- `internal/marketdata`：Tushare client、请求签名、限速、响应解析。
- `internal/evaluation`：推荐事件评估、收益率计算、窗口计算。
- `internal/stats`：聚合统计查询与指标计算。

## 数据库工程实践

- 修改表结构后先更新 `migrations/`。
- 运行 `go run generate.go`，从 Nacos 配置读取 MySQL DSN，并基于实际数据库结构执行 `gorm.io/gen`。
- 生成或同步 `internal/domain/db_model` 后，再补充 `internal/dal` 中对应模型的 DML。
- 所有 DML 方法必须显式接收 `ctx context.Context` 和 `db *gorm.DB`。
- 查询方法统一使用 `dal.QueryParam`；更新方法统一使用 `dal.UpdateParam`。
- 事务由 service 控制；DAL 方法接收普通连接或事务型 `*gorm.DB`，但不自行开启事务。
- `domain` / `db_model` 转换放在 service，不放在 DAL。
- 联表查询按业务相关性放在最相关的 DAL 文件中，例如推荐事件与评估聚合优先放到推荐相关 DAL。

## 配置扩展

新增配置时同步更新：

```text
internal/config/types.go
internal/config/validate.go
configs/example_nacos_config.json
configs/example_nacos_config.annotated.jsonc
```

Token 不应提交真实值，生产环境继续由 Nacos 配置。

## 实现顺序

### 阶段 1：表结构与推荐事件

1. 新增 migrations。
2. 运行 `go run generate.go` 生成或同步 `internal/domain/db_model`。
3. 在 `internal/dal` 增加推荐事件 DML。
4. 在 service 中从候选计划派生推荐事件。
5. 增加单元测试。

### 阶段 2：Tushare Client

1. 增加配置。
2. 实现 client 和响应解析。
3. 实现股票基础信息、交易日历、日线同步。
4. 增加 `httptest` 覆盖异常响应和字段缺失。

### 阶段 3：评估引擎

1. 实现交易窗口计算。
2. 实现收益率、最大浮盈、最大回撤。
3. 写入评估结果。
4. 增加固定行情样例测试，确保确定性。

### 阶段 4：统计 API

1. 增加聚合查询 DAL。
2. 增加 stats service。
3. 暴露查询 API。
4. 增加集成级 DAL 测试或 SQL 回归样例。

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
