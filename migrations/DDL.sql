-- bloggers: table
CREATE TABLE `bloggers` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `name` varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL,
  `normalized_name` varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL,
  `institution` varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `source_type` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT 'DOCUMENT',
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_bloggers_normalized_institution` (`normalized_name`,`institution`),
  KEY `idx_bloggers_name` (`name`)
) ENGINE=InnoDB AUTO_INCREMENT=17707 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- No native definition for element: idx_bloggers_name (index)

-- config_snapshots: table
CREATE TABLE `config_snapshots` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `config_version` bigint NOT NULL,
  `source` varchar(128) COLLATE utf8mb4_general_ci NOT NULL,
  `sha256` varchar(128) COLLATE utf8mb4_general_ci NOT NULL,
  `raw_json` json NOT NULL,
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB AUTO_INCREMENT=134 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- documents: table
CREATE TABLE `documents` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `author` varchar(128) COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `institution` varchar(128) COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `title` varchar(255) COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `file_name` varchar(255) COLLATE utf8mb4_general_ci NOT NULL,
  `sha256` varchar(128) COLLATE utf8mb4_general_ci NOT NULL,
  `pdf_ocr_enabled` tinyint(1) NOT NULL DEFAULT '0',
  `status` varchar(64) COLLATE utf8mb4_general_ci NOT NULL,
  `config_version` bigint NOT NULL,
  `raw_content` longblob NOT NULL,
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_documents_sha256` (`sha256`),
  KEY `idx_documents_status` (`status`),
  KEY `idx_documents_created_at` (`created_at`)
) ENGINE=InnoDB AUTO_INCREMENT=6874 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- No native definition for element: idx_documents_status (index)

-- No native definition for element: idx_documents_created_at (index)

-- instrument_resolution_runs: table
CREATE TABLE `instrument_resolution_runs` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `document_id` bigint NOT NULL,
  `parse_run_id` bigint DEFAULT NULL,
  `config_version` bigint NOT NULL,
  `agent_mode` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `route` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `status` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL,
  `schema_version` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `agent_version` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `skill_name` varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `skill_version` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `skill_hash` varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `fallback_used` tinyint(1) NOT NULL DEFAULT '0',
  `raw_target_count` int NOT NULL DEFAULT '0',
  `candidate_plan_input_count` int NOT NULL DEFAULT '0',
  `candidate_plan_count` int NOT NULL DEFAULT '0',
  `untrackable_count` int NOT NULL DEFAULT '0',
  `tool_call_count` int NOT NULL DEFAULT '0',
  `error_code` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `error_message` text CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL,
  `started_at` timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `finished_at` timestamp(3) NULL DEFAULT NULL,
  `duration_ms` int NOT NULL DEFAULT '0',
  `targets_json` json NOT NULL,
  `tool_traces_json` json NOT NULL,
  `shadow_compare_json` json NOT NULL,
  `raw_metadata_json` json NOT NULL,
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_resolution_runs_document` (`document_id`,`created_at`),
  KEY `idx_resolution_runs_parse_run` (`parse_run_id`),
  KEY `idx_resolution_runs_status` (`status`,`created_at`),
  KEY `idx_resolution_runs_mode_status` (`agent_mode`,`status`,`created_at`),
  KEY `idx_resolution_runs_skill_hash` (`skill_hash`),
  KEY `idx_resolution_runs_config_version` (`config_version`),
  CONSTRAINT `fk_resolution_runs_document` FOREIGN KEY (`document_id`) REFERENCES `documents` (`id`) ON DELETE CASCADE,
  CONSTRAINT `fk_resolution_runs_parse_run` FOREIGN KEY (`parse_run_id`) REFERENCES `parse_runs` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB AUTO_INCREMENT=9561 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- No native definition for element: idx_resolution_runs_document (index)

-- No native definition for element: idx_resolution_runs_parse_run (index)

-- No native definition for element: idx_resolution_runs_config_version (index)

-- No native definition for element: idx_resolution_runs_mode_status (index)

-- No native definition for element: idx_resolution_runs_status (index)

-- No native definition for element: idx_resolution_runs_skill_hash (index)

-- market_data_sync_missing_items: table
CREATE TABLE `market_data_sync_missing_items` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `sync_run_id` bigint NOT NULL,
  `security_master_id` bigint NOT NULL,
  `ts_code` varchar(16) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL,
  `symbol` varchar(16) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL,
  `security_name` varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `trade_date` date NOT NULL,
  `reason` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL,
  `message` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_market_sync_missing_run_ts_date` (`sync_run_id`,`ts_code`,`trade_date`),
  KEY `idx_market_sync_missing_ts_date` (`ts_code`,`trade_date`),
  KEY `fk_market_sync_missing_security` (`security_master_id`),
  CONSTRAINT `fk_market_sync_missing_run` FOREIGN KEY (`sync_run_id`) REFERENCES `market_data_sync_runs` (`id`) ON DELETE CASCADE,
  CONSTRAINT `fk_market_sync_missing_security` FOREIGN KEY (`security_master_id`) REFERENCES `security_master` (`id`)
) ENGINE=InnoDB AUTO_INCREMENT=1005263 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- No native definition for element: idx_market_sync_missing_ts_date (index)

-- market_data_sync_runs: table
CREATE TABLE `market_data_sync_runs` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `sync_type` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL,
  `provider` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT 'TUSHARE',
  `trade_date` date DEFAULT NULL,
  `status` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL,
  `expected_count` int NOT NULL DEFAULT '0',
  `fetched_count` int NOT NULL DEFAULT '0',
  `matched_count` int NOT NULL DEFAULT '0',
  `upserted_count` int NOT NULL DEFAULT '0',
  `missing_count` int NOT NULL DEFAULT '0',
  `failed_count` int NOT NULL DEFAULT '0',
  `token_alias` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `request_params_json` json NOT NULL,
  `error_code` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `error_message` text CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci,
  `queued_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `started_at` timestamp NULL DEFAULT NULL,
  `finished_at` timestamp NULL DEFAULT NULL,
  `claimed_by` varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `claimed_at` timestamp NULL DEFAULT NULL,
  `config_version` bigint NOT NULL,
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_market_sync_runs_claim` (`sync_type`,`status`,`queued_at`),
  KEY `idx_market_sync_runs_type_date` (`sync_type`,`trade_date`,`created_at`),
  KEY `idx_market_sync_runs_status` (`status`,`created_at`)
) ENGINE=InnoDB AUTO_INCREMENT=1075 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- No native definition for element: idx_market_sync_runs_type_date (index)

-- No native definition for element: idx_market_sync_runs_claim (index)

-- No native definition for element: idx_market_sync_runs_status (index)

-- parse_runs: table
CREATE TABLE `parse_runs` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `document_id` bigint NOT NULL,
  `status` varchar(64) COLLATE utf8mb4_general_ci NOT NULL,
  `parser_name` varchar(128) COLLATE utf8mb4_general_ci NOT NULL,
  `parser_version` varchar(64) COLLATE utf8mb4_general_ci NOT NULL,
  `error_message` text COLLATE utf8mb4_general_ci NOT NULL,
  `cleaned_text` longtext COLLATE utf8mb4_general_ci NOT NULL,
  `chunks_json` json NOT NULL,
  `raw_metadata_json` json NOT NULL,
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_parse_runs_document` (`document_id`,`created_at`),
  CONSTRAINT `fk_parse_runs_document` FOREIGN KEY (`document_id`) REFERENCES `documents` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB AUTO_INCREMENT=9633 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- No native definition for element: idx_parse_runs_document (index)

-- recommendation_event_evidences: table
CREATE TABLE `recommendation_event_evidences` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `recommendation_event_id` bigint NOT NULL,
  `source_document_id` bigint NOT NULL,
  `plan_id` bigint NOT NULL,
  `chunk_index` int NOT NULL DEFAULT '0',
  `evidence_text` text CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL,
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_recommendation_event_evidences_event` (`recommendation_event_id`,`id`),
  KEY `idx_recommendation_event_evidences_document` (`source_document_id`),
  KEY `fk_recommendation_event_evidences_plan` (`plan_id`),
  CONSTRAINT `fk_recommendation_event_evidences_document` FOREIGN KEY (`source_document_id`) REFERENCES `documents` (`id`) ON DELETE CASCADE,
  CONSTRAINT `fk_recommendation_event_evidences_event` FOREIGN KEY (`recommendation_event_id`) REFERENCES `recommendation_events` (`id`) ON DELETE CASCADE,
  CONSTRAINT `fk_recommendation_event_evidences_plan` FOREIGN KEY (`plan_id`) REFERENCES `trade_candidate_plans` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB AUTO_INCREMENT=18571 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- No native definition for element: idx_recommendation_event_evidences_event (index)

-- No native definition for element: idx_recommendation_event_evidences_document (index)

-- recommendation_events: table
CREATE TABLE `recommendation_events` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `blogger_id` bigint NOT NULL,
  `source_document_id` bigint NOT NULL,
  `plan_id` bigint NOT NULL,
  `parse_run_id` bigint NOT NULL,
  `symbol` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL,
  `asset_type` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `market` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `direction` varchar(16) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL,
  `recommend_date` date NOT NULL,
  `reference_price` double NOT NULL DEFAULT '0',
  `confidence` double NOT NULL DEFAULT '0',
  `status` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL,
  `thesis` text CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL,
  `dedupe_key` varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL,
  `config_version` bigint NOT NULL,
  `rule_version` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL,
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_recommendation_events_dedupe_key` (`dedupe_key`),
  KEY `idx_recommendation_events_blogger_date` (`blogger_id`,`recommend_date`),
  KEY `idx_recommendation_events_symbol_date` (`symbol`,`recommend_date`),
  KEY `idx_recommendation_events_document` (`source_document_id`,`created_at`),
  KEY `idx_recommendation_events_plan` (`plan_id`),
  KEY `fk_recommendation_events_parse_run` (`parse_run_id`),
  CONSTRAINT `fk_recommendation_events_blogger` FOREIGN KEY (`blogger_id`) REFERENCES `bloggers` (`id`),
  CONSTRAINT `fk_recommendation_events_document` FOREIGN KEY (`source_document_id`) REFERENCES `documents` (`id`) ON DELETE CASCADE,
  CONSTRAINT `fk_recommendation_events_parse_run` FOREIGN KEY (`parse_run_id`) REFERENCES `parse_runs` (`id`) ON DELETE CASCADE,
  CONSTRAINT `fk_recommendation_events_plan` FOREIGN KEY (`plan_id`) REFERENCES `trade_candidate_plans` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB AUTO_INCREMENT=17698 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- No native definition for element: idx_recommendation_events_blogger_date (index)

-- No native definition for element: idx_recommendation_events_document (index)

-- No native definition for element: idx_recommendation_events_plan (index)

-- No native definition for element: idx_recommendation_events_symbol_date (index)

-- security_aliases: table
CREATE TABLE `security_aliases` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `security_master_id` bigint NOT NULL,
  `alias` varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL,
  `normalized_alias` varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL,
  `alias_type` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT 'MANUAL',
  `source` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT 'MANUAL',
  `confidence` double NOT NULL DEFAULT '1',
  `is_active` tinyint(1) NOT NULL DEFAULT '1',
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_security_aliases_alias_master_type` (`normalized_alias`,`security_master_id`,`alias_type`),
  KEY `idx_security_aliases_normalized_active` (`normalized_alias`,`is_active`),
  KEY `idx_security_aliases_master` (`security_master_id`),
  CONSTRAINT `fk_security_aliases_master` FOREIGN KEY (`security_master_id`) REFERENCES `security_master` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB AUTO_INCREMENT=16683 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- No native definition for element: idx_security_aliases_master (index)

-- No native definition for element: idx_security_aliases_normalized_active (index)

-- security_master: table
CREATE TABLE `security_master` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `ts_code` varchar(16) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL,
  `symbol` varchar(16) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL,
  `name` varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL,
  `full_name` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `exchange` varchar(16) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL,
  `market` varchar(16) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL,
  `asset_type` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL,
  `list_status` varchar(8) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT 'L',
  `list_date` date DEFAULT NULL,
  `delist_date` date DEFAULT NULL,
  `industry` varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `is_active` tinyint(1) NOT NULL DEFAULT '1',
  `source` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT 'MANUAL',
  `raw_json` json NOT NULL,
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_security_master_ts_code` (`ts_code`),
  KEY `idx_security_master_symbol` (`symbol`),
  KEY `idx_security_master_name` (`name`),
  KEY `idx_security_master_market_symbol` (`market`,`symbol`),
  KEY `idx_security_master_asset_status` (`asset_type`,`list_status`,`is_active`)
) ENGINE=InnoDB AUTO_INCREMENT=21025 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- No native definition for element: idx_security_master_symbol (index)

-- No native definition for element: idx_security_master_name (index)

-- No native definition for element: idx_security_master_market_symbol (index)

-- No native definition for element: idx_security_master_asset_status (index)

-- stock_daily_quotes: table
CREATE TABLE `stock_daily_quotes` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `security_master_id` bigint NOT NULL,
  `ts_code` varchar(16) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL,
  `symbol` varchar(16) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL,
  `security_name` varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `exchange` varchar(16) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `market` varchar(16) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `asset_type` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT 'STOCK',
  `industry` varchar(128) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `list_status` varchar(8) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT 'L',
  `trade_date` date NOT NULL,
  `open_price` decimal(18,4) DEFAULT NULL,
  `high_price` decimal(18,4) DEFAULT NULL,
  `low_price` decimal(18,4) DEFAULT NULL,
  `close_price` decimal(18,4) DEFAULT NULL,
  `pre_close_price` decimal(18,4) DEFAULT NULL,
  `change_amount` decimal(18,4) DEFAULT NULL,
  `pct_chg` decimal(18,6) DEFAULT NULL,
  `volume` decimal(24,4) DEFAULT NULL,
  `amount` decimal(24,4) DEFAULT NULL,
  `source` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT 'TUSHARE',
  `tushare_content` json NOT NULL,
  `config_version` bigint NOT NULL,
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_stock_daily_quotes_ts_date_source` (`ts_code`,`trade_date`,`source`),
  KEY `idx_stock_daily_quotes_trade_date` (`trade_date`),
  KEY `idx_stock_daily_quotes_security_date` (`security_master_id`,`trade_date`),
  KEY `idx_stock_daily_quotes_symbol_date` (`symbol`,`trade_date`),
  KEY `idx_stock_daily_quotes_market_date` (`market`,`trade_date`),
  KEY `idx_stock_daily_quotes_industry_date` (`industry`,`trade_date`),
  KEY `idx_stock_daily_quotes_pct_chg` (`trade_date`,`pct_chg`),
  CONSTRAINT `fk_stock_daily_quotes_security` FOREIGN KEY (`security_master_id`) REFERENCES `security_master` (`id`)
) ENGINE=InnoDB AUTO_INCREMENT=1817167 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- No native definition for element: idx_stock_daily_quotes_security_date (index)

-- No native definition for element: idx_stock_daily_quotes_symbol_date (index)

-- No native definition for element: idx_stock_daily_quotes_market_date (index)

-- No native definition for element: idx_stock_daily_quotes_industry_date (index)

-- No native definition for element: idx_stock_daily_quotes_pct_chg (index)

-- No native definition for element: idx_stock_daily_quotes_trade_date (index)

-- trade_candidate_plans: table
CREATE TABLE `trade_candidate_plans` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `document_id` bigint NOT NULL,
  `parse_run_id` bigint NOT NULL,
  `analyst` varchar(128) COLLATE utf8mb4_general_ci NOT NULL,
  `institution` varchar(128) COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `symbol` varchar(32) COLLATE utf8mb4_general_ci NOT NULL,
  `asset_type` varchar(32) COLLATE utf8mb4_general_ci NOT NULL,
  `market` varchar(32) COLLATE utf8mb4_general_ci NOT NULL,
  `strategy` varchar(64) COLLATE utf8mb4_general_ci NOT NULL,
  `direction` varchar(16) COLLATE utf8mb4_general_ci NOT NULL,
  `trade_date` date NOT NULL,
  `reference_price` double NOT NULL DEFAULT '0',
  `entry_price` double NOT NULL DEFAULT '0',
  `stop_loss` double NOT NULL DEFAULT '0',
  `take_profit` double NOT NULL DEFAULT '0',
  `position_pct` double NOT NULL DEFAULT '0',
  `confidence` double NOT NULL DEFAULT '0',
  `status` varchar(64) COLLATE utf8mb4_general_ci NOT NULL,
  `thesis` text COLLATE utf8mb4_general_ci NOT NULL,
  `risks_json` json NOT NULL,
  `evidence_json` json NOT NULL,
  `pricing_note` text COLLATE utf8mb4_general_ci NOT NULL,
  `config_version` bigint NOT NULL,
  `rule_version` varchar(64) COLLATE utf8mb4_general_ci NOT NULL,
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `fk_trade_candidate_plans_parse_run` (`parse_run_id`),
  KEY `idx_trade_candidate_plans_document` (`document_id`,`created_at`),
  KEY `idx_trade_candidate_plans_symbol_trade_date` (`symbol`,`trade_date`),
  CONSTRAINT `fk_trade_candidate_plans_document` FOREIGN KEY (`document_id`) REFERENCES `documents` (`id`) ON DELETE CASCADE,
  CONSTRAINT `fk_trade_candidate_plans_parse_run` FOREIGN KEY (`parse_run_id`) REFERENCES `parse_runs` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB AUTO_INCREMENT=19588 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- No native definition for element: idx_trade_candidate_plans_document (index)

-- No native definition for element: idx_trade_candidate_plans_symbol_trade_date (index)

-- untrackable_targets: table
CREATE TABLE `untrackable_targets` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `resolution_run_id` bigint NOT NULL,
  `document_id` bigint NOT NULL,
  `parse_run_id` bigint DEFAULT NULL,
  `raw_target` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL,
  `normalized_target` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `target_kind` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL,
  `reason_code` varchar(64) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL,
  `reason_message` text CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL,
  `source` varchar(32) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `evidence_json` json NOT NULL,
  `candidates_json` json NOT NULL,
  `config_version` bigint NOT NULL,
  `is_active` tinyint(1) NOT NULL DEFAULT '1',
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_untrackable_document_active` (`document_id`,`is_active`,`created_at`),
  KEY `idx_untrackable_run` (`resolution_run_id`),
  KEY `idx_untrackable_parse_run` (`parse_run_id`),
  KEY `idx_untrackable_kind_reason` (`target_kind`,`reason_code`),
  KEY `idx_untrackable_normalized` (`normalized_target`),
  CONSTRAINT `fk_untrackable_document` FOREIGN KEY (`document_id`) REFERENCES `documents` (`id`) ON DELETE CASCADE,
  CONSTRAINT `fk_untrackable_parse_run` FOREIGN KEY (`parse_run_id`) REFERENCES `parse_runs` (`id`) ON DELETE CASCADE,
  CONSTRAINT `fk_untrackable_run` FOREIGN KEY (`resolution_run_id`) REFERENCES `instrument_resolution_runs` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB AUTO_INCREMENT=3266 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- No native definition for element: idx_untrackable_run (index)

-- No native definition for element: idx_untrackable_document_active (index)

-- No native definition for element: idx_untrackable_parse_run (index)

-- No native definition for element: idx_untrackable_normalized (index)

-- No native definition for element: idx_untrackable_kind_reason (index)

-- recommendation_evaluation_runs: table
CREATE TABLE `recommendation_evaluation_runs` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `run_type` varchar(32) NOT NULL DEFAULT 'MANUAL',
  `status` varchar(32) NOT NULL DEFAULT 'QUEUED',
  `request_params_json` json NOT NULL,
  `target_event_count` int NOT NULL DEFAULT 0,
  `evaluated_event_count` int NOT NULL DEFAULT 0,
  `window_metric_count` int NOT NULL DEFAULT 0,
  `pending_count` int NOT NULL DEFAULT 0,
  `incomplete_count` int NOT NULL DEFAULT 0,
  `failed_count` int NOT NULL DEFAULT 0,
  `worker_id` varchar(128) NOT NULL DEFAULT '',
  `queued_at` timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `started_at` timestamp(3) NULL DEFAULT NULL,
  `finished_at` timestamp(3) NULL DEFAULT NULL,
  `error_code` varchar(64) NOT NULL DEFAULT '',
  `error_message` text NOT NULL,
  `config_version` bigint NOT NULL,
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_rec_eval_runs_status` (`status`,`queued_at`),
  KEY `idx_rec_eval_runs_type_created` (`run_type`,`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- recommendation_event_window_metrics: table
CREATE TABLE `recommendation_event_window_metrics` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `recommendation_event_id` bigint NOT NULL,
  `blogger_id` bigint NOT NULL,
  `security_master_id` bigint NOT NULL DEFAULT 0,
  `ts_code` varchar(16) NOT NULL DEFAULT '',
  `symbol` varchar(32) NOT NULL,
  `security_name` varchar(128) NOT NULL DEFAULT '',
  `asset_type` varchar(32) NOT NULL DEFAULT '',
  `market` varchar(32) NOT NULL DEFAULT '',
  `industry` varchar(128) NOT NULL DEFAULT '',
  `direction` varchar(16) NOT NULL,
  `recommend_date` date NOT NULL,
  `window_days` int NOT NULL,
  `quote_source` varchar(32) NOT NULL DEFAULT 'TUSHARE',
  `status` varchar(32) NOT NULL,
  `reason_code` varchar(64) NOT NULL DEFAULT '',
  `reason_message` text NOT NULL,
  `base_date` date NULL DEFAULT NULL,
  `base_close_price` decimal(18,4) NULL DEFAULT NULL,
  `entry_date` date NULL DEFAULT NULL,
  `entry_price` decimal(18,4) NULL DEFAULT NULL,
  `exit_date` date NULL DEFAULT NULL,
  `exit_close_price` decimal(18,4) NULL DEFAULT NULL,
  `expected_quote_count` int NOT NULL DEFAULT 0,
  `actual_quote_count` int NOT NULL DEFAULT 0,
  `missing_quote_count` int NOT NULL DEFAULT 0,
  `raw_return_ratio` decimal(18,8) NULL DEFAULT NULL,
  `direction_return_ratio` decimal(18,8) NULL DEFAULT NULL,
  `max_favorable_return_ratio` decimal(18,8) NULL DEFAULT NULL,
  `max_adverse_return_ratio` decimal(18,8) NULL DEFAULT NULL,
  `max_drawdown_ratio` decimal(18,8) NULL DEFAULT NULL,
  `win_flag` tinyint(1) NULL DEFAULT NULL,
  `best_trade_date` date NULL DEFAULT NULL,
  `worst_trade_date` date NULL DEFAULT NULL,
  `evaluation_run_id` bigint NOT NULL DEFAULT 0,
  `calc_version` varchar(64) NOT NULL,
  `config_version` bigint NOT NULL,
  `calculated_at` timestamp(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_rec_window_metric` (`recommendation_event_id`,`window_days`,`quote_source`),
  KEY `idx_rec_window_metric_blogger` (`blogger_id`,`window_days`,`status`,`recommend_date`),
  KEY `idx_rec_window_metric_symbol` (`symbol`,`window_days`,`recommend_date`),
  KEY `idx_rec_window_metric_security` (`security_master_id`,`window_days`,`recommend_date`),
  KEY `idx_rec_window_metric_rank` (`window_days`,`status`,`direction_return_ratio`),
  CONSTRAINT `fk_rec_window_metric_event` FOREIGN KEY (`recommendation_event_id`) REFERENCES `recommendation_events` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
