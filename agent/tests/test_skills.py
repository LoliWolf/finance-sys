from pathlib import Path

import pytest

from app.skills import (
    SkillLoadError,
    load_skill_examples,
    load_skill_from_path,
    normalize_skill_content,
    render_skill_prompt_block,
)


VALID_SKILL = """---
name: instrument_resolution
version: instrument-resolution-m5-v1
description: Rules for Chinese A-share instrument resolution.
---

# Instrument Resolution Rules 标的解析规则

1. 不允许编造 `ts_code`。
2. 板块、主题、行业、指数、泛称不能进入 `candidate_plan_inputs`。
"""


def test_load_skill_from_path_returns_stable_hash_for_normalized_content(tmp_path: Path):
    root = tmp_path / "skills"
    skill_dir = root / "instrument_resolution"
    skill_dir.mkdir(parents=True)
    skill_file = skill_dir / "SKILL.md"
    skill_file.write_bytes(VALID_SKILL.encode("utf-8"))

    loaded = load_skill_from_path(skill_file, root)
    skill_file.write_bytes(("\ufeff" + VALID_SKILL.replace("\n", "\r\n")).encode("utf-8"))
    loaded_crlf = load_skill_from_path(skill_file, root)

    assert loaded.name == "instrument_resolution"
    assert loaded.version == "instrument-resolution-m5-v1"
    assert loaded.skill_hash.startswith("sha256:")
    assert loaded.skill_hash == loaded_crlf.skill_hash
    assert loaded.content == normalize_skill_content(VALID_SKILL)


def test_load_skill_rejects_missing_empty_invalid_and_traversal(tmp_path: Path):
    root = tmp_path / "skills"
    root.mkdir()

    with pytest.raises(SkillLoadError, match="not found"):
        load_skill_from_path(root / "instrument_resolution" / "SKILL.md", root)

    skill_dir = root / "instrument_resolution"
    skill_dir.mkdir()
    empty = skill_dir / "SKILL.md"
    empty.write_text("", encoding="utf-8")
    with pytest.raises(SkillLoadError, match="empty"):
        load_skill_from_path(empty, root)

    invalid = skill_dir / "invalid.md"
    invalid.write_text(VALID_SKILL.replace("version: instrument-resolution-m5-v1\n", ""), encoding="utf-8")
    with pytest.raises(SkillLoadError, match="missing version"):
        load_skill_from_path(invalid, root)

    outside = tmp_path / "outside.md"
    outside.write_text(VALID_SKILL, encoding="utf-8")
    with pytest.raises(SkillLoadError, match="under agent/skills"):
        load_skill_from_path(outside, root)


def test_render_skill_prompt_block_contains_contract_fields(tmp_path: Path):
    root = tmp_path / "skills"
    skill_dir = root / "instrument_resolution"
    skill_dir.mkdir(parents=True)
    skill_file = skill_dir / "SKILL.md"
    skill_file.write_text(VALID_SKILL, encoding="utf-8")

    skill = load_skill_from_path(skill_file, root)
    block = render_skill_prompt_block(skill)

    assert 'name="instrument_resolution"' in block
    assert 'version="instrument-resolution-m5-v1"' in block
    assert f'hash="{skill.skill_hash}"' in block
    assert "不允许编造" in block


def test_load_skill_examples_accepts_jsonl_and_rejects_bad_lines(tmp_path: Path):
    examples = tmp_path / "examples.jsonl"
    examples.write_text(
        '{"id":"valid","input":{"text":"推荐新易盛"},"expected":{"raw_intents":["新易盛"]}}\n',
        encoding="utf-8",
    )
    assert load_skill_examples(examples)[0]["id"] == "valid"

    examples.write_text('{"id":"bad","input":{}}\n', encoding="utf-8")
    with pytest.raises(SkillLoadError, match="input.text"):
        load_skill_examples(examples)
