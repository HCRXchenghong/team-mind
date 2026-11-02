CREATE TABLE auth_sessions (
  id BINARY(16) PRIMARY KEY,
  user_id BINARY(16) NOT NULL,
  token_hash BINARY(32) NOT NULL,
  expires_at DATETIME(3) NOT NULL,
  revoked_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uq_auth_sessions_hash (token_hash),
  KEY idx_auth_sessions_user (user_id, revoked_at, expires_at),
  CONSTRAINT fk_auth_sessions_user FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE password_resets (
  id BINARY(16) PRIMARY KEY,
  user_id BINARY(16) NOT NULL,
  token_hash BINARY(32) NOT NULL,
  expires_at DATETIME(3) NOT NULL,
  used_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uq_password_reset_hash (token_hash),
  KEY idx_password_reset_user (user_id, used_at),
  CONSTRAINT fk_password_reset_user FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE login_failures (
  email VARCHAR(254) PRIMARY KEY,
  failures INT UNSIGNED NOT NULL DEFAULT 0,
  locked_until DATETIME(3) NULL,
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE notification_preferences (
  user_id BINARY(16) PRIMARY KEY,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  quiet_start TIME NULL,
  quiet_end TIME NULL,
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  CONSTRAINT fk_notification_pref_user FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
