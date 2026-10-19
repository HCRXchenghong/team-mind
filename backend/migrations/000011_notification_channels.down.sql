-- Discard external-channel history before restoring the in_app-only enum.
DELETE FROM notification_deliveries WHERE channel <> 'in_app';

ALTER TABLE notification_deliveries
  DROP FOREIGN KEY fk_notification_delivery_subscription,
  DROP INDEX idx_notification_delivery_claim,
  DROP COLUMN lease_until,
  DROP COLUMN subscription_id,
  MODIFY status ENUM('queued','delivered','suppressed','failed') NOT NULL,
  MODIFY channel ENUM('in_app') NOT NULL DEFAULT 'in_app';
ALTER TABLE notification_preferences DROP COLUMN push_enabled, DROP COLUMN email_enabled;
DROP TABLE IF EXISTS notification_push_subscriptions;
