from dataclasses import dataclass
from functools import lru_cache
import json
import os
from typing import Any, Dict, List, Optional
from urllib.parse import urlparse

import httpx
from pydantic import BaseModel, Field


class LLMSettings(BaseModel):
    enabled: bool = Field(default=False)
    provider: str = Field(default="openai_compatible")
    endpoint: str = Field(default="")
    api_key: str = Field(default="")
    model: str = Field(default="")
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
    agent_version: str = Field(default="m4-agent-0.1.0")
    config_source: str = Field(default="env")
    auth_enabled: bool = Field(default=False)
    auth_header: str = Field(default="X-Agent-Token")
    auth_token: str = Field(default="")
    internal_api: InternalAPISettings = Field(default_factory=InternalAPISettings)
    tushare: TushareSettings = Field(default_factory=TushareSettings)
    llm: LLMSettings = Field(default_factory=LLMSettings)


@dataclass(frozen=True)
class NacosBootstrap:
    server_addr: str
    namespace: str
    group: str
    data_id: str
    username: str = ""
    password: str = ""
    timeout_ms: int = 5000


class NacosConfigLoader:
    def __init__(
        self,
        bootstrap: NacosBootstrap,
        http_client: Optional[httpx.Client] = None,
    ) -> None:
        self.bootstrap = bootstrap
        self.http_client = http_client or httpx.Client(timeout=bootstrap.timeout_ms / 1000)

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
                response = self.http_client.get(
                    f"{base_url}/nacos/v1/cs/configs",
                    params=params,
                    timeout=self.bootstrap.timeout_ms / 1000,
                )
                response.raise_for_status()
                content = response.text.strip()
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
                response = self.http_client.post(
                    f"{base_url}/nacos/v1/auth/login",
                    data={
                        "username": self.bootstrap.username,
                        "password": self.bootstrap.password,
                    },
                    timeout=self.bootstrap.timeout_ms / 1000,
                )
                response.raise_for_status()
                payload = response.json()
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


@lru_cache
def get_settings() -> AgentSettings:
    if _nacos_bootstrap_available():
        try:
            config = NacosConfigLoader(_nacos_bootstrap_from_env()).load()
            return _settings_from_nacos_config(config)
        except Exception:
            if _env_bool("AGENT_NACOS_FAIL_FAST", True):
                raise

    return _settings_from_env()


def _settings_from_nacos_config(config: Dict[str, Any]) -> AgentSettings:
    agent = config.get("agent") or {}
    auth = agent.get("auth") or {}
    security_auth = (config.get("security") or {}).get("auth") or {}
    internal_api_auth_header, internal_api_auth_token = _go_security_auth_from_nacos(security_auth)
    llm_config = dict(config.get("llm") or {})
    llm_extra_headers = dict(llm_config.get("extra_headers") or {})
    llm_extra_headers.update(_env_json_object("AGENT_LLM_EXTRA_HEADERS"))
    if llm_extra_headers:
        llm_config["extra_headers"] = llm_extra_headers
    _apply_env_int_override(llm_config, "timeout_ms", "AGENT_LLM_TIMEOUT_MS")
    _apply_env_int_override(llm_config, "max_retries", "AGENT_LLM_MAX_RETRIES")
    return AgentSettings(
        agent_version=os.getenv("AGENT_VERSION", "m4-agent-0.1.0"),
        config_source="nacos",
        auth_enabled=bool(auth.get("enabled", False)),
        auth_header=str(auth.get("header_name") or "X-Agent-Token"),
        auth_token=str(auth.get("static_token") or ""),
        internal_api=InternalAPISettings(
            base_url=str(agent.get("internal_api_base_url") or ""),
            auth_header=internal_api_auth_header,
            auth_token=internal_api_auth_token,
            timeout_ms=_env_int("AGENT_INTERNAL_API_TIMEOUT_MS", 3000),
            max_candidates=_env_int("AGENT_INTERNAL_API_MAX_CANDIDATES", 5),
        ),
        tushare=TushareSettings(**(agent.get("tushare") or {})),
        llm=LLMSettings(**llm_config),
    )


def _settings_from_env() -> AgentSettings:
    return AgentSettings(
        agent_version=os.getenv("AGENT_VERSION", "m4-agent-0.1.0"),
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
    return all(
        os.getenv(name, "").strip()
        for name in ("NACOS_SERVER_ADDR", "NACOS_NAMESPACE", "NACOS_GROUP", "NACOS_DATA_ID")
    )


def _nacos_bootstrap_from_env() -> NacosBootstrap:
    return NacosBootstrap(
        server_addr=os.environ["NACOS_SERVER_ADDR"],
        namespace=os.environ["NACOS_NAMESPACE"],
        group=os.environ["NACOS_GROUP"],
        data_id=os.environ["NACOS_DATA_ID"],
        username=os.getenv("NACOS_USERNAME", ""),
        password=os.getenv("NACOS_PASSWORD", ""),
        timeout_ms=_env_int("NACOS_TIMEOUT_MS", 5000),
    )


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


def _apply_env_int_override(config: Dict[str, Any], key: str, env_name: str) -> None:
    value = os.getenv(env_name)
    if value is not None and value.strip() != "":
        config[key] = int(value)


def _env_json_object(name: str) -> Dict[str, str]:
    value = os.getenv(name)
    if value is None or value.strip() == "":
        return {}
    payload = json.loads(value)
    if not isinstance(payload, dict):
        raise RuntimeError(f"{name} must be a JSON object")
    return {str(key): str(item) for key, item in payload.items()}
