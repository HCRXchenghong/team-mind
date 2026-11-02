DROP INDEX idx_notification_visible ON notifications;
ALTER TABLE notifications DROP COLUMN visible_at;
