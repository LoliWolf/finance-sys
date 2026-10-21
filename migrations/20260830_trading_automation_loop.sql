-- Recommendation-driven automatic simulation trading loop.
-- Apply to expert_trade_test first and verify before applying unchanged to expert_trade.

ALTER TABLE `trading_agent_runs`
  ADD COLUMN `claim_token` char(64) NOT NULL DEFAULT '' AFTER `worker_id`,
  ADD COLUMN `claimed_at` timestamp(3) NULL DEFAULT NULL AFTER `claim_token`,
  ADD COLUMN `claim_deadline` timestamp(3) NULL DEFAULT NULL AFTER `claimed_at`,
  ADD COLUMN `decision_completed_at` timestamp(3) NULL DEFAULT NULL AFTER `claim_deadline`,
  ADD KEY `idx_trading_agent_runs_claim` (`status`,`claim_deadline`,`queued_at`);

ALTER TABLE `trading_intents`
  ADD COLUMN `ts_code` varchar(16) NOT NULL DEFAULT '' AFTER `symbol`,
  ADD COLUMN `board_type` varchar(32) NOT NULL DEFAULT 'UNKNOWN' AFTER `asset_type`,
  ADD COLUMN `position_cycle_id` bigint NULL DEFAULT NULL AFTER `candidate_plan_id`,
  ADD COLUMN `execution_status` varchar(32) NOT NULL DEFAULT 'PENDING' AFTER `status`,
  ADD COLUMN `execution_attempt_count` int NOT NULL DEFAULT 0 AFTER `execution_status`,
  ADD COLUMN `next_execution_at` timestamp(3) NULL DEFAULT NULL AFTER `execution_attempt_count`,
  ADD COLUMN `execution_claim_token` char(64) NOT NULL DEFAULT '' AFTER `next_execution_at`,
  ADD COLUMN `execution_claimed_by` varchar(128) NOT NULL DEFAULT '' AFTER `execution_claim_token`,
  ADD COLUMN `execution_claimed_at` timestamp(3) NULL DEFAULT NULL AFTER `execution_claimed_by`,
  ADD COLUMN `execution_claim_deadline` timestamp(3) NULL DEFAULT NULL AFTER `execution_claimed_at`,
  ADD COLUMN `executed_at` timestamp(3) NULL DEFAULT NULL AFTER `execution_claim_deadline`,
  ADD KEY `idx_trading_intents_board` (`board_type`,`created_at`),
  ADD KEY `idx_trading_intents_execution` (`execution_status`,`next_execution_at`,`execution_claim_deadline`),
  ADD KEY `idx_trading_intents_cycle` (`position_cycle_id`);

-- Preserve deterministic security identity for intents created before this migration
-- and prevent legacy rows from being claimed by the new execution worker.
UPDATE `trading_intents`
SET
  `ts_code` = CONCAT(`symbol`, IF(UPPER(`market`) = 'SH', '.SH', '.SZ')),
  `board_type` = CASE
    WHEN UPPER(`asset_type`) = 'ETF' THEN 'ETF'
    WHEN UPPER(`market`) = 'SZ' AND (`symbol` LIKE '300%' OR `symbol` LIKE '301%') THEN 'CHINEXT'
    WHEN UPPER(`market`) = 'SH' AND (`symbol` LIKE '688%' OR `symbol` LIKE '689%') THEN 'STAR'
    WHEN UPPER(`market`) = 'SH' THEN 'SH_MAIN'
    WHEN UPPER(`market`) = 'SZ' THEN 'SZ_MAIN'
    ELSE 'UNKNOWN'
  END,
  `execution_status` = 'LEGACY_COMPLETE'
WHERE `ts_code` = ''
  AND `board_type` = 'UNKNOWN'
  AND `symbol` REGEXP '^[0-9]{6}$'
  AND UPPER(`market`) IN ('SH','SZ')
  AND UPPER(`asset_type`) IN ('STOCK','A_SHARE','ETF');

CREATE TABLE IF NOT EXISTS `trading_daily_sessions` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `session_key` char(64) NOT NULL,
  `environment` varchar(16) NOT NULL DEFAULT 'SIMULATION',
  `provider` varchar(32) NOT NULL DEFAULT 'EASTMONEY_GM',
  `account_id` varchar(128) NOT NULL,
  `trade_date` date NOT NULL,
  `status` varchar(32) NOT NULL DEFAULT 'PENDING',
  `preflight_status` varchar(32) NOT NULL DEFAULT 'PENDING',
  `auth_state` varchar(32) NOT NULL DEFAULT 'UNKNOWN',
  `bridge_state` varchar(32) NOT NULL DEFAULT 'UNKNOWN',
  `decision_run_id` bigint NULL DEFAULT NULL,
  `opened_at` timestamp(3) NULL DEFAULT NULL,
  `closed_at` timestamp(3) NULL DEFAULT NULL,
  `preflight_json` json NOT NULL,
  `summary_json` json NOT NULL,
  `error_code` varchar(64) NOT NULL DEFAULT '',
  `error_message` text NOT NULL,
  `config_version` bigint NOT NULL,
  `created_at` timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_trading_daily_sessions_key` (`session_key`),
  UNIQUE KEY `uk_trading_daily_sessions_account_date` (`provider`,`account_id`,`trade_date`),
  KEY `idx_trading_daily_sessions_status` (`status`,`trade_date`),
  CONSTRAINT `fk_trading_daily_sessions_run` FOREIGN KEY (`decision_run_id`) REFERENCES `trading_agent_runs` (`id`) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `trading_position_cycles` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `cycle_key` char(64) NOT NULL,
  `environment` varchar(16) NOT NULL DEFAULT 'SIMULATION',
  `provider` varchar(32) NOT NULL DEFAULT 'EASTMONEY_GM',
  `account_id` varchar(128) NOT NULL,
  `symbol` varchar(16) NOT NULL,
  `ts_code` varchar(16) NOT NULL,
  `eastmoney_symbol` varchar(32) NOT NULL,
  `market` varchar(16) NOT NULL,
  `asset_type` varchar(32) NOT NULL,
  `board_type` varchar(32) NOT NULL,
  `status` varchar(32) NOT NULL DEFAULT 'OPEN',
  `source_recommendation_event_id` bigint NULL DEFAULT NULL,
  `source_buy_intent_id` bigint NULL DEFAULT NULL,
  `entry_order_id` bigint NULL DEFAULT NULL,
  `exit_order_id` bigint NULL DEFAULT NULL,
  `entry_trade_date` date NOT NULL,
  `sellable_trade_date` date NOT NULL,
  `entry_price` decimal(20,6) NOT NULL,
  `initial_volume` bigint NOT NULL,
  `current_volume` bigint NOT NULL,
  `available_volume` bigint NOT NULL DEFAULT 0,
  `stop_loss_price` decimal(20,6) NOT NULL,
  `take_profit_price` decimal(20,6) NOT NULL,
  `max_holding_trade_days` int NOT NULL,
  `holding_trade_days` int NOT NULL DEFAULT 0,
  `exit_reason` varchar(64) NOT NULL DEFAULT '',
  `opened_at` timestamp(3) NOT NULL,
  `closed_at` timestamp(3) NULL DEFAULT NULL,
  `last_evaluated_at` timestamp(3) NULL DEFAULT NULL,
  `strategy_version` varchar(64) NOT NULL,
  `rule_version` varchar(64) NOT NULL,
  `config_version` bigint NOT NULL,
  `created_at` timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_trading_position_cycles_key` (`cycle_key`),
  KEY `idx_trading_position_cycles_open` (`account_id`,`status`,`eastmoney_symbol`),
  KEY `idx_trading_position_cycles_sellable` (`status`,`sellable_trade_date`,`last_evaluated_at`),
  KEY `idx_trading_position_cycles_event` (`source_recommendation_event_id`),
  CONSTRAINT `fk_trading_position_cycles_event` FOREIGN KEY (`source_recommendation_event_id`) REFERENCES `recommendation_events` (`id`) ON DELETE SET NULL,
  CONSTRAINT `fk_trading_position_cycles_buy_intent` FOREIGN KEY (`source_buy_intent_id`) REFERENCES `trading_intents` (`id`) ON DELETE SET NULL,
  CONSTRAINT `fk_trading_position_cycles_entry_order` FOREIGN KEY (`entry_order_id`) REFERENCES `trading_orders` (`id`) ON DELETE SET NULL,
  CONSTRAINT `fk_trading_position_cycles_exit_order` FOREIGN KEY (`exit_order_id`) REFERENCES `trading_orders` (`id`) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

ALTER TABLE `trading_intents`
  ADD CONSTRAINT `fk_trading_intents_cycle` FOREIGN KEY (`position_cycle_id`) REFERENCES `trading_position_cycles` (`id`) ON DELETE SET NULL;

CREATE TABLE IF NOT EXISTS `trading_skill_decisions` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `decision_key` char(64) NOT NULL,
  `trading_agent_run_id` bigint NOT NULL,
  `trading_intent_id` bigint NULL DEFAULT NULL,
  `position_cycle_id` bigint NULL DEFAULT NULL,
  `stage` varchar(32) NOT NULL,
  `skill_name` varchar(128) NOT NULL,
  `skill_version` varchar(64) NOT NULL,
  `decision` varchar(32) NOT NULL,
  `score` decimal(18,8) NOT NULL DEFAULT 0,
  `reason` text NOT NULL,
  `input_json` json NOT NULL,
  `output_json` json NOT NULL,
  `evaluated_at` timestamp(3) NOT NULL,
  `created_at` timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_trading_skill_decisions_key` (`decision_key`),
  KEY `idx_trading_skill_decisions_run` (`trading_agent_run_id`,`stage`,`id`),
  KEY `idx_trading_skill_decisions_intent` (`trading_intent_id`,`id`),
  KEY `idx_trading_skill_decisions_cycle` (`position_cycle_id`,`id`),
  CONSTRAINT `fk_trading_skill_decisions_run` FOREIGN KEY (`trading_agent_run_id`) REFERENCES `trading_agent_runs` (`id`) ON DELETE CASCADE,
  CONSTRAINT `fk_trading_skill_decisions_intent` FOREIGN KEY (`trading_intent_id`) REFERENCES `trading_intents` (`id`) ON DELETE SET NULL,
  CONSTRAINT `fk_trading_skill_decisions_cycle` FOREIGN KEY (`position_cycle_id`) REFERENCES `trading_position_cycles` (`id`) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `trading_board_capabilities` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `capability_key` char(64) NOT NULL,
  `provider` varchar(32) NOT NULL DEFAULT 'EASTMONEY_GM',
  `environment` varchar(16) NOT NULL DEFAULT 'SIMULATION',
  `account_id` varchar(128) NOT NULL,
  `board_type` varchar(32) NOT NULL,
  `asset_type` varchar(32) NOT NULL,
  `buy_enabled` tinyint(1) NOT NULL DEFAULT 0,
  `sell_enabled` tinyint(1) NOT NULL DEFAULT 0,
  `minimum_buy_volume` bigint NOT NULL,
  `buy_step` bigint NOT NULL,
  `minimum_sell_volume` bigint NOT NULL,
  `sell_step` bigint NOT NULL,
  `residual_sell_supported` tinyint(1) NOT NULL DEFAULT 1,
  `verification_status` varchar(32) NOT NULL DEFAULT 'UNVERIFIED',
  `verified_at` timestamp(3) NULL DEFAULT NULL,
  `evidence_json` json NOT NULL,
  `trading_rule_version` varchar(64) NOT NULL,
  `config_version` bigint NOT NULL,
  `created_at` timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_trading_board_capabilities_key` (`capability_key`),
  UNIQUE KEY `uk_trading_board_capabilities_account` (`provider`,`environment`,`account_id`,`board_type`,`asset_type`),
  KEY `idx_trading_board_capabilities_status` (`verification_status`,`board_type`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
