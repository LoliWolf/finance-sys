-- Apply once to expert_trade_test, verify, then expert_trade. No USE clause.
-- No legacy order, intent or fill is deleted or re-queued by this migration.
ALTER TABLE trading_orders
  ADD UNIQUE KEY uk_trading_orders_one_per_intent (trading_intent_id);

ALTER TABLE trading_position_cycles
  ADD COLUMN open_symbol_key char(64) NULL DEFAULT NULL AFTER cycle_key,
  MODIFY COLUMN sellable_trade_date date NULL DEFAULT NULL,
  ADD UNIQUE KEY uk_trading_position_cycles_open_symbol (open_symbol_key),
  ADD UNIQUE KEY uk_trading_position_cycles_entry_order (entry_order_id);

UPDATE trading_position_cycles
SET open_symbol_key=SHA2(CONCAT(account_id,'|',eastmoney_symbol),256)
WHERE status <> 'CLOSED';

ALTER TABLE trading_reconciliation_runs
  ADD COLUMN worker_id varchar(128) NOT NULL DEFAULT '' AFTER status,
  ADD COLUMN claim_token char(64) NOT NULL DEFAULT '' AFTER worker_id,
  ADD COLUMN claim_deadline timestamp(3) NULL DEFAULT NULL AFTER claim_token,
  ADD KEY idx_trading_reconciliation_claim (status,claim_deadline,started_at);
