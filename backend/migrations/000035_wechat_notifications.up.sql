ALTER TABLE notification_deliveries
  MODIFY channel ENUM('in_app','email','web_push','native_push','wechat') NOT NULL;

ALTER TABLE notification_preferences
  ADD COLUMN wechat_enabled TINYINT(1) NOT NULL DEFAULT 1 AFTER native_push_enabled;

ALTER TABLE channel_bindings
  ADD COLUMN sender_ciphertext VARBINARY(4096) NULL,
  ADD COLUMN context_ciphertext VARBINARY(4096) NULL;
