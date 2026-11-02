# TeamMind 前端功能与 API 清单（重建基线）

> **状态**：本清单已作为重建基线落地，新前端（Vue 3 + Ant Design Vue 4）位于 `src/`，19 个页面均已实现。
>
> **用途**：这是一份"删库前快照"。它记录旧 Web 前端**已经实现的功能**、每个功能对应的**后端接口与参数**，以及重建时必须保留的权限与交互规则。新前端应从这份清单出发，而不是从旧代码出发。
>
> **生成时间**：2026-10-31（Asia/Shanghai）
> **取证范围**：`src/`（14 个业务页面、5 个认证/公开页，以及布局、组件、状态、路由、请求层）、`backend/internal/live/`（路由分发与请求结构体）、`docs/` 既有文档。
> **备份**：删除前的完整前端源码已打包为 `.archive/frontend-legacy-<时间戳>.tar.gz`（见仓库根目录）。
> **约定**：下文路径均省略 `/api/v1` 前缀；`{T}`=组织 ID、`{P}`=项目 ID、`{K}`=任务 ID、`{D}`=部门/小组 ID、`{U}`=用户 ID、`{C}`=会话 ID、`{R}`=提案 ID、`{N}`=通知 ID。

---

## 1. 产品与信息架构

### 1.1 产品定位

TeamMind 是面向 **20–50 人团队**的协作 + AI 工作台：项目、任务、甘特图、审批流、成员与部门、通知提醒、AI 助手、记忆/知识、微信渠道、平台运营。组织分两种形态：

| 形态 | `kind` | 结构 | 说明 |
| --- | --- | --- | --- |
| 团队版 | `team` | 单层小组 | 不启用部门；页面文案用"小组" |
| 公司版 | `company` | 部门 | 项目、成员可归属部门；部门负责人（lead）有局部管理权 |

组织状态：`active`（可用）、`pending_review`（待平台审核）、`suspended`（停用）。非 `active` 时工作台只显示"等待审核"空状态。

### 1.2 角色与权限

| 角色 | 来源 | 能力 |
| --- | --- | --- |
| `member` | `team_memberships.role` | 看本组织数据（受数据隔离范围限制）；只能给自己建任务；变更计划/项目/身份需提交提案 |
| `team_admin` | 同上 | 组织管理：成员、部门、邀请、项目/任务删改、审批、组织设置、AI 供应商（团队池） |
| `team_owner` | 同上 | 管理员全部能力 + 组织所有权相关操作 |
| 部门负责人 `isLead` | `departments.lead_user_id` | 在其部门范围内可编辑项目/部门、处理审批（后端判定） |
| `super_admin` | `platform_roles` | 平台控制台、平台 AI 池、组织审核、运行配置；可越过组织角色进入工作台 |

> 前端菜单隐藏只是体验优化，**所有权限以接口鉴权结果为准**；403 时应提示而非静默失败。

### 1.3 路由地图（旧版）

| 路由 | 页面 | 可见范围 | 一句话职责 |
| --- | --- | --- | --- |
| `/login` | AuthPage | 公开 | 邮箱密码登录 + MFA 两步验证 |
| `/register` | AuthPage | 公开（受开关控制） | 公开注册；关闭时只显示“邀请注册”说明 |
| `/company-invite?code=...` | AuthPage | 邀请链接 | 用公司邀请码创建公司并注册负责人 |
| `/superadmin-setup?code=...` | AuthPage | 邀请链接 | 首次部署创建平台超管；无 `code` 时只显示“需要邀请链接” |
| `/forgot-password` `/reset-password?token=` | AuthPage | 公开 | 找回 / 重置密码 |
| `/verify-email-change?token=` | EmailConfirmPage | 公开 | 邮箱变更确认（一次性 token） |
| `/join/:token` | JoinPage | 公开 | 邀请落地：注册并入队，或登录后接受 |
| `/share/gantt/:token` | GanttSharePage | 公开 | 只读甘特图分享 |
| `/teammind/overview` | OverviewPage | 登录 | 组织总览 |
| `/teammind/tasks` | TasksPage | 登录 | 任务中心（列表/详情/评论/依赖/协作者/提案） |
| `/teammind/projects` | ProjectsPage | 登录 | 项目管理 + 变更提案 |
| `/teammind/gantt` | GanttPage | 登录 | 甘特图 + AI 排期体检 + 只读分享 |
| `/teammind/approvals` | ApprovalsPage | 管理员 / 负责人 | 审批中心 |
| `/teammind/departments` | DepartmentsPage | 管理员 / 负责人 | 部门（小组）管理 |
| `/teammind/members` | MembersPage | 管理员 / 负责人 | 成员、邀请、身份提案 |
| `/teammind/notifications` | NotificationsPage | 登录 | 通知 / 提醒 / AI 协作消息 |
| `/teammind/assistant` | AssistantPage | 登录 | AI 助手对话、智能体执行、协作 |
| `/teammind/memories` | MemoriesPage | 登录 | 个人记忆 / 团队知识 |
| `/teammind/settings` | SettingsPage | 登录 | 个人、安全、通知、组织、数据、审计 |
| `/teammind/wechat` | WechatPage | 登录 | 微信渠道绑定（扫码） |
| `/teammind/ai-admin` | AiAdminPage | 管理员 / 超管 | AI 供应商池管理 |
| `/teammind/platform` | PlatformPage | 超管 | 平台控制台 |
| `/redirect/:path` / `*` | — / NotFoundPage | 公开 | 跳转辅助 / 404 |

---

## 2. 接口通用约定

| 项 | 约定 |
| --- | --- |
| 基址 | `/api/v1`，可用 `VITE_API_BASE_URL` 覆盖；开发环境 Vite 代理到 `http://127.0.0.1:8188` |
| 响应信封 | 成功 `{ "code": "OK", "data": ... }`；错误 `{ "code": "XXX", "message": "中文提示" }` |
| 鉴权 | `Authorization: Bearer <accessToken>`；令牌存 `sessionStorage`（未勾选"保持登录"）或 `localStorage`（勾选） |
| 令牌失效 | `401 SESSION_EXPIRED` / `UNAUTHENTICATED` → 清令牌，跳 `/login?redirect=<当前地址>` |
| 分页 | `page`（默认 1）、`pageSize`（默认 20，上限 100）；返回 `{items,total,page,pageSize}`；部分列表接口直接返回数组 |
| 日期 | 业务日期 `YYYY-MM-DD`；时间戳为 ISO8601（UTC）；UI 按组织/用户时区展示 |
| 版本控制 | 任务、项目等可变资源带 `version`；`PATCH` 必须回传，冲突返回 409 `VERSION_CONFLICT`，UI 需提示刷新 |
| 组织切换 | 切组织后所有列表重新拉取；旧组织未完成请求要忽略（用 `teamId` 闭包校验），避免串数据 |
| 幂等/重试 | 创建类操作不做前端自动重试；SSE 断线 3 秒重连 |
| 上传 | 工作区文件仅 UTF-8 文本，单文件 ≤ 64 KiB |
| 限流 | 登录、MFA、找回等接口按 IP 限流，返回 429 时提示稍后再试 |

---

## 3. 页面功能与 API 明细

### 3.1 登录 / 注册 / 密码找回

**页面职责**：左侧品牌插画（桌面端），右侧表单；同一组件按路由切换 6 种模式（登录、注册、公司邀请、超管注册、忘记密码、重置密码）；MFA 为登录的第二步。

**功能点**

- 登录：邮箱 + 密码 + "保持登录"；成功后写入令牌并跳转 `redirect` 参数或 `/teammind/overview`。
- MFA：返回 `mfaRequired` 时切到验证码步骤，支持 6 位动态码或恢复码；可返回账号密码步骤。
- 注册：公开注册开关来自 `registration-status`；关闭时隐藏注册表单，显示“使用公司/团队邀请链接”的说明；密码 ≥ 12 位。
- 公司邀请：`/company-invite?code=...` 从 URL 读取邀请码，填写公司名称、类型、负责人姓名/邮箱/密码；创建成功即成为 `team_owner` 并自动登录。已有账号也可用原密码通过公司邀请再创建公司。
- 超管注册：只通过 `bootstrap-invite` 输出的完整链接 `/superadmin-setup?code=...` 进入；邀请码从 URL 读取，页面不再暴露输入框，登录页也没有“平台超管”入口。
- 找回：提交邮箱，始终提示"如果账号存在，重置说明将发送至邮箱"。
- 重置：读取 URL `token`，设置新密码（≥12 位）。
- 错误全部就地展示，不做全局弹窗轰炸。

| 方法 | 路径 | 关键参数 | 说明 |
| --- | --- | --- | --- |
| GET | `/auth/registration-status` | — | `{enabled}` 公开注册是否开启 |
| POST | `/auth/login` | `{email,password,remember}` | 成功 `{accessToken,expiresIn}`；需 MFA 时 `{mfaRequired:true,challengeToken,expiresIn:300}` |
| POST | `/auth/mfa/complete` | `{challengeToken,code}` | 动态码或恢复码，成功返回 `{accessToken,expiresIn}` |
| POST | `/auth/register` | `{email,name,password}` | 公开注册；关闭时 403 `REGISTRATION_DISABLED` |
| POST | `/auth/register-invited` | `{token,email,name,password}` | 邀请注册，返回 `{accessToken,teamId}` |
| POST | `/auth/register-superadmin` | `{code,email,name,password}` | 平台超管邀请码注册 |
| GET | `/auth/company-invite-status` | `?code=` | 公司邀请是否有效、剩余次数 |
| POST | `/auth/register-company` | `{code,companyName,companyKind,name,email,password}` | 邀请码创建公司 + 负责人；支持已有账号原密码 |
| POST | `/auth/forgot` | `{email}` | 开发模式可能返回 `resetToken` 便于本地调试 |
| POST | `/auth/reset` | `{token,password}` | 重置密码 |

### 3.2 邀请落地（JoinPage）

**功能点**：未登录时展示注册表单（姓名/邮箱/密码）→ 注册并接受邀请；已登录时展示"接受邀请"按钮；成功后刷新组织列表并进入工作台。邀请 token 失效/用过要给出明确提示。

| 方法 | 路径 | 关键参数 | 说明 |
| --- | --- | --- | --- |
| POST | `/auth/register-invited` | `{token,email,name,password}` | 新用户注册 + 入队 |
| POST | `/invitations/accept` | `{token}` | 已登录用户接受邀请 |
| GET | `/me/teams` | — | 接受后刷新组织列表 |

### 3.3 邮箱变更确认（EmailConfirmPage）

**功能点**：无会话也可打开（邮件链接可能在另一台设备）；进入即提交 token；成功提示"邮箱已更新，请用新邮箱重新登录"；失败展示原因与返回登录入口。

| 方法 | 路径 | 关键参数 | 说明 |
| --- | --- | --- | --- |
| POST | `/me/email-change/confirm` | `{token}` | 一次性 token，成功后旧会话失效 |

### 3.4 全局框架（Layout）

**布局结构**：固定顶栏（品牌、组织选择、新建组织、任务搜索、通知角标、主题切换、账号菜单）+ 固定侧栏（分组菜单，可折叠）+ 页面标签栏（ProTabBar）+ 面包屑 + 内容区 + 页脚。≤900px 侧栏变抽屉；≤640px 精简顶栏元素。

**功能点**

- 组织切换：下拉选择，切换后回总览；`activeId` 存 `sessionStorage`。
- 新建组织：名称（≥2 字）、类型（团队/公司）、描述、时区；创建后选中并刷新。
- 任务搜索：弹窗输入 → 跳 `/teammind/tasks?q=...`。
- 通知角标：未读数，SSE 推送时刷新 + 每 30 秒轮询兜底。
- 主题：`arco-theme` 属性 + `localStorage`；新前端应换成设计 token 方案。
- 页面标签：保留访问过的页面，支持关闭当前/其他/全部；切组织清空。
- 菜单按角色过滤；超管可见平台控制台。

| 方法 | 路径 | 关键参数 | 说明 |
| --- | --- | --- | --- |
| GET | `/me` | — | `{id,email,name,timezone,role,superAdmin}` |
| GET | `/me/teams` | — | `Team[]`：`{id,name,kind,description,timezone,role,isLead,status,members}` |
| POST | `/teams` | `{name,description,timezone,kind}` | 创建组织，返回 `{id}` |
| GET | `/me/notifications/unread-count` | — | `{count}` |
| GET | `/me/events` | SSE，Bearer 头 | 事件流；旧版只用来触发未读刷新 |
| POST | `/auth/logout` | — | 注销当前令牌 |

### 3.5 总览（OverviewPage）

**功能点**：组织名 + 版本标签；6 个统计卡（项目、任务、阻塞任务、待审批、成员、部门/小组）；近期 5 条任务表（标题、状态、负责人、截止日期，可跳详情）；部门/小组概况（成员数、项目数）；无组织、待审核两种空状态；手动刷新。

| 方法 | 路径 | 关键参数 | 说明 |
| --- | --- | --- | --- |
| GET | `/teams/{T}/summary` | — | `{projects,tasks,blocked,pending,members,departments,role}` |
| GET | `/teams/{T}/tasks` | `?pageSize=5` | 近期任务 |
| GET | `/teams/{T}/departments` | — | `{items:Department[]}` |

### 3.6 项目管理（ProjectsPage）

**功能点**

- 列表：项目名 + 描述、状态标签、所属部门、进度条、开始/截止日期、行内操作。
- 筛选：关键词、状态（规划中/进行中/已暂停/已完成/已归档）、部门（仅公司版）；分页 20/页。
- 创建/编辑：名称、状态、描述（≤10000 字）、开始/截止日期、部门（公司版）。
- 删除：管理员，二次确认。
- 变更提案：普通成员改项目走提案（审批前不生效），字段含项目字段 + 变更原因 + 影响说明。
- 跳转：任务列表按项目过滤、甘特图按项目过滤。
- 编辑权限：管理员，或该项目所属部门的负责人。

| 方法 | 路径 | 关键参数 | 说明 |
| --- | --- | --- | --- |
| GET | `/teams/{T}/projects` | `q,status,departmentId,page,pageSize` | `{items,total,page,pageSize}` |
| POST | `/teams/{T}/projects` | `{name,description,status,startDate,dueDate,departmentId}` | 公司版才允许 `departmentId` |
| PATCH | `/teams/{T}/projects/{P}` | 同上 + `version` | 版本冲突需刷新 |
| DELETE | `/teams/{T}/projects/{P}` | — | 管理员 |
| POST | `/teams/{T}/project-proposals` | `{projectId,name?,description?,status?,startDate?,dueDate?,departmentId?,reason,impactSummary}` | 提案，`reason` ≥5 字 |
| GET | `/teams/{T}/departments` | — | 部门下拉 |

项目状态枚举：`planning | active | paused | completed | archived`。

### 3.7 任务中心（TasksPage）

**功能点**

- 列表筛选：关键词、状态、项目、部门（公司版）、负责人（含"未分配"）、排序字段（创建时间/截止日期/优先级/标题）+ 方向；"我的任务"一键过滤当前用户。
- 列表列：任务（标题 + 里程碑标签）、状态、优先级、负责人、部门、进度、截止、操作。
- 创建：必须选项目；成员只能把任务指派给自己；优先级默认 medium。
- 编辑：标题、描述、负责人、优先级、状态、进度、开始/截止、里程碑；带 `version`。
- 删除：管理员，二次确认。
- 详情抽屉：基础信息 + 状态/进度快捷更新 + 评论（增删改）+ 依赖（增删，显示前置任务名）+ 协作者（增删）+ 版本历史时间线。
- 计划提案：普通成员修改计划走提案；审批前不影响正式任务。
- 深链：`/teammind/tasks?taskId=...` 直接打开详情；`?projectId=` 预过滤。

| 方法 | 路径 | 关键参数 | 说明 |
| --- | --- | --- | --- |
| GET | `/teams/{T}/tasks` | `q,status,projectId,departmentId,assigneeId,sortBy,sortDir,page,pageSize` | `sortBy`：`createdAt\|dueDate\|priority\|title\|assignee`；`sortDir`：`ASC\|DESC`；`assigneeId=unassigned` 查未分配 |
| GET | `/teams/{T}/tasks/{K}` | — | 任务详情 |
| POST | `/teams/{T}/tasks` | `{projectId,title,description,assigneeId,priority,startDate,dueDate,milestone}` | `assigneeId` 空则默认自己；成员只能指派自己 |
| PATCH | `/teams/{T}/tasks/{K}` | `{version,title?,description?,priority?,status?,progress?,assigneeId?,startDate?,dueDate?,milestone?}` | 版本冲突 409 |
| DELETE | `/teams/{T}/tasks/{K}` | — | 管理员 |
| GET | `/teams/{T}/tasks/{K}/comments` | — | 评论数组 |
| POST | `/teams/{T}/tasks/{K}/comments` | `{body}` | 新增评论 |
| PATCH | `/teams/{T}/tasks/{K}/comments/{id}` | `{body}` | 编辑自己的评论 |
| DELETE | `/teams/{T}/tasks/{K}/comments/{id}` | — | 删除评论 |
| GET | `/teams/{T}/tasks/{K}/dependencies` | — | 前置任务 ID 数组 |
| POST | `/teams/{T}/tasks/{K}/dependencies` | `{dependsOnTaskId}` | 增加依赖 |
| DELETE | `/teams/{T}/tasks/{K}/dependencies` | `{dependsOnTaskId}` | 删除依赖 |
| GET | `/teams/{T}/tasks/{K}/collaborators` | — | 协作者成员数组 |
| POST | `/teams/{T}/tasks/{K}/collaborators` | `{userId}` | 增加协作者 |
| DELETE | `/teams/{T}/tasks/{K}/collaborators/{U}` | — | 移除协作者 |
| GET | `/teams/{T}/tasks/{K}/history` | — | `[{version,source,createdAt}]` |
| POST | `/teams/{T}/plan-proposals` | `{taskId,title?,description?,priority?,status?,progress?,assigneeId?,startDate?,dueDate?,milestone?,reason,impactSummary}` | 计划变更提案 |

任务状态：`todo | doing | blocked | review | done | cancelled`；优先级：`low | medium | high | urgent`。

### 3.8 甘特图（GanttPage）

**功能点**

- 筛选：项目、部门、负责人、状态、日期区间（开始/结束）；`gantt=1` 拉取时间轴数据（不分页返回全量，`pageSize=100`）。
- 时间轴：按天渲染，最长 120 天；今日高亮；"今天"定位按钮；里程碑菱形标记；依赖关系以连线展示；任务条按起止日期定位。
- 排期编辑（新增）：拖拽任务条整体平移（保持工期）、拖动左右边缘调整开始/截止日期；管理员/负责人直接 `PATCH`（带 `version`），普通成员转为计划提案。拖拽与点击详情互斥，冲突时提示刷新。
- 依赖编辑（新增）：拖动任务条右侧圆点到另一任务建立依赖，点击连线二次确认后删除；自依赖与重复依赖在前端拦截。
- CSV 导出：浏览器本地生成（任务、状态、负责人、起止、进度），不调后端。
- AI 排期体检：提交当前筛选条件，返回分析文本 + 任务数。
- 只读分享：管理员可开启/关闭，范围=整个组织 | 单个项目 | 单个部门；可轮换链接（旧链接立即失效）；复制公开链接 `${origin}/share/gantt/{token}`。
- 公开页：无需登录，支持项目/状态筛选与"定位本月"，只展示安全字段。

| 方法 | 路径 | 关键参数 | 说明 |
| --- | --- | --- | --- |
| GET | `/teams/{T}/tasks` | `gantt=1,projectId,departmentId,assigneeId,status,dateFrom,dateTo,page,pageSize` | 甘特数据 |
| GET | `/teams/{T}/tasks/{K}/dependencies` | — | 依赖映射 |
| POST | `/teams/{T}/gantt-insights` | `{projectId,status,assigneeId,dateFrom,dateTo}` | `{analysis,taskCount}` |
| GET | `/teams/{T}/gantt-share` | `projectId` 或 `unitId` | `{enabled,scope,token?,updatedAt?}` |
| PATCH | `/teams/{T}/gantt-share` | `{enabled,projectId?,unitId?,rotate?}` | 管理员 |
| GET | `/public/gantt/{token}` | — | 公开只读数据；不含成员邮箱、任务描述、评论、内部 ID |

### 3.9 审批中心（ApprovalsPage）

**功能点**

- 三栏：任务计划提案、项目变更提案、成员身份提案；每类分"待处理 / 已处理"。
- 列表字段：提案对象、发起人、原因、影响、状态、时间；点击打开详情抽屉。
- 审批动作：通过（可同时调整字段，`edited_approved`）、驳回、要求补充（后两者必须填 ≥5 字说明）、重新提交。
- 详情展示变更前后对比（旧版为字段级 diff 表）。
- 入口仅管理员/负责人可见；处理人由后端再校验。

| 方法 | 路径 | 关键参数 | 说明 |
| --- | --- | --- | --- |
| GET | `/teams/{T}/plan-proposals` | — | 全部任务计划提案（≤100） |
| POST | `/teams/{T}/plan-proposals/{R}/decision` | `{decision,reason,可带计划字段}` | `decision`：`approved\|edited_approved\|rejected\|needs_info` |
| POST | `/teams/{T}/plan-proposals/{R}/resubmit` | `{reason,impactSummary,可带计划字段}` | 驳回/补充后重提 |
| GET | `/teams/{T}/project-proposals` | — | 项目变更提案 |
| POST | `/teams/{T}/project-proposals/{R}/decision` | 同上 | 同上 |
| POST | `/teams/{T}/project-proposals/{R}/resubmit` | 同上 | 同上 |
| GET | `/teams/{T}/member-proposals` | — | 成员身份提案 |
| POST | `/teams/{T}/member-proposals/{R}/decision` | `{decision,reason,role?,status?}` | 同上 |
| POST | `/teams/{T}/member-proposals/{R}/resubmit` | 同上 | 同上 |

### 3.10 部门 / 小组（DepartmentsPage）

**功能点**：列表展示名称、描述、负责人、成员数、项目数；新建/编辑（名称、描述、负责人）；删除（管理员，二次确认）；团队版文案显示"小组"，公司版显示"部门"；点击成员/项目数跳对应页面。

| 方法 | 路径 | 关键参数 | 说明 |
| --- | --- | --- | --- |
| GET | `/teams/{T}/departments` | — | `{items:[{id,name,description,leadUserId,leadName,memberCount,projectCount}]}` |
| POST | `/teams/{T}/departments` | `{name,description,leadUserId}` | 管理员/负责人 |
| PATCH | `/teams/{T}/departments/{D}` | 同上 | 同上 |
| DELETE | `/teams/{T}/departments/{D}` | — | 管理员 |
| GET | `/teams/{T}/members` | — | 负责人下拉 |

### 3.11 成员与邀请（MembersPage）

**功能点**：成员表（姓名、邮箱、角色、状态、部门、加入时间、操作）；编辑角色/状态/部门（管理员）；成员身份变更提案；邀请链接：创建（有效小时、最大使用次数）→ 复制 `${origin}/join/{token}` → 撤销；已有邀请列表显示使用次数与过期时间。

| 方法 | 路径 | 关键参数 | 说明 |
| --- | --- | --- | --- |
| GET | `/teams/{T}/members` | — | 数组 |
| PATCH | `/teams/{T}/members/{U}` | `{role,status,departmentId}` | 管理员；`departmentId` 仅公司版 |
| GET | `/teams/{T}/invitations` | — | 邀请列表 |
| POST | `/teams/{T}/invitations` | `{expiresInHours,maxUses}` | 返回 `{token,...}` |
| DELETE | `/teams/{T}/invitations/{id}` | — | 撤销 |
| PATCH | `/teams/{T}/departments/{D}/members/{U}` | `{departmentId}` | 调整部门归属 |
| POST | `/teams/{T}/member-proposals` | `{memberId,role?,status?,departmentId?,reason,impactSummary}` | 身份变更提案 |

成员角色：`member | team_admin | team_owner`；状态：`active | suspended | left`。

### 3.12 通知中心（NotificationsPage）

**功能点**：四个标签——全部通知、未读、提醒、AI 协作消息。通知显示标题、正文、时间、未读标记；单条已读、全部已读；提醒显示标题、正文、执行时间、状态；AI 协作消息显示来源与已读状态；加载失败以页面级错误提示。

| 方法 | 路径 | 关键参数 | 说明 |
| --- | --- | --- | --- |
| GET | `/me/notifications` | — | 最近 100 条数组 |
| PATCH | `/me/notifications/{N}` | — | 标记单条已读 |
| POST | `/me/notifications/read-all` | — | 全部已读 |
| GET | `/me/reminders` | — | 提醒列表 |
| GET | `/me/agent-messages` | — | AI 协作消息 |
| PATCH | `/me/agent-messages/{id}` | — | 标记已读 |

### 3.13 设置（SettingsPage）

七个标签页，均通过 `?tab=` 深链：`profile | security | notifications | ai | team | data | audit`。

**个人资料**：姓名、时区、只读邮箱；"修改邮箱"需要当前密码，发送确认邮件。

**账号安全**：修改密码（≥12 位，改完重新登录）；MFA 开关（开始设置 → 显示密钥/二维码 → 输入 6 位码启用 → 展示恢复码；关闭需密码 + 动态码）；登录设备列表（当前会话标记、撤销、刷新当前会话）。

**通知与提醒**：站内/邮件/浏览器推送/原生推送/微信提醒开关；免打扰起止；静音类型；摘要间隔（0=关闭）；Web Push 订阅/取消；创建定时提醒；投递历史（含微信 suppressed 原因）。

**AI 日报**：创建 AI 定时问候/日报；频率支持每天、工作日、每周、仅一次；可写补充要求；支持立即生成、暂停/启用、编辑、删除、查看最近生成内容。到点后由 worker 汇总团队快照，优先调用团队 AI 生成，失败时退回模板摘要，并通过通知偏好投递站内/微信/邮件/推送。

**组织设置**：组织名称、描述、时区；团队↔公司转换（二次确认）；权限隔离策略（`aiAccess`/`wechatAccess`/`dataIsolation`）；组织审计入口。

**个人数据**：导出 JSON（浏览器下载）；删除账号（需当前密码 + MFA + 输入"删除账号"确认）。

**审计日志**：管理员查看组织操作记录（分页）。

| 方法 | 路径 | 关键参数 | 说明 |
| --- | --- | --- | --- |
| GET/PATCH | `/me` | `{name,timezone}` | 个人资料 |
| POST | `/me/email-change` | `{email,currentPassword}` | 发送确认邮件 |
| POST | `/auth/password` | `{current,password}` | 改密后旧会话失效 |
| GET | `/me/mfa` | — | `{enabled,available,remainingRecoveryCodes}` |
| POST | `/me/mfa/setup` | `{currentPassword}` | 返回 `{secret,otpauthUrl}` |
| DELETE | `/me/mfa/setup` | — | 取消设置 |
| POST | `/me/mfa/enable` | `{code}` | 返回 `{recoveryCodes:[]}` |
| POST | `/me/mfa/disable` | `{currentPassword,code}` | 关闭 MFA |
| POST | `/auth/refresh` | — | 换新令牌 `{accessToken,expiresIn}` |
| GET | `/auth/sessions` | — | 登录设备 |
| DELETE | `/auth/sessions/{id}` | — | 撤销设备 |
| GET/PATCH | `/me/notification-preferences` | `{enabled,emailEnabled,pushEnabled,nativePushEnabled,wechatEnabled,quietStart,quietEnd,mutedTypes,digestMinutes}` | 通知偏好 |
| GET | `/me/notification-channels` | — | `{emailConfigured,emailEnabled,pushConfigured,pushEnabled,pushPublicKey,pushSubscriptions,nativePushConfigured,nativePushEnabled,nativeDevices,deliveries}` |
| POST | `/me/notification-channels/push-subscriptions` | PushSubscription JSON | 订阅浏览器推送 |
| DELETE | `/me/notification-channels/push-subscriptions` | `{endpoint}` | 取消订阅 |
| GET | `/me/notification-deliveries` | — | 投递历史（含 `wechat/suppressed` 原因） |
| GET/POST | `/me/reminders` | `{teamId?,title,body,runAt,resourceType?,resourceId?}` | 提醒 |
| DELETE | `/me/reminders/{id}` | — | 取消提醒 |
| GET/POST | `/me/ai-proactive` | `{kind,cadence,time,weekday,timezone,prompt,teamId,enabled,runAt?}` | AI 定时问候/日报列表、创建 |
| PATCH | `/me/ai-proactive/{id}` | 同上 | 编辑/暂停/启用 |
| DELETE | `/me/ai-proactive/{id}` | — | 删除计划 |
| POST | `/me/ai-proactive/{id}/run` | — | 立即加入生成队列 |
| GET | `/teams/{T}` | — | 组织详情 |
| PATCH | `/teams/{T}` | `{name,description,timezone}` | 管理员 |
| PATCH | `/teams/{T}/kind` | `{kind}` | 管理员，团队↔公司 |
| GET/PATCH | `/teams/{T}/access-policy` | `{aiAccess,wechatAccess,dataIsolation}` | 管理员 |
| GET | `/me/data-export` | — | 个人数据 JSON |
| POST | `/me/account` | `{currentPassword,mfaCode,confirmation}` | 删除账号 |
| GET | `/teams/{T}/audit-log` | `page,pageSize` | 管理员 |

### 3.14 AI 助手（AssistantPage）

**功能点**

- 左侧会话列表：新建（私人 / 带团队上下文，需明确勾选确认）、选择、删除；显示标题与更新时间。
- 主区：消息流（用户/AI 气泡、Markdown 渲染并 DOMPurify 消毒、流式逐字追加、可中止）、输入框（Ctrl/⌘+Enter 发送）。
- 开关：自动工具、允许联网（每次会话级别）。
- 服务状态：`/me/ai/status` 未配置时给出提示。
- 会话摘要：查看/重新生成。
- 记忆候选：把单条消息保存为候选；或从整段对话批量提取候选。
- 智能体执行：任务指令 + 逐次授权（团队上下文、联网、Python、Shell、团队知识候选、保存工作区文件、选择工作区文件、临时文本文件），结果以 JSON 展示。
- 多智能体协作：指令 + 团队确认 + 联网授权。
- 工作区文件弹窗：列表、新建/编辑文本、导入本地文本、删除；限 64 KiB。

| 方法 | 路径 | 关键参数 | 说明 |
| --- | --- | --- | --- |
| GET | `/me/ai/status` | — | `{provider,configured,agentTools,sandboxAvailable,shellAvailable,webAvailable,workspaceAvailable}` |
| GET | `/me/conversations` | — | 会话数组 |
| POST | `/me/conversations` | `{teamId?,confirmTeam}` | 创建；带团队必须 `confirmTeam:true` |
| DELETE | `/me/conversations/{C}` | — | 删除会话 |
| GET | `/me/conversations/{C}/messages` | — | 消息数组 |
| POST | `/me/conversations/{C}/stream` | `{content,confirmTeam,autoTools,allowWebAccess}` | SSE：`event: delta` + `data: {"content":...}`，结束再拉一次消息 |
| GET/POST | `/me/conversations/{C}/summary` | — | 读取/生成摘要 |
| POST | `/me/conversations/{C}/agent` | `{instruction,confirmTeam,allowCodeExecution,allowShellExecution,allowTeamKnowledgeCandidate,allowWebAccess,persistWorkspaceFiles,workspaceFileIds,files:[{name,content}]}` | 单智能体执行，返回结构化结果 |
| POST | `/me/conversations/{C}/collaborate` | `{instruction,confirmTeam,allowWebAccess}` | 多智能体协作 |
| POST | `/me/agent/tools/execute` | `{tool:"list_team_tasks",teamId,confirmTeam:true,limit}` | 只读工具调用 |
| POST | `/me/conversations/{C}/messages/{id}/memory-candidate` | `{category,content}` | 单条消息存为候选 |
| POST | `/me/conversations/{C}/extract-memory-candidates` | `{confirmTeam}` | 整段对话提取候选 |
| GET | `/me/agent/workspace/files` | — | `{files:[{id,name,size,updatedAt}]}` |
| POST | `/me/agent/workspace/files` | `{name,content}` | 新建 |
| GET | `/me/agent/workspace/files/{id}` | — | 读取 |
| PUT | `/me/agent/workspace/files/{id}` | `{name,content}` | 保存 |
| DELETE | `/me/agent/workspace/files/{id}` | — | 删除 |

### 3.15 记忆与知识（MemoriesPage）

**功能点**：个人记忆 / 团队知识两个标签；类别（profile、experience、current_work、operational_knowledge）；内容、复核日期、到期日期；已确认/待确认状态；来源对话/消息可回链；团队知识仅管理员可写（UI 提示并由后端校验）；增删改。

| 方法 | 路径 | 关键参数 | 说明 |
| --- | --- | --- | --- |
| GET/POST | `/me/memories` | `{category,content,reviewAt?,expiresAt?}` | 个人记忆 |
| PATCH/DELETE | `/me/memories/{id}` | `{category?,content?,reviewAt?,expiresAt?,confirmed?}` | 更新/删除 |
| GET/POST | `/teams/{T}/memories` | 同上 | 团队知识（管理员写） |
| PATCH/DELETE | `/teams/{T}/memories/{id}` | 同上 | 更新/删除 |

### 3.16 AI 供应商管理（AiAdminPage）

**功能点**：超管看"平台池"，组织管理员看"当前组织"；供应商表（名称、Base URL、模型、状态、优先级、失败次数、熔断时间、最后错误、是否有密钥）；新增/编辑（名称、Base URL、API Key、模型列表、启用、优先级、平台池可指定组织 ID）；从供应商拉取模型；"测试连接"；"重置熔断"；删除。密钥永不回显，只显示"已配置"。

| 方法 | 路径 | 关键参数 | 说明 |
| --- | --- | --- | --- |
| GET/POST | `/platform/ai/providers` | `{teamId?,name,baseUrl,apiKey,models,enabled,priority}` | 平台池；`teamId` 空=全平台 |
| PATCH/DELETE | `/platform/ai/providers/{id}` | 同上，`apiKey` 留空保持 | 更新/删除 |
| POST | `/platform/ai/models` | `{baseUrl,apiKey,id?}` | 拉模型列表 |
| POST | `/platform/ai/providers/{id}/test` | — | 连通性测试 |
| GET/POST | `/teams/{T}/ai/providers` | 同上（不需要 `teamId`） | 团队池 |
| PATCH/DELETE | `/teams/{T}/ai/providers/{id}` | 同上 | 更新/删除 |
| POST | `/teams/{T}/ai/models` | `{baseUrl,apiKey,id?}` | 拉模型 |
| POST | `/teams/{T}/ai/providers/{id}/test` | — | 测试 |

### 3.17 微信渠道（WechatPage）

**功能点**：渠道状态卡（模式、消息服务是否就绪、AI 服务模式）；我的绑定列表（provider、状态、绑定时间、解绑）；扫码绑定：创建会话 → 展示二维码 + 可复制内容 → 每 2.5 秒轮询状态 → 需要验证码时输入 → 取消；终态（confirmed/expired/cancelled/failed）停止轮询并刷新。

| 方法 | 路径 | 关键参数 | 说明 |
| --- | --- | --- | --- |
| GET | `/me/channels/wechat` | — | `{mode,messagingReady,aiMode,bindings[]}`；绑定项含 `hasContext`、`lastInboundAt`、`notificationsReady` |
| POST | `/me/channels/wechat/sessions` | `{teamId}` | `{id,qrContent,expiresAt,status}` |
| GET | `/me/channels/wechat/sessions/{id}` | — | 轮询状态 |
| POST | `/me/channels/wechat/sessions/{id}/verify` | `{code}` | 提交验证码 |
| DELETE | `/me/channels/wechat/sessions/{id}` | — | 取消扫码 |
| DELETE | `/me/channels/wechat/{bindingId}` | — | 解绑 |
| POST | `/me/channels/wechat/test` | — | 用当前绑定的最近联系人/上下文发送一条测试文本，验证主动消息通道 |

状态：`pending | scaned | verify_code_blocked | confirmed | confirmed_simulated | expired | cancelled | failed`（以接口实际返回为准）。

### 3.18 平台控制台（PlatformPage，仅超管）

**功能点**：指标卡（组织总数、待审核、活跃、安全告警）；组织审核表（名称、类型、状态、创建人、成员数、项目数、创建时间、通过/驳回）；筛选（关键词、状态、类型）+ 分页 20；审核记录；安全告警；公司邀请（生成/复制完整链接、有效期、可用次数、备注、撤销，邀请码仅创建时展示）；运行配置表单（开放注册、公开 URL、SMTP、语音 STT、VAPID、微信网关），密钥类字段只写不回显。

**平台超管创建组织**：登录后点顶栏「+」直接创建公司/团队，后端对 `super_admin` 跳过“一人一公司”限制，创建结果立即 active。公司邀请则用于让外部负责人自己创建公司。

| 方法 | 路径 | 关键参数 | 说明 |
| --- | --- | --- | --- |
| GET | `/platform/overview` | — | `{teams:{total,pending,active,...},audit:{...},alerts:{active,...}}` |
| GET | `/platform/teams` | `q,status,kind,page,pageSize` | 组织列表 |
| POST | `/platform/teams/{T}/decision` | `{decision:"approved"\|"rejected",reason}` | 审核 |
| GET | `/platform/audit-log` | `page,pageSize` | 平台审计 |
| GET | `/platform/security-alerts` | — | 告警数组 |
| GET/PATCH | `/platform/settings` | `registrationEnabled,publicUrl,smtpHost,smtpPort,smtpUser,smtpPassword,smtpFrom,smtpTlsInsecure,sttProvider,sttBaseUrl,sttApiKey,sttModel,sttLanguage,vapidSubject,wechatMode,wechatGatewayUrl,wechatBridgeToken` | GET 只返回 `*Set` 标志 |

### 3.19 公开甘特图（GanttSharePage）

**功能点**：无登录访问；展示项目名/范围、更新时间；项目与状态筛选；"定位本月"；只读时间轴；不出现成员邮箱、任务描述、评论和内部 ID；token 失效显示友好错误。

| 方法 | 路径 | 关键参数 | 说明 |
| --- | --- | --- | --- |
| GET | `/public/gantt/{token}` | — | 公开只读数据 |

---

## 4. 跨页客户端行为

| 行为 | 实现要点 | 重建建议 |
| --- | --- | --- |
| SSE 实时刷新 | `fetch('/me/events')` 带 Bearer，读流即刷新未读；断线 3 秒重连；另有 30 秒轮询兜底 | 抽成 `useLiveEvents()`，页面卸载时 abort |
| 未读角标 | `GET /me/notifications/unread-count`，路由变化、SSE 事件、轮询三处触发 | 统一到全局 store，避免重复请求 |
| 主题 | `document.body[arco-theme]` + `localStorage` | 改为 CSS 变量/token 双主题 |
| 页面标签 | 访问即入栈，切组织清空 | 保留，作为工作台核心体验 |
| 任务搜索 | 顶栏弹窗 → `/tasks?q=` | 可升级为全局命令面板（⌘K） |
| CSV 导出 | 前端拼 CSV + Blob 下载 | 保留；大数据量考虑后端导出 |
| Markdown | `marked` + `DOMPurify` | 必须保留消毒，禁止 `v-html` 裸用 |
| 二维码 | `qrcode.vue` 渲染 `qrContent` | 可替换为目标库内置 QR |
| Web Push | 注册 `/sw.js`，`pushManager.subscribe`，VAPID 公钥转 Uint8Array | Service Worker 需随新前端重写并保留通知点击处理 |
| 本地存储键 | `teammind.accessToken`、`teammind.teamId`、`teammind.theme`、`teammind.menuCollapsed` | 重建时统一命名空间 |
| 请求取消 | 切组织/切页面时 `AbortController` | 新前端用请求库的 signal 统一处理 |

---

## 5. 数据模型与枚举

```ts
User        { id, email, name, timezone, role, superAdmin }
Team        { id, name, kind: 'team'|'company', description, timezone, role, isLead, status, members }
Department  { id, name, description, leadUserId, leadName, memberCount, projectCount }
Member      { id, name, email, role, status, departmentId?, departmentName?, joinedAt? }
Project     { id, name, description, status, departmentId, departmentName, startDate, dueDate, progress, version }
Task        { id, projectId, title, description, status, priority, assigneeId, assigneeName, departmentId?, departmentName?, startDate, dueDate, progress, milestone, version, createdBy }
Page<T>     { items: T[], total, page, pageSize }
Notice      { id, teamId, type, title, body, read, createdAt }
Reminder    { id, teamId, title, body, runAt, status, attempts, createdAt }
Memory      { id, category, content, confirmed, reviewAt?, expiresAt?, sourceMessageId?, sourceConversationId?, sourceExcerpt? }
Conversation{ id, teamId, title, visibility, updatedAt }
ChatMessage { id, role: 'user'|'assistant', content, provider?, createdAt? }
```

| 枚举 | 取值 |
| --- | --- |
| 组织状态 | `active / pending_review / suspended` |
| 组织类型 | `team / company` |
| 成员角色 | `member / team_admin / team_owner` |
| 成员状态 | `active / suspended / left` |
| 项目状态 | `planning / active / paused / completed / archived` |
| 任务状态 | `todo / doing / blocked / review / done / cancelled` |
| 任务优先级 | `low / medium / high / urgent` |
| 提案状态 | `pending / approved / edited_approved / rejected / needs_info`（以接口为准） |
| 记忆类别 | `profile / experience / current_work / operational_knowledge` |
| 通知渠道 | `in_app / email / web_push / native_push / wechat` |
| 甘特分享范围 | `all / <projectId> / unit:<departmentId>` |

---

## 6. 后端已具备、但旧前端未接入的能力

这些接口在后端存在，重建时可选做（能显著提升产品完整度）：

| 能力 | 接口 | 价值 |
| --- | --- | --- |
| 平台指标/就绪检查 | `GET /platform/metrics`、`GET /platform/readiness`、`POST /platform/readiness/test-email` | 运维面板、上线自检 |
| 组织权限快照 | `GET /teams/{T}/permissions`、`GET /teams/{T}/authz` | 前端精细化按钮权限，而不是只看角色 |
| 我可访问的组织 | `GET /me/authorized-teams` | 跨组织/代管场景 |
| 智能体运行记录 | `GET /me/agent-runs`、`GET /me/agent-runs/{id}` | 长任务可视化、断线恢复 |
| 沙箱会话 | `GET /me/sandbox/sessions`、`DELETE /me/sandbox/sessions/{id}` | 资源回收、会话面板 |
| 原生推送设备 | `POST/GET /me/notification-channels/native-devices`、`DELETE /{id}` | 移动端/桌面壳推送（旧版代码已移除） |

---

## 7. 重建必须保留的规则（踩坑清单）

1. **公开链路不依赖会话**：邀请注册、邀请接受、邮箱确认、密码重置、公开甘特图都能在无令牌/令牌过期的浏览器里打开；邮箱确认甚至可能在另一台设备完成。
2. **团队上下文必须显式确认**：创建团队会话、发消息、智能体执行、记忆提取都要 `confirmTeam:true`；这是隐私边界，不能默认勾选。
3. **逐次授权**：联网、Python、Shell、团队知识候选、持久化文件均为单次操作授权，不做"记住本次选择"式的默认放行。
4. **审批不是装饰**：普通成员改任务计划/项目/成员身份必须走提案，UI 不得直接 PATCH 成功；审批通过前正式数据不变。
5. **版本冲突要可见**：任务/项目 PATCH 带 `version`，409 时提示"数据已被他人修改，请刷新"，不要静默覆盖。
6. **切组织要防串数据**：异步请求回来先比对 `teamId`，不一致直接丢弃；页面组件用 `:key="teamId"` 强制重建。
7. **只读分享最小化字段**：公开甘特图不得泄露邮箱、描述、评论、内部 ID；轮换 token 立即失效旧链接。
8. **密钥只写不读**：SMTP 密码、STT Key、微信 Bridge Token、AI API Key 只回显 `*Set` 布尔值；编辑时留空表示保持原值。
9. **密码策略**：注册/重置/改密均 ≥12 位；改密、关 MFA、删号后必须清理本地令牌并回登录页。
10. **销毁性操作二次确认**：删除项目/任务/部门/成员/账号要有明确确认文案，删除账号还需输入固定短语。
11. **移动端**：≤900px 侧栏抽屉化；表格需横向滚动或卡片化；抽屉/弹窗宽度不得超出视口。
12. **空状态与错误态**：无组织、待审核、无权限、无数据、AI 未配置、微信未配置都要有专门文案，而不是空白页。

---

## 8. 重建验收清单

- [ ] 登录（含 MFA）、注册开关、邀请注册/接受、找回/重置、邮箱确认全链路通过。
- [ ] 组织创建、切换、待审核态、团队↔公司转换、权限隔离策略生效。
- [ ] 总览统计与近期任务、部门概况数据正确。
- [ ] 项目 CRUD + 筛选分页 + 变更提案 + 审批后生效。
- [ ] 任务 CRUD + 筛选排序 + 详情抽屉（评论/依赖/协作者/历史）+ 版本冲突提示 + 计划提案。
- [ ] 甘特图渲染（含里程碑、依赖、今日线、120 天上限）、CSV 导出、AI 体检、分享开启/轮换/关闭、公开页。
- [ ] 审批三类提案的通过/改后通过/驳回/要求补充/重提。
- [ ] 部门与成员管理、邀请链接创建/复制/撤销。
- [ ] 通知已读、全部已读、提醒、AI 协作消息、未读角标实时刷新。
- [ ] 设置七个标签页全部可用；Web Push 订阅/取消；AI 日报计划创建/暂停/立即生成；数据导出；删号。
- [ ] AI 助手：会话、流式、摘要、记忆候选、智能体执行、多智能体协作、工作区文件。
- [ ] 记忆/知识增删改与确认；团队知识权限提示。
- [ ] AI 供应商增删改查、拉模型、测试连接、熔断展示。
- [ ] 微信扫码绑定全状态机与解绑。
- [ ] 平台控制台：审核、审计、告警、运行配置（密钥不回显）。
- [ ] 深色/浅色主题、响应式（375/768/1280/1440）、无控制台报错、无内存泄漏（SSE/定时器清理）。

---

## 附录 A：API 全量索引

> 除标注"公开"外均需 Bearer 令牌。组织级接口要求调用者是该组织 `active` 成员。

### A.1 认证与账号

| 方法 | 路径 | 权限 |
| --- | --- | --- |
| GET | `/auth/registration-status` | 公开 |
| POST | `/auth/login` | 公开 |
| POST | `/auth/mfa/complete` | 公开（challengeToken） |
| POST | `/auth/register` | 公开（受开关） |
| POST | `/auth/register-invited` | 公开（邀请 token） |
| POST | `/auth/register-superadmin` | 公开（邀请码） |
| POST | `/auth/forgot` | 公开 |
| POST | `/auth/reset` | 公开（重置 token） |
| POST | `/auth/logout` | 登录 |
| POST | `/auth/refresh` | 登录 |
| POST | `/auth/password` | 登录 |
| GET | `/auth/sessions` | 登录 |
| DELETE | `/auth/sessions/{id}` | 登录 |
| GET/PATCH | `/me` | 登录 |
| POST | `/me/email-change` | 登录 |
| POST | `/me/email-change/confirm` | 公开（一次性 token） |
| GET | `/me/mfa` | 登录 |
| POST/DELETE | `/me/mfa/setup` | 登录 |
| POST | `/me/mfa/enable` `/me/mfa/disable` | 登录 |
| GET | `/me/data-export` | 登录 |
| POST | `/me/account` | 登录 |
| GET | `/me/teams` | 登录 |
| GET | `/me/authorized-teams` | 登录 |
| GET | `/me/events` | 登录（SSE） |
| POST | `/invitations/accept` | 登录 |

### A.2 组织

| 方法 | 路径 | 权限 |
| --- | --- | --- |
| POST | `/teams` | 登录 |
| GET/PATCH | `/teams/{T}` | 成员 / 管理员 |
| PATCH | `/teams/{T}/kind` | 管理员 |
| GET | `/teams/{T}/permissions` `/teams/{T}/authz` | 成员 |
| GET | `/teams/{T}/summary` | 成员 |
| GET/PATCH | `/teams/{T}/access-policy` | 成员 / 管理员 |
| GET | `/teams/{T}/audit-log` | 管理员 |
| GET/POST | `/teams/{T}/departments` | 成员 / 管理员（或负责人） |
| PATCH/DELETE | `/teams/{T}/departments/{D}` | 管理员（或负责人） |
| PATCH | `/teams/{T}/departments/{D}/members/{U}` | 管理员 |
| GET | `/teams/{T}/members` | 成员 |
| PATCH | `/teams/{T}/members/{U}` | 管理员 |
| GET/POST | `/teams/{T}/invitations` | 管理员 |
| DELETE | `/teams/{T}/invitations/{id}` | 管理员 |

### A.3 项目 / 任务 / 甘特

| 方法 | 路径 | 权限 |
| --- | --- | --- |
| GET/POST | `/teams/{T}/projects` | 成员 |
| PATCH/DELETE | `/teams/{T}/projects/{P}` | 管理员/负责人 |
| GET/POST | `/teams/{T}/tasks` | 成员 |
| GET/PATCH/DELETE | `/teams/{T}/tasks/{K}` | 成员/管理员 |
| GET/POST | `/teams/{T}/tasks/{K}/comments` | 成员 |
| PATCH/DELETE | `/teams/{T}/tasks/{K}/comments/{id}` | 作者/管理员 |
| GET/POST/DELETE | `/teams/{T}/tasks/{K}/dependencies` | 成员 |
| GET/POST | `/teams/{T}/tasks/{K}/collaborators` | 成员 |
| DELETE | `/teams/{T}/tasks/{K}/collaborators/{U}` | 成员 |
| GET | `/teams/{T}/tasks/{K}/history` | 成员 |
| GET/POST | `/teams/{T}/plan-proposals` | 成员 |
| POST | `/teams/{T}/plan-proposals/{R}/decision` `/resubmit` | 审批人/提案人 |
| GET/POST | `/teams/{T}/project-proposals` | 成员 |
| POST | `/teams/{T}/project-proposals/{R}/decision` `/resubmit` | 审批人/提案人 |
| GET/POST | `/teams/{T}/member-proposals` | 成员 |
| POST | `/teams/{T}/member-proposals/{R}/decision` `/resubmit` | 审批人/提案人 |
| POST | `/teams/{T}/gantt-insights` | 成员 |
| GET/PATCH | `/teams/{T}/gantt-share` | 成员 / 管理员 |
| GET | `/public/gantt/{token}` | 公开 |

### A.4 通知 / 提醒 / 渠道

| 方法 | 路径 |
| --- | --- |
| GET | `/me/notifications` |
| GET | `/me/notifications/unread-count` |
| PATCH | `/me/notifications/{N}` |
| POST | `/me/notifications/read-all` |
| GET/PATCH | `/me/notification-preferences` |
| GET | `/me/notification-deliveries` |
| GET | `/me/notification-channels` |
| POST/DELETE | `/me/notification-channels/push-subscriptions` |
| GET/POST | `/me/notification-channels/native-devices` |
| DELETE | `/me/notification-channels/native-devices/{id}` |
| GET/POST | `/me/reminders` |
| DELETE | `/me/reminders/{id}` |
| GET | `/me/agent-messages` |
| PATCH | `/me/agent-messages/{id}` |

### A.5 AI / 记忆 / 工作区

| 方法 | 路径 |
| --- | --- |
| GET | `/me/ai/status` |
| GET/POST | `/me/conversations` |
| DELETE | `/me/conversations/{C}` |
| GET | `/me/conversations/{C}/messages` |
| POST | `/me/conversations/{C}/stream` |
| GET/POST | `/me/conversations/{C}/summary` |
| POST | `/me/conversations/{C}/agent` |
| POST | `/me/conversations/{C}/collaborate` |
| POST | `/me/conversations/{C}/messages/{id}/memory-candidate` |
| POST | `/me/conversations/{C}/extract-memory-candidates` |
| POST | `/me/agent/tools/execute` |
| GET/POST | `/me/agent/workspace/files` |
| GET/PUT/DELETE | `/me/agent/workspace/files/{id}` |
| GET | `/me/agent-runs` `/me/agent-runs/{id}` |
| GET/DELETE | `/me/sandbox/sessions` `/me/sandbox/sessions/{id}` |
| GET/POST | `/me/memories` |
| PATCH/DELETE | `/me/memories/{id}` |
| GET/POST | `/teams/{T}/memories` |
| PATCH/DELETE | `/teams/{T}/memories/{id}` |
| GET/POST | `/me/channels/wechat` → `/me/channels/wechat/sessions` |
| GET/DELETE | `/me/channels/wechat/sessions/{id}` |
| POST | `/me/channels/wechat/sessions/{id}/verify` |
| DELETE | `/me/channels/wechat/{bindingId}` |

### A.6 平台 / AI 池

| 方法 | 路径 |
| --- | --- |
| GET | `/platform/metrics` `/platform/overview` `/platform/readiness` |
| POST | `/platform/readiness/test-email` |
| GET | `/platform/security-alerts` `/platform/audit-log` |
| GET | `/platform/teams` |
| POST | `/platform/teams/{T}/decision` |
| GET/PATCH | `/platform/settings` |
| GET/POST | `/platform/ai/providers` |
| PATCH/DELETE | `/platform/ai/providers/{id}` |
| POST | `/platform/ai/models` |
| POST | `/platform/ai/providers/{id}/test` |
| GET/POST | `/teams/{T}/ai/providers` |
| PATCH/DELETE | `/teams/{T}/ai/providers/{id}` |
| POST | `/teams/{T}/ai/models` |
| POST | `/teams/{T}/ai/providers/{id}/test` |

---

## 附录 B：被删除的前端资产清单（重建时可作参考）

| 目录/文件 | 内容 |
| --- | --- |
| `src/views/team/*.vue` | 总览、任务、项目、甘特、审批、部门、成员、通知、助手、记忆、设置、微信、AI 管理、平台共 14 页 |
| `src/views/auth/*.vue` | 登录/注册/找回（AuthPage）、邀请加入、邮箱确认 |
| `src/views/public/*.vue` | 公开甘特图、404 |
| `src/layout/default-layout.vue` | 顶栏、侧栏、抽屉、面包屑、搜索、建组织、SSE、主题 |
| `src/components/ProTabBar.vue` | 页面标签栏 |
| `src/components/GanttTimeline.vue` | 自研时间轴（28px/天，120 天上限） |
| `src/components/WorkspaceFiles.vue` | AI 工作区文件弹窗 |
| `src/api/client.ts` `types.ts` | axios 封装、令牌、错误归一化、类型 |
| `src/store/session.ts` | 用户/组织/Pinia 会话 |
| `src/router/*` | 路由与权限守卫 |
| `src/utils/labels.ts` | 状态/优先级中文映射 |
| `src/assets/style/global.css` | 页面栅格、表格操作、响应式断点 |
| `public/sw.js` | Web Push Service Worker |
| `index.html` `vite.config.ts` `tsconfig.json` `package.json` | 构建与依赖（Vue 3 + Vite + Arco Design Vue） |

> 新前端的技术栈与 UI 库尚未定稿；选型对比与可交互预览见 [`docs/ui-preview/index.html`](ui-preview/index.html)。
