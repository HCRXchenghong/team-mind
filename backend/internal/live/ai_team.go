package live

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
)

// teamAIAdmin lets a team administrator manage only providers owned by that
// team. Global providers stay under the platform console, and a team ID from
// the request body is never trusted here.
func (s *Server) teamAIAdmin(w http.ResponseWriter, r *http.Request, u User, tid, role string, p []string) error {
	if err := admin(role); err != nil {
		return err
	}
	if len(p) < 2 || p[0] != "ai" {
		return bad(http.StatusNotFound, "NOT_FOUND", "资源不存在")
	}
	if len(p) == 2 && p[1] == "models" {
		if r.Method != http.MethodPost {
			return bad(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "不支持此请求方式")
		}
		var input struct {
			BaseURL string `json:"baseUrl"`
			APIKey  string `json:"apiKey"`
			ID      string `json:"id"`
		}
		if err := body(w, r, &input); err != nil {
			return err
		}
		key := strings.TrimSpace(input.APIKey)
		if key == "" && validID(input.ID) {
			var ciphertext []byte
			if err := s.DB.QueryRowContext(r.Context(), `SELECT api_key_ciphertext FROM ai_providers WHERE id=UNHEX(?) AND team_id=UNHEX(?)`, input.ID, tid).Scan(&ciphertext); err != nil {
				return bad(http.StatusNotFound, "NOT_FOUND", "AI 供应商不存在")
			}
			decrypted, err := decryptAIKey(input.ID, ciphertext)
			if err != nil {
				return bad(http.StatusServiceUnavailable, "AI_KEY_UNAVAILABLE", "无法解密已保存的 API Key")
			}
			key = decrypted
		}
		models, err := fetchAIModels(r.Context(), input.BaseURL, key)
		if err != nil {
			return bad(http.StatusBadGateway, "AI_MODELS_FAILED", err.Error())
		}
		write(w, http.StatusOK, map[string]any{"models": models})
		return nil
	}
	if len(p) == 2 && p[1] == "providers" {
		switch r.Method {
		case http.MethodGet:
			return s.listTeamAIProviders(w, r, tid)
		case http.MethodPost:
			return s.createTeamAIProvider(w, r, u, tid)
		default:
			return bad(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "不支持此请求方式")
		}
	}
	if len(p) >= 3 && p[1] == "providers" && validID(p[2]) {
		id := p[2]
		if len(p) == 4 && p[3] == "test" && r.Method == http.MethodPost {
			var ciphertext []byte
			var baseURL string
			if err := s.DB.QueryRowContext(r.Context(), `SELECT base_url,api_key_ciphertext FROM ai_providers WHERE id=UNHEX(?) AND team_id=UNHEX(?)`, id, tid).Scan(&baseURL, &ciphertext); err != nil {
				return bad(http.StatusNotFound, "NOT_FOUND", "AI 供应商不存在")
			}
			key, err := decryptAIKey(id, ciphertext)
			if err != nil {
				return bad(http.StatusServiceUnavailable, "AI_KEY_UNAVAILABLE", "无法解密已保存的 API Key")
			}
			models, err := fetchAIModels(r.Context(), baseURL, key)
			if err != nil {
				s.markAIFailure(r.Context(), id, err)
				return bad(http.StatusBadGateway, "AI_MODELS_FAILED", err.Error())
			}
			s.markAISuccess(r.Context(), id)
			write(w, http.StatusOK, map[string]any{"models": models})
			return nil
		}
		switch r.Method {
		case http.MethodPatch:
			return s.updateTeamAIProvider(w, r, id, tid)
		case http.MethodDelete:
			res, err := s.DB.ExecContext(r.Context(), `DELETE FROM ai_providers WHERE id=UNHEX(?) AND team_id=UNHEX(?)`, id, tid)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return bad(http.StatusNotFound, "NOT_FOUND", "AI 供应商不存在")
			}
			write(w, http.StatusOK, map[string]bool{"deleted": true})
			return nil
		default:
			return bad(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "不支持此请求方式")
		}
	}
	return bad(http.StatusNotFound, "NOT_FOUND", "资源不存在")
}

func (s *Server) listTeamAIProviders(w http.ResponseWriter, r *http.Request, tid string) error {
	rows, err := s.DB.QueryContext(r.Context(), `SELECT HEX(p.id),p.name,p.base_url,p.models,p.enabled,p.priority,p.failure_count,p.cooldown_until,p.last_error,p.created_at,p.updated_at,COALESCE(HEX(p.team_id),''),COALESCE(t.name,'') FROM ai_providers p LEFT JOIN teams t ON t.id=p.team_id WHERE p.team_id=UNHEX(?) ORDER BY p.priority,p.id`, tid)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := make([]aiProviderView, 0)
	for rows.Next() {
		var item aiProviderView
		var rawModels []byte
		var cooldown sql.NullTime
		var lastError sql.NullString
		if err = rows.Scan(&item.ID, &item.Name, &item.BaseURL, &rawModels, &item.Enabled, &item.Priority, &item.FailureCount, &cooldown, &lastError, &item.CreatedAt, &item.UpdatedAt, &item.TeamID, &item.TeamName); err != nil {
			return err
		}
		_ = json.Unmarshal(rawModels, &item.Models)
		if cooldown.Valid {
			value := cooldown.Time
			item.CooldownUntil = &value
		}
		item.LastError = lastError.String
		item.HasKey = true
		out = append(out, item)
	}
	write(w, http.StatusOK, out)
	return rows.Err()
}

func (s *Server) createTeamAIProvider(w http.ResponseWriter, r *http.Request, u User, tid string) error {
	var input aiAdminInput
	if err := body(w, r, &input); err != nil {
		return err
	}
	base, err := normalizeAIBaseURL(input.BaseURL)
	if err != nil {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
	}
	if strings.TrimSpace(input.Name) == "" || len([]rune(input.Name)) > 120 || len(strings.TrimSpace(input.APIKey)) < 8 {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", "请填写供应商名称和有效 API Key")
	}
	models, err := validAIModels(input.Models)
	if err != nil {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
	}
	if _, err = aiConfigKey(); err != nil {
		return bad(http.StatusServiceUnavailable, "AI_KEY_NOT_CONFIGURED", "服务端尚未配置 AI_CONFIG_ENCRYPTION_KEY")
	}
	providerID := id()
	ciphertext, err := encryptAIKey(providerID, strings.TrimSpace(input.APIKey))
	if err != nil {
		return err
	}
	rawModels, _ := json.Marshal(models)
	enabled := true
	priority := 100
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	if input.Priority != nil {
		priority = *input.Priority
	}
	if priority < 1 || priority > 9999 {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", "优先级必须在 1 到 9999 之间")
	}
	if _, err = s.DB.ExecContext(r.Context(), `INSERT INTO ai_providers(id,team_id,name,base_url,api_key_ciphertext,models,enabled,priority,created_by) VALUES(UNHEX(?),UNHEX(?),?,?,?,?,?,?,UNHEX(?))`, providerID, tid, strings.TrimSpace(input.Name), base, ciphertext, rawModels, enabled, priority, u.ID); err != nil {
		return err
	}
	write(w, http.StatusCreated, map[string]string{"id": providerID})
	return nil
}

func (s *Server) updateTeamAIProvider(w http.ResponseWriter, r *http.Request, providerID, tid string) error {
	var input aiAdminInput
	if err := body(w, r, &input); err != nil {
		return err
	}
	if input.BaseURL != "" {
		base, err := normalizeAIBaseURL(input.BaseURL)
		if err != nil {
			return bad(http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		}
		input.BaseURL = base
	}
	if input.Name != "" && len([]rune(input.Name)) > 120 {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", "供应商名称过长")
	}
	if input.Priority != nil && (*input.Priority < 1 || *input.Priority > 9999) {
		return bad(http.StatusBadRequest, "VALIDATION_ERROR", "优先级必须在 1 到 9999 之间")
	}
	if input.Models != nil {
		models, err := validAIModels(input.Models)
		if err != nil {
			return bad(http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		}
		input.Models = models
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), `UPDATE ai_providers SET name=COALESCE(NULLIF(?,''),name),base_url=COALESCE(NULLIF(?,''),base_url),enabled=COALESCE(?,enabled),priority=COALESCE(?,priority),failure_count=0,cooldown_until=NULL,last_error=NULL WHERE id=UNHEX(?) AND team_id=UNHEX(?)`, strings.TrimSpace(input.Name), input.BaseURL, input.Enabled, input.Priority, providerID, tid)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed == 0 {
		var exists int
		if err = tx.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM ai_providers WHERE id=UNHEX(?) AND team_id=UNHEX(?)`, providerID, tid).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return bad(http.StatusNotFound, "NOT_FOUND", "AI 供应商不存在")
		}
	}
	if len(input.Models) > 0 {
		raw, _ := json.Marshal(input.Models)
		if _, err = tx.ExecContext(r.Context(), `UPDATE ai_providers SET models=? WHERE id=UNHEX(?) AND team_id=UNHEX(?)`, raw, providerID, tid); err != nil {
			return err
		}
	}
	if strings.TrimSpace(input.APIKey) != "" {
		ciphertext, err := encryptAIKey(providerID, strings.TrimSpace(input.APIKey))
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(r.Context(), `UPDATE ai_providers SET api_key_ciphertext=? WHERE id=UNHEX(?) AND team_id=UNHEX(?)`, ciphertext, providerID, tid); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	write(w, http.StatusOK, map[string]bool{"updated": true})
	return nil
}
