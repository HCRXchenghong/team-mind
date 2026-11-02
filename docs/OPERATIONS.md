# TeamMind 运维手册

# 状态提示

Web 前端已基于 Vue 3 + Ant Design Vue 4 重建。根目录 `npm ci`、Vite 和本地 Web 端口 `5174` 可用；本地启动以根目录 `README.md` 为准。界面功能/API 基线见 `docs/FRONTEND_FUNCTIONS_API.md`。

> 适用于单机/单服务部署。命令默认在仓库根目录执行。

## 1. 依赖

| 组件 | 要求 |
| --- | --- |
| MySQL | 8.x，UTF-8，UTC |
| Go | ≥ 1.18（当前构建可用更高版本） |
| Node.js / npm | ≥ 22；OpenClaw Gateway 需 24.16–24.x 或 26.1+ |
| Docker | 可选；macOS 沙箱/沙箱会话必需 |
| bubblewrap | 可选；Ubuntu 一次性沙箱首选 |
| Ollama | 可选；本地模型 `qwen3:14b` |
| SearXNG | 可选；联网搜索 |
| Python + Trafilatura | 可选；正文抽取 |

## 2. 首次部署

```sh
cp .env.example .env
# 编辑 .env：MYSQL_DSN、密钥、PUBLIC_URL 等
docker compose up -d mysql
scripts/migrate.sh
npm ci
(cd backend/wechat-plugin-bridge && npm ci)
scripts/dev-up.sh
```

检查：

```sh
curl -s http://127.0.0.1:8188/healthz
curl -s http://127.0.0.1:8188/readyz
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:5174/
scripts/preflight.sh
```

`readyz` 会检查数据库与 `000041_platform_runtime_config` 迁移。

## 3. 环境变量

### 必填

| 变量 | 用途 |
| --- | --- |
| `MYSQL_DSN` | MySQL DSN，含库名 |
| `LOGIN_RATE_LIMIT_HMAC_KEY` | 来源 IP HMAC，64 位 hex |
| `MFA_ENCRYPTION_KEY` | MFA 密钥加密，32 字节 Base64 |
| `AGENT_WORKSPACE_ENCRYPTION_KEY` | 智能体工作区 AES-GCM |
| `AI_CONFIG_ENCRYPTION_KEY` | AI provider key 加密 |
| `GANTT_SHARE_SECRET` | 甘特分享签名 |
| `NOTIFICATION_ENCRYPTION_KEY` | 通知投递凭据加密 |

### 常用可选

```sh
APP_ENV=production
PUBLIC_URL=https://team.example.com
CORS_ORIGIN=https://team.example.com
API_ADDR=127.0.0.1:8188
TEAM_APPROVAL_REQUIRED=1
TRUSTED_PROXY_CIDRS=127.0.0.1/32

SEARCH_PROVIDER=searxng
SEARXNG_URL=http://127.0.0.1:8080
OLLAMA_BASE_URL=http://127.0.0.1:11434
MEMORY_EMBEDDING_MODEL=nomic-embed-text
```

密钥生成：

```sh
openssl rand -base64 32   # AES 类
openssl rand -hex 32      # HMAC
```

## 4. 数据库迁移

```sh
scripts/migrate.sh                 # 使用 .env 的 MYSQL_DSN
scripts/migrate.sh teammind_local  # 指定库
```

- 迁移按文件名顺序执行，记录在 `schema_migrations`。
- 应用表已存在但迁移历史为空时脚本拒绝执行；先核对数据库或从备份恢复。
- 回滚：

```sh
scripts/rollback.sh <数据库名>
```

只回滚最近一个已记录版本；DDL 非事务，失败从备份恢复。

## 5. 启动/停止/日志

```sh
scripts/dev-up.sh
scripts/dev-down.sh
scripts/wechat-gateway.sh status
scripts/wechat-gateway.sh restart
scripts/wechat-gateway.sh logs
```

- API `8188`，Web `5174`，Gateway `18793`（仅回环）。
- 日志：`.run/logs/api.log`、`vite.log`、`wechat-worker.log`、`openclaw-gateway.log`。
- `dev-up.sh` 幂等；已监听端口不会重启。
- Gateway/worker 有 watchdog，默认 30 秒内自动拉起；`TEAMMIND_GATEWAY_WATCHDOG=0` / `TEAMMIND_WORKER_WATCHDOG=0` 可关闭。
- Gateway 状态目录 `.run/openclaw-weixin-state`；`reset-state` 会要求重新扫码。

### Node 版本

OpenClaw `2026.10.4` 只接受 Node `>=24.16 <25` 或 `>=26.1`。`scripts/wechat-gateway.sh` 会自动在 PATH/nvm/fnm/volta/asdf/Homebrew 中找合格 Node，必要时下载便携版到 `.run/`。不要用 Node 25 硬跑。

## 5.1 团队 AI 供应商

平台超管可在 AI 管理配置全局 provider；团队管理员在“设置 → 组织 → 团队 AI 供应商”管理本团队 provider。团队 provider 只被本团队上下文选中，未配置时回退全局。

## 5.2 权限与隔离

管理员在“设置 → 组织 → 权限与隔离”配置：

| 配置 | 可选值 | 作用 |
| --- | --- | --- |
| AI 助手访问 | 所有成员 / 负责人+管理员 / 仅管理员 | 非授权账号调用 AI 返回 403 |
| 微信绑定 | 所有成员 / 负责人+管理员 / 仅管理员 | 非授权账号扫码绑定返回 403 |
| 数据隔离 | 共享 / 按部门小组隔离 | 隔离时普通成员只看到本单元项目/任务/甘特 |

AI 工具仍可读写全组织总甘特，用于跨组协作；正式写入走提案/审批。

## 5.3 上线凭证检查

生产 env 只放启动引导：数据库 DSN、CORS、根加密密钥等。SMTP/STT/Public URL/VAPID 联系地址/微信网关在 **平台管理 → 系统配置**，AI 在 **AI 管理** 中配置。

```bash
./scripts/production-check.sh .env.production
```

脚本只检查 bootstrap；运行配置由平台超管在后台填。`GET /api/v1/platform/readiness` 返回 AI/SMTP/微信/STT/PUBLIC_URL 的细项，`POST /api/v1/platform/readiness/test-email` 可直接测试 SMTP；详见 [GO_LIVE.md](GO_LIVE.md)。

## 6. AI 供应商

当前生产默认使用小米 MiMo（OpenAI 兼容）。在“AI 管理”页添加：

- Base URL：`https://api.xiaomimimo.com/v1`
- API Key：小米 MiMo key
- 模型：`mimo-v2.6-flash`（首选，快）、`mimo-v2.6-pro`（备选）
- 优先级：`1`
- 作用范围：全局，或指定团队
- 启用

同时禁用本地 Ollama 和旧中转 provider，避免回落到旧模型。

本地 Ollama（仅离线开发）：

```sh
ollama pull qwen3:14b
```

- Base URL `http://127.0.0.1:11434/v1`
- API Key 任意 ≥8 字符占位
- 模型 `qwen3:14b`
- 优先级高于 MiMo 时才会被优先使用

必须固定 `AI_CONFIG_ENCRYPTION_KEY`，否则已存 key 无法解密；换 key 后在 AI 管理页重新保存。

## 7. 联网检索

### SearXNG

```sh
SEARCH_PROVIDER=searxng
SEARXNG_URL=http://127.0.0.1:8080
```

预检会检查 `/?q=teammind&format=json`。

### Firecrawl（可选）

```sh
FIRECRAWL_API_KEY=...
FIRECRAWL_BASE_URL=https://api.firecrawl.dev
```

失败自动回退原生抓取。

### Trafilatura（可选）

```sh
scripts/install-web-extractor.sh
# 输出会提示路径
TRAFILATURA_PYTHON=/usr/bin/python3
TRAFILATURA_PYTHONPATH=/opt/teammind/py-deps
TRAFILATURA_SCRIPT=/opt/teammind/scripts/web_extract.py
```

不配置时 `web_fetch` 使用内部 HTML 清洗。

## 8. 微信语音 STT

> 运行配置也可以在 **平台管理 → 系统配置** 保存；下列 env 仅作为启动 fallback。

需要 `WECHAT_MODE=real`。

### MiMo（当前验证）

```sh
STT_PROVIDER=mimo
STT_BASE_URL=https://api.xiaomimimo.com/v1
STT_API_KEY=...
STT_MODEL=mimo-v2.5-asr
STT_LANGUAGE=zh
```

- 微信自带 `voice_item.text` 优先，不需要 STT key；
- 无转写时：桥接下载/解密/SILK→WAV，Go 用 ffmpeg 转 MP3，再调 MiMo `/chat/completions`；
- 需要 `ffmpeg`；`silk-wasm` 已随 bridge 依赖安装；
- 生产必须用持久 key；临时 key 过期会报 401。

### OpenAI 兼容

```sh
STT_PROVIDER=openai
STT_BASE_URL=https://api.openai.com/v1
STT_API_KEY=...
STT_MODEL=whisper-1
STT_LANGUAGE=zh
```

走 `/audio/transcriptions`；回环地址允许无 key（本地 whisper server）。

**限制**：微信出站只发文本，不发原生语音条。

## 9. 邮件、微信与推送

平台超管 → **平台管理 → 系统配置**：

- Public URL
- SMTP Host / Port / User / Password / From
- STT Provider / Base URL / API Key / Model
- VAPID 联系地址
- 微信模式 / 网关 URL / Bridge Token

敏感字段加密存 MySQL，接口只返回 `*Set` 标记。保存后立即生效，无需重启。

- 预检 `SMTP not configured` 是警告；填完后台后可用 `POST /api/v1/platform/readiness/test-email` 验证。
- 浏览器推送：`VAPID_PRIVATE_KEY` 仍为 bootstrap；`VAPID_SUBJECT` 在系统配置中填。
- 原生推送：`FCM_SERVICE_ACCOUNT_FILE` 指向 service account JSON。
- 微信提醒：用户绑定微信后，定时任务会通过 `channel_bindings` 的最新 sender/context 调 OpenClaw `send-text`；需要用户先给绑定微信发过至少一条消息，系统才能保存主动发送所需的联系人上下文。
- AI 定时问候/日报：`scheduled_jobs` 中 `job_type='ai.proactive'`，由 API 进程内的 `RunAIProactiveWorker` 每 10 秒扫描。生成时使用团队 AI 供应商与团队快照；AI 未配置或调用失败会自动降级为模板摘要。投递复用 `notifyResource`，因此遵循通知偏好与微信/邮件/推送通道。
- OpenClaw 只作为微信桥接运行时：`backend/wechat-plugin-bridge/run-gateway.mjs` 显式关闭了 OpenClaw 内置 `main` agent 的 heartbeat、memory dreaming 与 skills workshop collection review。这样即使没有 OpenClaw 的 OpenAI 鉴权，也不会再出现 `No route-compatible authentication source is configured for openai` 的周期告警；TeamMind 的微信收发与 AI 均不依赖这些任务。
- 主动消息排查：设置页「微信渠道」会显示绑定是否 `hasContext`，并提供「发送测试消息」按钮（`POST /me/channels/wechat/test`）。若按钮不可用，先在微信里给机器人发一条消息；若发送失败，查看 `.run/logs/api.log` 与投递历史里的 `last_error`（`wechat_send_failed` / `wechat_contact_required`）。
- 绑定后没有可用上下文时，通知仍会生成站内消息，并写入一条 `wechat/suppressed` 投递记录，原因 `wechat_contact_required`，不会静默丢失。
- Agent 消息：AI 可调用 `send_agent_message` 给成员或部门/小组负责人，落 `agent_messages` 并进入通知中心；`list_agent_messages` 读取收件箱。后台 worker 对人工触发消息自动回复一次，自动回复不再触发（防循环）。
- 通知投递是 outbox + 重试；不是“配置即送达”，需真实 SMTP/推送凭证。

## 10. 沙箱

### macOS

```sh
scripts/sandbox/build-image.sh
```

镜像 `teammind-sandbox-python:local`；Docker Desktop 必须运行。

### Ubuntu

```sh
sudo apt-get install -y bubblewrap
# 或把运行用户加入 docker 组，使用 Docker 回退
```

一次性沙箱：断网、只读根、非 root、256 MiB、1 CPU、64 PID、8 秒。

### Runtime 会话

- `POST /me/sandbox/sessions` 创建常驻 Docker 容器；
- `POST /me/sandbox/sessions/{id}/exec` 执行；
- `DELETE /me/sandbox/sessions/{id}` 停止；
- 30 分钟 TTL，每用户最多 2 个，后台每分钟回收；
- 需要 Docker 和镜像。

## 11. 备份/恢复

```sh
mysqldump --single-transaction --routines --triggers teammind > backup.sql
mysql teammind < backup.sql
```

- 迁移前/回滚前必须备份并演练恢复。
- 备份包含：用户、团队、任务、元组、AI 运行、通知、MFA/加密密文。
- 丢失 `MFA_ENCRYPTION_KEY` / `AGENT_WORKSPACE_ENCRYPTION_KEY` / `AI_CONFIG_ENCRYPTION_KEY` 会导致对应密文不可解。
- 不要只备份 MySQL 而漏掉 `.env` 密钥；两者要分开安全保管。

## 12. 排障

| 现象 | 检查 |
| --- | --- |
| `readyz` 失败 | MySQL DSN、迁移 034 |
| 微信“暂无法连接” | `channel_bindings.current_team_id` 是否指向 active 团队；用户成员关系；`channel_sync_states.updated_at` 是否更新 |
| 微信语音无回复 | `channel_message_deliveries.last_error`；`voice_transcription_unavailable` 查 STT/ffmpeg；`unsupported media` 查 item.type |
| 语音 401 | STT key 过期/错误 |
| 沙箱不可用 | Docker 镜像 / bwrap / 权限 |
| 登录限流 | `auth_ip_rate_limits`；确认 `TRUSTED_PROXY_CIDRS` |
| Gateway 起不来 | `scripts/wechat-gateway.sh logs`；Node 版本；`reset-state` 会丢登录 |

## 13. 生产检查

- `APP_ENV=production`，TLS 由反向代理终止。
- 替换所有示例密钥，禁止提交 `.env`。
- 限制 8188/3306 访问，18793 只绑回环。
- 配置 SMTP、备份、监控、日志轮转。
- 先跑 `scripts/preflight.sh`、Go 测试、24/24 验收与浏览器冒烟。
