CREATE TABLE member_change_proposals (
  id BINARY(16) PRIMARY KEY,
  team_id BINARY(16) NOT NULL,
  target_user_id BINARY(16) NOT NULL,
  proposed_by BINARY(16) NOT NULL,
  source_ref BINARY(16) NULL,
  before_snapshot JSON NOT NULL,
  proposed_snapshot JSON NOT NULL,
  reason TEXT NOT NULL,
  impact_summary TEXT NOT NULL,
  status ENUM('pending','approved','rejected','needs_info','stale','withdrawn') NOT NULL DEFAULT 'pending',
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  resolved_at DATETIME(3) NULL,
  active_target_user_id BINARY(16) GENERATED ALWAYS AS (CASE WHEN status IN ('pending','needs_info') THEN target_user_id ELSE NULL END) STORED,
  UNIQUE KEY uq_member_change_proposal_team (id,team_id),
  UNIQUE KEY uq_pending_member_change (team_id,active_target_user_id),
  KEY idx_member_change_target (team_id,target_user_id),
  KEY idx_member_change_proposals_status (team_id,status,created_at),
  CONSTRAINT fk_member_change_proposal_team FOREIGN KEY (team_id) REFERENCES teams(id),
  CONSTRAINT fk_member_change_proposal_target FOREIGN KEY (target_user_id) REFERENCES users(id),
  CONSTRAINT fk_member_change_proposal_author FOREIGN KEY (proposed_by) REFERENCES users(id),
  CONSTRAINT fk_member_change_proposal_source FOREIGN KEY (source_ref) REFERENCES conversations(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE member_change_approvals (
  id BINARY(16) PRIMARY KEY,
  team_id BINARY(16) NOT NULL,
  proposal_id BINARY(16) NOT NULL,
  approver_id BINARY(16) NOT NULL,
  decision ENUM('approved','rejected','needs_info','edited_approved') NOT NULL,
  reason TEXT NOT NULL,
  before_snapshot JSON NOT NULL,
  after_snapshot JSON NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_member_change_approvals_proposal (proposal_id,created_at),
  KEY idx_member_change_approvals_team_date (team_id,created_at),
  CONSTRAINT fk_member_change_approval_proposal FOREIGN KEY (proposal_id,team_id) REFERENCES member_change_proposals(id,team_id) ON DELETE CASCADE,
  CONSTRAINT fk_member_change_approval_approver FOREIGN KEY (approver_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
