# TeamMind

TeamMind 是面向 20–50 人团队的协作与 AI 工作台，覆盖项目、任务、甘特图、审批、通知、AI、组织与平台管理。

> **前端状态：已用 Ant Design Vue 4 从零重建。** 覆盖认证、总览、任务、项目、甘特、审批、成员、部门、通知、设置、AI 助手、记忆、微信、AI 管理、平台控制台与公开分享共 19 个页面。功能与接口基线见 [docs/FRONTEND_FUNCTIONS_API.md](docs/FRONTEND_FUNCTIONS_API.md)，选型记录见 [docs/ui-preview/index.html](docs/ui-preview/index.html)，旧前端源码备份在 `.archive/frontend-legacy-*.tar.gz`。

- 架构与模块：[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)
- AI 定时问候/日报：设置 → AI 日报，可创建每天/工作日/每周/一次的计划
- 注册与入驻：平台超管可关闭公开注册；公司用「公司邀请链接」创建，员工用公司/团队邀请链接加入
- 部署与运维：[docs/OPERATIONS.md](docs/OPERATIONS.md)
- 验收与边界：[docs/ACCEPTANCE.md](docs/ACCEPTANCE.md)
- 前端重建基线：[docs/FRONTEND_FUNCTIONS_API.md](docs/FRONTEND_FUNCTIONS_API.md)

## 技术栈

| 层 | 实现 |
| --- | --- |
| 前端 | Vue 3、Vite 7、TypeScript、Ant Design Vue 4（按需自动导入，双主题 token） |
| 后端 | Go `net/http`、原生 SQL、SSE、MySQL Outbox/后台 worker |
| 数据库 | MySQL 8（43 个迁移，当前 `000043_ai_provider_created_by_nullable`） |
| AI | 小米 MiMo `mimo-v2.6-flash`（OpenAI 兼容，可切 provider）；工具调用、审批、记忆 |
| 检索 | SearXNG + Firecrawl（可选）+ Trafilatura（可选）+ 原生抓取回退 |
| 沙箱 | Docker 常驻会话 / bwrap / 一次性 Docker；断网、只读、资源限额 |
| 微信 | OpenClaw Gateway + `@tencent-weixin/openclaw-weixin@2.4.10` + TeamMind 桥接（仅通道，禁用 OpenClaw 内置 agent cron） |

后台沿用“固定导航 + 侧栏 + 页面标签 + 面包屑 + 内容区 + 移动端抽屉”的信息架构；Ant Design 组件通过 `unplugin-vue-components` 按需引入，主题由 `ConfigProvider` + CSS 变量（`data-theme`）共同驱动，亮/暗主题与品牌色集中在 `src/theme.ts` 与 `src/styles/tokens.css`。

## 本地启动

依赖：Docker Compose、Go、Node.js/npm。微信 Gateway 需要 Node 24.16–24.x 或 26.1+；脚本会自动选择/下载合格 Node。

```sh
cp .env.example .env
# 修改 MYSQL_DSN、各类密钥；不要提交 .env
docker compose up -d mysql
scripts/migrate.sh
(cd backend/wechat-plugin-bridge && npm ci)
scripts/dev-up.sh
```

启动后：

- API：`http://127.0.0.1:8188`（`/healthz`、`/readyz`）
- Web：`http://127.0.0.1:5174`（Vite 将 `/api` 代理到本地 API）
- OpenClaw Gateway：`http://127.0.0.1:18793`（仅回环）

停止：

```sh
scripts/dev-down.sh
```

`scripts/dev-up.sh` 幂等；已监听的端口不会重复启动。日志在 `.run/logs/`。

## 初始账号

生产环境不预置账号。本地 `.env` 可用 `BOOTSTRAP_*` 初始化第一个超管。

当前演示环境（仅本地）：

- 公司 owner：`verify-1792468162@example.com`
- 团队版 owner：`team-demo@teammind.local`
- 平台超管：`ops@teammind.local`

密码只保存在本地数据库，不写进仓库。

## 关键配置

完整说明见 [docs/OPERATIONS.md](docs/OPERATIONS.md)。至少配置：

```sh
MYSQL_DSN=
LOGIN_RATE_LIMIT_HMAC_KEY=
MFA_ENCRYPTION_KEY=
AGENT_WORKSPACE_ENCRYPTION_KEY=
AI_CONFIG_ENCRYPTION_KEY=
GANTT_SHARE_SECRET=
NOTIFICATION_ENCRYPTION_KEY=
```

可选：

- `SEARCH_PROVIDER=searxng` + `SEARXNG_URL`
- `FIRECRAWL_API_KEY`（可选）
- `TRAFILATURA_PYTHON` / `TRAFILATURA_PYTHONPATH` / `TRAFILATURA_SCRIPT`
- `STT_PROVIDER=mimo|openai` + `STT_BASE_URL` / `STT_API_KEY` / `STT_MODEL`
- `SMTP_*` + `PUBLIC_URL`（邮件）
- `VAPID_*` / `FCM_SERVICE_ACCOUNT_FILE`（浏览器/原生推送）
- `WECHAT_MODE=real` + `WECHAT_ENCRYPTION_KEY` + `WECHAT_GATEWAY_BRIDGE_TOKEN`

## AI 模型

当前默认使用小米 MiMo：

- Base URL：`https://api.xiaomimimo.com/v1`
- 模型：`mimo-v2.6-flash`（备选 `mimo-v2.6-pro`）
- 通过 AI 管理 API 维护 provider、API Key、优先级和模型列表

切换供应商后无需重启 API。

## 验证

```sh
cd backend && go test ./...
python3 scripts/acceptance_ai_upgrade.py
python3 scripts/run_agent_eval.py
./scripts/preflight.sh
```

前端验证：`npm ci && npm run typecheck && npm run build`，浏览器逐页对照 [docs/FRONTEND_FUNCTIONS_API.md](docs/FRONTEND_FUNCTIONS_API.md) 的验收清单。

## 目录

```text
src/                     前端源码（Vue 3 + Ant Design Vue）
public/                  Service Worker 等静态资源
backend/                 Go API、迁移、测试
backend/wechat-plugin-bridge/  微信桥接（补丁 + 语音下载接口）
scripts/                 启动、迁移、预检、验收
evals/                   黄金评测任务
docs/                    架构、运维、验收、前端 API 清单
docs/ui-preview/         前端 UI 库选型报告与真实组件预览
deploy/                  辅助服务配置
.archive/                删除的旧前端源码备份（本地）
```

## 前端结构

```text
src/api/          axios 封装、SSE fetch、类型定义
src/store/        Pinia 会话（用户 / 组织 / 角色）
src/router/       路由守卫与侧栏菜单
src/layout/       顶栏、侧栏、标签页、移动端抽屉、SSE 未读
src/components/   PageHeader、StatusTag、UserAvatar、GanttTimeline（拖拽改期/依赖连线）、AppTabs
src/views/auth    登录 / 注册 / 超管 / 找回 / 邀请 / 邮箱确认
src/views/team    14 个业务页面
src/views/settings 设置页六个分区
src/views/public  公开甘特分享、404
src/styles/       设计 token、全局样式、响应式
```

重建已覆盖 [docs/FRONTEND_FUNCTIONS_API.md](docs/FRONTEND_FUNCTIONS_API.md) 的 19 个页面与接口；甘特图支持拖拽改期、边缘调整起止日期、拖点建立依赖与点击删除依赖。上线前仍需接真实后端做一轮逐页验收（当前 UI 已在本地 mock 数据下核对亮/暗主题、窄屏与主要弹层）。

## 边界

- 未实现 OIDC/SSO/SCIM。
- 权限是 MySQL 元组 Check/ListObjects，不是完整 Zanzibar（无条件关系/Expand）。
- 审批续跑是耐久 follow-up，不是 Temporal 级任意步骤重放。
- 微信只做出站文本；不做原生语音条（入站语音已支持 STT）。
- 生产部署前必须替换临时密钥、配置 TLS/反向代理并做备份演练。

## 生产上线

复制 `.env.production.example` 为 `.env.production`，只填启动引导项。SMTP/STT/Public URL/VAPID/微信网关可通过平台配置 API 管理，AI 通过 AI 管理 API 管理。完整清单见 [docs/GO_LIVE.md](docs/GO_LIVE.md)。
