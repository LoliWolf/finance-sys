import io
import json
import socket
import threading
import time
from http.server import BaseHTTPRequestHandler

import pytest

from app.chat_stream import StreamProtocolError, read_chat_stream
from app.config import AgentSettings, LLMSettings
from app.http_client import StdlibHTTPClient
from app.llm_client import LLMClient
from app.request_budget import RequestBudget, RequestCancelled, current_budget


def wire(events):
    return b"".join(b"data: " + (e.encode() if isinstance(e, str) else json.dumps(e, ensure_ascii=False).encode()) + b"\n\n" for e in events)


def test_production_llm_client_consumes_sse_and_validates_final_json(run_http_server):
    final = json.dumps({"author": "研究员", "raw_intents": []}, ensure_ascii=False)
    body = wire([{"choices": [{"delta": {"reasoning_content": "private reasoning"}}]},
                 {"choices": [{"delta": {"content": final[:8]}}]},
                 {"choices": [{"delta": {"content": final[8:]}, "finish_reason": "stop"}]}, "[DONE]"])
    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *args): pass
        def do_POST(self):
            payload = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
            assert payload["stream"] is True
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.end_headers()
            # Deliberately fragmented TCP writes, not complete JSON chunks.
            for i in range(0, len(body), 7):
                self.wfile.write(body[i:i+7])
            self.wfile.flush()
    endpoint = "http://" + run_http_server(Handler)
    client = LLMClient(AgentSettings(llm=LLMSettings(enabled=True, stream=True, endpoint=endpoint, model="test", timeout_ms=2000)))
    result = client.extract_document("测试文章", 20)
    assert result.raw_intents == []
    assert result.author == "研究员"


@pytest.mark.parametrize("events", [
    [{"choices": [{"delta": {"content": "partial"}}]}],
    [{"choices": [{"delta": {}, "finish_reason": "length"}]}, "[DONE]"],
    [{"error": {"message": "secret"}}],
    ["not-json"],
])
def test_production_stream_rejects_incomplete_and_error_output(events):
    with pytest.raises(StreamProtocolError) as error:
        read_chat_stream(io.BytesIO(wire(events)), time.monotonic()+1, lambda *args: None)
    assert "secret" not in str(error.value)


def test_retry_budget_is_shared(monkeypatch):
    clock = [0.0]
    monkeypatch.setattr("app.llm_client.time.monotonic", lambda: clock[0])
    calls = []
    class Client:
        def post(self, *args, **kwargs):
            from app.http_client import HTTPTimeoutError
            calls.append(kwargs["timeout"])
            clock[0] += 2
            raise HTTPTimeoutError("timed out")
    client = LLMClient(AgentSettings(llm=LLMSettings(enabled=True, endpoint="http://test", model="test", timeout_ms=2000, max_retries=8)), Client(), sleep=lambda _: None)
    with pytest.raises(RuntimeError): client.extract_document("source", 20)
    assert calls == [2.0]


def test_cancellation_wakes_blocked_stream_read(run_http_server):
    ready = threading.Event()
    release = threading.Event()
    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *args): pass
        def do_POST(self):
            self.rfile.read(int(self.headers["Content-Length"]))
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.end_headers()
            self.wfile.write(b": heartbeat\n\n")
            self.wfile.flush()
            ready.set()
            release.wait(3)
    endpoint = "http://" + run_http_server(Handler)
    budget = RequestBudget(1000)
    finished = threading.Event()
    errors = []
    def request():
        token = current_budget.set(budget)
        try: StdlibHTTPClient(1000).post(endpoint, json={"stream": True})
        except Exception as error: errors.append(error)
        finally:
            current_budget.reset(token)
            finished.set()
    thread = threading.Thread(target=request, daemon=True)
    thread.start()
    try:
        assert ready.wait(2)
        budget.cancel()
        assert finished.wait(2), "cancellation did not unblock the socket"
        assert errors
    finally:
        release.set()
        thread.join(2)


def test_cancelled_budget_prevents_new_request():
    budget = RequestBudget(1000)
    budget.cancel()
    token = current_budget.set(budget)
    try:
        with pytest.raises(RequestCancelled):
            StdlibHTTPClient(1000).post("http://must-not-connect", json={})
    finally: current_budget.reset(token)
