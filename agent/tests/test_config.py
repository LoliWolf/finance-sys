import json
from http.server import BaseHTTPRequestHandler
from urllib.parse import parse_qs, urlparse

import httpx
import pytest

from app.config import (
    DEFAULT_AGENT_VERSION,
    DEFAULT_NACOS_DATA_ID,
    DEFAULT_NACOS_GROUP,
    DEFAULT_NACOS_NAMESPACE,
    DEFAULT_NACOS_TIMEOUT_MS,
    NacosBootstrap,
    NacosConfigLoader,
    _nacos_bootstrap_available,
    _nacos_bootstrap_from_env,
    _settings_from_nacos_config,
    get_settings,
)


class QuietHTTPHandler(BaseHTTPRequestHandler):
    def log_message(self, format, *args):
        return


def test_settings_from_nacos_config_reuses_llm_and_agent_auth():
    settings = _settings_from_nacos_config(
        {
            "llm": {
                "enabled": True,
                "provider": "openai_compatible",
                "endpoint": "https://llm.test/v1/chat/completions",
                "api_key": "nacos-key",
                "model": "nacos-model",
                "timeout_ms": 20000,
                "max_retries": 2,
                "extra_headers": {
                    "X-Client-Name": "finance-sys-agent",
                    "X-Request-Source": "nacos-config-test",
                },
            },
            "agent": {
                "internal_api_base_url": "http://go.test",
                "tushare": {
                    "enabled": True,
                    "token": "tushare-token",
                    "endpoint": "https://api.tushare.pro",
                    "timeout_ms": 4000,
                },
                "auth": {
                    "enabled": True,
                    "header_name": "X-Agent-Token",
                    "static_token": "agent-token",
                }
            },
            "security": {
                "auth": {
                    "enabled": True,
                    "header_name": "X-Internal-Token",
                    "token_prefix": "Bearer ",
                    "static_tokens": ["go-token"],
                }
            },
        }
    )

    assert settings.config_source == "nacos"
    assert settings.auth_enabled is True
    assert settings.auth_header == "X-Agent-Token"
    assert settings.auth_token == "agent-token"
    assert settings.internal_api.base_url == "http://go.test"
    assert settings.internal_api.auth_header == "X-Internal-Token"
    assert settings.internal_api.auth_token == "Bearer go-token"
    assert settings.tushare.enabled is True
    assert settings.tushare.token == "tushare-token"
    assert settings.tushare.endpoint == "https://api.tushare.pro"
    assert settings.tushare.timeout_ms == 4000
    assert settings.llm.enabled is True
    assert settings.llm.endpoint == "https://llm.test/v1/chat/completions"
    assert settings.llm.api_key == "nacos-key"
    assert settings.llm.model == "nacos-model"
    assert settings.llm.timeout_ms == 20000
    assert settings.llm.max_retries == 2
    assert settings.llm.extra_headers == {
        "X-Client-Name": "finance-sys-agent",
        "X-Request-Source": "nacos-config-test",
    }


def test_settings_from_nacos_config_ignores_all_runtime_env_overrides(monkeypatch):
    monkeypatch.setenv("AGENT_VERSION", "env-agent-version")
    monkeypatch.setenv("AGENT_AUTH_ENABLED", "false")
    monkeypatch.setenv("AGENT_AUTH_TOKEN", "env-agent-token")
    monkeypatch.setenv("AGENT_INTERNAL_API_BASE_URL", "http://env-internal.test")
    monkeypatch.setenv("AGENT_INTERNAL_API_TIMEOUT_MS", "90000")
    monkeypatch.setenv("AGENT_INTERNAL_API_MAX_CANDIDATES", "20")
    monkeypatch.setenv("AGENT_LLM_ENDPOINT", "https://env-llm.test")
    monkeypatch.setenv("AGENT_LLM_API_KEY", "env-llm-key")
    monkeypatch.setenv("AGENT_LLM_MODEL", "env-model")
    monkeypatch.setenv("AGENT_LLM_TIMEOUT_MS", "90000")
    monkeypatch.setenv("AGENT_LLM_MAX_RETRIES", "0")
    monkeypatch.setenv(
        "AGENT_LLM_EXTRA_HEADERS",
        json.dumps({"X-Request-Source": "env-source"}),
    )
    monkeypatch.setenv("TUSHARE_TOKEN", "env-tushare-token")

    settings = _settings_from_nacos_config(
        {
            "agent": {
                "internal_api_base_url": "http://nacos-internal.test",
                "auth": {
                    "enabled": True,
                    "header_name": "X-Agent-Token",
                    "static_token": "nacos-agent-token",
                },
                "tushare": {
                    "enabled": True,
                    "token": "nacos-tushare-token",
                },
            },
            "llm": {
                "enabled": True,
                "provider": "openai_compatible",
                "endpoint": "https://nacos-llm.test/v1/chat/completions",
                "api_key": "nacos-key",
                "model": "nacos-model",
                "timeout_ms": 300000,
                "max_retries": 3,
                "extra_headers": {"X-Request-Source": "nacos-source"},
            }
        }
    )

    assert settings.agent_version == DEFAULT_AGENT_VERSION
    assert settings.auth_enabled is True
    assert settings.auth_token == "nacos-agent-token"
    assert settings.internal_api.base_url == "http://nacos-internal.test"
    assert settings.internal_api.timeout_ms == 3000
    assert settings.internal_api.max_candidates == 5
    assert settings.tushare.token == "nacos-tushare-token"
    assert settings.llm.endpoint == "https://nacos-llm.test/v1/chat/completions"
    assert settings.llm.api_key == "nacos-key"
    assert settings.llm.model == "nacos-model"
    assert settings.llm.timeout_ms == 300000
    assert settings.llm.max_retries == 3
    assert settings.llm.extra_headers == {"X-Request-Source": "nacos-source"}


def test_nacos_loader_fetches_same_json_config():
    requests = []

    def handler(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        assert request.url.path == "/nacos/v1/cs/configs"
        assert request.url.params["dataId"] == "expert_trade"
        assert request.url.params["group"] == "DEFAULT_GROUP"
        assert request.url.params["tenant"] == "public"
        return httpx.Response(
            200,
            text=json.dumps(
                {
                    "llm": {
                        "enabled": True,
                        "provider": "openai_compatible",
                        "endpoint": "https://llm.test/v1/chat/completions",
                        "api_key": "",
                        "model": "nacos-model",
                        "timeout_ms": 20000,
                        "max_retries": 2,
                    }
                }
            ),
        )

    loader = NacosConfigLoader(
        NacosBootstrap(
            server_addr="127.0.0.1:8848",
            namespace="public",
            group="DEFAULT_GROUP",
            data_id="expert_trade",
        ),
        httpx.Client(transport=httpx.MockTransport(handler)),
    )

    config = loader.load()
    assert config["llm"]["model"] == "nacos-model"
    assert len(requests) == 1


def test_nacos_stdlib_transport_gets_config_with_encoded_query(run_http_server):
    observed = {}

    class Handler(QuietHTTPHandler):
        def do_GET(self):
            parsed = urlparse(self.path)
            observed["path"] = parsed.path
            observed["query"] = parse_qs(parsed.query)
            body = json.dumps({"agent": {"internal_api_base_url": "http://go.test"}}).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json; charset=utf-8")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

    loader = NacosConfigLoader(
        NacosBootstrap(
            server_addr=run_http_server(Handler),
            namespace="租户 /+",
            group="研究组 &=",
            data_id="专家 配置/+",
            timeout_ms=1000,
        )
    )

    config = loader.load()

    assert config["agent"]["internal_api_base_url"] == "http://go.test"
    assert observed == {
        "path": "/nacos/v1/cs/configs",
        "query": {
            "dataId": ["专家 配置/+"],
            "group": ["研究组 &="],
            "tenant": ["租户 /+"],
        },
    }


def test_nacos_stdlib_transport_logs_in_and_encodes_form(run_http_server):
    observed = {"login": None, "config": None}

    class Handler(QuietHTTPHandler):
        def do_POST(self):
            length = int(self.headers["Content-Length"])
            body = self.rfile.read(length).decode("utf-8")
            observed["login"] = parse_qs(body)
            response = json.dumps({"accessToken": "token +/&="}).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(response)))
            self.end_headers()
            self.wfile.write(response)

        def do_GET(self):
            observed["config"] = parse_qs(urlparse(self.path).query)
            response = b"{}"
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(response)))
            self.end_headers()
            self.wfile.write(response)

    loader = NacosConfigLoader(
        NacosBootstrap(
            server_addr=run_http_server(Handler),
            username="用户 +&=",
            password="密码 /+&=",
            timeout_ms=1000,
        )
    )

    assert loader.load() == {}
    assert observed["login"] == {
        "username": ["用户 +&="],
        "password": ["密码 /+&="],
    }
    assert observed["config"]["accessToken"] == ["token +/&="]


def test_nacos_stdlib_transport_reports_http_error(run_http_server):
    class Handler(QuietHTTPHandler):
        def do_GET(self):
            response = b"temporarily unavailable"
            self.send_response(503, "Service Unavailable")
            self.send_header("Content-Type", "text/plain; charset=utf-8")
            self.send_header("Content-Length", str(len(response)))
            self.end_headers()
            self.wfile.write(response)

    loader = NacosConfigLoader(
        NacosBootstrap(
            server_addr=run_http_server(Handler),
            timeout_ms=1000,
        )
    )

    with pytest.raises(
        RuntimeError,
        match=r"nacos HTTP 503 Service Unavailable: temporarily unavailable",
    ):
        loader.load()


def test_nacos_stdlib_transport_converts_timeout_milliseconds(monkeypatch):
    observed = {}

    class Headers:
        @staticmethod
        def get_content_charset():
            return "utf-8"

    class Response:
        headers = Headers()

        def __init__(self):
            self._consumed = False

        def __enter__(self):
            return self

        def __exit__(self, exc_type, exc_value, traceback):
            return False

        def read(self, _size=-1):
            if self._consumed:
                return b""
            self._consumed = True
            return b"{}"

    def fake_urlopen(request, timeout):
        observed["method"] = request.get_method()
        observed["timeout"] = timeout
        return Response()

    monkeypatch.setattr("app.config.open_url", fake_urlopen)
    loader = NacosConfigLoader(
        NacosBootstrap(
            server_addr="127.0.0.1:8848",
            timeout_ms=1250,
        )
    )

    assert loader.load() == {}
    assert observed == {"method": "GET", "timeout": 1.25}


def test_nacos_bootstrap_only_requires_server_address(monkeypatch):
    monkeypatch.setenv("NACOS_SERVER_ADDR", " 127.0.0.1:8848 ")

    assert _nacos_bootstrap_available() is True

    bootstrap = _nacos_bootstrap_from_env()
    assert bootstrap.server_addr == "127.0.0.1:8848"
    assert bootstrap.namespace == DEFAULT_NACOS_NAMESPACE
    assert bootstrap.group == DEFAULT_NACOS_GROUP
    assert bootstrap.data_id == DEFAULT_NACOS_DATA_ID
    assert bootstrap.username == ""
    assert bootstrap.password == ""


def test_nacos_bootstrap_ignores_every_env_value_except_server_address(monkeypatch):
    monkeypatch.setenv("NACOS_SERVER_ADDR", "127.0.0.1:8848")
    monkeypatch.setenv("NACOS_NAMESPACE", "env-namespace")
    monkeypatch.setenv("NACOS_GROUP", "ENV_GROUP")
    monkeypatch.setenv("NACOS_DATA_ID", "env-data-id")
    monkeypatch.setenv("NACOS_USERNAME", "env-user")
    monkeypatch.setenv("NACOS_PASSWORD", "env-password")
    monkeypatch.setenv("NACOS_TIMEOUT_MS", "99999")

    bootstrap = _nacos_bootstrap_from_env()

    assert bootstrap.namespace == DEFAULT_NACOS_NAMESPACE
    assert bootstrap.group == DEFAULT_NACOS_GROUP
    assert bootstrap.data_id == DEFAULT_NACOS_DATA_ID
    assert bootstrap.username == ""
    assert bootstrap.password == ""
    assert bootstrap.timeout_ms == DEFAULT_NACOS_TIMEOUT_MS


def test_nacos_bootstrap_is_disabled_without_server_address(monkeypatch):
    monkeypatch.delenv("NACOS_SERVER_ADDR", raising=False)
    assert _nacos_bootstrap_available() is False

    monkeypatch.setenv("NACOS_SERVER_ADDR", "  ")
    assert _nacos_bootstrap_available() is False


def test_get_settings_loads_nacos_with_only_server_address(monkeypatch):
    monkeypatch.setenv("NACOS_SERVER_ADDR", "127.0.0.1:8848")
    monkeypatch.setattr(NacosConfigLoader, "load", lambda self: {})

    settings = get_settings()

    assert settings.config_source == "nacos"


def test_get_settings_never_falls_back_when_nacos_load_fails(monkeypatch):
    monkeypatch.setenv("NACOS_SERVER_ADDR", "127.0.0.1:8848")
    monkeypatch.setenv("AGENT_NACOS_FAIL_FAST", "false")
    monkeypatch.setenv("AGENT_LLM_ENABLED", "false")

    def fail_load(self):
        raise RuntimeError("nacos unavailable")

    monkeypatch.setattr(NacosConfigLoader, "load", fail_load)

    with pytest.raises(RuntimeError, match="nacos unavailable"):
        get_settings()
