"""OpenAI-compatible SSE reassembly. Never treats a partial stream as a result."""
import json
import time

from app.request_budget import current_budget


class StreamProtocolError(RuntimeError):
    pass


def read_chat_stream(response, deadline, set_timeout):
    content = []
    content_bytes = wire_bytes = 0
    event_lines = []
    finish_reason = None
    while True:
        budget = current_budget.get()
        if budget:
            budget.remaining()
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise TimeoutError("stream total deadline exceeded")
        set_timeout(response, remaining)
        line = response.readline(1024 * 1024 + 1)
        wire_bytes += len(line)
        if len(line) > 1024 * 1024 or wire_bytes > 64 * 1024 * 1024:
            raise StreamProtocolError("stream exceeds safety size limit")
        if not line:
            raise StreamProtocolError("incomplete stream: missing [DONE]")
        try:
            text = line.decode("utf-8").rstrip("\r\n")
        except UnicodeError as exc:
            raise StreamProtocolError("invalid stream encoding") from exc
        if text.startswith("data:"):
            event_lines.append(text[5:].lstrip(" "))
            continue
        if text or not event_lines:
            continue
        data = "\n".join(event_lines)
        event_lines = []
        if data == "[DONE]":
            if finish_reason != "stop":
                raise StreamProtocolError("incomplete model output: finish_reason is not stop")
            return {"choices": [{"message": {"content": "".join(content)}, "finish_reason": finish_reason}]}
        try:
            event = json.loads(data)
            if not isinstance(event, dict) or event.get("error"):
                raise StreamProtocolError("relay stream error event")
            for choice in event.get("choices", []):
                if choice.get("index", 0) != 0:
                    continue
                value = (choice.get("delta") or {}).get("content") or ""
                if not isinstance(value, str):
                    raise StreamProtocolError("non-text stream content")
                content_bytes += len(value.encode("utf-8"))
                if content_bytes > 8 * 1024 * 1024:
                    raise StreamProtocolError("model content exceeds safety size limit")
                content.append(value)
                # reasoning_content deliberately is neither stored nor parsed as JSON.
                if choice.get("finish_reason"):
                    finish_reason = choice["finish_reason"]
        except (ValueError, TypeError, AttributeError) as exc:
            raise StreamProtocolError("invalid stream event") from exc
