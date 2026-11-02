CREATE TABLE superadmin_invites (
  id BINARY(16) NOT NULL,
  code_hash BINARY(32) NOT NULL,
  expires_at DATETIME(3) NOT NULL,
  used_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uq_superadmin_invites_hash (code_hash),
  KEY idx_superadmin_invites_open (used_at, expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
