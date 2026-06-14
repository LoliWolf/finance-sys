import os

import pytest

from app.config import get_settings


@pytest.fixture(autouse=True)
def isolate_runtime_settings(monkeypatch):
    for name in list(os.environ):
        if name.startswith("NACOS_") or name.startswith("AGENT_LLM_"):
            monkeypatch.delenv(name, raising=False)
    monkeypatch.delenv("AGENT_NACOS_FAIL_FAST", raising=False)
    get_settings.cache_clear()
    yield
    get_settings.cache_clear()
