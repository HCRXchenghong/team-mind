ALTER TABLE projects
  ADD COLUMN version INT UNSIGNED NOT NULL DEFAULT 1;

CREATE TABLE project_change_proposals (
  id BINARY(16) PRIMARY KEY,
  team_id BINARY(16) NOT NULL,
  project_id BINARY(16) NOT NULL,
  project_version INT UNSIGNED NOT NULL,
  proposed_by BINARY(16) NOT NULL,
  source_ref BINARY(16) NULL,
  reason TEXT NOT NULL,
  impact_summary TEXT NOT NULL,
  before_snapshot JSON NOT NULL,
  proposed_snapshot JSON NOT NULL,
  status ENUM('pending','approved','rejected','needs_info','withdrawn','stale') NOT NULL DEFAULT 'pending',
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  resolved_at DATETIME(3) NULL,
  pending_project_id BINARY(16) GENERATED ALWAYS AS (CASE WHEN status='pending' THEN project_id ELSE NULL END) STORED,
  UNIQUE KEY uq_project_change_proposal_team (id,team_id),
  UNIQUE KEY uq_pending_project_change (team_id,pending_project_id),
  KEY idx_project_change_proposals_status (team_id,status,created_at),
  CONSTRAINT fk_project_change_proposal_project FOREIGN KEY (project_id,team_id) REFERENCES projects(id,team_id),
  CONSTRAINT fk_project_change_proposal_author FOREIGN KEY (proposed_by) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE project_change_approvals (
  id BINARY(16) PRIMARY KEY,
  team_id BINARY(16) NOT NULL,
  proposal_id BINARY(16) NOT NULL,
  approver_id BINARY(16) NOT NULL,
  decision ENUM('approved','rejected','needs_info','edited_approved') NOT NULL,
  reason TEXT NOT NULL,
  before_snapshot JSON NOT NULL,
  after_snapshot JSON NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_project_change_approvals_proposal (proposal_id,created_at),
  KEY idx_project_change_approvals_team_date (team_id,created_at),
  CONSTRAINT fk_project_change_approval_proposal FOREIGN KEY (proposal_id,team_id) REFERENCES project_change_proposals(id,team_id),
  CONSTRAINT fk_project_change_approval_approver FOREIGN KEY (approver_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
