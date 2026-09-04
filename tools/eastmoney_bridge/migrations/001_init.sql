PRAGMA journal_mode = WAL;
PRAGMA synchronous = FULL;
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;

CREATE TABLE IF NOT EXISTS bridge_commands (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  command_type TEXT NOT NULL,
  client_order_id TEXT NOT NULL,
  idempotency_key TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'QUEUED',
  payload_json TEXT NOT NULL,
  cl_ord_id TEXT,
  attempt_count INTEGER NOT NULL DEFAULT 0,
  next_attempt_at TEXT,
  claimed_by TEXT,
  claimed_at TEXT,
  finished_at TEXT,
  error_code TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(client_order_id, command_type),
  UNIQUE(idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_bridge_commands_claim
  ON bridge_commands(status, next_attempt_at, id);

CREATE TABLE IF NOT EXISTS bridge_order_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  event_hash TEXT NOT NULL UNIQUE,
  client_order_id TEXT NOT NULL,
  account_id TEXT NOT NULL,
  cl_ord_id TEXT NOT NULL DEFAULT '',
  exec_id TEXT NOT NULL DEFAULT '',
  event_type TEXT NOT NULL,
  provider_status TEXT NOT NULL DEFAULT '',
  normalized_status TEXT NOT NULL DEFAULT '',
  event_at TEXT NOT NULL,
  raw_payload_json TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_bridge_order_events_cursor
  ON bridge_order_events(id, event_at);

CREATE TABLE IF NOT EXISTS bridge_callback_outbox (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  event_hash TEXT NOT NULL UNIQUE,
  callback_url TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'PENDING',
  attempt_count INTEGER NOT NULL DEFAULT 0,
  next_attempt_at TEXT NOT NULL,
  last_http_status INTEGER,
  last_error TEXT NOT NULL DEFAULT '',
  delivered_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_bridge_callback_outbox_send
  ON bridge_callback_outbox(status, next_attempt_at, id);

CREATE TABLE IF NOT EXISTS bridge_snapshots (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  snapshot_version TEXT NOT NULL UNIQUE,
  account_id TEXT NOT NULL,
  terminal_state TEXT NOT NULL,
  account_state TEXT NOT NULL,
  account_json TEXT NOT NULL,
  positions_json TEXT NOT NULL,
  orders_json TEXT NOT NULL,
  executions_json TEXT NOT NULL,
  snapshot_at TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_bridge_snapshots_latest
  ON bridge_snapshots(account_id, id DESC);

CREATE TABLE IF NOT EXISTS bridge_nonces (
  nonce TEXT PRIMARY KEY,
  key_id TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_bridge_nonces_expire
  ON bridge_nonces(expires_at);

CREATE TABLE IF NOT EXISTS bridge_state (
  state_key TEXT PRIMARY KEY,
  state_value TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

INSERT OR IGNORE INTO bridge_state(state_key,state_value,updated_at)
VALUES ('kill_switch','true',strftime('%Y-%m-%dT%H:%M:%fZ','now'));

CREATE TABLE IF NOT EXISTS bridge_schema_migrations (
  version TEXT PRIMARY KEY,
  applied_at TEXT NOT NULL
);

