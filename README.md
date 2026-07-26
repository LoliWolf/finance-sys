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

当前已经落地推荐事件事实层，以及 Tushare 日线同步任务、同步 worker 和同步记录查询。推荐后窗口评估、统计排行榜和可视化报表仍属于后续规划链路。

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
- 真实运行的 Tushare token 只能来自 Nacos，不能提交到仓库或本地 bootstrap 文件。
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
- `POST /api/v1/admin/market/stock-daily/sync`
- `GET /api/v1/admin/market/sync-runs`
- `POST /api/v1/internal/security/resolve`
- `POST /api/v1/internal/security/verify`
- `POST /api/v1/admin/config/reload`

## 跨平台开发环境

原生开发机只需要 Go 1.22.x、Python 3.9+ 和文档处理依赖。MySQL、Nacos、LLM 和 Tushare 都使用 Nacos 指向的现有远程服务，不要在 Mac/Windows 本机另外安装这些中间件。

macOS：

```bash
python3 --version
python3 -m venv tools/guziyuan_pdf_ocr_tool/.venv
tools/guziyuan_pdf_ocr_tool/.venv/bin/python -m pip install -r tools/guziyuan_pdf_ocr_tool/requirements.txt
go version
pdftoppm -v
swift --version
```

Go 可使用官方安装包或版本管理器；不要在脚本中写死 `/opt/homebrew` 或 `/usr/local`。macOS 的旧 `.doc` 解析优先使用系统 `/usr/bin/textutil`，不需要 antiword。OCR 只硬依赖 `pdftoppm`；其缺失时再可选安装 Poppler，其他 Poppler 提取命令缺失时工具会降级到整页渲染 + Vision OCR。

Windows：

- 安装 Go 1.22.x、Python 3.9+ 和 Poppler，并把它们的可执行目录加入 `PATH`。只有需要解析旧 `.doc` 时才额外安装 antiword。
- 图片型 PDF OCR 使用 Windows.Media.Ocr，需安装简体中文 OCR 语言包。
- 使用 `py -3 -m venv tools\guziyuan_pdf_ocr_tool\.venv` 创建 OCR 虚拟环境，再用该环境的 `python.exe -m pip install -r tools\guziyuan_pdf_ocr_tool\requirements.txt` 安装依赖。
- 用 `go version`、`where pdftoppm` 和 `py -3 --version` 检查环境；如需 `.doc` 再检查 `where antiword`。

## Nacos 配置

运行时通过环境变量 `NACOS_SERVER_ADDR` 读取 Nacos 中的单个 JSON 配置文档。生产 MySQL 完整配置位于 `database`，同构开发测试库完整配置位于 `database_test`。只有进程环境变量 `FINANCE_SYS_ENV` 精确等于 `PROD` 才使用 `database`；变量缺失、为空、大小写不符或其他值均使用 `database_test`。HTTP 端口、LLM Key、Tushare token 和业务开关也都在 Nacos，不在本地 bootstrap 文件中重复维护。没有设置 Nacos 地址，或 Nacos 配置读取失败时，Go 主程序会降级读取 `configs/example_nacos_config.json`，并应用同一数据库选择规则。

复制 bootstrap 示例：

```bash
# macOS
cp bootstrap_go122.env.example bootstrap_go122.env
```

```bat
rem Windows cmd.exe
copy bootstrap_go122.env.example bootstrap_go122.env
```

`bootstrap_go122.env` 已被 Git 忽略。项目当前 Nacos 地址已写入示例；只有切换网络或环境时才需要改 `NACOS_SERVER_ADDR`。用于定位配置文档的 `public / DEFAULT_GROUP / expert_trade` 固定在代码中。程序不会读取本地的 namespace、group、dataId、Nacos 凭据或业务配置覆盖值；bootstrap 文件若出现地址以外的配置键，启动脚本会直接报错。`FINANCE_SYS_ENV` 是进程级安全开关，不写入 bootstrap 文件。

本地开发、调试、模型生成和测试无需设置 `FINANCE_SYS_ENV`，默认连接 `database_test`。线上服务必须显式设置：

```bash
export FINANCE_SYS_ENV=PROD
./start_api_nacos.sh
```

Windows PowerShell：

```powershell
$env:FINANCE_SYS_ENV = "PROD"
.\start_api_nacos.bat
```

`config_snapshots.raw_json` 会把实际加载的配置 JSON 原文写入数据库，供内部审计和版本追踪；该内容不会写入仓库。

## 生成数据库模型

修改表结构后执行：

```bash
go run generate.go
```

该命令会按启动路径和 `FINANCE_SYS_ENV` 选择 MySQL DSN，并基于当前数据库结构同步 `internal/domain/db_model`；默认使用测试 Schema。

## 运行 API

当前 Nacos 配置为 `agent.enabled=true`，因此完整文档分析要先在一个终端启动 Agent（首次安装见 `agent/README.md`）：

```bash
cd agent
.venv/bin/python -m app.runner
```

Windows PowerShell：

```powershell
Set-Location agent
.\.venv\Scripts\python.exe -m app.runner
```

然后在另一个终端启动 API。

直接本地调试且不提供 `NACOS_SERVER_ADDR` 时，可以运行：

```bash
go run ./cmd/api
```

此时主程序使用 `configs/example_nacos_config.json`；如果设置了 Nacos 地址但远端暂时不可读，也会使用同一份本地示例配置。

macOS 后台启动（构建、等待健康检查，默认打开上传页）：

```bash
./start_api_nacos.sh
```

前台调试：

```bash
./debug_api_nacos.sh
```

Windows 使用：

```bat
start_api_nacos.bat
debug_api_nacos.bat
```

脚本优先读取不入库的 `bootstrap_go122.env`，文件不存在时读取 `bootstrap_go122.env.example`。它们会直接从 Nacos JSON 读取 `service.http.port`，因此 bootstrap 文件不保存 HTTP 端口，也不接受本地 `APP_PORT`/`APP_BASE_URL` 覆盖。

脚本只会停止上次由同一脚本记录的 PID；如果端口被其他进程占用会报错，不会误杀其他服务。macOS 日志位于 `tmp/api_nacos.log`，Windows 日志位于 `tmp/api_nacos.out.log` 和 `tmp/api_nacos.err.log`。当前 Nacos 配置解析为 `http://127.0.0.1:30005`。

如需初始化 Tushare 证券主数据：

```bash
GOTOOLCHAIN=local go run ./cmd/init-tushare-security --python "$(pwd)/agent/.venv/bin/python"
```

## 验证

macOS/Linux：

```bash
GOTOOLCHAIN=local go test ./...
GOTOOLCHAIN=local go build ./...
```

在 Windows PowerShell：

```powershell
$env:GOTOOLCHAIN = 'local'
go test ./...
go build ./...
```

部分集成测试会写入配置指向的 MySQL，默认通过环境变量门禁跳过。运行前先阅读对应测试文件中的说明。

## 项目文档

完整 PRD、技术方案、阶段复盘和演进路线统一维护在飞书知识库：

<https://my.feishu.cn/wiki/DAUVw7Lr6i7L3XkulELcWmoCnLh>

仓库内只保留必要入口说明、子模块运行说明和代码旁文档。
