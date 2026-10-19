# TeamMind 交付报告（2026-10-19，Asia/Shanghai）

本报告记录当前工作区内的实现和测试情况。`通过` 仅表示所列测试已通过；`部分通过` 表示仍有工作缺失或尚未验证。接口路径均相对于 `/api/v1`。逐项功能审计见 [FUNCTION_AUDIT.md](FUNCTION_AUDIT.md)，安全证据和详细缺口见 [ACCEPTANCE.md](ACCEPTANCE.md)。

| 功能/页面 | 接口 | 数据库 | 可用角色 | 测试结果 | 外部依赖 |
| --- | --- | --- | --- | --- | --- |
| 登录、注册、退出、找回密码 / 登录与验证页 | `/auth/register`、`/auth/login`、`/auth/logout`、`/auth/forgot`、`/auth/reset`、`/me` | `users`、`auth_sessions`、`password_resets`、`login_failures`、`auth_ip_rate_limits` | 访客登录；本人退出 | **本地通过**：Chromium 验证登录六种状态、对比度、键盘、触摸、失败、超时和会话撤销；隔离 MySQL 验证同来源第 31 次登录返回 429、可信代理解析和超管脱敏告警。生产限流 HMAC 密钥/代理配置及 SMTP 仍待部署验证 | 生产需 `LOGIN_RATE_LIMIT_HMAC_KEY`、按实际情况配置可信代理、密码重置邮件需 SMTP |
| 团队导航、搜索、通知、主题 / 顶栏与手机抽屉 | `/teams/{id}/tasks`、`/me/notifications/unread-count`、`/me/notifications`、`/auth/logout` | `tasks`、`notifications`、`auth_sessions`；设备主题偏好存储在本地 | 有效成员；本人 | **通过**：Chromium 验证账号菜单进入设置、退出后撤销令牌、顶栏真实任务搜索、按接口未读数打开收件箱、390px 触摸打开导航抽屉并进入任务页；主题刷新后保留  无 | 无 |
| 资料、时区、密码、会话 / 设置 | `/me`、`/auth/password`、`/auth/refresh`、`/auth/sessions` | `users`、`auth_sessions` | 本人 | **通过**：表单可用键盘提交；无效 IANA 时区在请求前被拒；MySQL 中的资料和团队时区刷新后仍在；当前设备识别、续期及撤销当前会话后返回登录；跨浏览器撤销和修改密码流程通过  无 | 无 |
| 邮箱变更 / 设置与公开验证页 | `/me/email-change`、`/me/email-change/confirm` | `users`、`email_change_requests`、`auth_sessions`、`password_resets`、`audit_logs` | 本人发起；持有发送到新邮箱的单次链接者确认 | **通过（隔离 MySQL + Chromium）**：错误当前密码、邮箱冲突、过期/重放/并发令牌、无会话跨浏览器确认、旧会话撤销、新邮箱登录和脱敏审计均通过；本地脚本已加入设置回归。生产 SMTP 发送需凭据和部署验证 | 生产 SMTP 与 `PUBLIC_URL` |
| 多因素认证 / 登录与设置 | `/me/mfa`、`/me/mfa/setup`、`/me/mfa/enable`、`/me/mfa/disable`、`/auth/mfa/complete` | `user_mfa`、`mfa_recovery_codes`、`auth_mfa_challenges`、`auth_sessions`、`audit_logs` | 本人设置；登录挑战持有人完成第二因素 | **通过（迁移 15 隔离 MySQL + Chromium）**：RFC 6238、TOTP 密钥加密、恢复码哈希和单次使用、挑战限时单次、同一验证码并发只允许一个挑战成功、重放拒绝、登录和关闭后会话撤销；设置全视口与真实交互回归通过 | 生产需安全托管 `MFA_ENCRYPTION_KEY` |
| 团队创建、切换、审核 / 设置与超管 | `/teams`、`/me/teams`、`/platform/teams`、`/platform/audit-log` | `teams`、`team_memberships`、`platform_roles`、`audit_logs`、`notifications` | 登录用户创建；成员切换；超管审核/查阅审核记录 | **通过**：Chromium 创建、编辑、双团队切换和任务隔离；旧团队延迟概览响应竞态回归通过；390px 流程覆盖超管批准/拒绝、审核记录、拒绝原因持久化和申请人通知  无 | 无 |
| 成员与所有权 / 成员页、审批页 | `/teams/{id}/members`、`/teams/{id}/member-proposals`、`/decision`、`/resubmit` | `team_memberships`（迁移 21 成员版本）、`member_change_proposals`、`member_change_approvals`、`audit_logs`、`notifications`、`outbox_events` | 管理员/所有者管理成员；成员可提交身份提案；管理员审批普通成员变更，任命管理员须所有者审批 | **通过（隔离 MySQL + Chromium/WebKit 移动浏览器）**：Chromium 与 WebKit 真 API 流程均验证审批前身份不变、审批后才更新，320–1280px 页面无横向溢出；并发/重复审批、提案人自批拒绝、管理员无权任命管理员、成员版本变化使旧提案过期均有 MySQL 集成覆盖 | 否 |
| 邀请与接受 / 成员、邀请页 | `/teams/{id}/invitations`、`/invitations/accept` | `team_invitations`、`team_memberships`、`audit_logs` | 管理员/所有者签发；已登录用户接受 | **通过**：并发单次使用、双浏览器、登录后返回邀请、390px 触摸流程通过；已撤销邀请无法接受  无 | 无 |
| 概览统计 / 概览页 | `/teams/{id}/summary`、`/tasks` | `projects`、`tasks`、`team_memberships`、`plan_change_proposals` | 有效成员 | **通过**：Chromium 五项统计与当前团队概览统计接口逐项相等；近期任务可打开对应任务详情  无 | 无 |
| 项目 / 项目页 | `/teams/{id}/projects` | `projects`、`audit_logs` | 成员可读；管理员可写 | **通过**：接口搜索/状态筛选、日期编辑和校验、角色拒绝及删除约束；Chromium 创建、编辑排期、确认删除、13 项数据分页、状态/名称筛选及项目到任务/甘特图的正确跳转  无 | 无 |
| 任务、依赖、评论 / 任务与详情页 | `/teams/{id}/tasks` 及 `comments`、`dependencies`、`collaborators` 子路径 | `tasks`、`task_dependencies`、`task_comments`、`task_collaborators`、`task_plan_versions`、`audit_logs` | 成员可读；创建者/负责人/协作者按权限更新；评论作者管理本人评论；管理员可管理 | **通过**：接口与 Chromium 验证搜索、状态筛选、白名单排序、分页、并发版本冲突、评论编辑/删除、依赖增删与环检测、协作者增删和协作者进度更新；双浏览器主要流程通过  无 | 无 |
| 任务计划变更 / 任务与审批页 | `POST /teams/{id}/plan-proposals`、`/resubmit`、`/decision` | `plan_change_proposals`、`plan_change_approvals`、`tasks`、`task_plan_versions`、`notifications`、`outbox_events`、`audit_logs` | 创建者/负责人提交；团队管理员/所有者审批 | **通过**：标题、描述、优先级、负责人、日期、里程碑使用版本校验和事务；审批前正式计划不变；修改后批准、补充后重提时，只有最终批准才更新正式字段；负责人必须是有效团队成员  无 | 无 |
| 项目变更提案 / 审批页 | `GET/POST /teams/{id}/project-proposals`、`/resubmit`、`/decision` | `project_change_proposals`、`projects`、`audit_logs`、`notifications`、`outbox_events` | 团队成员提交；管理员/所有者要求补充、拒绝或批准 | **通过（迁移 21 隔离 MySQL + WebKit 双浏览器）**：智能体和成员均只能创建待审批提案；补充后重提、修改后批准后才更新项目；版本冲突、重复审批、通知/审计/outbox 同事务验证通过 | 否 |
| 甘特图 / 甘特页 | `GET /teams/{id}/tasks`、`/dependencies` | `tasks`、`task_dependencies` | 有效团队成员 | **通过**：Chromium 桌面和 390px 触屏验证项目/状态/负责人/日期筛选、依赖与阻塞展示、分页、日期定位和横向滚动；正式计划在审批完成前只读  无 | 无 |
| 通知、邮件、浏览器/原生推送与提醒 / 通知页 | `/me/notifications`、`/me/notification-channels`、`/me/notification-channels/native-devices`、`/me/notification-preferences`、`/me/notification-deliveries`、`/me/reminders` | `notifications`、`notification_preferences`、`notification_push_subscriptions`、`notification_native_devices`、`notification_deliveries`、`scheduled_jobs` | 收件人管理自己的偏好/设备；团队提醒须为有效成员 | **代码/隔离 MySQL 通过**：迁移 18；原有 SMTP/Web Push 测试继续有效；新增 FCM 服务账号 JWT、OAuth 缓存、HTTP v1 投递、令牌加密、跨用户撤销拒绝、失效令牌撤销、重试和数据库持久化测试。真实 Firebase/APNs 和实体设备送达未验证 | SMTP；浏览器需 VAPID/HTTPS；原生需 Firebase 客户端配置、服务账号和 iOS APNs |
| AI 对话 / 助手页 | `/me/conversations`、`/stream`、`/extract-memory-candidates`、`/me/conversations/{id}/agent` | `conversations`、`conversation_messages`、`agent_runs`、`agent_tool_calls`、`audit_logs` | 对话本人；团队上下文须明确确认且为有效成员 | **部分通过**：Chromium 模拟 SSE、上下文确认和记忆来源校验通过；真实模型服务未配置，不能验收真实回复 | 真实模型服务凭据 |
| 智能体自主工具、审批提案、团队知识候选、沙箱与私人工作区 / 助手页 | `/me/conversations/{id}/agent`、`/me/agent/tools/execute`、`/me/agent/workspace/files`、`GET /teams/{id}/memories`、任务/项目/成员提案接口 | `agent_runs`、`agent_tool_calls`、`agent_workspace_files`、`plan_change_proposals`、`project_change_proposals`、`member_change_proposals`、`team_memories`、`conversation_messages`、`task_plan_versions`、`notifications`、`audit_logs`、`tasks`、`projects`、`team_memberships` | 当前团队有效成员；团队知识候选仅管理员/所有者且逐次授权；私人工作区仅本人；成员身份写入须有权管理员审批 | **隔离 MySQL 与 Chromium/WebKit 通过**：模型可有界串行读取任务、项目和成员基本身份，并提交待审批任务/项目/成员变更；正式成员身份只在合格审批事务内改变。管理员逐次授权后可写入未确认团队知识候选。未确认知识不进入模型上下文；来源对话不向其他成员泄露。私人文件 AES-GCM 加密写入 MySQL，沙箱输出须逐次授权才持久保存。真实模型服务和目标主机逃逸评估未验证 | 真实模型服务凭据；服务端工作区密钥；生产沙箱主机评估 |
| 个人记忆、团队知识 / 记忆页 | `/me/memories`、`/teams/{id}/memories`、`/messages/{messageId}/memory-candidate` | `user_memories`、`team_memories`、`conversation_messages`、`audit_logs` | 私人记忆仅本人；团队成员可读、管理员可写 | **通过**：API 验证来源、重复冲突、人工确认/撤回、复核日期、过期日期和团队重复冲突；Chromium 验证候选编辑确认、来源摘录和跳回原对话 | 自动提取需要配置模型服务 |
| 微信扫码绑定 / 渠道页 | `/me/channels/wechat` 及会话接口；本机 OpenClaw Gateway 调用固定腾讯插件 2.4.10 | `channel_accounts`、`channel_bindings`、`audit_logs`、`provider_base_url`；绑定凭据 AES-GCM 加密存入 MySQL | 本人且属于有效团队 | **真人扫码和绑定通过，重启读回通过**：修复测试清理顺序并删除 7 个测试夹具后，当前隔离库只读聚合为 1 个有效绑定、1 条入站记录和 1 个会话映射；worker 同步游标持续更新。此前 API/Gateway/worker 重启读回已验；身份争抢、并发绑定、解绑审计和重绑测试通过（含 race detector）。实体微信内置浏览器页面仍未回归 | 腾讯网络；真实模型服务用于 AI 回复；实体微信内置浏览器回归 |
| 微信收发 / 独立后台进程 | Go worker 经本机 Gateway 调用腾讯插件 `getUpdates` / `sendMessage` 适配器 | `channel_sync_states`、`channel_conversations`、`channel_message_deliveries`、`conversation_messages` | 有效绑定的发送者 | **真实入站已验证；生产 AI 闭环未通过**：当前净化后记录为 1 条入站和 1 条失败投递，无 `sent` 记录。运行服务 `AI_PROVIDER=mock` 且没有模型 URL、模型名、密钥；模拟回复被拒绝。此前计入的 7 个 `Worker Test` 帐号/消息属于测试夹具，已清理。 | 真实模型地址、模型名和服务端凭据；腾讯网络及真人收件确认 |
| 团队/平台审计 / 设置与平台页 | `/teams/{id}/audit-log`、`/platform/audit-log` | `audit_logs`、`teams`、`platform_roles` | 团队管理员/所有者；仅超管可看平台审核记录 | **通过**：API 允许团队所有者读取、拒绝普通成员/非成员；Chromium 设置页验证团队创建/修改记录、320px 无溢出，普通成员不显示团队审计  无 | 无 |
| 平台超管 / 平台页 | `/platform/teams`、`/platform/audit-log`、`/platform/teams/{id}/decision` | `platform_roles`、`teams`、`audit_logs`、`notifications` | 通过 `bootstrap-admin` 初始化的超管 | **通过**：接口与 Chromium 390px 流程验证批准/拒绝、拒绝理由及持久化、审核审计、申请人通知；超管不能读取他人私人记忆/对话、未加入团队的数据或调用团队工具 | 运维人员执行一次性初始化命令 |
| 来源限流与告警 / 全部 API、平台管理 | 所有 `/api/v1` 接口；认证/MFA/邮箱端点有更严格策略；`GET /platform/security-alerts` | `auth_ip_rate_limits` | 所有 API 客户端受限；仅平台超管查看告警 | **本地通过**：每来源每分钟 1200 次的 API 总限速跨业务路由生效，健康检查不受影响；认证来源限额和脱敏告警已有 MySQL 验证。真实生产告警渠道尚未接入 | 生产监控/值班告警渠道；生产需限流 HMAC 密钥及可信代理网段 |
| 个人数据副本 / 设置页 | `GET /me/data-export` | `users`、`team_memberships`、`user_memories`、`conversations`、`conversation_messages`、`notifications`、`notification_preferences`、`scheduled_jobs`、`agent_runs`、`agent_tool_calls`、`notification_deliveries`、`channel_bindings`、`auth_sessions`、`audit_logs` | 本人 | **通过（隔离 MySQL + Chromium）**：下载 JSON 包含本人数据，未登录拒绝；排除其他用户对话、密码哈希和会话令牌并记录审计。账号注销/逐对话删除及本地留存清理已有隔离 MySQL 与 Chromium 证据；外部不可变审计归档和生产法律期限仍待部署 | 无 |
| 账号注销与数据留存 / 设置、助手与后台任务 | `POST /me/account`、`DELETE /me/conversations/{id}`；API 内每小时清理任务 | `users`、`auth_sessions`、私人数据表、`channel_*`、`scheduled_jobs`、`notifications`、`notification_deliveries`、`audit_logs` | 本人删除本人数据；系统清理过期记录 | **通过（隔离 MySQL + Chromium 390px）**：当前密码/MFA、所有权及唯一超管保护、私人数据删除、共享数据匿名保留、会话撤销、对话删除来源处理、过期数据窗口均通过；生产外部审计归档/法律期限待配置 | 启用 MFA 时需第二因素；生产需审定留存期限
| 移动端网页（原生壳为可选扩展） | 各页面接口；网页通知使用 `/me/notification-channels/push-subscriptions` | 各业务表、`notification_push_subscriptions` | 与桌面端相同 | **响应式网页及 Android 模拟器浏览器验收通过；实体设备仍未验收**：Android API 35 模拟器 Chrome 124 通过 CDP 触摸完成登录、资料/通知偏好刷新持久化、提醒创建与取消；412×765 CSS px 无页面横向溢出。原生 App 和 FCM/APNs 不属于网页交付门槛；实体 Android/iOS/微信内置浏览器仍待实机。 | Web Push 需 VAPID、HTTPS 和允许访问推送服务；实体设备用于 Safari/微信浏览器验收 |
| 安全与等保三级建设 | `/healthz`、`/readyz` 及全站授权检查 | `auth_sessions`、`audit_logs` 等 | 服务端权限；应用数据库账号仅有目标库增删改查权限 | **部分通过**：技术控制证据见 `ACCEPTANCE.md`；正式测评尚未开展 | TLS、密钥设施、备份、监控和测评环境 |

## 命令与结果

- `scripts/migrate.sh teammind_verify_postgrant_1017x`：新建隔离库后，`000001`–`000010` 全部迁移成功；应用账号可读写 34 张表。将该测试库账号临时放宽至 `ALL` 后重跑，权限再次收敛到该库的增删改查；其他数据库授权不变。
- `scripts/migrate.sh teammind_auth_rate_fresh_1018`：新建空库后，`000001`–`000012` 全部迁移成功。
- `scripts/rollback.sh teammind_verify_rollback_final_1017x`：隔离空库上的十个回滚迁移按顺序执行，再从空数据库完整迁移成功；回滚命令只处理最新版本，README 要求先备份。
- `cd backend && go test ./...`：通过。
- `cd backend && MYSQL_DSN=<隔离测试库> go test ./... -count=1 -v`：真实 MySQL 测试通过，另覆盖登录来源限流、可信代理解析、超管脱敏告警，以及个人数据导出的认证/隔离/审计。未引用的旧内存存储实现已从 Go 包中移除。
- `TEAMMIND_API_BASE=http://127.0.0.1:8189/api/v1 python3 scripts/acceptance_api.py`：在仅有增删改查权限的 MySQL 应用账号下通过，覆盖注册/登录、项目日期筛选与删除、团队知识审计、邀请/所有权并发、团队角色、提案审批/重审、评论/依赖/协作者增删改查、跨用户/团队拒绝、私人数据隔离、会话撤销和密码重置。
- `python3 scripts/acceptance_reminders.py`：在 `teammind_verify_reminder_20261017a` 隔离库使用 Chromium 手机触屏创建/取消提醒；将测试数据设为到期后由接口常驻后台进程写入收件箱，刷新后记录仍在；320px 页面无横向溢出。另在到期前停止 API、仅将隔离夹具设为到期再重启服务，后台进程恢复后只投递一次，`/healthz` 和 `/readyz` 正常。
- AI 与记忆：接口测试覆盖 SSE 片段、个人/团队上下文确认、来源消息身份、待确认候选、重复冲突、复核/到期持久化；模型服务提供方候选提取解析器单测只接受用户来源并拒绝敏感候选。未配置真实模型密钥，线上模型调用未验证。
- 通知：MySQL 集成单测验证事务投递记录幂等；禁用偏好时只产生 `suppressed`（已抑制）投递记录，不产生收件箱通知。
- `python3 scripts/acceptance_browser.py`：通过。Chromium 实际注册、找回、单次密码重置及重新登录，并覆盖六种登录按钮状态；截图位于 `/tmp/teammind-browser-current/`。
- `python3 scripts/acceptance_browser_workflow.py`：通过。两个浏览器上下文中，Chromium 覆盖账号菜单进入设置、退出后旧令牌返回 401、主题刷新保持、顶栏搜索、通知入口、390px 触摸邀请和导航抽屉、项目分页/筛选、任务搜索/排序、项目到任务与甘特图跳转、概览统计对账和近期任务详情；还覆盖资料/团队设置、延迟旧响应后的团队隔离、跨浏览器撤销/续期/改密、项目增删改查、成员角色操作、通知设置、依赖、评论、提案审批、甘特图、协作者，以及桌面/手机只读智能体工具查询。
- `python3 scripts/acceptance_settings.py`：新建隔离 MySQL 库并迁移后通过。Chromium 验证个人数据 JSON 下载及账号归属、资料/团队时区保存、无效时区不提交、刷新后数据仍在、会话续期和撤销后立即返回登录，以及通知渠道说明；设置/通知页在 320/360/375/390/430/768/1024/1280px 均无整页溢出。
- `scripts/migrate.sh teammind_emailchange_acceptance_20261018c`：从空隔离库执行迁移 `000001`–`000013` 成功；`MYSQL_DSN=<该隔离库> go test ./... -count=1` 通过。新增邮箱变更集成用例覆盖错误密码、邮箱冲突、过期/重放/并发令牌、无旧会话的确认、会话撤销、新邮箱登录与脱敏审计。
- `TEAMMIND_WEB_BASE=http://127.0.0.1:5175 TEAMMIND_API_BASE=http://127.0.0.1:8189/api/v1 python3 scripts/acceptance_settings.py`：Chromium 设置全回归通过；额外在无登录的第二浏览器打开验证链接，确认成功后旧浏览器令牌返回 401，并使用新邮箱登录。
- `scripts/migrate.sh teammind_mfa_verify_20261018d`：从空隔离库执行迁移 `000001`–`000014` 成功；`MYSQL_DSN=<该隔离库> go test ./... -count=1` 通过，覆盖 RFC TOTP 向量、密钥加密与用户隔离、恢复码哈希与单次使用、限时登录挑战和关闭后的会话撤销。
- `scripts/migrate.sh teammind_mfa_browser_20261018e`：从空隔离库执行迁移 `000001`–`000014` 成功；配置临时 MFA 密钥后设置页 Chromium 回归通过，覆盖 TOTP 设置、登录、恢复码下载、关闭 MFA、跨设备会话失效和 320–1280px 视口。
- `MYSQL_DSN=<迁移 15 隔离库> APP_ENV=development go test ./internal/live -run 'TestAccountDeletion|TestRetention|TestConversationDeletion' -count=1 -v`：通过；覆盖 MFA 强制、所有权/唯一超管阻止、私人数据删除、团队记录匿名保留、对话所有权拒绝和引用清理、各类过期记录窗口。
- `TEAMMIND_WEB_BASE=http://127.0.0.1:5176 TEAMMIND_API_BASE=http://127.0.0.1:8190/api/v1 python3 scripts/acceptance_account_deletion.py`：390px Chromium 手机触屏通过，覆盖错误密码恢复、删除确认、令牌失效和原邮箱登录拒绝。
- `TEAMMIND_WEB_BASE=http://127.0.0.1:5177 TEAMMIND_API_BASE=http://127.0.0.1:8191/api/v1 python3 scripts/acceptance_settings.py`：2026-10-19 在迁移 15 隔离库、临时 MFA 密钥和独立 API/Vite 上完整重跑通过；首轮团队表单等待断言超时，后续重跑通过，保留为低频测试不稳定风险。
- `python3 scripts/acceptance_cross_browser.py`：2026-10-19 重跑 Firefox + WebKit，登录/注册/找回/重置和 11 个业务路由（含审批页）在八种视口均通过；不代表实体设备验证。
- OpenClaw 微信插件隔离实验：在 `/tmp` 安装并启动 OpenClaw 2026.8.1 与腾讯 `@tencent-weixin/openclaw-weixin@2.4.10`；发现上游未声明 `web.login.start`/`web.login.wait`，临时增加元数据后从 Gateway 获得真实 QR。使用 `OPENCLAW_LOG_LEVEL=error` 时共享日志未出现二维码 URL。10 秒 `web.login.wait` 调用超时，未进行真人扫码；该代码没有进入 TeamMind 部署路径。
- `scripts/migrate.sh teammind_mfa_replay_20261018f`：从空隔离库执行迁移 `000001`–`000015` 成功；`APP_ENV=development go test ./... -count=1` 通过。新增并发回归在 `-race` 下通过：两个不同登录挑战并发提交同一有效 TOTP，恰有一个成功、另一个因时间步已消费而拒绝。使用迁移 15 和临时 MFA 密钥重跑 `scripts/acceptance_settings.py`，MFA 启用、登录、恢复码下载、关闭、会话撤销及 320–1280px 页面回归通过。
- `python3 scripts/acceptance_cross_browser.py`：Firefox 与 WebKit iPhone 视口仿真对注册、找回、重置、登录和 11 个业务路由通过；结果不代表实体 iOS/Android/微信浏览器测试。
- `python3 scripts/acceptance_cross_browser.py`（2026-10-19）：重跑 Firefox 桌面与 WebKit 手机视口，八个宽度下全部 11 个业务路由（含审批）及登录/注册/找回/重置检查通过；此前 WebKit 在审批页退出的问题本次未复现。该脚本不覆盖实体设备或设置控件逐项操作。
- `python3 scripts/acceptance_platform.py`：在全新迁移的隔离数据库 `teammind_auth_platform_ui_final_1018` 上通过。测试错误邮箱被接口拒绝、合法申请人可登录；390px 页面批准/拒绝、拒绝原因、平台审计、申请人通知与投递记录、登录限流告警展示、团队所有者越权拒绝均通过。临时凭据只在进程环境中使用，进程结束后丢弃。
- 持久化重启验收：通过 接口创建团队和带复核/到期日期的个人记忆，重启 Go 服务后使用同一有效会话重新读取，内容和日期均保持一致；`/readyz` 为 ready。
- `npm run type:check` 与 `npm run build`：通过。
- 重启 Go 接口服务重启后，`/healthz`、`/readyz` 返回 200；迁移验证库中的测试团队和审批历史在重启后仍存在。
- `scripts/migrate.sh` 面对当前旧 `teammind` 数据库时按预期拒绝：库内已有表却没有迁移版本记录；未修改该库业务数据。

## 登录故障根因与改动

历史样式把十六进制颜色赋给 Arco 的 `--primary-6`，而组件使用 `rgb(var(--primary-6))`，导致默认状态背景声明无效；历史登录还使用固定演示凭据与本地演示会话。本次审计开始时，当前业务登录已能通过 Chromium 验收。本次移除未使用的旧业务样式导入，并为主按钮默认、悬停、聚焦、按下、加载和禁用状态显式设置可读颜色。当前账号使用 MySQL 中的 bcrypt 哈希和可撤销哈希会话；没有演示密码预填或浏览器明文密码存储。

2026-10-19 补充复验：在 390px Chromium 页面、鼠标未悬停时实测按钮为可见、启用、44px 高，白字/蓝底对比度为 5.19:1；真实坐标鼠标单击中心和右边缘均经 API 完成登录。空表单字段错误、错误凭据反馈、服务端延迟期间 loading 与解除、键盘焦点轮廓、Enter、Space 和 390px WebKit 触屏登录均以自动化断言验证。Chromium 的 Playwright `locator.click()` 仍会等待稳定状态：此环境里的 Chromium 即使在 `about:blank` 也不派发 `requestAnimationFrame`；同一元素用真实坐标鼠标事件可提交，Firefox/WebKit 的动画帧和稳定点击检查正常。这是本地无头浏览器运行限制，不能据此判定页面按钮不可点击；没有用 `force` 绕过检查。`acceptance_agent_workspace.py` 已支持选择 Firefox/WebKit 并将页面 API 请求显式路由到隔离 API；迁移 21 的新空库上运行 WebKit 390px 流程，验证私人文件创建、服务端校验失败后保留内容、编辑、刷新读回、导出、删除及 320px 无横向溢出，随后注销账号并删除测试库。

## 外部阻塞与剩余事项

账号 MFA、邮箱变更/IP 限流、个人数据导出/注销、私人对话删除和本地留存清理均有实现及隔离 MySQL/Chromium 验收。腾讯插件 Gateway 已完成真人扫码绑定，当前隔离数据库有真实入站记录，API/Gateway/worker 重启后绑定仍能读回；AI 模式为 mock，因此微信真实回复仍未通过。Android API 35 模拟器上的 Chrome 触摸回归通过，但实体 iOS Safari、Android Chrome 和微信内置浏览器未验证。SMTP 未配置；Android Chrome Web Push 订阅受浏览器推送服务错误阻塞，真实送达未验证。智能体白名单、待审批提案、逐次授权的知识候选、断网 Python/Bash 沙箱和加密私人工作区已实现并通过隔离 MySQL/浏览器验收；真实模型凭据未配置，商业模型自主选工具仍未验证，生产沙箱逃逸评估未完成。设置页完整 Chromium 与 WebKit 仿真回归通过；浏览器仿真仍不替代实体设备验收。等保三级只能在真实部署、定级备案、管理制度、日志/备份证据和第三方测评完成后表述为“通过”。

## 2026-10-19 真人扫码确认后的登录与工作区续验

- 用户确认已在手机微信中扫码并确认。保留的 API `127.0.0.1:8240` 健康/就绪检查均为 200；只读聚合显示 1 个活动微信渠道账户、1 条有效绑定、1 条入站、1 个隔离会话、1 条用户消息、0 条助手消息、1 条失败投递、0 条成功投递。此次查询不读取内容、身份标识或凭据。真实回复外发仍因 `AI_PROVIDER=mock` 被拒绝，不能标记为 AI 微信闭环通过。
- 登录实际浏览器复验覆盖未悬停、悬停、键盘焦点、加载、失败、成功六种状态；真实鼠标中心和左右边缘事件、Enter、Space、WebKit 390px 触屏都通过真实账号登录与目标路由断言。按钮未悬停时 233×44 CSS px、白字/蓝底对比度 5.19:1。测试账号通过注销 API 清除。
- 登录定位器超时根因是本机 Chromium headless shell 对空白页和 TeamMind 页都不派发 `requestAnimationFrame`，导致 Playwright 等待控件“稳定”；该环境的标准截图也卡在字体等待。Firefox/WebKit 帧调度和稳定定位器检查通过。Chromium 真实坐标鼠标单击通过同一表单及真实后端 API，没有用 `force` 选项规避问题；六状态使用自动化断言验收。
- `scripts/acceptance_agent_workspace.py` 新增 `TEAMMIND_BROWSER` 引擎选择和隔离 API 路由。新空库执行迁移 `000001`–`000021` 后，WebKit 390px 浏览器验收通过私人工作区创建、字段校验失败后保留内容、编辑、刷新回读、数据导出、删除和 320px 页面宽度检查。账号注销、隔离 API、迁移库均已清理。
- 本轮检查 `python3 -m py_compile scripts/acceptance_agent_workspace.py`、`npm run type:check`、`npm run build` 均通过；主 API/Gateway/微信 Worker/MySQL 与网页开发服务保留，临时 API 和端口已关闭。

## 2026-10-19 WebKit 审批与设置回归续验

- 新空 MySQL 迁移 21 库 + 独立 API/Vite 上运行 `TEAMMIND_BROWSER=webkit python3 scripts/acceptance_browser_workflow.py`，完整双浏览器审批工作流通过；页面进程正常结束，无此前报告的 WebKit 审批页崩溃。
- 另一座新空迁移 21 库上运行 `TEAMMIND_BROWSER=webkit python3 scripts/acceptance_settings.py`，资料、团队、邮箱跨浏览器确认、MFA/TOTP、恢复码、会话、个人数据导出、通知偏好、提醒，以及 320–1280px 设置/通知/MFA 视口均通过。
- 两座隔离数据库、API 与临时 Vite 已关闭并删除；真人微信主验收数据库未用于浏览器流程。主 API `/healthz`、`/readyz` 仍为 200。
- 最新只读微信汇总为 1 个活动插件账户、1 条有效绑定、1 条入站、1 个会话映射、1 条用户消息、0 条助手消息、0 条成功/1 条失败投递；同步时间为 `2026-10-19T15:05:07.294Z`。失败仍是 AI 模拟模式下拒绝外发，真实模型回复未完成。
- 现有运行环境未提供 AI 服务地址/模型名/密钥，也没有本机模型服务或模型文件；真实 AI 对话、模型自主选择工具、记忆提取及微信回复仍待真实模型环境。SMTP 与有效 VAPID 联系方式/浏览器订阅也未配置，因此真实邮件/Web Push 投递仍未证实。实体移动浏览器缺少接入设备；原生 App 属用户确认之外的网页外可选范围。

2026-10-19 又在迁移 16 的隔离 API/Vite 环境重跑 `scripts/acceptance_cross_browser.py`：Firefox 桌面与 WebKit 手机视口仿真均通过全部 11 个业务路由和 8 个视口，脚本现在接受隔离地址并在失败后尝试注销随机账号。此次测试仍不覆盖真实设备。

## 七项遗留能力逐项复核（2026-10-19）

| 能力 | 当前状态 | 证据或剩余工作 |
| --- | --- | --- |
| 腾讯 OpenClaw 微信插件完整接入 | **真人扫码绑定和真实入站通过；AI 回复外发未通过** | 清理测试夹具后当前 MySQL 有 1 条有效绑定、1 条真实入站记录及 1 个隔离会话映射；API/Gateway/worker 重启读回此前已验。当前 `AI_PROVIDER=mock`，唯一投递失败，worker 按设计不发送模拟回复。 |
| 真实 AI 服务 | **未验证** | OpenAI 兼容适配器和模拟模式存在；没有真实服务凭据，真实对话、记忆提取和模型自主工具选择未验收。 |
| 智能体写入、执行和文件能力 | **受限能力已实现，超出范围的操作未开放** | 有白名单任务读取、待审批任务与项目提案、逐次授权的团队知识候选、断网 Python/Bash 沙箱，以及本人 AES-GCM 加密的持久文本工作区和逐次授权的输出保存。成员/邀请等其他高影响业务提案尚未实现；任意宿主机 Shell 或访问宿主机文件系统仍关闭。全新 API 进程重启后的工作区读回已通过。 |
| MFA、邮箱验证和来源限流 | **代码与隔离 MySQL 集成测试通过** | 本轮在迁移 16 的隔离库重新运行 MFA 启用/登录/恢复/关闭、邮箱变更验证、来源限流/超管告警相关测试。生产密钥托管、可信代理与外部告警联动仍待部署验收。 |
| 个人数据访问、导出、删除和留存 | **代码与隔离 MySQL 集成测试通过** | 本轮重新运行个人数据导出、账号注销及留存窗口测试。团队共享数据期限需由部署方法务核定；外部不可变审计归档尚未部署。 |
| 原生 iOS/Android 应用（可选范围） | **TeamMind 核心为网页；原生应用不是网页交付前置项** | 仓库含复用网页的 Capacitor 壳；Android Debug 模拟器可启动，但 iOS 发布构建和 FCM/APNs 未验。如产品范围维持网页，可将它们从核心未完成功能中排除。 |
| 移动设备设置页验收 | **Chromium、WebKit 和 Android Chrome 模拟器通过；实体设备未验证** | Chromium 与 WebKit 均完成设置全流程回归；Android Chrome 124 模拟器完成触摸登录、资料/通知偏好保存刷新、提醒创建取消；Firefox/WebKit 全路由视口仿真通过。未在实体 Android Chrome、iOS Safari 或微信内置浏览器上验收。 |

本轮数据库验证使用隔离库 `teammind_goal_20261019b`，通过读取本地隔离 API 进程的运行环境取得 DSN；测试使用临时密钥，不输出 DSN 或密钥。执行 `go test -count=1`，覆盖 `TestMFAEnrollmentLoginRecoveryAndDisable`、`TestEmailChangeVerificationLifecycle`、`TestLoginIPRateLimitAndPlatformAlert`、`TestPersonalDataExportIsAuthenticatedAndScoped`、`TestAccountDeletionRequiresOwnershipTransferAndPurgesPrivateData`、`TestRetentionPolicyPrunesExpiredRecordsAndPreservesRecentHistory`，结果通过（38.199 秒）。这证明代码和隔离数据库流程，不证明生产密钥托管、外部渠道送达或实体设备行为。

## 2026-10-19 继续验收记录

- 在隔离迁移 18 MySQL `teammind_goal_wechat_20261019a` 上，智能体待审批提案专项测试与完整 `go test ./... -count=1` 均通过（全量 38.509 秒）。审批事务断言覆盖正式任务更新、计划版本、审批记录、通知、审计和 outbox。测试查询的通知外键列名已按实际 schema 修正为 `resource_id`。
- WebKit 设置回归 `TEAMMIND_BROWSER=webkit TEAMMIND_WEB_BASE=http://127.0.0.1:5211 TEAMMIND_API_BASE=http://127.0.0.1:8211/api/v1 python3 scripts/acceptance_settings.py` 通过；WebKit 双浏览器业务审批流程 `TEAMMIND_BROWSER=webkit TEAMMIND_WEB_BASE=http://127.0.0.1:5211 TEAMMIND_API_BASE=http://127.0.0.1:8211/api/v1 python3 scripts/acceptance_browser_workflow.py` 通过。WebKit 审批流程先前所称未跑完在本次已完成复测。
- 以上均是隔离 MySQL 与浏览器自动化证据；未产生真人微信确认、真实模型供应商响应或实体 Android/iOS/微信浏览器证据。Capacitor 原生壳仍属可选项，不作为网页产品核心的交付门槛。
- Agent 从单工具限制扩为最多三步的串行白名单工作流；当前实际业务组合为先读取团队任务，再创建一项待审批任务提案。计划提案或 Python/Bash 沙箱结束工具链；沙箱依旧需逐次授权且为本轮唯一工具。每个步骤复核团队权限并单独留审计。工作流的隔离 MySQL 测试以及 390px Chromium 展示/授权流程通过；真实模型凭据未配置，未验证商业模型实际采用该流程。
- 对上述实现执行 `go test ./... -count=1`（迁移 18 隔离 MySQL，最终复跑 63.973 秒）、`go test -race ./internal/live -run '^TestModelSelectedWriteToolCreatesOnlyPendingProposal$' -count=1`、`npm run type:check && npm run build`：全部通过。`cd backend/wechat-plugin-bridge && npm test` 4/4 通过；`scripts/acceptance_wechat_plugin.py` 通过真实插件二维码/等待/取消阶段，但真人扫码未发生。
- 账号安全/生命周期 6 项集成测试在隔离迁移 18 MySQL 重跑全部通过（50.587 秒）。另在当前运行的 real-mode API `127.0.0.1:8194` 重跑微信插件验收，真实 QR/等待/取消均通过；未进行真人扫码。

## 2026-10-19 后续复核

- 重新核对 `backend/internal/live/wechat_gateway.go`、`wechat_worker.go`、`backend/wechat-plugin-bridge/` 和锁文件：当前 Go 服务只访问认证 loopback 桥接；实际 Gateway 固定加载 OpenClaw `2026.10.4` 与腾讯 `@tencent-weixin/openclaw-weixin@2.4.10`。扫码实现调用插件登录模块，收发调用插件 API 适配器。因此“Go 直接实现 iLink、没有运行腾讯插件”的描述与当前工作区不符。真人扫码及真实微信收发仍未验证。
- `cd backend/wechat-plugin-bridge && npm test`：4 项通过，覆盖桥接鉴权/回环限制、腾讯插件消息 API 映射、扫码取消，以及模拟插件确认后返回身份凭据并清除一次性会话。该确认用例使用本地模拟 HTTP 响应，不等价于真人扫码。
- `TEAMMIND_API_BASE=http://127.0.0.1:8197/api/v1 python3 scripts/acceptance_wechat_plugin.py`：通过真实腾讯插件 Gateway 获得非模拟二维码、读取扫码等待状态、取消会话，并确认仅创建二维码不会写入绑定。测试未打印二维码内容，没有真人扫码。
- 本轮再次以隔离迁移 18 数据库启动 real-mode API，并连接运行中的 OpenClaw `2026.10.4` Gateway / 腾讯插件 `2.4.10`；`TEAMMIND_API_BASE=http://127.0.0.1:8200/api/v1 python3 scripts/acceptance_wechat_plugin.py` 全部通过：插件模式、非模拟 QR、状态轮询、取消持久化、扫码前无绑定。没有真人扫码；因此真人确认和消息闭环仍未验证。
- `scripts/migrate.sh teammind_goal_wechat_20261019a`：从空 MySQL 库完成迁移 `000001`–`000018`。在该隔离库执行 `go test ./... -count=1`：通过（52.574 秒），包括微信成员授权、加密凭据、绑定数据经新 API 实例重读、入站消息 SQL 隔离，以及 MFA、邮箱验证、限流、数据导出、账号删除和留存测试。
- `npm run type:check && npm run build`：通过。
- 在上述迁移 18 隔离库以独立 API/Vite 进程运行 `scripts/acceptance_settings.py`：Chromium 通过个人资料和团队设置持久化、邮箱跨浏览器确认、TOTP 启用/登录/恢复码/关闭、会话撤销、个人数据导出、通知偏好、提醒以及 320–1280px 设置/通知页面检查。
- 在同一隔离环境运行 `scripts/acceptance_cross_browser.py`：Firefox 桌面和 WebKit 手机视口仿真均通过登录/注册/找回/重置及 11 个业务路由在 320/360/375/390/430/768/1024/1280px 的检查；此前 WebKit 进程退出问题本次未复现。不是实体 iOS Safari、Android Chrome 或微信浏览器验收。
- 真实 AI 地址、模型和 API 密钥、SMTP/Firebase 凭据均未配置；VAPID 联系地址是占位值，修复后 API 报告未配置；模拟器推送订阅失败。Android Debug APK 已通过本地构建并在 Android API 35 模拟器安装，完成真实隔离 API 注册/登录；这不是实体 Android Chrome 或 FCM 送达验收。当前仍无 Xcode/CocoaPods、Firebase 客户端配置或连接的实体设备。
- Android 模拟器实际验收：设置 `TEAMMIND_API_BASE_URL=http://localhost:8199/api/v1`、`ALLOW_INSECURE_NATIVE_API=1`、`NATIVE_PUSH_OPTIONAL=1`、`NATIVE_TARGET=android` 后运行 `npm run build:native`，再以 JDK 21/Android SDK 35 运行 `android/gradlew assembleDebug`；构建成功。将 APK 安装到 `TeamMind_API35` 模拟器并通过 `adb reverse tcp:8199 tcp:8199` 连接隔离 API，实际完成注册、登录，进入设置页；WebView 版本 124、屏幕 1080×2400、CSS 视口 412×915，设置页 `scrollWidth` 为 412。Firebase 客户端配置缺失，原生推送入口不能视为已接通。该构建为仅本机 HTTP 的 Debug 包，不可用于生产。
- `TEAMMIND_WEB_BASE=http://127.0.0.1:5191 TEAMMIND_API_BASE=http://127.0.0.1:8198/api/v1 python3 scripts/acceptance_android_chrome.py`：Android 15 / API 35 模拟器的 Android Chrome 124 实际运行；CDP 触摸完成登录、资料/通知偏好持久化及提醒创建/取消，刷新后数据和取消状态正确，412×765 CSS px 页面无横向溢出，测试账号注销成功。该结果是模拟器 Chrome 验收，不代表实体 Android 设备。
- `TEAMMIND_BROWSER=webkit TEAMMIND_WEB_BASE=http://127.0.0.1:5191 TEAMMIND_API_BASE=http://127.0.0.1:8198/api/v1 python3 scripts/acceptance_settings.py`：完整设置工作流在 WebKit 上通过，包含跨浏览器邮箱更新、TOTP/MFA、会话撤销、通知与提醒和所有设置相关页面的 8 种视口；该浏览器仿真不等于实体 iOS Safari。
- `TEAMMIND_API_BASE=http://127.0.0.1:8200/api/v1 python3 scripts/acceptance_wechat_plugin.py`：本轮重新通过当前 real-mode API 与腾讯插件 Gateway 的非模拟二维码获取、等待状态轮询、取消及“扫码前无绑定”检查。脚本不输出二维码；无人扫码，因此真人确认、绑定成功和消息往返仍未验证。
- 在当前迁移 18 隔离 MySQL 上重跑 MFA/TOTP、邮箱更改、可信来源解析与限流、个人数据导出、注销和留存清理的 12 个集成测试：全部通过（58.136 秒）。生产密钥托管、SMTP 和外部告警仍需部署配置与真实投递验收。
- 同一隔离库重跑模型选写工具、断网 Python 临时沙箱、加密私人工作区 CRUD 与跨用户隔离的 4 项测试：通过（0.343 秒）。正式任务计划仍走审批；宿主机任意 Shell 与未授权业务写入保持关闭。

## 2026-10-19 智能体 Shell 与设置复验续记

- 修正受限 Bash 执行器的运行时检查：Bash 沙箱现在检查 bubblewrap、prlimit、Bash 与非 root 条件，不再错误地依赖 Python 运行时。智能体审计摘要现在按实际持久化状态记录临时/已保存标记；工具回复也明确区分“生成文件已写入私人工作区”和仅本次可见的标准输出。
- 新增审计摘要单元测试，确保两类保存状态正确，且审计不包含命令、输出、文件名或内容。隔离 MySQL 上 Python 与 Bash 的模型工具集成测试通过；Bash 断网、无宿主机文件/环境变量访问、只读根目录和显式授权路径通过。全量 Go 测试、沙箱相关 `go test -race`、`npm run type:check && npm run build` 均通过。
- `npm test`（腾讯微信桥接）4/4 通过；real-mode TeamMind API 再次从正在运行的 OpenClaw `2026.10.4` / 腾讯插件 `2.4.10` 获得二维码、轮询并取消。未打印二维码，也没有真人扫码，绑定与消息往返仍未验证。
- `scripts/acceptance_agent_shell_ui.py` 在 390px Chromium 上验证本次 Shell 授权独立于 Python、执行后授权复位，并通过真实私人工作区 API 保存结果。该脚本的 AI 响应由浏览器测试桩提供，不证明真实模型已连接。
- `scripts/acceptance_settings.py` 的 Chromium 与 WebKit 全设置流程均通过，覆盖个人数据导出、资料/团队设置、邮箱跨浏览器确认、TOTP/MFA、会话跨设备撤销、通知/提醒和 320–1280px 页面。WebKit 首次失败来自测试脚本按列表首项撤销会话；已改为明确选择非当前设备会话，重跑通过。
- 在独立限流密钥的 API/Vite 实例上，`scripts/acceptance_browser_workflow.py` 的 WebKit 双浏览器全流程通过，实际完成邀请接受、成员角色、项目/任务、计划提案修改/重提/审批（含状态和进度更新）、甘特筛选和跨团队隔离。共享限流实例第一次在注册阶段返回 429，隔离新实例重跑通过。
- 扩展智能体待审批写入提案：模型现在可提议任务状态、进度及既有计划字段变更；提案仍通过版本检查，审批前正式任务不变，审批后在同一事务更新任务、版本历史、审批记录、通知、审计和 outbox。隔离 MySQL 集成测试验证模型工具只创建 pending 提案，并由管理员批准后才将状态/进度写入正式任务；WebKit 流程也实际批准了状态 `doing` 和进度 `65%`。
- 仍缺真实模型凭据、真人微信扫码与收发、SMTP/VAPID 外部送达及实体 Android/iOS/微信浏览器。TeamMind 核心仍是网页；原生壳是可选扩展。

## 2026-10-19 继续复核

- 重新运行 Firefox 桌面和 WebKit 移动仿真：登录/注册/找回/重置及 11 个业务页面（包括审批）在 320–1280px 全部通过。WebKit 全设置专项也通过，覆盖 MFA、邮箱、会话、通知和提醒。
- 微信状态轮询故障复核后，状态字段已从互斥锁改为不可变原子快照；扫码长轮询不再和 API 状态读取争用同一把锁。Go 全量隔离 MySQL 测试通过，新增并发快照 `-race` 测试通过。一个旧进程中的临时会话曾稳定超时；重启到新构建后真实插件新会话的 QR/等待/取消正常。
- 手机页面的轮询失败不再显示旧的“等待扫码”。在 390px Playwright 浏览器中注入一次 503 后，界面明确显示“状态未知”，刷新能取得新二维码并恢复实时状态。
- 一次真实腾讯二维码通过临时隔离账号展示 5 分钟，未收到真人扫码确认并已过期；未写入真实微信绑定，消息往返和绑定后的 Gateway/worker 重启恢复仍未验。
- **产品范围说明**：TeamMind 按网页产品交付，站内通知和浏览器 Web Push 属于网页能力；原生 FCM/APNs 是仓库中的可选 Capacitor 扩展，不应作为网页产品的上线前提。网页邮件的 SMTP 未配置；Web Push 的 VAPID 与订阅加密密钥已配置，但推送订阅失败，仍缺真实送达验收。当前仓库没有原生正式发布包、iOS 构建和实体推送验收。
- `go test ./... -count=1` 首次在含历史隔离测试告警的数据库中失败，原因是测试将全库告警条数硬编码为 1；已改为按本用例 HMAC 来源匹配。修复后全量 Go 集成测试通过（42.037 秒）。`npm test` 4 项通过；`npm run type:check && npm run build` 通过。

## 2026-10-19 分项复验续记

- 从空隔离 MySQL 建立 `teammind_step_goal_20261019` 并应用迁移 `000001`–`000018`；受限应用账号启动后的 `/healthz`、`/readyz` 均正常。全量 `go test ./... -count=1` 在该空库通过（43.767 秒）。另在已有迁移 18 隔离库复跑也通过。
- 修复微信 worker 测试的唯一键夹具冲突：测试发送者哈希过去为固定值，旧隔离记录可能使后续测试冲突；现在按本用例随机渠道账号生成，目标测试及干净迁移库全量 Go 测试通过。生产微信身份校验未改变。
- 前端类型检查/生产构建通过；腾讯 OpenClaw 桥接的 Node 测试 4/4 通过。Firefox + WebKit 的全路由/八视口检查通过，WebKit 设置专项和包含审批修改、重提的 WebKit 双浏览器业务流程通过。
- real-mode TeamMind API 再次通过腾讯插件二维码/等待/取消和“扫码前不写绑定”验收。**真人扫码、持久绑定、微信真实消息往返及绑定后的 Gateway/worker 重启恢复仍未验证**。
- 本轮在新建迁移 18 隔离库执行 `scripts/acceptance_agent_workspace_restart.py` 通过：AES-GCM 文件存入 MySQL，完整停止并重启 API 后用原登录会话读回原文，最后清理测试账号。这证明私人工作区文件具备跨 API 进程重启持久性，不开放宿主机文件访问。
- MFA/TOTP、邮箱变更、限流和脱敏告警、个人数据导出/注销/留存已有代码及 MySQL 集成覆盖；智能体有白名单工具、待审批提案、断网 Python/Bash 与加密私人工作区。任意宿主机 Shell 和绕过审批直接改正式业务数据不属于应开放的能力。
- 真实模型密钥、SMTP/Firebase 凭据均未配置；VAPID 联系地址是占位值，修复后 API 报告未配置；模拟器推送订阅失败。当前主机没有连接的 Android 设备，只有 platform-tools/JDK 17，且没有 Xcode/CocoaPods；实体手机浏览器和原生推送不能标为通过。Capacitor 为可选壳，TeamMind 核心仍是网页。
- 本次 Android API 35 模拟器中的 Capacitor WebView 实际触摸登录成功，并打开设置/通知页面；首次失败定位为验收服务未允许 Debug 来源 `http://localhost`，配置精确加入后通过。412×915 CSS px 下页面无横向溢出，未配置的 SMTP/VAPID/Firebase 均明确显示为不可用。此项不代表实体手机或推送已验收。

## 仍未实现与外部阻塞（2026-10-19）

| 类别 | 当前状态 | 要达到完整可用还需什么 |
| --- | --- | --- |
| 产品范围：邀请触达 | 邀请链接创建、复制、跨浏览器接受已实现；没有自动邀请邮件或短信发送 | 如需自动触达，需增加邀请投递流程，并配置邮件或短信服务 |
| 产品范围：离线使用 | PWA 可安装，但不缓存私人业务数据，也不能离线查看或编辑 | 若要离线业务，需另行设计加密本地缓存、冲突合并及远程注销清除；当前不支持 |
| 产品范围：微信媒体 | 插件桥接当前只发送文本；图片、语音、文件和其他媒体能力未实现 | 按腾讯插件实际支持范围逐类接入、验证权限与脱敏后再开放 |
| 产品范围：智能体自主工作 | 已支持最多三步的串行工具循环、任务/项目/非所有者成员待审批变更、管理员逐次授权的团队知识候选、断网沙箱及本人加密工作区；成员提案 Chromium/WebKit 浏览器审批通过 | 邀请和所有权转移不开放给智能体；商业模型实际选工具待真实凭据验证。绕过人工审批的业务写入、任意宿主机 Shell/文件/网络访问不开放 |
| 真实 AI | 接口适配器存在，当前环境使用 `mock`；真实对话、真实记忆提取和模型自主选择工具未验 | 在服务端受保护环境配置兼容模型地址、模型名及凭据并做真实调用验收 |
| 邮件与浏览器推送 | SMTP、Web Push 适配器、持久队列和重试代码存在；当前未配置生产 SMTP；VAPID 密钥格式有效，但联系地址是占位值，状态 API 会报告未配置；没有活动订阅和真实送达证据 | 配置 SMTP、HTTPS、VAPID 和订阅加密密钥，并在真实邮箱及移动浏览器验证投递/撤销 |
| 原生 App 与推送 | TeamMind 核心是响应式网页；Capacitor 是复用网页的可选 WebView 外壳，不是独立原生 UI。Android Debug 模拟器的登录、设置、通知页已验；没有正式发布包、iOS 构建或真实 FCM/APNs 送达 | 提供 Firebase 客户端文件、服务账号和 iOS APNs 配置；在实体 Android/iOS 设备完成构建、权限、通知送达及发布签名验收 |
| 微信完整闭环 | 真人扫码、确认、持久绑定和真实入站已通过；净化测试夹具后唯一投递记录因 `AI_PROVIDER=mock` 而失败 | 配置真实模型并完成微信真实回复/外发验收；实体微信内置浏览器仍待测 |
| 生产运行与合规 | 本地服务提供健康检查、持久化、限流和安全控制；生产 TLS/WAF、KMS 密钥轮换、自动加密备份、恢复/灾备演练、集中监控与值班告警、外部不可变审计归档、独立渗透测试和等保三级测评尚未完成 | 在正式部署环境落实运维控制、保留管理证据、定级备案并完成第三方测评与整改；目前不能宣称通过等保三级 |
| 实体设备验收 | 浏览器引擎和视口仿真、Android API 35 模拟器均有记录；实体 Android Chrome、iOS Safari、微信内置浏览器尚未验收 | 需要实体设备及相应系统/浏览器，逐页检查旋转、键盘、安全区、字体放大、弱网和无网恢复 |

## 2026-10-19 当前回合复验

- WebKit 设置专项在空迁移 18 隔离库上完整通过：个人资料与团队设置、数据导出、跨浏览器邮箱确认、TOTP/MFA 启用/登录/关闭和恢复码、不同浏览器会话撤销、会话续期、通知偏好、个人/团队提醒，以及 320–1280px 设置相关页面布局。
- WebKit 双浏览器业务流程完整通过：项目和任务操作、邀请接受/撤销、成员角色、依赖、协作者、甘特筛选、跨团队隔离，以及计划提案要求补充/重提/修改后批准。审批结论来自实际操作流程，不是路由打开检查。
- 测试脚本修复两处可复现问题：会话回归改为按 API 返回的会话 ID 撤销指定浏览器；MFA 设置回归启动前验证服务端 MFA 密钥配置，缺少配置时清理临时账号并立即报出前置条件，避免跑到流程中途才失败。
- 在全新迁移 18 隔离库 `teammind_goal_security_20261019d` 执行 `go test ./... -count=1`：通过，`internal/live` 用时 50.082 秒。腾讯桥接 `npm test` 4/4 通过，`npm run type:check`、`npm run build` 和验收脚本 Python 语法检查通过。
- TeamMind real-mode API 本轮再次通过真实插件 QR 创建、状态轮询、取消和扫码前无绑定检查。Go 通过 loopback Gateway 调用固定 OpenClaw `2026.10.4` / 腾讯插件 `2.4.10`，没有自行实现 iLink 协议。真人扫码/绑定、真实消息往返和绑定后进程重启恢复仍未验证。
- 真实模型凭据、SMTP 配置、Web Push 实际订阅/送达、实体 Android/iOS/微信浏览器，以及生产安全运维与等保三级测评仍未完成；WebKit 自动化不替代实体设备验收。

Android Debug 的 API 跨域来源已在 `.env.example` 补充；本轮模拟器使用的隔离 API 明确放行 `http://localhost` 后登录通过。现有私有 `.env` 与生产部署不会被自动改动，构建/部署时仍须按真实 Web、Android、iOS 来源配置精确的 `CORS_ORIGIN`。

### 对原清单中已过时状态的更正

- 清单第 1 条把当前实现写成“Go 直接实现 iLink、没有实际运行腾讯插件”，与工作区不符。Go 只调用 loopback Gateway；本轮通过运行中的 OpenClaw `2026.10.4` / 腾讯插件 `2.4.10` 再次取得真实插件 QR、轮询并取消。尚未完成的是真人确认后的真实绑定、消息往返和绑定后的整进程重启验收。
- 清单第 4 条把 MFA、邮箱变更、来源/IP 限流和告警写成尚未实现；代码、MySQL 集成测试及 WebKit 设置专项已覆盖这些功能。未完成的是生产密钥托管/轮换、SMTP 实际送达和生产告警联动。
- 清单第 5 条把个人数据访问/导出、删除和留存写成待实现；个人数据导出、注销清理和有界留存任务已有实现及隔离 MySQL 测试。仍待部署确认的是团队共享数据的法定期限和外部不可变审计归档。
- 清单第 6 条把仓库描述为完全没有原生端；当前有可选 Capacitor 外壳和 FCM 服务端适配器。它仍是 WebView 外壳而非独立原生 UI；Android Debug 模拟器已验，iOS 正式构建、Firebase/APNs 配置及真机推送未验。
- 清单第 7 条所说 WebKit 审批流程未完整运行已过时：本轮完整双浏览器工作流实际执行了计划提案修改、补充后重提和批准，退出码为 0；完整设置专项也退出码为 0。实体浏览器验收仍未完成。

## 2026-10-19 七项遗留清单继续验收

- **微信插件起始流程**：当前运行的 OpenClaw Gateway 使用 Node.js 24.16.0；桥接依赖锁定 OpenClaw `2026.10.4` 与腾讯 `@tencent-weixin/openclaw-weixin@2.4.10`。`npm test` 4/4 通过；`TEAMMIND_API_BASE=http://127.0.0.1:8194/api/v1 python3 scripts/acceptance_wechat_plugin.py` 通过真实插件非模拟二维码、等待状态、取消、取消状态及扫码前不写绑定。此测试没有真人扫码，不是微信完整闭环验收。源代码确认 Go 经受令牌保护的 loopback 桥接调用 Gateway；“Go 直接实现 iLink、没有运行腾讯插件”是过时描述。
- **微信真人验收环境已就绪**：从空库执行 `scripts/migrate.sh teammind_worker_goal_20261019`，启动 real-mode API `127.0.0.1:8207`、网页 `127.0.0.1:5217` 和共享同一隔离数据库/密钥的 `wechat-worker`；`/readyz` 为 ready，真人扫码起始验收通过。桌面打开网页后可自行注册隔离测试账号、创建团队并进入微信渠道页扫码。该库仅用于验收；当前仍无真人绑定或消息往返。
- **账户安全与数据生命周期**：在隔离迁移 18 MySQL 上重跑 MFA/TOTP、邮箱变更、登录来源限流/告警、个人数据导出、账号注销、留存清理、私人工作区 CRUD/隔离、微信绑定成员关系/加密凭据/重启读回、模型待审批任务提案和团队知识候选等专项测试，全部通过；完整 `go test ./... -count=1` 同库通过（40.603 秒）。这些本地证据不代表生产密钥托管、真实外部告警、法规留存期限或不可变审计归档已经落实。
- **WebKit 设置回归**：`TEAMMIND_BROWSER=webkit TEAMMIND_WEB_BASE=http://127.0.0.1:5194 TEAMMIND_API_BASE=http://127.0.0.1:8202/api/v1 python3 scripts/acceptance_settings.py` 完整通过，覆盖资料/团队、跨浏览器邮箱确认、TOTP/MFA、恢复码、会话续期/撤销、数据导出、通知偏好、提醒及 320–1280px 设置相关页面。修正验收脚本对登录 URL 的匹配，容许会话失效后的 `?redirect=` 查询参数。
- **WebKit 双浏览器业务和审批**：旧测试 API 8203 返回过时 JSON 契约错误；没有据此判定产品审批失败。用仓库当前源码启动隔离 API 8206 后，同一请求通过 JSON 解析并进入业务校验；新的完整 `acceptance_browser_workflow.py` 在 WebKit 双浏览器中通过项目/任务、提案修改后批准、补充/重提、甘特、成员、邀请、跨团队隔离和移动抽屉。失败诊断只记录请求字段名和类型，不记录字段值。
- **Android 网页触摸回归**：Android API 35 模拟器内 Chrome 124 通过触摸登录、资料保存/刷新、通知偏好保存/刷新、提醒创建/取消/刷新；本轮在生产构建上重跑，412×765 CSS px 下页面宽度与视口一致。该结果是模拟器证据，不是实体 Android 手机验收。
- **额外全路由仿真**：开发服务器版本的回归曾在 Firefox 页面导航期间超时；改用同一构建产物的静态服务并代理到隔离 API 后，`TEAMMIND_WEB_BASE=http://127.0.0.1:5220 TEAMMIND_API_BASE=http://127.0.0.1:5220/api/v1 python3 scripts/acceptance_cross_browser.py` 完整通过。Firefox 桌面与 WebKit 移动仿真均验证登录、注册、找回/重置，以及 11 个业务路由在 320、360、375、390、430、768、1024、1280px 下显示正确标题且无整页横向溢出；WebKit 触屏登录通过。此结果仍是浏览器引擎仿真，不是实体 iOS Safari。
- **真实服务依赖仍待提供**：当前测试 API 使用模拟 AI；没有真实模型地址/模型名/密钥、实体 Android/iOS/微信内置浏览器、SMTP/Firebase 真实投递凭据或证据。VAPID 已配置，但 Android 模拟器无法连接推送服务，也没有活动订阅。微信真实绑定、消息往返、绑定后 Gateway/worker 重启恢复仍未验证。TeamMind 仍按网页产品交付；Capacitor 是可选 WebView 外壳，不把独立原生 UI/FCM/APNs 当作网页完成条件。

## 2026-10-19 遗留目标续验

- 当前运行服务复核：`127.0.0.1:8194` 与 `127.0.0.1:8207` 的 `/readyz` 均为 `ready`；网页 `127.0.0.1:5217` 代理到隔离验收 API `8207`。该 API 使用迁移 18 的隔离 MySQL、`WECHAT_MODE=real` 和 `AI_PROVIDER=mock`。没有读取或记录密钥值。
- 微信集成现状纠正：当前实现不是 Go 直接实现 iLink。Go 调用 loopback 上受令牌保护的 OpenClaw Gateway；依赖锁定 OpenClaw `2026.10.4` 和腾讯 `@tencent-weixin/openclaw-weixin@2.4.10`。本轮 `scripts/acceptance_wechat_plugin.py` 通过真实插件生成二维码、状态轮询、取消、取消状态持久化以及扫码前不建立绑定；`backend/wechat-plugin-bridge` 的 `npm test` 4/4 通过。没有真人扫码，所以真实绑定、微信消息往返、绑定后 Gateway/worker 重启恢复仍未验证。
- 设置和审批回归在当前网页/API 上重跑：WebKit `scripts/acceptance_settings.py` 通过邮箱异设备确认、TOTP/MFA、会话管理、数据导出、通知/提醒和 320–1280px 设置相关页面；WebKit 双浏览器 `scripts/acceptance_browser_workflow.py` 通过项目/任务、成员角色、邀请、跨团队隔离、提案补充/重提/修改后批准、甘特筛选和手机抽屉。由此，旧记录中“WebKit 审批曾未完整通过”已不适用于当前验收证据。
- 智能体 Chromium UI `scripts/acceptance_agent_shell_ui.py` 通过：Shell 必须单独逐次授权且调用后复位；输出只有再次明确同意后才能保存到加密私人工作区；任务查询后提交的是待审批提案。该界面测试将 AI 接口固定为确定性响应，不证明真实模型接通。任意宿主机 Shell、宿主机文件访问和绕过审批的业务写入仍保持关闭，这是安全边界。
- 当前迁移 18 隔离库完整 `cd backend && go test ./... -count=1` 通过（`internal/live` 54.965 秒）；`npm run type:check`、微信桥接 `npm test`（4/4）通过。后端覆盖 MFA、邮箱变更、来源/IP 限流和告警、个人数据导出/注销/留存、微信授权关系、智能体提案和隔离执行等实现；这些本地测试不等于生产密钥运维、真实投递或外部测评。
- 实际外部验收阻塞：服务端未配置真实 AI 地址/模型/凭据；没有真人微信确认；`adb devices -l` 没有连接设备，本机也没有可调用的 Android Emulator 或 Xcode。因此实体 Android Chrome、iOS Safari、微信内置浏览器和真实 AI 服务仍未验。实体设备的输入不能以模拟器/浏览器视口仿真代替；TeamMind 的核心交付范围仍是网页，可选 Capacitor 壳不改变该范围。

## 2026-10-19 本轮复验补充

- 当前源码 `go test ./... -count=1` 在隔离迁移 19 MySQL `teammind_goal_wechat_20261019a` 通过（`internal/live` 43.836 秒）；`npm run type:check && npm run build` 通过；腾讯桥接 `npm test` 4/4 通过。
- Firefox/WebKit 全路由验收在当前源码 API `8230` + Vite `5230` 通过：登录、注册、找回/重置和 11 个业务路由覆盖 320–1280px。WebKit 设置专项通过个人数据导出、资料/团队设置、跨浏览器邮箱验证、MFA/TOTP、恢复码、会话续期/撤销、通知与提醒。WebKit 双浏览器业务流程也在迁移 19 当前源码 API 上通过，包含邀请/角色、任务提案补充/重提/修改后审批、甘特和跨团队隔离。
- 验收中发现 `8202` 服务实际连接的测试库仍停留迁移 18，故新项目提案 API 返回 404；这导致审批页的并行加载失败，并非审批按钮或 API 19 实现故障。对专用隔离库应用 `000019_agent_project_proposals` 并使用当前服务后，完整双浏览器流程通过。旧测试服务没有被用于最终审批结论。
- 微信网页验收使用固定 OpenClaw `2026.10.4`、腾讯插件 `2.4.10` 的 Gateway。`acceptance_wechat_plugin.py` 与 390px WebKit `acceptance_wechat_ui.py` 均通过真实二维码读取/显示、等待、取消和未绑定断言；该证据仍没有真人扫码。API 当前 AI 模式为 `mock`，因此不能验证真实 AI 工具选择或微信 AI 回复。
- TeamMind 的正式验收目标仍是响应式网页。Capacitor 与 FCM/APNs 是可选扩展，不将缺少独立原生界面视为网页功能缺陷；SMTP 尚未配置；Web Push 有 VAPID 密钥但目前没有活动订阅、缺送达证据；实体 Android Chrome、iOS Safari、微信内置浏览器仍待授权设备和部署环境。


## 2026-10-19 本轮逐项复验

- **微信插件实现核对**：当前实现不是 Go 自行实现 iLink。依赖锁定 OpenClaw `2026.10.4` 和腾讯 `@tencent-weixin/openclaw-weixin@2.4.10`；Gateway 进程实际运行，Go 仅调用受令牌保护的 loopback 桥接。本轮 `npm test` 4/4 通过，TeamMind real-mode API 的真实插件二维码、等待状态、取消和“扫码前不创建绑定”验收通过。真人扫码确认、绑定、真实消息收发和绑定后完整进程重启恢复仍未验证。
- **项目变更提案**：迁移 19 已进入当前代码；WebKit 双浏览器流程完整通过成员提交、管理员要求补充、成员重提、管理员修改后批准，并在项目页确认正式数据更新。为适应真实通知行为，修正验收脚本此前“投递记录必须为空”的过时断言，并增加筛选完成后的 DOM 同步等待。
- **WebKit 双浏览器业务回归**：`TEAMMIND_BROWSER=webkit TEAMMIND_WEB_BASE=http://127.0.0.1:5222 TEAMMIND_API_BASE=http://127.0.0.1:8221/api/v1 python3 scripts/acceptance_browser_workflow.py` 通过，覆盖邀请、角色管理、团队隔离、项目/任务/审批、甘特、通知、智能体只读查询及手机抽屉。
- **WebKit 设置专项**：`TEAMMIND_BROWSER=webkit TEAMMIND_WEB_BASE=http://127.0.0.1:5222 TEAMMIND_API_BASE=http://127.0.0.1:8221/api/v1 python3 scripts/acceptance_settings.py` 通过，覆盖个人数据导出、资料和团队设置、跨浏览器邮箱确认、TOTP/MFA、恢复码、会话续期/撤销、通知偏好、提醒及 320–1280px 页面。
- **隔离 MySQL 安全和生命周期测试**：迁移 19 测试库上的全量 `go test ./... -count=1` 通过（`internal/live` 49.658 秒）；MFA、邮箱变更、IP 限流/超管告警、个人数据导出、账号注销、留存清理、任务/项目智能体提案、团队知识候选及受限 Shell/加密工作区专项测试全部通过。`npm run type:check`、`npm run build`、验收脚本 `py_compile` 及微信桥接 `npm test` 通过。
- **智能体回归**：Chromium 390px 助手 UI 验证 Shell 单次授权、调用后复位、另行授权保存输出，以及查询任务/待审批提案顺序；工作区 API 进程完全停止并重新启动后，原会话可读回相同 AES-GCM 文件。两项均未使用真实模型，不能证明真实模型自主选择工具。
- **仍需外部条件**：当前工作区没有真实模型服务凭据、SMTP/Firebase 投递凭据；Web Push 的 VAPID 联系地址为占位值，API 已正确报告未配置；订阅失败；没有实体 Android/iOS/微信浏览器或可用真人微信账号。WebKit 与 Android 模拟器不能替代这些验收。TeamMind 以网页为核心；Capacitor/FCM 属可选扩展，手机网页推送应验收浏览器 Web Push。

## 2026-10-19 真人微信和 Android Chrome 复验（数据库数量为后续清理前快照）

- 用户已在局域网测试页完成真人扫码和手机确认。对主 API 所用隔离 MySQL 的脱敏聚合查询确认：4 个腾讯微信活动账户、4 条有效绑定、4 条入站消息记录、4 个渠道会话映射；两个 API 进程均读到相同记录，最近 5 分钟内 worker 同步状态仍在更新。API/Gateway/worker 独立重启后的绑定恢复已在本轮先前复核。
- 完整微信回复闭环仍未通过：4 条外发投递为失败，AI 模式明确为 `mock`，worker 按设计拒绝发送假回复。要验真实回复与外发，仍需用户提供可用模型服务地址、模型名及服务端凭据；目前不能把真实入站描述成双向消息闭环。
- Android API 35 模拟器 Chrome 124 对生产构建网页完成触摸登录，资料和通知偏好保存并刷新后仍存在，以及提醒创建/取消和刷新检查；412×765 CSS px 下设置、通知页面无横向溢出。首次脚本使用了不适用于网页 history 路由的哈希地址，设置页断言失败；改用网页实际路由后整段流程通过。该证据不等同于实体手机验收。
- `npm run build` 通过。Android Chrome 的 PushManager 订阅仍报 `AbortError: Registration failed - push service error`，没有写入订阅；SMTP 未配置。真实 Web Push/邮件送达、真实模型对话/工具选择/记忆提取、实体 Android/iOS/微信内置浏览器，以及正式 TLS/KMS/备份恢复/监控/第三方渗透与等保三级测评仍待外部环境和证据。Capacitor 原生壳与 FCM/APNs 属网页产品之外的可选扩展。

## 2026-10-19 本轮七项目标复验结果

- 空隔离 MySQL `teammind_resume_20261019` 从零执行迁移 `000001`–`000019` 成功；其全量 Go 套件通过（`internal/live` 44.311 秒）。`npm run type:check && npm run build` 通过。Node.js `24.16.0` 下腾讯桥接 `npm test` 4/4 通过。
- 迁移 19 隔离库上的 MFA/TOTP、邮箱变更、来源限流与告警、个人数据导出/注销/留存共 8 项集成测试通过；Agent 定向 8 项测试通过，覆盖待审批任务/项目变更、团队知识逐次授权、Python/Bash 断网执行以及加密私人工作区 CRUD/账号隔离。
- WebKit 设置专项及双浏览器审批工作流通过。Android API 35 模拟器 Chrome 触摸登录、资料、通知偏好和提醒回归通过，412×765 CSS 像素没有横向溢出。该结果不等同于实体手机验收。
- Firefox 桌面与 WebKit 手机仿真在 LAN 页面 `http://192.168.31.110:5190` 完成认证和 11 个业务页面的 320–1280px 全路由检查。第一次序列的 Firefox 1024px 成员页超时；单页重放与完整复跑通过，记录为一次测试瞬时超时，不作为产品失败或最终通过的唯一证据。
- （当时记录，已被 2026-10-19 后续真人扫码复核取代）真实 OpenClaw Gateway + 腾讯微信插件运行于隔离环境；二维码、等待状态、取消、390px 页面渲染与未绑定断言通过。该时点真人扫码、真实绑定、消息往返及绑定后完整进程重启恢复尚未验收。供真人验收的临时入口为 `http://192.168.31.110:5190`；连接迁移 19 隔离库，AI 明确使用模拟模式。
- （当时记录，状态已由后续真人扫码复核取代）再从该 LAN 来源运行 WebKit 390px 微信页验收：Vite 对真实插件 QR 的代理、二维码图片显示、等待/取消、无横向溢出、无运行时错误均通过；取消后仍无绑定。自动化浏览器运行在主机，该记录时真人扫码仍待完成。
- Android Chrome 模拟器获通知权限且 Service Worker/PushManager 可用，但实际创建 Web Push 订阅时浏览器返回 `AbortError: Registration failed - push service error`，未写入订阅；因此真实浏览器推送服务送达仍未验证。真实 SMTP 未配置。
- 七项清单的准确状态：微信插件接入、MFA/邮箱/限流、数据导出/注销/留存、Agent 安全白名单/审批/沙箱/加密私人工作区和 WebKit 设置/审批流程已有实现与本地验证；仍缺真人微信闭环、真实 AI 凭据下的对话/工具选择/记忆提取、移动浏览器真实推送、实体 Android/iOS/微信浏览器验收及生产安全运维证据。Capacitor 原生 App 和 FCM/APNs 是网页产品的可选扩展。

真人扫码与真实外部模型/推送/邮件验证需要相应账号、服务凭据或实体设备。当前测试入口是临时 HTTP 开发服务，只允许隔离验收使用，不能视为生产环境；本地自动化也不能证明已通过等保三级。

## 最新状态更正（2026-10-19）

下列为早期真人扫码后的历史快照，当前数据库数字已在文档末尾的“七项遗留能力当前复核”更新：当时隔离库有 4 条有效绑定、4 条入站记录及 4 个会话映射。后续发现 Provider→桥接集成测试因 `t.Cleanup` 晚于 `defer DB.Close()` 而残留了测试夹具，已修复清理顺序并删除专用 `Worker Test` 数据。当前净化后数字以最新复核为准。

本轮 Android API 35 模拟器 Chrome 124 通过生产构建网页的触摸登录、资料和通知偏好持久化、提醒创建/取消及刷新回读；412×765 CSS px 页面没有横向溢出。它不是实体手机验收。`npm run build` 通过；Android Chrome Web Push 创建订阅仍报 `AbortError: Registration failed - push service error`，未产生投递证据；SMTP 和真实模型凭据未配置。

当前未完成项：真实模型对话、记忆提取与模型自主工具选择；SMTP 邮件送达；Web Push 订阅与真实送达；微信文本回复外发；微信图片/语音/文件媒体；邀请链接邮件/短信自动触达；离线查看/编辑；实体 Android/iOS/微信内置浏览器回归。邀请/所有权转移不由智能体代办、任意宿主机命令和宿主机文件访问保持关闭。正式 TLS/KMS、密钥轮换、加密备份与恢复演练、生产监控告警、不可变审计归档、第三方安全评估、定级备案和等保三级测评仍待部署，不能宣称通过。

## 2026-10-19 真人扫码后的早期复核快照（已更新）

- 真人微信扫码/确认已完成；当时主隔离数据库聚合查询为 4 条有效绑定、4 条入站消息及 4 个隔离会话映射。该数字为当时快照，后续发现其中 7 个账号/会话为测试夹具并已清理。当前状态见文档末尾的“七项遗留能力当前复核”。
- Android API 35 模拟器 Chrome 124 已通过实际触摸登录、资料/通知偏好持久化、提醒创建/取消和刷新；412×765 CSS px 无页面横向溢出。该结果不代表实体 Android、iOS Safari 或微信内置浏览器。
- 生产前端 `npm run build` 通过。SMTP 未配置；Android Chrome Web Push 注册返回 `AbortError: Registration failed - push service error`，没有成功创建订阅，所以真实邮件和浏览器推送投递仍未验证。真实模型服务地址、模型名及凭据也未配置，真实模型对话、记忆提取、工具选择尚未验收。
- 尚未完成功能/验收还包括：微信图片/语音/文件等媒体（当前只支持文本）、邀请链接邮件/短信自动触达、离线查看/编辑、实体移动浏览器回归。AI 智能体的邀请/团队所有权转移和任意宿主机命令/文件访问不开放，属于现有安全边界。正式 TLS/KMS、备份恢复、监控与不可变审计、第三方安全评估和等保三级测评仍需正式部署完成，不能表述为已通过。
- 本地环境没有 SMTP 投递凭据、可用真实模型凭据或实体手机。若继续真实外部验收，准确需要提供的是模型兼容 API 地址/模型名/密钥、SMTP 服务器/账号/应用密码、可用移动设备与腾讯网络；Web Push 还需允许 Android Chrome 访问浏览器推送服务。原生 App、FCM/APNs 不属于网页交付门槛。
- 为复核 WebKit 审批和设置流程，本轮在空迁移 21 隔离库重新运行双浏览器 `acceptance_browser_workflow.py` 与完整 `acceptance_settings.py`，均通过。工作流首次复跑揭示会话列表更新异步时序使测试过早断言；修正为等待服务端列表读回达到预期行数后，完整工作流通过。两次独立登录及 API 会话列表也确认平台正确保存两条会话。
- 同一隔离库 `go test ./... -count=1` 通过（`internal/live` 59.625 秒），包括 MFA、邮箱变更、来源限流、数据导出/账号注销/留存、微信隔离和 Agent 提案/沙箱/工作区测试。`npm run type:check`、Python 浏览器脚本语法检查通过，腾讯桥接 `npm test` 4/4 通过。
- Agent Chromium 界面回归 `scripts/acceptance_agent_shell_ui.py` 通过：单次 Shell 授权、授权复位、另行授权后将输出保存进 MySQL 私人工作区、读取任务并展示待审批提案。模型接口由确定性测试路由提供，不能证明真实模型工具选择。
- `scripts/acceptance_agent_workspace_restart.py` 在独立测试限流密钥及隔离迁移 21 数据库通过：私人工作区文件 AES-GCM 加密持久化，API 完整退出并启动后可沿用会话读取。第一次使用共享验收库时注册触发来源限流，重试迁移到隔离库后成功，限流未被放宽。
- 曾评估在主机部署真实本地模型：Ollama 官方下载重定向资产返回 404；预编译 llama-cpp 包及 Qwen GGUF 可发现，但 GitHub 代理实测约 28 KB/s，13.5 MB 运行包下载数分钟仍未完成，约 0.94 GB 权重无法在本轮可靠下载。该替代方案未启动，真实模型对话/工具调用依然待实际服务凭据或可用模型分发网络。

## 2026-10-19 后续验收补充

- 隔离迁移 21 MySQL 上，全量 Go 测试通过（internal/live 63.555 秒）；前端类型检查与生产构建通过；腾讯微信桥接测试 4/4 通过。
- 微信生命周期集成用例重复 5 次及 race detector 检查均通过，验证身份争抢、并发绑定单一胜者、所有者解绑审计、另一账号重绑及旧账号无权操作新绑定。
- 新建空库后，WebKit 双浏览器项目/任务/邀请/审批/甘特工作流通过；完整 WebKit 设置回归通过；Android API 35 模拟器 Chrome 124 触摸登录、资料/通知偏好持久化及提醒创建/取消通过。相关视口范围和具体边界见验收记录。
- 仍未验证真实模型工具选择/微信 AI 回复、SMTP 邮件送达和浏览器 Web Push 真实送达。设备回归目前覆盖浏览器仿真及 Android 模拟器，没有实体 Android/iOS/微信内置浏览器证据。正式生产安全运维和第三方等保测评未完成。
- Firefox 桌面与 WebKit 手机仿真逐页通过认证路由和 11 个 TeamMind 路由，覆盖 8 种视口，无横向溢出或页面运行时错误；该结果不替代实体设备验收。

## 2026-10-19 最新代码回归补充

- 新增 `TestWechatWorkerRejectsMockAndDeliversConfiguredProviderReply`：隔离 MySQL 验证模拟 Provider 不会向微信桥接发送假回复；切换到本地 OpenAI 兼容测试服务后，验证模型请求、助手回复及 Provider 元数据落库，再验证经过令牌认证的渠道桥接接受文本并将投递状态保存为 `sent`。定向测试通过。
- 迁移 21 隔离 MySQL 上 `go test ./... -count=1` 全量通过；`npm run type:check && npm run build` 通过。
- 该项是 Provider、持久化和桥接协议的集成测试，不是生产模型或真实微信外发验收。用户已完成真人扫码和手机确认；真实入站状态已在前文记录。当前服务使用 `AI_PROVIDER=mock`，因此真实双向微信回复仍未通过。真实模型端点/模型名/服务端密钥、SMTP 邮件送达、Web Push 订阅和送达、实体设备及生产安全运维证据仍为外部待验项。

## 2026-10-19 七项遗留能力当前复核

- 腾讯插件链路在当前环境实际运行：固定 OpenClaw `2026.10.4` / `@tencent-weixin/openclaw-weixin@2.4.10`；本次 `acceptance_wechat_plugin.py` 的真实 QR、等待状态、取消和扫码前不写绑定检查通过。用户此前完成人工扫码/确认；修复测试夹具清理顺序后，当前隔离库只读聚合为 1 个有效绑定、1 条入站消息、1 个隔离会话，Worker 同步持续更新。API/Gateway/worker 重启读回此前已有验证。
- 早前 8 个绑定/会话、3 条本地测试模型 `sent` 回复及 5 条失败中包含 Go 集成测试遗留夹具。本次从隔离库移除 7 个 `Worker Test` 测试账号、绑定和消息；净化后的投递记录为 1 条失败、0 条已发送。当前 API/worker 均使用 `AI_PROVIDER=mock`，模型地址、模型名和密钥均未配置。
- `acceptance_settings.py` 在 WebKit 通过完整设置流程；`acceptance_browser_workflow.py` 在 WebKit 通过双浏览器审批、重提、甘特等业务流程；`acceptance_android_chrome.py` 在 API 35 模拟器 Chrome 通过触摸登录、资料/通知偏好刷新持久化及提醒创建/取消（412×765 CSS px，无横向溢出）。实体 Android、iOS Safari、微信内置浏览器尚未验。
- 当前 API 的 MFA/TOTP、邮箱变更、来源限流、个人数据导出/注销/留存专项 Go 测试通过（16 项，40.104 秒）；Agent 项目提案、逐次授权知识候选、断网 Shell 沙箱、加密工作区及按次同意测试通过（5 项，0.510 秒）。无审批业务写入和宿主机 Shell/文件访问按既定安全边界关闭。
- TeamMind 是响应式网页产品；原生 App/FCM/APNs 是可选扩展。`.env` 与运行中服务未配置真实模型或 SMTP；VAPID 与订阅加密密钥格式有效，但 `VAPID_SUBJECT=mailto:admin@example.invalid` 仍为占位值，服务端现将其判定为未配置；活动 Web Push 订阅为 0，Android Chrome 模拟器连接推送服务超时。真实模型回复、邮件/Web Push 投递仍未通过。生产 TLS/KMS、备份恢复、告警、外部不可变审计归档、独立安全评估和等保三级测评仍未完成。
- 发现并修复 `TestWechatWorkerRejectsMockAndDeliversConfiguredProviderReply` 的夹具清理时序：原先数据库先关闭、`t.Cleanup` 后执行，SQL 错误被忽略，测试账号未删。现在以先清理、后关闭连接的顺序运行，并对清理错误显式失败。隔离库中仅按专用 `wechat-mock-…@example.invalid` 与 `Worker Test` 标记删除了 7 个测试夹具；其他用户与团队未触碰。
- 修复后当前隔离 MySQL 上再次运行 `go test ./... -count=1`，全量通过（`internal/live` 56.887 秒）；复查测试标记账号与本地测试模型回复均为 0，真人渠道仍只有 1 个有效绑定/入站会话。

## 2026-10-19 用户确认扫码后的当前复核

- 用户确认已完成微信扫码和手机确认。对运行中的 API `127.0.0.1:8240` 所连接的隔离 MySQL 做只读聚合核验：1 个活动渠道账户、1 条有效绑定、1 条入站记录、1 个渠道会话映射；该会话有 1 条用户消息、没有助手回复。Worker 同步游标持续更新。查询未读取消息正文、微信身份或渠道凭据。
- 当前 API 和 Worker 使用 `AI_PROVIDER=mock`，投递记录为 1 条失败、0 条已发送；失败是 Worker 按设计拒绝模拟回复。真实 AI 对话和微信回复外发尚未通过；服务环境未配置 `AI_BASE_URL`、`AI_MODEL` 或 `AI_API_KEY`。
- 当前 Android API 35 模拟器 Chrome 再次通过触摸登录、资料保存和刷新、通知偏好保存和刷新、提醒创建与取消。执行 `TEAMMIND_WEB_BASE=http://127.0.0.1:5191 TEAMMIND_API_BASE=http://127.0.0.1:8240/api/v1 ANDROID_CHROME_CDP=http://127.0.0.1:9222 python3 scripts/acceptance_android_chrome.py`；视口 412×765 CSS px、DPR 2.625，设置与通知页面没有整页横向溢出。首次运行暴露验收脚本在偏好异步加载前过早断言，已修为等待服务端读回后重测通过。
- 该结果不等于实体 Android、iOS Safari 或微信内置浏览器验收。真实模型、邮件/Web Push 投递和生产安全运维仍需要实际凭据与部署环境。

## 2026-10-19 七项目标复验结果

| 项目 | 当前结论 | 本轮证据与剩余事项 |
| --- | --- | --- |
| 微信腾讯插件闭环 | **扫码、绑定、入站和三进程重启恢复通过；AI 回复外发未通过** | 固定 OpenClaw `2026.10.4` / 腾讯 `@tencent-weixin/openclaw-weixin@2.4.10`；用户已确认真人扫码。当前隔离 MySQL 有 1 个活动绑定、1 条入站消息和 1 个渠道会话；该会话有 1 条用户消息、0 条助手消息。依次重启 API、微信 Worker 和 Gateway 后，绑定仍在、Worker 解密凭据并继续同步。最新游标为 `2026-10-19 12:43:08 UTC`。当前 AI 为模拟模式，未产生真实外发。 |
| 真实 AI | **未验证** | API/Worker 使用 `AI_PROVIDER=mock`；`AI_BASE_URL`、`AI_MODEL`、`AI_API_KEY` 均未设置，本机也没有可用 Ollama/llama.cpp 服务或已下载模型。真实对话、记忆提取及商业模型自主选工具都未验。 |
| Agent 写入与执行 | **受控能力已有实现和测试；不开放无界宿主机操作** | 新建迁移 21 隔离库执行 `cd backend && go test ./... -count=1` 全部通过（`internal/live` 59.270 秒），覆盖只读查询、经审批的任务/项目/成员提案、逐次授权团队知识候选、断网 Python/Bash 沙箱和加密私人工作区。真实模型选择工具仍待凭据；任意宿主机命令、文件和网络访问不是产品开放能力。 |
| 账号安全 | **功能与隔离数据库/浏览器回归通过** | 同一 Go 全量测试包含 MFA/TOTP、邮箱变更、登录来源限流与平台告警；WebKit 设置流程通过 MFA 启用/登录/恢复码/关闭和跨浏览器邮箱确认。生产密钥托管、轮换和外部告警联动待部署。 |
| 个人数据生命周期 | **功能与隔离数据库/浏览器回归通过** | Go 全量测试覆盖个人数据导出、注销删除和留存清理；WebKit 完成数据副本下载。法定留存期限、生产备份恢复和外部不可变审计归档仍需部署/组织证据。 |
| 原生移动应用与网页推送 | **原生应用属可选范围；网页推送未完成真实送达验收** | 用户此前确认 TeamMind 是网页产品；iOS/Android 发布及 APNs/FCM 不属于网页门槛。VAPID 与订阅加密密钥格式有效，但 `VAPID_SUBJECT=mailto:admin@example.invalid` 是占位值，服务端现将其判定为未配置；活动订阅为 0，Android Chrome 模拟器订阅返回 `AbortError`，到 Google 推送服务的连接超时。 |
| 设置/移动浏览器 | **设置流程和 WebKit 审批工作流通过；实体设备仍未验** | WebKit 设置流程及双浏览器业务审批流程在新隔离迁移 21 数据库通过。Android API 35 模拟器 Chrome 通过触摸登录、资料/通知偏好保存和刷新、提醒创建/取消，412×765 CSS px 无横向溢出。实体 Android Chrome、iOS Safari 和微信内置浏览器没有设备证据。 |

本轮另执行：腾讯插件桥接 `npm test` 4/4 通过；`npm run type:check && npm run build` 通过；API、Worker、Gateway 重启后的持久绑定与同步检查通过；真实插件扫码起始流程在独立隔离库通过，没有打印二维码或创建测试绑定。HTTP 开发环境仅用于验收，不能作为生产部署或等保三级通过证明。


## 2026-10-19 VAPID 状态校验修复

此前的配置判断只匹配 `mailto:`/`https://` 前缀，误把 `example.invalid` 占位联系人报告为可用。现在服务端验证联系信息格式并拒绝示例/保留域、本机域名和 IP 地址；失效状态不会向前端公开 VAPID 公钥。环境模板中的 `VAPID_SUBJECT` 改为空值，部署时须填写实际维护联系地址。代码不会探测邮箱是否可收件或 HTTPS 联系页归属。

验证结果：VAPID 配置和通知渠道状态测试通过；真实迁移数据库的状态路由测试通过；真实 API 返回 `pushConfigured=false`、空公钥、0 个订阅；390px 手机浏览器显示未配置提示，启用和撤销按钮不可用。一次性测试账号已注销清理。`go test ./... -count=1`、`npm run type:check && npm run build` 均通过。真实推送仍待可用的联系人配置、HTTPS 页面、浏览器订阅成功及外网推送服务送达。


## 2026-10-19 七项能力最新复核

| 功能 | 当前状态 | 验收证据 | 剩余外部条件 |
| --- | --- | --- | --- |
| 微信腾讯插件闭环 | **真人扫码、绑定、入站和服务重启恢复通过；出站 AI 回复未通过** | 用户已确认手机扫码。真实库聚合仍为 1 个活动绑定、1 条入站、1 个会话、1 条用户消息/0 条助手消息。重启 Gateway、worker、API 后绑定仍在且 worker 游标更新。重启后再次从 TeamMind API 完成非模拟二维码、等待、取消验收。桥接测试 4/4。 | `AI_PROVIDER=mock`；需要真实模型配置来验证回复外发。 |
| 真实 AI | **未验证** | 运行进程明确为 mock，没有配置 AI 地址、模型或密钥；没有本地可用模型。 | 模型供应商凭据；真实对话、记忆和工具选择验收。 |
| 智能体 | **有界工具、待审批写入、断网沙箱和私人工作区已实现并验收** | 新迁移 21 数据库全量 Go 集成测试 48.699 秒通过；浏览器验私人文件创建/编辑、刷新持久化、导出/删除。WebKit 工作流通过任务/项目审批与团队成员业务流程。 | 真实模型自主选工具需模型凭据；生产沙箱逃逸评估。无界宿主机 Shell 和绕过审批写正式业务数据不属于开放范围。 |
| 账号安全 | **应用功能测试通过** | 全量 Go 测试覆盖 MFA/TOTP、恢复码、邮箱验证、IP/API 限流与告警；WebKit 完整设置回归通过。 | 生产密钥设施、SMTP 邮件、反向代理及外部安全告警部署验证。 |
| 数据生命周期 | **导出、注销、留存测试通过** | Go 集成测试、WebKit 数据导出和 Chromium 手机布局账号注销均通过；注销后会话无效、旧密码不能登录。 | 共享业务数据法律留存期限、生产备份恢复、外部不可变审计归档。 |
| 原生 App / 推送 | **原生 App 不属于网页交付范围；Web Push 未完成真实送达** | 用户明确产品为网页；浏览器推送状态已修为拒绝占位 VAPID 联系信息并在页面显示未配置。 | SMTP 凭据、实际 VAPID 联系方式、HTTPS、成功浏览器订阅及推送服务网络连通性。Capacitor/APNs/FCM 是可选扩展。 |
| 设置与设备 | **Firefox/WebKit 多视口及 Android API 35 模拟器设置流程通过** | WebKit 设置专项、双浏览器审批流程及 11 个路由八视口通过；Android Chrome 触摸登录、设置保存/刷新和提醒流程通过，412×765 无溢出。 | 实体 Android Chrome、iOS Safari 和微信内置浏览器尚未验收。 |

全量 Go 集成测试运行于空库迁移 `000001`–`000021` 的专用数据库；静态生产前端运行在隔离 API 上。真实模型、邮件/推送送达、实体设备与生产运维条件未满足，因此本报告不将其标为通过。

## 2026-10-19 最新复核

- 用户本轮确认已完成微信扫码和手机确认。只读数据库聚合仍为 1 个活动腾讯插件账号、1 条有效绑定、1 个映射会话、1 条入站用户消息、0 条助手消息、1 条失败投递、0 条成功投递；同步游标为 `2026-10-19T15:08:44.872Z`。绑定/入站记录早于本轮核验，不据此宣称本轮操作新增了绑定。失败是 `AI_PROVIDER=mock` 时按设计拒绝模拟回复外发。
- 当前实现确实通过固定 OpenClaw `2026.10.4` / 腾讯 `@tencent-weixin/openclaw-weixin@2.4.10` 插件处理扫码与消息，Go Worker 只调用受令牌保护的 loopback Gateway。插件桥接测试 4/4 通过。
- 在空库迁移 `000001`–`000021` 后运行 `go test ./... -count=1`：通过，`internal/live` 46.055 秒。`npm run type:check && npm run build`：通过。新隔离库已删除。
- 在另一座空迁移 21 库重跑 WebKit 审批工作流和设置专项，均通过，覆盖双浏览器团队/审批/邀请/Gantt 流程及邮箱、MFA、会话、通知、导出与八种视口。专用测试账号和团队随隔离库删除；临时 API/Vite 已停止。
- 本机无真实模型/SMTP/VAPID 配置；无 Android 设备，当前为 Linux。当前未运行 API、Worker、Gateway 或 Vite。微信 AI 回复、邮件/Web Push 实际送达、实体 Android/iOS/微信内置浏览器验收和生产运维/等保三级证据仍未完成。原生移动 App 不属于已确认的网页交付范围。

## 2026-10-19 真人扫码确认后的最终状态

- 用户确认真实微信扫码并在手机上确认后，主验收 API 的 `/healthz` 与 `/readyz` 均返回 200。主隔离库只读聚合为：1 个活动渠道账户、1 条有效绑定、1 条入站记录、1 个会话映射、1 条用户消息、0 条助手消息；投递为 1 条失败、0 条成功，Worker 同步游标时间为 `2026-10-19 13:39:40 UTC`。查询不包含消息正文、微信标识或凭据。
- 当前 `AI_PROVIDER=mock` 且没有配置模型地址、名称或密钥。该条失败表示系统按设计拒绝向真实微信发送模拟回复；因此真人扫码、绑定和入站通过，真实模型回复及微信外发尚未通过。
- 本轮已停止临时 API、静态验收代理和 Android 模拟器，并删除两座仅用于本轮测试的隔离库 `teammind_audit_1019b`、`teammind_agent_audit_1019b`。主隔离库和 API、微信 Worker、OpenClaw Gateway、MySQL、网页开发服务器均保留；清理后主 API 健康及就绪检查仍为 200。
- 交付仍有外部验收项：模型服务凭据（真实 AI 与微信回复）、SMTP 配置（邮件通知）、有效 VAPID 联系方式/HTTPS/推送网络（Web Push）、实体 Android/iOS/微信内置浏览器回归、生产密钥托管与轮换、备份恢复、外部告警/不可变审计归档，以及正式等保三级测评。代码和本地自动化测试不代表这些外部条件已满足。

## 2026-10-19 分项复验与顺序重启结果

- 当前腾讯插件桥接二维码验收通过：真实插件返回二维码、等待状态、取消及状态持久读取通过；未打印二维码或创建绑定。桥接 Node 测试 4/4 通过。此前用户确认真人扫码及手机确认。
- 本轮顺序重启腾讯插件 Gateway、微信 Worker、TeamMind API 后，主 API `/healthz` 和 `/readyz` 均返回 200，Worker 同步游标更新到 `2026-10-19 14:04:59 UTC`。主库只读计数保持 1 个有效绑定、1 条入站消息、1 个映射会话、1 条用户消息、0 条助手消息；发送记录 1 条失败/0 条成功。当前 AI 为 mock，真实 AI 回复与微信出站仍未通过。
- WebKit 双浏览器完整业务流程重跑通过，覆盖审批与重提、项目/任务、团队隔离、成员、邀请、通知、Gantt、手机抽屉和撤销会话。测试所创建的两名账号、两个团队及相关记录已按本次唯一标记清理，并验证不存在残留。
- WebKit 设置回归在独立迁移 21 数据库通过，覆盖 MFA/TOTP、跨浏览器邮箱确认、资料/团队设置、导出、会话、通知、提醒和八种视口。`go test ./... -count=1`（隔离迁移 21 库）、前端类型检查/生产构建及腾讯桥接测试均通过。该次隔离 API/Vite 和测试库已清理。
- 外部阻塞仍是模型供应商凭据、SMTP、可联系的 VAPID 配置与推送网络、实体设备、生产 KMS/备份/告警/审计证据和正式等保三级测评。此报告不把 mock、接口桩或浏览器仿真写成真实外部服务通过。
