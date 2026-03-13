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
    status VARCHAR(64) NOT NULL,
    config_version BIGINT NOT NULL,
    raw_content LONGBLOB NOT NULL,
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
    error_message TEXT NOT NULL,
    page_count INT NOT NULL DEFAULT 1,
    content_text LONGTEXT NOT NULL,
    cleaned_text LONGTEXT NOT NULL,
    chunks_json JSON NOT NULL,
    raw_metadata_json JSON NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT fk_parse_runs_document FOREIGN KEY (document_id) REFERENCES documents(id) ON DELETE CASCADE
);

CREATE INDEX idx_parse_runs_document ON parse_runs(document_id, created_at);

CREATE TABLE IF NOT EXISTS trade_candidate_plans (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    document_id BIGINT NOT NULL,
    parse_run_id BIGINT NOT NULL,
    analyst VARCHAR(128) NOT NULL,
    institution VARCHAR(128) NOT NULL DEFAULT '',
    symbol VARCHAR(32) NOT NULL,
    asset_type VARCHAR(32) NOT NULL,
    market VARCHAR(32) NOT NULL,
    strategy VARCHAR(64) NOT NULL,
    direction VARCHAR(16) NOT NULL,
    trade_date DATE NOT NULL,
    reference_price DOUBLE NOT NULL DEFAULT 0,
    entry_price DOUBLE NOT NULL DEFAULT 0,
    stop_loss DOUBLE NOT NULL DEFAULT 0,
    take_profit DOUBLE NOT NULL DEFAULT 0,
    position_pct DOUBLE NOT NULL DEFAULT 0,
    confidence DOUBLE NOT NULL DEFAULT 0,
    status VARCHAR(64) NOT NULL,
    thesis TEXT NOT NULL,
    risks_json JSON NOT NULL,
    evidence_json JSON NOT NULL,
    pricing_note TEXT NOT NULL,
    config_version BIGINT NOT NULL,
    rule_version VARCHAR(64) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    CONSTRAINT fk_trade_candidate_plans_document FOREIGN KEY (document_id) REFERENCES documents(id) ON DELETE CASCADE,
    CONSTRAINT fk_trade_candidate_plans_parse_run FOREIGN KEY (parse_run_id) REFERENCES parse_runs(id) ON DELETE CASCADE
);

CREATE INDEX idx_trade_candidate_plans_document ON trade_candidate_plans(document_id, created_at);
CREATE INDEX idx_trade_candidate_plans_symbol_trade_date ON trade_candidate_plans(symbol, trade_date);
