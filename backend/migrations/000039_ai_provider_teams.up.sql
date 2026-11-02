ALTER TABLE ai_providers
  ADD COLUMN team_id BINARY(16) NULL AFTER id,
  ADD KEY idx_ai_providers_team (team_id, enabled, priority),
  ADD CONSTRAINT fk_ai_providers_team FOREIGN KEY (team_id) REFERENCES teams (id) ON DELETE CASCADE;
