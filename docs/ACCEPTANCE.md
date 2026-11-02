# TeamMind 验收记录

# 状态提示

前端已用 Ant Design Vue 4 重建，浏览器验收项需接真实后端重跑；API 验收仍可执行。功能与接口基线见 `docs/FRONTEND_FUNCTIONS_API.md`。

> 最近更新：2026-10-25。只记录实际执行过的结果；未执行的项目明确标注。

## 1. 结论

当前版本在本地 macOS 完整栈通过：

- Go 全量测试
- AI 升级验收 **24/24**
- 黄金评测 **3/3**
- 11 页浏览器冒烟 0 错误
- 微信**真实语音**入站测试成功（转写 + AI 回复 + 发送）
- 预检 0 blocking

未做：OIDC/SSO/SCIM、微信出站语音条、真实 Ubuntu 主机的完整 Web+Gateway 联调。

## 2. 平台能力

| 能力 | 实现 | 实测 |
| --- | --- | --- |
| 审批后 AI 继续执行 | `agent_followups` + worker，审批后读 checkpoint 续跑并写回对话 | 批准截止日期提案后自动回复“已延后至 2026-11-20…” |
| 关系元组权限 | `relation_tuples` + 触发器；`authorize()` 走 Check；`/authz/check`、`/authz/objects` | owner 团队 manage/project view/task contribute 全 true；跨公司 403；ListObjects 1 项目/4 任务/1 部门/1 团队 |
| 运行 trace | `/me/agent-runs/{id}/trace` 聚合 run/tool/approval/followup | 返回 run + approval span |
| 评测集 | `evals/golden_tasks.json` + `scripts/run_agent_eval.py` | 3/3：团队任务、Python 1024、联网 OpenAI |
| 检索分层 | Firecrawl → Trafilatura → 原生；`extractor` 字段 | Trafilatura 样本去导航/页脚；example.com 实网；Firecrawl mock |
| 沙箱 runtime | `sandbox_sessions` + Docker 常驻容器 + TTL | 两次 exec 读到同一 `state.txt`；跨用户 404；停止后容器消失 |
| SMTP digest | 多条到期通知合并投递 | 2 条通知只收 1 封，2 条 delivered |
| 共享总甘特 | 甘特任务读取支持 `gantt=1`，不受 `data_isolation=units` 限制 | 代码/构建通过；项目/任务管理仍隔离 |
| 权限策略 | `team_access_policies` + 设置页；AI/微信/数据隔离 | 成员在 ai=admins、wechat=admins、isolation=units 下：AI 403、微信 403、项目列表仅本部门 1 项 |
| AI 供应商团队隔离 | `ai_providers.team_id`；团队上下文优先，未配置回退全局；AI 管理页可选团队 | 公司专属 `mimo-v2.6-pro` provider 创建后，公司 Agent 返回 `mimo-v2.6-pro`；删除后恢复全局 |
| Agent 自动回复 | 消息 worker 对 `auto_reply=1` 自动回复一次 | 临时成员收到消息后 15s 内生成 `auto_reply=0` 回复；无二次循环 |
| 跨组审批 | B 组 AI 提案 → 公司管理员批准 | 临时任务进度从 0 变为 55；审批记录落库；随后清理 |
| 团队 AI 自助 | 团队管理员 `/teams/{id}/ai/providers` | 团队所有者创建/读取/删除本团队 provider；普通成员需 403（路由 `admin(role)`） |
| 隔离任务创建 | 隔离模式下 B 组在 A 组项目建任务 | `403 PROJECT_ACCESS_DENIED` |
| 后台运行配置 | 平台管理 → 系统配置（`PATCH /platform/settings`） | 保存 SMTP/Public URL 后 readiness 立即 `smtp:true`；测试邮件 200 并收到；清除后恢复 false；STT/微信未受影响 |
| 后台配置迁移 | 迁移 041 `platform_runtime_config` | 敏感字段加密存 DB，接口不返回明文；服务启动自动投影到运行配置 |
| SMTP 发信路径 | 本地 STARTTLS SMTP + `/platform/readiness/test-email` | 测试邮件返回 200，捕获 From/To/Subject/正文；生产 SMTP 凭证仍未注入 |
| 平台 readiness | `GET /api/v1/platform/readiness`（平台超管） | 当前返回 `degraded`，`missing=["smtp"]`；AI/微信/STT/Web Push/迁移均 ready |
| 团队 AI 权限 | 普通成员访问团队 AI API | `403 TEAM_ADMIN_REQUIRED` |
| 上线检查 | `scripts/production-check.sh` | 模板缺项显式列出；真实 key/SMTP/微信待注入 |
| 跨组 AI 共享甘特 | AI 工具团队级读取 + 提案写入 | B 小组 AI 读到 4 个任务并提交 55% 进度提案，正式任务保持 0，等待审批 |
| Agent 自动消息 | `agent_messages` + `send_agent_message`/`list_agent_messages` + 收件箱 | 模型实际调用 `send_agent_message`，落库并向接收方生成 `agent_message` 通知 |
| 微信单绑 | `channel_bindings.active_user_id` 唯一索引 + 绑定 API/UI 检查 | 当前 `seroncheng` 仅 1 个 active 绑定；索引存在 |
| 团队版小组 | 团队版允许 `departments` 作为小组；负责人可为团队管理员 | 创建“负责人小组”，lead 为 `team_owner`，返回 201；测试后删除 |
| 部门/小组甘特与公开链接 | Gantt `departmentId` 筛选；分享 scope `unit:<id>` | 公司默认部门开启 unit 分享，公开页返回 1 个项目/4 个任务；随后关闭 |
| 微信语音入站 | 桥接 `voice_item` + download-voice + STT | 真实微信语音转写“你现在能听到我的信息吗…”，AI 回复并 `sent` |

## 3. 微信语音实测细节

- 消息 ID：`7520130842543996296`
- 状态：`sent`
- 转写：`你现在能听到我的信息吗？能听到的话，回复一下，并且告诉我我刚刚说了什么`
- AI 回复：引用上述转写并确认听到
- STT：MiMo `mimo-v2.5-asr`，WAV→MP3，`/chat/completions` + `input_audio`
- 之前两条语音被拒是因为入站 item.type 写成 4（实际 3），已修正并重启 worker

## 4. 回归命令

```sh
cd backend && go test ./...
python3 scripts/acceptance_ai_upgrade.py
python3 scripts/run_agent_eval.py
./scripts/preflight.sh
```

浏览器冒烟：11 页，0 console，0 failed（本地测试工具，不纳入生产代码）。

## 5. 最近结果

| 检查 | 结果 |
| --- | --- |
| `go test ./...` | PASS |
| AI 验收（MiMo `mimo-v2.6-flash`） | 24/24 |
| 黄金评测 | 3/3 |
| 浏览器冒烟 | 11 页，0 console，0 failed |
| preflight | 0 blocking |
| 平台组织 | 2 个演示组织，无验收残留 |

## 6. 已知限制

- 权限：无条件关系、递归组、Expand、一致性令牌；不是完整 Zanzibar。
- AI 续跑：审批 follow-up 耐久，但不是 Temporal 级从任意工具步骤重放。
- Trace：内部 span，无外部 OTel/Langfuse exporter、token 成本。
- 检索：Firecrawl 需 key；Trafilatura 需 Python 依赖；无抓取队列/浏览器池。
- 沙箱：runtime 需 Docker；进程变量不跨 exec；不是 microVM。
- 微信：只发文本，不发原生语音条；群聊不进入 AI。
- 邮件/推送：本地 SMTP 已验证；生产需真实凭证。
- 未做 OIDC/SSO/SCIM。
- 未在真实 Ubuntu 主机跑完整 Web+Gateway+沙箱联调。

## 7. 验收数据

本地演示：

- 公司 `验收与演示团队`（`877DD3B7D9E5A8A48328FFD6E5BB9348`）
- 团队 `团队版演示`（`D4917A1C53F7D0DD316D011739D3AF6F`）
- 平台超管 `ops@teammind.local`

验收产生的临时组织会清理；不要把演示账号/密码用于生产。
