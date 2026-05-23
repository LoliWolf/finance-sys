from fastapi import Depends, FastAPI, Header, HTTPException, Request

from app.config import get_settings
from app.graph import resolve_document
from app.schemas import AgentResolveDocumentRequest, AgentResolveDocumentResponse

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
    return {"status": "ok", "agent_version": get_settings().agent_version}


@app.post("/v1/resolve-document", response_model=AgentResolveDocumentResponse)
def resolve_document_endpoint(
    request: AgentResolveDocumentRequest,
    _: None = Depends(require_agent_token),
):
    return resolve_document(request)
