-- Persist provider-reported account fee totals and make fill-level commission
-- provenance explicit. Apply to expert_trade_test first, verify, then apply to
-- expert_trade. The migration is intentionally one-shot and should be tracked
-- by the deployment runbook before rerunning it.

ALTER TABLE `trading_account_snapshots`
  ADD COLUMN `cumulative_inout` decimal(24,6) NOT NULL DEFAULT 0 AFTER `floating_pnl`,
  ADD COLUMN `cumulative_trade` decimal(24,6) NOT NULL DEFAULT 0 AFTER `cumulative_inout`,
  ADD COLUMN `last_trade` decimal(24,6) NOT NULL DEFAULT 0 AFTER `cumulative_commission`,
  ADD COLUMN `last_pnl` decimal(24,6) NOT NULL DEFAULT 0 AFTER `last_trade`,
  ADD COLUMN `last_commission` decimal(24,6) NOT NULL DEFAULT 0 AFTER `last_pnl`,
  ADD COLUMN `commission_data_status` varchar(32) NOT NULL DEFAULT 'UNAVAILABLE' AFTER `last_commission`;

ALTER TABLE `trading_fills`
  ADD COLUMN `commission_status` varchar(32) NOT NULL DEFAULT 'PENDING' AFTER `commission`,
  ADD COLUMN `commission_source` varchar(64) NOT NULL DEFAULT '' AFTER `commission_status`,
  ADD COLUMN `commission_evidence_json` json NULL AFTER `commission_source`,
  ADD COLUMN `commission_reconciled_at` timestamp(3) NULL DEFAULT NULL AFTER `commission_evidence_json`,
  ADD KEY `idx_trading_fills_commission_status` (`commission_status`,`traded_at`);
