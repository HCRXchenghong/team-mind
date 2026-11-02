ALTER TABLE channel_accounts ADD COLUMN scanner_hash BINARY(32) NULL;
ALTER TABLE channel_accounts ADD UNIQUE KEY uq_channel_scanner (provider, scanner_hash);
ALTER TABLE channel_bindings ADD COLUMN current_team_id BINARY(16) NULL;
ALTER TABLE channel_bindings ADD CONSTRAINT fk_channel_binding_team FOREIGN KEY (current_team_id) REFERENCES teams(id);
