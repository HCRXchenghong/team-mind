import { DEFAULT_LAYOUT } from '../base';
import type { AppRouteRecordRaw } from '../types';

const TEAMMIND: AppRouteRecordRaw = {
  path: '/teammind',
  name: 'teammind',
  component: DEFAULT_LAYOUT,
  redirect: '/teammind/overview',
  meta: {
    locale: 'menu.teammind',
    requiresAuth: true,
    icon: 'icon-apps',
    order: 1,
  },
  children: [
    {
      path: 'overview',
      name: 'TeamOverview',
      component: () => import('@/views/team/OverviewPage.vue'),
      meta: {
        locale: 'menu.teammind.overview',
        requiresAuth: true,
        roles: ['*'],
      },
    },
    {
      path: 'projects',
      name: 'TeamProjects',
      component: () => import('@/views/team/pages/ProjectsPage.vue'),
      meta: {
        locale: 'menu.teammind.projects',
        requiresAuth: true,
        roles: ['*'],
      },
    },
    {
      path: 'tasks',
      name: 'TeamTasks',
      component: () => import('@/views/team/pages/TasksPage.vue'),
      meta: { locale: 'menu.teammind.tasks', requiresAuth: true, roles: ['*'] },
    },
    {
      path: 'gantt',
      name: 'TeamGantt',
      component: () => import('@/views/team/pages/GanttPage.vue'),
      meta: { locale: 'menu.teammind.gantt', requiresAuth: true, roles: ['*'] },
    },
    {
      path: 'approvals',
      name: 'TeamApprovals',
      component: () => import('@/views/team/pages/ApprovalsPage.vue'),
      meta: {
        locale: 'menu.teammind.approvals',
        requiresAuth: true,
        roles: ['*'],
      },
    },
    {
      path: 'members',
      name: 'TeamMembers',
      component: () => import('@/views/team/pages/MembersPage.vue'),
      meta: {
        locale: 'menu.teammind.members',
        requiresAuth: true,
        roles: ['*'],
      },
    },
    {
      path: 'assistant',
      name: 'TeamAssistant',
      component: () => import('@/views/team/pages/AssistantPage.vue'),
      meta: {
        locale: 'menu.teammind.assistant',
        requiresAuth: true,
        roles: ['*'],
      },
    },
    {
      path: 'memories',
      name: 'TeamMemories',
      component: () => import('@/views/team/pages/MemoriesPage.vue'),
      meta: {
        locale: 'menu.teammind.memories',
        requiresAuth: true,
        roles: ['*'],
      },
    },
    {
      path: 'notifications',
      name: 'TeamNotifications',
      component: () => import('@/views/team/pages/NotificationsPage.vue'),
      meta: {
        locale: 'menu.teammind.notifications',
        requiresAuth: true,
        roles: ['*'],
      },
    },
    {
      path: 'settings',
      name: 'TeamSettings',
      component: () => import('@/views/team/pages/SettingsPage.vue'),
      meta: {
        locale: 'menu.teammind.settings',
        requiresAuth: true,
        roles: ['*'],
      },
    },
    {
      path: 'join/:token',
      name: 'TeamJoin',
      component: () => import('@/views/team/pages/JoinPage.vue'),
      meta: {
        locale: 'menu.teammind.join',
        requiresAuth: true,
        roles: ['*'],
        hideInMenu: true,
      },
    },
    { path: 'wechat', name: 'TeamWechat', component: () => import('@/views/team/pages/WechatPage.vue'), meta: { locale: 'menu.teammind.wechat', requiresAuth: true, roles: ['*'] } },
    { path: 'platform', name: 'TeamPlatform', component: () => import('@/views/team/pages/PlatformPage.vue'), meta: { locale: 'menu.teammind.platform', requiresAuth: true, roles: ['super_admin'] } },
  ],
};

export default TEAMMIND;
