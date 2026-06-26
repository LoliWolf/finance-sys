import json

import httpx

from app.config import (
    NacosBootstrap,
    NacosConfigLoader,
    _settings_from_nacos_config,
)


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


def test_settings_from_nacos_config_merges_env_llm_extra_headers(monkeypatch):
    monkeypatch.setenv(
        "AGENT_LLM_EXTRA_HEADERS",
        json.dumps(
            {
                "X-Client-Name": "finance-sys-agent",
                "X-Request-Source": "m9-real-history",
            }
        ),
    )

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
                    "X-Client-Name": "finance-sys-agent-from-nacos",
                    "X-Nacos-Only": "kept",
                },
            }
        }
    )

    assert settings.llm.extra_headers == {
        "X-Client-Name": "finance-sys-agent",
        "X-Nacos-Only": "kept",
        "X-Request-Source": "m9-real-history",
    }


def test_settings_from_nacos_config_allows_env_llm_timeout_and_retry_overrides(monkeypatch):
    monkeypatch.setenv("AGENT_LLM_TIMEOUT_MS", "90000")
    monkeypatch.setenv("AGENT_LLM_MAX_RETRIES", "0")

    settings = _settings_from_nacos_config(
        {
            "llm": {
                "enabled": True,
                "provider": "openai_compatible",
                "endpoint": "https://llm.test/v1/chat/completions",
                "api_key": "nacos-key",
                "model": "nacos-model",
                "timeout_ms": 300000,
                "max_retries": 3,
            }
        }
    )

    assert settings.llm.timeout_ms == 90000
    assert settings.llm.max_retries == 0


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
