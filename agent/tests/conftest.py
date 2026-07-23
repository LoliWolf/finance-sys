from http.server import ThreadingHTTPServer
import os
from threading import Thread

import pytest

from app.config import get_settings


@pytest.fixture
def run_http_server():
    servers = []

    def start(handler):
        server = ThreadingHTTPServer(("127.0.0.1", 0), handler)
        thread = Thread(target=server.serve_forever, daemon=True)
        thread.start()
        servers.append((server, thread))
        return f"127.0.0.1:{server.server_port}"

    yield start

    for server, thread in servers:
        server.shutdown()
        thread.join(timeout=2)
        server.server_close()


@pytest.fixture(autouse=True)
def isolate_runtime_settings(monkeypatch):
    for name in list(os.environ):
        if name.startswith("NACOS_") or name.startswith("AGENT_LLM_"):
            monkeypatch.delenv(name, raising=False)
    get_settings.cache_clear()
    yield
    get_settings.cache_clear()
