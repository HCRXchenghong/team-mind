ALTER TABLE projects DROP FOREIGN KEY fk_projects_department, DROP KEY idx_projects_department, DROP COLUMN department_id;
ALTER TABLE team_memberships DROP FOREIGN KEY fk_memberships_department, DROP KEY idx_memberships_department, DROP COLUMN department_id;
DROP TABLE IF EXISTS departments;
