CREATE TABLE agent_messages (
  id BINARY(16) NOT NULL,
  team_id BINARY(16) NOT NULL,
  from_user_id BINARY(16) NOT NULL,
  to_user_id BINARY(16) NOT NULL,
  from_conversation_id BINARY(16) NULL,
  body VARCHAR(4000) NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  read_at DATETIME(3) NULL,
  PRIMARY KEY (id),
  KEY idx_agent_messages_inbox (to_user_id, team_id, read_at, created_at),
  CONSTRAINT fk_agent_messages_team FOREIGN KEY (team_id) REFERENCES teams (id) ON DELETE CASCADE,
  CONSTRAINT fk_agent_messages_from FOREIGN KEY (from_user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_agent_messages_to FOREIGN KEY (to_user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
