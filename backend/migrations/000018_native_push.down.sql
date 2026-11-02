DELETE FROM notification_deliveries WHERE channel='native_push';

ALTER TABLE notification_deliveries
  DROP FOREIGN KEY fk_notification_native_device,
  DROP INDEX idx_notification_native_device,
  DROP COLUMN native_device_id,
  MODIFY channel ENUM('in_app','email','web_push') NOT NULL;

ALTER TABLE notification_preferences DROP COLUMN native_push_enabled;
DROP TABLE IF EXISTS notification_native_devices;
