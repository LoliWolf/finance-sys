import os
from pathlib import Path
from typing import Iterable, Optional, Tuple
from urllib.parse import urlsplit

import uvicorn

from app.config import NacosBootstrap, NacosConfigLoader


def load_nacos_server_address(paths: Optional[Iterable[Path]] = None) -> str:
    explicit = os.getenv("NACOS_SERVER_ADDR", "").strip()
    if explicit:
        return explicit

    if paths is None:
        repository_root = Path(__file__).resolve().parents[2]
        paths = (
            repository_root / "bootstrap_go122.env",
            repository_root / "bootstrap_go122.env.example",
        )

    for path in paths:
        if not path.is_file():
            continue
        server_addr = _read_address_only_file(path)
        os.environ["NACOS_SERVER_ADDR"] = server_addr
        return server_addr
    raise RuntimeError(
        "NACOS_SERVER_ADDR is required and no bootstrap_go122.env file was found"
    )


def _read_address_only_file(path: Path) -> str:
    server_addr = ""
    for raw_line in path.read_text(encoding="utf-8-sig").splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        if "=" not in line:
            raise RuntimeError(
                f"invalid Nacos bootstrap line in {path}: "
                "only NACOS_SERVER_ADDR is allowed"
            )
        key, value = (part.strip() for part in line.split("=", 1))
        if key != "NACOS_SERVER_ADDR":
            raise RuntimeError(
                f"only NACOS_SERVER_ADDR is allowed in {path}; found {key}"
            )
        value = value.strip("\"'").strip()
        if not value:
            raise RuntimeError(f"NACOS_SERVER_ADDR is empty in {path}")
        if server_addr:
            raise RuntimeError(f"duplicate NACOS_SERVER_ADDR in {path}")
        server_addr = value
    if not server_addr:
        raise RuntimeError(f"NACOS_SERVER_ADDR is missing from {path}")
    return server_addr


def resolve_listen_address(config: dict) -> Tuple[str, int]:
    endpoint = str((config.get("agent") or {}).get("endpoint") or "").strip()
    if not endpoint:
        raise RuntimeError("agent.endpoint is required in Nacos")
    parsed = urlsplit(endpoint)
    if parsed.scheme != "http" or not parsed.hostname:
        raise RuntimeError("agent.endpoint in Nacos must be an http URL with a host")
    try:
        port = parsed.port
    except ValueError as exc:
        raise RuntimeError("agent.endpoint in Nacos contains an invalid port") from exc
    if port is None:
        port = 80
    return parsed.hostname, port


def main() -> None:
    server_addr = load_nacos_server_address()
    config = NacosConfigLoader(NacosBootstrap(server_addr=server_addr)).load()
    host, port = resolve_listen_address(config)
    uvicorn.run("app.main:app", host=host, port=port)


if __name__ == "__main__":
    main()
