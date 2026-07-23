# M4 Python Agent Sidecar

This is the Python sidecar used by M4. It accepts cleaned parser chunks from the Go service and returns stable JSON for instrument-resolution analysis.

The first M4 implementation keeps the safety boundary narrow:

- The Agent does not write the database.
- The Agent does not generate entry price, stop loss, take profit, or position size.
- The Agent can return raw intents; Go M3 still resolves securities against local security master data before rules run.
- Python validation happens first with Pydantic, then Go validates the response again.

## Prerequisites

- Python 3.9 or newer.
- Run the following commands from the `agent` directory.

The commands below call the virtual-environment interpreter directly. Activation
is optional, and Windows PowerShell execution policy does not affect startup.

## Run on macOS or Linux

The Python bundled with macOS can have an old `pip`, so upgrade the packaging
tools before installing this `pyproject.toml` project.

```bash
python3 -m venv .venv
.venv/bin/python -m pip install --upgrade pip setuptools wheel
.venv/bin/python -m pip install -e '.[test]'
.venv/bin/python -m app.runner
```

## Run on Windows PowerShell

```powershell
py -3 -m venv .venv
.\.venv\Scripts\python.exe -m pip install --upgrade pip setuptools wheel
.\.venv\Scripts\python.exe -m pip install -e ".[test]"
.\.venv\Scripts\python.exe -m app.runner
```

## Test

macOS or Linux:

```bash
.venv/bin/python -m pytest -q
```

Windows PowerShell:

```powershell
.\.venv\Scripts\python.exe -m pytest -q
```

## API

- `GET /healthz`
- `POST /v1/resolve-document`

The sidecar reads the same Nacos JSON document as the Go service whenever
`NACOS_SERVER_ADDR` is set. It is the only required bootstrap variable:

```text
NACOS_SERVER_ADDR=127.0.0.1:8848
```

In real Nacos mode, all other bootstrap values are fixed in code:
namespace `public`, group `DEFAULT_GROUP`, data ID `expert_trade`, no Nacos
username/password, and a 5000 ms timeout. Other `NACOS_*`, `AGENT_*`, and
`TUSHARE_*` environment variables do not override Nacos or runtime settings.
If Nacos cannot be loaded or validated, the Agent fails immediately instead of
falling back to local environment configuration.

`python -m app.runner` safely reads only `NACOS_SERVER_ADDR` from the repository
bootstrap file when the variable is not already set, then derives the listen
host and port from `agent.endpoint` in Nacos. Do not pass a separate local port.

In Nacos mode, Python reuses:

- `llm`: OpenAI-compatible endpoint, model, key, timeout, and retry settings.
- `agent.auth`: inbound service token used by Go when calling Python.

Environment-based configuration is only a local test/development fallback when
`NACOS_SERVER_ADDR` is completely unset. It is not used by real Nacos runs:

```text
AGENT_AUTH_ENABLED=true
AGENT_AUTH_TOKEN=local-token
AGENT_AUTH_HEADER=X-Agent-Token
AGENT_LLM_ENABLED=true
AGENT_LLM_ENDPOINT=https://example.com/v1/chat/completions
AGENT_LLM_MODEL=your-model
AGENT_LLM_API_KEY=your-key
AGENT_LLM_TIMEOUT_MS=15000
AGENT_LLM_MAX_RETRIES=1
```

Set these with `export NAME=value` on macOS/Linux or
`$env:NAME = "value"` in Windows PowerShell. With no Nacos or LLM variables,
the sidecar starts in its deterministic local-development mode.
