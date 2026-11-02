CREATE TABLE relation_tuples (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  team_id BINARY(16) NOT NULL,
  resource_type VARCHAR(32) NOT NULL,
  resource_id BINARY(16) NOT NULL,
  relation VARCHAR(40) NOT NULL,
  subject_type VARCHAR(32) NOT NULL,
  subject_id BINARY(16) NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uniq_relation_tuple (resource_type, resource_id, relation, subject_type, subject_id),
  KEY idx_relation_subject (subject_type, subject_id, relation, resource_type),
  KEY idx_relation_resource (team_id, resource_type, resource_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT IGNORE INTO relation_tuples(team_id,resource_type,resource_id,relation,subject_type,subject_id)
SELECT team_id,'team',team_id,
  CASE role WHEN 'team_owner' THEN 'owner' WHEN 'team_admin' THEN 'admin' ELSE 'member' END,
  'user',user_id
FROM team_memberships WHERE status='active';

INSERT IGNORE INTO relation_tuples(team_id,resource_type,resource_id,relation,subject_type,subject_id)
SELECT team_id,'department',id,'team','team',team_id FROM departments WHERE deleted_at IS NULL;
INSERT IGNORE INTO relation_tuples(team_id,resource_type,resource_id,relation,subject_type,subject_id)
SELECT team_id,'department',id,'lead','user',lead_user_id FROM departments WHERE deleted_at IS NULL AND lead_user_id IS NOT NULL;
INSERT IGNORE INTO relation_tuples(team_id,resource_type,resource_id,relation,subject_type,subject_id)
SELECT team_id,'department',department_id,'member','user',user_id FROM team_memberships WHERE status='active' AND department_id IS NOT NULL;

INSERT IGNORE INTO relation_tuples(team_id,resource_type,resource_id,relation,subject_type,subject_id)
SELECT team_id,'project',id,'team','team',team_id FROM projects WHERE deleted_at IS NULL;
INSERT IGNORE INTO relation_tuples(team_id,resource_type,resource_id,relation,subject_type,subject_id)
SELECT team_id,'project',id,'department','department',department_id FROM projects WHERE deleted_at IS NULL AND department_id IS NOT NULL;

INSERT IGNORE INTO relation_tuples(team_id,resource_type,resource_id,relation,subject_type,subject_id)
SELECT team_id,'task',id,'project','project',project_id FROM tasks WHERE deleted_at IS NULL;
INSERT IGNORE INTO relation_tuples(team_id,resource_type,resource_id,relation,subject_type,subject_id)
SELECT team_id,'task',id,'creator','user',created_by FROM tasks WHERE deleted_at IS NULL;
INSERT IGNORE INTO relation_tuples(team_id,resource_type,resource_id,relation,subject_type,subject_id)
SELECT team_id,'task',id,'assignee','user',assignee_id FROM tasks WHERE deleted_at IS NULL AND assignee_id IS NOT NULL;
INSERT IGNORE INTO relation_tuples(team_id,resource_type,resource_id,relation,subject_type,subject_id)
SELECT team_id,'task',task_id,'collaborator','user',user_id FROM task_collaborators;

DELIMITER //
CREATE TRIGGER trg_relation_membership_ai AFTER INSERT ON team_memberships FOR EACH ROW
BEGIN
  DELETE FROM relation_tuples WHERE team_id=NEW.team_id AND subject_type='user' AND subject_id=NEW.user_id AND resource_type IN ('team','department');
  IF NEW.status='active' THEN
    INSERT IGNORE INTO relation_tuples(team_id,resource_type,resource_id,relation,subject_type,subject_id)
    VALUES(NEW.team_id,'team',NEW.team_id,CASE NEW.role WHEN 'team_owner' THEN 'owner' WHEN 'team_admin' THEN 'admin' ELSE 'member' END,'user',NEW.user_id);
    IF NEW.department_id IS NOT NULL THEN
      INSERT IGNORE INTO relation_tuples(team_id,resource_type,resource_id,relation,subject_type,subject_id)
      VALUES(NEW.team_id,'department',NEW.department_id,'member','user',NEW.user_id);
    END IF;
  END IF;
END//

CREATE TRIGGER trg_relation_membership_au AFTER UPDATE ON team_memberships FOR EACH ROW
BEGIN
  DELETE FROM relation_tuples WHERE team_id=NEW.team_id AND subject_type='user' AND subject_id=NEW.user_id AND resource_type IN ('team','department');
  IF NEW.status='active' THEN
    INSERT IGNORE INTO relation_tuples(team_id,resource_type,resource_id,relation,subject_type,subject_id)
    VALUES(NEW.team_id,'team',NEW.team_id,CASE NEW.role WHEN 'team_owner' THEN 'owner' WHEN 'team_admin' THEN 'admin' ELSE 'member' END,'user',NEW.user_id);
    IF NEW.department_id IS NOT NULL THEN
      INSERT IGNORE INTO relation_tuples(team_id,resource_type,resource_id,relation,subject_type,subject_id)
      VALUES(NEW.team_id,'department',NEW.department_id,'member','user',NEW.user_id);
    END IF;
  END IF;
END//

CREATE TRIGGER trg_relation_membership_ad AFTER DELETE ON team_memberships FOR EACH ROW
BEGIN
  DELETE FROM relation_tuples WHERE team_id=OLD.team_id AND subject_type='user' AND subject_id=OLD.user_id AND resource_type IN ('team','department');
END//

CREATE TRIGGER trg_relation_department_ai AFTER INSERT ON departments FOR EACH ROW
BEGIN
  INSERT IGNORE INTO relation_tuples(team_id,resource_type,resource_id,relation,subject_type,subject_id)
  VALUES(NEW.team_id,'department',NEW.id,'team','team',NEW.team_id);
  IF NEW.lead_user_id IS NOT NULL THEN
    INSERT IGNORE INTO relation_tuples(team_id,resource_type,resource_id,relation,subject_type,subject_id)
    VALUES(NEW.team_id,'department',NEW.id,'lead','user',NEW.lead_user_id);
  END IF;
END//

CREATE TRIGGER trg_relation_department_au AFTER UPDATE ON departments FOR EACH ROW
BEGIN
  DELETE FROM relation_tuples WHERE team_id=NEW.team_id AND resource_type='department' AND resource_id=NEW.id AND relation IN ('team','lead');
  IF NEW.deleted_at IS NULL THEN
    INSERT IGNORE INTO relation_tuples(team_id,resource_type,resource_id,relation,subject_type,subject_id)
    VALUES(NEW.team_id,'department',NEW.id,'team','team',NEW.team_id);
    IF NEW.lead_user_id IS NOT NULL THEN
      INSERT IGNORE INTO relation_tuples(team_id,resource_type,resource_id,relation,subject_type,subject_id)
      VALUES(NEW.team_id,'department',NEW.id,'lead','user',NEW.lead_user_id);
    END IF;
  END IF;
END//

CREATE TRIGGER trg_relation_department_ad AFTER DELETE ON departments FOR EACH ROW
BEGIN
  DELETE FROM relation_tuples WHERE team_id=OLD.team_id AND resource_type='department' AND resource_id=OLD.id;
END//

CREATE TRIGGER trg_relation_project_ai AFTER INSERT ON projects FOR EACH ROW
BEGIN
  INSERT IGNORE INTO relation_tuples(team_id,resource_type,resource_id,relation,subject_type,subject_id)
  VALUES(NEW.team_id,'project',NEW.id,'team','team',NEW.team_id);
  IF NEW.department_id IS NOT NULL THEN
    INSERT IGNORE INTO relation_tuples(team_id,resource_type,resource_id,relation,subject_type,subject_id)
    VALUES(NEW.team_id,'project',NEW.id,'department','department',NEW.department_id);
  END IF;
END//

CREATE TRIGGER trg_relation_project_au AFTER UPDATE ON projects FOR EACH ROW
BEGIN
  DELETE FROM relation_tuples WHERE team_id=NEW.team_id AND resource_type='project' AND resource_id=NEW.id AND relation IN ('team','department');
  IF NEW.deleted_at IS NULL THEN
    INSERT IGNORE INTO relation_tuples(team_id,resource_type,resource_id,relation,subject_type,subject_id)
    VALUES(NEW.team_id,'project',NEW.id,'team','team',NEW.team_id);
    IF NEW.department_id IS NOT NULL THEN
      INSERT IGNORE INTO relation_tuples(team_id,resource_type,resource_id,relation,subject_type,subject_id)
      VALUES(NEW.team_id,'project',NEW.id,'department','department',NEW.department_id);
    END IF;
  END IF;
END//

CREATE TRIGGER trg_relation_project_ad AFTER DELETE ON projects FOR EACH ROW
BEGIN
  DELETE FROM relation_tuples WHERE team_id=OLD.team_id AND resource_type='project' AND resource_id=OLD.id;
END//

CREATE TRIGGER trg_relation_task_ai AFTER INSERT ON tasks FOR EACH ROW
BEGIN
  INSERT IGNORE INTO relation_tuples(team_id,resource_type,resource_id,relation,subject_type,subject_id)
  VALUES(NEW.team_id,'task',NEW.id,'project','project',NEW.project_id);
  INSERT IGNORE INTO relation_tuples(team_id,resource_type,resource_id,relation,subject_type,subject_id)
  VALUES(NEW.team_id,'task',NEW.id,'creator','user',NEW.created_by);
  IF NEW.assignee_id IS NOT NULL THEN
    INSERT IGNORE INTO relation_tuples(team_id,resource_type,resource_id,relation,subject_type,subject_id)
    VALUES(NEW.team_id,'task',NEW.id,'assignee','user',NEW.assignee_id);
  END IF;
END//

CREATE TRIGGER trg_relation_task_au AFTER UPDATE ON tasks FOR EACH ROW
BEGIN
  DELETE FROM relation_tuples WHERE team_id=NEW.team_id AND resource_type='task' AND resource_id=NEW.id AND relation IN ('project','creator','assignee');
  IF NEW.deleted_at IS NULL THEN
    INSERT IGNORE INTO relation_tuples(team_id,resource_type,resource_id,relation,subject_type,subject_id)
    VALUES(NEW.team_id,'task',NEW.id,'project','project',NEW.project_id);
    INSERT IGNORE INTO relation_tuples(team_id,resource_type,resource_id,relation,subject_type,subject_id)
    VALUES(NEW.team_id,'task',NEW.id,'creator','user',NEW.created_by);
    IF NEW.assignee_id IS NOT NULL THEN
      INSERT IGNORE INTO relation_tuples(team_id,resource_type,resource_id,relation,subject_type,subject_id)
      VALUES(NEW.team_id,'task',NEW.id,'assignee','user',NEW.assignee_id);
    END IF;
  END IF;
END//

CREATE TRIGGER trg_relation_task_ad AFTER DELETE ON tasks FOR EACH ROW
BEGIN
  DELETE FROM relation_tuples WHERE team_id=OLD.team_id AND resource_type='task' AND resource_id=OLD.id;
END//

CREATE TRIGGER trg_relation_collab_ai AFTER INSERT ON task_collaborators FOR EACH ROW
BEGIN
  INSERT IGNORE INTO relation_tuples(team_id,resource_type,resource_id,relation,subject_type,subject_id)
  VALUES(NEW.team_id,'task',NEW.task_id,'collaborator','user',NEW.user_id);
END//

CREATE TRIGGER trg_relation_collab_ad AFTER DELETE ON task_collaborators FOR EACH ROW
BEGIN
  DELETE FROM relation_tuples WHERE team_id=OLD.team_id AND resource_type='task' AND resource_id=OLD.task_id AND relation='collaborator' AND subject_id=OLD.user_id;
END//
DELIMITER ;
