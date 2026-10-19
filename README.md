# TeamMind

TeamMind 使用获得授权的 Arco Design Pro Vue Vite 2.7.3 布局、Go 接口服务和 MySQL 8。前端通过 `/api/v1` 调用接口，正式业务数据存储在 MySQL。Arco 模板原始许可证保留于 `LICENSE.arco-design-pro-vue`。本项目仍在开发中，**这不代表已完成生产部署或通过等保三级测评**。请参阅[架构与实施交接文档](docs/IMPLEMENTATION_ARCHITECTURE_HANDOFF.md)、[交付报告](docs/DELIVERY_REPORT.md)、[可见功能审计](docs/FUNCTION_AUDIT.md)和[验收记录与缺口](docs/ACCEPTANCE.md)。

## 使用空数据库本地启动

需要 Docker Compose、Go、Node.js 和 npm；运行 OpenClaw Gateway 2026.10.4 需要 Node.js 24.16–24.x 或 26.1 及以上，腾讯微信插件本身要求 Node.js ≥22.13。将 `.env.example` 复制为新的 `.env`，并替换 MySQL 密码。请妥善保管 `.env`。MySQL 数据卷会在进程重启后保留数据。

当前工作区已有的 `.env` 指向较早建立、没有迁移版本记录的 `teammind` 数据库。不要对它运行默认迁移命令；请另选一个数据库（例如 `teammind_local`），运行 `scripts/migrate.sh teammind_local`，并只将私有 `.env` 中 `MYSQL_DSN` 的数据库名部分改为 `/teammind_local`，然后再启动接口服务。若使用全新的 MySQL 数据卷，可使用下方命令中的默认数据库名 `teammind`。

```sh
cp .env.example .env
# 编辑 .env：设置 MYSQL_ROOT_PASSWORD、MYSQL_PASSWORD，并使 MYSQL_DSN 与之匹配。
docker compose up -d mysql
scripts/migrate.sh
cd backend
set -a; source ../.env; set +a
go run ./cmd/server
```

在另一个终端启动前端：

```sh
npm ci
npm run dev
```

打开终端显示的 Vite 地址，通常是 `http://127.0.0.1:5174`。`config/vite.config.dev.ts` 默认将 `/api/v1` 代理到 `127.0.0.1:8188`；运行隔离验收时可用 `TEAMMIND_API_PROXY=http://127.0.0.1:<端口> npm run dev -- --port <前端端口>` 指向隔离 API。Go 服务无法连接 MySQL 时会拒绝启动。`/healthz` 检查进程状态；`/readyz` 检查数据库和迁移标记。所有 SQL 连接均使用 UTC；界面按用户时区显示日期，默认时区为 `Asia/Shanghai`。迁移脚本会在 `schema_migrations` 中记录已应用的二十一个版本，并跳过已经完成的迁移；接口服务启动时要求存在迁移 `000021_membership_versions`。邮箱变更须输入当前密码并验证发送到新邮箱的短期单次链接；令牌只以哈希存储，确认后旧会话立即撤销。开发模式会在设置页显示本地验收链接；生产模式需配置 SMTP 和 `PUBLIC_URL`。设置页提供基于 TOTP 的多因素认证；必须配置 `MFA_ENCRYPTION_KEY`，密钥经 AES-GCM 加密后保存在 MySQL，动态验证码按已接受时间步去重，登录时须完成动态验证码或单次恢复码验证。恢复码只在启用时显示一次。生成 MFA 密钥可运行 `openssl rand -base64 32`；密钥应由服务端密钥设施保管，丢失时用户需重置 MFA。智能体私人工作区使用 `AGENT_WORKSPACE_ENCRYPTION_KEY`（32 字节 Base64 密钥）对用户文本文件做 AES-GCM 加密后存入 MySQL；最多 12 个文件、总计 256 KiB，支持从设备保存、选择性提供给单次授权的断网 Python/Bash 沙箱，以及显式授权后持久保存沙箱输出。工作区属于账号私人数据，会随个人数据导出，并在注销时删除；必须持久保管该密钥，否则已有文件无法解密。个人记忆候选会保留来源消息，要求用户确认，支持复核日期和过期日期，并拒绝重复内容。站内通知及邮件/浏览器/原生推送的幂等投递记录在事务中写入。用户可通过 `/me/reminders` 创建、查看和取消个人提醒或当前团队提醒。API 进程会运行 MySQL `FOR UPDATE SKIP LOCKED` 提醒及通知后台任务；独立投递模式可运行 `go run ./cmd/server notification-worker`。工作进程使用有限重试和退避；邮件、浏览器及原生推送须按下方说明配置，不能仅凭代码部署视为已经连通。若应用表已存在但没有迁移历史记录，迁移脚本会拒绝重放 DDL；请从已验证的备份恢复或先完成数据库核对。MySQL DDL 不支持事务；迁移中途失败时应从备份恢复。

回滚一个迁移前，先创建并验证数据库备份，然后运行 `scripts/rollback.sh <明确的数据库名>`。脚本只回滚最近一次已记录的版本，执行对应的 `.down.sql`，并删除该版本标记。DDL 不支持事务；如果回滚中途失败，请从备份恢复。用于应用数据库前，先在一次性数据库中演练回滚。

请在注册页面创建普通账号。生产环境没有预置账号。部署服务时设置 `APP_ENV=production`，为密码重置配置 SMTP 和 `PUBLIC_URL`，并通过反向代理以 TLS 提供前端和接口服务。应用本身不负责终止 TLS。请将接口和数据库的直接访问限制在可信网络内。

迁移 `000012_auth_ip_rate_limits` 为所有 `/api/v1` 接口增加 MySQL 持久的来源限速：每个来源每分钟最多 1200 次；健康检查、就绪检查和 CORS 预检不计入。登录每分钟 30 次、注册每小时 10 次、找回和重置各 15 分钟 5/10 次，并为邮箱变更和 MFA 操作设置更严格的独立限额。来源键使用 `LOGIN_RATE_LIMIT_HMAC_KEY` 做 HMAC 后存储，不保存明文 IP；生产环境必须配置 `openssl rand -hex 32` 生成的 64 位十六进制密钥。反向代理部署时，仅把实际代理网段填入 `TRUSTED_PROXY_CIDRS`，否则服务只使用 TCP 对端地址并忽略 `X-Forwarded-For`。超管平台页可查看活动认证/API 限流告警的脱敏来源标识。该告警入口不替代生产监控平台的值班通知。

## 角色与首位超管初始化

只有运维人员才能通过命令，在平台角色表为空时创建首位平台超管。将 `BOOTSTRAP_EMAIL`、`BOOTSTRAP_NAME` 和 `BOOTSTRAP_PASSWORD` 临时放入私有环境，然后运行 `cd backend && go run ./cmd/server bootstrap-admin`。使用后立即移除这些环境变量。公开注册和邀请只能创建普通账号。同一用户可以在一个团队中担任成员，在另一个团队中担任管理员或所有者；接口每次都会重新检查有效成员关系和资源所属团队。平台超管不会仅凭平台角色读取私人对话或个人记忆。

团队所有者管理所有权和管理员；团队管理员管理普通成员、邀请、项目和审批；成员可以处理自己的任务并提交计划变更提案。接口行为及尚有限制请参阅[验收记录与缺口](docs/ACCEPTANCE.md)。

设置页的“下载我的数据副本”通过 `GET /api/v1/me/data-export` 生成当前账号拥有的数据 JSON 文件并记录审计；文件排除团队共享业务数据、密码哈希、访问令牌、微信凭据和推送订阅密钥。用户可在设置页注销账号（`POST /api/v1/me/account`）：须重新输入当前密码、准确输入“删除账号”，启用 MFA 时还须提交未使用的动态码或恢复码。有未转移的团队所有权或该用户是唯一的平台超管时，服务端拒绝注销。成功后事务内撤销登录能力，删除私人对话与消息、个人记忆、通知、提醒、设备订阅、MFA/重置令牌、个人微信绑定等个人数据；清除任务负责人并移除团队成员关系。团队共享项目、任务、评论、审批、团队知识和审计作为业务/安全记录保留，用户身份变为“已注销用户”，个人对话内容不保留。


数据留存由 API 内每小时运行的有界后台清理器执行，每条规则每轮最多处理 20 批、每批 1000 条：过期/已用密码重置、邮箱变更、MFA 挑战和登录失败/IP 限流记录保留 30 天；过期/撤销会话历史保留 90 天；失效邀请和已结束提醒保留 90 天；微信消息加密载荷保留 30 天，去重记录保留 180 天；通知及投递记录保留 365 天；本地安全审计日志保留 2555 天（约 7 年）。清理器不删除仍有效的凭据、待执行提醒、近期通知或团队共享业务对象。账号注销会立即清除该账号的私人数据；已确认个人记忆在单独删除对话时保留内容但移除来源引用。团队共享记录没有自动到期清理；上线前仍须由部署方结合合同、适用法规和等保制度确认期限，并配置不可变外部审计归档与备份恢复流程。

如果每个新团队都必须等待平台超管审批，请在启动接口服务前设置 `TEAM_APPROVAL_REQUIRED=1`。待审核团队在审核通过前不能调用团队业务接口。当前工作区已有的开发 `.env` 指向较早建立的无版本数据库，迁移脚本会有意拒绝修改它。要使用新的隔离数据库，请运行 `scripts/migrate.sh <数据库名>`；脚本会创建该数据库，只将该库应用账号的权限重置为增删改查，并使用数据库超级用户执行迁移。要使用已有数据库，请先恢复经过验证且带版本记录的备份，或人工完成核对，再将接口服务指向该数据库。

## AI 与微信

`AI_PROVIDER=mock` 会返回明确标注的本地模拟回复，并持久化对话；模拟模式不运行工具。`AI_PROVIDER=openai_compatible` 需要配置 `AI_BASE_URL`、`AI_MODEL` 和 `AI_API_KEY`，这些配置只保留在服务端。助手的“运行智能体”支持最多三步串行、不同工具的短工作流：可读取已确认团队任务，再创建待审批的任务计划提案；团队管理员/所有者还可逐次授权创建一条待确认团队知识候选。任务提案只能修改标题、描述、优先级、状态、进度、负责人、排期或里程碑，须由现有审批流程批准后才更新正式计划；团队知识候选默认不确认、不进入模型上下文，确认前只对管理员/所有者可见，来源对话只对候选作者显示。项目变更也可由智能体生成待审批提案；项目数据以版本号防止过期提案覆盖新修改，只有管理员/所有者批准后才写入正式项目，审批历史、审计、通知和 outbox 在同一事务中记录。Python/Bash 沙箱是本次唯一工具步骤，必须由用户逐次授权并在断网隔离环境运行。并行调用、重复调用和未经批准的业务写入均被拒绝；每个工具步骤分别写入调用记录和审计。Python 和 Bash 沙箱使用 bubblewrap、prlimit，以非 root 运行；每次最多 6 秒、256 MiB 地址空间、每进程文件 1 MiB、临时工作区累计 2 MiB 且最多 64 个文件系统条目，并限制输出和并发最多 2 个任务。临时工作区放在 `/dev/shm` 内存文件系统，部署环境必须为服务账号提供足够的临时空间。用户可以上传最多 12 个 UTF-8 文本文件作为临时输入，单文件 64 KiB、总计 256 KiB；文件及代码不会持久化，除非用户另行勾选一次性授权并配置私人工作区密钥。私人工作区支持 `.txt`、`.csv`、`.json`、`.md` 文本文件，最多 12 个、总量 256 KiB，MySQL 中只存 AES-GCM 密文；选中的文件只有本次明确授权后才提供给沙箱，输出也只有用户单独授权时才持久保存。沙箱默认断网，根文件系统为只读，只挂载临时工作区和运行依赖，也不提供生产目录或生产密钥。结果文件可在浏览器内显示、下载或按授权保存到私人工作区。部署环境缺少沙箱依赖时，界面会显示不可用，服务端会拒绝执行。真实模型凭据未配置时不能验收模型实际工具选择。

## 邮件与手机浏览器推送

通知页提供站内、邮件、浏览器推送偏好。邮件使用配置的 SMTP 发送不含通知正文的通用站内链接；端口 `465` 使用隐式 TLS，其他端口必须支持 STARTTLS。需要配置 `SMTP_HOST`、`SMTP_USER`、`SMTP_PASSWORD`、`SMTP_FROM`、`SMTP_PORT` 和 HTTPS `PUBLIC_URL`。密码重置邮件与用户主动选择的通知邮件使用同一 SMTP 配置，但订阅偏好分别管理。

网页手机推送使用浏览器标准 Web Push。需要 HTTPS、VAPID P-256 私钥、真实可联系的 `VAPID_SUBJECT`（`mailto:` 地址或 HTTPS 联系页面），以及 32 字节 `NOTIFICATION_ENCRYPTION_KEY`。示例域名、保留域名和本机地址会使服务端将推送标记为未配置。可运行 `cd backend && go run ./cmd/server generate-vapid-key` 生成 VAPID 私钥；使用 `openssl rand -base64 32` 生成加密密钥。密钥放在服务端密钥设施或私有环境中；不要提交到仓库。iOS/iPadOS 通常要求用户先将网站加入主屏幕，再由支持的系统版本授予通知权限。浏览器订阅在 MySQL 中以 AES-GCM 加密保存，投递会拒绝内网和保留地址、阻止重定向，并按退订状态停止投递。

网页入口提供可安装的 PWA 清单和 Android/iOS 桌面图标，手机用户可从浏览器菜单添加到主屏幕。PWA 继续使用网页会话和后端 API；它不提供离线业务数据，也不等同于原生应用。隐私业务数据不会缓存到 Service Worker。

## 原生 iOS/Android App 与系统推送

当前网页可独立运行；仓库另提供 Capacitor 原生 App 外壳。原生推送使用 `@capacitor-firebase/messaging` 7.5.0（Apache-2.0）和 Firebase Cloud Messaging HTTP v1；它与网页 Web Push 使用独立设备令牌、偏好和投递记录。原生设备令牌只以 AES-GCM 密文保存在 MySQL，服务端通过固定 Google OAuth 与 FCM HTTPS 接口投递，不把服务账号凭据或设备令牌交给浏览器代码。

原生壳通过 WebView 加载同一套网页界面，不是独立原生 UI。开发 Debug 包的 Android 来源通常为 `http://localhost`，iOS Capacitor 来源为 `capacitor://localhost`；后端 `CORS_ORIGIN` 必须精确列出实际使用的来源。生产构建应使用 HTTPS API，并将 `CORS_ORIGIN` 替换为生产 Web/原生来源，不能沿用本地 Debug 来源。

部署前须由 Firebase 项目管理员提供 Android `android/app/google-services.json`、iOS `ios/App/App/GoogleService-Info.plist`，并在 Firebase 项目上传 APNs 认证密钥/证书。两个客户端配置文件已加入 `.gitignore`，不能提交。API 服务设置 `FCM_SERVICE_ACCOUNT_FILE` 指向 Firebase 服务账号 JSON 文件；生产文件建议只允许运行 API 的账号读取（权限 `0600`）。还须配置 `NOTIFICATION_ENCRYPTION_KEY`，此密钥同时用于加密浏览器推送订阅与原生设备令牌；轮换前必须实现数据重加密。Firebase 服务账号应仅授予发送 FCM 消息所需权限。

在 macOS/Linux 上构建时，设置 HTTPS API 地址并分别执行：

```sh
TEAMMIND_API_BASE_URL=https://api.example.com/api/v1 npm run build:android
TEAMMIND_API_BASE_URL=https://api.example.com/api/v1 npm run build:ios:sim
```

Android 构建还需安装 Android SDK/JDK；iOS 构建需要 macOS、Xcode 和 CocoaPods。iOS 真机发布需要 Apple 开发者签名及已配置 APNs 的 Firebase 项目。原生 App 运行后，用户在通知设置中授权系统通知、注册设备、启用原生推送偏好；可以查看并撤销已绑定设备。收到的推送只含通用提示，不含任务、聊天或通知正文，点击后进入应用通知中心。若服务端 Firebase 服务账号或 App 内 Firebase 配置缺失，启用按钮会禁用并显示配置状态。

本地 Android Debug 包已在 Android API 35 模拟器构建、安装并完成触摸登录、设置和通知页检查；设置/通知页在 412 CSS px 下无横向溢出。该次验收所连接的隔离 API 未配置 SMTP、VAPID 与 Firebase，页面会如实提示；使用的是仅限本地验收的 HTTP API 地址。当前没有 Firebase 项目凭据、Xcode/CocoaPods 或实体手机；本地 OAuth/FCM 假服务测试不证明真实云端投递，生产包、iOS 构建和实体设备送达仍需外部配置并验收。

当前开发环境没有 SMTP 服务凭据；本地 API 配置了格式有效的 VAPID 和订阅加密密钥，但 `VAPID_SUBJECT=mailto:admin@example.invalid` 是保留域名，API 现将推送报告为未配置且不返回公钥；数据库没有活动浏览器推送订阅。Android Chrome 模拟器在 `PushManager.subscribe()` 时返回 `AbortError: Registration failed - push service error`；设备到 `fcm.googleapis.com:443` 超时，所以真实网页推送仍未验证送达。真人已在隔离验收环境完成微信扫码与手机确认，并验证了真实入站消息；这不能证明邮件、浏览器推送或原生 Android/iOS 推送已经送达。自动化覆盖 SMTP STARTTLS 本地协议对端、邮件正文脱敏、网页推送加密/VAPID、原生推送 OAuth/FCM HTTP 契约和服务工作线程。原生推送还需要 Firebase 项目配置及相应实体设备验收。

## 腾讯 OpenClaw 微信通道

TeamMind 在独立的本机 OpenClaw Gateway 进程中运行固定版本 OpenClaw `2026.10.4` 和腾讯维护的 `@tencent-weixin/openclaw-weixin@2.4.10`（MIT；插件要求 Node.js ≥22.13，当前 OpenClaw 版本要求 Node.js ≥24.16 且 <25，或 ≥26.1）。启动脚本会校验依赖版本，并对已审阅的插件包应用 TeamMind Web 桥接补丁：二维码登录沿用腾讯插件实现；扫码状态、验证码与取消走插件登录模块；入站 `getUpdates` 和出站 `sendMessage` 调用腾讯插件自己的 API 适配器。Go 服务只通过绑定回环地址、要求 32 字节以上共享令牌的接口与 Gateway 通信，不直接向微信服务器发送协议请求。

先在 `.env` 中配置 `WECHAT_MODE=real`、Base64 编码的 32 字节 `WECHAT_ENCRYPTION_KEY`、回环 `WECHAT_GATEWAY_URL` 和高强度随机 `WECHAT_GATEWAY_BRIDGE_TOKEN`。API、Gateway 和消息 worker 必须使用相同的 Gateway 地址与桥接令牌。可用 `openssl rand -base64 32` 生成微信加密密钥，用 `openssl rand -hex 32` 生成桥接令牌。Gateway 配置存放在权限为 0700 的 `OPENCLAW_STATE_DIR`；默认位于 `/dev/shm`。登录 QR 会话只驻留 Gateway 内存并在取消、过期或 Gateway 重启时失效；正式绑定令牌以 AES-GCM 加密后存入 MySQL，不写入 OpenClaw 明文账号文件。密钥轮换前必须重新加密已有绑定。

按以下顺序分别启动三个进程：

```sh
# 终端一：腾讯微信插件 Gateway
cd backend/wechat-plugin-bridge
npm ci
npm start # 从仓库根目录 .env 只读取 Gateway 令牌、端口和状态目录

# 终端二：TeamMind Go API
cd backend
set -a; source ../.env; set +a
go run ./cmd/server

# 终端三：消息收发 worker
cd backend
set -a; source ../.env; set +a
go run ./cmd/server wechat-worker
```

用户在 TeamMind 渠道页面创建扫码会话；手机无法扫描同一屏幕时，应在另一台设备展示二维码。流程覆盖等待扫码、已扫码、验证码、二维码过期、取消和真实确认后绑定。消息 worker 从 MySQL 恢复绑定与同步游标，重新校验微信身份、用户、团队成员关系，未知发送者默认拒绝；消息按账号和发送者/用户/团队隔离，入站记录去重，发送失败有界重试。目前只向微信发送文本；非文本媒体明确拒绝。

本地 `npm test` 会检查桥接认证、循环地址限制、腾讯插件 API 适配器的收发映射和取消长轮询。真人已在隔离数据库对应的真实腾讯插件流程中扫码并确认。修复 Go 集成测试的清理时序并移除其 7 个遗留夹具后，数据库仍读到 1 个有效绑定、1 条真实入站记录和 1 个隔离会话；API、Gateway 与 worker 重启后的绑定读回此前已验证，当前 worker 持续同步。**生产 AI 微信回复尚未验证**：当前 `AI_PROVIDER=mock`，唯一投递失败，worker 会拒绝发送模拟回复。生产回复仍需配置真实模型服务并再次验证；模拟模式不会创建真实绑定。
## 验证

```sh
cd backend && go test ./...
python3 scripts/acceptance_api.py   # 在仓库根目录运行，接口监听 8188 端口
python3 scripts/acceptance_browser.py
python3 scripts/acceptance_browser_workflow.py
TEAMMIND_WEB_BASE=http://127.0.0.1:5190 TEAMMIND_API_BASE=http://127.0.0.1:8240/api/v1 TEAMMIND_BROWSER=chromium python3 scripts/acceptance_member_proposals.py  # 需要隔离迁移 MySQL、浏览器与运行中的网页/API
python3 scripts/acceptance_settings.py  # 需要全新迁移的隔离 MySQL 数据库和接口服务
python3 scripts/acceptance_reminders.py  # 使用隔离提醒接口和 Chromium 手机流程
python3 scripts/acceptance_cross_browser.py
# 需要 real 模式 API、隔离 MySQL、运行中的 OpenClaw Gateway/腾讯插件及 WebKit
TEAMMIND_BROWSER=webkit TEAMMIND_WEB_BASE=http://127.0.0.1:5174 TEAMMIND_API_BASE=http://127.0.0.1:8188/api/v1 python3 scripts/acceptance_wechat_ui.py
# 先启动隔离接口服务并设置 TEAM_APPROVAL_REQUIRED=1，再初始化测试超管。
ACCEPTANCE_SUPERADMIN_EMAIL="$BOOTSTRAP_EMAIL" ACCEPTANCE_SUPERADMIN_PASSWORD="$BOOTSTRAP_PASSWORD" python3 scripts/acceptance_platform.py
npm run type:check
npm run build
```

设置页验收会实际操作个人资料、团队创建/编辑、错误/成功密码修改、跨浏览器会话撤销与当前会话续期/退出、审计刷新、通知开关/免打扰保存、提醒创建/取消，并检查通知和设置页在多种视口下无页面级横向溢出。若要做沙箱端到端验收，需在非 root 环境以隔离数据库启动 API，并配置真实兼容模型或专用的工具调用测试模型；不得用模拟 AI 的回复冒充工具执行结果。

集成脚本会在配置的数据库中创建测试用户和团队。请使用隔离测试数据库，避免污染共享环境。浏览器验收及已知缺口记录在[验收记录与缺口](docs/ACCEPTANCE.md)中。请备份数据库卷，并在目标部署环境演练恢复。本仓库尚未提供完整的自动备份、监控告警、独立渗透测试或等保三级测评。
