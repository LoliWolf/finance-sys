import json
import re
import time
from typing import Any, Callable, Dict, List, Optional

import httpx

from app.config import AgentSettings, get_settings
from app.http_client import HTTPTransportError, StdlibHTTPClient, redact_secrets
from app.schemas import AgentRawIntent
from app.skills import SkillSpec, load_instrument_resolution_skill, render_skill_prompt_block


JSON_FENCE_RE = re.compile(r"^```(?:json)?\s*(.*?)\s*```$", re.DOTALL)
TS_CODE_RE = re.compile(r"(?:\b\d{6}\.(?:SH|SZ|BJ)\b|\bBK\d{4}\.DC\b)")
PROTECTED_EXTRA_HEADER_NAMES = {"authorization", "content-type"}


class LLMClient:
    """OpenAI-compatible model client for raw intent extraction.

    The client is optional at runtime. When the runtime LLM setting is disabled,
    the graph uses deterministic extraction for local tests and smoke runs.
    """

    def __init__(
        self,
        settings: Optional[AgentSettings] = None,
        http_client: Optional[httpx.Client] = None,
        sleep: Optional[Callable[[float], None]] = None,
    ) -> None:
        self.settings = settings or get_settings()
        self.llm = self.settings.llm
        self.http_client = (
            http_client
            if http_client is not None
            else StdlibHTTPClient(timeout=self.llm.timeout_ms / 1000)
        )
        self._sleep = sleep or time.sleep

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

        headers = _request_headers(self.llm.extra_headers, self.llm.api_key)

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
                response_text = response.text.strip()
                if response.status_code < 200 or response.status_code >= 300:
                    safe_response_text = redact_secrets(
                        response_text,
                        [self.llm.api_key, *self.llm.extra_headers.values()],
                    )
                    message = f"llm http {response.status_code}: {safe_response_text}"
                    if _is_provider_moderation_block(response_text):
                        raise _NonRetryableLLMError(message)
                    if response.status_code >= 500:
                        raise _RetryableLLMError(message)
                    raise _NonRetryableLLMError(message)
                try:
                    return _parse_chat_completion(response.json(), max_intents)
                except Exception as exc:
                    raise _RetryableLLMError(f"invalid llm response: {exc}") from exc
            except _NonRetryableLLMError as exc:
                raise RuntimeError(str(exc)) from exc
            except (
                httpx.TimeoutException,
                httpx.TransportError,
                HTTPTransportError,
                _RetryableLLMError,
            ) as exc:
                last_error = exc
                if attempt == attempts:
                    break
                self._sleep(_retry_delay_seconds(attempt))

        raise RuntimeError(f"llm extraction failed after {attempts} attempts: {last_error}")


class _RetryableLLMError(Exception):
    pass


class _NonRetryableLLMError(Exception):
    pass


def _retry_delay_seconds(attempt: int) -> float:
    return min(float(2 ** max(attempt - 1, 0)), 10.0)


def _request_headers(extra_headers: Dict[str, str], api_key: str) -> Dict[str, str]:
    headers = {
        name: value
        for name, value in extra_headers.items()
        if name.strip().lower() not in PROTECTED_EXTRA_HEADER_NAMES
    }
    headers["Content-Type"] = "application/json"
    if api_key:
        headers["Authorization"] = f"Bearer {api_key}"
    return headers


def _is_provider_moderation_block(response_text: str) -> bool:
    normalized = response_text.lower()
    return (
        "smart moderation blocked by" in normalized
        or "moderation_blocked" in normalized
    )


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
    return [AgentRawIntent(**_normalize_raw_intent_item(item)) for item in raw_items[:max_intents]]


def _normalize_raw_intent_item(item: Any) -> Any:
    if not isinstance(item, dict):
        return item
    normalized = dict(item)
    note = str(normalized.get("reference_price_note", "")).strip()
    if note not in {"explicit_price_mention", "price_missing_in_text"}:
        price = _float_or_zero(normalized.get("reference_price"))
        normalized["reference_price_note"] = (
            "explicit_price_mention" if price > 0 else "price_missing_in_text"
        )
    raw_symbol = str(normalized.get("raw_symbol", "")).strip()
    ts_code = TS_CODE_RE.search(raw_symbol)
    if ts_code:
        normalized["raw_symbol"] = ts_code.group(0)
    return normalized


def _float_or_zero(value: Any) -> float:
    try:
        return float(value)
    except (TypeError, ValueError):
        return 0.0


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
