CREATE TABLE task_collaborators (
  team_id BINARY(16) NOT NULL,
  task_id BINARY(16) NOT NULL,
  user_id BINARY(16) NOT NULL,
  added_by BINARY(16) NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (team_id, task_id, user_id),
  KEY idx_task_collaborators_user (team_id, user_id, task_id),
  CONSTRAINT fk_collaborator_task_team FOREIGN KEY (task_id, team_id) REFERENCES tasks(id, team_id),
  CONSTRAINT fk_collaborator_membership FOREIGN KEY (team_id, user_id) REFERENCES team_memberships(team_id, user_id),
  CONSTRAINT fk_collaborator_added_by FOREIGN KEY (added_by) REFERENCES users(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
