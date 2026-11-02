UPDATE agent_runs SET status='failed', finished_at=UTC_TIMESTAMP(3) WHERE status='waiting_approval';
ALTER TABLE agent_runs MODIFY COLUMN status ENUM('queued','running','succeeded','failed','cancelled') NOT NULL;
