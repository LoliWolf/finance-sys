-- config_snapshots: table
CREATE TABLE `config_snapshots` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `config_version` bigint NOT NULL,
  `source` varchar(128) COLLATE utf8mb4_general_ci NOT NULL,
  `sha256` varchar(128) COLLATE utf8mb4_general_ci NOT NULL,
  `raw_json` json NOT NULL,
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB AUTO_INCREMENT=24 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- documents: table
CREATE TABLE `documents` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `source_type` varchar(64) COLLATE utf8mb4_general_ci NOT NULL,
  `source_name` varchar(128) COLLATE utf8mb4_general_ci NOT NULL,
  `author` varchar(128) COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `institution` varchar(128) COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `title` varchar(255) COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `file_name` varchar(255) COLLATE utf8mb4_general_ci NOT NULL,
  `extension` varchar(32) COLLATE utf8mb4_general_ci NOT NULL,
  `content_type` varchar(128) COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
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
) ENGINE=InnoDB AUTO_INCREMENT=6 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

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
  `page_count` int NOT NULL DEFAULT '1',
  `content_text` longtext COLLATE utf8mb4_general_ci NOT NULL,
  `cleaned_text` longtext COLLATE utf8mb4_general_ci NOT NULL,
  `chunks_json` json NOT NULL,
  `raw_metadata_json` json NOT NULL,
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_parse_runs_document` (`document_id`,`created_at`),
  CONSTRAINT `fk_parse_runs_document` FOREIGN KEY (`document_id`) REFERENCES `documents` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB AUTO_INCREMENT=7 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- No native definition for element: idx_parse_runs_document (index)

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
) ENGINE=InnoDB AUTO_INCREMENT=2 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

-- No native definition for element: idx_trade_candidate_plans_document (index)

-- No native definition for element: idx_trade_candidate_plans_symbol_trade_date (index)

