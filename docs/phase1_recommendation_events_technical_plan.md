# 阶段一：表结构与推荐事件详细技术方案

## 1. 阶段目标

阶段一只解决“候选计划如何沉淀为可长期跟踪的推荐事件”。本阶段不接入 Tushare，不计算收益率，不做排行榜统计，也不恢复 worker/scheduler。

交付结果：

1. 从现有 `trade_candidate_plans` 派生稳定的 `recommendation_events`。
2. 建立博主归一化表，避免同一来源以多个字符串重复统计。
3. 将同一事件下的多段证据拆成可查询的 `recommendation_event_evidences`。
4. 在 `internal/dal`、`internal/service`、`internal/httpapi` 中补齐创建和查询链路。
5. 重复分析同一文档时，推荐事件保持幂等，不产生重复主事件。

阶段一完成后，主链路变为：

```text
上传文档
-> SHA256 去重
-> 保存原始文件字节
-> parser 输出 ParseRun
-> llm 输出 []PlanIntent
-> rules 输出 []CandidatePlan
-> 保存 trade_candidate_plans
-> 从 CandidatePlan 派生 RecommendationEvent
-> 查询推荐事件
```

## 2. 设计边界

保持当前仓库边界：

- `internal/domain`：新增推荐事件领域结构。
- `internal/domain/db_model`：由 `gorm.io/gen` 生成或同步数据库模型。
- `internal/dal`：新增 `blogger.go`、`recommendation_event.go`、`recommendation_event_evidence.go`。
- `internal/service`：在 `DocumentService` 内完成候选计划到推荐事件的派生和事务编排；后续如逻辑变重，再拆 `RecommendationService`。
- `internal/httpapi`：新增推荐事件查询 handler。
- `migrations`：增加新表 DDL。

不允许在本阶段做的事：

- 不让 LLM 直接输出推荐事件。
- 不让规则层消费数据库模型。
- 不把推荐事件创建逻辑下沉到 DAL。
- 不在 DAL 中开启事务。
- 不引入行情 provider、收益评估、异步任务、Redis 或对象存储。

## 3. 领域模型

新增文件建议：`internal/domain/recommendation.go`。

推荐事件是事实层对象，表达“某来源在某日期推荐某股票及方向”。它不是交易计划，也不持有入场、止损、止盈、仓位等执行参数。

建议结构：

```go
type Blogger struct {
    ID            int64     `json:"id"`
    Name          string    `json:"name"`
    NormalizedName string   `json:"normalized_name"`
    Institution   string    `json:"institution"`
    SourceType    string    `json:"source_type"`
    CreatedAt     time.Time `json:"created_at"`
    UpdatedAt     time.Time `json:"updated_at"`
}

type RecommendationEvent struct {
    ID              int64          `json:"id"`
    BloggerID       int64          `json:"blogger_id"`
    BloggerName     string         `json:"blogger_name"`
    SourceDocumentID int64         `json:"source_document_id"`
    PlanID          int64          `json:"plan_id"`
    ParseRunID      int64          `json:"parse_run_id"`
    Symbol          string         `json:"symbol"`
    AssetType       string         `json:"asset_type"`
    Market          string         `json:"market"`
    Direction       string         `json:"direction"`
    RecommendDate   time.Time      `json:"recommend_date"`
    ReferencePrice  float64        `json:"reference_price"`
    Confidence      float64        `json:"confidence"`
    Status          string         `json:"status"`
    Thesis          string         `json:"thesis"`
    Evidence        []EvidenceSpan `json:"evidence"`
    DedupeKey       string         `json:"dedupe_key"`
    ConfigVersion   int64          `json:"config_version"`
    RuleVersion     string         `json:"rule_version"`
    CreatedAt       time.Time      `json:"created_at"`
    UpdatedAt       time.Time      `json:"updated_at"`
}
```

字段映射口径：

| 推荐事件字段 | 来源 |
| --- | --- |
| `blogger_name` | 优先 `CandidatePlan.Analyst`，为空则用 `Document.Author`，仍为空则用配置默认 author |
| `source_document_id` | `CandidatePlan.DocumentID` |
| `plan_id` | 已保存后的 `CandidatePlan.ID` |
| `parse_run_id` | `CandidatePlan.ParseRunID` |
| `symbol` | `CandidatePlan.Symbol` |
| `asset_type` | `CandidatePlan.AssetType` |
| `market` | `CandidatePlan.Market` |
| `direction` | `CandidatePlan.Direction` |
| `recommend_date` | `CandidatePlan.TradeDate` |
| `reference_price` | `CandidatePlan.ReferencePrice` |
| `confidence` | `CandidatePlan.Confidence` |
| `thesis` | `CandidatePlan.Thesis` |
| `evidence` | `CandidatePlan.Evidence` |
| `config_version` | `CandidatePlan.ConfigVersion` |
| `rule_version` | `CandidatePlan.RuleVersion` |

## 4. 状态设计

`recommendation_events.status` 使用字符串，阶段一只需要以下值：

- `ACTIVE`：已从候选计划成功派生，后续可进入行情评估。
- `NEEDS_REVIEW`：候选计划本身需要人工确认，例如缺价格或低置信度。
- `SUPERSEDED`：同一文档重复分析后，旧 plan 对应事件被新 plan 替代，但业务上仍保留审计记录。

阶段一默认策略：

1. `CandidatePlan.Status == "READY"` 时，事件状态为 `ACTIVE`。
2. 其他候选计划状态映射为 `NEEDS_REVIEW`。
3. 对同一文档重新分析时，不物理删除已存在的推荐事件；如果 dedupe key 命中新事件，则更新事件字段和 `plan_id`。如果旧事件不再由本次候选计划产生，可先保持原状态，第二阶段再决定是否标记 `SUPERSEDED`。

## 5. 新表设计

### 5.1 bloggers

用途：归一化推荐来源，支持后续按博主统计。

```sql
CREATE TABLE `bloggers` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `name` varchar(128) COLLATE utf8mb4_general_ci NOT NULL,
  `normalized_name` varchar(128) COLLATE utf8mb4_general_ci NOT NULL,
  `institution` varchar(128) COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `source_type` varchar(32) COLLATE utf8mb4_general_ci NOT NULL DEFAULT 'DOCUMENT',
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_bloggers_normalized_institution` (`normalized_name`, `institution`),
  KEY `idx_bloggers_name` (`name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
```

字段说明：

- `name`：展示名，保留原始较完整名称。
- `normalized_name`：归一化名称，用于唯一约束。规则为 `strings.TrimSpace` 后转小写；中文不做拼音转换。
- `institution`：机构或来源平台，优先来自候选计划，候选计划为空时用文档机构。
- `source_type`：阶段一固定为 `DOCUMENT`，后续可扩展为 `MANUAL`、`API`。

### 5.2 recommendation_events

用途：主推荐事件表，一条记录对应一次可统计的推荐事实。

```sql
CREATE TABLE `recommendation_events` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `blogger_id` bigint NOT NULL,
  `source_document_id` bigint NOT NULL,
  `plan_id` bigint NOT NULL,
  `parse_run_id` bigint NOT NULL,
  `symbol` varchar(32) COLLATE utf8mb4_general_ci NOT NULL,
  `asset_type` varchar(32) COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `market` varchar(32) COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `direction` varchar(16) COLLATE utf8mb4_general_ci NOT NULL,
  `recommend_date` date NOT NULL,
  `reference_price` double NOT NULL DEFAULT '0',
  `confidence` double NOT NULL DEFAULT '0',
  `status` varchar(64) COLLATE utf8mb4_general_ci NOT NULL,
  `thesis` text COLLATE utf8mb4_general_ci NOT NULL,
  `dedupe_key` varchar(128) COLLATE utf8mb4_general_ci NOT NULL,
  `config_version` bigint NOT NULL,
  `rule_version` varchar(64) COLLATE utf8mb4_general_ci NOT NULL,
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_recommendation_events_dedupe_key` (`dedupe_key`),
  KEY `idx_recommendation_events_blogger_date` (`blogger_id`, `recommend_date`),
  KEY `idx_recommendation_events_symbol_date` (`symbol`, `recommend_date`),
  KEY `idx_recommendation_events_document` (`source_document_id`, `created_at`),
  KEY `idx_recommendation_events_plan` (`plan_id`),
  CONSTRAINT `fk_recommendation_events_blogger` FOREIGN KEY (`blogger_id`) REFERENCES `bloggers` (`id`),
  CONSTRAINT `fk_recommendation_events_document` FOREIGN KEY (`source_document_id`) REFERENCES `documents` (`id`) ON DELETE CASCADE,
  CONSTRAINT `fk_recommendation_events_plan` FOREIGN KEY (`plan_id`) REFERENCES `trade_candidate_plans` (`id`) ON DELETE CASCADE,
  CONSTRAINT `fk_recommendation_events_parse_run` FOREIGN KEY (`parse_run_id`) REFERENCES `parse_runs` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
```

`dedupe_key` 由 service 生成，不交给数据库拼接。推荐格式：

```text
sha256(normalized_blogger_name + "|" + institution + "|" + source_document_id + "|" + symbol + "|" + direction + "|" + recommend_date)
```

这样满足 PRD 中“同一博主、同一股票、同一方向、同一交易日来自同一文档只保留一条主事件”的要求。

### 5.3 recommendation_event_evidences

用途：保留推荐事件的证据片段，支持后续详情页展示和人工校对。

```sql
CREATE TABLE `recommendation_event_evidences` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `recommendation_event_id` bigint NOT NULL,
  `source_document_id` bigint NOT NULL,
  `plan_id` bigint NOT NULL,
  `chunk_index` int NOT NULL DEFAULT '0',
  `evidence_text` text COLLATE utf8mb4_general_ci NOT NULL,
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_recommendation_event_evidences_event` (`recommendation_event_id`, `id`),
  KEY `idx_recommendation_event_evidences_document` (`source_document_id`),
  CONSTRAINT `fk_recommendation_event_evidences_event` FOREIGN KEY (`recommendation_event_id`) REFERENCES `recommendation_events` (`id`) ON DELETE CASCADE,
  CONSTRAINT `fk_recommendation_event_evidences_document` FOREIGN KEY (`source_document_id`) REFERENCES `documents` (`id`) ON DELETE CASCADE,
  CONSTRAINT `fk_recommendation_event_evidences_plan` FOREIGN KEY (`plan_id`) REFERENCES `trade_candidate_plans` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
```

证据不放 JSON 的原因：

- 主事件查询不需要加载长文本证据。
- 详情页可以按事件 ID 延迟加载证据。
- 后续人工校对时可以对单条证据做状态或标注扩展。

## 6. Migration 实施细则

当前仓库只有 `migrations/DDL.sql`。阶段一建议新增独立迁移文件：

```text
migrations/002_recommendation_events.sql
```

执行顺序：

1. 先创建 `bloggers`。
2. 再创建 `recommendation_events`。
3. 最后创建 `recommendation_event_evidences`。
4. 在本地 MySQL 执行迁移。
5. 执行 `go run generate.go` 重新同步 `internal/domain/db_model`。

生成后应新增或更新：

```text
internal/domain/db_model/blogger.gen.go
internal/domain/db_model/recommendation_event.gen.go
internal/domain/db_model/recommendation_event_evidence.gen.go
```

如果 `gorm.io/gen` 对表名生成的结构体名称不符合预期，以生成结果为准，不手写覆盖 generated 文件。

## 7. DAL 设计

所有 DML 方法继续显式传入 `ctx context.Context` 和 `db *gorm.DB`，并使用 `QueryParam` / `UpdateParam`。

### 7.1 internal/dal/blogger.go

包级单例：

```go
var Bloggers = &BloggerDML{}
```

建议方法：

```go
func (*BloggerDML) Create(ctx context.Context, db *gorm.DB, model *db_model.Blogger) error
func (*BloggerDML) QueryByID(ctx context.Context, db *gorm.DB, id int64) (*db_model.Blogger, error)
func (*BloggerDML) QueryByNormalizedNameAndInstitution(ctx context.Context, db *gorm.DB, normalizedName string, institution string) (*db_model.Blogger, error)
func (*BloggerDML) UpsertByNormalizedNameAndInstitution(ctx context.Context, db *gorm.DB, model *db_model.Blogger) error
```

`UpsertByNormalizedNameAndInstitution` 可以使用 GORM 的 `clause.OnConflict`，冲突列为 `normalized_name, institution`。冲突时只更新：

- `name`
- `source_type`
- `updated_at`

如果担心覆盖更好的展示名，service 里应选择“较长且非空的名称”作为 `name`。

### 7.2 internal/dal/recommendation_event.go

包级单例：

```go
var RecommendationEvents = &RecommendationEventDML{}
```

建议方法：

```go
func (*RecommendationEventDML) Create(ctx context.Context, db *gorm.DB, model *db_model.RecommendationEvent) error
func (*RecommendationEventDML) UpsertByDedupeKey(ctx context.Context, db *gorm.DB, model *db_model.RecommendationEvent) error
func (*RecommendationEventDML) QueryByID(ctx context.Context, db *gorm.DB, id int64) (*db_model.RecommendationEvent, error)
func (*RecommendationEventDML) QueryByDocumentID(ctx context.Context, db *gorm.DB, documentID int64) ([]db_model.RecommendationEvent, error)
func (*RecommendationEventDML) QueryLatest(ctx context.Context, db *gorm.DB, limit int) ([]db_model.RecommendationEvent, error)
func (*RecommendationEventDML) QueryByParam(ctx context.Context, db *gorm.DB, param QueryParam) ([]db_model.RecommendationEvent, error)
```

`UpsertByDedupeKey` 冲突时更新：

- `blogger_id`
- `plan_id`
- `parse_run_id`
- `reference_price`
- `confidence`
- `status`
- `thesis`
- `config_version`
- `rule_version`
- `updated_at`

不要更新：

- `source_document_id`
- `symbol`
- `direction`
- `recommend_date`
- `dedupe_key`

这些字段属于事件身份，冲突时不应被改写。

### 7.3 internal/dal/recommendation_event_evidence.go

包级单例：

```go
var RecommendationEventEvidences = &RecommendationEventEvidenceDML{}
```

建议方法：

```go
func (*RecommendationEventEvidenceDML) CreateBatch(ctx context.Context, db *gorm.DB, models []db_model.RecommendationEventEvidence) error
func (*RecommendationEventEvidenceDML) DeleteByEventID(ctx context.Context, db *gorm.DB, eventID int64) error
func (*RecommendationEventEvidenceDML) QueryByEventID(ctx context.Context, db *gorm.DB, eventID int64) ([]db_model.RecommendationEventEvidence, error)
```

证据写入策略：

1. 主事件 upsert 完成后拿到事件 ID。
2. 删除该事件原有证据。
3. 按当前 plan 的 evidence 全量重建证据。

这样可以保证重复分析同一文档时证据与最新候选计划一致。

## 8. Service 链路设计

阶段一可以继续放在 `DocumentService` 中，因为推荐事件是文档分析结果的派生物，事务边界和候选计划保存强相关。

### 8.1 修改 AnalyzeDocument

当前 `AnalyzeDocument` 流程在 `replacePlansByDocumentID` 后更新文档状态为 `PLANNED`。阶段一改为：

```text
生成 CandidatePlan
-> replacePlansByDocumentID 保存候选计划
-> deriveRecommendationEventsFromPlans 保存推荐事件
-> 更新 document.status = PLANNED
```

关键点：

- 推荐事件必须基于“已保存后的 plans”，因为需要 `plan_id`。
- 候选计划和推荐事件最好在同一个事务内完成。可以将 `replacePlansByDocumentID` 改造成 `replacePlansAndEventsByDocumentID`，在一个 transaction 中先删旧 plans、插新 plans、再派生事件。
- 如果保持两个事务，可能出现 plans 成功但 events 失败的中间状态，不利于幂等和排错。

推荐改造：

```go
func (s *DocumentService) replacePlansAndEventsByDocumentID(
    ctx context.Context,
    document domain.Document,
    plans []domain.CandidatePlan,
) ([]domain.CandidatePlan, []domain.RecommendationEvent, error)
```

事务内部步骤：

1. `dal.TradeCandidatePlans.DeleteByDocumentID(ctx, tx, document.ID)`。
2. 循环创建新的 `trade_candidate_plans`。
3. 将每个保存后的 plan map 回 `domain.CandidatePlan`。
4. 调用 `s.upsertRecommendationEventForPlan(ctx, tx, document, savedPlan)`。
5. 事件 upsert 后重建证据。
6. 返回保存后的 plans 和 events。

### 8.2 Blogger 归一化

新增 service 私有方法：

```go
func (s *DocumentService) resolveBloggerForPlan(ctx context.Context, db *gorm.DB, document domain.Document, plan domain.CandidatePlan) (*db_model.Blogger, error)
```

规则：

1. `name = strings.TrimSpace(plan.Analyst)`。
2. 如果为空，用 `document.Author`。
3. 如果仍为空，用当前配置 `cfg.Document.SourceDefaults.Author`。
4. 如果仍为空，置为 `"UNKNOWN"`。
5. `institution = strings.TrimSpace(plan.Institution)`，为空时用 `document.Institution`。
6. `normalized_name = normalizeBloggerName(name)`。
7. upsert `bloggers` 后重新查询，拿到 `blogger_id`。

`normalizeBloggerName` 只做确定性本地处理：

```text
trim space -> collapse internal whitespace -> lower case
```

不做远程解析，不做 AI 修正。

### 8.3 Dedupe Key 生成

新增 service 私有方法：

```go
func recommendationEventDedupeKey(blogger db_model.Blogger, documentID int64, plan domain.CandidatePlan) string
```

输入固定为：

```text
normalized_blogger_name
institution
source_document_id
symbol
direction
recommend_date(YYYY-MM-DD)
```

然后用现有 `utils.SHA256Hex` 生成 64 字符 hex。为避免将字符串转字节的逻辑散落，可直接复用：

```go
utils.SHA256Hex([]byte(rawKey))
```

### 8.4 推荐事件状态映射

新增私有方法：

```go
func recommendationStatusFromPlan(plan domain.CandidatePlan) string
```

规则：

```text
READY -> ACTIVE
其他 -> NEEDS_REVIEW
```

不要把 `CandidatePlan.Status` 原样写入推荐事件，因为两者语义不同：候选计划状态关注交易参数是否可执行，推荐事件状态关注事实是否可进入评估。

### 8.5 查询服务

阶段一可以先在 `DocumentService` 中增加：

```go
func (s *DocumentService) ListRecommendationEvents(ctx context.Context, limit int) ([]domain.RecommendationEvent, error)
func (s *DocumentService) ListRecommendationEventsByDocumentID(ctx context.Context, documentID int64) ([]domain.RecommendationEvent, error)
func (s *DocumentService) GetRecommendationEventByID(ctx context.Context, id int64) (*domain.RecommendationEvent, error)
```

后续阶段统计和评估逻辑变多时，再把推荐事件查询迁移到单独 `RecommendationService`。

## 9. HTTP API 设计

阶段一只提供推荐事件主查询，先不实现 `refresh-evaluation`。

新增路由：

```text
GET /api/v1/recommendations
GET /api/v1/recommendations/{id}
GET /api/v1/documents/{id}/recommendations
```

`GET /api/v1/recommendations` 参数：

| 参数 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `limit` | int | 100 | 最大建议 500 |
| `blogger` | string | 空 | 可按展示名模糊或 normalized name 精确匹配，阶段一建议先不做模糊 |
| `symbol` | string | 空 | 股票代码 |
| `direction` | string | 空 | `LONG` / `SHORT` |
| `from` | date | 空 | `recommend_date >= from` |
| `to` | date | 空 | `recommend_date <= to` |
| `status` | string | 空 | `ACTIVE` / `NEEDS_REVIEW` |

阶段一可以先只实现 `limit`、`symbol`、`direction`、`status`，日期过滤放在同一个 DAL 查询能力中预留。

响应建议：

```json
[
  {
    "id": 1,
    "blogger_id": 1,
    "blogger_name": "张三",
    "source_document_id": 7,
    "plan_id": 12,
    "symbol": "600000.SH",
    "direction": "LONG",
    "recommend_date": "2026-05-16T00:00:00Z",
    "reference_price": 10.5,
    "confidence": 0.82,
    "status": "ACTIVE",
    "thesis": "...",
    "evidence": []
  }
]
```

详情接口 `GET /api/v1/recommendations/{id}` 应加载 evidence。列表接口默认不加载 evidence，避免长文本影响列表响应。

## 10. 代码落点清单

阶段一建议按下面顺序改：

1. 新增 `migrations/002_recommendation_events.sql`。
2. 在数据库执行 migration。
3. 执行 `go run generate.go`。
4. 检查生成文件：
   - `internal/domain/db_model/blogger.gen.go`
   - `internal/domain/db_model/recommendation_event.gen.go`
   - `internal/domain/db_model/recommendation_event_evidence.gen.go`
5. 新增领域模型：
   - `internal/domain/recommendation.go`
6. 新增 DAL：
   - `internal/dal/blogger.go`
   - `internal/dal/recommendation_event.go`
   - `internal/dal/recommendation_event_evidence.go`
7. 修改 service：
   - `internal/service/document.go`
   - 增加 plan -> event 转换
   - 增加 blogger 归一化
   - 增加事件查询方法
8. 修改 API：
   - `internal/httpapi/server.go`
   - 注册推荐事件路由
   - 增加 handler 和参数解析
9. 增加测试：
   - service 派生测试
   - dedupe key 测试
   - DAL 查询测试或最小集成测试
10. 执行：
   - `gofmt -w` 修改过的 Go 文件
   - `env GOTOOLCHAIN=local go test ./...`
   - `env GOTOOLCHAIN=local go build ./...`

## 11. 幂等策略

阶段一幂等由三层保证：

1. `bloggers.uk_bloggers_normalized_institution` 保证来源实体不重复。
2. `recommendation_events.uk_recommendation_events_dedupe_key` 保证推荐主事件不重复。
3. `recommendation_event_evidences` 对命中事件先删后插，保证证据与最新 plan 一致。

重复运行同一文档分析时：

```text
旧 trade_candidate_plans 被替换
-> 新 trade_candidate_plans 生成新 ID
-> recommendation_events 命中 dedupe_key
-> 更新 plan_id 和派生字段
-> 重建 evidence
```

这样后续评估结果可以稳定挂在推荐事件 ID 上，而不是挂在易变的候选计划 ID 上。

## 12. 测试细则

### 12.1 单元测试

建议新增或覆盖：

- `normalizeBloggerName`：
  - 去首尾空格。
  - 合并连续空白。
  - 英文大小写归一。
  - 中文保持原文。
- `recommendationEventDedupeKey`：
  - 相同输入返回相同 key。
  - 不同方向返回不同 key。
  - 不同文档 ID 返回不同 key。
- `recommendationStatusFromPlan`：
  - `READY -> ACTIVE`。
  - `NEEDS_REVIEW -> NEEDS_REVIEW`。

### 12.2 Service 测试

可以使用 SQLite 或 mock DAL 不太适合当前 GORM/MySQL 结构；若不引入新测试依赖，优先测试纯函数。涉及事务和外键的测试可以作为后续集成测试。

最小 service 场景：

1. 构造 document 和两个 saved plan。
2. 一个 plan 状态 `READY`，一个状态 `NEEDS_REVIEW`。
3. 验证派生出的事件状态、blogger、dedupe key 符合预期。

### 12.3 API 测试

在 `cmd/api` 或 `internal/httpapi` 现有测试风格上增加：

- `GET /api/v1/recommendations` 返回数组。
- 非法 `direction` 返回 400。
- 非法日期返回 400。
- `GET /api/v1/recommendations/{id}` 对不存在 ID 返回 404 或现有错误风格的 500。建议本阶段统一把 `dal.ErrNotFound` 映射为 404。

## 13. 风险与取舍

### 13.1 是否拆 RecommendationService

阶段一不强制拆。理由：

- 推荐事件创建依赖候选计划保存后的 ID。
- 当前 `DocumentService` 已经承担分析链路编排。
- 提前拆 service 会增加装配复杂度。

但如果阶段二开始加入评估刷新、统计查询，建议新增：

```text
internal/service/recommendation.go
internal/service/evaluation.go
internal/service/stats.go
```

### 13.2 推荐事件是否保留 plan_id

需要保留。`plan_id` 用于审计“本次推荐事件从哪个候选计划派生”。但后续评估和统计主键应使用 `recommendation_event_id`，不能使用 `plan_id`，因为重新分析同一文档会替换候选计划。

### 13.3 旧事件是否标记 SUPERSEDED

阶段一可以暂不做。原因是当前 `dedupe_key` 命中时会更新原事件；如果某次重新分析减少了某个 symbol，是否应该废弃旧事件属于产品口径，需要人工确认。本阶段只保证不重复创建。

### 13.4 是否建 evidence 独立表

建议建。候选计划当前用 `evidence_json`，但推荐事件会进入详情查询、人工校对和后续统计排查，独立表更便于扩展，同时避免列表查询加载长 JSON。

## 14. 验收标准

阶段一验收以以下行为为准：

1. 上传并分析样本文档后，系统保存 `trade_candidate_plans` 的同时生成 `recommendation_events`。
2. `GET /api/v1/recommendations` 可以查到最新推荐事件。
3. `GET /api/v1/documents/{id}/recommendations` 可以查到指定文档派生的推荐事件。
4. `GET /api/v1/recommendations/{id}` 可以返回主事件和证据。
5. 同一文档重复分析，不新增重复 `recommendation_events` 主事件，只更新命中 dedupe key 的事件内容和证据。
6. 候选计划状态为 `READY` 时，推荐事件状态为 `ACTIVE`；否则为 `NEEDS_REVIEW`。
7. `go test ./...` 和 `go build ./...` 通过。
