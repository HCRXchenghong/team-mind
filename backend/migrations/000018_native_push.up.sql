CREATE TABLE notification_native_devices (
  id BINARY(16) PRIMARY KEY,
  user_id BINARY(16) NOT NULL,
  platform ENUM('android','ios') NOT NULL,
  token_hash BINARY(32) NOT NULL,
  encrypted_token VARBINARY(8192) NOT NULL,
  status ENUM('active','revoked') NOT NULL DEFAULT 'active',
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uq_native_device_token (token_hash),
  KEY idx_native_device_user_status (user_id,status),
  CONSTRAINT fk_native_device_user FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

ALTER TABLE notification_deliveries
  MODIFY channel ENUM('in_app','email','web_push','native_push') NOT NULL,
  ADD COLUMN native_device_id BINARY(16) NULL AFTER subscription_id,
  ADD KEY idx_notification_native_device (native_device_id),
  ADD CONSTRAINT fk_notification_native_device FOREIGN KEY (native_device_id) REFERENCES notification_native_devices(id);

ALTER TABLE notification_preferences
  ADD COLUMN native_push_enabled BOOLEAN NOT NULL DEFAULT FALSE;
