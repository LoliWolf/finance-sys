import json
from pathlib import Path

import httpx

from app.config import AgentSettings, LLMSettings
from app.llm_client import LLMClient, build_system_prompt
from app.skills import load_skill_from_path


VALID_SKILL = """---
name: instrument_resolution
version: instrument-resolution-m5-v1
description: Rules for Chinese A-share instrument resolution.
---

# Instrument Resolution Rules 标的解析规则

1. 不允许编造 `ts_code`。
"""


def test_build_system_prompt_injects_skill_content_and_hash(tmp_path: Path):
    skill = _write_skill(tmp_path)
    prompt = build_system_prompt(skill)

    assert "<instrument_resolution_skill" in prompt
    assert "system instructions" in prompt
    assert "不允许编造" in prompt
    assert skill.version in prompt
    assert skill.skill_hash in prompt


def test_llm_client_sends_skill_prompt_in_system_message(tmp_path: Path):
    skill = _write_skill(tmp_path)
    requests = []

    def handler(request: httpx.Request) -> httpx.Response:
        requests.append(json.loads(request.content.decode("utf-8")))
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
                                            "reference_price": 0,
                                            "reference_price_note": "price_missing_in_text",
                                            "thesis": "source text supports recommendation",
                                            "evidence": [{"chunk_index": 0, "text": "推荐新易盛"}],
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

    client = LLMClient(
        _settings(),
        httpx.Client(transport=httpx.MockTransport(handler)),
    )
    intents = client.extract_raw_intents("推荐新易盛", max_intents=5, skill=skill)

    assert intents[0].raw_symbol == "新易盛"
    system_message = requests[0]["messages"][0]
    user_message = requests[0]["messages"][1]
    assert system_message["role"] == "system"
    assert skill.skill_hash in system_message["content"]
    assert "不允许编造" in system_message["content"]
    assert "推荐新易盛" not in system_message["content"]
    assert "推荐新易盛" in user_message["content"]


def _write_skill(tmp_path: Path):
    root = tmp_path / "skills"
    skill_dir = root / "instrument_resolution"
    skill_dir.mkdir(parents=True)
    skill_file = skill_dir / "SKILL.md"
    skill_file.write_text(VALID_SKILL, encoding="utf-8")
    return load_skill_from_path(skill_file, root)


def _settings():
    return AgentSettings(
        config_source="test",
        llm=LLMSettings(
            enabled=True,
            endpoint="https://llm.test/v1/chat/completions",
            api_key="test-key",
            model="m5-test-model",
            timeout_ms=1000,
            max_retries=0,
        ),
    )
