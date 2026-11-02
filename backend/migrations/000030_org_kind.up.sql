ALTER TABLE teams ADD COLUMN kind VARCHAR(20) NOT NULL DEFAULT 'company' AFTER name;
UPDATE teams SET kind='company' WHERE kind IS NULL OR kind='';
