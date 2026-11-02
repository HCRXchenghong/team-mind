CREATE TABLE departments (
  id BINARY(16) NOT NULL,
  team_id BINARY(16) NOT NULL,
  name VARCHAR(120) NOT NULL,
  description VARCHAR(1000) NOT NULL DEFAULT '',
  lead_user_id BINARY(16) NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  deleted_at DATETIME(3) NULL,
  PRIMARY KEY (id),
  KEY idx_departments_team (team_id, deleted_at),
  CONSTRAINT fk_departments_team FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
  CONSTRAINT fk_departments_lead FOREIGN KEY (lead_user_id) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

ALTER TABLE team_memberships ADD COLUMN department_id BINARY(16) NULL AFTER role,
  ADD KEY idx_memberships_department (department_id),
  ADD CONSTRAINT fk_memberships_department FOREIGN KEY (department_id) REFERENCES departments(id) ON DELETE SET NULL;

ALTER TABLE projects ADD COLUMN department_id BINARY(16) NULL AFTER team_id,
  ADD KEY idx_projects_department (department_id),
  ADD CONSTRAINT fk_projects_department FOREIGN KEY (department_id) REFERENCES departments(id) ON DELETE SET NULL;

INSERT INTO departments(id, team_id, name)
SELECT UNHEX(REPLACE(UUID(),'-','')), t.id, '默认部门'
FROM teams t WHERE t.deleted_at IS NULL;

UPDATE team_memberships m
JOIN departments d ON d.team_id=m.team_id AND d.deleted_at IS NULL
SET m.department_id=d.id
WHERE m.department_id IS NULL;

UPDATE projects p
JOIN departments d ON d.team_id=p.team_id AND d.deleted_at IS NULL
SET p.department_id=d.id
WHERE p.department_id IS NULL;
