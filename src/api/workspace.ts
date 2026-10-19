import axios, { AxiosRequestConfig } from 'axios';
export type ApiResponse<T> = { code: string; data: T; message?: string };
export async function api<T>(method: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE', path: string, data?: unknown, params?: Record<string, unknown>): Promise<T> {
  const response = await axios.request<ApiResponse<T>>({ method, url: path, data, params } as AxiosRequestConfig);
  return (response as unknown as ApiResponse<T>).data;
}
export interface Team { id: string; name: string; description: string; timezone: string; role: 'member' | 'team_admin' | 'team_owner'; status: string; members: number }
export interface Project { id: string; teamId: string; name: string; description: string; status: string; startDate: string | null; dueDate: string | null; progress: number; version?: number }
export interface Task { id: string; teamId: string; projectId: string; title: string; description: string; assigneeId: string | null; assigneeName: string | null; createdBy: string; status: string; priority: string; startDate: string | null; dueDate: string | null; progress: number; milestone: boolean; version: number }
export interface Page<T> { items: T[]; total: number; page: number; pageSize: number }
export interface Proposal { id: string; taskId: string; taskTitle: string; proposedBy: string; reason: string; impactSummary: string; proposed: { title: string; description: string; priority: string; status?: string; progress?: number; assigneeId: string | null; startDate: string | null; dueDate: string | null; milestone: boolean }; status: string; taskVersion: number; createdAt: string }
export interface ProjectProposal { id: string; projectId: string; projectName: string; proposedBy: string; reason: string; impactSummary: string; before: { name: string; description: string; status: string; startDate: string | null; dueDate: string | null }; proposed: { name: string; description: string; status: string; startDate: string | null; dueDate: string | null }; status: string; projectVersion: number; createdAt: string }
export interface MemberProposal { id: string; memberId: string; memberName: string; proposedBy: string; proposedByName: string; before: { role: string; status: string }; proposed: { role: string; status: string }; reason: string; impactSummary: string; status: string; createdAt: string; resolvedAt?: string }
export interface Member { id: string; name: string; email: string; role: string; status: string; joinedAt: string }
export interface Notification { id: string; teamId: string; title: string; body: string; type: string; read: boolean; createdAt: string }
export interface NotificationDelivery { id: string; type: string; channel: string; status: 'queued' | 'sending' | 'delivered' | 'suppressed' | 'retry' | 'failed'; createdAt: string; deliveredAt: string | null; reason: string }
export interface Reminder { id: string; teamId: string; title: string; body: string; runAt: string; status: 'scheduled' | 'running' | 'succeeded' | 'failed' | 'paused' | 'cancelled'; attempts: number; createdAt: string; resourceType?: string; resourceId?: string }
export interface Memory { id: string; category: string; content: string; confirmed: boolean; updatedAt: string; reviewAt?: string | null; expiresAt?: string | null; sourceMessageId?: string; sourceConversationId?: string; sourceRole?: string; sourceExcerpt?: string }
