ALTER TABLE channel_bindings
  ADD COLUMN active_user_id BINARY(16) GENERATED ALWAYS AS (IF(unbound_at IS NULL, user_id, NULL)) STORED;

CREATE UNIQUE INDEX uq_channel_bindings_active_user ON channel_bindings(active_user_id);
