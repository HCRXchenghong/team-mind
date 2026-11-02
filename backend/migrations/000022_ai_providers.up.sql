CREATE TABLE ai_providers (
  id BINARY(16) PRIMARY KEY,
  name VARCHAR(120) NOT NULL,
  base_url VARCHAR(500) NOT NULL,
  api_key_ciphertext VARBINARY(4096) NOT NULL,
  models JSON NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  priority INT NOT NULL DEFAULT 100,
  failure_count INT UNSIGNED NOT NULL DEFAULT 0,
  cooldown_until DATETIME(3) NULL,
  last_error VARCHAR(240) NULL,
  created_by BINARY(16) NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  KEY idx_ai_providers_pick (enabled, priority, cooldown_until),
  CONSTRAINT fk_ai_provider_creator FOREIGN KEY (created_by) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
