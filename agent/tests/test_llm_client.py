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
        sleep=lambda _: None,
    )

    intents = client.extract_raw_intents("推荐旭创", max_intents=5)
    assert attempts == 2
    assert intents[0].raw_symbol == "旭创"


def test_llm_client_does_not_retry_provider_moderation_block():
    attempts = 0

    def handler(request: httpx.Request) -> httpx.Response:
        nonlocal attempts
        attempts += 1
        return httpx.Response(
            500,
            text='{"error":{"message":"Smart moderation blocked by ai"}}',
        )

    client = LLMClient(
        _settings(max_retries=2),
        httpx.Client(transport=httpx.MockTransport(handler)),
    )

    with pytest.raises(RuntimeError, match="Smart moderation blocked by ai"):
        client.extract_raw_intents("text", max_intents=5)

    assert attempts == 1


def test_llm_client_retries_invalid_model_output():
    attempts = 0

    def handler(request: httpx.Request) -> httpx.Response:
        nonlocal attempts
        attempts += 1
        if attempts == 1:
            return httpx.Response(
                200,
                json={
                    "choices": [
                        {
                            "message": {
                                "content": '{"raw_intents":[]}\n{"extra":true}',
                            }
                        }
                    ]
                },
            )
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
                                            "raw_symbol": "300502.SZ",
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
        sleep=lambda _: None,
    )

    intents = client.extract_raw_intents("recommend 300502.SZ", max_intents=5)

    assert attempts == 2
    assert intents[0].raw_symbol == "300502.SZ"


def test_llm_client_sends_configured_extra_headers():
    requests = []

    def handler(request: httpx.Request) -> httpx.Response:
        requests.append(request)
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
                                            "raw_symbol": "300502.SZ",
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
        _settings(
            extra_headers={
                "X-Client-Name": "finance-sys-agent",
                "X-Request-Source": "m9-real-history",
                "Accept-Language": "zh-CN",
            }
        ),
        httpx.Client(transport=httpx.MockTransport(handler)),
    )

    intents = client.extract_raw_intents("recommend 300502.SZ", max_intents=5)

    assert intents[0].raw_symbol == "300502.SZ"
    assert requests[0].headers["x-client-name"] == "finance-sys-agent"
    assert requests[0].headers["x-request-source"] == "m9-real-history"
    assert requests[0].headers["accept-language"] == "zh-CN"
    assert requests[0].headers["authorization"] == "Bearer test-key"


def test_llm_client_extra_headers_cannot_override_auth_or_content_type():
    requests = []

    def handler(request: httpx.Request) -> httpx.Response:
        requests.append(request)
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
                                            "raw_symbol": "300502.SZ",
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
        _settings(
            extra_headers={
                "Authorization": "Bearer wrong-key",
                "Content-Type": "text/plain",
                "X-Client-Name": "finance-sys-agent",
            }
        ),
        httpx.Client(transport=httpx.MockTransport(handler)),
    )

    client.extract_raw_intents("recommend 300502.SZ", max_intents=5)

    assert requests[0].headers["authorization"] == "Bearer test-key"
    assert requests[0].headers["content-type"] == "application/json"
    assert requests[0].headers["x-client-name"] == "finance-sys-agent"


def test_llm_client_normalizes_blank_reference_price_note():
    def handler(request: httpx.Request) -> httpx.Response:
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
                                            "raw_symbol": "300502.SZ",
                                            "direction": "LONG",
                                            "reference_price": 88.8,
                                            "reference_price_note": "",
                                            "thesis": "source text supports recommendation",
                                            "evidence": [
                                                {"chunk_index": 0, "text": "source evidence"}
                                            ],
                                            "risks": [],
                                            "confidence": 0.8,
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

    intents = client.extract_raw_intents("recommend 300502.SZ reference price 88.8", max_intents=5)

    assert intents[0].reference_price_note == "explicit_price_mention"


def test_llm_client_normalizes_unknown_reference_price_note():
    def handler(request: httpx.Request) -> httpx.Response:
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
                                            "raw_symbol": "300308.SZ",
                                            "direction": "LONG",
                                            "reference_price": 0,
                                            "reference_price_note": "text_specified",
                                            "thesis": "source text supports recommendation",
                                            "evidence": [
                                                {"chunk_index": 0, "text": "source evidence"}
                                            ],
                                            "risks": [],
                                            "confidence": 0.8,
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

    intents = client.extract_raw_intents("recommend 300308.SZ", max_intents=5)

    assert intents[0].reference_price_note == "price_missing_in_text"


def test_llm_client_normalizes_raw_symbol_with_ts_code_and_name():
    def handler(request: httpx.Request) -> httpx.Response:
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
                                            "raw_symbol": "300502.SZ 新易盛",
                                            "direction": "LONG",
                                            "reference_price": 88.8,
                                            "reference_price_note": "explicit_price_mention",
                                            "thesis": "source text supports recommendation",
                                            "evidence": [
                                                {"chunk_index": 0, "text": "source evidence"}
                                            ],
                                            "risks": [],
                                            "confidence": 0.8,
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

    intents = client.extract_raw_intents("recommend 300502.SZ 新易盛", max_intents=5)

    assert intents[0].raw_symbol == "300502.SZ"


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
