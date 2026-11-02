package live

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	agentWorkspaceFileLimit  = 12
	agentWorkspaceBytesLimit = 256 * 1024
	agentWorkspaceFileBytes  = 64 * 1024
)

type agentWorkspaceFile struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Size      int    `json:"size"`
	UpdatedAt string `json:"updatedAt"`
}

type agentWorkspaceContent struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

func agentWorkspaceKey() ([]byte, error) {
	key, err := decodeBase64Key(strings.TrimSpace(os.Getenv("AGENT_WORKSPACE_ENCRYPTION_KEY")))
	if err != nil || len(key) != 32 {
		return nil, errors.New("AGENT_WORKSPACE_ENCRYPTION_KEY must be a base64 encoded 32 byte key")
	}
	return key, nil
}

func encryptAgentWorkspaceFile(userID, fileID, name, content string) ([]byte, error) {
	key, err := agentWorkspaceKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce, err := randomBytes(gcm.NonceSize())
	if err != nil {
		return nil, err
	}
	aad := []byte(strings.ToLower(userID) + ":" + strings.ToLower(fileID) + ":" + name)
	return gcm.Seal(nonce, nonce, []byte(content), aad), nil
}

func decryptAgentWorkspaceFile(userID, fileID, name string, encrypted []byte) (string, error) {
	key, err := agentWorkspaceKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(encrypted) < gcm.NonceSize()+gcm.Overhead() {
		return "", errors.New("invalid encrypted workspace file")
	}
	plain, err := gcm.Open(nil, encrypted[:gcm.NonceSize()], encrypted[gcm.NonceSize():], []byte(strings.ToLower(userID)+":"+strings.ToLower(fileID)+":"+name))
	if err != nil || !utf8.Valid(plain) {
		return "", errors.New("cannot decrypt workspace file")
	}
	return string(plain), nil
}

func randomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	return b, err
}

func validWorkspaceName(name string) bool {
	if name == "" || len([]rune(name)) > 160 || !utf8.ValidString(name) || strings.ContainsAny(name, "\\\x00\r\n\t") {
		return false
	}
	if path.IsAbs(name) || path.Clean(name) != name || name == "." || name == ".." || strings.HasPrefix(name, "../") {
		return false
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".txt", ".csv", ".json", ".md":
	default:
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." || strings.TrimSpace(part) == "" {
			return false
		}
	}
	return true
}

func (s *Server) agentWorkspaceFilesRoute(w http.ResponseWriter, r *http.Request, u User, pathParts []string) error {
	if len(pathParts) < 4 || pathParts[0] != "me" || pathParts[1] != "agent" || pathParts[2] != "workspace" || pathParts[3] != "files" {
		return bad(http.StatusNotFound, "NOT_FOUND", "资源不存在")
	}
	if len(pathParts) == 4 {
		switch r.Method {
		case http.MethodGet:
			rows, err := s.DB.QueryContext(r.Context(), `SELECT HEX(id),name,content_size,updated_at FROM agent_workspace_files WHERE user_id=UNHEX(?) ORDER BY name,id`, u.ID)
			if err != nil {
				return err
			}
			defer rows.Close()
			files := make([]agentWorkspaceFile, 0)
			for rows.Next() {
				var file agentWorkspaceFile
				var updatedAt time.Time
				if err = rows.Scan(&file.ID, &file.Name, &file.Size, &updatedAt); err != nil {
					return err
				}
				file.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
				files = append(files, file)
			}
			if err = rows.Err(); err != nil {
				return err
			}
			_, keyErr := agentWorkspaceKey()
			write(w, http.StatusOK, map[string]any{"files": files, "configured": keyErr == nil, "maxFiles": agentWorkspaceFileLimit, "maxFileBytes": agentWorkspaceFileBytes, "maxTotalBytes": agentWorkspaceBytesLimit})
			return nil
		case http.MethodPost:
			var input agentWorkspaceContent
			if err := body(w, r, &input); err != nil {
				return err
			}
			if !validWorkspaceName(input.Name) || len(input.Content) > agentWorkspaceFileBytes || !utf8.ValidString(input.Content) {
				return bad(http.StatusBadRequest, "VALIDATION_ERROR", "文件名或内容无效；仅支持 UTF-8 文本文件，每个文件最多 64 KiB")
			}
			_, err := agentWorkspaceKey()
			if err != nil {
				return bad(http.StatusServiceUnavailable, "AGENT_WORKSPACE_NOT_CONFIGURED", "个人工作区加密密钥未配置，请联系部署管理员")
			}
			fileID := id()
			if err = s.writeAgentWorkspaceFiles(r.Context(), u.ID, []workspaceWrite{{ID: fileID, Name: input.Name, Content: input.Content}}, true); err != nil {
				return err
			}
			write(w, http.StatusCreated, map[string]any{"id": fileID, "name": input.Name, "size": len(input.Content)})
			return nil
		default:
			return bad(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "不支持此请求方式")
		}
	}
	if len(pathParts) == 5 && validID(pathParts[4]) {
		fileID := pathParts[4]
		switch r.Method {
		case http.MethodGet:
			var name string
			var encrypted []byte
			if err := s.DB.QueryRowContext(r.Context(), `SELECT name,encrypted_content FROM agent_workspace_files WHERE id=UNHEX(?) AND user_id=UNHEX(?)`, fileID, u.ID).Scan(&name, &encrypted); err == sql.ErrNoRows {
				return bad(http.StatusNotFound, "NOT_FOUND", "工作区文件不存在")
			} else if err != nil {
				return err
			}
			content, err := decryptAgentWorkspaceFile(u.ID, fileID, name, encrypted)
			if err != nil {
				return bad(http.StatusServiceUnavailable, "AGENT_WORKSPACE_CANNOT_DECRYPT", "无法解密个人工作区文件；请检查密钥配置")
			}
			write(w, http.StatusOK, agentWorkspaceContent{ID: fileID, Name: name, Content: content})
			return nil
		case http.MethodPut:
			var input agentWorkspaceContent
			if err := body(w, r, &input); err != nil {
				return err
			}
			if !validWorkspaceName(input.Name) || len(input.Content) > agentWorkspaceFileBytes || !utf8.ValidString(input.Content) {
				return bad(http.StatusBadRequest, "VALIDATION_ERROR", "文件名或内容无效；仅支持 UTF-8 文本文件，每个文件最多 64 KiB")
			}
			if err := s.updateAgentWorkspaceFile(r.Context(), u.ID, fileID, input.Name, input.Content); err != nil {
				return err
			}
			write(w, http.StatusOK, map[string]any{"id": fileID, "name": input.Name, "size": len(input.Content)})
			return nil
		case http.MethodDelete:
			tx, err := s.DB.BeginTx(r.Context(), nil)
			if err != nil {
				return err
			}
			defer tx.Rollback()
			var lockedUser string
			if err = tx.QueryRowContext(r.Context(), `SELECT HEX(id) FROM users WHERE id=UNHEX(?) FOR UPDATE`, u.ID).Scan(&lockedUser); err != nil {
				return err
			}
			var exists int
			err = tx.QueryRowContext(r.Context(), `SELECT 1 FROM agent_workspace_files WHERE id=UNHEX(?) AND user_id=UNHEX(?) FOR UPDATE`, fileID, u.ID).Scan(&exists)
			if err == sql.ErrNoRows {
				return bad(http.StatusNotFound, "NOT_FOUND", "工作区文件不存在")
			}
			if err != nil {
				return err
			}
			if _, err = tx.ExecContext(r.Context(), `DELETE FROM agent_workspace_files WHERE id=UNHEX(?) AND user_id=UNHEX(?)`, fileID, u.ID); err != nil {
				return err
			}
			if err = addAudit(tx, u.ID, "", "agent.workspace.file.delete", "agent_workspace_file", fileID); err != nil {
				return err
			}
			if err = tx.Commit(); err != nil {
				return err
			}
			write(w, http.StatusOK, map[string]bool{"deleted": true})
			return nil
		default:
			return bad(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "不支持此请求方式")
		}
	}
	return bad(http.StatusNotFound, "NOT_FOUND", "资源不存在")
}

type workspaceWrite struct {
	ID      string
	Name    string
	Content string
}

func (s *Server) loadAgentWorkspaceFiles(ctx context.Context, userID string, fileIDs []string) ([]sandboxInputFile, error) {
	if len(fileIDs) == 0 {
		return nil, nil
	}
	if len(fileIDs) > agentWorkspaceFileLimit {
		return nil, bad(http.StatusBadRequest, "VALIDATION_ERROR", "一次最多选择 12 个个人工作区文件")
	}
	if _, err := agentWorkspaceKey(); err != nil {
		return nil, bad(http.StatusServiceUnavailable, "AGENT_WORKSPACE_NOT_CONFIGURED", "个人工作区加密密钥未配置")
	}
	files := make([]sandboxInputFile, 0, len(fileIDs))
	seenID := map[string]bool{}
	seenName := map[string]bool{}
	total := 0
	for _, fileID := range fileIDs {
		if !validID(fileID) || seenID[strings.ToLower(fileID)] {
			return nil, bad(http.StatusBadRequest, "VALIDATION_ERROR", "工作区文件选择无效")
		}
		seenID[strings.ToLower(fileID)] = true
		var name string
		var encrypted []byte
		if err := s.DB.QueryRowContext(ctx, `SELECT name,encrypted_content FROM agent_workspace_files WHERE id=UNHEX(?) AND user_id=UNHEX(?)`, fileID, userID).Scan(&name, &encrypted); err == sql.ErrNoRows {
			return nil, bad(http.StatusNotFound, "AGENT_WORKSPACE_FILE_NOT_FOUND", "所选工作区文件不存在或不属于当前账号")
		} else if err != nil {
			return nil, err
		}
		if seenName[name] {
			return nil, bad(http.StatusBadRequest, "VALIDATION_ERROR", "所选文件名重复")
		}
		seenName[name] = true
		content, err := decryptAgentWorkspaceFile(userID, fileID, name, encrypted)
		if err != nil {
			return nil, bad(http.StatusServiceUnavailable, "AGENT_WORKSPACE_CANNOT_DECRYPT", "无法解密个人工作区文件；请检查密钥配置")
		}
		total += len(content)
		if total > agentWorkspaceBytesLimit {
			return nil, bad(http.StatusBadRequest, "VALIDATION_ERROR", "所选工作区文件总量超过 256 KiB")
		}
		files = append(files, sandboxInputFile{Name: name, Content: content})
	}
	return files, nil
}

func (s *Server) updateAgentWorkspaceFile(ctx context.Context, userID, fileID, name, content string) error {
	if _, err := agentWorkspaceKey(); err != nil {
		return bad(http.StatusServiceUnavailable, "AGENT_WORKSPACE_NOT_CONFIGURED", "个人工作区加密密钥未配置")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var lockedUser string
	if err = tx.QueryRowContext(ctx, `SELECT HEX(id) FROM users WHERE id=UNHEX(?) FOR UPDATE`, userID).Scan(&lockedUser); err != nil {
		return err
	}
	var oldName string
	var oldSize int
	err = tx.QueryRowContext(ctx, `SELECT name,content_size FROM agent_workspace_files WHERE id=UNHEX(?) AND user_id=UNHEX(?) FOR UPDATE`, fileID, userID).Scan(&oldName, &oldSize)
	if err == sql.ErrNoRows {
		return bad(http.StatusNotFound, "NOT_FOUND", "工作区文件不存在")
	}
	if err != nil {
		return err
	}
	var duplicate int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_workspace_files WHERE user_id=UNHEX(?) AND name=? AND id<>UNHEX(?)`, userID, name, fileID).Scan(&duplicate); err != nil {
		return err
	}
	if duplicate != 0 {
		return bad(http.StatusConflict, "AGENT_WORKSPACE_NAME_EXISTS", "工作区中已有同名文件")
	}
	var currentTotal int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(content_size),0) FROM agent_workspace_files WHERE user_id=UNHEX(?)`, userID).Scan(&currentTotal); err != nil {
		return err
	}
	if currentTotal-oldSize+len(content) > agentWorkspaceBytesLimit {
		return bad(http.StatusRequestEntityTooLarge, "AGENT_WORKSPACE_QUOTA_EXCEEDED", "个人工作区总量不能超过 256 KiB")
	}
	encrypted, err := encryptAgentWorkspaceFile(userID, fileID, name, content)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_workspace_files SET name=?,encrypted_content=?,content_size=? WHERE id=UNHEX(?) AND user_id=UNHEX(?)`, name, encrypted, len(content), fileID, userID); err != nil {
		return err
	}
	if err = addAudit(tx, userID, "", "agent.workspace.file.update", "agent_workspace_file", fileID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Server) writeAgentWorkspaceFiles(ctx context.Context, userID string, files []workspaceWrite, insertOnly bool) error {
	if len(files) == 0 {
		return nil
	}
	if len(files) > agentWorkspaceFileLimit {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", "个人工作区最多保存 12 个文件")
	}
	if _, err := agentWorkspaceKey(); err != nil {
		return bad(http.StatusServiceUnavailable, "AGENT_WORKSPACE_NOT_CONFIGURED", "个人工作区加密密钥未配置")
	}
	seen := map[string]bool{}
	requestedTotal := 0
	for _, file := range files {
		if !validWorkspaceName(file.Name) || len(file.Content) > agentWorkspaceFileBytes || !utf8.ValidString(file.Content) || seen[file.Name] {
			return bad(http.StatusBadRequest, "VALIDATION_ERROR", "工作区文件名、编码、大小或重复项无效")
		}
		seen[file.Name] = true
		requestedTotal += len(file.Content)
	}
	if requestedTotal > agentWorkspaceBytesLimit {
		return bad(http.StatusRequestEntityTooLarge, "AGENT_WORKSPACE_QUOTA_EXCEEDED", "个人工作区总量不能超过 256 KiB")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = writeAgentWorkspaceFilesTx(ctx, tx, userID, files, insertOnly); err != nil {
		return err
	}
	return tx.Commit()
}

func writeAgentWorkspaceFilesTx(ctx context.Context, tx *sql.Tx, userID string, files []workspaceWrite, insertOnly bool) error {
	if len(files) == 0 {
		return nil
	}
	if len(files) > agentWorkspaceFileLimit {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", "个人工作区最多保存 12 个文件")
	}
	if _, err := agentWorkspaceKey(); err != nil {
		return bad(http.StatusServiceUnavailable, "AGENT_WORKSPACE_NOT_CONFIGURED", "个人工作区加密密钥未配置")
	}
	seen := map[string]bool{}
	requestedTotal := 0
	for _, file := range files {
		if !validWorkspaceName(file.Name) || len(file.Content) > agentWorkspaceFileBytes || !utf8.ValidString(file.Content) || seen[file.Name] {
			return bad(http.StatusBadRequest, "VALIDATION_ERROR", "工作区文件名、编码、大小或重复项无效")
		}
		seen[file.Name] = true
		requestedTotal += len(file.Content)
	}
	if requestedTotal > agentWorkspaceBytesLimit {
		return bad(http.StatusRequestEntityTooLarge, "AGENT_WORKSPACE_QUOTA_EXCEEDED", "个人工作区总量不能超过 256 KiB")
	}
	var lockedUser string
	if err := tx.QueryRowContext(ctx, `SELECT HEX(id) FROM users WHERE id=UNHEX(?) FOR UPDATE`, userID).Scan(&lockedUser); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT name,content_size FROM agent_workspace_files WHERE user_id=UNHEX(?) FOR UPDATE`, userID)
	if err != nil {
		return err
	}
	existing := map[string]int{}
	for rows.Next() {
		var name string
		var size int
		if err = rows.Scan(&name, &size); err != nil {
			rows.Close()
			return err
		}
		existing[name] = size
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	newTotal := 0
	newCount := len(existing)
	for _, file := range files {
		if oldSize, ok := existing[file.Name]; ok {
			if insertOnly {
				return bad(http.StatusConflict, "AGENT_WORKSPACE_NAME_EXISTS", "工作区中已有同名文件")
			}
			newTotal -= oldSize
		} else {
			newCount++
		}
		newTotal += file.ContentSize()
	}
	for name, size := range existing {
		found := false
		for _, file := range files {
			if file.Name == name {
				found = true
				break
			}
		}
		if !found {
			newTotal += size
		}
	}
	if newCount > agentWorkspaceFileLimit || newTotal > agentWorkspaceBytesLimit {
		return bad(http.StatusRequestEntityTooLarge, "AGENT_WORKSPACE_QUOTA_EXCEEDED", "个人工作区最多保存 12 个文件、总量 256 KiB")
	}
	for _, file := range files {
		fileID := file.ID
		if _, ok := existing[file.Name]; ok {
			if err = tx.QueryRowContext(ctx, `SELECT HEX(id) FROM agent_workspace_files WHERE user_id=UNHEX(?) AND name=? FOR UPDATE`, userID, file.Name).Scan(&fileID); err != nil {
				return err
			}
		} else if fileID == "" {
			fileID = id()
		}
		ciphertext, encryptErr := encryptAgentWorkspaceFile(userID, fileID, file.Name, file.Content)
		if encryptErr != nil {
			return encryptErr
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO agent_workspace_files(id,user_id,name,encrypted_content,content_size) VALUES(UNHEX(?),UNHEX(?),?,?,?) ON DUPLICATE KEY UPDATE encrypted_content=VALUES(encrypted_content),content_size=VALUES(content_size),updated_at=UTC_TIMESTAMP(3)`, fileID, userID, file.Name, ciphertext, len(file.Content)); err != nil {
			return err
		}
		if err = addAudit(tx, userID, "", "agent.workspace.file.write", "agent_workspace_file", fileID); err != nil {
			return err
		}
	}
	return nil
}

func (file workspaceWrite) ContentSize() int { return len(file.Content) }

func exportAgentWorkspace(ctx context.Context, tx *sql.Tx, userID string) ([]map[string]any, error) {
	rows, err := tx.QueryContext(ctx, `SELECT HEX(id),name,encrypted_content,updated_at FROM agent_workspace_files WHERE user_id=UNHEX(?) ORDER BY name,id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	files := make([]map[string]any, 0)
	for rows.Next() {
		var fileID, name string
		var encrypted []byte
		var updatedAt time.Time
		if err = rows.Scan(&fileID, &name, &encrypted, &updatedAt); err != nil {
			return nil, err
		}
		content, decryptErr := decryptAgentWorkspaceFile(userID, fileID, name, encrypted)
		if decryptErr != nil {
			return nil, bad(http.StatusServiceUnavailable, "AGENT_WORKSPACE_CANNOT_DECRYPT", "个人工作区文件无法解密，个人数据副本未完成；请检查服务端加密密钥")
		}
		files = append(files, map[string]any{"id": fileID, "name": name, "content": content, "updatedAt": updatedAt.UTC()})
	}
	return files, rows.Err()
}
