CREATE TABLE notification_push_subscriptions (
  id BINARY(16) PRIMARY KEY,
  user_id BINARY(16) NOT NULL,
  endpoint_hash BINARY(32) NOT NULL,
  encrypted_subscription VARBINARY(8192) NOT NULL,
  status ENUM('active','revoked') NOT NULL DEFAULT 'active',
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uq_push_subscription_endpoint (endpoint_hash),
  KEY idx_push_subscription_user_status (user_id,status),
  CONSTRAINT fk_push_subscription_user FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

ALTER TABLE notification_deliveries
  MODIFY channel ENUM('in_app','email','web_push') NOT NULL,
  MODIFY status ENUM('queued','sending','delivered','suppressed','retry','failed') NOT NULL,
  ADD COLUMN subscription_id BINARY(16) NULL AFTER recipient_user_id,
  ADD COLUMN lease_until DATETIME(3) NULL AFTER next_retry_at,
  ADD KEY idx_notification_delivery_claim (channel,status,next_retry_at,lease_until,created_at),
  ADD CONSTRAINT fk_notification_delivery_subscription FOREIGN KEY (subscription_id) REFERENCES notification_push_subscriptions(id);

ALTER TABLE notification_preferences
  ADD COLUMN email_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN push_enabled BOOLEAN NOT NULL DEFAULT FALSE;
