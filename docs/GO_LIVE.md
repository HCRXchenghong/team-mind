# TeamMind 上线清单

# 状态提示

当前 Web 基于 Vue 3 + Ant Design Vue 4 重建。本文中的页面操作应在新界面复核；后端配置与迁移仍可参考，界面功能/API 基线见 `docs/FRONTEND_FUNCTIONS_API.md`。

## 1. 启动引导与运行配置的边界

**必须留在部署环境（bootstrap）**：

| 变量 | 用途 |
| --- | --- |
| `APP_ENV=production` | 生产模式 |
| `API_ADDR` | API 监听地址 |
| `CORS_ORIGIN` | 浏览器来源白名单，启动时加载 |
| `MYSQL_DSN` / `MYSQL_*` | 数据库连接 |
| `LOGIN_RATE_LIMIT_HMAC_KEY` | 登录/限流签名 |
| `MFA_ENCRYPTION_KEY` | MFA 密文根密钥 |
| `AGENT_WORKSPACE_ENCRYPTION_KEY` | Agent 工作区密文根密钥 |
| `AI_CONFIG_ENCRYPTION_KEY` | AI/后台敏感配置密文根密钥 |
| `GANTT_SHARE_SECRET` | 公开甘特签名 |
| `NOTIFICATION_ENCRYPTION_KEY` | 通知订阅密文根密钥 |
| `WECHAT_ENCRYPTION_KEY` | 微信绑定凭据密文根密钥 |
| `VAPID_PRIVATE_KEY` | Web Push 根密钥 |

这些是启动前就需要的信任根；不能存在被它们加密的同一数据库里。

**在管理后台配置（不要写进 env）**：

| 配置 | 入口 |
| --- | --- |
| Public URL、SMTP、STT、VAPID 联系地址、微信模式/网关 URL/Bridge Token | 平台超管 → 平台管理 → 系统配置 |
| 全局 AI Provider Base URL / Key / 模型 | 平台超管 → AI 管理 |
| 团队专属 AI Provider | 团队管理员 → 设置 → 组织 → 团队 AI 供应商 |
| AI/微信/数据隔离策略 | 管理员 → 设置 → 组织 → 权限与隔离 |

SMTP 密码、STT API Key、微信 Bridge Token 在数据库中加密存储，接口只返回 `*Set` 标记。

## 2. 必须由你申请/提供的第三方资源

| 资源 | 费用 | 说明 |
| --- | --- | --- |
| AI Provider Key | 按量付费 | 小米 MiMo/OpenAI/DeepSeek；本地 Ollama 可免费替代 |
| SMTP 账号 | 企业邮箱通常免费；第三方邮件服务有免费额度/付费 | Host/Port/User/Password/From |
| 域名 + HTTPS | 已有域名免费；新域名年费；TLS 可免费 | Public URL 必须是实际 HTTPS 地址 |
| 微信 OpenClaw Gateway | 微信账号免费；服务器可能付费 | 网关必须与 API 同机 loopback |
| STT Key | 按量付费 | 可与 MiMo AI Key 共用；本地 Whisper 可替代 |
| VAPID 联系地址 | 免费 | 真实 `mailto:` 或 HTTPS 联系页 |

## 2.1 首次超管邀请

空库没有任何账号。部署后生成一次性超管邀请码：

```bash
set -a; . ./.env.production; set +a
./.run/teammind-server bootstrap-invite
```

把输出的 `SUPERADMIN_INVITE_URL`（或 `PUBLIC_URL` + `SUPERADMIN_INVITE_PATH`）**完整链接**发给负责人：

```text
https://team.example.com/superadmin-setup?code=<64位邀请码>
```

登录页不再展示“平台超管”入口；只有带 `code` 的完整邀请链接才能看到注册表单。邀请码使用后立即失效，且只有平台没有任何超管时可用。

## 3. 配置步骤

1. 复制 `.env.production.example` 为 `.env.production`，只替换 bootstrap 项。
2. `./scripts/production-check.sh .env.production`。
3. 启动 MySQL 8、API、Web、SearXNG 和 OpenClaw 微信 Gateway。
4. 平台超管登录 → **平台管理 → 系统配置**：
   - 填 Public URL；
   - 填 SMTP Host/Port/User/Password/From；
   - 填 STT Provider/Base URL/Key/Model；
   - 填 VAPID 联系地址；
   - 选择微信 `real`，填网关 URL 和 Bridge Token（至少 32 字符）。
5. 平台超管 → **AI 管理** 配置全局 provider；团队管理员配置本团队 provider。
6. 管理员 → **设置 → 组织 → 权限与隔离** 选择策略。
7. 用真实微信扫码；一个 TeamMind 账号只允许一个 active 微信。
8. 在系统配置点“保存”，然后调用测试邮件接口。

## 4. 发布前验证

```bash
./scripts/production-check.sh .env.production
./scripts/preflight.sh
curl -fsS https://你的域名/readyz

# 平台超管登录后
curl -fsS -H "Authorization: Bearer $TOKEN" \
  https://你的域名/api/v1/platform/readiness

# SMTP 测试
curl -fsS -X POST -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"to":"ops@yourdomain.com"}' \
  https://你的域名/api/v1/platform/readiness/test-email
```

`platform/readiness` 返回 `status=ready|degraded` 和 `missing`，覆盖：
数据库迁移、AI provider、SMTP、微信网关、STT、Public URL、Web Push。

## 5. 上线前人工确认

- 反向代理只暴露 HTTPS；`CORS_ORIGIN` 只填实际来源。
- 微信 Gateway 与 API 同机 loopback；Bridge Token 与后台保存值一致。
- 一个账号只绑定一个微信。
- 跨组 AI 提案 → 管理员审批 → 任务进度生效，用临时任务验证一次。
- 备份、日志轮转、磁盘、告警已配置。
- 不要把 `.env.production`、SMTP 密码、AI key、微信 token 提交仓库。

## 6. 当前自动验收边界

- 跨组提案审批已端到端通过。
- 团队管理员自助 AI 已通过 API/前端基础验证。
- 后台保存 SMTP 配置 → 测试邮件已用本地 STARTTLS SMTP 实际收到；生产 SMTP 账号仍需你在后台填。
- 真实生产 AI/SMTP/微信账号未注入；没有这些不能宣称生产消息链路完成。
