# TeamMind 功能审计与验收记录

本记录汇总截至 2026-10-19 在本地执行的验收。`通过` 表示对应测试已运行；`部分通过` 表示仍有未验证或缺失的步骤。本文件不是认证报告。下列接口路径均相对于 `/api/v1`。所列数据表是 MySQL 中的权威业务数据来源，不是浏览器存储。

| 页面 → 可见控件/操作 | 原问题 → 目标行为 | 接口 | 数据表 | 角色 | 验收结果 |
| --- | --- | --- | --- | --- | --- |
| 登录 → 邮箱/密码、登录、记住登录、键盘/触摸 | 历史 CSS 将十六进制文本颜色赋给 Arco `--primary-6`，而 Arco 按 `rgb(var(--primary-6))` 解析；旧登录还使用浏览器演示会话。当前审计开始时登录已可用；已移除旧样式并固定按钮状态 | `POST /auth/login`、`GET /me`、`POST /auth/logout` | `users`、`auth_sessions`、`login_failures` | 访客 | **通过**：Chromium/Firefox/WebKit 自动化断言覆盖未悬停、悬停、焦点、加载、失败、成功、中心/边缘命中、对比度 ≥4.5、Enter、Space、触摸、刷新、退出及令牌撤销；本轮用真实坐标点击和 WebKit 真实 API 会话再次复验。当前 Chromium headless shell 不派发 `requestAnimationFrame`，故其 Playwright 稳定定位器和截图等待受限；Firefox/WebKit 定位器点击通过，Chromium 鼠标坐标点击通过；Safari 实体设备待验 |
| 注册 → 创建账号 | 演示身份改为 bcrypt 账号，并严格校验邮箱语法 | `POST /auth/register` | `users` | 访客 | **通过**：接口及浏览器邀请流程通过；拒绝带空白或显示名格式的邮箱地址 |
| 登录 → 来源限流与超管告警 | 账号失败保护之外，对登录、注册、找回和重置按来源计数；只在 HMAC 下保存来源键，代理转发地址仅在可信 CIDR 命中时采用 | `POST /auth/login`、`/auth/register`、`/auth/forgot`、`/auth/reset`、`GET /platform/security-alerts` | `login_failures`、`auth_ip_rate_limits`、`platform_roles` | 访客触发限流；平台超管读取告警 | **通过（隔离 MySQL + API）**：同来源第 31 次登录返回 429；伪造代理头不改变来源键；可信代理链解析测试通过；平台告警仅返回脱敏标识。生产密钥和真实反向代理配置仍需部署 |
| 忘记/重置密码 → 申请重置并设置新密码 | 原先没有真实重置；改为短期、单次使用的哈希令牌；生产环境通过邮件发送 | `POST /auth/forgot`、`POST /auth/reset` | `password_resets`、`users`、`auth_sessions` | 访客 | **通过**：开发环境接口单次令牌测试通过；生产 SMTP 投递未验证 |
| 顶栏/设置 → 身份、退出、资料、密码、续期/撤销会话 | 浏览器演示身份改为服务端身份和可撤销会话 | `GET/PATCH /me`、`POST /auth/logout`、`/auth/password`、`/auth/refresh`、`GET/DELETE /auth/sessions` | `users`、`auth_sessions` | 已登录用户 | **通过**：Chromium 验证资料持久化、续期、撤销另一浏览器会话、修改密码后重新登录及退出；接口确认所有旧会话均已撤销 |
| 设置 → 更改邮箱、异设备确认 | 当前密码验证后发送 30 分钟单次链接；链接令牌哈希存储，确认不依赖原浏览器会话并撤销全部会话 | `POST /me/email-change`、`POST /me/email-change/confirm` | `users`、`email_change_requests`、`auth_sessions`、`password_resets`、`audit_logs` | 本人发起；持有新邮箱验证链接者确认 | **通过（隔离 MySQL + Chromium）**：错误密码、已占用邮箱、请求后邮箱冲突、过期、重放和并发确认；在无登录的第二浏览器完成确认，旧会话 401，新邮箱重新登录成功，审计只含脱敏地址。生产 SMTP 投递仍需凭据验证 |
| 设置/登录 → 多因素认证 | TOTP 设置、验证码确认、一次性恢复码、动态码登录和关闭；MFA 密钥加密存储，登录挑战单次使用并限时，已接受的 TOTP 时间步不可重放，接受过的 TOTP 时间步不能重放 | `GET /me/mfa`、`POST/DELETE /me/mfa/setup`、`POST /me/mfa/enable`、`POST /me/mfa/disable`、`POST /auth/mfa/complete` | `user_mfa`、`mfa_recovery_codes`、`auth_mfa_challenges`、`auth_sessions`、`audit_logs` | 本人；登录挑战持有人完成第二因素 | **通过（迁移 15 隔离 MySQL + Chromium）**：RFC 6238 向量、密钥加密/用户隔离、当前密码再验证、错误码拒绝、10 个恢复码哈希存储、普通密码登录不创建会话、错误/正确 MFA 登录、恢复码一次性、相同 TOTP 并发仅一个挑战成功、重放拒绝、关闭后会话撤销均通过；设置和通知页面在 320–1280px 回归。生产密钥运维配置仍需完成 |
| 设置 → 个人资料/团队表单及时区 | 将只有点击事件的非语义表单改为可键盘提交表单；同时执行浏览器 IANA 时区校验和服务端权威校验，并分别处理加载/错误状态 | `PATCH /me`、`POST /teams`、`PATCH /teams/{id}`、`GET /me/teams` | `users`、`teams`、`team_memberships`、`audit_logs` | 用户；团队管理员/所有者可编辑团队 | **通过**：隔离 MySQL + Chromium 验证无效时区在请求前被拦截，个人资料和新建团队时区刷新后仍存在；设置页 320/360/375/390/430/768/1024/1280px 无整页溢出 |
| 设置 → 撤销当前会话 | 原先撤销当前设备后不会立即退出；现在先续期轮换令牌，再撤销当前设备并返回登录页 | `POST /auth/refresh`、`GET/DELETE /auth/sessions` | `auth_sessions` | 会话所有者 | **通过**：隔离接口 + Chromium 确认当前会话可识别、续期、撤销并返回登录页；跨浏览器撤销和旧令牌拒绝也通过 |
| 设置 → 下载个人数据副本 | 生成不缓存的 JSON 文件，包含本人账号数据和直接关联记录；事务内登记导出审计，排除团队共享业务数据及密码/渠道/推送密钥 | `GET /me/data-export` | 用户关联表、`audit_logs` | 本人 | **通过**：新建隔离数据库迁移后执行 Go 数据范围集成测试及 Chromium 设置页下载断言；未登录请求拒绝，文件包含本人会话/对话/记忆且无他人对话、密码哈希或访问令牌。另见账号注销和分批留存规则验收 |
| 设置 → 注销账号、清除私人数据 | 当前密码 + 确认语句；启用 MFA 时再验证动态码/恢复码；团队所有权和唯一超管保护；私人数据事务清除，团队记录匿名保留 | `POST /me/account` | `users`、`auth_sessions`、`conversations`、`user_memories`、`notifications`、`scheduled_jobs`、`channel_bindings`、`audit_logs` 等 | 本人；所有者先转移全部团队；唯一超管先建立另一位超管 | **通过（隔离 MySQL + Chromium 手机视口）**：错密码恢复、错误确认拒绝、团队所有权/唯一超管阻止、MFA 强制验证、私人对话/记忆清除、会话失效、共享任务和评论署名匿名保留；原邮箱不能再次登录 | 否；启用 MFA 时需第二因素
| 团队切换/设置 → 创建、切换、编辑多个团队 | 本地团队夹具改为按有效成员关系查询数据库团队；阻止较慢的旧请求覆盖新选中团队页面 | `GET /me/teams`、`POST /teams`、`GET/PATCH /teams/{id}` 及页面团队范围查询 | `teams`、`team_memberships`、`audit_logs` 及各页面业务表 | 成员可读；管理员可编辑；已登录用户可创建 | **通过**：接口及 Chromium 创建/编辑/刷新通过；测试故意延迟空团队概览响应，切换到有数据团队后再释放旧响应，确认新团队统计未被覆盖 |
| 邀请/加入 → 签发、复制、撤销、跨浏览器接受 | 本地令牌不能跨设备的问题改为哈希存储、过期且受使用次数限制的令牌 | `GET/POST/DELETE /teams/{id}/invitations`、`POST /invitations/accept` | `team_invitations`、`team_memberships`、`audit_logs` | 管理员/所有者签发；已登录用户接受 | **通过**：接口并发单次使用和两个浏览器上下文通过，包含登录后返回原邀请流程 |
| 成员 → 列表、角色、停用/移除、转移所有权 | 静态成员和角色改动改为持久化成员关系及审计 | `GET/PATCH /teams/{id}/members` | `team_memberships`、`audit_logs` | 成员可查看；管理员管理普通成员；所有者管理管理员/所有权 | **通过**：接口并发转移后仅保留一个所有者，并拒绝停用成员接任；Chromium 管理员升权和成员恢复通过 |
| 概览 → 统计和活动 | 演示统计改为 SQL 查询计算 | `GET /teams/{id}/summary` | `projects`、`tasks`、`team_memberships`、`plan_change_proposals` | 有效成员 | **通过**：Chromium 将五项统计逐项与当前团队接口响应比对，并打开匹配任务详情；没有预置演示统计 |
| 项目 → 搜索、状态、分页、创建/编辑/删除、排期 | 本地项目卡片改为经角色校验和父行锁保护的 SQL 增删改查 | `GET/POST/PATCH/DELETE /teams/{id}/projects` | `projects`、`audit_logs` | 有效成员可读；管理员/所有者可写 | **通过**：接口编辑/读取/日期校验/筛选/删除/角色检查通过；Chromium 创建、编辑日期和确认删除通过 |
| 任务/详情 → 搜索、筛选、排序、分页、状态、进度、评论、依赖、协作者、历史 | 浏览器夹具改为有版本控制的 SQL 任务；变更任务计划字段须提交审批提案 | `GET/POST/PATCH/DELETE /teams/{id}/tasks` 及嵌套 `comments`、`dependencies`、`collaborators`、`history` | `tasks`、`task_comments`、`task_dependencies`、`task_collaborators`、`task_plan_versions`、`audit_logs` | 成员可读；创建者/负责人/协作者按权限更新进度和依赖；评论作者管理自己的评论；管理员/所有者管理 | **通过**：接口允许列表排序、筛选、分页、协作者进度、评论归属、评论/依赖/协作者审计增删改查及依赖环拒绝通过。Chromium 创建/更新、依赖、评论编辑/删除、协作者持久化及手机端协作者更新通过 |
| 甘特图 → 项目/状态/负责人/日期筛选、依赖、里程碑、阻塞、日期定位 | 静态排期改为读取已批准的 SQL 任务计划 | `GET /teams/{id}/tasks`、`GET /teams/{id}/tasks/{task}/dependencies` | `tasks`、`task_dependencies` | 有效成员 | **通过**：Chromium 筛选、依赖标签、阻塞状态、日期标记和滚动定位通过；审批决定前正式日期保持不变；390px 触摸滚动和日期定位通过 |
| 项目/审批 → 智能体或成员提交项目变更提案、要求补充、重提、批准 | 项目写入必须经过待审批快照和项目版本校验；审批、正式更新、审计、通知及 outbox 同事务提交 | `GET/POST /teams/{id}/project-proposals`、`POST .../{id}/decision`、`POST .../{id}/resubmit` | `project_change_proposals`、`projects`、`audit_logs`、`notifications`、`outbox_events` | 团队成员提交；管理员/所有者处理 | **通过（迁移 21 隔离 MySQL + WebKit 双浏览器）**：要求补充→重提→修改后批准；审批前项目不变、批准后刷新可见；版本过期和重复处理拒绝 |
| 成员/审批 → 提交成员角色或状态提案、补充、重新提交、批准/拒绝 | 服务端存提案快照；审批前不变更成员权限；成员版本不一致则提案过期；管理员晋升须所有者审批 | `GET/POST /teams/{id}/member-proposals`、`POST .../{id}/decision`、`POST .../{id}/resubmit` | `member_change_proposals`、`member_change_approvals`、`team_memberships.version`、`notifications`、`audit_logs`、`outbox_events` | 有效成员提交；管理员处理普通成员变更；所有者处理管理员身份变更 | **通过（迁移 21 隔离 MySQL + Chromium/WebKit）**：审批前成员仍有效、审批后停用；拒绝自批/重复审批/管理员越权；成员版本变化使提案过期 |
| 审批 → 提交、批准/修改后批准/拒绝/要求补充/重新提交 | 完整任务提案快照在一次版本校验事务中应用 | `GET/POST /teams/{id}/plan-proposals`、`/decision`、`/resubmit` | `plan_change_proposals`、`plan_change_approvals`、`tasks`、`task_plan_versions`、`outbox_events`、`notifications`、`audit_logs` | 提交者须创建/拥有任务或为负责人；管理员/所有者审批 | **通过**：接口验证标题、描述、优先级、状态、进度、有效负责人、日期、里程碑、审批前数据不变、修改后批准、重新提交和版本冲突；Chromium 修改后批准、要求补充、重提通过，正式任务和甘特计划只在批准后更新 |
| 通知 → 列表、偏好、投递历史、邮件、浏览器/原生设备推送和定时提醒 | 迁移 11 增加邮件与加密 Web Push；迁移 18 增加加密 FCM 设备令牌及原生投递；外发通知仅含通用提示 | `GET /me/notification-channels`、`POST/DELETE /me/notification-channels/push-subscriptions`、`GET/POST/DELETE /me/notification-channels/native-devices`、`GET/PATCH /me/notification-preferences`、`GET /me/notification-deliveries`、`GET/POST/DELETE /me/reminders` | `notification_preferences`、`notification_push_subscriptions`、`notification_native_devices`、`notification_deliveries`、`notifications`、`scheduled_jobs` | 本人管理自己的通道/设备；团队提醒须为有效成员；投递工作进程按收件人偏好再次检查 | **代码、模拟外部协议和隔离 MySQL 通过；真实投递未验证**：SMTP 本地 STARTTLS、Web Push 加密/VAPID、FCM 服务账号 JWT/OAuth/HTTP v1 假对端、令牌加密、跨用户撤销拒绝、失效令牌撤销、失败重试、设置偏好与队列持久化通过。尚无真实 SMTP/VAPID/Firebase/APNs 配置和实体设备送达证据 |
| AI 助手 → 对话、流式发送、选择记忆候选 | 模拟模式明确标记；每次发送团队上下文都需确认；不保存未完成的流 | `GET/POST/DELETE /me/conversations`、`POST /me/conversations/{id}/stream` 及候选接口 | `conversations`、`conversation_messages`、`user_memories`、`audit_logs` | 对话所有者；团队操作另需当前有效成员关系 | **通过**：接口 SSE 分片/完成事件、持久化、团队上下文确认和跨用户隔离通过；Chromium 模拟流、历史刷新、候选选择和删除对话通过；隔离 MySQL 验证消息硬删、未确认候选删除、已确认记忆保留并清除来源。受限提取适配器已实现，真实模型服务仍需凭据 |
| 设置/助手 → 删除个人对话 | 所有者可删除自己的私人或团队上下文对话；消息级联清除，未确认记忆候选删除，已确认记忆保留但移除来源 | `DELETE /me/conversations/{id}` | `conversations`、`conversation_messages`、`user_memories`、`agent_runs`、`agent_tool_calls`、`channel_conversations`、`audit_logs` | 对话所有者 | 隔离 MySQL 验证其他账号 404、消息/工具记录清除、候选删除、已确认记忆去来源并留审计 | 否
| 记忆 → 私人/团队列表、创建/编辑/确认/删除、来源、复核/过期 | 候选由所有者复核前保持未确认；保留来源消息；重复内容返回冲突；显示计划复核和过期日期 | `GET/POST/PATCH/DELETE /me/memories`、`/me/conversations/{id}/messages/{messageId}/memory-candidate`、`/teams/{id}/memories` | `user_memories`、`team_memories`、`conversation_messages`、`audit_logs` | 个人数据仅所有者可读写；团队成员可读，管理员可写团队知识 | **通过**：接口隐私隔离、来源追溯、重复拒绝、确认/撤销及日期校验/持久化通过；Chromium 候选编辑、确认和来源摘录通过。真实凭据未配置，受限模型提取待联调 |
| 微信绑定 → 二维码/状态/验证/取消/解绑 | 腾讯 OpenClaw Gateway/插件发起扫码；Go 只访问受令牌保护的回环桥接；绑定写入 AES-GCM 加密凭据 | `/me/channels/wechat/...`；插件侧 `sessions/start`、`sessions/poll`、`sessions/cancel` | `channel_accounts`、`channel_bindings`、`audit_logs`、迁移 17 的 `provider_base_url` | 已登录且属于有效团队 | **真人扫码、确认、绑定和 API/Gateway/worker 重启读回通过**：清理测试夹具后，当前隔离库核实 1 条有效腾讯插件绑定；Worker 仍持续同步。实体微信内置浏览器页面待验 |
| 微信消息 → 接收/回复 | Go worker 经本机 Gateway 调用腾讯插件 `getUpdates` / `sendMessage` 适配器；重检绑定、发送者和团队成员关系 | Gateway `channel/updates`、`channel/send-text` | `channel_sync_states`、`channel_conversations`、`channel_message_deliveries`、`conversation_messages` | 扫码者身份 + 已绑定用户 + 有效团队 | **真实入站已验证，生产模型往返未通过**：清理测试夹具后保留 1 条有效渠道会话及 1 条入站记录；当前 `AI_PROVIDER=mock`，这条投递为失败。此前的测试模型/桥接数据不再计入真人验收；真实模型回复仍待验证 |
| 平台 → 审批/拒绝团队、查看审核审计、通知申请人 | 服务端校验审核权限；将原因与审核操作记入审计，并在同一事务写入通知 | `GET /platform/teams`、`GET /platform/audit-log`、`POST /platform/teams/{id}/decision` | `platform_roles`、`teams`、`audit_logs`、`notifications` | 仅平台超管 | **通过**：隔离环境初始化的超管可用；Chromium 390px 批准/拒绝，拒绝需填写原因，审核历史和原因持久化并通知申请人；拒绝团队不可访问；团队所有者调用审核/审计接口被拒 |

## 已执行命令与结果

| 命令/检查 | 结果 |
| --- | --- |
| 在新建空 MySQL 数据库运行 `scripts/migrate.sh teammind_verify_postgrant_1017x` | **通过**：执行迁移 `000001`–`000010`，之后仅授予应用账号该库的增删改查权限；应用账号可连接并查看全部 34 张表；MySQL 超级用户密码未出现在命令参数中 |
| 临时将同一测试账号权限扩为 `ALL` 后再次运行 `scripts/migrate.sh teammind_verify_postgrant_1017x` | **通过**：跳过全部十个已完成版本，并将数据库权限重新收敛为增删改查 |
| 使用新隔离库在 `127.0.0.1:8189` 启动 Go 接口服务 | **通过**：服务以 MySQL 模式启动；`/healthz` 与 `/readyz` 均返回 `OK` / `ready` |
| 在空隔离库运行 `scripts/rollback.sh teammind_verify_rollback_final_1017x` | **通过**：按相反顺序执行全部十个回滚迁移，只保留迁移账本；随后重新应用全部正向迁移成功 |
| `cd backend && MYSQL_DSN=<versioned isolated DB> go test ./... -count=1 -v` | **通过**：真实 SQL 测试覆盖微信发送者隔离、停用绑定拒绝、事务通知去重/抑制、持久化提醒创建/取消、私人数据读取范围、团队成员校验、并发后台处理与幂等投递、凭据加密及 AI 候选校验/来源检查 |
| `scripts/migrate.sh teammind_goal_20261019a` 后，设置隔离库 `MYSQL_DSN` 执行 `cd backend && go test ./... -count=1 -v`（2026-10-19） | **通过**：从空库执行 `000001`–`000015`；全量 Go 测试连接真实 MySQL，覆盖账号注销/MFA/IP 限流与告警/邮箱变更/个人数据导出/留存清理/智能体提案与断网沙箱/微信成员校验与收件人隔离/通知和提醒任务。首次运行的微信重启断言将 MySQL `HEX()` 大小写当作身份差异，已修正为十六进制大小写无关比较；重测通过 |
| `scripts/migrate.sh teammind_goal_20261019b` | **通过**：从空隔离 MySQL 数据库依次执行迁移 `000001`–`000016`，包括新增的 `agent_workspace_files` |
| `scripts/migrate.sh teammind_wechat_plugin_20261019` | **通过**：在全新隔离 MySQL 数据库执行迁移 `000001`–`000017`；迁移 17 为渠道绑定增加 `provider_base_url` |
| 在干净目录执行 `npm ci && npm test && npm test`（当前锁定版本） | **通过**：Node.js 24.16、OpenClaw 2026.10.4、腾讯 `@tencent-weixin/openclaw-weixin` 2.4.10；对 OpenClaw 声明的 npm 子依赖覆盖锁定 11.21.1，`npm audit` 为 0 项；干净安装后桥接测试连续两次通过，覆盖回环/令牌鉴权、请求校验、插件收发映射和二维码长轮询取消 |
| `TEAMMIND_API_BASE=http://127.0.0.1:8194/api/v1 python3 scripts/acceptance_wechat_plugin.py` | **通过（真实插件扫码起始阶段）**：在隔离迁移 17 数据库上经 TeamMind API 取得真实 QR、读到渠道等待状态并取消；测试不会输出 QR，未创建假绑定。未有真人扫码 |
| `cd backend && MYSQL_DSN=<teammind_wechat_plugin_20261019 隔离库> go test ./... -count=1` | **通过**：全量 Go 测试连接真实 MySQL；包括微信绑定加密、数据库重启恢复、区域 URL 安全校验和 worker 桥接请求。真人微信绑定/消息往返未验证 |
| 对 `teammind_goal_20261019a` 设置隔离库 `MYSQL_DSN` 后运行 `cd backend && go test ./... -count=1`（2026-10-19） | **通过**：全量 Go 测试连接真实 MySQL；新增覆盖工作区 AES-GCM 密文、账号隔离、CRUD/校验/审计、个人数据导出、注销清除，以及 Python 沙箱输出未授权时不落库、经本次明确授权后加密持久化。`internal/live` 用时 40.283 秒 |
| `TEAMMIND_WEB_BASE=http://127.0.0.1:5179 TEAMMIND_API_BASE=http://127.0.0.1:8193/api/v1 python3 scripts/acceptance_agent_workspace.py`（Chromium 手机触屏仿真） | **通过**：在迁移 16 的全新隔离库、服务端临时加密密钥下，实际注册/登录，导入文件、编辑、服务端校验失败后保留表单并重试、刷新后读取持久数据、导出数据副本、确认删除，并检查 320px 页面无横向溢出；脚本最后注销测试账号。该结果是手机视口仿真，不是实体手机验收 |
| `python3 scripts/acceptance_agent_workspace_restart.py`（隔离数据库 `teammind_accept_workspace_restart_20261019`） | **通过**：脚本构建并启动独立 API 二进制，注册临时账号，将 AES-GCM 密文文件写入 MySQL，发送 SIGTERM 完整停止 API，再以同一测试密钥启动新进程；沿用原会话从新进程读取并逐字节核对文件内容，随后删除文件并注销账号。验证真实 API 进程重启后的数据库恢复，不是仅重建 handler 或浏览器刷新 |
| `TEAMMIND_API_BASE=http://127.0.0.1:8189/api/v1 python3 scripts/acceptance_api.py`（应用账号仅有增删改查权限） | **通过**：注册、邀请/所有权并发、任务筛选/排序、协作者/评论/依赖、审批快照/重提、隐私、流式 AI、拒绝模拟提取、来源候选、重复冲突、复核/过期、投递记录、退出/重置、白名单团队任务查询，以及拒绝未知工具/非成员调用并审计 |
| 对有业务表但没有迁移历史的测试库运行 `scripts/migrate.sh teammind_verify_guard_1017x` | **通过**：脚本在执行 DDL 或修改权限前拒绝继续 |
| `npm run type:check` 和 `npm run build` | **通过**：当前代码类型检查和构建成功 |
| `python3 scripts/acceptance_browser.py`（Chromium） | **通过**：浏览器实际注册、开发重置链接跳转、单次密码重置和登录；六种登录状态、对比度、中心/边缘、Enter、Space、触摸、刷新、退出、会话过期、网络和超时反馈；模拟 AI 流、记忆候选来源/复核、模拟微信二维码/确认且不伪造绑定；TeamMind 十一个路由在 320/360/375/390/430/768/1024/1280px 均无整页溢出。截图位于 `/tmp/teammind-browser-current/` |
| `python3 scripts/acceptance_browser_workflow.py`（Chromium） | **通过**：两个浏览器上下文覆盖账号菜单设置/退出及令牌即时撤销、主题持久化、顶栏搜索、通知徽标导航、资料/团队设置、多团队切换隔离及旧响应保护、会话续期/撤销、密码修改、项目增删改查、13 项分页、项目状态/名称筛选、任务搜索/状态/排序、任务/甘特跳转、概览统计对账和近期任务详情、成员角色/停用/恢复/移除、通知已读/偏好、任务提案/重提、依赖、评论、甘特筛选/日期定位、桌面/手机只读任务工具、协作者进度、邀请复制、390px 手机抽屉和邀请接受/撤销；已撤销邀请无法接受 |
| `python3 scripts/acceptance_settings.py`（迁移至 14 的隔离库 + Chromium） | **通过**：资料/团队创建与编辑/时区校验、审计刷新、改密、会话管理、通知偏好与提醒、无登录浏览器邮箱确认、TOTP 设置/登录/关闭、恢复码下载与旧会话撤销；设置/通知/MFA 页面在 320/360/375/390/430/768/1024/1280px 无整页溢出 |
| `TEAMMIND_WEB_BASE=http://127.0.0.1:5177 TEAMMIND_API_BASE=http://127.0.0.1:8191/api/v1 python3 scripts/acceptance_settings.py`（2026-10-19，迁移 15 隔离库 + 独立开发 API/Vite） | **通过（完整重跑）**：重新验证个人数据导出、资料和团队设置、无效 IANA 时区拒绝、跨浏览器邮箱变更及撤销旧会话、TOTP 启用/登录/关闭、恢复码下载、会话撤销、通知/提醒和 320–1280px 设置/通知/MFA 页面；此次设置了临时 MFA 加密密钥以执行真实 TOTP 流程。第一次完整运行在新建团队后刷新时偶发等待团队表单超时；独立浏览器复现和后续完整重跑通过，未能再次复现原因，验收脚本仍有低频不稳定风险 |
| `TEAMMIND_WEB_BASE=http://127.0.0.1:5178 TEAMMIND_API_BASE=http://127.0.0.1:8192/api/v1 python3 scripts/acceptance_settings.py`（2026-10-19，本轮新建迁移 15 隔离库） | **通过**：在全新隔离库和独立 API/Vite 上重跑个人数据导出、资料/团队设置与刷新持久性、无效时区校验、第二浏览器邮箱验证及旧会话撤销、TOTP 启用/登录/关闭、恢复码下载、会话撤销与续期、通知偏好/免打扰、个人/团队提醒创建取消，以及 320–1280px 设置/通知/MFA 页面无横向溢出 |
| `python3 scripts/acceptance_reminders.py`（隔离 MySQL） | **通过**：Chromium 触摸登录、手机端创建/取消个人提醒、账号时区转换、后台进程将提醒投递到持久化收件箱、刷新后保留，以及 320px 溢出检查 |
| 有一条持久化提醒仍待执行时重启 API | **通过**：在 MySQL 创建任务，到期前停止 API，仅在 API 停机期间将隔离夹具设为到期，重启后观察到后台进程只处理一次；`/healthz` 和 `/readyz` 正常 |
| `python3 scripts/acceptance_cross_browser.py`（Firefox、WebKit） | **通过**：登录、注册、找回/重置密码及全部十一个业务路由在 320/360/375/390/430/768/1024/1280px 均无整页溢出。WebKit 使用 iPhone 类设备视口仿真，不是实体 iOS Safari |
| `python3 scripts/acceptance_cross_browser.py`（2026-10-19 重跑，Firefox + WebKit） | **通过（浏览器引擎/视口自动化）**：Firefox 桌面和 WebKit 手机视口均完成登录、注册、找回/重置密码及 11 个业务路由在 320/360/375/390/430/768/1024/1280px 的检查，包含审批页；页面无整页横向溢出，登录按钮默认可见并可提交。此前 WebKit 进入审批页时进程退出的问题本次未复现。该脚本是路由和布局冒烟检查，不替代设置操作回归或实体 iOS Safari 测试 |
| `TEAMMIND_WEB_BASE=http://127.0.0.1:5179 TEAMMIND_API_BASE=http://127.0.0.1:8193/api/v1 python3 scripts/acceptance_cross_browser.py`（本轮，Firefox + WebKit，迁移 16 隔离库） | **通过**：Firefox 桌面和 WebKit 手机仿真逐一完成登录、注册、找回/重置密码，以及全部 11 个业务路由（含审批）在 320/360/375/390/430/768/1024/1280px 的回归，无页面横向溢出，登录按钮可见并可提交；脚本通过后注销随机测试账号。脚本现支持隔离地址配置和测试后账号清理。仍不构成实体 iOS Safari/微信内置浏览器验收 |
| `python3 scripts/acceptance_platform.py`（Chromium） | **通过**：新迁移隔离库 `teammind_verify_review_1017` 上进行超管初始化与 390px 审核/拒绝；拒绝原因必填且写入 SQL，申请人收到通知，平台审计 API/界面可见，普通所有者不能查看该审计；其他越权请求也被拒，夹具邮箱符合格式 |
| 对工作区原有、未版本化的 `teammind` 数据库运行 `scripts/migrate.sh` | **按预期拒绝**：数据库已有表但 `schema_migrations` 没有记录；脚本不会盲目重放 DDL |
| TeamMind API 连接 OpenClaw 2026.10.4 Gateway 与腾讯插件（Node.js 24.16、隔离 MySQL） | **通过（扫码起始阶段）**：实际启动当前锁定版本 Gateway，经 TeamMind API 取得腾讯插件生成的二维码、读取状态并取消；未扫码时不创建绑定，二维码不写入报告。真人扫码、确认、消息交换和绑定后 Gateway/worker 重启恢复仍**未验证** |
| OpenClaw 2026.8.1 插件接入可行性实验（历史） | **仅为历史实验，已由当前插件桥接取代**：不代表当前运行形态。当前 Go API 和 worker 经认证的 loopback 桥接调用 OpenClaw 2026.10.4 Gateway 中运行的腾讯插件 |
| 测试 API 设置 `WECHAT_MODE=mock` | **通过**：模拟二维码/确认有明确标记，未创建真实绑定记录。浏览器测试首次在 `unconfigured` 模式失败，符合预期；按文档切换到模拟测试模式后重跑通过 |
| 在新的版本化测试库运行 `bootstrap-admin` | **通过**：首次运行创建唯一平台超管，第二次运行被拒。另一个已初始化超管可在 Chromium 390px 查看平台团队，但不能读取非成员团队项目或其他用户的个人记忆 |
| 测试 API 设置 `TEAM_APPROVAL_REQUIRED=1` | **通过**：待审核团队不能调用业务接口，普通用户不能审核；超管可填写原因批准/拒绝，重复审核返回冲突，申请人通知持久化 |

## 安全控制证据与剩余工作

| 控制领域 | 已实现位置/证据 | 部署并申请等保三级测评前仍需完成 |
| --- | --- | --- |
| 身份与访问控制 | bcrypt 哈希；可撤销的哈希会话；服务端团队范围检查；拒绝类接口测试；使用 MySQL advisory lock 的首位超管命令；迁移仅向所选应用数据库授予增删改查权限；迁移 12–15 的 IP 限流、可信代理解析、超管脱敏告警、邮箱变更、TOTP 多因素认证及时间步防重放 | 已有账号功能仍需生产规则复核；面向生产监控系统的告警联动；`MFA_ENCRYPTION_KEY` 的密钥托管、备份与轮换流程 |
| 安全审计 | 团队/成员/邀请/项目/任务/评论/依赖/协作者/计划/绑定/团队审核审计；计划和平台审核事务；审核原因存于团队记录 | 覆盖所有操作、不可变外部日志归档、本地日志保留 2555 天并由后台清理；外部不可变归档仍待部署 |
| 数据生命周期 → 到期数据清理后台任务 | 每小时按有界批次清理过期认证材料、结束提醒、微信敏感载荷、通知和旧本地审计；不删除有效凭据、待执行提醒或近期记录 | 后台任务（随 API 启动） | 认证/邀请/提醒/微信/通知/审计表 | 服务运维 | 隔离 MySQL 夹具验证各保留窗口前后边界；当前覆盖过期重置/邮箱/MFA/会话、通知、提醒和审计 | 否；生产期限需部署方法务与合规确认
| 传输与存储 | AES-GCM 渠道令牌；上游支持 TLS；凭据不进入浏览器 | TLS 反向代理、数据库静态加密、托管密钥及轮换流程 |
| 输入与 Web 防护 | JSON 大小限制、拒绝未知字段、SQL 参数绑定、安全响应头；所有 `/api/v1` 接口每来源每分钟限速 1200 次，认证、MFA 和邮箱操作另有严格限额；迁移 17 隔离 MySQL 验证接口限速跨业务路由生效且健康检查不受影响 | CSRF/CORS 部署审查、XSS/SSRF 测试和独立渗透测试 |
| 容器/主机 | MySQL 数据卷、本机端口、健康检查 | 加固镜像、服务以非特权账户运行、漏洞扫描和主机基线 |
| 监控与恢复 | `/healthz`、`/readyz`；有上限批次和脱敏重试日志的持久提醒后台进程；数据库通知投递记录 | 告警、自动备份、恢复演练、事件处置流程 |
| AI/智能体 | 模型工具白名单允许读团队任务/项目、建待审批任务/项目提案、经逐次授权创建待确认团队知识候选，或运行一次性授权 Python/Bash；SQL 集成测试验证提案审批边界、通知/审计/outbox/对话事务；沙箱使用非 root bubblewrap、断网、只读根、内存临时目录及资源限制 | 真实模型凭据未配置；目标生产主机须完成独立沙箱逃逸评估 |
| 第三方保障 | 腾讯插件协议固定为 2.4.10 并记录 MIT 许可证；本地模拟有明确标记 | 真人扫码、绑定和真实入站已验；真实模型回复与微信外发仍需真实模型配置和用户收发确认；另需依赖审计、独立评估、定级备案和运维证据 |

等保三级正式测评还要求结合实际部署进行定级备案、建立管理控制、提供日志和运维证据、接受第三方测评并完成整改。本仓库**并未通过等保三级测评**。

## 明确的验收限制

邮件通知和浏览器 Web Push 已接入 MySQL 队列、偏好、投递记录、加密订阅、SMTP/Web Push 适配器和失败重试；服务端现会拒绝示例域、保留域名及本机地址作为 VAPID 联系信息；当前服务的 `VAPID_SUBJECT=mailto:admin@example.invalid` 是占位值，SMTP 未配置；Android Chrome 模拟器订阅因推送服务连接失败而未创建，真实邮件与浏览器推送送达未验。智能体支持最多三步串行短工作流：可查询当前团队任务/项目，并提交待审批任务、项目或成员提案；管理员/所有者可逐次授权创建待确认团队知识候选；并行/重复调用拒绝，正式业务变更仍须有权人员审批。经逐次授权的断网 Python/Bash 沙箱必须作为本轮唯一工具。私人工作区支持加密持久文本文件，沙箱输出另需逐次授权才可保存。迁移 21 隔离 MySQL 测试覆盖这些权限边界；真实模型凭据未配置，商业模型实际选工具尚未验证，目标主机还需独立沙箱逃逸评估。设置控件逐项回归已覆盖资料、团队、密码、会话、邮箱跨浏览器验证、TOTP、审计、通知偏好、外部渠道空状态、个人/团队提醒；本次 WebKit 完整设置和双浏览器审批工作流通过。Android API 35 模拟器中的 Chrome 124 本次通过触摸登录、资料/通知偏好刷新持久化、提醒创建取消，412×765 CSS px 无横向溢出；实体 Android/iOS/微信内置浏览器仍未验。账号注销、导出和有界留存清理已在隔离 MySQL 验证；共享数据法律期限、外部不可变审计归档及正式部署核验仍待完成。

TeamMind 使用 OpenClaw `2026.10.4` Gateway 与腾讯维护的 `@tencent-weixin/openclaw-weixin@2.4.10` 插件（MIT）；二维码登录与消息 API 均由该插件实现，Go 仅经受认证的 loopback 桥接调用。本次重新运行 `scripts/acceptance_wechat_plugin.py`，真实插件 QR、等待状态、取消及扫码前不落绑定均通过。用户已真人扫码确认；清除由 Go 测试清理顺序缺陷遗留的 7 个专用 `Worker Test` 夹具后，当前隔离 MySQL 保留 1 个有效绑定、1 条入站记录、1 个渠道会话，Worker 仍持续同步；此前 API/Gateway/worker 重启后的绑定读回已验证。当前唯一投递失败发生在 `AI_PROVIDER=mock` 模式。历史上曾计入真人统计的本地 `openai_compatible` 测试记录已清除，不能作为真实模型或真实外发证据。运行中 API/worker 没有真实模型地址、模型名或密钥；真实模型回复闭环仍未验。绑定凭据在 MySQL 中 AES-GCM 加密；模拟模式不能创建真实绑定或发送模拟回复。

工作区已有的 `teammind` 数据库包含业务表但没有迁移记录，无法从当前代码确认其来源；未向该库写入迁移版本，接口服务现会拒绝连接。验证库 `teammind_verify_fresh_1017` 从空库创建并迁移至 `000010`；`teammind_verify_full_9c31` 也有截至 `000010` 的版本化迁移记录，供 SQL 集成测试使用。生产部署还需要 TLS 反向代理、密码重置 SMTP、托管密钥及轮换、备份/恢复演练、监控、运维流程、漏洞审查和正式等保三级定级/测评。以上测试结果均不构成这些事项的认证证明。

## 2026-10-19 后续复验

- 微信源码复核确认：Go 只访问认证 loopback Gateway 桥接；Gateway 固定加载 OpenClaw `2026.10.4` 与腾讯 `@tencent-weixin/openclaw-weixin@2.4.10`。扫码调用插件登录模块，收发调用插件自身 API 适配器。当前实现不是 Go 自行实现 iLink 协议。
- `cd backend/wechat-plugin-bridge && npm test`：4 项通过，覆盖桥接鉴权/回环限制、插件收发映射、扫码取消、插件确认结果映射和一次性登录会话清理。
- `TEAMMIND_API_BASE=http://127.0.0.1:8197/api/v1 python3 scripts/acceptance_wechat_plugin.py`：通过真实插件 Gateway 创建非模拟二维码、读取等待状态、取消并确认二维码创建不会直接产生绑定；未打印二维码且未由真人扫码。
- 从空数据库 `teammind_goal_wechat_20261019a` 执行迁移 `000001`–`000018` 后，`MYSQL_DSN=<隔离库> go test ./... -count=1` 通过。新增 `TestWechatPluginConfirmationPersistsTheConfirmedIdentity` 由测试桥接返回确认响应，核验 Go 端成员复查、绑定事务、扫码者哈希、区域地址与加密令牌的 MySQL 写入。该确认响应来自测试桩，不证明真实微信账号已绑定。
- 在该隔离库启动独立 API/Vite，`scripts/acceptance_settings.py` 通过：邮箱跨浏览器确认、MFA 设置/登录/关闭与会话撤销、个人数据下载、资料/团队设置、通知偏好、提醒及 320–1280px 设置/通知页面。
- 同一隔离环境运行 `scripts/acceptance_cross_browser.py`：Firefox 桌面和 WebKit 手机视口仿真均通过登录/注册/找回/重置及全部 11 个业务路由，在 320/360/375/390/430/768/1024/1280px 下检查通过；本次 WebKit 进程未退出。该结果不代表实体 iOS Safari、Android Chrome 或微信内置浏览器验收。
- `npm run type:check && npm run build` 通过。真实模型、SMTP/Firebase 凭据、Xcode/CocoaPods 及实体设备仍缺失；VAPID 已配置但没有活动订阅；真实模型调用、真人微信确认/消息往返、外部邮件/Web Push/FCM 送达和真实手机浏览器验收仍未验证。
- Android API 35 模拟器验收（2026-10-19）：使用 `TEAMMIND_API_BASE_URL=http://localhost:8199/api/v1 ALLOW_INSECURE_NATIVE_API=1 NATIVE_PUSH_OPTIONAL=1 NATIVE_TARGET=android npm run build:native`，随后以 JDK 21 和 Android SDK 35 执行 `android/gradlew assembleDebug`，均成功。APK 安装并启动成功；通过 `adb reverse tcp:8199 tcp:8199` 连到隔离 MySQL API，以手机触控完成注册并登录，看到服务端返回的真实空团队状态；随后打开设置页。WebView 124 的 412×915 CSS 视口下，设置页没有整页横向溢出（`scrollWidth=412`）。这是模拟器/WebView 验收，不是实体 Android Chrome 或 FCM 推送验收；Debug 包只允许本地 HTTP，不得部署生产。
- Android 冷启动会话复验发现并修复了路由守卫缺陷：勾选“保持登录”时，API 令牌已保存且 `/me` 返回 200，但重启后仍停留登录页。根因是已有令牌时守卫完成身份恢复后仍无条件放行 `/login`。`src/router/guard/userLoginInfo.ts` 现将有效会话从登录路由转到安全的站内 redirect 或团队概览。重新构建/安装后，在 API 35 模拟器强制结束并重开 App，已恢复到团队概览，之前创建的团队及真实成员统计仍显示；令牌留在 `localStorage`，没有保存密码。
- `TEAMMIND_WEB_BASE=http://127.0.0.1:5190 TEAMMIND_API_BASE=http://127.0.0.1:5190/api/v1 python3 scripts/acceptance_cross_browser.py`：本轮重跑 Firefox 桌面和 WebKit 手机仿真，认证页及全部 11 个业务路由（含审批）在 320/360/375/390/430/768/1024/1280px 全部通过，测试账号清理通过。这证明浏览器引擎/视口检查已完整运行；WebKit 仿真不等于实体 iOS Safari。
- `TEAMMIND_WEB_BASE=http://127.0.0.1:5190 TEAMMIND_API_BASE=http://127.0.0.1:5190/api/v1 python3 scripts/acceptance_settings.py`：完整设置专项重跑通过，覆盖个人数据导出、资料/团队设置、跨浏览器邮箱验证、TOTP 启用/登录/恢复码/关闭、会话撤销、通知与提醒，以及设置/通知/MFA 页 320–1280px 布局。接口经 Vite 同源代理访问隔离 API。
- `TEAMMIND_BROWSER=webkit TEAMMIND_WEB_BASE=http://127.0.0.1:5191 TEAMMIND_API_BASE=http://127.0.0.1:8198/api/v1 python3 scripts/acceptance_settings.py`：完整 WebKit 设置专项通过，包括个人数据导出、资料/团队设置、跨浏览器邮箱验证、TOTP 启用/登录/恢复码下载/关闭、会话撤销/续期、通知偏好和个人/团队提醒，以及设置/通知/MFA 页 320–1280px 布局。为跨域检测 CORS 失败的探测改由测试脚本直接请求隔离 API；产品 API 和浏览器的正常同源请求保持原样。WebKit 是浏览器引擎仿真，不代表实体 iOS Safari。
- `TEAMMIND_WEB_BASE=http://127.0.0.1:5191 TEAMMIND_API_BASE=http://127.0.0.1:8198/api/v1 python3 scripts/acceptance_android_chrome.py`：Android 15 / API 35 模拟器中启动的 Android Chrome 124，经 CDP 触摸完成登录、个人资料保存及刷新持久化、通知偏好保存及刷新持久化、提醒创建与取消。取消的提醒刷新后仍显示“已取消”且不再提供取消按钮。412×765 CSS px、DPR 2.625 下，设置页和通知页 `scrollWidth` 均为 412。测试账号经账号注销 API 清理；本轮使用独立隔离 API/限流密钥。测试前需执行 `adb reverse tcp:5191 tcp:5191` 和 `adb forward tcp:9222 localabstract:chrome_devtools_remote`。这是模拟器里的 Android Chrome 实际浏览器测试，不是实体 Android 手机验收。
- 本轮在相同迁移 18 隔离 MySQL 上重新运行账号安全与数据生命周期集成测试：`go test ./internal/live -run '^(TestRFC6238TOTPVectors|TestMFASecretEncryptionIsBoundToUser|TestMFAEnrollmentLoginRecoveryAndDisable|TestEmailChangeVerificationLifecycle|TestRequestClientIPUsesOnlyTrustedProxyHeaders|TestRateLimitKeyProductionConfiguration|TestLoginIPRateLimitAndPlatformAlert|TestAPIRateLimitCoversBusinessRoutesAndLeavesHealthChecksAvailable|TestConcurrentFirstRequestsShareOneRateLimitCounter|TestPersonalDataExportIsAuthenticatedAndScoped|TestAccountDeletionRequiresOwnershipTransferAndPurgesPrivateData|TestRetentionPolicyPrunesExpiredRecordsAndPreservesRecentHistory)$' -count=1`，通过（58.136 秒）。验证 MFA/TOTP、邮箱更改、来源限流、个人数据导出、账号删除和留存清理；不证明生产密钥托管、邮件送达或生产告警联动。
- 同一隔离迁移 18 MySQL 重跑 `TestModelSelectedWriteToolCreatesOnlyPendingProposal`、`TestModelSelectedSandboxToolUsesTemporaryInputsAndOutputs`、`TestAgentWorkspaceFilesEncryptedCRUDAndUserIsolation`、`TestAgentWorkspaceFileNameValidation`：通过（0.343 秒）。确认模型写工具只建待审批提案，Python 沙箱按授权处理临时输入/输出，私人工作区文件加密持久化且跨用户隔离。后续新增的 Bash 沙箱另有集成测试；它只在隔离、断网环境执行，不开放宿主机任意 Shell 或未经审批的业务数据写入。
- `TEAMMIND_API_BASE=http://127.0.0.1:8200/api/v1 python3 scripts/acceptance_wechat_plugin.py`：本轮重新连接 real-mode TeamMind API 与当前腾讯插件 Gateway，通过非模拟二维码、等待状态、取消和扫码前无绑定断言。未真人扫码；不构成绑定或消息往返验收。

## 2026-10-19 继续复核

- 在迁移 18 隔离库 `teammind_goal_wechat_20261019a` 执行 `go test ./... -count=1`：修正限流告警测试对“整个数据库只能有一条告警”的错误假设后，全量 Go 集成测试通过（42.037 秒）。该测试现在只断言本用例的 HMAC 来源标识存在，并断言响应不包含明文 IP。
- `go test -race ./internal/live -run '^TestWechatSessionStateConcurrentUpdatesKeepIndependentFields$' -count=1`：通过。新增并发快照测试，覆盖扫码状态、验证码、错误和终态并发更新。
- `cd backend/wechat-plugin-bridge && npm test`：4 项通过；固定依赖仍为 OpenClaw `2026.10.4` 和腾讯 `@tencent-weixin/openclaw-weixin@2.4.10`。
- `npm run type:check && npm run build`：通过。
- `TEAMMIND_WEB_BASE=http://127.0.0.1:5192 TEAMMIND_API_BASE=http://127.0.0.1:8200/api/v1 python3 scripts/acceptance_cross_browser.py`：Firefox 桌面及 WebKit 移动仿真均通过登录/注册/找回/重置，以及 11 个业务路由（包括审批页）在 320/360/375/390/430/768/1024/1280px 的检查。
- `TEAMMIND_BROWSER=webkit TEAMMIND_WEB_BASE=http://127.0.0.1:5192 TEAMMIND_API_BASE=http://127.0.0.1:8200/api/v1 python3 scripts/acceptance_settings.py`：WebKit 设置全流程通过，覆盖数据导出、资料/团队设置、跨浏览器邮箱确认、TOTP/MFA、会话撤销、通知、提醒及 320–1280px 设置相关页面。
- 微信状态超时复核：模拟器保留的一个旧会话状态请求超时超过 15 秒；同一服务的普通身份/渠道 API、新建扫码会话和新会话状态读取均在毫秒级完成。原状态端点需取得会话互斥锁；现改为 Go 1.18 兼容的 `atomic.Value` 不可变快照，状态读取不会等待扫码长轮询。新构建重启后，`scripts/acceptance_wechat_plugin.py` 再次通过真实插件二维码、等待状态、取消和扫码前无绑定检查。
- 微信里手机页面的轮询失败恢复：Playwright 在 390px 视口对第一次会话 GET 注入 503，确认界面将状态改为“状态未知”、说明服务端未确认且清除过期的“等待扫码”状态；刷新后取得新的真实插件二维码并恢复等待状态，取消成功，页面无横向溢出。
- 为避免任何二维码被误报为仍有效，本轮专门展示的真实二维码在 5 分钟内未被真人扫描，已过期；未创建持久绑定。真人确认、消息收发及 Gateway/worker 重启恢复仍未验证。
- Android 原生壳 WebView 复验（2026-10-19）：在 Android 15 / API 35 模拟器、WebView Chrome 124、412×915 CSS px（DPR 2.625）中，以 Android 触摸事件完成真实 TeamMind API 登录并打开设置和通知页。发现并修正验收服务的 CORS 配置：Debug 包来源为 `http://localhost`，隔离 API 原先只放行 Vite 来源；本地验收服务现仅精确允许 `http://localhost`，生产跨域策略没有放宽。设置和通知页均无横向溢出（`scrollWidth=412`）；页面明确显示 SMTP、VAPID 和 Firebase 未配置，并禁用相应外发/原生推送入口。测试账号已通过注销 API 删除。该结果是模拟器 WebView 验收，不是实体手机、Firebase/APNs 或推送送达验收。
- WebKit 完整设置回归（2026-10-19）：`TEAMMIND_BROWSER=webkit TEAMMIND_WEB_BASE=http://127.0.0.1:5194 TEAMMIND_API_BASE=http://127.0.0.1:8202/api/v1 python3 scripts/acceptance_settings.py`，退出码 0。覆盖个人数据导出、资料与团队设置、跨浏览器邮箱确认、TOTP/MFA 启用/登录/关闭和恢复码、会话撤销/续期、通知偏好、个人/团队提醒及 320–1280px 布局。
- WebKit 双浏览器业务全流程（2026-10-19）：`TEAMMIND_BROWSER=webkit TEAMMIND_WEB_BASE=http://127.0.0.1:5195 TEAMMIND_API_BASE=http://127.0.0.1:8203/api/v1 python3 scripts/acceptance_browser_workflow.py`，退出码 0。实际完成审批修改、提案重提与批准、甘特筛选、邀请撤销/接受、成员角色、任务依赖、协作者和跨团队隔离；不是仅验证审批页渲染。
- 真实插件扫码起始流程再次复验（2026-10-19）：`TEAMMIND_API_BASE=http://127.0.0.1:8197/api/v1 python3 scripts/acceptance_wechat_plugin.py`，退出码 0。实际由 TeamMind API 取得腾讯插件生成的二维码内容、轮询状态并取消；脚本未输出二维码，未有人扫码，因此不能据此认定真实绑定或消息收发通过。运行 API 连接 loopback `127.0.0.1:18793` Gateway，其可执行文件来自 Node.js 24.16.0；依赖锁定 OpenClaw `2026.10.4` 与 `@tencent-weixin/openclaw-weixin@2.4.10`（MIT）。这直接反证“当前 Go 直接实现 iLink 且没有运行腾讯插件”的描述。
- 账号安全、个人数据生命周期及智能体权限专项（2026-10-19）：在迁移 18 隔离数据库执行 `go test ./internal/live -run '<MFA/TOTP、邮箱变更、来源限流、数据导出/注销/留存、计划提案、断网 Python、私人工作区>' -count=1`，16 项通过（37.363 秒）。这证明对应实现和隔离 MySQL 集成，不证明生产密钥托管、真实邮件送达、生产主机沙箱逃逸评估或外部告警联动。

### 对过时缺口描述的核对

- 腾讯 OpenClaw 微信插件已经通过受令牌保护的回环 Gateway 实际运行；Go 服务与 worker 调用该桥接，不直接向 iLink 发请求。插件扫码、确认映射及消息 API 适配代码/测试存在。尚缺真人微信完整闭环验收。
- MFA/TOTP、邮箱变更确认、来源/API 限流、脱敏告警、个人数据访问导出、注销删除和有界留存清理均已有实现及隔离 MySQL 测试。生产密钥托管、SMTP 投递、外部告警、外部审计归档和部署规则仍未完成。
- Agent 私人工作区已有 AES-GCM 加密的 MySQL 文件导入、读取、编辑、删除、导出和逐次授权沙箱读写。任意宿主机 Shell、任意业务表写入和绕过人工审批的计划修改是明确关闭的安全边界，不应列为遗漏功能。
- 本次新增的成员身份提案覆盖非所有者成员角色/状态调整，按成员版本检测过期并要求合格管理员审批。智能体仍不能代办邀请或所有权转移；任意宿主机 Shell 和绕过审批的业务写入继续关闭。
- TeamMind 是以网页为主的产品；浏览器 Web Push 是网页推送路径。仓库另含可选 Capacitor 外壳和原生 FCM 投递适配器，但正式原生包发布、iOS 构建/签名、Firebase/APNs 配置及真实设备送达未验。本机本轮没有 Android 设备连接，当前 JDK 17，不能据此声称重新构建或真机验收通过。

## 2026-10-19 分项复验续记

- 从空 MySQL 库 `teammind_step_goal_20261019` 执行 `scripts/migrate.sh`：迁移 `000001_core` 至 `000018_native_push` 全部成功。随后使用该库的受限应用账号启动 API，`/healthz` 返回 `ok`、`/readyz` 返回 `ready`。
- 在这份全新迁移库上运行 `go test ./... -count=1`：通过（43.767 秒）。另一次在已有迁移 18 隔离库运行全量套件也通过（53.218 秒）。首次复跑曾发现 `TestWechatWorkerDoesNotDeliverMockAIReplies` 使用固定发送者哈希，与旧验收夹具的唯一索引冲突；测试改为按本用例随机渠道账号生成发送者标识，针对性用例和空库全量测试均通过。
- `npm run type:check && npm run build`：通过；`cd backend/wechat-plugin-bridge && npm test`：4 项通过，覆盖回环/令牌鉴权、腾讯 API 收发映射、扫码取消和插件确认结果映射。
- `TEAMMIND_WEB_BASE=http://127.0.0.1:5194 TEAMMIND_API_BASE=http://127.0.0.1:8202/api/v1 python3 scripts/acceptance_cross_browser.py`：Firefox 桌面和 WebKit 手机仿真通过登录、注册、找回/重置，以及包含审批在内的 11 个业务路由；视口为 320、360、375、390、430、768、1024、1280 像素。
- `TEAMMIND_BROWSER=webkit TEAMMIND_WEB_BASE=http://127.0.0.1:5194 TEAMMIND_API_BASE=http://127.0.0.1:8202/api/v1 python3 scripts/acceptance_settings.py`：完整 WebKit 设置流程通过，覆盖个人数据导出、资料和团队设置、跨浏览器邮箱确认、TOTP/MFA、会话续期/撤销、通知偏好和提醒，并检查设置、通知、MFA 页面全部八种视口。
- `TEAMMIND_BROWSER=webkit TEAMMIND_WEB_BASE=http://127.0.0.1:5195 TEAMMIND_API_BASE=http://127.0.0.1:8203/api/v1 python3 scripts/acceptance_browser_workflow.py`：WebKit 双浏览器业务流程通过，覆盖主题和顶栏、手机抽屉、跨团队隔离、项目分页与筛选、成员角色、通知、任务、依赖、计划提案修改后审批与重提、甘特筛选、协作者、邀请复制/撤销/手机接受及只读智能体工具。该流程覆盖 WebKit 审批操作，不再只是审批路由打开检查。
- `TEAMMIND_API_BASE=http://127.0.0.1:8205/api/v1 python3 scripts/acceptance_wechat_plugin.py`：以新的测试来源限流密钥启动 real-mode API 后检查全部通过，包含真实腾讯插件生成二维码、读取等待状态、取消和确认未扫码前没有绑定。脚本不打印二维码；未进行真人扫码、绑定或消息发送，因此微信完整闭环仍未验证。此前在旧 API 上的单次脚本失败是注册来源限流触发，并非扫码流程失败；新隔离限流密钥重跑通过。

## 2026-10-19 智能体 Shell 与设置复验续记

- 新增 `TestAgentResultSummaryReflectsSandboxPersistence`：核对临时与加密持久化输出的审计状态准确，且不写入命令、输出、文件名或文件内容。修正 Bash 沙箱可用性检查不应依赖 Python 的问题。针对性隔离 MySQL Python/Bash 执行测试、`go test ./... -count=1` 和沙箱相关 `go test -race` 通过。
- `scripts/acceptance_agent_shell_ui.py`：390px Chromium 测试验证 Bash 独立授权、调用后授权复位、结果经私人工作区 API 保存，最终删除测试账号。模型响应由测试桩提供，仅证明 UI 授权和保存链路。
- `scripts/acceptance_settings.py` 分别在 Chromium 和 WebKit 全流程通过。首次 WebKit 回归暴露脚本按列表首项选择撤销对象的问题；已明确选择非当前会话并重跑通过，确认跨设备会话撤销、续期和当前会话退出流程。
- `TEAMMIND_BROWSER=webkit TEAMMIND_WEB_BASE=http://127.0.0.1:5210 TEAMMIND_API_BASE=http://127.0.0.1:8210/api/v1 python3 scripts/acceptance_browser_workflow.py`：在独立限流密钥的迁移 18 隔离库上通过。真实执行双浏览器邀请/角色、项目与任务、提案修改/重提/批准（含状态/进度更新）、甘特筛选、通知、跨团队隔离和移动抽屉操作；第一次使用共享限流实例时注册收到 429，未进入流程，改用隔离 HMAC 限流键重跑通过。
- 智能体 `propose_task_change` 增加 `status` 和 `progress` 字段。隔离 MySQL 全量 Go 测试验证模型可创建包含状态/进度的 pending 提案，正式任务在审批前仍保持原值，管理员批准后才同步更新任务行和 `task_plan_versions`。WebKit 审批表单实测批准状态 `doing` / 进度 `65%`，任务列表读取到新值；新审批服务实例上 `go test ./... -count=1` 全部通过。
- `TEAMMIND_API_BASE=http://127.0.0.1:8197/api/v1 python3 scripts/acceptance_wechat_plugin.py` 与 `cd backend/wechat-plugin-bridge && npm test` 通过（插件桥接 4/4）。真实腾讯插件二维码获取/轮询/取消通过；无人扫码，真人确认、绑定持久化、微信消息往返和绑定后整进程重启恢复仍未验证。

## 2026-10-19 当前回合续验

- 在新建隔离 MySQL 库 `teammind_goal_settings_20261019c` 上启动带临时 `MFA_ENCRYPTION_KEY` 的 API，运行 `TEAMMIND_BROWSER=webkit TEAMMIND_WEB_BASE=http://127.0.0.1:5201 TEAMMIND_API_BASE=http://127.0.0.1:8211/api/v1 python3 scripts/acceptance_settings.py`：**通过**。覆盖个人数据导出、资料/团队设置、跨浏览器邮箱确认、MFA 启用/登录/关闭与恢复码、会话续期/指定设备撤销、通知偏好、提醒及设置/通知/MFA 页面 320–1280px。
- 本次 WebKit 设置复验修正了测试会话选择：脚本现在读取接口返回的 session ID 并准确撤销第二浏览器对应的记录，不再假设第一个非当前会话就是目标。未配置 MFA 密钥的旧 API 曾使原脚本在流程中途失败；脚本现先检查 `/me/mfa`，未配置时立即删除刚创建的测试账号并报告缺少 `MFA_ENCRYPTION_KEY`。该前置条件分支已在未配置密钥的隔离开发 API 上验证。
- 在上述隔离 API 上运行 `TEAMMIND_BROWSER=webkit TEAMMIND_WEB_BASE=http://127.0.0.1:5201 TEAMMIND_API_BASE=http://127.0.0.1:8211/api/v1 python3 scripts/acceptance_browser_workflow.py`：**通过**。这是真正的 WebKit 双浏览器操作回归，完成审批修改/重提/批准、邀请、成员角色、项目与任务、甘特筛选、通知和跨团队隔离，不只是打开审批页面。
- `scripts/migrate.sh teammind_goal_security_20261019d` 从空库完成迁移 `000001`–`000018`；在此隔离库运行 `go test ./... -count=1`：通过（`internal/live` 50.082 秒）。腾讯桥接 `npm test`：4/4 通过；`npm run type:check` 和设置脚本 `py_compile` 通过。
- `TEAMMIND_API_BASE=http://127.0.0.1:8194/api/v1 python3 scripts/acceptance_wechat_plugin.py`：通过。TeamMind real-mode API 经 OpenClaw Gateway 获取腾讯插件生成的二维码、读取等待状态、取消扫码会话并验证未扫码前没有绑定；该测试不输出二维码，也没有真人扫码。
- 本回合另通过真实插件创建并展示一张限时二维码供真人扫码；会话到期时数据库仍无活动绑定，故真人扫码/确认未通过验收。旧图已过期，不能继续使用。为检查 worker 启动前置条件，将明确的隔离测试库从迁移 17 升级到 18；worker 在升级后可启动，但由于没有确认绑定，未处理真实微信消息。AI 凭据仍缺失，不能验证真实回复。
- 本回合没有真实模型凭据、真人微信扫码/消息往返或实体手机浏览器证据。WebKit 测试只证明浏览器引擎自动化；它不等于实体 iOS Safari 或微信内置浏览器验收。
- 当前服务进程没有设置真实模型地址、模型名和 API 密钥，`AI_PROVIDER` 走默认模拟模式。真实模型对话/记忆提取/模型选工具未验证；不使用模拟服务冒充上线。
- `scripts/acceptance_agent_workspace_restart.py`：在本轮新建的迁移 18 隔离库上通过。脚本写入 AES-GCM 私人工作区文件、完整停止 API 进程，再以相同测试密钥启动新 API，沿用原会话逐字节读回文件；随后清理测试账号。
- 当前工作区 `.env` 未设置 `AI_PROVIDER`，服务端在未设置时回退到 `mock`；真实模型地址/模型名/密钥、SMTP、Firebase 均未配置；VAPID 已配置，但外部推送订阅失败，因此真实 AI 调用/工具选择和外部通知送达仍未验。Agent 代码范围是白名单查询、待审批任务/项目提案、逐次授权的待确认团队知识候选、断网 Python/Bash 沙箱和本人加密工作区；宿主机 Shell、绕过审批的业务写入明确不开放。
- `adb devices` 当前没有连接设备；本机仅发现 Android platform-tools，JDK 为 17，没有 Xcode/CocoaPods。此前记录的 Android 模拟器与 Android Chrome 测试仍是模拟器证据；本轮无法据此声称实体 Android、iOS Safari 或微信内置浏览器已验收。TeamMind 是网页产品，Capacitor 原生壳为可选扩展。

## 2026-10-19 七项清单续验

- 在隔离迁移 18 MySQL `teammind_goal_wechat_20261019a` 上，先运行 `TestModelSelectedWriteToolCreatesOnlyPendingProposal`，再运行 `go test ./... -count=1`：均通过。全量 Go 测试用时 38.509 秒；覆盖智能体计划提案获批后正式任务、版本快照、审批记录、通知、审计和 outbox 事务写入。新增断言最初查询了不存在的 `notifications.related_id`；依据表结构改为 `resource_id` 后针对性与全量测试通过。
- `TEAMMIND_BROWSER=webkit TEAMMIND_WEB_BASE=http://127.0.0.1:5211 TEAMMIND_API_BASE=http://127.0.0.1:8211/api/v1 python3 scripts/acceptance_settings.py`：**通过**。WebKit 完成个人数据导出、资料/团队设置、跨浏览器邮箱确认、TOTP/MFA、恢复码、会话续期/撤销、通知与提醒，以及设置/通知/MFA 页面 320–1280px 视口检查。
- `TEAMMIND_BROWSER=webkit TEAMMIND_WEB_BASE=http://127.0.0.1:5211 TEAMMIND_API_BASE=http://127.0.0.1:8211/api/v1 python3 scripts/acceptance_browser_workflow.py`：**通过**。WebKit 双浏览器实际完成项目 CRUD/筛选/分页、成员角色、通知读写、任务依赖、计划提案修改与审批/重提、甘特筛选与日期定位、协作者、邀请复制/撤销/接受、手机抽屉及跨团队隔离。
- 本次仍未执行真人微信扫码、真实 AI 服务调用或实体手机浏览器验收。模拟模型、插件测试桩、WebKit 视口仿真均不作为这些真实外部验收的替代证据。
- 智能体串行工作流（本地模型 HTTP 测试端 + 隔离 MySQL）：修改后的 `TestModelSelectedWriteToolCreatesOnlyPendingProposal` 通过。脚本桩按顺序要求先调用 `list_team_tasks`、检查第二轮上下文包含任务结果，再调用 `propose_task_change`；第三轮生成最终回复。验证两个工具调用和审计均存在、提案状态为待审批、正式任务标题/状态/进度在审批前未变化。
- 智能体界面（Chromium 390px）：`TEAMMIND_WEB_BASE=http://127.0.0.1:5211 TEAMMIND_API_BASE=http://127.0.0.1:8211/api/v1 python3 scripts/acceptance_agent_shell_ui.py` 通过；验证 Shell 需单独勾选且授权使用后复位、输出经私人工作区 API 保存，以及两步任务查询/待审批提案结果按步骤展示。该 UI 流程用固定接口响应，不证明真实模型已连接。
- 腾讯微信插件真实扫码起始阶段：`TEAMMIND_API_BASE=http://127.0.0.1:8197/api/v1 python3 scripts/acceptance_wechat_plugin.py` 全部通过，包括 TeamMind API 实际取得非模拟腾讯 QR、轮询、取消、会话取消状态持久化和扫码前无绑定；脚本没有输出二维码，没有真人扫描。
- `go test ./... -count=1` 在隔离迁移 18 MySQL 上通过（最终复跑 63.973 秒）；`go test -race ./internal/live -run '^TestModelSelectedWriteToolCreatesOnlyPendingProposal$' -count=1` 通过；`npm run type:check && npm run build` 通过。全量后端、针对性竞态和前端构建均覆盖本轮智能体串行流程代码。
- 本轮重新执行账号安全和数据生命周期集成测试：`TestMFAEnrollmentLoginRecoveryAndDisable`、`TestEmailChangeVerificationLifecycle`、`TestLoginIPRateLimitAndPlatformAlert`、`TestPersonalDataExportIsAuthenticatedAndScoped`、`TestAccountDeletionRequiresOwnershipTransferAndPurgesPrivateData`、`TestRetentionPolicyPrunesExpiredRecordsAndPreservesRecentHistory` 均在隔离迁移 18 MySQL 通过（50.587 秒）。证明这些是已有并通过本地数据库验证的控制；不证明生产密钥托管、SMTP 送达、外部告警和法律留存期限已完成。
- 当前运行的 real-mode TeamMind API `127.0.0.1:8194` 再次通过 `scripts/acceptance_wechat_plugin.py`：ready 检查通过；实际取回腾讯插件二维码、读取等待状态、取消会话、验证取消状态持久化和扫码前无绑定。脚本不输出二维码，没有真人扫描确认，因此此项仍只覆盖扫码起始阶段。


## 2026-10-19 智能体团队知识候选

- 新增团队管理员/所有者逐次授权的 `create_team_memory_candidate` 工具。授权缺失、普通成员、敏感内容和重复内容均由服务端拒绝；未确认候选在人工确认前不进入 AI 团队上下文。候选写入、来源用户消息、工具调用记录、审计和管理员通知同属 MySQL 事务。
- 团队知识读取规则收紧：未确认候选只对管理员/所有者可见；来源对话 ID 与摘录只返回给候选作者，避免其他管理员借知识列表读取私人对话。候选在团队知识页进入待确认状态，管理员可编辑并确认。
- `MYSQL_DSN=<隔离迁移18库> go test ./internal/live -run TestModelSelectedTeamKnowledgeCandidateRequiresConsentAndKeepsSourcePrivate -count=1 -v`：通过。覆盖本地模型 HTTP 工具选择、管理员授权、成员越权在模型调用前拒绝、默认未确认、来源消息、AI 上下文隔离、管理员通知、审计、工具调用记录、来源隐私、普通成员不可见、敏感内容和重复候选拒绝，以及两个并发候选只允许一个落库。
- `TEAMMIND_WEB_BASE=http://127.0.0.1:5197 TEAMMIND_API_BASE=http://127.0.0.1:8219/api/v1 python3 scripts/acceptance_agent_team_memory_ui.py`：Chromium 通过；再设置 `TEAMMIND_BROWSER=webkit` 重跑，WebKit 交互也通过。两者在助手页和团队知识页检查 320、360、375、390、430、768、1024、1280 CSS 像素下均无页面级横向溢出；覆盖管理员/所有者一次性授权、团队上下文确认、提交后复位、团队知识审核/来源跳转，以及普通成员不显示授权控件。WebKit 在 1280px 团队知识布局记录到一条非致命 `ResizeObserver loop completed with undelivered notifications`，其余交互、来源跳转和视口断言均通过。UI 使用确定性 API 响应，不代表真实模型服务已接通，也不替代 iOS 真机 Safari。
- 本轮隔离库 `teammind_agent_memory_20261019a` 由空库执行 `000001`–`000018` 迁移成功；针对性 MySQL 集成测试和全量 `go test ./... -count=1` 通过。`npm run type:check && npm run build` 与腾讯插件桥接 `npm test`（4/4）通过。




### 2026-10-19 腾讯微信移动网页入口复验

- 从空隔离数据库执行迁移 `000001`–`000019` 后，启动真实模式 API、微信 worker 和固定版本 OpenClaw Gateway/腾讯插件。
- `TEAMMIND_BROWSER=webkit TEAMMIND_WEB_BASE=http://127.0.0.1:5225 TEAMMIND_API_BASE=http://127.0.0.1:8222/api/v1 python3 scripts/acceptance_wechat_ui.py`：通过。390px WebKit 登录后实际创建腾讯插件二维码、将其渲染为网页图片、显示扫码等待和“另一设备扫码”说明，无横向溢出、无运行时错误；取消后会话持久为 cancelled，未创建渠道绑定。二维码没有被打印，未进行真人扫码。
- 页面 API 会将没有活动绑定的账号显示为未连通；本测试断言了该状态。真实消息 worker 需要绑定后才会报告该账号通道可用。

## 2026-10-19 本次持续验收

- 复核 `backend/wechat-plugin-bridge` 源码和锁定依赖：Go API/worker 只调用带 32 字节共享令牌的回环接口；Node 桥接从腾讯插件 `@tencent-weixin/openclaw-weixin@2.4.10` 导入扫码登录模块和消息 API 适配器。固定 Gateway 为 OpenClaw `2026.10.4`。因此“Go 自行实现 iLink、没有实际运行腾讯插件”的旧描述不符合当前工作树；Go 源码中的 `ilinkai.weixin.qq.com` 仅用于校验/默认化腾讯插件返回的渠道地址。
- 在真实模式验收 API `127.0.0.1:8197` 上运行 `scripts/acceptance_wechat_plugin.py`：全部通过，实际创建非模拟腾讯二维码、读取等待状态并取消；脚本不输出二维码。`127.0.0.1:5225` 网页与 `127.0.0.1:8222` API 的 `scripts/acceptance_wechat_ui.py` 通过：WebKit 390px 页面真实取得并显示二维码，验证另一设备扫码提示、取消、无页面横向溢出且没有误报绑定。真人仍未扫码。
- WebKit 双浏览器审批流程首次复验曾失败：测试 API `8202` 对项目提案接口返回 404。追查确认它连接的隔离数据库停在迁移 18，且该旧服务二进制不包含项目提案路由；并非审批 UI 无法工作。将同一隔离库补到迁移 `000019_agent_project_proposals`，以当前源码重启 API `8230` 后，`acceptance_browser_workflow.py` 全流程通过，实际完成邀请接受/撤销、成员角色、跨团队隔离、任务提案修改后批准、要求补充后重提并批准、正式计划更新、甘特筛选及会话撤销。
- `TEAMMIND_BROWSER=webkit TEAMMIND_WEB_BASE=http://127.0.0.1:5194 TEAMMIND_API_BASE=http://127.0.0.1:8202/api/v1 python3 scripts/acceptance_settings.py`：通过，覆盖资料、团队设置、跨浏览器邮箱确认、TOTP/MFA 启用/登录/关闭和恢复码、会话续期/撤销、个人数据导出、通知/提醒及 320–1280px 设置页面。该服务用于设置专项；完整审批流程使用上述迁移 19 当前源码 API `8230` 重跑通过。
- `TEAMMIND_WEB_BASE=http://127.0.0.1:5230 TEAMMIND_API_BASE=http://127.0.0.1:8230/api/v1 python3 scripts/acceptance_cross_browser.py`：当前源码 API 上 Firefox 桌面与 WebKit 手机视口仿真均通过；认证页和 11 个业务路由覆盖 320、360、375、390、430、768、1024、1280px。`TEAMMIND_BROWSER=webkit ... python3 scripts/acceptance_settings.py` 在该 API 上再次完整通过。
- `TEAMMIND_WEB_BASE=http://127.0.0.1:5230 TEAMMIND_API_BASE=http://127.0.0.1:8230/api/v1 python3 scripts/acceptance_agent_shell_ui.py`：Chromium 390px 通过；Shell 每次单独授权、执行后授权复位、结果另行授权才保存到本人工作区，并展示查询任务后提交待审批提案的顺序流程。模型响应由脚本固定，不证明真实模型已接通。
- `TEAMMIND_WEB_BASE=http://127.0.0.1:5230 TEAMMIND_API_BASE=http://127.0.0.1:8230/api/v1 python3 scripts/acceptance_account_deletion.py`：Chromium 手机视口通过，覆盖错误密码可恢复、确认注销、私人数据清理、会话立即失效和注销账号无法重新登录。
- `TEAMMIND_BROWSER=webkit TEAMMIND_WEB_BASE=http://127.0.0.1:5231 TEAMMIND_API_BASE=http://127.0.0.1:8231/api/v1 python3 scripts/acceptance_wechat_ui.py`：当前源码 API 实际调用 Node.js 24 Gateway 上固定的腾讯插件；390px 页面收到并显示真实二维码、可取消，未建立假绑定，无横向溢出和运行时错误。二维码未打印，也未由真人扫描。
- 已为下一步真人扫码保留隔离环境：网页 `http://127.0.0.1:5231`、当前源码 real-mode API `127.0.0.1:8231`、Node.js 24 Gateway 和微信 worker 均已启动；底层使用迁移 19 隔离数据库，无活动绑定。注册隔离测试账号、创建团队后可进入微信渠道页；真人扫码确认与后续消息往返仍需用户本人操作及外部微信网络。
- 已准备实体手机网页验收入口：`http://192.168.31.110:5232` 绑定本机局域网网卡；仅用于同一局域网验收，前端代理到回环 API `127.0.0.1:8232`，API CORS 精确允许该网页来源，后端仍使用隔离测试数据库。主机本地 HTTP/预检检查通过；实体手机实际可达性、Android Chrome/iOS Safari/微信内置浏览器交互仍待设备打开实测。该入口为临时开发服务器，不得当成生产部署。
- 同一 LAN 页面/API 来源上运行 `TEAMMIND_WEB_BASE=http://192.168.31.110:5232 TEAMMIND_API_BASE=http://127.0.0.1:8232/api/v1 python3 scripts/acceptance_cross_browser.py`：Firefox 与 WebKit 仿真全路由通过；这验证了指定 LAN 来源与 CORS 配置的浏览器路径，但浏览器进程仍运行在服务器主机，不能代替实体手机。
- `MYSQL_DSN=<teammind_goal_wechat_20261019a 隔离库> go test ./... -count=1`：全部通过，`internal/live` 用时 43.836 秒。`npm run type:check && npm run build` 通过；`cd backend/wechat-plugin-bridge && npm test` 4/4 通过。
- 当前运行环境 `AI_PROVIDER=mock`；真实模型服务地址、模型名和凭据未配置。SMTP 未配置；VAPID 已配置，但实际浏览器推送订阅失败且无实体设备，故真实 AI、真实外发通知仍未验收。`adb devices -l` 无实体设备，当前为 Linux，不能据此声称实体 Android Chrome、iOS Safari 或微信内置浏览器已验收。TeamMind 网页是交付主体；Capacitor/FCM/APNs 属可选原生扩展，不属于网页交付的前置功能。

## 2026-10-19 七项目标当前核验

| 目标 | 当前结论 | 仍缺的证据/工作 |
| --- | --- | --- |
| 腾讯 OpenClaw 微信插件及真人闭环 | 真人扫码确认、真实绑定持久化和 API/Gateway/worker 重启读回已验证；腾讯插件固定为 OpenClaw `2026.10.4` 与 `2.4.10` | 真实 AI 回复和微信外发未通过；当前服务为 `mock`，拒绝发送模拟回复。Android/iOS/微信内置浏览器实体设备仍未验 |
| 真实 AI | 适配器和 mock 模式存在；当前验收服务明确处于 `mock` | 真实服务地址、模型名、凭据；实测对话、记忆提取及模型自主选工具 |
| 智能体写入/执行/文件能力 | 任务、项目和非所有者成员身份变更均走待审批流程；成员提案角色/状态只在授权审批事务中更新，提案版本变化会过期；团队知识候选须管理员逐次授权并人工确认；沙箱和私人工作区已有隔离 | 商业模型实际工具选择待真实凭据；邀请/所有权转移不开放给智能体。任意宿主机 Shell、宿主机文件/网络和绕过审批写入不开放 |
| MFA、邮箱验证、登录限流 | 隔离 MySQL 专项测试与 WebKit 设置流程通过 | 生产密钥托管/轮换、SMTP 实际送达、反向代理信任和生产告警联动 |
| 个人数据导出、注销和留存 | 隔离 MySQL 集成测试与 WebKit 设置流程通过；共享记录匿名保留和过期记录清理已覆盖 | 团队共享记录期限须部署方审定；外部不可变审计归档和生产备份恢复仍待部署 |
| 移动端范围 | 核心产品是响应式网页；Capacitor/App 与 FCM/APNs 为可选外壳/通道，不是网页交付前置项 | 实体移动设备上的浏览器 Web Push 和网页行为仍需实测；只有选做原生 App 时才需 iOS/Android 发布及 FCM/APNs |
| 设置与审批浏览器验收 | WebKit 设置专项、双浏览器项目/任务审批、成员身份提案 Chromium/WebKit 手机浏览器流程通过；视口覆盖 320–1280px | 实体 Android Chrome、iOS Safari、微信内置浏览器仍未验收；本次环境未连接实体设备 |

本轮执行：WebKit `acceptance_browser_workflow.py` 与 `acceptance_settings.py` 均通过；迁移 19 隔离 MySQL 上 MFA、邮箱验证、IP 限流/告警、导出、注销、留存、任务/项目提案、团队知识候选、沙箱/私人工作区专项通过；`acceptance_agent_workspace_restart.py` 验证加密文件在 API 进程完全重启后仍可读；微信插件 `npm test` 4/4、真实插件 QR/等待/取消验收通过；`npm run type:check` 与 Python 语法检查通过。

## 2026-10-19 本轮七项目标逐项复验

- 在新建隔离数据库 `teammind_resume_20261019` 从空库执行 `scripts/migrate.sh teammind_resume_20261019`：迁移 `000001`–`000019` 全部成功。隔离库上的 `cd backend && go test ./... -count=1` 通过，`internal/live` 用时 44.311 秒。随后 `npm run type:check && npm run build` 通过。
- 在另一迁移 19 隔离库重跑账号与数据生命周期专项：MFA/TOTP、邮箱变更、来源限流/告警、个人数据导出、账号注销和留存清理 8 项测试全部通过。Agent 定向测试 8 项通过，覆盖任务/项目待审批提案、团队知识逐次授权与来源隔离、Python/Bash 受限沙箱、加密私人工作区 CRUD 与账号隔离。
- 在同一隔离环境执行 WebKit 设置专项与双浏览器业务审批流程：均通过。设置覆盖邮箱跨浏览器确认、MFA、会话、导出、通知和提醒；业务覆盖邀请、成员角色、跨团队隔离、项目与任务、审批补充/重提/批准、甘特和手机抽屉。均为实际浏览器自动化，不是路由打开检查。
- Android API 35 模拟器中的 Chrome 触摸回归通过：登录、资料保存并刷新、通知偏好保存并刷新、提醒创建/取消并刷新；412×765 CSS 像素下页面 `scrollWidth=412`，通知页无横向溢出。此项是模拟器结果，不是真机验收。
- 为 Web Push 生成临时 VAPID 密钥后，Android Chrome 的安全上下文、通知权限、Service Worker 与 PushManager 均正常，但 `pushManager.subscribe` 返回 `AbortError: Registration failed - push service error`；没有订阅写入 TeamMind。该模拟器环境未能连接浏览器推送服务，不能记为外部送达通过。SMTP 仍未配置。
- Firefox + WebKit 全路由在局域网页面 `http://192.168.31.110:5190` 重跑通过：认证页面和 11 个业务路由覆盖 320、360、375、390、430、768、1024、1280 像素。首次长序列在 Firefox 1024 像素成员页超时；单页重放正常，完整重跑通过。该结果仍是主机浏览器仿真。
- 腾讯桥接测试使用锁定所需的 Node.js `24.16.0` 重跑，4/4 通过。real-mode API 与 OpenClaw Gateway/单个微信 worker 使用同一隔离迁移 19 数据库及一致的回环地址/令牌。真实插件生成二维码、轮询和取消以及 390px WebKit 二维码展示均通过；未有人扫码，未建立真实绑定。
- 同一微信页面验收改由实际 LAN 来源 `http://192.168.31.110:5190` 打开并通过：浏览器经 Vite 代理访问 `127.0.0.1:8240`，页面收到真实腾讯 QR、显示为二维码图片、等待和取消正常；390px 无整页溢出、无运行时错误，取消后没有绑定。此检查仍运行在主机 WebKit 仿真，不代表实体手机扫码。
- 当前供真人扫码的临时入口为 `http://192.168.31.110:5190`，后端为 `127.0.0.1:8240`，Gateway 为 `127.0.0.1:18793`；仅用于本机局域网隔离验收，数据库名为 `teammind_resume_20261019`，AI 模式是 `mock`。请勿当成生产地址。真人扫码、绑定读回和服务重启恢复已在下方最新记录；真实 AI 回复和微信外发仍待验证。
- 旧七项清单中的账号安全和个人数据导出/删除/留存已由当前隔离 MySQL 和 WebKit 专项验证；“当前只用 Go 自行实现 iLink”“Agent 只有临时 Python、没有 Shell 或持久文件”“WebKit 审批未完成”均与当前源码或本轮实测不符。Agent 实际为有限白名单：任务/项目写入走人工审批，知识候选逐次授权，Python/Bash 运行在断网沙箱，私人文本工作区加密持久化；不开放任意宿主机 Shell、宿主机文件/网络或绕过审批的直接业务写入。
- 真人微信扫码、确认、持久绑定及真实入站已经在下方记录完成；剩余外部条件是配置真实模型服务并验收 AI 回复/工具选择/记忆提取、真实 SMTP 投递、浏览器 Web Push 服务订阅和送达，以及实体 Android/iOS/微信内置浏览器验收。TeamMind 以网页为核心，原生 App/FCM/APNs 是可选扩展。正式 TLS/KMS、备份恢复、生产监控告警及等保三级测评仍需部署与第三方证据。

## 2026-10-19 真人扫码确认与成员提案回归（数据库数量为后续清理前快照）

- 用户在局域网临时验收页完成人工确认：电脑打开 `http://192.168.31.110:5190`，微信在另一台设备扫码并确认。查询当前服务实际使用的隔离数据库 `teammind_resume_20261019`，确认有 4 个腾讯 `openclaw-weixin` 活动渠道账户、4 条有效绑定及 4 个隔离渠道会话。该证据替代本文件较早记录的“尚未真人扫码/没有有效绑定”状态；早期扫码页和二维码已过期，不再使用。
- 数据库里有真实入站消息，worker 同步状态持续更新；API、Gateway、worker 独立重启后绑定仍可读取。最新查询看到 4 条回复投递为失败，最多一条重试 20 次。当前 `AI_PROVIDER=mock`，worker 明确拒绝把模拟回复发送到微信，因此真实 AI 回复及微信外发**未通过**，不能宣称完整消息闭环。
- 为成员身份提案增加迁移 `000021_membership_versions`：成员关系保存单调递增版本，直接成员管理、重新接受邀请和提案审批均递增版本；提案审批比对版本，成员关系即使之后回到相同角色/状态，旧提案也转为 `stale`。
- `MYSQL_DSN=<teammind_resume_20261019 隔离库> go test ./internal/live -run TestMemberProposalApprovalBoundaryAndStaleState -count=1 -v`：通过，包括管理员不能任命管理员、所有者审批、重复/自批拒绝、状态过期及只改变成员版本就让提案过期。
- `TEAMMIND_WEB_BASE=http://192.168.31.110:5190 TEAMMIND_API_BASE=http://127.0.0.1:8240/api/v1 TEAMMIND_BROWSER=chromium python3 scripts/acceptance_member_proposals.py`：通过。真实 MySQL/API 创建团队、邀请、接受邀请、提交成员停用提案；Chromium 390px 页面审批后才写入正式成员状态，320–1280px 无横向溢出。
- 同一命令改用 `TEAMMIND_BROWSER=webkit`：通过相同成员提案流程和八种视口检查。脚本不固定 API 响应，浏览器保存操作实际到达 Go API 与隔离 MySQL。
- 空隔离库 `teammind_fresh_accept_20261019` 由 `scripts/migrate.sh` 从 `000001` 迁移至 `000021_membership_versions`；最新 API 对该库的 `/readyz` 返回 `ready`。
- `go test ./... -count=1`（迁移 21 的 `teammind_resume_20261019`）通过，`internal/live` 用时 47.182 秒；`npm run type:check && npm run build` 通过；`backend/wechat-plugin-bridge` 的 `npm test` 4/4 通过；`python3 -m py_compile scripts/acceptance_member_proposals.py` 通过。
- 本回合还用迁移 21 的 API 重新启动主要隔离环境及微信 worker：`127.0.0.1:8240/readyz` 返回 `ready`；数据库仍有 4 条有效微信绑定且 worker 同步时间在重启后继续前进。
- 成员提案 Chromium/WebKit 回归均通过。实体设备、真实模型、邮件/推送服务、生产运维与等保测评仍有各自外部依赖。

## 2026-10-19 用户确认扫码后的早期复核（测试夹具清理前快照）

- 收到用户完成另一台设备微信扫码和手机确认的反馈后，直接对当前隔离 MySQL 做聚合核验，没有读取消息正文或打印微信标识：4 个活动腾讯渠道账户、4 条有效绑定、4 条入站消息记录、4 个渠道会话映射均存在；最近 5 分钟内有同步游标更新。两个当前 API 实例都读到相同绑定和消息计数。
- 外发记录有 4 条失败。当前服务 `AI_PROVIDER=mock`，worker 拒绝把模拟回复发送给微信；真人扫码与真实入站已经通过，真实 AI 回复和微信外发仍未通过。此前已记录 API、Gateway、worker 重启后的绑定读回和 worker 恢复同步。
- 在 Android API 35 模拟器的 Chrome 124 中，使用触摸完成注册后的登录、资料保存/刷新、通知偏好保存/刷新、提醒创建/取消/刷新；基于生产构建的页面在 412×765 CSS px 下 `scrollWidth=412`，设置和通知页均无页面级横向溢出。Playwright 首次使用了不匹配 Chrome history 路由的哈希入口而在设置页断言失败；改用真实 history 路由后完整重跑通过。此项是 Android 模拟器浏览器验收，不是实体 Android/iOS/微信浏览器验收。
- 本轮 `npm run build` 通过。Android Chrome PushManager 曾返回 `AbortError: Registration failed - push service error`，未创建订阅；当前未配置 SMTP，故真实邮件和浏览器推送服务送达仍未验证。真实模型地址、模型名和服务端凭据也未配置，不能据此验真实模型工具选择或微信回复。
- 本轮重新创建空隔离库 `teammind_webkit_resume_20261019b` 并执行 `000001`–`000021` 迁移。`TEAMMIND_BROWSER=webkit TEAMMIND_WEB_BASE=http://127.0.0.1:5191 TEAMMIND_API_BASE=http://127.0.0.1:8242/api/v1 python3 scripts/acceptance_browser_workflow.py` 完整通过双浏览器设置、团队隔离、项目/任务、审批、甘特、邀请、通知及移动抽屉流程。首次测试发现脚本在会话续期后没有等待异步会话列表加载；单独核验两次登录产生两条有效会话后，将断言改为等待至少两行真实会话数据，再完整重跑通过。该更改只修复验收时序。
- 同一隔离库上的 `TEAMMIND_BROWSER=webkit TEAMMIND_WEB_BASE=http://127.0.0.1:5191 TEAMMIND_API_BASE=http://127.0.0.1:8242/api/v1 python3 scripts/acceptance_settings.py` 通过，覆盖资料/团队、异浏览器邮箱确认、TOTP/MFA 和恢复码、会话续期/撤销、个人数据导出、通知/提醒及 320–1280px 设置页面。
- 同一迁移 21 隔离库运行 `go test ./... -count=1` 通过（`internal/live` 59.625 秒）；`go test ./internal/live -run '^TestModelSelected' -count=1` 通过；`npm run type:check` 通过；腾讯微信桥接 `npm test` 4/4 通过；上述两个 Python 浏览器脚本 `py_compile` 通过。
- Agent UI 与持久工作区另行复核：`scripts/acceptance_agent_shell_ui.py` 通过 Shell 单次授权/执行后复位、输出另行授权保存、任务查询和待审批提案展示；此 UI 用确定性模型响应，不作为真实模型验收。`scripts/acceptance_agent_workspace_restart.py` 在隔离迁移 21 数据库通过，确认加密私人文件经完整 API 进程停止/重启后仍可读。首次误用已触发来源限流的共享库，注册阶段得到 429、未写入工作区；改用新的测试限流密钥和隔离数据库后通过。
- 曾尝试本地真实模型路径：Ollama 下载跳转至 404 的发行资产；`llama-cpp-python` 预编译包与 Qwen GGUF 可确认存在，但本机 GitHub 下载速率约 28 KB/s，13.5 MB 运行包下载数分钟仍未完成，约 0.94 GB 模型文件无法在本轮可靠下载。没有启动或验证本地模型，不把它记为 AI 通过；商业模型 API 地址/模型/密钥仍需配置。

## 2026-10-19 后续隔离库回归

- 新建空库 teammind_progress_20261019，迁移脚本从 000001 到 000021 全部执行；API 就绪检查返回 ready。旧共享验收服务触发来源限流时没有放宽规则，后续流程改用该空库完成。
- 新增微信账号竞争、解绑和重绑的 MySQL 集成用例：验证第二个账号不能抢占有效绑定；并发首次绑定只有一个胜者；绑定者解绑会写审计；解绑后另一账号可以重绑；旧账号不能解绑或抢回新绑定。用例重复运行 5 次和 Go race detector 检查均通过。
- 同一迁移 21 隔离库执行全量 Go 测试通过，internal/live 用时 63.555 秒；前端类型检查与生产构建通过；腾讯微信桥接测试 4/4 通过。
- WebKit 双浏览器工作流通过，覆盖团队隔离、项目/任务、邀请、审批与重提、甘特、通知和手机抽屉。完整 WebKit 设置流程通过，覆盖跨浏览器邮箱确认、TOTP/MFA/恢复码、会话续期/撤销、个人数据导出、通知/提醒及 320–1280px 设置视口。首次运行的临时 API 缺少 MFA_ENCRYPTION_KEY，测试账号按脚本逻辑清理后中止；注入隔离测试密钥后完整重跑通过。
- Android API 35 模拟器上的 Chrome 124 触摸流程通过：登录、资料保存并刷新、通知偏好保存并刷新、提醒创建/取消并刷新。412×765 CSS 像素下设置与通知页面 scrollWidth 均为 412，无页面横向溢出。此项是模拟器验收，不是实体 Android、iOS Safari 或微信内置浏览器验收。
- 仍待真实模型端点/模型名/服务端凭据以验证模型自主选工具、记忆提取与微信回复；SMTP 凭据以验证邮件送达；HTTPS 网页和活动推送订阅以验证 Web Push 送达。生产沙箱逃逸评估、TLS/KMS/备份/监控和等保测评也未完成。
- 同一隔离环境的全站引擎回归通过：Firefox 桌面与 WebKit 手机仿真覆盖登录、注册、找回/重置页面及 11 个 TeamMind 业务路由；320、360、375、390、430、768、1024、1280 CSS 像素均无页面横向溢出且没有浏览器运行时错误。这仍不代表实体 iOS/Android 或微信内置浏览器验收。

## 2026-10-19 Provider 到微信桥接闭环集成测试

- 在隔离迁移 21 MySQL 上运行 `go test ./internal/live -run '^TestWechatWorkerRejectsMockAndDeliversConfiguredProviderReply$' -count=1 -v`：通过。测试先断言 `AI_PROVIDER=mock` 时不会向渠道桥接发送消息，并持久化可重试失败；随后使用本地 OpenAI 兼容测试端点验证服务端配置的模型 Provider 请求、助手回复数据库记录及模型元数据、腾讯桥接认证与文本投递，以及投递记录转为 `sent`。
- 运行 `MYSQL_DSN=<隔离测试库> go test ./... -count=1`：通过；`npm run type:check && npm run build`：通过。
- 上述测试使用本地确定性模型端点和模拟桥接 HTTP 服务，只证明 Provider 接口、MySQL 持久化和 Worker 桥接协议的集成行为。用户已完成真人微信扫码/确认，真实入站已记录；本测试不等同于真实模型服务调用，也不证明真实微信外发。`AI_PROVIDER=mock` 下的真实外发仍按安全设计被拒绝。

## 2026-10-19 七项遗留状态复核（当前权威结果）

1. **微信插件与真实账号**：固定版本 OpenClaw `2026.10.4` / 腾讯插件 `2.4.10` 正在运行；本次 TeamMind API 二维码/等待/取消自动化通过。真人扫码和绑定此前已由用户确认。清理 7 个测试夹具后，当前隔离 MySQL 有 1 个有效绑定、1 条真实入站记录和 1 个会话映射，Worker 仍在同步；唯一投递失败发生在 mock 模式。早前 4/8 绑定与本地测试模型的 `sent` 统计包含测试夹具，已从数据库和真人验收口径中移除。真实模型回复仍待真实服务配置。
2. **真实 AI**：当前服务没有 `AI_BASE_URL`、`AI_MODEL`、`AI_API_KEY`，因此真实对话、自动记忆提取和真实模型自主选工具未验。OpenAI 兼容接口到持久化回复和模拟桥接的集成测试通过，不代表真实模型上线。
3. **智能体能力**：隔离 MySQL 测试通过任务/项目/成员提案、管理员审批边界、逐次授权团队知识候选、断网 Python/Bash 沙箱及加密私人工作区。直接无审批业务写入、宿主机 Shell/文件/网络访问保持关闭，这是既定安全约束，不作为缺陷。
4. **账号安全**：当前代码的 MFA/TOTP、邮箱变更、来源限流和超管脱敏告警专项测试通过。生产 MFA 密钥托管/轮换与邮件投递仍待部署配置。
5. **数据生命周期**：个人数据导出、对话删除、账号注销和留存清理的隔离 MySQL 测试通过；共享业务数据的正式留存期限、外部不可变审计归档和生产恢复演练仍需组织级决策和环境证据。
6. **移动产品范围**：TeamMind 按用户要求以响应式网页验收；原生 App 与 APNs/FCM 不属于本次交付门槛。Web Push 是网页功能：VAPID 与订阅加密密钥格式有效，但联系地址为保留的 `.invalid` 域名，修复后 API 报告 `pushConfigured=false`、不返回公钥；活动订阅为 0。Android Chrome 模拟器此前的订阅返回 `AbortError`，访问 `fcm.googleapis.com:443` 超时。SMTP 未配置，真实外发未验。
7. **设备/浏览器回归**：本次 WebKit 完整设置专项、双浏览器审批工作流、Android API 35 模拟器 Chrome 124 触摸流程通过。Firefox/WebKit 多视口记录仍有效；实体 Android、iOS Safari 和微信内置浏览器没有设备证据。

本次直接执行的证据：`acceptance_wechat_plugin.py` 通过；`acceptance_settings.py`（WebKit）通过；`acceptance_browser_workflow.py`（WebKit 双浏览器）通过；`acceptance_android_chrome.py` 通过（412×765、无横向溢出）；账号安全/数据生命周期 16 项 Go 定向测试通过（40.104 秒）；Agent 项目/知识/Shell 5 项 Go 测试通过；修复测试清理顺序后的 `go test ./... -count=1` 全量通过（`internal/live` 56.887 秒）；微信数据库只读聚合检查确认当前 1 个有效绑定/入站会话，未读取或打印消息正文、渠道身份或令牌。此次还移除了 7 个由原清理缺陷遗留的测试账号/绑定；修复后的定向用例和全量测试均通过。

## 2026-10-19 用户确认扫码后的当前复核

- 用户确认已完成微信扫码和手机确认。对运行中的 API `127.0.0.1:8240` 所连接的隔离 MySQL 做只读聚合核验：1 个活动渠道账户、1 条有效绑定、1 条入站记录、1 个渠道会话映射；该会话有 1 条用户消息、没有助手回复。Worker 同步游标继续更新（最近复核为 `2026-10-19 12:10:52 UTC`）。查询未读取消息正文、微信身份或渠道凭据。
- 当前 `AI_PROVIDER=mock`，投递记录为 1 条失败、0 条已发送；失败是 Worker 按设计拒绝模拟回复。真人绑定和入站已通过，真实 AI 回复与微信外发仍未通过。服务环境没有 `AI_BASE_URL`、`AI_MODEL` 或 `AI_API_KEY`。
- 将 Android Chrome 验收脚本接到当前 API `8240` 后重新执行：`TEAMMIND_WEB_BASE=http://127.0.0.1:5191 TEAMMIND_API_BASE=http://127.0.0.1:8240/api/v1 ANDROID_CHROME_CDP=http://127.0.0.1:9222 python3 scripts/acceptance_android_chrome.py`，通过触摸登录、资料持久化、通知偏好刷新回读、提醒创建/取消；视口为 412×765 CSS px（DPR 2.625），设置与通知页面均无横向溢出。首次运行发现脚本在页面标题出现后、偏好异步加载前就断言；脚本已改为等待服务端值读回，修正后重跑通过，不是产品保存故障。
- 此项是 Android API 35 模拟器 Chrome，不是实体手机。实体 Android、iOS Safari 和微信内置浏览器仍需设备验收；真实模型和 SMTP 仍需凭据；Web Push 已配置 VAPID，但 Android 模拟器无法连接推送服务；生产安全运维仍需部署环境。

## 2026-10-19 七项目标复验结果

| 项目 | 当前结论 | 本轮证据与剩余事项 |
| --- | --- | --- |
| 微信腾讯插件闭环 | **扫码、绑定、入站和三进程重启恢复通过；AI 回复外发未通过** | 固定 OpenClaw `2026.10.4` / 腾讯 `@tencent-weixin/openclaw-weixin@2.4.10`；用户已确认真人扫码。当前隔离 MySQL 有 1 个活动绑定、1 条入站消息和 1 个渠道会话；该会话有 1 条用户消息、0 条助手消息。依次重启 API、微信 Worker 和 Gateway 后，绑定仍在、Worker 解密凭据并继续同步。最新游标为 `2026-10-19 12:43:08 UTC`。当前 AI 为模拟模式，未产生真实外发。 |
| 真实 AI | **未验证** | API/Worker 使用 `AI_PROVIDER=mock`；`AI_BASE_URL`、`AI_MODEL`、`AI_API_KEY` 均未设置，本机也没有可用 Ollama/llama.cpp 服务或已下载模型。真实对话、记忆提取及商业模型自主选工具都未验。 |
| Agent 写入与执行 | **受控能力已有实现和测试；不开放无界宿主机操作** | 新建迁移 21 隔离库执行 `cd backend && go test ./... -count=1` 全部通过（`internal/live` 59.270 秒），覆盖只读查询、经审批的任务/项目/成员提案、逐次授权团队知识候选、断网 Python/Bash 沙箱和加密私人工作区。真实模型选择工具仍待凭据；任意宿主机命令、文件和网络访问不是产品开放能力。 |
| 账号安全 | **功能与隔离数据库/浏览器回归通过** | 同一 Go 全量测试包含 MFA/TOTP、邮箱变更、登录来源限流与平台告警；WebKit 设置流程通过 MFA 启用/登录/恢复码/关闭和跨浏览器邮箱确认。生产密钥托管、轮换和外部告警联动待部署。 |
| 个人数据生命周期 | **功能与隔离数据库/浏览器回归通过** | Go 全量测试覆盖个人数据导出、注销删除和留存清理；WebKit 完成数据副本下载。法定留存期限、生产备份恢复和外部不可变审计归档仍需部署/组织证据。 |
| 原生移动应用与网页推送 | **原生应用属可选范围；网页推送真实送达未通过** | 用户此前确认 TeamMind 是网页产品；原生应用发布和 APNs/FCM 不列为网页验收门槛。网页推送的 VAPID 与订阅加密密钥格式有效，但联系地址是保留域名；修复后 API 返回 `pushConfigured=false` 且不下发公钥，活动订阅为 0。Android Chrome 模拟器的 `PushManager.subscribe()` 返回 `AbortError`，到 `fcm.googleapis.com:443` 的连接超时。 |
| 设置/移动浏览器 | **设置流程和 WebKit 审批工作流通过；实体设备仍未验** | WebKit 设置流程及双浏览器业务审批流程在新隔离迁移 21 数据库通过。Android API 35 模拟器 Chrome 通过触摸登录、资料/通知偏好保存和刷新、提醒创建/取消，412×765 CSS px 无横向溢出。实体 Android Chrome、iOS Safari 和微信内置浏览器没有设备证据。 |

本轮另执行：腾讯插件桥接 `npm test` 4/4 通过；`npm run type:check && npm run build` 通过；API、Worker、Gateway 重启后的持久绑定与同步检查通过；真实插件扫码起始流程在独立隔离库通过，没有打印二维码或创建测试绑定。HTTP 开发环境仅用于验收，不能作为生产部署或等保三级通过证明。


## 2026-10-19 VAPID 配置状态修正

- 根因：服务端此前只判断 `VAPID_SUBJECT` 是否以 `mailto:` 或 `https://` 开头，导致 `mailto:admin@example.invalid` 被误报为已配置。
- 修复：新增联系人格式和域名校验，拒绝示例/保留域、本机域名和 IP 地址；状态接口在无效时同时返回 `pushConfigured=false` 和空 `pushPublicKey`，订阅写入与发送也使用同一校验。环境模板将该配置留空。校验只检查格式与已知占位域，不验证邮箱投递能力或 HTTPS 联系页面所有权。
- 验证：新增 VAPID 联系地址、配置状态和状态接口测试；使用运行中 API 的迁移数据库执行状态接口测试通过。通过 `/api/v1/me/notification-channels` 核验真实响应为 `pushConfigured=false`、公钥为空、活动订阅 0。390px 手机浏览器页面显示“浏览器推送未配置”，启用和撤销按钮均禁用。临时验收账号已通过账号注销接口删除。
- `cd backend && go test ./... -count=1` 通过；`npm run type:check && npm run build` 通过。邮件投递、浏览器推送订阅和真实送达仍未验，SMTP 未配置且模拟器到 Google 推送服务超时。


## 2026-10-19 七项能力的本轮验收

| 项目 | 当前状态 | 本轮证据与剩余项 |
| --- | --- | --- |
| 微信与腾讯 OpenClaw 插件 | **真人扫码、确认、绑定、入站、服务重启恢复已验证；AI 回复外发未验证** | 当前真实隔离库脱敏聚合为 1 个活动插件账户、1 条有效绑定、1 条入站记录、1 个会话映射；会话中 1 条用户消息、0 条助手消息。按 Gateway → worker → API 重启并用新 API 再跑插件二维码/等待/取消；绑定和 worker 同步恢复。固定 OpenClaw `2026.10.4`、腾讯插件 `2.4.10`；桥接测试 4/4 通过。AI 为 mock，真实出站回复仍须模型配置。
| 真实 AI | **未验证** | 运行 API 使用 `AI_PROVIDER=mock`，无服务地址、模型名或 API 密钥；本机没有可用 Ollama/llama.cpp 服务或模型文件。真实对话、记忆提取和真实模型自主选择工具仍待凭据。
| 智能体 | **受限工具、审批提案和私人工作区已有实现与测试** | 新空库迁移 21 后 `go test ./... -count=1` 全量通过（48.699 秒）；覆盖任务/项目/成员待审批提案、逐次授权团队知识候选、断网 Python/Bash 和加密私人工作区。浏览器实测私人文件创建、编辑、失败重试、刷新持久化、导出和删除通过。任意宿主机命令、任意业务直写刻意不开放；生产沙箱仍需独立评估。
| MFA、邮箱和限流 | **功能与迁移 21 集成/WebKit 回归通过** | 全量 Go 集成测试覆盖 TOTP、邮箱单次验证、来源/IP 限流及脱敏告警；WebKit 设置流程实测 MFA 启用/登录/恢复码/关闭、邮箱跨浏览器确认、会话管理。生产密钥托管、SMTP 投递、外部值班告警需部署配置。
| 个人数据生命周期 | **导出、注销、留存有实现且隔离数据库/浏览器测试通过** | 全量 Go 测试覆盖个人数据导出、注销清理和留存窗口；WebKit 完成数据导出；Chromium 390px 实测错密码恢复、确认注销、会话撤销和注销后登录拒绝。团队共享记录法定保留期限、备份恢复和外部不可变审计归档仍待部署/合规决定。
| 原生移动应用 | **不是网页交付门槛** | 用户已明确产品是网页。仓库有可选 Capacitor WebView 外壳；不把 iOS/Android 原生界面或 APNs/FCM 当作网页缺口。网页 Web Push 是当前范围内的移动通知能力，但真实送达仍未验。
| 设置、浏览器和移动视口 | **WebKit/Firefox 全路由、设置与审批流程及 Android 模拟器回归通过；实体设备未验** | Firefox 桌面和 WebKit 移动仿真覆盖登录/注册/找回及 11 个业务路由，在 320/360/375/390/430/768/1024/1280px 通过；WebKit 双浏览器业务审批和设置专项通过。Android API 35 模拟器 Chrome 触摸登录、资料/通知偏好持久化、提醒创建/取消通过，412×765 CSS px 无横向溢出。实体 Android Chrome、iOS Safari 和微信内置浏览器仍需实际设备。

额外命令与结果：

- `cd backend && go test ./... -count=1`（隔离迁移 21 数据库）：通过，48.699 秒。
- `PATH=/tmp/teammind-node24/node_modules/node/bin:$PATH npm test`（微信桥接）：4/4 通过。
- `TEAMMIND_BROWSER=webkit ... python3 scripts/acceptance_browser_workflow.py`：WebKit 双浏览器审批全流程通过。
- `TEAMMIND_BROWSER=webkit ... python3 scripts/acceptance_settings.py`：WebKit 设置、MFA、导出、通知/提醒和八种视口通过。
- `... python3 scripts/acceptance_cross_browser.py`：静态生产构建下 Firefox 与 WebKit 全路由/八视口通过；此前 Vite 开发服务器在 Firefox 记忆页发生单次 30 秒导航超时，改由静态构建重跑后通过。
- `... python3 scripts/acceptance_android_chrome.py`：Android API 35 模拟器 Chrome 触摸设置流程通过。
- `... python3 scripts/acceptance_agent_workspace.py`、`... python3 scripts/acceptance_account_deletion.py`：私人工作区持久化/导出/删除及手机布局账号注销通过。

真实 SMTP 邮件、Web Push 订阅/送达、真实模型回复、实体 Android/iOS/微信内置浏览器、生产密钥/备份/告警及正式等保三级测评仍未通过；代码和模拟测试不构成这些外部验收。

## 2026-10-19 真人扫码确认后的最终状态

- 用户已确认使用真实微信扫码并在手机上确认。随后对主验收服务做只读健康检查：`http://127.0.0.1:8240/healthz` 与 `/readyz` 均返回 200；数据库当前为 `teammind_resume_20261019`。
- 只查询计数和同步时间，没有读取消息正文、微信身份标识或凭据：1 个活动渠道账户、1 条有效绑定、1 条真实入站、1 个渠道会话、1 条用户消息、0 条助手消息；发送记录为 1 条失败、0 条成功。同步游标最近更新时间为 `2026-10-19 13:39:40 UTC`。
- 当前 API/Worker 的 `AI_PROVIDER=mock`，模型地址、模型名和密钥未设置。失败记录是 Worker 拒绝把模拟回复发送到真实微信；真人绑定与入站通过，真实模型回复和微信外发仍未验证。
- 此次只读核对后清理了本轮临时 API、静态代理、Android 模拟器，以及临时库 `teammind_audit_1019b` 和 `teammind_agent_audit_1019b`。主库 `teammind_resume_20261019`、API、微信 Worker、OpenClaw Gateway、MySQL 和网页开发服务器保留；主 API `/healthz`、`/readyz` 在清理后仍为 200。
- 当前仍需外部条件的网页功能：真实 AI 对话/工具选择及微信回复需要有效模型服务配置；邮件通知需要 SMTP 与公开站点地址；Web Push 需要真实 VAPID 联系方式、HTTPS、成功订阅及推送服务网络可达。实体 Android、iOS Safari、微信内置浏览器逐项回归，生产密钥托管、备份恢复、外部告警/审计归档和等保三级正式测评也没有完成证据。用户在手机上完成本次微信确认不等于这些设备兼容性和生产运维验收。

## 2026-10-19 分项复验与顺序重启结果

- **微信插件**：主 API 上重新运行 `TEAMMIND_API_BASE=http://127.0.0.1:8240/api/v1 python3 scripts/acceptance_wechat_plugin.py`，真实模式二维码创建、腾讯插件二维码内容、状态轮询、取消与取消状态读取全部通过；没有输出二维码，也没有把测试扫码会话写成绑定。桥接 `npm test` 4/4 通过。
- **真人闭环状态**：用户先前确认真人扫码和手机确认。随后按 Gateway → Worker → API 顺序重启服务；重启后 `/healthz`、`/readyz` 均为 200，Worker 同步游标继续前进至 `2026-10-19 14:04:59 UTC`。隔离主库只读聚合仍为 1 个活动账户、1 条有效绑定、1 条入站记录、1 个会话、1 条用户消息、0 条助手消息、1 条失败投递、0 条成功投递。没有读取正文、微信标识或凭据。失败是 mock Provider 被正确拒绝外发。
- **WebKit 审批工作流**：`TEAMMIND_BROWSER=webkit TEAMMIND_WEB_BASE=http://127.0.0.1:5190 TEAMMIND_API_BASE=http://127.0.0.1:8240/api/v1 python3 scripts/acceptance_browser_workflow.py` 通过双浏览器会话、团队隔离、项目/任务 CRUD 与提案审批/重提、成员角色、邀请接受/撤销、通知读写、依赖/Gantt、手机抽屉和令牌撤销。该流程使用主验收 API；流程创建的本轮测试账号、团队及外键关联记录已按唯一测试标记清理并核对为 0。
- **设置控件与隔离库**：新建空库执行 `scripts/migrate.sh teammind_goal_settings_20261019`，迁移 `000001`–`000021` 成功；在独立 API/Vite 上运行 `TEAMMIND_BROWSER=webkit ... python3 scripts/acceptance_settings.py` 通过，覆盖资料、团队、跨浏览器邮箱确认、TOTP/MFA 启用/登录/恢复码/关闭、会话续期/撤销、个人数据导出、通知偏好、提醒及 320/360/375/390/430/768/1024/1280px 页面宽度。测试 API/Vite 和隔离库随后已停止/删除。
- **代码回归**：在新迁移 21 隔离库运行 `cd backend && go test ./... -count=1`，全部通过（`internal/live` 43.579 秒）；`npm run type:check && npm run build` 通过；腾讯桥接测试 4/4 通过。两座临时回归库均已删除。
- **仍未完成**：真实 AI 对话/模型自主工具选择/微信回复外发仍需要真实模型凭据；SMTP 邮件与 Web Push 送达需有效配置和外网条件；实体 Android、iOS Safari、微信内置浏览器的全站验收仍无实机证据；生产密钥/KMS、恢复演练、外部告警/审计归档、主机沙箱评估和等保三级正式测评仍待部署环境。原生 App/APNs/FCM 不属于已确认的网页交付范围。

## 2026-10-19 用户确认微信扫码后的登录与工作区续验

- 用户已确认在微信手机端完成扫码和确认。对保留的主 API `127.0.0.1:8240`、Gateway、Worker 和数据库做只读复核：API 健康/就绪均返回 200；聚合计数为 1 个活动渠道账户、1 条有效绑定、1 条入站记录、1 个会话映射、1 条用户消息、0 条助手消息、1 条失败投递、0 条成功投递；同步状态仍在更新，最后核对时间为 `2026-10-19T14:47:35.719Z`。查询未读取消息正文、渠道身份或凭据。失败是 `AI_PROVIDER=mock` 下按设计阻止模拟回复外发；不宣称真实 AI 微信回复已通。
- 登录按钮再次在鼠标未悬停时检查：`display:flex`、`visibility:visible`、`opacity:1`、启用，尺寸 233×44 CSS px，白字/蓝底对比度 5.19:1。空表单显示邮箱和密码字段错误；错误密码显示服务端反馈；登录接口人为延迟期间按钮进入禁用/loading 并在响应后恢复。真实坐标鼠标中心、左边缘和右边缘事件、键盘 Enter、键盘聚焦后的 Space、390px WebKit 触屏均通过登录和路由跳转断言。测试账号通过真实注销接口清除。
- 浏览器工具限制已定位：Chromium headless shell 在 `about:blank` 和 TeamMind 页面都不派发动画帧；Playwright `locator.click()` 的稳定检查以及标准截图的字体等待因此超时。Firefox/WebKit 的动画帧与稳定点击检查正常；Chromium 真实坐标点击确实触发同一表单和真实 API。没有用强制点击绕过限制；登录状态用自动化断言覆盖，未将浏览器截图等待超时误记为产品故障。
- `scripts/acceptance_agent_workspace.py` 现在可通过 `TEAMMIND_BROWSER=webkit` 或 `firefox` 选择引擎，并将页面 API 请求显式路由到给定隔离 API。新建 MySQL 空库执行迁移 `000001`–`000021` 并确认 `/readyz` 后，WebKit 390px 视口流程通过：私人文件创建、服务端校验失败后表单内容保留、编辑、刷新持久化回读、数据副本导出、确认删除和 320px 横向溢出检查；测试账号经注销接口删除。临时 API 与测试库已停止并删除。
- 新增可重复运行的 `scripts/acceptance_login.py`：在迁移 21 的隔离库和 WebKit 上执行未悬停、悬停、焦点、加载、失败、成功断言，校验对比度、键盘 Enter/Space、触屏以及鼠标中心/左右边缘真实坐标事件。六种状态都通过真实 API 登录验证；脚本在结束时注销并清理测试账号。相同环境命令为 `TEAMMIND_WEB_BASE=http://127.0.0.1:5190 TEAMMIND_API_BASE=http://127.0.0.1:8253/api/v1 TEAMMIND_BROWSER=webkit python3 scripts/acceptance_login.py`；临时 API 与数据库随后已关闭并删除。

## 2026-10-19 WebKit 审批与设置控件复验

- 在空 MySQL 库 `teammind_goal_webkit_20261019b` 执行 `000001`–`000021`，启动隔离 API `127.0.0.1:8254` 与 Vite `127.0.0.1:5194` 后运行 `TEAMMIND_BROWSER=webkit ... python3 scripts/acceptance_browser_workflow.py`：**通过**。两浏览器完整流程覆盖主题、账号菜单、搜索/通知入口、资料和团队设置、多团队切换及延迟响应隔离、项目 CRUD 和审批提案、任务/协作者、成员角色、邀请接受/撤销、通知和免打扰、Gantt 日期定位、手机抽屉/触控、会话撤销。
- 在另一座空迁移 21 库 `teammind_goal_settings_step7_20261019` 上运行 `TEAMMIND_BROWSER=webkit ... python3 scripts/acceptance_settings.py`：**通过**。覆盖个人数据导出、资料/团队时区设置、跨浏览器邮箱变更确认、TOTP 启用/登录/恢复码下载/关闭、会话续期和撤销、通知偏好、提醒，以及 320–1280px 的设置/通知/MFA 页面检查。
- 本轮两座隔离库、临时 API 和 Vite 均已停止并删除；主 API `/healthz`、`/readyz` 仍为 200。该结果解决了过往 WebKit 审批浏览器进程退出的未完成记录，但仍是 Linux WebKit 仿真；实体 Android、iOS Safari 和微信内置浏览器未验。
- 真人微信当前只读状态仍为 1 个活动腾讯插件账户、1 条有效绑定、1 条入站、1 个渠道映射、1 条用户消息、0 条助手消息、0 条成功/1 条失败投递；Worker 同步游标更新至 `2026-10-19T15:05:07.294Z`。失败源于 `AI_PROVIDER=mock` 的安全拒绝；真实模型回复和微信外发尚未验证。

## 2026-10-19 微信确认及七项缺口复验

- 用户本轮回复确认已用微信扫码并在手机确认。主验收 MySQL 的脱敏聚合为：1 个活动腾讯插件账号、1 条有效绑定、1 个映射会话、1 条真实入站用户消息、0 条助手消息、1 条失败投递、0 条成功投递；Worker 同步游标为 `2026-10-19T15:08:44.872Z`。未读取消息正文、微信身份或令牌。绑定和入站记录时间早于本次核验，因此它们证明系统已有真人绑定/入站，不单独证明本轮操作新增绑定。失败原因是模拟 AI 被安全拒绝外发。
- 运行代码使用固定 OpenClaw `2026.10.4` 与腾讯 `@tencent-weixin/openclaw-weixin@2.4.10` 插件；受令牌保护的 loopback Gateway 扩展调用插件扫码和消息 API。此前“完全由 Go 直连 iLink、未运行腾讯插件”的描述不符合当前实现。二维码桥接 Node 测试 4/4 通过。
- 新建隔离数据库 `teammind_goal_verify_20261019c` 后，迁移 `000001`–`000021` 全部通过；`cd backend && go test ./... -count=1` 全量通过（`internal/live` 46.055 秒）；`npm run type:check && npm run build` 通过。桥接 `npm test` 4/4 通过。验收数据库在测试后已删除。
- 随后在另一座空迁移 21 数据库、临时 API `127.0.0.1:8255` 和 Vite `127.0.0.1:5195` 上重跑 `TEAMMIND_BROWSER=webkit ... python3 scripts/acceptance_browser_workflow.py` 与 `... python3 scripts/acceptance_settings.py`：**均通过**。审批脚本覆盖双浏览器团队隔离、项目/任务与提案审批和重提、成员、邀请、通知、依赖/Gantt、手机抽屉、只读智能体工具及会话撤销；设置脚本覆盖邮箱跨浏览器确认、MFA/TOTP/恢复码、会话、数据导出、通知/提醒和 320–1280px 八种宽度。临时库删除前只剩本轮专用账号 2 个、团队 1 个；删除整座隔离库后已一并清除。API/Vite 也已停止。
- 本机 `.env` 没有真实 AI Provider 参数、SMTP 账号、VAPID 联系地址/私钥或通知加密密钥；无 Android 设备连接，运行环境为 Linux。API、微信 Worker、Gateway 和 Vite 在本轮末尾已停止，MySQL 容器仍在运行；因此未声称已从本轮确认生成新微信事件，也没有声称真实模型回复、邮件、Web Push 或实机兼容验证通过。
- 当前仍需外部条件：真实模型服务的 URL、模型名和服务端密钥；SMTP 服务器及账号；公开 HTTPS 域名、真实 VAPID 联系地址/密钥和推送网络；实体 Android、iOS Safari、微信内置浏览器；生产密钥管理、备份恢复、外部告警/不可变审计归档、目标主机沙箱评估和正式等保三级测评。原生 App/APNs/FCM 不属于网页交付要求。
