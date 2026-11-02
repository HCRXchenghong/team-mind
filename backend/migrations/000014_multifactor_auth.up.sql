CREATE TABLE user_mfa (
  user_id BINARY(16) PRIMARY KEY,
  secret_ciphertext VARBINARY(128) NULL,
  pending_secret_ciphertext VARBINARY(128) NULL,
  pending_expires_at DATETIME(3) NULL,
  enabled_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  CONSTRAINT fk_user_mfa_user FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE mfa_recovery_codes (
  id BINARY(16) PRIMARY KEY,
  user_id BINARY(16) NOT NULL,
  code_hash BINARY(32) NOT NULL,
  used_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uq_mfa_recovery_hash (code_hash),
  KEY idx_mfa_recovery_user_status (user_id, used_at),
  CONSTRAINT fk_mfa_recovery_user FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE auth_mfa_challenges (
  id BINARY(16) PRIMARY KEY,
  user_id BINARY(16) NOT NULL,
  token_hash BINARY(32) NOT NULL,
  remember_login BOOLEAN NOT NULL DEFAULT FALSE,
  attempts INT UNSIGNED NOT NULL DEFAULT 0,
  expires_at DATETIME(3) NOT NULL,
  consumed_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uq_auth_mfa_challenge_hash (token_hash),
  KEY idx_auth_mfa_challenge_user (user_id, consumed_at, expires_at),
  CONSTRAINT fk_auth_mfa_challenge_user FOREIGN KEY (user_id) REFERENCES users(id),
  CONSTRAINT chk_auth_mfa_challenge_attempts CHECK (attempts <= 5)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
