package live

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
)

type agentToolContext struct {
	ctx                 context.Context
	user                User
	teamID              string
	conversationID      string
	provider            string
	model               string
	allowCodeExecution  bool
	allowShellExecution bool
	persistWorkspace    bool
	uploadedFiles       []sandboxInputFile
	webEnabled          bool
	teamKnowledge       bool
	call                agentFunctionCall
}

type agentToolHandler func(*Server, agentToolContext) (map[string]any, []byte, string, error)

var agentToolHandlers = map[string]agentToolHandler{
	"list_team_members":            (*Server).agentToolListTeamMembers,
	"list_team_projects":           (*Server).agentToolListTeamProjects,
	"list_team_tasks":              (*Server).agentToolListTeamTasks,
	"create_task":                  (*Server).agentToolCreateTask,
	"propose_task_change":          (*Server).agentToolProposeTaskChange,
	"propose_project_change":       (*Server).agentToolProposeProjectChange,
	"propose_member_change":        (*Server).agentToolProposeMemberChange,
	"create_team_memory_candidate": (*Server).agentToolCreateTeamMemoryCandidate,
	"send_agent_message":           (*Server).agentToolSendAgentMessage,
	"list_agent_messages":          (*Server).agentToolListAgentMessages,
	"web_search":                   (*Server).agentToolWebSearch,
	"web_fetch":                    (*Server).agentToolWebFetch,
	"run_python":                   (*Server).agentToolRunPython,
	"run_shell":                    (*Server).agentToolRunShell,
}

func (s *Server) agentToolListTeamMembers(tc agentToolContext) (map[string]any, []byte, string, error) {
	if !validID(tc.teamID) {
		return nil, nil, "not_required", bad(http.StatusForbidden, "AGENT_TOOL_DENIED", "成员查询需要已确认的团队")
	}
	var args struct{}
	if err := strictJSON([]byte(tc.call.Function.Arguments), &args); err != nil {
		return nil, nil, "not_required", bad(http.StatusBadGateway, "AGENT_TOOL_REJECTED", "成员查询参数无效")
	}
	tx, err := s.DB.BeginTx(tc.ctx, nil)
	if err != nil {
		return nil, nil, "not_required", err
	}
	defer tx.Rollback()
	if _, err = activeTeamRoleTx(tc.ctx, tx, tc.teamID, tc.user); err != nil {
		return nil, nil, "not_required", err
	}
	rows, err := tx.QueryContext(tc.ctx, `SELECT HEX(u.id),u.display_name,m.role,m.status FROM team_memberships m JOIN users u ON u.id=m.user_id WHERE m.team_id=UNHEX(?) AND m.status IN ('active','suspended') ORDER BY u.display_name LIMIT 100`, tc.teamID)
	if err != nil {
		return nil, nil, "not_required", err
	}
	items := make([]map[string]any, 0)
	for rows.Next() {
		var memberID, name, memberRole, status string
		if err = rows.Scan(&memberID, &name, &memberRole, &status); err != nil {
			rows.Close()
			return nil, nil, "not_required", err
		}
		items = append(items, map[string]any{"id": memberID, "name": name, "role": memberRole, "status": status})
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, nil, "not_required", err
	}
	if err = rows.Close(); err != nil {
		return nil, nil, "not_required", err
	}
	if err = tx.Commit(); err != nil {
		return nil, nil, "not_required", err
	}
	return map[string]any{"count": len(items), "items": items}, []byte(`{"limit":100,"emailAddressesExposed":false}`), "not_required", nil
}

func (s *Server) agentToolListTeamProjects(tc agentToolContext) (map[string]any, []byte, string, error) {
	if !validID(tc.teamID) {
		return nil, nil, "not_required", bad(http.StatusForbidden, "AGENT_TOOL_DENIED", "项目查询需要已确认的团队")
	}
	var args struct{}
	if err := strictJSON([]byte(tc.call.Function.Arguments), &args); err != nil {
		return nil, nil, "not_required", bad(http.StatusBadGateway, "AGENT_TOOL_REJECTED", "项目查询参数无效")
	}
	tx, err := s.DB.BeginTx(tc.ctx, nil)
	if err != nil {
		return nil, nil, "not_required", err
	}
	defer tx.Rollback()
	if _, err = activeTeamRoleTx(tc.ctx, tx, tc.teamID, tc.user); err != nil {
		return nil, nil, "not_required", err
	}
	rows, err := tx.QueryContext(tc.ctx, `SELECT HEX(id),name,description,status,DATE_FORMAT(start_date,'%Y-%m-%d'),DATE_FORMAT(due_date,'%Y-%m-%d'),version FROM projects WHERE team_id=UNHEX(?) AND deleted_at IS NULL ORDER BY updated_at DESC LIMIT 10`, tc.teamID)
	if err != nil {
		return nil, nil, "not_required", err
	}
	items := make([]map[string]any, 0)
	for rows.Next() {
		var item struct {
			ID, Name, Description, Status string
			StartDate, DueDate            sql.NullString
			Version                       int
		}
		if err = rows.Scan(&item.ID, &item.Name, &item.Description, &item.Status, &item.StartDate, &item.DueDate, &item.Version); err != nil {
			rows.Close()
			return nil, nil, "not_required", err
		}
		items = append(items, map[string]any{"id": item.ID, "name": item.Name, "description": item.Description, "status": item.Status, "startDate": item.StartDate.String, "dueDate": item.DueDate.String, "version": item.Version})
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, nil, "not_required", err
	}
	if err = rows.Close(); err != nil {
		return nil, nil, "not_required", err
	}
	if err = tx.Commit(); err != nil {
		return nil, nil, "not_required", err
	}
	return map[string]any{"count": len(items), "items": items}, []byte(`{"limit":10}`), "not_required", nil
}

func (s *Server) agentToolListTeamTasks(tc agentToolContext) (map[string]any, []byte, string, error) {
	if !validID(tc.teamID) {
		return nil, nil, "not_required", bad(http.StatusForbidden, "AGENT_TOOL_DENIED", "任务查询工具需要已确认的团队")
	}
	var args struct{}
	if err := strictJSON([]byte(tc.call.Function.Arguments), &args); err != nil {
		return nil, nil, "not_required", bad(http.StatusBadGateway, "AGENT_TOOL_REJECTED", "查询工具参数无效")
	}
	tx, err := s.DB.BeginTx(tc.ctx, nil)
	if err != nil {
		return nil, nil, "not_required", err
	}
	defer tx.Rollback()
	if _, err = activeTeamRoleTx(tc.ctx, tx, tc.teamID, tc.user); err != nil {
		return nil, nil, "not_required", err
	}
	rows, err := tx.QueryContext(tc.ctx, `SELECT HEX(id),title,status,priority,progress,DATE_FORMAT(due_date,'%Y-%m-%d') FROM tasks WHERE team_id=UNHEX(?) AND deleted_at IS NULL ORDER BY due_date IS NULL,due_date,updated_at DESC LIMIT 10`, tc.teamID)
	if err != nil {
		return nil, nil, "not_required", err
	}
	items := make([]map[string]any, 0)
	for rows.Next() {
		var item struct {
			ID, Title, Status, Priority string
			Progress                    int
			DueDate                     sql.NullString
		}
		if err = rows.Scan(&item.ID, &item.Title, &item.Status, &item.Priority, &item.Progress, &item.DueDate); err != nil {
			return nil, nil, "not_required", err
		}
		items = append(items, map[string]any{"id": item.ID, "title": item.Title, "status": item.Status, "priority": item.Priority, "progress": item.Progress, "dueDate": item.DueDate.String})
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, nil, "not_required", err
	}
	if err = rows.Close(); err != nil {
		return nil, nil, "not_required", err
	}
	if err = tx.Commit(); err != nil {
		return nil, nil, "not_required", err
	}
	return map[string]any{"count": len(items), "items": items}, []byte(`{"limit":10}`), "not_required", nil
}

func (s *Server) agentToolCreateTask(tc agentToolContext) (map[string]any, []byte, string, error) {
	if !validID(tc.teamID) {
		return nil, nil, "not_required", bad(http.StatusForbidden, "AGENT_TOOL_DENIED", "创建任务需要先确认团队上下文")
	}
	var args agentCreateTaskArgs
	if err := strictJSON([]byte(tc.call.Function.Arguments), &args); err != nil {
		return nil, nil, "not_required", bad(http.StatusBadGateway, "AGENT_TOOL_REJECTED", "创建任务参数无效")
	}
	args.Title = strings.TrimSpace(args.Title)
	if args.Priority == "" {
		args.Priority = "medium"
	}
	if args.Title == "" || len([]rune(args.Title)) > 240 || len(args.Description) > 20000 || !in(args.Priority, "low", "medium", "high", "urgent") || !validDate(args.StartDate) || !validDate(args.DueDate) || (args.StartDate != "" && args.DueDate != "" && args.DueDate < args.StartDate) {
		return nil, nil, "not_required", bad(http.StatusBadRequest, "VALIDATION_ERROR", "任务标题、优先级或日期无效")
	}
	if args.AssigneeID == "" {
		args.AssigneeID = tc.user.ID
	}
	if !validID(args.AssigneeID) {
		return nil, nil, "not_required", bad(http.StatusBadRequest, "VALIDATION_ERROR", "负责人 ID 无效")
	}
	tx, err := s.DB.BeginTx(tc.ctx, nil)
	if err != nil {
		return nil, nil, "not_required", err
	}
	defer tx.Rollback()
	role, err := activeTeamRoleTx(tc.ctx, tx, tc.teamID, tc.user)
	if err != nil {
		return nil, nil, "not_required", err
	}
	if role == "member" && args.AssigneeID != tc.user.ID {
		return nil, nil, "not_required", bad(http.StatusForbidden, "ASSIGN_DENIED", "普通成员只能创建自己负责的任务")
	}
	projectID := strings.ToUpper(strings.TrimSpace(args.ProjectID))
	if projectID == "" {
		err = tx.QueryRowContext(tc.ctx, `SELECT HEX(id) FROM projects WHERE team_id=UNHEX(?) AND deleted_at IS NULL ORDER BY updated_at DESC LIMIT 1 FOR UPDATE`, tc.teamID).Scan(&projectID)
		if err == sql.ErrNoRows {
			// A task must belong to a project. Creating the default project
			// here keeps "帮我建个任务" working on a brand-new team instead
			// of failing with an internal error.
			projectID = id()
			if _, err = tx.ExecContext(tc.ctx, `INSERT INTO projects(id,team_id,name,description,status,start_date,due_date,created_by) VALUES(UNHEX(?),UNHEX(?),?,?,?,?,?,UNHEX(?))`, projectID, tc.teamID, "默认项目", "由 AI 助手创建任务时自动建立，可重命名", "active", nil, nil, tc.user.ID); err != nil {
				return nil, nil, "not_required", err
			}
			if err = addAudit(tx, tc.user.ID, tc.teamID, "project.create.agent", "project", projectID); err != nil {
				return nil, nil, "not_required", err
			}
		} else if err != nil {
			return nil, nil, "not_required", err
		}
	} else {
		if !validID(projectID) {
			return nil, nil, "not_required", bad(http.StatusBadRequest, "VALIDATION_ERROR", "项目 ID 无效")
		}
		var locked string
		if err = tx.QueryRowContext(tc.ctx, `SELECT HEX(id) FROM projects WHERE id=UNHEX(?) AND team_id=UNHEX(?) AND deleted_at IS NULL FOR UPDATE`, projectID, tc.teamID).Scan(&locked); err == sql.ErrNoRows {
			return nil, nil, "not_required", bad(http.StatusNotFound, "NOT_FOUND", "项目不存在")
		} else if err != nil {
			return nil, nil, "not_required", err
		}
	}
	var memberCount int
	if err = tx.QueryRowContext(tc.ctx, `SELECT COUNT(*) FROM team_memberships WHERE team_id=UNHEX(?) AND user_id=UNHEX(?) AND status='active'`, tc.teamID, args.AssigneeID).Scan(&memberCount); err != nil {
		return nil, nil, "not_required", err
	}
	if memberCount == 0 {
		return nil, nil, "not_required", bad(http.StatusBadRequest, "ASSIGNEE_NOT_MEMBER", "负责人不是公司成员")
	}
	taskID := id()
	if _, err = tx.ExecContext(tc.ctx, `INSERT INTO tasks(id,team_id,project_id,title,description,created_by,assignee_id,priority,start_date,due_date,is_milestone) VALUES(UNHEX(?),UNHEX(?),UNHEX(?),?,?,UNHEX(?),UNHEX(?),?,?,?,?)`, taskID, tc.teamID, projectID, args.Title, args.Description, tc.user.ID, args.AssigneeID, args.Priority, nullableDate(args.StartDate), nullableDate(args.DueDate), args.Milestone); err != nil {
		return nil, nil, "not_required", err
	}
	if _, err = tx.ExecContext(tc.ctx, `INSERT INTO task_plan_versions(team_id,task_id,version,snapshot,change_source,actor_id) VALUES(UNHEX(?),UNHEX(?),1,JSON_OBJECT('title',?,'description',?,'priority',?,'assigneeId',?,'startDate',?,'dueDate',?,'milestone',?),'manual',UNHEX(?))`, tc.teamID, taskID, args.Title, args.Description, args.Priority, args.AssigneeID, nullableDate(args.StartDate), nullableDate(args.DueDate), args.Milestone, tc.user.ID); err != nil {
		return nil, nil, "not_required", err
	}
	if err = addAudit(tx, tc.user.ID, tc.teamID, "task.create.agent", "task", taskID); err != nil {
		return nil, nil, "not_required", err
	}
	var assigneeName, projectName string
	_ = tx.QueryRowContext(tc.ctx, `SELECT display_name FROM users WHERE id=UNHEX(?)`, args.AssigneeID).Scan(&assigneeName)
	_ = tx.QueryRowContext(tc.ctx, `SELECT name FROM projects WHERE id=UNHEX(?)`, projectID).Scan(&projectName)
	if err = tx.Commit(); err != nil {
		return nil, nil, "not_required", err
	}
	safeArgs, _ := json.Marshal(map[string]any{"title": args.Title, "priority": args.Priority, "dueDate": args.DueDate, "projectId": projectID})
	return map[string]any{"taskId": taskID, "title": args.Title, "projectId": projectID, "projectName": projectName, "assigneeId": args.AssigneeID, "assigneeName": assigneeName, "dueDate": args.DueDate, "created": true}, safeArgs, "not_required", nil
}

func (s *Server) agentToolProposeTaskChange(tc agentToolContext) (map[string]any, []byte, string, error) {
	if !validID(tc.teamID) {
		return nil, nil, "pending", bad(http.StatusForbidden, "AGENT_TOOL_DENIED", "计划提案需要已确认的团队")
	}
	var args agentPlanChangeArgs
	if err := strictJSON([]byte(tc.call.Function.Arguments), &args); err != nil {
		return nil, nil, "pending", bad(http.StatusBadGateway, "AGENT_TOOL_REJECTED", "计划提案参数无效")
	}
	if !validID(args.TaskID) || len([]rune(strings.TrimSpace(args.Reason))) < 5 || len([]rune(args.Reason)) > 5000 || len([]rune(args.ImpactSummary)) > 5000 {
		return nil, nil, "pending", bad(http.StatusBadRequest, "VALIDATION_ERROR", "计划提案缺少有效任务、原因或影响说明")
	}
	result, safeArgs, err := s.createAgentPlanProposal(tc.ctx, tc.user, tc.teamID, tc.conversationID, args, tc.provider, tc.model)
	return result, safeArgs, "pending", err
}

func (s *Server) agentToolProposeProjectChange(tc agentToolContext) (map[string]any, []byte, string, error) {
	if !validID(tc.teamID) {
		return nil, nil, "pending", bad(http.StatusForbidden, "AGENT_TOOL_DENIED", "项目变更提案需要已确认的团队")
	}
	var args agentProjectChangeArgs
	if err := strictJSON([]byte(tc.call.Function.Arguments), &args); err != nil {
		return nil, nil, "pending", bad(http.StatusBadGateway, "AGENT_TOOL_REJECTED", "项目提案参数无效")
	}
	args.Reason = strings.TrimSpace(args.Reason)
	if !validID(args.ProjectID) || len([]rune(args.Reason)) < 5 || len([]rune(args.Reason)) > 5000 || len([]rune(args.ImpactSummary)) < 3 || len([]rune(args.ImpactSummary)) > 5000 {
		return nil, nil, "pending", bad(http.StatusBadRequest, "VALIDATION_ERROR", "项目提案缺少有效项目、原因或影响说明")
	}
	input := projectProposalInput{ProjectID: args.ProjectID, Name: args.Name, Description: args.Description, Status: args.Status, StartDate: args.StartDate, DueDate: args.DueDate, Reason: args.Reason, ImpactSummary: args.ImpactSummary, SourceRef: tc.conversationID, AgentProvider: tc.provider, AgentModel: tc.model}
	result, safeArgs, err := s.createAgentProjectProposal(tc.ctx, tc.user, tc.teamID, input)
	return result, safeArgs, "pending", err
}

func (s *Server) agentToolProposeMemberChange(tc agentToolContext) (map[string]any, []byte, string, error) {
	if !validID(tc.teamID) {
		return nil, nil, "pending", bad(http.StatusForbidden, "AGENT_TOOL_DENIED", "成员身份提案需要已确认的团队")
	}
	var args agentMemberChangeArgs
	if err := strictJSON([]byte(tc.call.Function.Arguments), &args); err != nil {
		return nil, nil, "pending", bad(http.StatusBadGateway, "AGENT_TOOL_REJECTED", "成员提案参数无效")
	}
	args.Reason = strings.TrimSpace(args.Reason)
	args.ImpactSummary = strings.TrimSpace(args.ImpactSummary)
	input := memberProposalInput{MemberID: args.MemberID, Role: args.Role, Status: args.Status, Reason: args.Reason, ImpactSummary: args.ImpactSummary, SourceRef: tc.conversationID, AgentProvider: tc.provider, AgentModel: tc.model}
	if err := validateMemberProposalInput(input); err != nil {
		return nil, nil, "pending", err
	}
	result, safeArgs, err := s.createAgentMemberProposal(tc.ctx, tc.user, tc.teamID, tc.conversationID, input)
	return result, safeArgs, "pending", err
}

func (s *Server) agentToolCreateTeamMemoryCandidate(tc agentToolContext) (map[string]any, []byte, string, error) {
	if !tc.teamKnowledge {
		return nil, nil, "pending", bad(http.StatusForbidden, "AGENT_TEAM_KNOWLEDGE_CONSENT_REQUIRED", "请先勾选本次团队知识候选授权")
	}
	if !validID(tc.teamID) {
		return nil, nil, "pending", bad(http.StatusForbidden, "AGENT_TOOL_DENIED", "团队知识候选需要已确认的团队")
	}
	var args agentTeamMemoryArgs
	if err := strictJSON([]byte(tc.call.Function.Arguments), &args); err != nil {
		return nil, nil, "pending", bad(http.StatusBadGateway, "AGENT_TOOL_REJECTED", "团队知识候选参数无效")
	}
	args.Category = strings.TrimSpace(args.Category)
	args.Content = strings.TrimSpace(args.Content)
	if args.Category == "" || len([]rune(args.Category)) > 80 || len([]rune(args.Content)) < 3 || len([]rune(args.Content)) > 5000 || sensitiveMemoryText(args.Category) {
		return nil, nil, "pending", bad(http.StatusBadRequest, "VALIDATION_ERROR", "团队知识类别或内容长度无效")
	}
	if sensitiveMemoryText(args.Content) {
		return nil, nil, "pending", bad(http.StatusBadRequest, "SENSITIVE_MEMORY_REJECTED", "团队知识候选包含不应保存的敏感信息")
	}
	tx, err := s.DB.BeginTx(tc.ctx, nil)
	if err != nil {
		return nil, nil, "pending", err
	}
	defer tx.Rollback()
	if err = requireTeamAdminTx(tc.ctx, tx, tc.teamID, tc.user); err != nil {
		return nil, nil, "pending", err
	}
	var lockedTeam string
	if err = tx.QueryRowContext(tc.ctx, `SELECT HEX(id) FROM teams WHERE id=UNHEX(?) AND status='active' FOR UPDATE`, tc.teamID).Scan(&lockedTeam); err != nil {
		return nil, nil, "pending", err
	}
	var duplicate string
	err = tx.QueryRowContext(tc.ctx, `SELECT HEX(id) FROM team_memories WHERE team_id=UNHEX(?) AND deleted_at IS NULL AND LOWER(TRIM(content))=LOWER(TRIM(?)) LIMIT 1`, tc.teamID, args.Content).Scan(&duplicate)
	if err == nil {
		return nil, nil, "pending", bad(http.StatusConflict, "MEMORY_DUPLICATE", "团队知识中已有相同内容，请编辑现有条目")
	}
	if err != sql.ErrNoRows {
		return nil, nil, "pending", err
	}
	if err = tx.Commit(); err != nil {
		return nil, nil, "pending", err
	}
	memoryID := id()
	safeArgs, _ := json.Marshal(map[string]any{"category": args.Category, "contentLength": len([]rune(args.Content))})
	return map[string]any{"memoryId": memoryID, "status": "pending_confirmation"}, safeArgs, "pending", nil
}

func (s *Server) agentToolWebSearch(tc agentToolContext) (map[string]any, []byte, string, error) {
	if !tc.webEnabled {
		return nil, nil, "not_required", bad(http.StatusForbidden, "AGENT_WEB_CONSENT_REQUIRED", "请勾选本次联网检索授权后再运行智能体")
	}
	var args webSearchArgs
	if err := strictJSON([]byte(tc.call.Function.Arguments), &args); err != nil {
		return nil, nil, "not_required", bad(http.StatusBadGateway, "AGENT_TOOL_REJECTED", "联网检索参数无效")
	}
	output, err := runWebSearch(tc.ctx, args.Query, args.MaxResults)
	if err != nil {
		return nil, nil, "not_required", err
	}
	safeArgs, _ := json.Marshal(map[string]any{"query": truncateRunes(args.Query, 120), "maxResults": args.MaxResults})
	return output, safeArgs, "not_required", nil
}

func (s *Server) agentToolWebFetch(tc agentToolContext) (map[string]any, []byte, string, error) {
	if !tc.webEnabled {
		return nil, nil, "not_required", bad(http.StatusForbidden, "AGENT_WEB_CONSENT_REQUIRED", "请勾选本次联网检索授权后再运行智能体")
	}
	var args webFetchArgs
	if err := strictJSON([]byte(tc.call.Function.Arguments), &args); err != nil {
		return nil, nil, "not_required", bad(http.StatusBadGateway, "AGENT_TOOL_REJECTED", "网页读取参数无效")
	}
	output, err := runWebFetch(tc.ctx, args.URL)
	if err != nil {
		return nil, nil, "not_required", err
	}
	safeArgs, _ := json.Marshal(map[string]any{"url": truncateRunes(args.URL, 300)})
	return output, safeArgs, "not_required", nil
}

func (s *Server) agentToolRunPython(tc agentToolContext) (map[string]any, []byte, string, error) {
	if !tc.allowCodeExecution {
		return nil, nil, "not_required", bad(http.StatusForbidden, "AGENT_CODE_CONSENT_REQUIRED", "请勾选本次代码沙箱授权后再运行智能体")
	}
	var args agentPythonArgs
	if err := strictJSON([]byte(tc.call.Function.Arguments), &args); err != nil {
		return nil, nil, "not_required", bad(http.StatusBadGateway, "AGENT_TOOL_REJECTED", "代码沙箱参数无效")
	}
	if !agentSandboxAvailable() {
		return nil, nil, "not_required", bad(http.StatusServiceUnavailable, "AGENT_SANDBOX_UNAVAILABLE", "此服务实例未启用受限代码沙箱")
	}
	select {
	case agentSandboxSlots <- struct{}{}:
		defer func() { <-agentSandboxSlots }()
	default:
		return nil, nil, "not_required", bad(http.StatusTooManyRequests, "AGENT_SANDBOX_BUSY", "当前沙箱任务较多，请稍后重试")
	}
	if tc.teamID != "" {
		tx, err := s.DB.BeginTx(tc.ctx, nil)
		if err != nil {
			return nil, nil, "not_required", err
		}
		if _, err = activeTeamRoleTx(tc.ctx, tx, tc.teamID, tc.user); err != nil {
			tx.Rollback()
			return nil, nil, "not_required", err
		}
		if err = tx.Commit(); err != nil {
			return nil, nil, "not_required", err
		}
	}
	output, err := runPythonSandbox(tc.ctx, args.Code, tc.uploadedFiles)
	if err != nil {
		return nil, nil, "not_required", bad(http.StatusBadRequest, "AGENT_SANDBOX_FAILED", err.Error())
	}
	persisted := tc.persistWorkspace && output.ExitCode == 0 && len(output.Files) > 0
	if persisted {
		if _, err = agentWorkspaceKey(); err != nil {
			return nil, nil, "not_required", bad(http.StatusServiceUnavailable, "AGENT_WORKSPACE_NOT_CONFIGURED", "个人工作区加密密钥未配置")
		}
	}
	fileNames := make([]string, 0, len(output.Files))
	for name := range output.Files {
		fileNames = append(fileNames, name)
	}
	encoded, _ := json.Marshal(map[string]any{"codeHash": output.CodeHash, "fileNames": fileNames, "inputFileCount": len(tc.uploadedFiles)})
	return map[string]any{"stdout": output.Stdout, "stderr": output.Stderr, "exitCode": output.ExitCode, "files": output.Files, "temporary": !persisted, "persisted": persisted}, encoded, "not_required", nil
}

func (s *Server) agentToolRunShell(tc agentToolContext) (map[string]any, []byte, string, error) {
	if !tc.allowShellExecution {
		return nil, nil, "not_required", bad(http.StatusForbidden, "AGENT_SHELL_CONSENT_REQUIRED", "请勾选本次 Shell 沙箱授权后再运行智能体")
	}
	var args agentShellArgs
	if err := strictJSON([]byte(tc.call.Function.Arguments), &args); err != nil {
		return nil, nil, "not_required", bad(http.StatusBadGateway, "AGENT_TOOL_REJECTED", "Shell 沙箱参数无效")
	}
	if !agentShellAvailable() {
		return nil, nil, "not_required", bad(http.StatusServiceUnavailable, "AGENT_SANDBOX_UNAVAILABLE", "此服务实例未启用受限 Shell 沙箱")
	}
	select {
	case agentSandboxSlots <- struct{}{}:
		defer func() { <-agentSandboxSlots }()
	default:
		return nil, nil, "not_required", bad(http.StatusTooManyRequests, "AGENT_SANDBOX_BUSY", "当前沙箱任务较多，请稍后重试")
	}
	if tc.teamID != "" {
		tx, err := s.DB.BeginTx(tc.ctx, nil)
		if err != nil {
			return nil, nil, "not_required", err
		}
		if _, err = activeTeamRoleTx(tc.ctx, tx, tc.teamID, tc.user); err != nil {
			tx.Rollback()
			return nil, nil, "not_required", err
		}
		if err = tx.Commit(); err != nil {
			return nil, nil, "not_required", err
		}
	}
	output, err := runShellSandbox(tc.ctx, args.Command, tc.uploadedFiles)
	if err != nil {
		return nil, nil, "not_required", bad(http.StatusBadRequest, "AGENT_SANDBOX_FAILED", err.Error())
	}
	persisted := tc.persistWorkspace && output.ExitCode == 0 && len(output.Files) > 0
	if persisted {
		if _, err = agentWorkspaceKey(); err != nil {
			return nil, nil, "not_required", bad(http.StatusServiceUnavailable, "AGENT_WORKSPACE_NOT_CONFIGURED", "个人工作区加密密钥未配置")
		}
	}
	fileNames := make([]string, 0, len(output.Files))
	for name := range output.Files {
		fileNames = append(fileNames, name)
	}
	encoded, _ := json.Marshal(map[string]any{"codeHash": output.CodeHash, "fileNames": fileNames, "inputFileCount": len(tc.uploadedFiles)})
	return map[string]any{"stdout": output.Stdout, "stderr": output.Stderr, "exitCode": output.ExitCode, "files": output.Files, "temporary": !persisted, "persisted": persisted}, encoded, "not_required", nil
}
