CREATE TABLE auth_ip_rate_limits (
  scope_hash BINARY(32) PRIMARY KEY,
  window_started_at DATETIME(3) NOT NULL,
  request_count INT UNSIGNED NOT NULL DEFAULT 0,
  blocked_until DATETIME(3) NULL,
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  KEY idx_auth_ip_rate_cleanup (updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
