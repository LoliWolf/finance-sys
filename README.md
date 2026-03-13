# finance-sys

Go 1.22 backend for a narrowed workflow:

1. Upload a parseable PDF/Word/text document
2. Extract plain text from the file
3. Send the plain text into the analysis layer
4. Generate stable structured T+1 candidate plans with deterministic rules

## Current Scope

- Supported input: `.pdf`, `.doc`, `.docx`, `.txt`, `.md`, `.csv`
- Storage model: raw document bytes stored in MySQL
- Parsing: plain-text extraction only
- Analysis: structured trade intent extraction from text
- Rules: deterministic generation of entry, stop loss, take profit, position
- API:
  - `GET /healthz`
  - `GET /api/v1/documents`
  - `POST /api/v1/documents/upload`
  - `POST /api/v1/documents/{id}/analyze`
  - `GET /api/v1/documents/{id}/plans`
  - `GET /api/v1/plans`
  - `POST /api/v1/admin/config/reload`

## Removed From This Revision

- Market provider chain
- Approval flow
- T+1 evaluation
- Scorecard/reporting
- Worker and scheduler
- Redis and object storage dependencies in the runtime path
- HTML/email/table/OCR side paths

## Run

```bash
go run ./cmd/api
```

## Validate

```bash
env GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go test ./...
env GOTOOLCHAIN=local GOCACHE=$(pwd)/.gocache go build ./...
```
