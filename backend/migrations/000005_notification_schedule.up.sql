ALTER TABLE notifications ADD COLUMN visible_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3);
CREATE INDEX idx_notification_visible ON notifications(recipient_user_id, visible_at, read_at);
