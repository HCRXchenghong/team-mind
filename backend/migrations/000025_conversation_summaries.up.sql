CREATE TABLE conversation_summaries (
  conversation_id BINARY(16) NOT NULL,
  summary TEXT NOT NULL,
  message_count INT NOT NULL,
  model VARCHAR(120) NOT NULL DEFAULT '',
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (conversation_id),
  CONSTRAINT fk_conversation_summaries_conversation FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
