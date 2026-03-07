-- name: InsertConfigSnapshot :one
INSERT INTO config_snapshots (
    config_version,
    source,
    sha256,
    raw_json
) VALUES (
    $1, $2, $3, $4
)
RETURNING id, config_version, source, sha256, raw_json, created_at;

-- name: InsertDocument :one
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
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
)
RETURNING id, source_type, source_name, author, institution, title, file_name, extension, content_type, sha256, object_key, status, config_version, created_at, updated_at;

-- name: GetDocumentByID :one
SELECT id, source_type, source_name, author, institution, title, file_name, extension, content_type, sha256, object_key, status, config_version, created_at, updated_at
FROM documents
WHERE id = $1;

-- name: GetDocumentBySHA :one
SELECT id, source_type, source_name, author, institution, title, file_name, extension, content_type, sha256, object_key, status, config_version, created_at, updated_at
FROM documents
WHERE sha256 = $1;

-- name: ListDocuments :many
SELECT id, source_type, source_name, author, institution, title, file_name, extension, content_type, sha256, object_key, status, config_version, created_at, updated_at
FROM documents
ORDER BY created_at DESC
LIMIT $1;

-- name: ListDocumentsByStatus :many
SELECT id, source_type, source_name, author, institution, title, file_name, extension, content_type, sha256, object_key, status, config_version, created_at, updated_at
FROM documents
WHERE status = $1
ORDER BY created_at ASC
LIMIT $2;

-- name: UpdateDocumentStatus :exec
UPDATE documents
SET status = $2, updated_at = NOW()
WHERE id = $1;

-- name: InsertParseRun :one
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
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
)
RETURNING id, document_id, status, parser_name, parser_version, requires_ocr, error_message, page_count, text_density, content_text, cleaned_text, sections_json, chunks_json, tables_json, raw_metadata_json, created_at, updated_at;

-- name: GetLatestParseRunByDocumentID :one
SELECT id, document_id, status, parser_name, parser_version, requires_ocr, error_message, page_count, text_density, content_text, cleaned_text, sections_json, chunks_json, tables_json, raw_metadata_json, created_at, updated_at
FROM parse_runs
WHERE document_id = $1
ORDER BY created_at DESC
LIMIT 1;

-- name: InsertSignal :one
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
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15
)
RETURNING id, document_id, parse_run_id, expert_name, expert_org, symbol, asset_type, market, sentiment, thesis, evidence_json, risks_json, confidence, config_version, rule_version, signal_date, created_at;

-- name: ListSignalsByDocumentID :many
SELECT id, document_id, parse_run_id, expert_name, expert_org, symbol, asset_type, market, sentiment, thesis, evidence_json, risks_json, confidence, config_version, rule_version, signal_date, created_at
FROM signals
WHERE document_id = $1
ORDER BY created_at ASC;

-- name: GetSignalByID :one
SELECT id, document_id, parse_run_id, expert_name, expert_org, symbol, asset_type, market, sentiment, thesis, evidence_json, risks_json, confidence, config_version, rule_version, signal_date, created_at
FROM signals
WHERE id = $1;

-- name: InsertMarketSnapshot :one
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
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
)
ON CONFLICT (symbol, trade_date)
DO UPDATE SET
    provider = EXCLUDED.provider,
    open = EXCLUDED.open,
    high = EXCLUDED.high,
    low = EXCLUDED.low,
    close = EXCLUDED.close,
    volume = EXCLUDED.volume,
    turnover = EXCLUDED.turnover,
    atr = EXCLUDED.atr,
    prev_close = EXCLUDED.prev_close,
    benchmark_return_pct = EXCLUDED.benchmark_return_pct,
    raw_object_key = EXCLUDED.raw_object_key,
    config_version = EXCLUDED.config_version
RETURNING id, symbol, trade_date, provider, open, high, low, close, volume, turnover, atr, prev_close, benchmark_return_pct, raw_object_key, config_version, created_at;

-- name: GetMarketSnapshotBySymbolDate :one
SELECT id, symbol, trade_date, provider, open, high, low, close, volume, turnover, atr, prev_close, benchmark_return_pct, raw_object_key, config_version, created_at
FROM market_snapshots
WHERE symbol = $1 AND trade_date = $2;

-- name: InsertPlan :one
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
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16
)
RETURNING id, signal_id, document_id, symbol, strategy, trade_date, direction, entry_price, stop_loss, take_profit, invalidation_price, position_pct, status, rationale, config_version, rule_version, market_snapshot_id, approved_by, approved_at, created_at, updated_at;

-- name: ListPlans :many
SELECT id, signal_id, document_id, symbol, strategy, trade_date, direction, entry_price, stop_loss, take_profit, invalidation_price, position_pct, status, rationale, config_version, rule_version, market_snapshot_id, approved_by, approved_at, created_at, updated_at
FROM plans
ORDER BY created_at DESC
LIMIT $1;

-- name: GetPlanByID :one
SELECT id, signal_id, document_id, symbol, strategy, trade_date, direction, entry_price, stop_loss, take_profit, invalidation_price, position_pct, status, rationale, config_version, rule_version, market_snapshot_id, approved_by, approved_at, created_at, updated_at
FROM plans
WHERE id = $1;

-- name: ApprovePlan :one
UPDATE plans
SET status = 'APPROVED',
    approved_by = $2,
    approved_at = NOW(),
    updated_at = NOW()
WHERE id = $1
RETURNING id, signal_id, document_id, symbol, strategy, trade_date, direction, entry_price, stop_loss, take_profit, invalidation_price, position_pct, status, rationale, config_version, rule_version, market_snapshot_id, approved_by, approved_at, created_at, updated_at;

-- name: ListApprovedPlansForTradeDateWithoutEvaluation :many
SELECT p.id, p.signal_id, p.document_id, p.symbol, p.strategy, p.trade_date, p.direction, p.entry_price, p.stop_loss, p.take_profit, p.invalidation_price, p.position_pct, p.status, p.rationale, p.config_version, p.rule_version, p.market_snapshot_id, p.approved_by, p.approved_at, p.created_at, p.updated_at
FROM plans p
LEFT JOIN evaluations e ON e.plan_id = p.id
WHERE p.status = 'APPROVED'
  AND p.trade_date = $1
  AND e.id IS NULL
ORDER BY p.created_at ASC;

-- name: InsertEvaluation :one
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
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
)
RETURNING id, plan_id, trade_date, status, entry_price, exit_price, close_price, pnl_pct, mfe_pct, mae_pct, benchmark_return_pct, excess_return_pct, reason, data_quality_flag, config_version, created_at;

-- name: ListEvaluations :many
SELECT id, plan_id, trade_date, status, entry_price, exit_price, close_price, pnl_pct, mfe_pct, mae_pct, benchmark_return_pct, excess_return_pct, reason, data_quality_flag, config_version, created_at
FROM evaluations
ORDER BY created_at DESC
LIMIT $1;
