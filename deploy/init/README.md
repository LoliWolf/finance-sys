Place local bootstrap assets here if you want to pre-seed Nacos or database fixtures during Docker-based development.

## Tushare security master initialization

Use this command to initialize existing security reference tables from the local Tushare skill:

```powershell
$env:GOTOOLCHAIN='local'
$env:GOCACHE=(Join-Path (Get-Location) '.gocache')
go run ./cmd/init-tushare-security
```

The command reads Nacos bootstrap variables from the current environment, falling back to `bootstrap_go122.env` or `bootstrap_go122.env.example` when present. It loads `agent.tushare.token` and the MySQL DSN from the Nacos JSON config, calls `agent/skills/tushare/scripts/tushare_call.py`, and upserts:

- `security_master` from `stock_basic`, `etf_basic`, or `fund_basic market=E` as an ETF fallback.
- `security_aliases` from `namechange` historical names.

Useful options:

```powershell
go run ./cmd/init-tushare-security --dry-run
go run ./cmd/init-tushare-security --stock-statuses= --skip-namechange
```
