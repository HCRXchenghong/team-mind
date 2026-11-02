CREATE TABLE team_access_policies (
  team_id BINARY(16) NOT NULL,
  ai_access ENUM('all','leads','admins') NOT NULL DEFAULT 'all',
  wechat_access ENUM('all','leads','admins') NOT NULL DEFAULT 'all',
  data_isolation ENUM('shared','units') NOT NULL DEFAULT 'shared',
  updated_by BINARY(16) NULL,
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (team_id),
  CONSTRAINT fk_team_access_policy_team FOREIGN KEY (team_id) REFERENCES teams (id) ON DELETE CASCADE,
  CONSTRAINT fk_team_access_policy_user FOREIGN KEY (updated_by) REFERENCES users (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
