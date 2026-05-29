from functools import lru_cache
import json
from pathlib import Path
from typing import List, Literal, Optional

from pydantic import BaseModel, Field, field_validator

from app.skills import DEFAULT_SKILL_NAME, SkillLoadError, default_skills_root


SourceType = Literal[
    "self_generated",
    "official_api",
    "official_skill",
    "community_mcp",
    "community_high_usage",
    "user_provided",
]


class ToolSourceSpec(BaseModel):
    tool_name: str = Field(min_length=1)
    source_type: SourceType
    source_url: str = Field(min_length=1)
    source_evidence: str = Field(min_length=1)
    license: str = Field(min_length=1)
    allowed_use: str = Field(min_length=1)
    blocked_use: str = Field(min_length=1)

    @field_validator("tool_name", "source_url", "source_evidence", "license", "allowed_use", "blocked_use")
    @classmethod
    def text_must_not_be_blank(cls, value: str) -> str:
        value = value.strip()
        if not value:
            raise ValueError("value is required")
        return value


@lru_cache
def load_instrument_resolution_tool_registry() -> List[ToolSourceSpec]:
    path = default_skills_root() / DEFAULT_SKILL_NAME / "tools.json"
    return load_tool_registry(path)


def load_tool_registry(path: Path, skills_root: Optional[Path] = None) -> List[ToolSourceSpec]:
    resolved = path.resolve()
    root = (skills_root or default_skills_root()).resolve()
    try:
        resolved.relative_to(root)
    except ValueError as exc:
        raise SkillLoadError("tool registry path must stay under agent/skills") from exc
    if not resolved.is_file():
        raise SkillLoadError(f"tool registry file not found: {resolved}")
    try:
        payload = json.loads(resolved.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        raise SkillLoadError(f"tool registry must be valid JSON: {exc}") from exc
    if not isinstance(payload, list) or not payload:
        raise SkillLoadError("tool registry must be a non-empty array")
    tools = [ToolSourceSpec.model_validate(item) for item in payload]
    names = [tool.tool_name for tool in tools]
    if len(names) != len(set(names)):
        raise SkillLoadError("tool registry contains duplicate tool_name")
    return tools


def clear_tool_registry_cache() -> None:
    load_instrument_resolution_tool_registry.cache_clear()
