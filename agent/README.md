# M4 Python Agent Sidecar

This is the Python sidecar used by M4. It accepts cleaned parser chunks from the Go service and returns stable JSON for instrument-resolution analysis.

The first M4 implementation keeps the safety boundary narrow:

- The Agent does not write the database.
- The Agent does not generate entry price, stop loss, take profit, or position size.
- The Agent can return raw intents; Go M3 still resolves securities against local security master data before rules run.
- Python validation happens first with Pydantic, then Go validates the response again.

## Run

```powershell
python -m venv .venv
.\.venv\Scripts\python -m pip install -e ".[test]"
.\.venv\Scripts\python -m uvicorn app.main:app --host 127.0.0.1 --port 8108
```

## Test

```powershell
.\.venv\Scripts\python -m pytest -q
```

## API

- `GET /healthz`
- `POST /v1/resolve-document`

The sidecar reads the same Nacos JSON document as the Go service when these bootstrap variables are present:

```powershell
$env:NACOS_SERVER_ADDR="127.0.0.1:8848"
$env:NACOS_NAMESPACE="public"
$env:NACOS_GROUP="DEFAULT_GROUP"
$env:NACOS_DATA_ID="expert_trade"
$env:NACOS_USERNAME=""
$env:NACOS_PASSWORD=""
```

In Nacos mode, Python reuses:

- `llm`: OpenAI-compatible endpoint, model, key, timeout, and retry settings.
- `agent.auth`: inbound service token used by Go when calling Python.

Local environment fallback is only for development without Nacos:

```powershell
$env:AGENT_AUTH_ENABLED="true"
$env:AGENT_AUTH_TOKEN="local-token"
$env:AGENT_AUTH_HEADER="X-Agent-Token"
$env:AGENT_LLM_ENABLED="true"
$env:AGENT_LLM_ENDPOINT="https://example.com/v1/chat/completions"
$env:AGENT_LLM_MODEL="your-model"
$env:AGENT_LLM_API_KEY="your-key"
$env:AGENT_LLM_TIMEOUT_MS="15000"
$env:AGENT_LLM_MAX_RETRIES="1"
```
