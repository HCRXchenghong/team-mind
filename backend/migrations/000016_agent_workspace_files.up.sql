CREATE TABLE agent_workspace_files (
  id BINARY(16) PRIMARY KEY,
  user_id BINARY(16) NOT NULL,
  name VARCHAR(160) NOT NULL,
  encrypted_content MEDIUMBLOB NOT NULL,
  content_size INT UNSIGNED NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uq_agent_workspace_user_name (user_id, name),
  KEY idx_agent_workspace_user_updated (user_id, updated_at, id),
  CONSTRAINT fk_agent_workspace_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
  CONSTRAINT chk_agent_workspace_file_size CHECK (content_size <= 65536)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
