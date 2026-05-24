-- config_snapshots: table
CREATE TABLE `config_snapshots` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `config_version` bigint NOT NULL,
  `source` varchar(128) COLLATE utf8mb4_general_ci NOT NULL,
  `sha256` varchar(128) COLLATE utf8mb4_general_ci NOT NULL,
  `raw_json` json NOT NULL,
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB AUTO_INCREMENT=47 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

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
) ENGINE=InnoDB AUTO_INCREMENT=46 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- No native definition for element: idx_documents_status (index)

-- No native definition for element: idx_documents_created_at (index)

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
) ENGINE=InnoDB AUTO_INCREMENT=41 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- No native definition for element: idx_parse_runs_document (index)

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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- No native definition for element: idx_security_master_symbol (index)

-- No native definition for element: idx_security_master_name (index)

-- No native definition for element: idx_security_master_market_symbol (index)

-- No native definition for element: idx_security_master_asset_status (index)

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
) ENGINE=InnoDB AUTO_INCREMENT=22 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- No native definition for element: idx_trade_candidate_plans_document (index)

-- No native definition for element: idx_trade_candidate_plans_symbol_trade_date (index)

-- instrument_resolution_runs: table
CREATE TABLE `instrument_resolution_runs` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `document_id` bigint NOT NULL,
  `parse_run_id` bigint DEFAULT NULL,
  `config_version` bigint NOT NULL,
  `agent_mode` varchar(32) COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `route` varchar(32) COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `status` varchar(32) COLLATE utf8mb4_general_ci NOT NULL,
  `schema_version` varchar(64) COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `agent_version` varchar(64) COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `skill_name` varchar(128) COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `skill_version` varchar(64) COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `skill_hash` varchar(128) COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `fallback_used` tinyint(1) NOT NULL DEFAULT '0',
  `raw_target_count` int NOT NULL DEFAULT '0',
  `candidate_plan_input_count` int NOT NULL DEFAULT '0',
  `candidate_plan_count` int NOT NULL DEFAULT '0',
  `untrackable_count` int NOT NULL DEFAULT '0',
  `tool_call_count` int NOT NULL DEFAULT '0',
  `error_code` varchar(64) COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `error_message` text COLLATE utf8mb4_general_ci NOT NULL,
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
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- untrackable_targets: table
CREATE TABLE `untrackable_targets` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `resolution_run_id` bigint NOT NULL,
  `document_id` bigint NOT NULL,
  `parse_run_id` bigint DEFAULT NULL,
  `raw_target` varchar(255) COLLATE utf8mb4_general_ci NOT NULL,
  `normalized_target` varchar(255) COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `target_kind` varchar(32) COLLATE utf8mb4_general_ci NOT NULL,
  `reason_code` varchar(64) COLLATE utf8mb4_general_ci NOT NULL,
  `reason_message` text COLLATE utf8mb4_general_ci NOT NULL,
  `source` varchar(32) COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
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
  CONSTRAINT `fk_untrackable_run` FOREIGN KEY (`resolution_run_id`) REFERENCES `instrument_resolution_runs` (`id`) ON DELETE CASCADE,
  CONSTRAINT `fk_untrackable_document` FOREIGN KEY (`document_id`) REFERENCES `documents` (`id`) ON DELETE CASCADE,
  CONSTRAINT `fk_untrackable_parse_run` FOREIGN KEY (`parse_run_id`) REFERENCES `parse_runs` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- No native definition for element: idx_resolution_runs_document (index)

-- No native definition for element: idx_resolution_runs_parse_run (index)

-- No native definition for element: idx_resolution_runs_status (index)

-- No native definition for element: idx_resolution_runs_mode_status (index)

-- No native definition for element: idx_resolution_runs_skill_hash (index)

-- No native definition for element: idx_resolution_runs_config_version (index)

-- No native definition for element: idx_untrackable_document_active (index)

-- No native definition for element: idx_untrackable_run (index)

-- No native definition for element: idx_untrackable_parse_run (index)

-- No native definition for element: idx_untrackable_kind_reason (index)

-- No native definition for element: idx_untrackable_normalized (index)
