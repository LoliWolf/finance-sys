"""SSE protocol checks plus an opt-in, read-only production-relay diagnostic.

No production client behavior is changed. Real runs only read Nacos/MySQL, then
make ONE model request; they never persist recommendations or raw reasoning.
"""

from dataclasses import dataclass, field
from datetime import datetime, timezone
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import time
from urllib.error import HTTPError
from urllib.request import Request, urlopen

import pytest

from app.config import NacosBootstrap, NacosConfigLoader, _settings_from_nacos_config
from app.http_client import _set_response_timeout, redact_secrets
from app.llm_client import LLMClient, _parse_chat_completion
from app.runner import load_nacos_server_address

ROOT = Path(__file__).resolve().parents[2]


@dataclass
class StreamCapture:
    content: str = field(default="", repr=False)
    reasoning_chars: int = 0
    events: int = 0
    wire_bytes: int = 0
    done: bool = False
    finish_reason: str = ""
    first_event_seconds: float = None
    first_content_seconds: float = None
    max_event_gap_seconds: float = 0
    elapsed_seconds: float = 0

    def metrics(self):
        return {key: value for key, value in vars(self).items() if key != "content"} | {"content_chars": len(self.content)}


def consume_sse(response, capture, started, total_seconds=600, idle_seconds=60, progress=None):
    """Parse complete SSE events; count reasoning separately from final JSON."""
    parts = []
    last_event = None
    last_report = started
    try:
        while not capture.done:
            remaining = total_seconds - (time.monotonic() - started)
            if remaining <= 0:
                raise TimeoutError("diagnostic total deadline reached; the test is closing the connection")
            _set_response_timeout(response, min(idle_seconds, remaining))
            raw = response.readline(2 * 1024 * 1024 + 1)
            capture.wire_bytes += len(raw)
            if len(raw) > 2 * 1024 * 1024 or capture.wire_bytes > 16 * 1024 * 1024:
                raise ValueError("diagnostic stream safety size limit reached")
            if not raw:
                raise ValueError("stream ended without [DONE]; response is incomplete")
            line = raw.decode("utf-8").rstrip("\r\n")
            if line.startswith("data:"):
                parts.append(line[5:].lstrip(" "))
            elif line == "" and parts:
                data = "\n".join(parts)
                parts = []
                if data == "[DONE]":
                    capture.done = True
                    break
                event = json.loads(data)
                if event.get("error"):
                    raise ValueError("relay returned an error event; not a successful completion")
                now = time.monotonic()
                capture.events += 1
                if capture.first_event_seconds is None:
                    capture.first_event_seconds = round(now - started, 3)
                if last_event is not None:
                    capture.max_event_gap_seconds = round(max(capture.max_event_gap_seconds, now - last_event), 3)
                last_event = now
                for choice in event.get("choices", []):
                    if choice.get("index", 0) != 0:
                        continue
                    delta = choice.get("delta") or {}
                    content = delta.get("content") or ""
                    reasoning = delta.get("reasoning_content") or ""
                    if not isinstance(content, str) or not isinstance(reasoning, str):
                        raise ValueError("non-text chat delta")
                    if content and capture.first_content_seconds is None:
                        capture.first_content_seconds = round(now - started, 3)
                    capture.content += content
                    capture.reasoning_chars += len(reasoning)
                    if choice.get("finish_reason"):
                        capture.finish_reason = choice["finish_reason"]
                if progress and now - last_report >= 15:
                    capture.elapsed_seconds = round(now - started, 3)
                    progress(capture.metrics())
                    last_report = now
    finally:
        capture.elapsed_seconds = round(time.monotonic() - started, 3)
    if capture.finish_reason != "stop":
        raise ValueError("model did not finish normally: " + (capture.finish_reason or "missing finish_reason"))


class PayloadCaptured(Exception):
    pass


class RequestCapture:
    def post(self, url, **kwargs):
        self.url, self.payload, self.headers = url, kwargs["json"], kwargs["headers"]
        raise PayloadCaptured()


def project_stream_request(settings, text):
    capture = RequestCapture()
    with pytest.raises(PayloadCaptured):
        LLMClient(settings, http_client=capture).extract_document(text, 20)
    # The ONLY payload change relative to the production extraction request.
    capture.payload["stream"] = True
    return capture


def sse(*events):
    return io.BytesIO(b"".join(b"data: " + (event.encode() if isinstance(event, str) else json.dumps(event).encode()) + b"\r\n\r\n" for event in events))


def test_sse_reassembles_final_json_without_reasoning():
    response = sse(
        {"choices": [{"delta": {"reasoning_content": "not final output"}}]},
        {"choices": [{"delta": {"content": '{"author":"",'}}]},
        {"choices": [{"delta": {"content": '"raw_intents":[]}'}, "finish_reason": "stop"}]},
        {"choices": [], "usage": {"completion_tokens": 20}}, "[DONE]",
    )
    capture = StreamCapture()
    consume_sse(response, capture, time.monotonic())
    assert capture.done
    assert capture.reasoning_chars == len("not final output")
    assert _parse_chat_completion({"choices": [{"message": {"content": capture.content}}]}, 20).raw_intents == []
    assert "not final output" not in str(capture.metrics())


def test_sse_eof_is_not_success():
    with pytest.raises(ValueError, match="without.*DONE"):
        consume_sse(sse({"choices": [{"delta": {"content": "{"}}]}), StreamCapture(), time.monotonic())


def test_sse_output_token_limit_is_not_success():
    with pytest.raises(ValueError, match="length"):
        consume_sse(sse({"choices": [{"delta": {}, "finish_reason": "length"}]}, "[DONE]"), StreamCapture(), time.monotonic())


def test_sse_error_event_does_not_echo_error_body():
    with pytest.raises(ValueError) as error:
        consume_sse(sse({"error": {"message": "secret upstream debug content"}}), StreamCapture(), time.monotonic())
    assert "secret upstream" not in str(error.value)


def test_sse_total_deadline_is_explicit():
    with pytest.raises(TimeoutError, match="test is closing"):
        consume_sse(sse("[DONE]"), StreamCapture(), time.monotonic() - 5, total_seconds=1)


def test_stream_request_preserves_production_extraction_parameters():
    from app.config import AgentSettings, LLMSettings
    request = project_stream_request(AgentSettings(llm=LLMSettings(enabled=True, provider="openai_compatible", endpoint="http://test/v1/chat/completions", model="test-model")), "测试文章")
    assert set(request.payload) == {"model", "messages", "response_format", "stream", "temperature"}
    assert request.payload["temperature"] == 0.0
    assert request.payload["response_format"] == {"type": "json_object"}
    assert request.payload["stream"] is True


@pytest.mark.skipif(os.environ.get("FINANCE_SYS_STREAM_TEST") != "1", reason="opt-in live relay test; no network/database access by default")
def test_real_relay_stream_completes_and_matches_agent_schema():
    document_id = int(os.environ.get("FINANCE_SYS_STREAM_DOCUMENT_ID", "7674"))
    total_seconds = int(os.environ.get("FINANCE_SYS_STREAM_TOTAL_SECONDS", "600"))
    assert document_id > 0 and 10 <= total_seconds <= 900
    cfg = NacosConfigLoader(NacosBootstrap(server_addr=load_nacos_server_address())).load()
    settings = _settings_from_nacos_config(cfg)
    dbx = shutil.which("dbx")
    assert dbx, "dbx is required for the read-only production document query"
    connection = os.environ.get("FINANCE_SYS_STREAM_DBX_CONNECTION", "home")
    query = "SELECT cleaned_text,chunks_json FROM expert_trade.parse_runs WHERE document_id={} ORDER BY id DESC LIMIT 1".format(document_id)
    result = subprocess.run([dbx, "query", connection, query, "--json"], capture_output=True, text=True, check=True, timeout=30)
    data = json.loads(result.stdout)
    assert not data.get("error") and data.get("rows"), "Cannot read document parse result through dbx"
    row = data["rows"][0]
    chunks = json.loads(row["chunks_json"]) if isinstance(row["chunks_json"], str) else row["chunks_json"]
    text = "\n\n".join(chunk["text"] for chunk in chunks) if chunks else row["cleaned_text"]
    request = project_stream_request(settings, text)
    capture = StreamCapture()
    summary = {"document_id": document_id, "model": settings.llm.model, "stream": True,
               "text_chars": len(text), "system_prompt_chars": len(request.payload["messages"][0]["content"]),
               "total_budget_seconds": total_seconds, "idle_budget_seconds": 60,
               "retries": 0, "writes_recommendations": False, "outcome": "RUNNING"}
    print("STREAM_TEST_START " + json.dumps(summary, ensure_ascii=False), flush=True)
    started = time.monotonic()
    failure = None
    try:
        raw = json.dumps(request.payload, ensure_ascii=False).encode()
        with urlopen(Request(request.url, data=raw, headers=request.headers, method="POST"), timeout=60) as response:
            summary.update(http_status=response.status, content_type=response.headers.get("Content-Type"), headers_after_seconds=round(time.monotonic()-started,3))
            assert "text/event-stream" in (summary["content_type"] or ""), "relay did not return SSE"
            consume_sse(response, capture, started, total_seconds=total_seconds,
                        progress=lambda metrics: print("STREAM_TEST_PROGRESS " + json.dumps(metrics), flush=True))
        parsed = _parse_chat_completion({"choices": [{"message": {"content": capture.content}}]}, 20)
        raw_intents = json.loads(capture.content)["raw_intents"]
        summary.update(outcome="SUCCEEDED", valid_agent_schema=True, parsed_intent_count=len(parsed.raw_intents), raw_intent_count=len(raw_intents))
    except Exception as error:
        failure = error
        summary.update(outcome="FAILED", error_type=type(error).__name__)
        if isinstance(error, HTTPError):
            summary["http_status"] = error.code
        else:
            summary["error"] = redact_secrets(str(error)[:200], [settings.llm.api_key, *settings.llm.extra_headers.values()])
    finally:
        summary.update(capture.metrics())
        summary["elapsed_seconds"] = round(time.monotonic()-started,3)
        output_dir = ROOT / "tmp/diagnostics"
        output_dir.mkdir(parents=True, exist_ok=True)
        report = output_dir / ("llm-stream-test-" + datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%S%fZ") + ".json")
        # Metrics only: no API keys, source text, final content, or reasoning text.
        report.write_text(json.dumps(summary, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        print("STREAM_TEST_RESULT " + json.dumps(summary, ensure_ascii=False), flush=True)
        print("STREAM_TEST_REPORT " + str(report), flush=True)
    if failure:
        pytest.fail("Streaming diagnostic did not complete; see sanitized report above", pytrace=False)


@pytest.mark.skipif(os.environ.get("FINANCE_SYS_STREAM_PRODUCTION_TEST") != "1", reason="opt-in live production-client test")
def test_real_production_streaming_client():
    """Exercise the real client/parser, with the requested pending config in memory."""
    from app.request_budget import RequestBudget, current_budget
    cfg = NacosConfigLoader(NacosBootstrap(server_addr=load_nacos_server_address())).load()
    settings = _settings_from_nacos_config(cfg)
    settings.llm = settings.llm.model_copy(update={"stream": True, "timeout_ms": 1000000, "max_retries": 0})
    document_id = int(os.environ.get("FINANCE_SYS_STREAM_DOCUMENT_ID", "7674"))
    assert document_id > 0
    query = "SELECT cleaned_text,chunks_json FROM expert_trade.parse_runs WHERE document_id={} ORDER BY id DESC LIMIT 1".format(document_id)
    result = subprocess.run([shutil.which("dbx") or "dbx", "query", os.environ.get("FINANCE_SYS_STREAM_DBX_CONNECTION", "home"), query, "--json"], capture_output=True, text=True, check=True, timeout=30)
    data = json.loads(result.stdout)
    assert not data.get("error") and data.get("rows"), "document read failed"
    row = data["rows"][0]
    chunks = json.loads(row["chunks_json"]) if isinstance(row["chunks_json"], str) else row["chunks_json"]
    text = "\n\n".join(chunk["text"] for chunk in chunks) if chunks else row["cleaned_text"]
    print("PRODUCTION_STREAM_TEST_START " + json.dumps({"model": settings.llm.model, "document_id": document_id, "stream": True, "total_seconds": 1000, "text_chars": len(text), "writes_recommendations": False}), flush=True)
    started = time.monotonic()
    budget = RequestBudget(1000)
    token = current_budget.set(budget)
    try:
        extraction = LLMClient(settings).extract_document(text, 20)
    finally:
        budget.cancel()
        current_budget.reset(token)
    summary = {"outcome": "SUCCEEDED", "client": "production", "model": settings.llm.model, "document_id": document_id,
               "stream": True, "total_seconds": 1000, "elapsed_seconds": round(time.monotonic()-started,3),
               "parsed_intent_count": len(extraction.raw_intents), "valid_agent_schema": True, "writes_recommendations": False}
    directory = ROOT / "tmp/diagnostics"
    directory.mkdir(parents=True, exist_ok=True)
    report = directory / ("production-stream-test-" + datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%S%fZ") + ".json")
    report.write_text(json.dumps(summary, indent=2) + "\n", encoding="utf-8")
    print("PRODUCTION_STREAM_TEST_RESULT " + json.dumps(summary), flush=True)
    print("PRODUCTION_STREAM_TEST_REPORT " + str(report), flush=True)
