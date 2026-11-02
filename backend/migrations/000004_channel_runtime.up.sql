CREATE TABLE channel_sync_states (
  channel_account_id BINARY(16) PRIMARY KEY,
  updates_buf MEDIUMTEXT NOT NULL,
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  CONSTRAINT fk_channel_sync_account FOREIGN KEY (channel_account_id) REFERENCES channel_accounts(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE channel_conversations (
  channel_account_id BINARY(16) NOT NULL,
  sender_hash BINARY(32) NOT NULL,
  user_id BINARY(16) NOT NULL,
  team_id BINARY(16) NOT NULL,
  conversation_id BINARY(16) NOT NULL,
  PRIMARY KEY (channel_account_id, sender_hash, user_id, team_id),
  UNIQUE KEY uq_channel_conversation (conversation_id),
  CONSTRAINT fk_channel_conv_account FOREIGN KEY (channel_account_id) REFERENCES channel_accounts(id),
  CONSTRAINT fk_channel_conv_user FOREIGN KEY (user_id) REFERENCES users(id),
  CONSTRAINT fk_channel_conv_team FOREIGN KEY (team_id) REFERENCES teams(id),
  CONSTRAINT fk_channel_conv_conversation FOREIGN KEY (conversation_id) REFERENCES conversations(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE channel_message_deliveries (
  channel_account_id BINARY(16) NOT NULL,
  provider_message_id VARCHAR(80) NOT NULL,
  sender_hash BINARY(32) NOT NULL,
  user_id BINARY(16) NULL,
  team_id BINARY(16) NULL,
  status ENUM('pending','sent','rejected','failed') NOT NULL DEFAULT 'pending',
  conversation_id BINARY(16) NULL,
  inbound_message_id BINARY(16) NULL,
  sender_ciphertext VARBINARY(4096) NULL,
  context_ciphertext VARBINARY(4096) NULL,
  reply_ciphertext MEDIUMBLOB NULL,
  attempts INT UNSIGNED NOT NULL DEFAULT 0,
  last_error VARCHAR(120) NULL,
  next_retry_at DATETIME(3) NULL,
  sent_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (channel_account_id, provider_message_id),
  KEY idx_channel_delivery_status (status, created_at),
  CONSTRAINT fk_channel_delivery_account FOREIGN KEY (channel_account_id) REFERENCES channel_accounts(id),
  CONSTRAINT fk_channel_delivery_user FOREIGN KEY (user_id) REFERENCES users(id),
  CONSTRAINT fk_channel_delivery_team FOREIGN KEY (team_id) REFERENCES teams(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
