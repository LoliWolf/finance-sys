import json
import re
from typing import Any, Dict, List, Optional

import httpx

from app.config import AgentSettings, get_settings
from app.schemas import AgentRawIntent
from app.skills import SkillSpec, load_instrument_resolution_skill, render_skill_prompt_block


JSON_FENCE_RE = re.compile(r"^```(?:json)?\s*(.*?)\s*```$", re.DOTALL)


class LLMClient:
    """OpenAI-compatible model client for raw intent extraction.

    The client is optional at runtime. When AGENT_LLM_ENABLED is false, the graph
    uses deterministic extraction for local tests and smoke runs.
    """

    def __init__(
        self,
        settings: Optional[AgentSettings] = None,
        http_client: Optional[httpx.Client] = None,
    ) -> None:
        self.settings = settings or get_settings()
        self.llm = self.settings.llm
        self.http_client = http_client or httpx.Client(
            timeout=self.llm.timeout_ms / 1000
        )

    def enabled(self) -> bool:
        return self.llm.enabled

    def extract_raw_intents(
        self,
        text: str,
        max_intents: int,
        skill: Optional[SkillSpec] = None,
    ) -> List[AgentRawIntent]:
        if not self.llm.enabled:
            raise RuntimeError("LLM extraction is disabled")
        if self.llm.provider != "openai_compatible":
            raise RuntimeError("llm.provider must be openai_compatible")
        if not self.llm.endpoint.strip():
            raise RuntimeError("llm.endpoint is required when LLM extraction is enabled")
        if not self.llm.model.strip():
            raise RuntimeError("llm.model is required when LLM extraction is enabled")

        skill = skill or load_instrument_resolution_skill()
        payload = {
            "model": self.llm.model,
            "response_format": {"type": "json_object"},
            "messages": [
                {
                    "role": "system",
                    "content": build_system_prompt(skill),
                },
                {
                    "role": "user",
                    "content": (
                        "Return this shape: "
                        '{"raw_intents":[{"intent_id":"intent-1","raw_symbol":"新易盛",'
                        '"direction":"LONG","reference_price":0,'
                        '"reference_price_note":"price_missing_in_text","thesis":"...",'
                        '"evidence":[{"chunk_index":0,"text":"..."}],"risks":[],"confidence":0.8}]}\n\n'
                        f"Source text:\n{text}"
                    ),
                },
            ],
        }

        headers = {"Content-Type": "application/json"}
        if self.llm.api_key:
            headers["Authorization"] = f"Bearer {self.llm.api_key}"

        attempts = self.llm.max_retries + 1
        last_error: Optional[Exception] = None
        for attempt in range(1, attempts + 1):
            try:
                response = self.http_client.post(
                    self.llm.endpoint,
                    json=payload,
                    headers=headers,
                    timeout=self.llm.timeout_ms / 1000,
                )
                if response.status_code >= 500:
                    raise _RetryableLLMError(
                        f"llm http {response.status_code}: {response.text.strip()}"
                    )
                if response.status_code >= 400:
                    raise RuntimeError(
                        f"llm http {response.status_code}: {response.text.strip()}"
                    )
                return _parse_chat_completion(response.json(), max_intents)
            except (httpx.TimeoutException, httpx.TransportError, _RetryableLLMError) as exc:
                last_error = exc
                if attempt == attempts:
                    break
            except Exception:
                raise

        raise RuntimeError(f"llm extraction failed after {attempts} attempts: {last_error}")


class _RetryableLLMError(Exception):
    pass


def build_system_prompt(skill: SkillSpec) -> str:
    return (
        "Extract Chinese equity trading intents from research text. "
        "Return JSON only. Do not invent ts_code. Do not output entry_price, "
        "stop_loss, take_profit, or position size.\n\n"
        "The following project-local rules are trusted system instructions:\n"
        f"{render_skill_prompt_block(skill)}"
    )


def _parse_chat_completion(payload: Dict[str, Any], max_intents: int) -> List[AgentRawIntent]:
    choices = payload.get("choices")
    if not isinstance(choices, list) or not choices:
        raise RuntimeError("llm response missing choices")
    message = choices[0].get("message", {})
    if not isinstance(message, dict):
        raise RuntimeError("llm response missing message")
    content = _message_content_to_text(message.get("content"))
    data = json.loads(_strip_json_fence(content))
    raw_items = data.get("raw_intents")
    if not isinstance(raw_items, list):
        raise RuntimeError("llm response missing raw_intents")
    return [AgentRawIntent(**item) for item in raw_items[:max_intents]]


def _message_content_to_text(content: Any) -> str:
    if isinstance(content, str):
        return content.strip()
    if isinstance(content, list):
        parts = []
        for item in content:
            if isinstance(item, dict) and isinstance(item.get("text"), str):
                parts.append(item["text"])
        text = "".join(parts).strip()
        if text:
            return text
    raise RuntimeError("unsupported llm message content")


def _strip_json_fence(content: str) -> str:
    match = JSON_FENCE_RE.match(content.strip())
    if match:
        return match.group(1).strip()
    return content.strip()
