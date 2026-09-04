ALTER TABLE bridge_commands ADD COLUMN result_json TEXT NOT NULL DEFAULT '{}';

INSERT OR IGNORE INTO bridge_state(state_key,state_value,updated_at)
VALUES ('quotes_json','[]',strftime('%Y-%m-%dT%H:%M:%fZ','now'));
