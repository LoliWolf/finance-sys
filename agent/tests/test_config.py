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
            },
            "agent": {
                "auth": {
                    "enabled": True,
                    "header_name": "X-Agent-Token",
                    "static_token": "agent-token",
                }
            },
        }
    )

    assert settings.config_source == "nacos"
    assert settings.auth_enabled is True
    assert settings.auth_header == "X-Agent-Token"
    assert settings.auth_token == "agent-token"
    assert settings.llm.enabled is True
    assert settings.llm.endpoint == "https://llm.test/v1/chat/completions"
    assert settings.llm.api_key == "nacos-key"
    assert settings.llm.model == "nacos-model"
    assert settings.llm.timeout_ms == 20000
    assert settings.llm.max_retries == 2


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
