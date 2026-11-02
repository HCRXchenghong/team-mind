ALTER TABLE agent_runs MODIFY COLUMN status ENUM('queued','running','succeeded','failed','cancelled','waiting_approval') NOT NULL;
