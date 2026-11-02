ALTER TABLE agent_messages
  ADD COLUMN auto_reply TINYINT(1) NOT NULL DEFAULT 1,
  ADD COLUMN parent_message_id BINARY(16) NULL,
  ADD KEY idx_agent_messages_auto (read_at, auto_reply, created_at);
