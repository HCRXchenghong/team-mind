# TeamMind 架构

# 状态提示

前端已用 Ant Design Vue 4 + Vue 3 + Vite 重建，`src/` 为新的响应式 Web 实现；功能与 API 基线见 `docs/FRONTEND_FUNCTIONS_API.md`，选型记录见 `docs/ui-preview/index.html`。

> 当前实现基线：Go 单服务 + MySQL 8 + Vue 3，迁移版本 `000041_platform_runtime_config`。

## 1. 形态与边界

- 单个 Go 二进制同时提供 API、SSE、后台 worker 与微信 worker 子命令；不引入 Redis/Kafka/独立网关。
- 一个部署只连接一个 MySQL；所有业务数据落 MySQL。
- 团队有两种形态：**公司（company）** 与 **团队（team）**；公司使用部门，团队使用小组，二者共用同一套组织单元模型（`departments`）、负责人、成员归属、项目/任务/审批/AI 模型。
- 团队/公司之间数据隔离；跨团队访问在 handler 与 SQL 两层都校验。
- 不实现 OIDC/SSO/SCIM；登录、邀请、MFA、邮箱变更、注销均为内置实现。

## 2. 代码结构

```text
backend/cmd/server             API / notification-worker / wechat-worker 入口
backend/internal/live          HTTP handler、领域逻辑、worker、迁移校验
backend/migrations             000001–000043，按序应用
src/                           Vue 3 + Ant Design Vue 前端
docs/ui-preview                前端 UI 库选型报告与真实组件预览
backend/wechat-plugin-bridge   OpenClaw 微信桥接补丁与语音下载
scripts/                       启动、迁移、预检、验收
```

后端按职责拆文件，但共享 `Server`：

- `auth*.go`：登录、MFA、限速、邮箱变更、会话
- `teams.go / members.go / departments.go`：组织与成员
- `work.go / project_proposals.go / task*`：项目、任务、协作
- `agent*.go / ai*.go / tool*.go`：AI 运行、供应商池、工具
- `authz.go / authz_tuple.go`：关系权限 Check/ListObjects
- `notification*.go / reminders.go`：通知、提醒、digest
- `wechat*.go`：Gateway 客户端、worker、语音 STT
- `sandbox_runtime.go / agent_sandbox.go`：沙箱会话与一次性执行
- `web_extract.go / agent_web.go / search_provider.go`：抓取与检索
- `observability.go`：请求日志、trace ID、指标

## 3. 身份与会话

- 邮箱+密码登录；密码 bcrypt 哈希存储；会话 token 哈希入库。
- 注册开关、仅邀请注册、邀请链接加入、邮箱验证/变更、密码重置均有独立限流。
- MFA：TOTP + 一次性恢复码；密钥 AES-GCM 加密，时间步去重。
- 平台超管与团队角色分离；平台页可管理组织、注册开关、审计与限流告警。
- 所有来源 IP 只存 HMAC 键；反向代理网段由 `TRUSTED_PROXY_CIDRS` 控制。

## 4. 权限模型

关系元组表 `relation_tuples`：

```text
(team_id, resource_type, resource_id, relation, subject_type, subject_id)
```

- 资源：`team / department / project / task`
- 关系：`owner / admin / member / lead / creator / assignee / collaborator / department / team / project / editor / approver ...`
- MySQL 触发器在成员、部门、项目、任务、协作者变化时增量维护；迁移自带存量种子。
- `authorize()` 走 `tupleCheck`：先查直接元组，再沿父边解析（project→team，task→project，department→team）。
- `GET /teams/{id}/authz/check` 单点检查；`GET /teams/{id}/authz/objects` 返回当前用户对某类对象的可见集合。
- 团队权限接口 `/teams/{id}/permissions` 也逐关系 Check。

**限制**：没有条件关系、递归组、Expand API、一致性令牌；不是完整 Zanzibar/OpenFGA。

### 4.1 可配置组织权限

`team_access_policies` 保存每个组织的：

- `ai_access`: `all | leads | admins`
- `wechat_access`: `all | leads | admins`
- `data_isolation`: `shared | units`

管理员在“设置 → 组织 → 权限与隔离”修改；`accessAllowed()` 统一判断“负责人”=管理员或组织单元负责人。`data_isolation=units` 时，普通成员的项目/任务列表、详情、更新、删除和任务创建都限定到自己的部门/小组；甘特页通过 `gantt=1` 读取全组织总甘特；AI 工具保留全组织总甘特读写，以实现跨组协作。成员目录是协作目录，不做数据行隔离。

## 5. AI 运行

- 供应商池：多个 OpenAI 兼容 provider，加密存储 key；`ai_providers.team_id` 支持团队专属 provider，团队上下文优先，未配置时回退全局；团队管理员通过 `/teams/{id}/ai/providers` 自助管理本团队 provider。
- `agent_runs` 保存状态、provider/model、checkpoint、耗时；`agent_tool_calls` 保存工具名、参数摘要、结果摘要、审批状态。
- 工具注册表 `tool_registry.go` 定义 name/description/schema；执行器按用户与团队做权限检查。
- 工具包括：团队/项目/任务读取、任务创建、三类提案（任务/项目/成员）、记忆候选、Python/Shell 沙箱、Web 搜索/抓取、Gantt 分析等。
- 审批：写工具先生成 proposal；审批通过后执行；拒绝则不落业务变更。
- 审批续跑：`agent_followups` 耐久表 + worker；审批后读取 checkpoint 上下文，生成确认回复写回原对话，失败重试 5 次。**不是** Temporal 级任意步骤重放。
- 运行恢复：`/me/agent-runs/{id}/resume` 只恢复只读步骤；写入类工具为避免重复副作用拒绝盲重放。
- 运行 trace：`/me/agent-runs/{id}/trace` 聚合 run/tool/approval/followup span。
- 评测：`evals/golden_tasks.json` + `scripts/run_agent_eval.py`（团队任务、Python、联网三条）。
- Agent 互通：`agent_messages` 表；工具 `send_agent_message`（发成员或部门/小组负责人）与 `list_agent_messages`；接收方通过现有通知（站内/微信）获知；前端通知中心有“AI 协作消息”收件箱。
- 自动回复 worker：对 `auto_reply=1` 的人工触发消息自动生成一次回复（`auto_reply=0`），避免两个 AI 无限互发；回复同样进入通知。
- AI 定时问候/日报：`scheduled_jobs.job_type='ai.proactive'`，API 进程内 worker 每 10 秒扫描；按用户时区计算下次运行，生成时读取团队快照并优先调用团队 AI，失败降级模板；结果通过 `notifyResource` 进入站内/微信/邮件/推送。
- 跨组共享总甘特：AI 工具仍按团队上下文读写项目/任务；写操作走提案/审批，不直接改正式计划。

## 6. 检索与抓取

`web_search`：

- `SEARCH_PROVIDER=searxng` 时请求 SearXNG JSON；
- 结果做 URL 去重、私网过滤、摘要截断。

`web_fetch`：

1. 配置 `FIRECRAWL_API_KEY` 时先走 Firecrawl `/v1/scrape`（markdown）；
2. 否则原生抓取，限制 480 KiB / 20s；
3. HTML 优先 Trafilatura（Python helper，`scripts/web_extract.py`）；
4. Trafilatura 不可用则内部 `htmlToText`；
5. 返回 `extractor` 字段（firecrawl/trafilatura/native）。

SSRF 防护：DNS 解析后拒绝回环、私网、链路本地、组播等地址；每个重定向重新校验。

## 7. 沙箱

两种模式：

- **一次性**：Linux bwrap（断网、只读、`prlimit`）或 Docker 一次性容器；
- **runtime 会话**：`sandbox_sessions` 表 + Docker 常驻容器（`sleep infinity`），网络 none、read-only、cap-drop ALL、非 root、256 MiB/1 CPU/64 PID，workspace 持久挂载；`exec` 走 `docker exec`，TTL 30 分钟，每用户最多 2 个会话，后台 worker 回收。

限制：Docker 必需（runtime 模式）；进程变量不跨 exec；不是 Firecracker microVM。

## 8. 微信通道

```text
微信用户
  → 腾讯 iLink / OpenClaw Gateway (127.0.0.1:18793)
  → @tencent-weixin/openclaw-weixin 插件
  → TeamMind 桥接 (channel/updates, channel/send-text, channel/download-voice)
  → Go wechat-worker
  → TeamMind 对话 / AI
```

- 绑定：扫码登录 → 微信账号绑定到 TeamMind 用户与当前团队；解绑/换团队后 worker 停止轮询。
- 入站文本：支持；群聊不进入 AI（安全回退）。
- 入站语音：
  1. 有 `voice_item.text` 直接用微信转写；
  2. 否则桥接下载 CDN 密文、AES 解密、`silk-wasm` 转 WAV；
  3. Go 调 STT：MiMo `mimo-v2.5-asr`（`/chat/completions` + `input_audio`，WAV→MP3）或 OpenAI 兼容 `/audio/transcriptions`；
  4. 失败记 `voice_transcription_unavailable`，不伪造回复。
- 出站：只发文本；不做微信原生语音条（按需求）。
- 安全：桥接只监听回环、Bearer token 常量时间比较、baseUrl 白名单、消息长度/媒体大小限制、凭据 AES-GCM 加密入库。

## 9. 通知与提醒

- Outbox 模式：业务事务内写 `notification_deliveries`，后台 worker 投递；邮件/浏览器/原生推送分渠道幂等。
- 微信提醒：定时任务触发后会排队 `wechat` 投递，用 `channel_bindings` 保存的最新 sender/context 通过 OpenClaw `send-text` 主动发送。
- SMTP digest：同一收件人的多条到期提醒可合并为一封邮件；本地 STARTTLS SMTP 已验证“两条通知合并一封”。
- 提醒：个人/团队提醒，`FOR UPDATE SKIP LOCKED` 抢占，有限重试与退避。
- AI 主动消息：`ai.proactive` 计划支持每天/工作日/每周/一次；生成结果复用同一通知事务，继承静音、免打扰与渠道偏好。
- Web Push：VAPID；原生推送：FCM（可选）。
- 所有投递记录可审计，失败保留错误并重试。

## 10. 前端

- 技术栈：Vue 3 + Vite 7 + TypeScript + Ant Design Vue 4；组件通过 `unplugin-vue-components` 按需引入。
- 页面：登录/注册/MFA、总览、任务、项目、甘特、审批、成员、部门、通知、设置、AI 助手、记忆、微信、AI 管理、平台控制台、公开甘特与 404，逐页功能见 `docs/FRONTEND_FUNCTIONS_API.md`。
- 信息架构：固定导航 + 侧栏 + 页面标签 + 面包屑 + 内容区；≤900px 侧栏抽屉化。
- 主题：`src/theme.ts` 提供 Ant Design token（亮/暗算法），`src/styles/tokens.css` 提供页面级 CSS 变量，二者由 `data-theme` 同步。
- 请求层：`src/api/client.ts` 统一 Bearer、响应信封、401 清理与 SSE `streamFetch`；组织切换用 `teamId` 校验防止串数据。
- 公开链接：`/share/gantt/:token`、`/join/:token`、邮箱确认、重置密码均不依赖登录态。

## 11. 数据与迁移

- 41 个迁移全部在 `backend/migrations`，服务启动强制要求 `000041_platform_runtime_config`。
- 迁移脚本记录 `schema_migrations`；已有表但无迁移历史会拒绝重放。
- MySQL DDL 不支持事务，迁移前必须备份；回滚只回退最近一个版本，失败从备份恢复。
- 时间统一 UTC 存储，前端按用户时区显示（默认 Asia/Shanghai）。

## 12. 关键限制

- 单 MySQL、单 API 进程模型；水平扩展需要处理 SSE/outbox/worker 抢占与迁移锁。
- 无 OIDC/SSO/SCIM。
- 权限无完整 Zanzibar 条件/Expand。
- 审批续跑非 Temporal 级。
- 检索层依赖外部 SearXNG/Firecrawl/Trafilatura 可选组件。
- 沙箱 runtime 依赖 Docker；不做 microVM 级隔离。
