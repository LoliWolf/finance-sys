import json

import httpx
import pytest

from app.config import AgentSettings, LLMSettings
from app.llm_client import LLMClient


def test_llm_client_extracts_raw_intents_from_chat_completion():
    requests = []

    def handler(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        payload = json.loads(request.content.decode("utf-8"))
        assert payload["model"] == "m4-test-model"
        assert payload["response_format"] == {"type": "json_object"}
        return httpx.Response(
            200,
            json={
                "choices": [
                    {
                        "message": {
                            "content": json.dumps(
                                {
                                    "raw_intents": [
                                        {
                                            "intent_id": "intent-1",
                                            "raw_symbol": "新易盛",
                                            "direction": "LONG",
                                            "reference_price": 88.8,
                                            "reference_price_note": "explicit_price_mention",
                                            "thesis": "source text supports recommendation",
                                            "evidence": [
                                                {"chunk_index": 0, "text": "source evidence"}
                                            ],
                                            "risks": ["volatility"],
                                            "confidence": 0.81,
                                        }
                                    ]
                                }
                            )
                        }
                    }
                ]
            },
        )

    client = LLMClient(_settings(), httpx.Client(transport=httpx.MockTransport(handler)))
    intents = client.extract_raw_intents("推荐新易盛", max_intents=5)

    assert len(intents) == 1
    assert intents[0].raw_symbol == "新易盛"
    assert requests[0].headers["authorization"] == "Bearer test-key"


def test_llm_client_retries_5xx():
    attempts = 0

    def handler(request: httpx.Request) -> httpx.Response:
        nonlocal attempts
        attempts += 1
        if attempts == 1:
            return httpx.Response(500, text="temporary")
        return httpx.Response(
            200,
            json={
                "choices": [
                    {
                        "message": {
                            "content": json.dumps(
                                {
                                    "raw_intents": [
                                        {
                                            "intent_id": "intent-1",
                                            "raw_symbol": "旭创",
                                            "direction": "LONG",
                                            "reference_price": 0,
                                            "reference_price_note": "price_missing_in_text",
                                            "thesis": "source text supports recommendation",
                                            "evidence": [
                                                {"chunk_index": 0, "text": "source evidence"}
                                            ],
                                            "risks": [],
                                            "confidence": 0.7,
                                        }
                                    ]
                                }
                            )
                        }
                    }
                ]
            },
        )

    client = LLMClient(
        _settings(max_retries=1),
        httpx.Client(transport=httpx.MockTransport(handler)),
    )

    intents = client.extract_raw_intents("推荐旭创", max_intents=5)
    assert attempts == 2
    assert intents[0].raw_symbol == "旭创"


def test_llm_client_rejects_invalid_model_output():
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200,
            json={"choices": [{"message": {"content": '{"raw_intents":[{"raw_symbol":"新易盛"}]}'}}]},
        )

    client = LLMClient(_settings(), httpx.Client(transport=httpx.MockTransport(handler)))

    with pytest.raises(Exception):
        client.extract_raw_intents("推荐新易盛", max_intents=5)


def _settings(**overrides):
    values = {
        "enabled": True,
        "endpoint": "https://llm.test/v1/chat/completions",
        "api_key": "test-key",
        "model": "m4-test-model",
        "timeout_ms": 1000,
        "max_retries": 0,
    }
    values.update(overrides)
    return AgentSettings(config_source="test", llm=LLMSettings(**values))
