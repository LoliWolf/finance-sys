---
name: tushare-data
description: Tushare Pro 数据调用技能。用于把股票、基金、指数、期货、债券、港美股、宏观、财务、资金流、公告新闻等数据请求转成可执行脚本调用；尤其适用于需要通过 scripts/tushare_call.py 使用命令行参数 --token 调用 references/数据接口.md 中列出的任意 Tushare 接口，并导出 CSV/JSON/JSONL 或预览结果的场景。
---

# tushare-data

使用本 skill 时，优先通过 `scripts/tushare_call.py` 调用 Tushare。脚本覆盖 `references/数据接口.md` 中列出的接口；接口入参按官方文档传给 `--param` 或 `--params-json`。

## Hard Rules

- 必须通过脚本参数 `--token` 传入 Tushare token。
- 不要从环境变量、`ts.get_token()`、本地配置文件或其他隐式位置读取 token。
- 不要调用 `ts.set_token()`；它会尝试写本地 token 缓存。普通接口使用 `ts.pro_api(token)`，`pro_bar` 使用显式 `api=ts.pro_api(token)`。
- 调用前先用 `--show <api>` 找到接口文档链接；不确定具体入参时打开官方文档核对。
- 没有权限、积分不足、空结果或日期不合理时，明确说明限制，不要伪造数据。

## Script

通用脚本：

```bash
python tushare/scripts/tushare_call.py <api> --token <TOKEN> [options]
```

常用检查：

```bash
python tushare/scripts/tushare_call.py --list
python tushare/scripts/tushare_call.py --show daily
python tushare/scripts/tushare_call.py daily --param ts_code=000001.SZ --dry-run
```

## Script Parameters

`<api>`：Tushare 接口名，例如 `daily`、`stock_basic`、`fund_nav`、`cn_cpi`。必须是 `references/数据接口.md` 中的接口，除非显式加 `--allow-unknown`。

`--token <TOKEN>`：Tushare token。真实取数必填，只能从这个参数传入。

`--param KEY=VALUE`：传给接口的入参，可重复。值默认按字符串传递，适合 `ts_code=000001.SZ`、`start_date=20240101` 这类 Tushare 参数。

`--params-json JSON_OR_@FILE`：从 JSON 对象传参，适合批量参数或需要数字/布尔类型的场景。可以直接传 JSON 字符串，也可以传 `@params.json`。

`--fields a,b,c`：传给 Tushare 的返回字段列表。`pro_bar` 不支持远端 fields 时，脚本会在本地结果中筛列。

`--output PATH` / `-o PATH`：输出文件路径。后缀 `.csv`、`.json`、`.jsonl` 会自动推断格式；未提供时只在 stdout 预览。

`--format table|csv|json|jsonl`：显式指定输出格式。未指定时，stdout 默认 `table`，文件输出按后缀推断。

`--encoding ENCODING`：CSV 文件编码，默认 `utf-8-sig`，便于 Excel 打开中文。

`--head N`：未指定 `--output` 时输出前 N 行，默认 20；`--head 0` 输出全部。

`--call-style auto|query|method|pro-bar`：调用方式。默认 `auto`，即 `pro_bar` 走 `ts.pro_bar`，其他接口走 `pro.query(api, ...)`。接口需要 SDK 方法直调时用 `--call-style method`。

`--allow-unknown`：允许调用未列入 `references/数据接口.md` 的接口，用于 Tushare 新增接口临时验证。

`--dry-run`：只打印解析后的接口名、参数和 fields，不导入 Tushare、不联网、不要求 token。

`--list`：列出 `references/数据接口.md` 中的全部接口、标题、分类和文档链接。

`--show API`：只显示某个接口在清单中的记录和文档链接。

## Examples

股票基础信息：

```bash
python tushare/scripts/tushare_call.py stock_basic --token "your_token" --param list_status=L --fields ts_code,symbol,name,area,industry,list_date --output out/stock_basic.csv
```

个股日线：

```bash
python tushare/scripts/tushare_call.py daily --token "your_token" --param ts_code=000001.SZ --param start_date=20240101 --param end_date=20240131 --fields ts_code,trade_date,open,high,low,close,vol,amount --output out/daily_000001.SZ.csv
```

复权行情：

```bash
python tushare/scripts/tushare_call.py pro_bar --token "your_token" --param ts_code=000001.SZ --param adj=qfq --param start_date=20240101 --param end_date=20240131 --output out/pro_bar_000001.SZ_qfq.csv
```

财务指标：

```bash
python tushare/scripts/tushare_call.py fina_indicator --token "your_token" --param ts_code=600519.SH --param start_date=20230101 --param end_date=20241231 --fields ts_code,end_date,roe,roa,grossprofit_margin,netprofit_margin,debt_to_assets --output out/fina_indicator_600519.SH.csv
```

基金净值：

```bash
python tushare/scripts/tushare_call.py fund_nav --token "your_token" --param ts_code=000001.OF --param start_date=20240101 --param end_date=20240131 --output out/fund_nav_000001.OF.csv
```

宏观 CPI：

```bash
python tushare/scripts/tushare_call.py cn_cpi --token "your_token" --param start_m=202301 --param end_m=202412 --output out/cn_cpi.csv
```

JSON 文件传参：

```bash
python tushare/scripts/tushare_call.py daily --token "your_token" --params-json '{\"ts_code\":\"000001.SZ\",\"start_date\":\"20240101\",\"end_date\":\"20240131\"}' --format json
```

在 Bash 中可去掉 JSON 字符串里的反斜杠；在 PowerShell 中保留上面的写法，或改用 `--params-json @params.json` 避免引号转义。

## Interface Coverage

- `references/数据接口.md` 是接口清单入口，当前脚本会解析该文件中的表格并校验接口名。
- 表里重复出现的接口名使用同一个 `<api>` 调用，例如重复的 `index_daily`、`pro_bar`、`rt_min`。
- 每一行接口的调用模板相同：`python tushare/scripts/tushare_call.py <api> --token <TOKEN> --param key=value ...`。
- 具体业务入参和返回字段以每行的 Tushare 官方文档链接为准。

## Workflow

1. 先把用户请求归类为行情、基础资料、财务、估值、资金流、板块、新闻公告、宏观或导出。
2. 用 `--list` 或 `references/数据接口.md` 找接口；用 `--show <api>` 取文档链接。
3. 核对官方文档中的必填参数、可选参数、权限和字段。
4. 用 `--dry-run` 检查命令参数，确认 token 只来自 `--token`。
5. 正式调用并输出 CSV/JSON/JSONL；大表优先写文件，不要把全量数据直接刷到 stdout。
6. 返回结果时说明接口、参数、数据范围、行数、输出文件路径和可能的权限限制。

## Error Handling

- 缺少 `tushare` 包：提示 `python -m pip install tushare`。
- 接口名不在清单：先检查拼写；确实是新增接口时才加 `--allow-unknown`。
- 空结果：区分非交易日、无数据、权限不足、参数错误、标的未上市。
- 权限或积分不足：直接说明当前 token 可能无该接口权限。
- 字段报错：回到官方文档核对 `--fields`，不要猜字段。
