CREATE TABLE IF NOT EXISTS config_snapshots (
    id BIGSERIAL PRIMARY KEY,
    config_version BIGINT NOT NULL,
    source TEXT NOT NULL,
    sha256 TEXT NOT NULL,
    raw_json JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS documents (
    id BIGSERIAL PRIMARY KEY,
    source_type TEXT NOT NULL,
    source_name TEXT NOT NULL,
    author TEXT NOT NULL DEFAULT '',
    institution TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    file_name TEXT NOT NULL,
    extension TEXT NOT NULL,
    content_type TEXT NOT NULL DEFAULT '',
    sha256 TEXT NOT NULL UNIQUE,
    object_key TEXT NOT NULL,
    status TEXT NOT NULL,
    config_version BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_documents_status ON documents(status);
CREATE INDEX IF NOT EXISTS idx_documents_created_at ON documents(created_at DESC);

CREATE TABLE IF NOT EXISTS parse_runs (
    id BIGSERIAL PRIMARY KEY,
    document_id BIGINT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    status TEXT NOT NULL,
    parser_name TEXT NOT NULL,
    parser_version TEXT NOT NULL,
    requires_ocr BOOLEAN NOT NULL DEFAULT FALSE,
    error_message TEXT NOT NULL DEFAULT '',
    page_count INTEGER NOT NULL DEFAULT 0,
    text_density DOUBLE PRECISION NOT NULL DEFAULT 0,
    content_text TEXT NOT NULL DEFAULT '',
    cleaned_text TEXT NOT NULL DEFAULT '',
    sections_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    chunks_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    tables_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    raw_metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_parse_runs_document ON parse_runs(document_id, created_at DESC);

CREATE TABLE IF NOT EXISTS signals (
    id BIGSERIAL PRIMARY KEY,
    document_id BIGINT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    parse_run_id BIGINT NOT NULL REFERENCES parse_runs(id) ON DELETE CASCADE,
    expert_name TEXT NOT NULL,
    expert_org TEXT NOT NULL DEFAULT '',
    symbol TEXT NOT NULL,
    asset_type TEXT NOT NULL,
    market TEXT NOT NULL,
    sentiment TEXT NOT NULL,
    thesis TEXT NOT NULL,
    evidence_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    risks_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
    config_version BIGINT NOT NULL,
    rule_version TEXT NOT NULL,
    signal_date DATE NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_signals_document ON signals(document_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_signals_symbol_date ON signals(symbol, signal_date DESC);

CREATE TABLE IF NOT EXISTS market_snapshots (
    id BIGSERIAL PRIMARY KEY,
    symbol TEXT NOT NULL,
    trade_date DATE NOT NULL,
    provider TEXT NOT NULL,
    open DOUBLE PRECISION NOT NULL,
    high DOUBLE PRECISION NOT NULL,
    low DOUBLE PRECISION NOT NULL,
    close DOUBLE PRECISION NOT NULL,
    volume DOUBLE PRECISION NOT NULL DEFAULT 0,
    turnover DOUBLE PRECISION NOT NULL DEFAULT 0,
    atr DOUBLE PRECISION NOT NULL DEFAULT 0,
    prev_close DOUBLE PRECISION NOT NULL DEFAULT 0,
    benchmark_return_pct DOUBLE PRECISION NOT NULL DEFAULT 0,
    raw_object_key TEXT NOT NULL DEFAULT '',
    config_version BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(symbol, trade_date)
);

CREATE INDEX IF NOT EXISTS idx_market_snapshots_symbol_date ON market_snapshots(symbol, trade_date DESC);

CREATE TABLE IF NOT EXISTS plans (
    id BIGSERIAL PRIMARY KEY,
    signal_id BIGINT NOT NULL REFERENCES signals(id) ON DELETE CASCADE,
    document_id BIGINT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    symbol TEXT NOT NULL,
    strategy TEXT NOT NULL,
    trade_date DATE NOT NULL,
    direction TEXT NOT NULL,
    entry_price DOUBLE PRECISION NOT NULL,
    stop_loss DOUBLE PRECISION NOT NULL,
    take_profit DOUBLE PRECISION NOT NULL,
    invalidation_price DOUBLE PRECISION NOT NULL,
    position_pct DOUBLE PRECISION NOT NULL,
    status TEXT NOT NULL,
    rationale TEXT NOT NULL,
    config_version BIGINT NOT NULL,
    rule_version TEXT NOT NULL,
    market_snapshot_id BIGINT NOT NULL REFERENCES market_snapshots(id),
    approved_by TEXT NOT NULL DEFAULT '',
    approved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_plans_trade_date ON plans(trade_date DESC, status);
CREATE INDEX IF NOT EXISTS idx_plans_symbol_trade_date ON plans(symbol, trade_date DESC);

CREATE TABLE IF NOT EXISTS evaluations (
    id BIGSERIAL PRIMARY KEY,
    plan_id BIGINT NOT NULL UNIQUE REFERENCES plans(id) ON DELETE CASCADE,
    trade_date DATE NOT NULL,
    status TEXT NOT NULL,
    entry_price DOUBLE PRECISION NOT NULL DEFAULT 0,
    exit_price DOUBLE PRECISION NOT NULL DEFAULT 0,
    close_price DOUBLE PRECISION NOT NULL DEFAULT 0,
    pnl_pct DOUBLE PRECISION NOT NULL DEFAULT 0,
    mfe_pct DOUBLE PRECISION NOT NULL DEFAULT 0,
    mae_pct DOUBLE PRECISION NOT NULL DEFAULT 0,
    benchmark_return_pct DOUBLE PRECISION NOT NULL DEFAULT 0,
    excess_return_pct DOUBLE PRECISION NOT NULL DEFAULT 0,
    reason TEXT NOT NULL DEFAULT '',
    data_quality_flag TEXT NOT NULL DEFAULT '',
    config_version BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_evaluations_trade_date ON evaluations(trade_date DESC, status);
