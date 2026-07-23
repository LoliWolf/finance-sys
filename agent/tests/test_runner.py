from pathlib import Path

import pytest

from app.runner import load_nacos_server_address, resolve_listen_address


def test_load_nacos_server_address_reads_address_only_file(
    monkeypatch, tmp_path: Path
) -> None:
    monkeypatch.delenv("NACOS_SERVER_ADDR", raising=False)
    path = tmp_path / "bootstrap.env"
    path.write_text(
        "# bootstrap\r\nNACOS_SERVER_ADDR='192.168.31.234:8848'\r\n",
        encoding="utf-8",
    )

    assert load_nacos_server_address([path]) == "192.168.31.234:8848"


def test_load_nacos_server_address_rejects_other_keys(
    monkeypatch, tmp_path: Path
) -> None:
    monkeypatch.delenv("NACOS_SERVER_ADDR", raising=False)
    path = tmp_path / "bootstrap.env"
    path.write_text(
        "NACOS_SERVER_ADDR=192.168.31.234:8848\nAPP_PORT=8108\n",
        encoding="utf-8",
    )

    with pytest.raises(RuntimeError, match="only NACOS_SERVER_ADDR is allowed"):
        load_nacos_server_address([path])


def test_resolve_listen_address_uses_agent_endpoint() -> None:
    assert resolve_listen_address(
        {"agent": {"endpoint": "http://127.0.0.1:8108/v1/resolve-document"}}
    ) == ("127.0.0.1", 8108)


@pytest.mark.parametrize(
    "endpoint",
    ["", "https://127.0.0.1:8108/v1/resolve-document", "not-a-url"],
)
def test_resolve_listen_address_rejects_invalid_endpoint(endpoint: str) -> None:
    with pytest.raises(RuntimeError):
        resolve_listen_address({"agent": {"endpoint": endpoint}})
