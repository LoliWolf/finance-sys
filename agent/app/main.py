import asyncio
import hashlib
import time
import anyio.to_thread
from contextlib import suppress
from fastapi import Depends, FastAPI, Header, HTTPException, Request
from starlette.concurrency import run_in_threadpool

from app.config import get_settings, settings_snapshot_stale
from app.graph import resolve_document
from app.schemas import AgentResolveDocumentRequest, AgentResolveDocumentResponse
from app.skills import load_instrument_resolution_skill
from app.request_budget import RequestBudget, current_budget

app = FastAPI(title="finance-sys M4 agent", version="0.1.0")


def require_agent_token(request: Request, x_agent_token: str = Header(default="")) -> None:
    settings = get_settings()
    if not settings.auth_enabled:
        return
    header_value = request.headers.get(settings.auth_header, x_agent_token)
    if header_value != settings.auth_token:
        raise HTTPException(status_code=401, detail="invalid agent token")


@app.get("/healthz")
def healthz():
    skill = load_instrument_resolution_skill()
    settings = get_settings()
    return {
        "status": "ok",
        "agent_version": settings.agent_version,
        "settings_stale": settings_snapshot_stale(),
        "skill_name": skill.name,
        "skill_version": skill.version,
        "skill_hash": skill.skill_hash,
        "extraction_config": {
            "processing_max_concurrency": settings.processing_max_concurrency,
            "temperature": settings.llm.temperature,
            "prompt_sha256": hashlib.sha256(settings.llm.extraction_prompt.encode("utf-8")).hexdigest(),
            "unlimited_default": True,
        },
    }


@app.post("/v1/resolve-document", response_model=AgentResolveDocumentResponse)
async def resolve_document_endpoint(
    request: AgentResolveDocumentRequest,
    http_request: Request,
    _: None = Depends(require_agent_token),
):
    settings = get_settings()
    # Starlette otherwise stops at 40 worker tokens, even when the Go pool and
    # measured upstream capacity allow more concurrent document extractions.
    anyio.to_thread.current_default_thread_limiter().total_tokens = settings.processing_max_concurrency
    seconds = settings.request_timeout_ms / 1000
    raw_deadline = http_request.headers.get("X-Finance-Deadline-Unix-Ms")
    if raw_deadline:
        try:
            seconds = min(seconds, int(raw_deadline) / 1000 - time.time())
        except (ValueError, OverflowError):
            raise HTTPException(status_code=400, detail="invalid request deadline")
    if seconds <= 0:
        raise HTTPException(status_code=504, detail="request deadline already exceeded")
    budget = RequestBudget(seconds)
    token = current_budget.set(budget)
    task = asyncio.create_task(run_in_threadpool(resolve_document, request))
    try:
        while not task.done():
            if await http_request.is_disconnected():
                raise HTTPException(status_code=499, detail="caller disconnected")
            if time.monotonic() >= budget.deadline:
                raise HTTPException(status_code=504, detail="shared request deadline exceeded")
            await asyncio.wait({task}, timeout=0.2)
        return await task
    finally:
        budget.cancel()
        if not task.done():
            task.cancel()
            with suppress(asyncio.CancelledError):
                await task
        current_budget.reset(token)
