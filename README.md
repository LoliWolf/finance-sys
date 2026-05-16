# finance-sys

Go 1.22 backend for a narrowed China-market research workflow:

1. Upload a parseable PDF/Word/text document
2. Deduplicate by SHA256 and store the original bytes in MySQL
3. Extract plain text from the file
4. Ask the LLM layer to extract structured trade intent only
5. Generate stable T+1 candidate plans with deterministic rules
6. Persist and query results

## Current Scope

- Supported input: `.pdf`, `.doc`, `.docx`, `.txt`, `.md`, `.csv`
- Storage model: raw document bytes stored in MySQL
- Parsing: plain-text extraction only
- Analysis: structured trade intent extraction from text
- Rules: deterministic generation of entry, stop loss, take profit, position
- Database access: `gorm.io/gorm` + `gorm.io/driver/mysql`
- Model generation: `gorm.io/gen`
- API:
  - `GET /healthz`
  - `GET /api/v1/documents`
  - `POST /api/v1/documents/upload`
  - `POST /api/v1/documents/{id}/analyze`
  - `GET /api/v1/documents/{id}/plans`
  - `GET /api/v1/plans`
  - `POST /api/v1/admin/config/reload`

## Database Practice

- Database models live in `internal/domain/db_model`.
- DAL singletons live in `internal/dal`.
- Every DAL DML method accepts `ctx context.Context` and `db *gorm.DB`.
- Query methods use `dal.QueryParam`; update methods use `dal.UpdateParam`.
- DAL exposes concrete methods for callers, such as `QueryByID`, `QueryLatestByDocumentID`, and `UpdateStatusByID`.
- DAL accepts and returns `internal/domain/db_model` records or primitive values.
- Service code owns transactions and conversion between domain models and database models.
- `sqlc` is no longer part of the project.

## Generate Models

`generate.go` follows the same config-loading path as the API bootstrap: it reads Nacos bootstrap environment variables, loads the Nacos JSON config, extracts the MySQL DSN, connects to MySQL, and runs `gorm.io/gen`.

```bash
go run generate.go
```

If Nacos bootstrap variables are absent, it follows the API's local development fallback and reads `configs/example_nacos_config.json`.

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
