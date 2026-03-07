CREATE TABLE IF NOT EXISTS config_snapshots (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    config_version BIGINT NOT NULL,
    source VARCHAR(128) NOT NULL,
    sha256 VARCHAR(128) NOT NULL,
    raw_json JSON NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS documents (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    source_type VARCHAR(64) NOT NULL,
    source_name VARCHAR(128) NOT NULL,
    author VARCHAR(128) NOT NULL DEFAULT '',
    institution VARCHAR(128) NOT NULL DEFAULT '',
    title VARCHAR(255) NOT NULL DEFAULT '',
    file_name VARCHAR(255) NOT NULL,
    extension VARCHAR(32) NOT NULL,
    content_type VARCHAR(128) NOT NULL DEFAULT '',
    sha256 VARCHAR(128) NOT NULL,
    object_key VARCHAR(512) NOT NULL,
    status VARCHAR(64) NOT NULL,
    config_version BIGINT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_documents_sha256 (sha256)
);

CREATE INDEX idx_documents_status ON documents(status);
CREATE INDEX idx_documents_created_at ON documents(created_at);

CREATE TABLE IF NOT EXISTS parse_runs (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    document_id BIGINT NOT NULL,
    status VARCHAR(64) NOT NULL,
    parser_name VARCHAR(128) NOT NULL,
    parser_version VARCHAR(64) NOT NULL,
    requires_ocr BOOLEAN NOT NULL DEFAULT FALSE,
    error_message TEXT NOT NULL,
    page_count INT NOT NULL DEFAULT 0,
    text_density DOUBLE NOT NULL DEFAULT 0,
    content_text LONGTEXT NOT NULL,
    cleaned_text LONGTEXT NOT NULL,
    sections_json JSON NOT NULL,
    chunks_json JSON NOT NULL,
    tables_json JSON NOT NULL,
    raw_metadata_json JSON NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT fk_parse_runs_document FOREIGN KEY (document_id) REFERENCES documents(id) ON DELETE CASCADE
);

CREATE INDEX idx_parse_runs_document ON parse_runs(document_id, created_at);

CREATE TABLE IF NOT EXISTS signals (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    document_id BIGINT NOT NULL,
    parse_run_id BIGINT NOT NULL,
    expert_name VARCHAR(128) NOT NULL,
    expert_org VARCHAR(128) NOT NULL DEFAULT '',
    symbol VARCHAR(32) NOT NULL,
    asset_type VARCHAR(32) NOT NULL,
    market VARCHAR(32) NOT NULL,
    sentiment VARCHAR(32) NOT NULL,
    thesis TEXT NOT NULL,
    evidence_json JSON NOT NULL,
    risks_json JSON NOT NULL,
    confidence DOUBLE NOT NULL DEFAULT 0,
    config_version BIGINT NOT NULL,
    rule_version VARCHAR(64) NOT NULL,
    signal_date DATE NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT fk_signals_document FOREIGN KEY (document_id) REFERENCES documents(id) ON DELETE CASCADE,
    CONSTRAINT fk_signals_parse_run FOREIGN KEY (parse_run_id) REFERENCES parse_runs(id) ON DELETE CASCADE
);

CREATE INDEX idx_signals_document ON signals(document_id, created_at);
CREATE INDEX idx_signals_symbol_date ON signals(symbol, signal_date);

CREATE TABLE IF NOT EXISTS market_snapshots (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    symbol VARCHAR(32) NOT NULL,
    trade_date DATE NOT NULL,
    provider VARCHAR(64) NOT NULL,
    open DOUBLE NOT NULL,
    high DOUBLE NOT NULL,
    low DOUBLE NOT NULL,
    close DOUBLE NOT NULL,
    volume DOUBLE NOT NULL DEFAULT 0,
    turnover DOUBLE NOT NULL DEFAULT 0,
    atr DOUBLE NOT NULL DEFAULT 0,
    prev_close DOUBLE NOT NULL DEFAULT 0,
    benchmark_return_pct DOUBLE NOT NULL DEFAULT 0,
    raw_object_key VARCHAR(512) NOT NULL DEFAULT '',
    config_version BIGINT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_market_snapshots_symbol_date (symbol, trade_date)
);

CREATE INDEX idx_market_snapshots_symbol_date ON market_snapshots(symbol, trade_date);

CREATE TABLE IF NOT EXISTS plans (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    signal_id BIGINT NOT NULL,
    document_id BIGINT NOT NULL,
    symbol VARCHAR(32) NOT NULL,
    strategy VARCHAR(64) NOT NULL,
    trade_date DATE NOT NULL,
    direction VARCHAR(16) NOT NULL,
    entry_price DOUBLE NOT NULL,
    stop_loss DOUBLE NOT NULL,
    take_profit DOUBLE NOT NULL,
    invalidation_price DOUBLE NOT NULL,
    position_pct DOUBLE NOT NULL,
    status VARCHAR(64) NOT NULL,
    rationale TEXT NOT NULL,
    config_version BIGINT NOT NULL,
    rule_version VARCHAR(64) NOT NULL,
    market_snapshot_id BIGINT NOT NULL,
    approved_by VARCHAR(128) NOT NULL DEFAULT '',
    approved_at TIMESTAMP NULL DEFAULT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT fk_plans_signal FOREIGN KEY (signal_id) REFERENCES signals(id) ON DELETE CASCADE,
    CONSTRAINT fk_plans_document FOREIGN KEY (document_id) REFERENCES documents(id) ON DELETE CASCADE,
    CONSTRAINT fk_plans_market_snapshot FOREIGN KEY (market_snapshot_id) REFERENCES market_snapshots(id)
);

CREATE INDEX idx_plans_trade_date ON plans(trade_date, status);
CREATE INDEX idx_plans_symbol_trade_date ON plans(symbol, trade_date);

CREATE TABLE IF NOT EXISTS evaluations (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    plan_id BIGINT NOT NULL,
    trade_date DATE NOT NULL,
    status VARCHAR(64) NOT NULL,
    entry_price DOUBLE NOT NULL DEFAULT 0,
    exit_price DOUBLE NOT NULL DEFAULT 0,
    close_price DOUBLE NOT NULL DEFAULT 0,
    pnl_pct DOUBLE NOT NULL DEFAULT 0,
    mfe_pct DOUBLE NOT NULL DEFAULT 0,
    mae_pct DOUBLE NOT NULL DEFAULT 0,
    benchmark_return_pct DOUBLE NOT NULL DEFAULT 0,
    excess_return_pct DOUBLE NOT NULL DEFAULT 0,
    reason TEXT NOT NULL,
    data_quality_flag VARCHAR(64) NOT NULL DEFAULT '',
    config_version BIGINT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_evaluations_plan_id (plan_id),
    CONSTRAINT fk_evaluations_plan FOREIGN KEY (plan_id) REFERENCES plans(id) ON DELETE CASCADE
);

CREATE INDEX idx_evaluations_trade_date ON evaluations(trade_date, status);
