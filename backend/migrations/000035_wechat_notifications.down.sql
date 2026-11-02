ALTER TABLE channel_bindings DROP COLUMN context_ciphertext, DROP COLUMN sender_ciphertext;
ALTER TABLE notification_preferences DROP COLUMN wechat_enabled;
ALTER TABLE notification_deliveries
  MODIFY channel ENUM('in_app','email','web_push','native_push') NOT NULL;
