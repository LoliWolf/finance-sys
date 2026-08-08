-- FinanceSys sector tracking migration.
-- Apply to expert_trade_test first, validate, then apply to expert_trade.

ALTER TABLE recommendation_event_evidences
  DROP FOREIGN KEY fk_recommendation_event_evidences_plan,
  MODIFY COLUMN plan_id BIGINT NULL;
ALTER TABLE recommendation_event_evidences
  ADD CONSTRAINT fk_rec_event_evidence_plan_sector
    FOREIGN KEY (plan_id) REFERENCES trade_candidate_plans (id) ON DELETE CASCADE;

ALTER TABLE recommendation_events
  DROP FOREIGN KEY fk_recommendation_events_plan,
  MODIFY COLUMN plan_id BIGINT NULL;
ALTER TABLE recommendation_events
  ADD CONSTRAINT fk_rec_event_plan_sector
    FOREIGN KEY (plan_id) REFERENCES trade_candidate_plans (id) ON DELETE CASCADE;

ALTER TABLE market_data_sync_missing_items
  DROP FOREIGN KEY fk_market_sync_missing_security,
  MODIFY COLUMN security_master_id BIGINT NULL;
ALTER TABLE market_data_sync_missing_items
  ADD CONSTRAINT fk_market_missing_security_sector
    FOREIGN KEY (security_master_id) REFERENCES security_master (id);

ALTER TABLE security_master
  ADD COLUMN sector_type VARCHAR(32) NOT NULL DEFAULT '' AFTER industry;

ALTER TABLE stock_daily_quotes
  ADD COLUMN sector_type VARCHAR(32) NOT NULL DEFAULT '' AFTER industry;

ALTER TABLE recommendation_event_window_metrics
  ADD COLUMN sector_type VARCHAR(32) NOT NULL DEFAULT '' AFTER industry;
