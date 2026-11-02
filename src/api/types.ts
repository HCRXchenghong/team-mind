export interface User {
  id: string;
  email: string;
  name: string;
  timezone: string;
  role: string;
  superAdmin: boolean;
}

export type TeamKind = 'team' | 'company';
export type TeamRole = 'member' | 'team_admin' | 'team_owner';

export interface Team {
  id: string;
  name: string;
  kind: TeamKind;
  description: string;
  timezone: string;
  role: TeamRole;
  isLead: boolean;
  status: 'active' | 'pending_review' | 'suspended';
  members: number;
}

export interface Department {
  id: string;
  name: string;
  description: string;
  leadUserId: string;
  leadName: string;
  memberCount: number;
  projectCount: number;
}

export interface Member {
  id: string;
  name: string;
  email: string;
  role: TeamRole;
  status: 'active' | 'suspended' | 'left';
  departmentId: string;
  departmentName: string;
  joinedAt: string | null;
}

export type ProjectStatus = 'planning' | 'active' | 'paused' | 'completed' | 'archived';

export interface Project {
  id: string;
  name: string;
  description: string;
  status: ProjectStatus;
  departmentId: string;
  departmentName: string;
  startDate: string | null;
  dueDate: string | null;
  progress: number;
  version: number;
  createdAt?: string;
}

export type TaskStatus = 'todo' | 'doing' | 'blocked' | 'review' | 'done' | 'cancelled';
export type TaskPriority = 'low' | 'medium' | 'high' | 'urgent';

export interface Task {
  id: string;
  projectId: string;
  title: string;
  description: string;
  status: TaskStatus;
  priority: TaskPriority;
  assigneeId: string | null;
  assigneeName: string | null;
  departmentId?: string;
  departmentName?: string;
  startDate: string | null;
  dueDate: string | null;
  progress: number;
  milestone: boolean;
  version: number;
  createdBy?: string;
}

export interface PageResult<T> {
  items: T[];
  total: number;
  page: number;
  pageSize: number;
}

export interface Notice {
  id: string;
  teamId: string;
  type: string;
  title: string;
  body: string;
  read: boolean;
  createdAt: string;
}

export interface Reminder {
  id: string;
  teamId: string;
  title: string;
  body: string;
  runAt: string;
  status: string;
  attempts?: number;
  createdAt: string;
}

export interface Memory {
  id: string;
  category: string;
  content: string;
  confirmed: boolean;
  reviewAt?: string | null;
  expiresAt?: string | null;
  sourceMessageId?: string;
  sourceConversationId?: string;
  sourceExcerpt?: string;
  createdAt?: string;
  updatedAt?: string;
}

export interface Conversation {
  id: string;
  teamId: string | null;
  title: string;
  visibility: string;
  updatedAt: string;
}

export interface ChatMessage {
  id: string;
  role: 'user' | 'assistant';
  content: string;
  provider?: string;
  createdAt?: string;
}

export interface AiStatus {
  provider: string;
  configured: boolean;
  agentTools: boolean;
  sandboxAvailable: boolean;
  shellAvailable: boolean;
  webAvailable: boolean;
  workspaceAvailable: boolean;
}

export interface WorkspaceFile {
  id: string;
  name: string;
  size: number;
  updatedAt?: string;
  content?: string;
}

export interface AuditEntry {
  id?: string;
  teamId?: string;
  teamName?: string;
  actorName?: string;
  action: string;
  resourceType?: string;
  resourceId?: string;
  reason?: string;
  createdAt: string;
}

export interface AiProvider {
  id: string;
  teamId?: string;
  teamName?: string;
  name: string;
  baseUrl: string;
  models: string[];
  enabled: boolean;
  priority: number;
  failureCount: number;
  cooldownUntil?: string | null;
  lastError?: string;
  hasKey?: boolean;
  createdAt?: string;
  updatedAt?: string;
}

export interface ProposalBase {
  id: string;
  status: string;
  proposedBy: string;
  reason: string;
  impactSummary: string;
  createdAt: string;
  resolvedAt?: string | null;
}

export interface PlanProposal extends ProposalBase {
  taskId: string;
  taskTitle: string;
  taskVersion: number;
  departmentId?: string;
  proposed: {
    title: string;
    description: string;
    priority: TaskPriority;
    status: TaskStatus;
    progress: number;
    assigneeId: string | null;
    startDate: string | null;
    dueDate: string | null;
    milestone: boolean;
  };
}

export interface ProjectProposal extends ProposalBase {
  projectId: string;
  projectName: string;
  proposed: Record<string, unknown>;
}

export interface MemberProposal extends ProposalBase {
  memberId: string;
  memberName: string;
  proposed: { role?: TeamRole; status?: string; departmentId?: string };
}

export interface AiProactiveJob {
  id: string;
  teamId: string;
  status: string;
  attempts: number;
  nextRunAt: string;
  createdAt: string;
  kind: 'greeting' | 'daily_report';
  cadence: 'daily' | 'weekdays' | 'weekly' | 'once';
  time: string;
  weekday: number;
  timezone: string;
  prompt: string;
  enabled: boolean;
  lastRunAt: string;
  lastStatus: string;
  lastError: string;
  lastTitle: string;
  lastBody: string;
}
