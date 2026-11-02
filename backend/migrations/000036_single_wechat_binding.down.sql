DROP INDEX uq_channel_bindings_active_user ON channel_bindings;
ALTER TABLE channel_bindings DROP COLUMN active_user_id;
