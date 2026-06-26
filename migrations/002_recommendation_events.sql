-- Phase 1 recommendation event persistence.
-- Run this after the current base schema in migrations/DDL.sql.

CREATE TABLE `bloggers` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `name` varchar(128) COLLATE utf8mb4_general_ci NOT NULL,
  `normalized_name` varchar(128) COLLATE utf8mb4_general_ci NOT NULL,
  `institution` varchar(128) COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `source_type` varchar(32) COLLATE utf8mb4_general_ci NOT NULL DEFAULT 'DOCUMENT',
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_bloggers_normalized_institution` (`normalized_name`, `institution`),
  KEY `idx_bloggers_name` (`name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE `recommendation_events` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `blogger_id` bigint NOT NULL,
  `source_document_id` bigint NOT NULL,
  `plan_id` bigint NOT NULL,
  `parse_run_id` bigint NOT NULL,
  `symbol` varchar(32) COLLATE utf8mb4_general_ci NOT NULL,
  `asset_type` varchar(32) COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `market` varchar(32) COLLATE utf8mb4_general_ci NOT NULL DEFAULT '',
  `direction` varchar(16) COLLATE utf8mb4_general_ci NOT NULL,
  `recommend_date` date NOT NULL,
  `reference_price` double NOT NULL DEFAULT '0',
  `confidence` double NOT NULL DEFAULT '0',
  `status` varchar(64) COLLATE utf8mb4_general_ci NOT NULL,
  `thesis` text COLLATE utf8mb4_general_ci NOT NULL,
  `dedupe_key` varchar(128) COLLATE utf8mb4_general_ci NOT NULL,
  `config_version` bigint NOT NULL,
  `rule_version` varchar(64) COLLATE utf8mb4_general_ci NOT NULL,
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_recommendation_events_dedupe_key` (`dedupe_key`),
  KEY `idx_recommendation_events_blogger_date` (`blogger_id`, `recommend_date`),
  KEY `idx_recommendation_events_symbol_date` (`symbol`, `recommend_date`),
  KEY `idx_recommendation_events_document` (`source_document_id`, `created_at`),
  KEY `idx_recommendation_events_plan` (`plan_id`),
  CONSTRAINT `fk_recommendation_events_blogger` FOREIGN KEY (`blogger_id`) REFERENCES `bloggers` (`id`),
  CONSTRAINT `fk_recommendation_events_document` FOREIGN KEY (`source_document_id`) REFERENCES `documents` (`id`) ON DELETE CASCADE,
  CONSTRAINT `fk_recommendation_events_plan` FOREIGN KEY (`plan_id`) REFERENCES `trade_candidate_plans` (`id`) ON DELETE CASCADE,
  CONSTRAINT `fk_recommendation_events_parse_run` FOREIGN KEY (`parse_run_id`) REFERENCES `parse_runs` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;

CREATE TABLE `recommendation_event_evidences` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `recommendation_event_id` bigint NOT NULL,
  `source_document_id` bigint NOT NULL,
  `plan_id` bigint NOT NULL,
  `chunk_index` int NOT NULL DEFAULT '0',
  `evidence_text` text COLLATE utf8mb4_general_ci NOT NULL,
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_recommendation_event_evidences_event` (`recommendation_event_id`, `id`),
  KEY `idx_recommendation_event_evidences_document` (`source_document_id`),
  CONSTRAINT `fk_recommendation_event_evidences_event` FOREIGN KEY (`recommendation_event_id`) REFERENCES `recommendation_events` (`id`) ON DELETE CASCADE,
  CONSTRAINT `fk_recommendation_event_evidences_document` FOREIGN KEY (`source_document_id`) REFERENCES `documents` (`id`) ON DELETE CASCADE,
  CONSTRAINT `fk_recommendation_event_evidences_plan` FOREIGN KEY (`plan_id`) REFERENCES `trade_candidate_plans` (`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci;
