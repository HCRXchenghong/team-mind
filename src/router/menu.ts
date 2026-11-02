import type { Component } from 'vue';
import {
  ApartmentOutlined,
  ApiOutlined,
  AuditOutlined,
  BellOutlined,
  BookOutlined,
  CalendarOutlined,
  CheckSquareOutlined,
  DashboardOutlined,
  ProjectOutlined,
  RobotOutlined,
  SafetyCertificateOutlined,
  SettingOutlined,
  TeamOutlined,
  WechatOutlined,
} from '@ant-design/icons-vue';

export interface MenuItem {
  path: string;
  title: string;
  icon: Component;
  manage?: boolean;
  superAdmin?: boolean;
}

export interface MenuGroup {
  title: string;
  items: MenuItem[];
}

export const workspaceMenu: MenuGroup[] = [
  {
    title: '工作台',
    items: [
      { path: '/teammind/overview', title: '总览', icon: DashboardOutlined },
      { path: '/teammind/tasks', title: '任务中心', icon: CheckSquareOutlined },
    ],
  },
  {
    title: '项目与计划',
    items: [
      { path: '/teammind/projects', title: '项目管理', icon: ProjectOutlined },
      { path: '/teammind/gantt', title: '甘特图', icon: CalendarOutlined },
    ],
  },
  {
    title: '协作',
    items: [
      { path: '/teammind/approvals', title: '审批中心', icon: AuditOutlined, manage: true },
      { path: '/teammind/departments', title: '部门与小组', icon: ApartmentOutlined, manage: true },
      { path: '/teammind/members', title: '成员管理', icon: TeamOutlined, manage: true },
      { path: '/teammind/notifications', title: '通知中心', icon: BellOutlined },
    ],
  },
  {
    title: 'AI 与知识',
    items: [
      { path: '/teammind/assistant', title: 'AI 助手', icon: RobotOutlined },
      { path: '/teammind/memories', title: '记忆与知识', icon: BookOutlined },
    ],
  },
  {
    title: '账号与组织',
    items: [
      { path: '/teammind/settings', title: '设置', icon: SettingOutlined },
      { path: '/teammind/wechat', title: '微信渠道', icon: WechatOutlined },
      { path: '/teammind/ai-admin', title: 'AI 管理', icon: ApiOutlined, manage: true },
    ],
  },
  {
    title: '平台',
    items: [{ path: '/teammind/platform', title: '平台控制台', icon: SafetyCertificateOutlined, superAdmin: true }],
  },
];
