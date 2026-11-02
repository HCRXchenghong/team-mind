CREATE TABLE sandbox_sessions (
  id BINARY(16) NOT NULL,
  user_id BINARY(16) NOT NULL,
  runtime VARCHAR(20) NOT NULL,
  container_id VARCHAR(128) NOT NULL DEFAULT '',
  workspace_path VARCHAR(512) NOT NULL,
  status VARCHAR(20) NOT NULL DEFAULT 'active',
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  last_used_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  expires_at DATETIME(3) NOT NULL,
  last_error VARCHAR(500) NOT NULL DEFAULT '',
  PRIMARY KEY (id),
  KEY idx_sandbox_sessions_user (user_id, status),
  KEY idx_sandbox_sessions_expiry (status, expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
