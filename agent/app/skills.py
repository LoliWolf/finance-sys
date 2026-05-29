from __future__ import annotations

import hashlib
import json
import re
from datetime import datetime, timezone
from functools import lru_cache
from pathlib import Path
from typing import Any, Dict, List, Optional

from pydantic import BaseModel, Field


DEFAULT_SKILL_NAME = "instrument_resolution"
MAX_SKILL_BYTES = 64 * 1024
SKILL_NAME_RE = re.compile(r"^[A-Za-z0-9_.-]+$")
SKILL_HASH_RE = re.compile(r"^sha256:[0-9a-f]{64}$")


class SkillLoadError(RuntimeError):
    pass


class SkillSpec(BaseModel):
    name: str = Field(min_length=1)
    version: str = Field(min_length=1)
    description: str = Field(min_length=1)
    content: str = Field(min_length=1)
    skill_hash: str = Field(pattern=r"^sha256:[0-9a-f]{64}$")
    loaded_at: datetime


def load_instrument_resolution_skill() -> SkillSpec:
    return load_skill(DEFAULT_SKILL_NAME)


@lru_cache(maxsize=8)
def load_skill(skill_name: str = DEFAULT_SKILL_NAME) -> SkillSpec:
    skills_root = default_skills_root()
    return load_skill_from_path(skills_root / skill_name / "SKILL.md", skills_root)


def clear_skill_cache() -> None:
    load_skill.cache_clear()


def default_skills_root() -> Path:
    return Path(__file__).resolve().parents[1] / "skills"


def load_skill_from_path(path: Path, skills_root: Optional[Path] = None) -> SkillSpec:
    root = (skills_root or default_skills_root()).resolve()
    resolved = path.resolve()
    _ensure_under_root(resolved, root)
    if not resolved.exists():
        raise SkillLoadError(f"skill file not found: {resolved}")
    if not resolved.is_file():
        raise SkillLoadError(f"skill path is not a file: {resolved}")

    raw = resolved.read_bytes()
    if not raw:
        raise SkillLoadError("skill file is empty")
    if len(raw) > MAX_SKILL_BYTES:
        raise SkillLoadError(f"skill file exceeds {MAX_SKILL_BYTES} bytes")
    try:
        text = raw.decode("utf-8")
    except UnicodeDecodeError as exc:
        raise SkillLoadError("skill file must be valid UTF-8") from exc

    normalized = normalize_skill_content(text)
    if not normalized.strip():
        raise SkillLoadError("skill file is blank")

    metadata = parse_front_matter(normalized)
    for key in ("name", "version", "description"):
        if not metadata.get(key, "").strip():
            raise SkillLoadError(f"skill front matter missing {key}")
    if not SKILL_NAME_RE.fullmatch(metadata["name"]):
        raise SkillLoadError("skill name contains unsupported characters")

    skill_hash = "sha256:" + hashlib.sha256(normalized.encode("utf-8")).hexdigest()
    return SkillSpec(
        name=metadata["name"].strip(),
        version=metadata["version"].strip(),
        description=metadata["description"].strip(),
        content=normalized,
        skill_hash=skill_hash,
        loaded_at=datetime.now(timezone.utc),
    )


def normalize_skill_content(text: str) -> str:
    if text.startswith("\ufeff"):
        text = text[1:]
    text = text.replace("\r\n", "\n").replace("\r", "\n")
    return text.strip() + "\n"


def parse_front_matter(content: str) -> Dict[str, str]:
    if not content.startswith("---\n"):
        raise SkillLoadError("skill front matter is required")
    end = content.find("\n---\n", 4)
    if end < 0:
        raise SkillLoadError("skill front matter end marker is required")
    front_matter = content[4:end]
    metadata: Dict[str, str] = {}
    for line in front_matter.split("\n"):
        line = line.strip()
        if not line:
            continue
        key, sep, value = line.partition(":")
        if not sep or not key.strip():
            raise SkillLoadError(f"invalid skill front matter line: {line}")
        metadata[key.strip()] = value.strip()
    return metadata


def render_skill_prompt_block(skill: SkillSpec) -> str:
    return (
        '<instrument_resolution_skill '
        f'name="{skill.name}" '
        f'version="{skill.version}" '
        f'hash="{skill.skill_hash}"'
        ">\n"
        f"{skill.content}"
        "</instrument_resolution_skill>"
    )


def load_skill_examples(path: Path) -> List[Dict[str, Any]]:
    if not path.exists():
        raise SkillLoadError(f"skill examples file not found: {path}")
    examples: List[Dict[str, Any]] = []
    for line_number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), start=1):
        line = line.strip()
        if not line:
            continue
        try:
            item = json.loads(line)
        except json.JSONDecodeError as exc:
            raise SkillLoadError(f"invalid examples.jsonl line {line_number}") from exc
        _validate_example(item, line_number)
        examples.append(item)
    if not examples:
        raise SkillLoadError("examples.jsonl must contain at least one example")
    return examples


def _validate_example(item: Dict[str, Any], line_number: int) -> None:
    if not isinstance(item.get("id"), str) or not item["id"].strip():
        raise SkillLoadError(f"examples.jsonl line {line_number} missing id")
    if not isinstance(item.get("input"), dict) or not isinstance(item["input"].get("text"), str):
        raise SkillLoadError(f"examples.jsonl line {line_number} missing input.text")
    if not isinstance(item.get("expected"), dict):
        raise SkillLoadError(f"examples.jsonl line {line_number} missing expected")


def _ensure_under_root(path: Path, root: Path) -> None:
    try:
        path.relative_to(root)
    except ValueError as exc:
        raise SkillLoadError("skill path must stay under agent/skills") from exc
