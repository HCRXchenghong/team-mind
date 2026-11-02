import { createRouter, createWebHistory } from 'vue-router';
import NProgress from 'nprogress';
import { getToken } from '@/api/client';
import { useSession } from '@/store/session';

const router = createRouter({
  history: createWebHistory(),
  scrollBehavior: () => ({ top: 0 }),
  routes: [
    { path: '/', redirect: '/teammind/overview' },
    {
      path: '/login',
      component: () => import('@/views/auth/AuthPage.vue'),
      meta: { public: true, title: '登录', mode: 'login' },
    },
    {
      path: '/register',
      component: () => import('@/views/auth/AuthPage.vue'),
      meta: { public: true, title: '注册', mode: 'register' },
    },
    {
      path: '/company-invite',
      component: () => import('@/views/auth/AuthPage.vue'),
      meta: { public: true, title: '创建公司', mode: 'company' },
    },
    {
      path: '/superadmin-setup',
      component: () => import('@/views/auth/AuthPage.vue'),
      meta: { public: true, title: '创建平台超管', mode: 'superadmin' },
    },
    {
      path: '/forgot-password',
      component: () => import('@/views/auth/AuthPage.vue'),
      meta: { public: true, title: '找回密码', mode: 'forgot' },
    },
    {
      path: '/reset-password',
      component: () => import('@/views/auth/AuthPage.vue'),
      meta: { public: true, title: '重置密码', mode: 'reset' },
    },
    {
      path: '/verify-email-change',
      component: () => import('@/views/auth/EmailConfirmPage.vue'),
      meta: { public: true, title: '确认邮箱' },
    },
    {
      path: '/join/:token',
      component: () => import('@/views/auth/JoinPage.vue'),
      meta: { public: true, title: '加入组织' },
    },
    {
      path: '/share/gantt/:token',
      component: () => import('@/views/public/GanttSharePage.vue'),
      meta: { public: true, title: '公开甘特图' },
    },
    {
      path: '/teammind',
      component: () => import('@/layout/DefaultLayout.vue'),
      children: [
        { path: '', redirect: '/teammind/overview' },
        { path: 'overview', component: () => import('@/views/team/OverviewPage.vue'), meta: { title: '总览' } },
        { path: 'tasks', component: () => import('@/views/team/TasksPage.vue'), meta: { title: '任务中心' } },
        { path: 'projects', component: () => import('@/views/team/ProjectsPage.vue'), meta: { title: '项目管理' } },
        { path: 'gantt', component: () => import('@/views/team/GanttPage.vue'), meta: { title: '甘特图' } },
        {
          path: 'approvals',
          component: () => import('@/views/team/ApprovalsPage.vue'),
          meta: { title: '审批中心', manage: true },
        },
        {
          path: 'departments',
          component: () => import('@/views/team/DepartmentsPage.vue'),
          meta: { title: '部门与小组', manage: true },
        },
        {
          path: 'members',
          component: () => import('@/views/team/MembersPage.vue'),
          meta: { title: '成员管理', manage: true },
        },
        {
          path: 'notifications',
          component: () => import('@/views/team/NotificationsPage.vue'),
          meta: { title: '通知中心' },
        },
        { path: 'assistant', component: () => import('@/views/team/AssistantPage.vue'), meta: { title: 'AI 助手' } },
        { path: 'memories', component: () => import('@/views/team/MemoriesPage.vue'), meta: { title: '记忆与知识' } },
        { path: 'settings', component: () => import('@/views/team/SettingsPage.vue'), meta: { title: '设置' } },
        { path: 'wechat', component: () => import('@/views/team/WechatPage.vue'), meta: { title: '微信渠道' } },
        {
          path: 'ai-admin',
          component: () => import('@/views/team/AiAdminPage.vue'),
          meta: { title: 'AI 管理', manage: true },
        },
        {
          path: 'platform',
          component: () => import('@/views/team/PlatformPage.vue'),
          meta: { title: '平台控制台', superAdmin: true },
        },
      ],
    },
    {
      path: '/:pathMatch(.*)*',
      component: () => import('@/views/public/NotFoundPage.vue'),
      meta: { public: true, title: '页面不存在' },
    },
  ],
});

router.beforeEach(async (to) => {
  NProgress.start();
  document.title = `${String(to.meta.title || '工作台')} · TeamMind`;

  if (to.meta.public) return true;

  if (!getToken()) {
    return { path: '/login', query: { redirect: to.fullPath } };
  }

  const session = useSession();
  if (!session.loaded) {
    try {
      await session.load();
    } catch {
      session.reset();
      return { path: '/login', query: { redirect: to.fullPath } };
    }
  }

  if (to.meta.superAdmin && !session.isSuperAdmin) return '/teammind/overview';
  if (to.meta.manage && !session.canManage) return '/teammind/overview';
  return true;
});

router.afterEach(() => NProgress.done());
router.onError(() => NProgress.done());

export default router;
