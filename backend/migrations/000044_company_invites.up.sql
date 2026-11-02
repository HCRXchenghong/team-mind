CREATE TABLE company_invites (
  id BINARY(16) NOT NULL,
  code_hash BINARY(32) NOT NULL,
  created_by BINARY(16) NOT NULL,
  expires_at DATETIME(3) NOT NULL,
  max_uses INT UNSIGNED NOT NULL DEFAULT 1,
  used_count INT UNSIGNED NOT NULL DEFAULT 0,
  note VARCHAR(160) NOT NULL DEFAULT '',
  revoked_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uq_company_invites_hash (code_hash),
  KEY idx_company_invites_open (revoked_at, expires_at),
  CONSTRAINT fk_company_invites_creator FOREIGN KEY (created_by) REFERENCES users(id),
  CONSTRAINT chk_company_invite_max_uses CHECK (max_uses > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
