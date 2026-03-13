-- name: InsertConfigSnapshot :execresult
INSERT INTO config_snapshots (
    config_version,
    source,
    sha256,
    raw_json
) VALUES (
    ?, ?, ?, ?
);

-- name: GetConfigSnapshotByID :one
SELECT id, config_version, source, sha256, raw_json, created_at
FROM config_snapshots
WHERE id = ?;

-- name: InsertDocument :execresult
INSERT INTO documents (
    source_type,
    source_name,
    author,
    institution,
    title,
    file_name,
    extension,
    content_type,
    sha256,
    status,
    config_version,
    raw_content
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
);

-- name: GetDocumentByID :one
SELECT id, source_type, source_name, author, institution, title, file_name, extension, content_type, sha256, status, config_version, raw_content, created_at, updated_at
FROM documents
WHERE id = ?;

-- name: GetDocumentBySHA :one
SELECT id, source_type, source_name, author, institution, title, file_name, extension, content_type, sha256, status, config_version, raw_content, created_at, updated_at
FROM documents
WHERE sha256 = ?;

-- name: ListDocuments :many
SELECT id, source_type, source_name, author, institution, title, file_name, extension, content_type, sha256, status, config_version, raw_content, created_at, updated_at
FROM documents
ORDER BY created_at DESC
LIMIT ?;

-- name: UpdateDocumentStatus :exec
UPDATE documents
SET status = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: InsertParseRun :execresult
INSERT INTO parse_runs (
    document_id,
    status,
    parser_name,
    parser_version,
    error_message,
    page_count,
    content_text,
    cleaned_text,
    chunks_json,
    raw_metadata_json
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
);

-- name: GetParseRunByID :one
SELECT id, document_id, status, parser_name, parser_version, error_message, page_count, content_text, cleaned_text, chunks_json, raw_metadata_json, created_at, updated_at
FROM parse_runs
WHERE id = ?;

-- name: GetLatestParseRunByDocumentID :one
SELECT id, document_id, status, parser_name, parser_version, error_message, page_count, content_text, cleaned_text, chunks_json, raw_metadata_json, created_at, updated_at
FROM parse_runs
WHERE document_id = ?
ORDER BY created_at DESC
LIMIT 1;

-- name: DeletePlansByDocumentID :exec
DELETE FROM trade_candidate_plans
WHERE document_id = ?;

-- name: InsertPlan :execresult
INSERT INTO trade_candidate_plans (
    document_id,
    parse_run_id,
    analyst,
    institution,
    symbol,
    asset_type,
    market,
    strategy,
    direction,
    trade_date,
    reference_price,
    entry_price,
    stop_loss,
    take_profit,
    position_pct,
    confidence,
    status,
    thesis,
    risks_json,
    evidence_json,
    pricing_note,
    config_version,
    rule_version
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
);

-- name: GetPlanByID :one
SELECT id, document_id, parse_run_id, analyst, institution, symbol, asset_type, market, strategy, direction, trade_date, reference_price, entry_price, stop_loss, take_profit, position_pct, confidence, status, thesis, risks_json, evidence_json, pricing_note, config_version, rule_version, created_at, updated_at
FROM trade_candidate_plans
WHERE id = ?;

-- name: ListPlans :many
SELECT id, document_id, parse_run_id, analyst, institution, symbol, asset_type, market, strategy, direction, trade_date, reference_price, entry_price, stop_loss, take_profit, position_pct, confidence, status, thesis, risks_json, evidence_json, pricing_note, config_version, rule_version, created_at, updated_at
FROM trade_candidate_plans
ORDER BY created_at DESC
LIMIT ?;

-- name: ListPlansByDocumentID :many
SELECT id, document_id, parse_run_id, analyst, institution, symbol, asset_type, market, strategy, direction, trade_date, reference_price, entry_price, stop_loss, take_profit, position_pct, confidence, status, thesis, risks_json, evidence_json, pricing_note, config_version, rule_version, created_at, updated_at
FROM trade_candidate_plans
WHERE document_id = ?
ORDER BY created_at ASC;
