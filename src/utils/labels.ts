import type { ProjectStatus, TaskPriority, TaskStatus, TeamRole } from '@/api/types';

export interface Option<T extends string> {
  value: T;
  label: string;
}

export const TASK_STATUS: Option<TaskStatus>[] = [
  { value: 'todo', label: '待开始' },
  { value: 'doing', label: '进行中' },
  { value: 'blocked', label: '阻塞' },
  { value: 'review', label: '待审查' },
  { value: 'done', label: '已完成' },
  { value: 'cancelled', label: '已取消' },
];

export const TASK_PRIORITY: Option<TaskPriority>[] = [
  { value: 'low', label: '低' },
  { value: 'medium', label: '中' },
  { value: 'high', label: '高' },
  { value: 'urgent', label: '紧急' },
];

export const PROJECT_STATUS: Option<ProjectStatus>[] = [
  { value: 'planning', label: '规划中' },
  { value: 'active', label: '进行中' },
  { value: 'paused', label: '已暂停' },
  { value: 'completed', label: '已完成' },
  { value: 'archived', label: '已归档' },
];

export const MEMBER_ROLE: Option<TeamRole>[] = [
  { value: 'member', label: '成员' },
  { value: 'team_admin', label: '管理员' },
  { value: 'team_owner', label: '所有者' },
];

export const MEMBER_STATUS: Option<string>[] = [
  { value: 'active', label: '正常' },
  { value: 'suspended', label: '停用' },
  { value: 'left', label: '已离开' },
];

export const MEMORY_CATEGORY: Option<string>[] = [
  { value: 'profile', label: '个人偏好' },
  { value: 'experience', label: '经验' },
  { value: 'current_work', label: '当前工作' },
  { value: 'operational_knowledge', label: '运营知识' },
];

export const PROPOSAL_STATUS_LABEL: Record<string, string> = {
  pending: '待处理',
  approved: '已通过',
  edited_approved: '调整后通过',
  rejected: '已驳回',
  needs_info: '待补充',
};

export function labelOf<T extends string>(options: Option<T>[], value: string | null | undefined): string {
  return options.find((item) => item.value === value)?.label || value || '—';
}

const TASK_STATUS_COLOR: Record<string, string> = {
  todo: 'default',
  doing: 'processing',
  blocked: 'error',
  review: 'warning',
  done: 'success',
  cancelled: 'default',
};

const PRIORITY_COLOR: Record<string, string> = {
  low: 'default',
  medium: 'blue',
  high: 'orange',
  urgent: 'red',
};

const PROJECT_STATUS_COLOR: Record<string, string> = {
  planning: 'default',
  active: 'processing',
  paused: 'warning',
  completed: 'success',
  archived: 'default',
};

const PROPOSAL_STATUS_COLOR: Record<string, string> = {
  pending: 'processing',
  approved: 'success',
  edited_approved: 'cyan',
  rejected: 'error',
  needs_info: 'warning',
};

export const taskStatusColor = (value: string): string => TASK_STATUS_COLOR[value] || 'default';
export const priorityColor = (value: string): string => PRIORITY_COLOR[value] || 'default';
export const projectStatusColor = (value: string): string => PROJECT_STATUS_COLOR[value] || 'default';
export const proposalStatusColor = (value: string): string => PROPOSAL_STATUS_COLOR[value] || 'default';

export function initials(name?: string | null): string {
  return (name || '?').trim().slice(0, 1).toUpperCase();
}
