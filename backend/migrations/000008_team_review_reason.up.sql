ALTER TABLE teams
  ADD COLUMN review_reason VARCHAR(2000) NULL AFTER approved_at;
