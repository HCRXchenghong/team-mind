-- MySQL 8.0+ | All DATETIME values are UTC. The application sets each DB session to +00:00.
CREATE TABLE users (
  id BINARY(16) PRIMARY KEY,
  email VARCHAR(254) NOT NULL,
  display_name VARCHAR(120) NOT NULL,
  password_hash VARCHAR(255) NOT NULL,
  status ENUM('active','disabled','pending_deletion') NOT NULL DEFAULT 'active',
  timezone VARCHAR(80) NOT NULL DEFAULT 'Asia/Shanghai',
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  deleted_at DATETIME(3) NULL,
  UNIQUE KEY uq_users_email (email),
  KEY idx_users_status (status, deleted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE platform_roles (
  user_id BINARY(16) NOT NULL,
  role ENUM('super_admin') NOT NULL,
  granted_by BINARY(16) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (user_id, role),
  CONSTRAINT fk_platform_role_user FOREIGN KEY (user_id) REFERENCES users(id),
  CONSTRAINT fk_platform_role_granter FOREIGN KEY (granted_by) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE teams (
  id BINARY(16) PRIMARY KEY,
  name VARCHAR(160) NOT NULL,
  slug VARCHAR(180) NOT NULL,
  status ENUM('pending_review','active','suspended','deleted') NOT NULL DEFAULT 'pending_review',
  created_by BINARY(16) NOT NULL,
  approved_by BINARY(16) NULL,
  approved_at DATETIME(3) NULL,
  settings JSON NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  deleted_at DATETIME(3) NULL,
  UNIQUE KEY uq_teams_slug (slug),
  KEY idx_teams_status_created (status, created_at),
  CONSTRAINT fk_teams_creator FOREIGN KEY (created_by) REFERENCES users(id),
  CONSTRAINT fk_teams_approver FOREIGN KEY (approved_by) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE team_memberships (
  team_id BINARY(16) NOT NULL,
  user_id BINARY(16) NOT NULL,
  role ENUM('team_owner','team_admin','member') NOT NULL DEFAULT 'member',
  status ENUM('invited','active','suspended','left') NOT NULL DEFAULT 'invited',
  joined_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (team_id, user_id),
  KEY idx_membership_user_status (user_id, status),
  CONSTRAINT fk_memberships_team FOREIGN KEY (team_id) REFERENCES teams(id),
  CONSTRAINT fk_memberships_user FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE team_invitations (
  id BINARY(16) PRIMARY KEY,
  team_id BINARY(16) NOT NULL,
  token_hash BINARY(32) NOT NULL,
  role ENUM('member') NOT NULL DEFAULT 'member',
  created_by BINARY(16) NOT NULL,
  expires_at DATETIME(3) NOT NULL,
  max_uses INT UNSIGNED NOT NULL DEFAULT 1,
  used_count INT UNSIGNED NOT NULL DEFAULT 0,
  revoked_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uq_invitation_token_hash (token_hash),
  KEY idx_invitation_team_expiry (team_id, expires_at, revoked_at),
  CONSTRAINT fk_invitations_team FOREIGN KEY (team_id) REFERENCES teams(id),
  CONSTRAINT fk_invitations_creator FOREIGN KEY (created_by) REFERENCES users(id),
  CONSTRAINT chk_invitation_max_uses CHECK (max_uses > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE projects (
  id BINARY(16) PRIMARY KEY,
  team_id BINARY(16) NOT NULL,
  name VARCHAR(200) NOT NULL,
  description TEXT NOT NULL,
  status ENUM('planning','active','paused','completed','archived') NOT NULL DEFAULT 'planning',
  start_date DATE NULL,
  due_date DATE NULL,
  created_by BINARY(16) NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  deleted_at DATETIME(3) NULL,
  UNIQUE KEY uq_project_id_team (id, team_id),
  KEY idx_projects_team_status (team_id, status, deleted_at),
  CONSTRAINT fk_projects_team FOREIGN KEY (team_id) REFERENCES teams(id),
  CONSTRAINT fk_projects_creator FOREIGN KEY (created_by) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE tasks (
  id BINARY(16) PRIMARY KEY,
  team_id BINARY(16) NOT NULL,
  project_id BINARY(16) NOT NULL,
  title VARCHAR(240) NOT NULL,
  description TEXT NOT NULL,
  created_by BINARY(16) NOT NULL,
  assignee_id BINARY(16) NULL,
  status ENUM('todo','doing','blocked','review','done','cancelled') NOT NULL DEFAULT 'todo',
  priority ENUM('low','medium','high','urgent') NOT NULL DEFAULT 'medium',
  start_date DATE NULL,
  due_date DATE NULL,
  estimate_days DECIMAL(7,2) NULL,
  progress TINYINT UNSIGNED NOT NULL DEFAULT 0,
  is_milestone BOOLEAN NOT NULL DEFAULT FALSE,
  source ENUM('manual','ai_proposal','conversation') NOT NULL DEFAULT 'manual',
  version INT UNSIGNED NOT NULL DEFAULT 1,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  deleted_at DATETIME(3) NULL,
  UNIQUE KEY uq_task_id_team (id, team_id),
  KEY idx_tasks_team_project_due (team_id, project_id, due_date, status),
  KEY idx_tasks_team_assignee_status (team_id, assignee_id, status),
  CONSTRAINT fk_tasks_team FOREIGN KEY (team_id) REFERENCES teams(id),
  CONSTRAINT fk_tasks_project_team FOREIGN KEY (project_id, team_id) REFERENCES projects(id, team_id),
  CONSTRAINT fk_tasks_creator FOREIGN KEY (created_by) REFERENCES users(id),
  CONSTRAINT fk_tasks_assignee FOREIGN KEY (assignee_id) REFERENCES users(id),
  CONSTRAINT chk_task_progress CHECK (progress <= 100)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE task_dependencies (
  team_id BINARY(16) NOT NULL,
  task_id BINARY(16) NOT NULL,
  depends_on_task_id BINARY(16) NOT NULL,
  dependency_type ENUM('finish_to_start','start_to_start','finish_to_finish','start_to_finish') NOT NULL DEFAULT 'finish_to_start',
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (team_id, task_id, depends_on_task_id),
  CONSTRAINT fk_dependency_task FOREIGN KEY (task_id, team_id) REFERENCES tasks(id, team_id),
  CONSTRAINT fk_dependency_prereq FOREIGN KEY (depends_on_task_id, team_id) REFERENCES tasks(id, team_id),
  CONSTRAINT chk_no_self_dependency CHECK (task_id <> depends_on_task_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE task_comments (
  id BINARY(16) PRIMARY KEY,
  team_id BINARY(16) NOT NULL,
  task_id BINARY(16) NOT NULL,
  author_id BINARY(16) NOT NULL,
  body TEXT NOT NULL,
  source_message_id BINARY(16) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  deleted_at DATETIME(3) NULL,
  KEY idx_task_comments_team_task (team_id, task_id, created_at),
  CONSTRAINT fk_comments_task_team FOREIGN KEY (task_id, team_id) REFERENCES tasks(id, team_id),
  CONSTRAINT fk_comments_author FOREIGN KEY (author_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE plan_change_proposals (
  id BINARY(16) PRIMARY KEY,
  team_id BINARY(16) NOT NULL,
  project_id BINARY(16) NOT NULL,
  task_id BINARY(16) NOT NULL,
  task_version INT UNSIGNED NOT NULL,
  proposed_by BINARY(16) NOT NULL,
  source_type ENUM('manual','conversation','agent_event') NOT NULL DEFAULT 'manual',
  source_ref BINARY(16) NULL,
  reason TEXT NOT NULL,
  impact_summary TEXT NOT NULL,
  before_snapshot JSON NOT NULL,
  proposed_snapshot JSON NOT NULL,
  status ENUM('pending','approved','rejected','needs_info','withdrawn') NOT NULL DEFAULT 'pending',
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  resolved_at DATETIME(3) NULL,
  pending_task_id BINARY(16) GENERATED ALWAYS AS (CASE WHEN status = 'pending' THEN task_id ELSE NULL END) STORED,
  UNIQUE KEY uq_proposal_id_team (id, team_id),
  UNIQUE KEY uq_pending_plan_per_task (team_id, pending_task_id),
  KEY idx_plan_proposals_team_status (team_id, status, created_at),
  CONSTRAINT fk_proposal_task_team FOREIGN KEY (task_id, team_id) REFERENCES tasks(id, team_id),
  CONSTRAINT fk_proposal_project_team FOREIGN KEY (project_id, team_id) REFERENCES projects(id, team_id),
  CONSTRAINT fk_proposal_author FOREIGN KEY (proposed_by) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE plan_change_approvals (
  id BINARY(16) PRIMARY KEY,
  team_id BINARY(16) NOT NULL,
  proposal_id BINARY(16) NOT NULL,
  approver_id BINARY(16) NOT NULL,
  decision ENUM('approved','rejected','needs_info','edited_approved') NOT NULL,
  reason TEXT NOT NULL,
  before_snapshot JSON NOT NULL,
  after_snapshot JSON NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uq_proposal_approval_decision (proposal_id),
  KEY idx_approvals_team_date (team_id, created_at),
  CONSTRAINT fk_approval_proposal_team FOREIGN KEY (proposal_id, team_id) REFERENCES plan_change_proposals(id, team_id),
  CONSTRAINT fk_approval_approver FOREIGN KEY (approver_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE task_plan_versions (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  team_id BINARY(16) NOT NULL,
  task_id BINARY(16) NOT NULL,
  version INT UNSIGNED NOT NULL,
  snapshot JSON NOT NULL,
  change_source ENUM('manual','proposal_approved','progress_update','migration') NOT NULL,
  source_ref BINARY(16) NULL,
  actor_id BINARY(16) NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uq_task_plan_version (team_id, task_id, version),
  CONSTRAINT fk_task_version_task FOREIGN KEY (task_id, team_id) REFERENCES tasks(id, team_id),
  CONSTRAINT fk_task_version_actor FOREIGN KEY (actor_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE conversations (
  id BINARY(16) PRIMARY KEY,
  owner_user_id BINARY(16) NOT NULL,
  team_id BINARY(16) NULL,
  visibility ENUM('personal','team_context') NOT NULL DEFAULT 'personal',
  title VARCHAR(200) NOT NULL DEFAULT '',
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  KEY idx_conversation_owner_update (owner_user_id, updated_at),
  KEY idx_conversation_team_owner (team_id, owner_user_id),
  CONSTRAINT fk_conversation_owner FOREIGN KEY (owner_user_id) REFERENCES users(id),
  CONSTRAINT fk_conversation_team FOREIGN KEY (team_id) REFERENCES teams(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE conversation_messages (
  id BINARY(16) PRIMARY KEY,
  conversation_id BINARY(16) NOT NULL,
  role ENUM('user','assistant','tool') NOT NULL,
  content MEDIUMTEXT NOT NULL,
  provider VARCHAR(80) NULL,
  model VARCHAR(120) NULL,
  token_usage JSON NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_messages_conversation_time (conversation_id, created_at),
  CONSTRAINT fk_messages_conversation FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE user_memories (
  id BINARY(16) PRIMARY KEY,
  owner_user_id BINARY(16) NOT NULL,
  category ENUM('profile','experience','current_work','operational_knowledge') NOT NULL,
  content TEXT NOT NULL,
  source_message_id BINARY(16) NULL,
  confidence DECIMAL(4,3) NOT NULL DEFAULT 0.500,
  importance TINYINT UNSIGNED NOT NULL DEFAULT 3,
  sensitivity ENUM('normal','sensitive','restricted') NOT NULL DEFAULT 'normal',
  confirmed_at DATETIME(3) NULL,
  review_at DATETIME(3) NULL,
  expires_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  deleted_at DATETIME(3) NULL,
  KEY idx_user_memory_owner_category (owner_user_id, category, deleted_at),
  CONSTRAINT fk_user_memory_owner FOREIGN KEY (owner_user_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE team_memories (
  id BINARY(16) PRIMARY KEY,
  team_id BINARY(16) NOT NULL,
  created_by BINARY(16) NOT NULL,
  category VARCHAR(80) NOT NULL,
  content TEXT NOT NULL,
  source_message_id BINARY(16) NULL,
  confidence DECIMAL(4,3) NOT NULL DEFAULT 0.500,
  importance TINYINT UNSIGNED NOT NULL DEFAULT 3,
  sensitivity ENUM('normal','sensitive','restricted') NOT NULL DEFAULT 'normal',
  confirmed_at DATETIME(3) NULL,
  review_at DATETIME(3) NULL,
  expires_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  deleted_at DATETIME(3) NULL,
  KEY idx_team_memories_scope (team_id, category, deleted_at),
  CONSTRAINT fk_team_memory_team FOREIGN KEY (team_id) REFERENCES teams(id),
  CONSTRAINT fk_team_memory_creator FOREIGN KEY (created_by) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE agent_runs (
  id BINARY(16) PRIMARY KEY,
  user_id BINARY(16) NOT NULL,
  team_id BINARY(16) NULL,
  conversation_id BINARY(16) NULL,
  provider VARCHAR(80) NOT NULL,
  model VARCHAR(120) NULL,
  status ENUM('queued','running','succeeded','failed','cancelled') NOT NULL,
  context_summary JSON NOT NULL,
  started_at DATETIME(3) NULL,
  finished_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_agent_runs_user_date (user_id, created_at),
  KEY idx_agent_runs_team_date (team_id, created_at),
  CONSTRAINT fk_agent_run_user FOREIGN KEY (user_id) REFERENCES users(id),
  CONSTRAINT fk_agent_run_team FOREIGN KEY (team_id) REFERENCES teams(id),
  CONSTRAINT fk_agent_run_conversation FOREIGN KEY (conversation_id) REFERENCES conversations(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE agent_tool_calls (
  id BINARY(16) PRIMARY KEY,
  run_id BINARY(16) NOT NULL,
  user_id BINARY(16) NOT NULL,
  team_id BINARY(16) NULL,
  tool_name VARCHAR(100) NOT NULL,
  argument_summary JSON NOT NULL,
  result_summary JSON NULL,
  approval_status ENUM('not_required','pending','approved','rejected') NOT NULL,
  status ENUM('succeeded','failed','denied') NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_tool_calls_team_time (team_id, created_at),
  CONSTRAINT fk_tool_call_run FOREIGN KEY (run_id) REFERENCES agent_runs(id),
  CONSTRAINT fk_tool_call_user FOREIGN KEY (user_id) REFERENCES users(id),
  CONSTRAINT fk_tool_call_team FOREIGN KEY (team_id) REFERENCES teams(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE channel_accounts (
  id BINARY(16) PRIMARY KEY,
  provider VARCHAR(40) NOT NULL,
  provider_user_hash BINARY(32) NOT NULL,
  encrypted_credentials VARBINARY(4096) NULL,
  status ENUM('active','revoked','pending') NOT NULL DEFAULT 'pending',
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uq_channel_provider_user (provider, provider_user_hash)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE channel_bindings (
  channel_account_id BINARY(16) NOT NULL,
  user_id BINARY(16) NOT NULL,
  bound_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  unbound_at DATETIME(3) NULL,
  PRIMARY KEY (channel_account_id, user_id),
  KEY idx_channel_binding_user (user_id, unbound_at),
  CONSTRAINT fk_channel_binding_account FOREIGN KEY (channel_account_id) REFERENCES channel_accounts(id),
  CONSTRAINT fk_channel_binding_user FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE notifications (
  id BINARY(16) PRIMARY KEY,
  team_id BINARY(16) NULL,
  recipient_user_id BINARY(16) NOT NULL,
  type VARCHAR(80) NOT NULL,
  title VARCHAR(200) NOT NULL,
  body TEXT NOT NULL,
  resource_type VARCHAR(80) NULL,
  resource_id BINARY(16) NULL,
  read_at DATETIME(3) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_notifications_recipient_date (recipient_user_id, created_at),
  KEY idx_notifications_team_date (team_id, created_at),
  CONSTRAINT fk_notification_team FOREIGN KEY (team_id) REFERENCES teams(id),
  CONSTRAINT fk_notification_recipient FOREIGN KEY (recipient_user_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE scheduled_jobs (
  id BINARY(16) PRIMARY KEY,
  team_id BINARY(16) NULL,
  owner_user_id BINARY(16) NULL,
  job_type VARCHAR(100) NOT NULL,
  payload JSON NOT NULL,
  run_at DATETIME(3) NOT NULL,
  status ENUM('scheduled','running','succeeded','failed','paused','cancelled') NOT NULL DEFAULT 'scheduled',
  dedupe_key VARCHAR(190) NULL,
  attempts INT UNSIGNED NOT NULL DEFAULT 0,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_jobs_status_runat (status, run_at),
  UNIQUE KEY uq_jobs_dedupe_key (dedupe_key),
  CONSTRAINT fk_job_team FOREIGN KEY (team_id) REFERENCES teams(id),
  CONSTRAINT fk_job_owner FOREIGN KEY (owner_user_id) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE audit_logs (
  id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  actor_user_id BINARY(16) NOT NULL,
  team_id BINARY(16) NULL,
  action VARCHAR(120) NOT NULL,
  resource_type VARCHAR(80) NOT NULL,
  resource_id BINARY(16) NULL,
  before_summary JSON NULL,
  after_summary JSON NULL,
  request_id BINARY(16) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_audit_team_date (team_id, created_at),
  KEY idx_audit_actor_date (actor_user_id, created_at),
  CONSTRAINT fk_audit_actor FOREIGN KEY (actor_user_id) REFERENCES users(id),
  CONSTRAINT fk_audit_team FOREIGN KEY (team_id) REFERENCES teams(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE outbox_events (
  id BINARY(16) PRIMARY KEY,
  team_id BINARY(16) NULL,
  event_type VARCHAR(120) NOT NULL,
  aggregate_type VARCHAR(80) NOT NULL,
  aggregate_id BINARY(16) NOT NULL,
  payload JSON NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  published_at DATETIME(3) NULL,
  attempts INT UNSIGNED NOT NULL DEFAULT 0,
  KEY idx_outbox_publish (published_at, created_at),
  KEY idx_outbox_team (team_id, created_at),
  CONSTRAINT fk_outbox_team FOREIGN KEY (team_id) REFERENCES teams(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
