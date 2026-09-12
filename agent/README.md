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

## Optional streaming relay diagnostic

`tests/test_llm_streaming_probe.py` includes offline SSE tests and one live test
that is **skipped by default**. The live test reads Nacos and a selected production
document through `dbx`, reuses the actual extraction prompt/JSON-output parameters,
and changes only `stream=true`. It makes one model request with no automatic
retries, never writes analysis results, and does not change the production client.

From the `agent` directory on macOS:

```bash
FINANCE_SYS_STREAM_TEST=1 FINANCE_SYS_STREAM_DOCUMENT_ID=7674 \
  FINANCE_SYS_STREAM_TOTAL_SECONDS=600 \
  .venv/bin/python -B -m pytest -s -q tests/test_llm_streaming_probe.py -k real_relay
```

Windows PowerShell:

```powershell
$env:FINANCE_SYS_STREAM_TEST = '1'
$env:FINANCE_SYS_STREAM_DOCUMENT_ID = '7674'
$env:FINANCE_SYS_STREAM_TOTAL_SECONDS = '600'
try {
  .\.venv\Scripts\python.exe -B -m pytest -s -q tests/test_llm_streaming_probe.py -k real_relay
} finally {
  Remove-Item Env:FINANCE_SYS_STREAM_TEST, Env:FINANCE_SYS_STREAM_DOCUMENT_ID, Env:FINANCE_SYS_STREAM_TOTAL_SECONDS -ErrorAction SilentlyContinue
}
```

The diagnostic uses the existing bootstrap address file. `dbx` must be on PATH;
its connection name defaults to `home` and can be selected with
`FINANCE_SYS_STREAM_DBX_CONNECTION`. The document ID and time budgets are test
inputs, not overrides of production business configuration.

Reports under `tmp/diagnostics/llm-stream-test-*.json` contain timing, character
counts, completion markers and schema validation results only. They never contain
keys, article text or raw reasoning. Success requires `[DONE]`, `finish_reason=stop`
and the existing Agent JSON/Pydantic validation. Headers or the first token alone
are not success. A diagnostic deadline actively closes the connection and may
appear as `context canceled` in relay logs; distinguish that from upstream errors.

## API

### Internal streaming and shared deadline

Set `llm.stream=true` in Nacos to consume model SSE incrementally. Reasoning is
discarded; only a complete `[DONE]` / `finish_reason=stop` response is parsed and
validated. JSON-only compatible responses are still accepted. Go's direct LLM
fallback supports the same switch. Go/HTTP callers still receive one JSON result.

The requested release values are **1000000 ms for all three**:
`llm.timeout_ms`, `agent.timeout_ms`, and `service.http.write_timeout_ms`.
They are shared total budgets, including retries, not a fresh allowance per
attempt. OCR and tool work consume the same document budget. Go forwards the
remaining deadline in `X-Finance-Deadline-Unix-Ms`; the Agent clamps it to its own
configured limit. Caller disconnect/deadline cancels the graph's HTTP work and
closes active streaming sockets. Go does not retry an expired caller deadline.

`configs/nacos_streaming_1000s.merge.json` is a **merge patch**, not a complete
Nacos configuration. Apply it to the latest `expert_trade` JSON via the Nacos API
with concurrency protection and a new config version during an authorized
deployment. Preserve model, keys, database, trading and other settings. The old
binary rejects equal LLM/Agent timeouts: coordinate the configuration update with
the new API and reload the analysis Agent (its settings are cached). Do not publish
this patch independently while keeping the old runtime. Never replace the entire
Nacos document with the patch. No production restart is implied by build-only tests.

To exercise the new production streaming client once, read-only, using the
1000-second pending settings in memory (from the `agent` directory):

```bash
FINANCE_SYS_STREAM_PRODUCTION_TEST=1 .venv/bin/python -B -m pytest -s -q tests/test_llm_streaming_probe.py -k real_production
```

This optional test does not update Nacos or write recommendation results.

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
