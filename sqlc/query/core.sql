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
    object_key,
    status,
    config_version
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
);

-- name: GetDocumentByID :one
SELECT id, source_type, source_name, author, institution, title, file_name, extension, content_type, sha256, object_key, status, config_version, created_at, updated_at
FROM documents
WHERE id = ?;

-- name: GetDocumentBySHA :one
SELECT id, source_type, source_name, author, institution, title, file_name, extension, content_type, sha256, object_key, status, config_version, created_at, updated_at
FROM documents
WHERE sha256 = ?;

-- name: ListDocuments :many
SELECT id, source_type, source_name, author, institution, title, file_name, extension, content_type, sha256, object_key, status, config_version, created_at, updated_at
FROM documents
ORDER BY created_at DESC
LIMIT ?;

-- name: ListDocumentsByStatus :many
SELECT id, source_type, source_name, author, institution, title, file_name, extension, content_type, sha256, object_key, status, config_version, created_at, updated_at
FROM documents
WHERE status = ?
ORDER BY created_at ASC
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
    requires_ocr,
    error_message,
    page_count,
    text_density,
    content_text,
    cleaned_text,
    sections_json,
    chunks_json,
    tables_json,
    raw_metadata_json
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
);

-- name: GetParseRunByID :one
SELECT id, document_id, status, parser_name, parser_version, requires_ocr, error_message, page_count, text_density, content_text, cleaned_text, sections_json, chunks_json, tables_json, raw_metadata_json, created_at, updated_at
FROM parse_runs
WHERE id = ?;

-- name: GetLatestParseRunByDocumentID :one
SELECT id, document_id, status, parser_name, parser_version, requires_ocr, error_message, page_count, text_density, content_text, cleaned_text, sections_json, chunks_json, tables_json, raw_metadata_json, created_at, updated_at
FROM parse_runs
WHERE document_id = ?
ORDER BY created_at DESC
LIMIT 1;

-- name: InsertSignal :execresult
INSERT INTO signals (
    document_id,
    parse_run_id,
    expert_name,
    expert_org,
    symbol,
    asset_type,
    market,
    sentiment,
    thesis,
    evidence_json,
    risks_json,
    confidence,
    config_version,
    rule_version,
    signal_date
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
);

-- name: GetSignalByID :one
SELECT id, document_id, parse_run_id, expert_name, expert_org, symbol, asset_type, market, sentiment, thesis, evidence_json, risks_json, confidence, config_version, rule_version, signal_date, created_at
FROM signals
WHERE id = ?;

-- name: ListSignalsByDocumentID :many
SELECT id, document_id, parse_run_id, expert_name, expert_org, symbol, asset_type, market, sentiment, thesis, evidence_json, risks_json, confidence, config_version, rule_version, signal_date, created_at
FROM signals
WHERE document_id = ?
ORDER BY created_at ASC;

-- name: InsertMarketSnapshot :exec
INSERT INTO market_snapshots (
    symbol,
    trade_date,
    provider,
    open,
    high,
    low,
    close,
    volume,
    turnover,
    atr,
    prev_close,
    benchmark_return_pct,
    raw_object_key,
    config_version
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
)
ON DUPLICATE KEY UPDATE
    provider = VALUES(provider),
    open = VALUES(open),
    high = VALUES(high),
    low = VALUES(low),
    close = VALUES(close),
    volume = VALUES(volume),
    turnover = VALUES(turnover),
    atr = VALUES(atr),
    prev_close = VALUES(prev_close),
    benchmark_return_pct = VALUES(benchmark_return_pct),
    raw_object_key = VALUES(raw_object_key),
    config_version = VALUES(config_version);

-- name: GetMarketSnapshotBySymbolDate :one
SELECT id, symbol, trade_date, provider, open, high, low, close, volume, turnover, atr, prev_close, benchmark_return_pct, raw_object_key, config_version, created_at
FROM market_snapshots
WHERE symbol = ? AND trade_date = ?;

-- name: InsertPlan :execresult
INSERT INTO plans (
    signal_id,
    document_id,
    symbol,
    strategy,
    trade_date,
    direction,
    entry_price,
    stop_loss,
    take_profit,
    invalidation_price,
    position_pct,
    status,
    rationale,
    config_version,
    rule_version,
    market_snapshot_id
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
);

-- name: GetPlanByID :one
SELECT id, signal_id, document_id, symbol, strategy, trade_date, direction, entry_price, stop_loss, take_profit, invalidation_price, position_pct, status, rationale, config_version, rule_version, market_snapshot_id, approved_by, approved_at, created_at, updated_at
FROM plans
WHERE id = ?;

-- name: ListPlans :many
SELECT id, signal_id, document_id, symbol, strategy, trade_date, direction, entry_price, stop_loss, take_profit, invalidation_price, position_pct, status, rationale, config_version, rule_version, market_snapshot_id, approved_by, approved_at, created_at, updated_at
FROM plans
ORDER BY created_at DESC
LIMIT ?;

-- name: ApprovePlan :exec
UPDATE plans
SET status = 'APPROVED',
    approved_by = ?,
    approved_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: ListApprovedPlansForTradeDateWithoutEvaluation :many
SELECT p.id, p.signal_id, p.document_id, p.symbol, p.strategy, p.trade_date, p.direction, p.entry_price, p.stop_loss, p.take_profit, p.invalidation_price, p.position_pct, p.status, p.rationale, p.config_version, p.rule_version, p.market_snapshot_id, p.approved_by, p.approved_at, p.created_at, p.updated_at
FROM plans p
LEFT JOIN evaluations e ON e.plan_id = p.id
WHERE p.status = 'APPROVED'
  AND p.trade_date = ?
  AND e.id IS NULL
ORDER BY p.created_at ASC;

-- name: InsertEvaluation :execresult
INSERT INTO evaluations (
    plan_id,
    trade_date,
    status,
    entry_price,
    exit_price,
    close_price,
    pnl_pct,
    mfe_pct,
    mae_pct,
    benchmark_return_pct,
    excess_return_pct,
    reason,
    data_quality_flag,
    config_version
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
);

-- name: GetEvaluationByID :one
SELECT id, plan_id, trade_date, status, entry_price, exit_price, close_price, pnl_pct, mfe_pct, mae_pct, benchmark_return_pct, excess_return_pct, reason, data_quality_flag, config_version, created_at
FROM evaluations
WHERE id = ?;

-- name: ListEvaluations :many
SELECT id, plan_id, trade_date, status, entry_price, exit_price, close_price, pnl_pct, mfe_pct, mae_pct, benchmark_return_pct, excess_return_pct, reason, data_quality_flag, config_version, created_at
FROM evaluations
ORDER BY created_at DESC
LIMIT ?;
