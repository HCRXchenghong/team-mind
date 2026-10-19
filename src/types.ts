export type Role = 'team_owner' | 'team_admin' | 'member'
export type TaskStatus = 'todo' | 'doing' | 'blocked' | 'review' | 'done'
export type Priority = 'high' | 'medium' | 'low'

export interface Team {
  id: string
  name: string
  initials: string
  color: string
  role: Role
  members: number
  description?: string
  timezone?: string
  joinPolicy?: 'invite' | 'request'
}

export interface Project {
  id: string
  teamId: string
  name: string
  description: string
  color: string
  progress: number
  dueDate: string
  members: string[]
}

export interface Task {
  id: string
  teamId: string
  projectId: string
  title: string
  owner: string
  ownerInitials: string
  ownerColor: string
  status: TaskStatus
  priority: Priority
  startDate: string
  dueDate: string
  progress: number
  source: string
  version: number
}

export interface Proposal {
  id: string
  teamId: string
  taskId: string
  taskTitle: string
  proposedBy: string
  proposedByInitials: string
  reason: string
  fromDate: string
  toDate: string
  oldOwner: string
  newOwner: string
  createdAt: string
  status: 'pending' | 'approved' | 'rejected'
  taskVersion: number
}

export interface Memory {
  id: string
  scope: '个人' | '团队'
  teamId?: string
  type: string
  content: string
  updatedAt: string
  confirmed: boolean
}

export interface TeamInvitation {
  id: string
  teamId: string
  teamName: string
  tokenHash: string
  role: 'member'
  expiresAt: string
  maxUses: number
  uses: number
  revoked: boolean
}
