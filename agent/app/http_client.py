from __future__ import annotations

from dataclasses import dataclass
import http.client
import json as jsonlib
import socket
import time
from typing import Any, Dict, Iterable, Mapping, Optional
from urllib.error import HTTPError, URLError
from urllib.parse import urlencode
from urllib.request import Request, urlopen


class HTTPClientError(RuntimeError):
    """Base error that never includes request headers or request bodies."""


class HTTPTransportError(HTTPClientError):
    pass


class HTTPTimeoutError(HTTPTransportError):
    pass


class HTTPStatusError(HTTPClientError):
    def __init__(self, status_code: int, reason: str = "") -> None:
        message = f"HTTP {status_code}"
        if reason:
            message += f" {reason}"
        super().__init__(message)
        self.status_code = status_code


@dataclass(frozen=True)
class StdlibHTTPResponse:
    status_code: int
    text: str
    reason: str = ""

    def json(self) -> Any:
        return jsonlib.loads(self.text)

    def raise_for_status(self) -> None:
        if self.status_code < 200 or self.status_code >= 300:
            raise HTTPStatusError(self.status_code, self.reason)


def open_url(request: Request, timeout: float):
    return urlopen(request, timeout=timeout)


class StdlibHTTPClient:
    """Small urllib-backed client for JSON/form HTTP calls.

    Transport errors intentionally omit the URL, headers, and body so bearer
    tokens and API tokens cannot be exposed through warnings or exceptions.
    """

    def __init__(self, timeout: float) -> None:
        self.timeout = timeout

    def post(
        self,
        url: str,
        *,
        json: Optional[Any] = None,
        data: Optional[Mapping[str, Any]] = None,
        headers: Optional[Mapping[str, str]] = None,
        timeout: Optional[float] = None,
    ) -> StdlibHTTPResponse:
        if json is not None and data is not None:
            raise ValueError("json and data cannot both be provided")

        request_headers: Dict[str, str] = dict(headers or {})
        if json is not None:
            body = jsonlib.dumps(json, ensure_ascii=False).encode("utf-8")
            _set_default_header(request_headers, "Content-Type", "application/json")
        elif data is not None:
            body = urlencode(data).encode("utf-8")
            _set_default_header(
                request_headers,
                "Content-Type",
                "application/x-www-form-urlencoded; charset=utf-8",
            )
        else:
            body = b""

        request = Request(url, data=body, headers=request_headers, method="POST")
        timeout_seconds = self.timeout if timeout is None else timeout
        deadline = time.monotonic() + timeout_seconds
        try:
            with open_url(request, timeout=timeout_seconds) as response:
                return _response_from_urlopen(response, deadline)
        except HTTPError as exc:
            return StdlibHTTPResponse(
                status_code=exc.code,
                text=_decode_body(_read_body_until(exc, deadline), exc.headers),
                reason=str(exc.reason or ""),
            )
        except (TimeoutError, socket.timeout) as exc:
            raise HTTPTimeoutError("HTTP request timed out") from exc
        except URLError as exc:
            if isinstance(exc.reason, (TimeoutError, socket.timeout)):
                raise HTTPTimeoutError("HTTP request timed out") from exc
            raise HTTPTransportError("HTTP transport failed") from exc
        except (OSError, http.client.HTTPException) as exc:
            raise HTTPTransportError("HTTP transport failed") from exc


def redact_secret(value: str, secret: str) -> str:
    if not secret:
        return value
    return value.replace(secret, "[REDACTED]")


def redact_secrets(value: str, secrets: Iterable[str]) -> str:
    redacted = value
    for secret in secrets:
        redacted = redact_secret(redacted, secret)
    return redacted


def read_url_response_body(response: Any, timeout: float) -> bytes:
    return _read_body_until(response, time.monotonic() + timeout)


def _response_from_urlopen(response: Any, deadline: float) -> StdlibHTTPResponse:
    status_code = int(getattr(response, "status", None) or response.getcode())
    return StdlibHTTPResponse(
        status_code=status_code,
        text=_decode_body(_read_body_until(response, deadline), response.headers),
        reason=str(getattr(response, "reason", "") or ""),
    )


def _read_body_until(response: Any, deadline: float) -> bytes:
    chunks = []
    reader = getattr(response, "read1", None) or response.read
    while True:
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise HTTPTimeoutError("HTTP request timed out")
        _set_response_timeout(response, remaining)
        try:
            chunk = reader(64 * 1024)
        except (TimeoutError, socket.timeout) as exc:
            raise HTTPTimeoutError("HTTP request timed out") from exc
        if not chunk:
            return b"".join(chunks)
        chunks.append(chunk)


def _set_response_timeout(response: Any, timeout: float) -> None:
    candidates = [response, getattr(response, "fp", None)]
    fp = getattr(response, "fp", None)
    if fp is not None:
        candidates.append(getattr(fp, "raw", None))
        raw = getattr(fp, "raw", None)
        if raw is not None:
            candidates.append(getattr(raw, "_sock", None))
    for candidate in candidates:
        setter = getattr(candidate, "settimeout", None)
        if setter is not None:
            setter(timeout)
            return


def _decode_body(body: bytes, headers: Any) -> str:
    charset = headers.get_content_charset() if headers is not None else None
    try:
        return body.decode(charset or "utf-8")
    except (LookupError, UnicodeDecodeError):
        return body.decode("utf-8", errors="replace")


def _set_default_header(headers: Dict[str, str], name: str, value: str) -> None:
    if not any(existing.lower() == name.lower() for existing in headers):
        headers[name] = value
