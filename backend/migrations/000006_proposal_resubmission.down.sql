DROP INDEX idx_proposal_approval_history ON plan_change_approvals;
-- Intentionally fails if a proposal has multiple review decisions. Restore a
-- compatible backup instead of deleting legally relevant approval history.
ALTER TABLE plan_change_approvals ADD UNIQUE KEY uq_proposal_approval_decision (proposal_id);
