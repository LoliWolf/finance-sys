from dataclasses import dataclass
import json
import logging
import os
import threading
import time
from typing import Any, Dict, List, Optional
from urllib.error import HTTPError, URLError
from urllib.parse import urlencode, urlparse, urlunparse
from urllib.request import Request

import httpx
from pydantic import BaseModel, Field

from app.http_client import open_url, read_url_response_body


DEFAULT_NACOS_NAMESPACE = "public"
DEFAULT_NACOS_GROUP = "DEFAULT_GROUP"
DEFAULT_NACOS_DATA_ID = "expert_trade"
DEFAULT_NACOS_TIMEOUT_MS = 5000
DEFAULT_AGENT_VERSION = "m4-agent-0.1.2"
FINANCE_SYS_ENV = "FINANCE_SYS_ENV"
PRODUCTION_ENVIRONMENT = "PROD"


class LLMSettings(BaseModel):
    enabled: bool = Field(default=False)
    provider: str = Field(default="openai_compatible")
    endpoint: str = Field(default="")
    api_key: str = Field(default="")
    model: str = Field(default="")
    temperature: float = Field(default=0.0, ge=0, le=1, allow_inf_nan=False)
    extraction_prompt: str = Field(default="", max_length=16000)
    stream: bool = Field(default=False)
    timeout_ms: int = Field(default=15000, gt=0)
    max_retries: int = Field(default=1, ge=0)
    extra_headers: Dict[str, str] = Field(default_factory=dict)


class InternalAPISettings(BaseModel):
    base_url: str = Field(default="")
    auth_header: str = Field(default="")
    auth_token: str = Field(default="")
    timeout_ms: int = Field(default=3000, gt=0)
    max_candidates: int = Field(default=5, ge=1, le=20)


class TushareSettings(BaseModel):
    enabled: bool = Field(default=False)
    token: str = Field(default="")
    endpoint: str = Field(default="https://api.tushare.pro")
    timeout_ms: int = Field(default=3000, gt=0)

class AgentSettings(BaseModel):
    agent_version: str = Field(default=DEFAULT_AGENT_VERSION)
    config_source: str = Field(default="env")
    auth_enabled: bool = Field(default=False)
    auth_header: str = Field(default="X-Agent-Token")
    auth_token: str = Field(default="")
    internal_api: InternalAPISettings = Field(default_factory=InternalAPISettings)
    tushare: TushareSettings = Field(default_factory=TushareSettings)
    llm: LLMSettings = Field(default_factory=LLMSettings)
    request_timeout_ms: int = Field(default=1000000, gt=0)
    processing_max_concurrency: int = Field(default=20, ge=1, le=1024)


@dataclass(frozen=True)
class NacosBootstrap:
    server_addr: str
    namespace: str = DEFAULT_NACOS_NAMESPACE
    group: str = DEFAULT_NACOS_GROUP
    data_id: str = DEFAULT_NACOS_DATA_ID
    username: str = ""
    password: str = ""
    timeout_ms: int = DEFAULT_NACOS_TIMEOUT_MS

    def __post_init__(self) -> None:
        object.__setattr__(self, "server_addr", self.server_addr.strip())
        object.__setattr__(
            self,
            "namespace",
            self.namespace.strip() or DEFAULT_NACOS_NAMESPACE,
        )
        object.__setattr__(self, "group", self.group.strip() or DEFAULT_NACOS_GROUP)
        object.__setattr__(self, "data_id", self.data_id.strip() or DEFAULT_NACOS_DATA_ID)


class NacosConfigLoader:
    def __init__(
        self,
        bootstrap: NacosBootstrap,
        http_client: Optional[httpx.Client] = None,
    ) -> None:
        self.bootstrap = bootstrap
        # The default transport intentionally uses urllib. The Python bundled
        # with macOS 15 has shown proxy/connection failures with httpx against
        # Nacos even when urllib and curl succeed. Tests and callers can still
        # inject an httpx client (including MockTransport).
        self.http_client = http_client

    def load(self) -> Dict[str, Any]:
        errors: List[str] = []
        access_token = self._login()
        for base_url in self._base_urls():
            try:
                params = {
                    "dataId": self.bootstrap.data_id,
                    "group": self.bootstrap.group,
                }
                if self.bootstrap.namespace:
                    params["tenant"] = self.bootstrap.namespace
                if access_token:
                    params["accessToken"] = access_token
                content = self._get_text(
                    f"{base_url}/nacos/v1/cs/configs",
                    params=params,
                )
                content = content.strip()
                if not content:
                    raise RuntimeError("nacos config response is empty")
                return json.loads(content)
            except Exception as exc:
                errors.append(f"{base_url}: {exc}")
        raise RuntimeError("load nacos config failed: " + "; ".join(errors))

    def _login(self) -> str:
        if not self.bootstrap.username:
            return ""
        errors: List[str] = []
        for base_url in self._base_urls():
            try:
                content = self._post_form_text(
                    f"{base_url}/nacos/v1/auth/login",
                    data={
                        "username": self.bootstrap.username,
                        "password": self.bootstrap.password,
                    },
                )
                payload = json.loads(content)
                token = payload.get("accessToken", "")
                if not token:
                    raise RuntimeError("nacos login response missing accessToken")
                return token
            except Exception as exc:
                errors.append(f"{base_url}: {exc}")
        raise RuntimeError("nacos login failed: " + "; ".join(errors))

    def _base_urls(self) -> List[str]:
        urls: List[str] = []
        for raw in self.bootstrap.server_addr.split(","):
            raw = raw.strip()
            if not raw:
                continue
            parsed = urlparse(raw)
            if "://" in raw and parsed.scheme:
                urls.append(raw.rstrip("/"))
            else:
                urls.append(f"http://{raw.rstrip('/')}")
        if not urls:
            raise RuntimeError("NACOS_SERVER_ADDR is required")
        return urls

    def _get_text(self, url: str, params: Dict[str, str]) -> str:
        if self.http_client is not None:
            response = self.http_client.get(
                url,
                params=params,
                timeout=self.bootstrap.timeout_ms / 1000,
            )
            response.raise_for_status()
            return response.text

        query = urlencode(params)
        separator = "&" if urlparse(url).query else "?"
        request = Request(
            f"{url}{separator}{query}",
            headers={"Accept": "application/json, text/plain, */*"},
            method="GET",
        )
        return self._urlopen_text(request)

    def _post_form_text(self, url: str, data: Dict[str, str]) -> str:
        if self.http_client is not None:
            response = self.http_client.post(
                url,
                data=data,
                timeout=self.bootstrap.timeout_ms / 1000,
            )
            response.raise_for_status()
            return response.text

        request = Request(
            url,
            data=urlencode(data).encode("utf-8"),
            headers={
                "Accept": "application/json, text/plain, */*",
                "Content-Type": "application/x-www-form-urlencoded; charset=utf-8",
            },
            method="POST",
        )
        return self._urlopen_text(request)

    def _urlopen_text(self, request: Request) -> str:
        timeout_seconds = self.bootstrap.timeout_ms / 1000
        try:
            with open_url(request, timeout=timeout_seconds) as response:
                body = read_url_response_body(response, timeout_seconds)
                charset = response.headers.get_content_charset() or "utf-8"
                return body.decode(charset)
        except HTTPError as exc:
            body = read_url_response_body(exc, timeout_seconds)
            charset = exc.headers.get_content_charset() or "utf-8"
            detail = body.decode(charset, errors="replace").strip()
            message = f"nacos HTTP {exc.code} {exc.reason}"
            if detail:
                message += f": {detail}"
            raise RuntimeError(message) from exc
        except URLError as exc:
            raise RuntimeError(f"nacos request failed: {exc.reason}") from exc


SETTINGS_REFRESH_SECONDS = 5.0
_settings_lock = threading.RLock()
_settings_cache: Optional[AgentSettings] = None
_settings_expires_at = 0.0
_settings_stale = False


def get_settings() -> AgentSettings:
    # Refresh entire immutable-in-practice snapshots, never update the existing
    # object used by an in-flight LLMClient. Initial load fails closed; a later
    # failed refresh keeps the last verified snapshot, like the Go runtime.
    global _settings_cache, _settings_expires_at, _settings_stale
    with _settings_lock:
        if _settings_cache is None or time.monotonic() >= _settings_expires_at:
            try:
                settings = _load_settings()
            except Exception as exc:
                if _settings_cache is None:
                    raise
                _settings_stale = True
                _settings_expires_at = time.monotonic() + SETTINGS_REFRESH_SECONDS
                # Do not log exception text: loader/validation errors may contain secrets.
                logging.getLogger(__name__).warning(
                    "settings refresh failed; retaining last verified snapshot (%s)",
                    type(exc).__name__,
                )
                return _settings_cache
            _settings_cache = settings
            _settings_stale = False
            _settings_expires_at = time.monotonic() + SETTINGS_REFRESH_SECONDS
        return _settings_cache


def _clear_settings_cache() -> None:
    global _settings_cache, _settings_expires_at, _settings_stale
    with _settings_lock:
        _settings_cache = None
        _settings_expires_at = 0.0
        _settings_stale = False


def settings_snapshot_stale() -> bool:
    with _settings_lock:
        return _settings_stale


# Preserve the existing explicit invalidation API used by tests and operators.
get_settings.cache_clear = _clear_settings_cache


def _load_settings() -> AgentSettings:
    if _nacos_bootstrap_available():
        config = NacosConfigLoader(_nacos_bootstrap_from_env()).load()
        return _settings_from_nacos_config(config)

    return _settings_from_env()


def _settings_from_nacos_config(config: Dict[str, Any]) -> AgentSettings:
    agent = config.get("agent") or {}
    auth = agent.get("auth") or {}
    security_auth = (config.get("security") or {}).get("auth") or {}
    internal_api_auth_header, internal_api_auth_token = _go_security_auth_from_nacos(security_auth)
    llm_config = dict(config.get("llm") or {})
    return AgentSettings(
        agent_version=DEFAULT_AGENT_VERSION,
        config_source="nacos",
        auth_enabled=bool(auth.get("enabled", False)),
        auth_header=str(auth.get("header_name") or "X-Agent-Token"),
        auth_token=str(auth.get("static_token") or ""),
        internal_api=InternalAPISettings(
            base_url=_runtime_internal_api_base_url(config, agent),
            auth_header=internal_api_auth_header,
            auth_token=internal_api_auth_token,
        ),
        tushare=TushareSettings(**(agent.get("tushare") or {})),
        llm=LLMSettings(**llm_config),
        request_timeout_ms=int(agent.get("timeout_ms") or 1000000),
        processing_max_concurrency=int((config.get("processing") or {}).get("llm_max_concurrency", 20)),
    )


def _runtime_internal_api_base_url(config: Dict[str, Any], agent: Dict[str, Any]) -> str:
    base_url = str(agent.get("internal_api_base_url") or "")
    if not base_url:
        return base_url
    try:
        http = (config.get("service") or {}).get("http") or {}
        production_port = int(http.get("port"))
        test_port = int(http.get("port_test"))
        effective_port = (
            production_port
            if os.getenv(FINANCE_SYS_ENV) == PRODUCTION_ENVIRONMENT
            else test_port
        )
        parsed = urlparse(base_url)
        if parsed.hostname is None or parsed.port not in {production_port, test_port}:
            return base_url
        host = f"[{parsed.hostname}]" if ":" in parsed.hostname else parsed.hostname
        netloc = f"{host}:{effective_port}"
        return urlunparse(parsed._replace(netloc=netloc))
    except (TypeError, ValueError):
        return base_url


def _settings_from_env() -> AgentSettings:
    return AgentSettings(
        agent_version=os.getenv("AGENT_VERSION", DEFAULT_AGENT_VERSION),
        config_source="env",
        auth_enabled=_env_bool("AGENT_AUTH_ENABLED", bool(os.getenv("AGENT_AUTH_TOKEN", ""))),
        auth_header=os.getenv("AGENT_AUTH_HEADER", "X-Agent-Token"),
        auth_token=os.getenv("AGENT_AUTH_TOKEN", ""),
        internal_api=InternalAPISettings(
            base_url=os.getenv("AGENT_INTERNAL_API_BASE_URL", ""),
            auth_header=os.getenv("AGENT_INTERNAL_API_AUTH_HEADER", ""),
            auth_token=os.getenv("AGENT_INTERNAL_API_AUTH_TOKEN", ""),
            timeout_ms=_env_int("AGENT_INTERNAL_API_TIMEOUT_MS", 3000),
            max_candidates=_env_int("AGENT_INTERNAL_API_MAX_CANDIDATES", 5),
        ),
        tushare=TushareSettings(
            enabled=_env_bool("TUSHARE_ENABLED", bool(os.getenv("TUSHARE_TOKEN", ""))),
            token=os.getenv("TUSHARE_TOKEN", ""),
            endpoint=os.getenv("TUSHARE_ENDPOINT", "https://api.tushare.pro"),
            timeout_ms=_env_int("TUSHARE_TIMEOUT_MS", 3000),
        ),
        llm=LLMSettings(
            enabled=_env_bool("AGENT_LLM_ENABLED", False),
            provider=os.getenv("AGENT_LLM_PROVIDER", "openai_compatible"),
            endpoint=os.getenv("AGENT_LLM_ENDPOINT", ""),
            api_key=os.getenv("AGENT_LLM_API_KEY", ""),
            model=os.getenv("AGENT_LLM_MODEL", ""),
            temperature=float(os.getenv("AGENT_LLM_TEMPERATURE", "0")),
            extraction_prompt=os.getenv("AGENT_LLM_EXTRACTION_PROMPT", ""),
            timeout_ms=_env_int("AGENT_LLM_TIMEOUT_MS", 15000),
            max_retries=_env_int("AGENT_LLM_MAX_RETRIES", 1),
            extra_headers=_env_json_object("AGENT_LLM_EXTRA_HEADERS"),
        ),
    )


def _go_security_auth_from_nacos(auth: Dict[str, Any]) -> tuple[str, str]:
    if not bool(auth.get("enabled", False)):
        return "", ""
    header = str(auth.get("header_name") or "Authorization")
    token_prefix = str(auth.get("token_prefix") or "")
    static_tokens = auth.get("static_tokens") or []
    if isinstance(static_tokens, str):
        static_tokens = [static_tokens]
    for token in static_tokens:
        token = str(token).strip()
        if token:
            return header, token_prefix + token
    return header, ""


def _nacos_bootstrap_available() -> bool:
    return bool(os.getenv("NACOS_SERVER_ADDR", "").strip())


def _nacos_bootstrap_from_env() -> NacosBootstrap:
    return NacosBootstrap(server_addr=os.environ["NACOS_SERVER_ADDR"])


def _env_bool(name: str, default: bool) -> bool:
    value = os.getenv(name)
    if value is None:
        return default
    return value.strip().lower() in {"1", "true", "yes", "on"}


def _env_int(name: str, default: int) -> int:
    value = os.getenv(name)
    if value is None or value.strip() == "":
        return default
    return int(value)


def _env_json_object(name: str) -> Dict[str, str]:
    value = os.getenv(name)
    if value is None or value.strip() == "":
        return {}
    payload = json.loads(value)
    if not isinstance(payload, dict):
        raise RuntimeError(f"{name} must be a JSON object")
    return {str(key): str(item) for key, item in payload.items()}
