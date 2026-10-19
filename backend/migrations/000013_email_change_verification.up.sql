CREATE TABLE email_change_requests (
  id BINARY(16) PRIMARY KEY,
  user_id BINARY(16) NOT NULL,
  new_email VARCHAR(254) NOT NULL,
  token_hash BINARY(32) NOT NULL,
  expires_at DATETIME(3) NOT NULL,
  consumed_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uq_email_change_token_hash (token_hash),
  KEY idx_email_change_user_status (user_id,consumed_at,expires_at),
  CONSTRAINT fk_email_change_user FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
