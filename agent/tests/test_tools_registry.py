import json
from pathlib import Path

import pytest

from app.skills import SkillLoadError
from app.tools.registry import load_instrument_resolution_tool_registry, load_tool_registry


def test_instrument_resolution_tool_registry_has_sources_for_every_tool():
    tools = load_instrument_resolution_tool_registry()
    names = {tool.tool_name for tool in tools}

    assert "local_security_lookup_tool" in names
    assert "local_security_verify_tool" in names
    assert "tushare_stock_basic_tool" in names
    for tool in tools:
        assert tool.source_type
        assert tool.source_url
        assert tool.source_evidence
        assert tool.license
        assert tool.allowed_use
        assert tool.blocked_use


def test_tool_registry_rejects_missing_required_source_fields(tmp_path: Path):
    root = tmp_path / "skills"
    skill_dir = root / "instrument_resolution"
    skill_dir.mkdir(parents=True)
    registry = skill_dir / "tools.json"
    registry.write_text(json.dumps([{"tool_name": "bad_tool"}]), encoding="utf-8")

    with pytest.raises(ValueError):
        load_tool_registry(registry, root)


def test_tool_registry_rejects_path_traversal(tmp_path: Path):
    root = tmp_path / "skills"
    root.mkdir()
    outside = tmp_path / "tools.json"
    outside.write_text("[]", encoding="utf-8")

    with pytest.raises(SkillLoadError, match="under agent/skills"):
        load_tool_registry(outside, root)
