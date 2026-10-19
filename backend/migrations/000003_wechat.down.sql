ALTER TABLE channel_bindings DROP FOREIGN KEY fk_channel_binding_team;
ALTER TABLE channel_bindings DROP COLUMN current_team_id;
ALTER TABLE channel_accounts DROP INDEX uq_channel_scanner;
ALTER TABLE channel_accounts DROP COLUMN scanner_hash;
