<template>
  <div class="page">
    <PageHeader title="通知中心" :subtitle="`${unreadCount} 条未读 · ${reminders.length} 个提醒 · ${agentMessages.length} 条 AI 协作消息`">
      <template #actions>
        <a-button :loading="loading" @click="loadAll"><reload-outlined /> 刷新</a-button>
        <a-button type="primary" ghost :disabled="!unreadCount" @click="readAll">全部已读</a-button>
      </template>
    </PageHeader>

    <a-alert v-if="error" type="error" show-icon :message="error" closable @close="error = ''" />

    <div class="panel notif-panel">
      <a-tabs v-model:activeKey="tab" class="notif-tabs">
        <a-tab-pane key="all" :tab="`全部通知 ${notices.length}`" />
        <a-tab-pane key="unread" :tab="`未读 ${unreadCount}`" />
        <a-tab-pane key="reminders" :tab="`提醒 ${reminders.length}`" />
        <a-tab-pane key="agents" :tab="`AI 协作 ${agentMessages.length}`" />
      </a-tabs>

      <a-spin :spinning="loading">
        <template v-if="tab === 'all' || tab === 'unread'">
          <div v-for="notice in filteredNotices" :key="notice.id" class="notice" :class="{ unread: !notice.read }">
            <div class="notice-dot" />
            <div class="notice-body">
              <div class="notice-title">
                {{ notice.title }}
                <a-tag v-if="!notice.read" color="blue" :bordered="false" class="mini">未读</a-tag>
              </div>
              <div class="notice-text">{{ notice.body }}</div>
              <div class="notice-time">{{ fmtDateTime(notice.createdAt) }} · {{ noticeTypeLabel(notice.type) }}</div>
            </div>
            <a-button v-if="!notice.read" size="small" @click="markRead(notice)">标为已读</a-button>
          </div>
          <div v-if="!filteredNotices.length" class="empty-block">没有{{ tab === 'unread' ? '未读' : '' }}通知</div>
        </template>

        <template v-else-if="tab === 'reminders'">
          <div v-for="reminder in reminders" :key="reminder.id" class="notice">
            <span class="reminder-icon"><clock-circle-outlined /></span>
            <div class="notice-body">
              <div class="notice-title">{{ reminder.title }}</div>
              <div class="notice-text">{{ reminder.body || '无正文' }}</div>
              <div class="notice-time">执行时间 {{ fmtDateTime(reminder.runAt) }}</div>
            </div>
            <a-tag :color="reminder.status === 'pending' ? 'processing' : reminder.status === 'sent' ? 'success' : 'default'">
              {{ reminder.status }}
            </a-tag>
          </div>
          <div v-if="!reminders.length" class="empty-block">还没有定时提醒，可在设置页创建。</div>
        </template>

        <template v-else>
          <div v-for="message in agentMessages" :key="message.id" class="notice" :class="{ unread: !message.readAt }">
            <span class="agent-icon"><robot-outlined /></span>
            <div class="notice-body">
              <div class="notice-title">{{ message.fromName || 'AI 协作消息' }}</div>
              <div class="notice-text">{{ message.body }}</div>
              <div class="notice-time">{{ fmtDateTime(message.createdAt) }}</div>
            </div>
            <a-button v-if="!message.readAt" size="small" @click="markAgentRead(message)">标为已读</a-button>
            <a-tag v-else color="success" :bordered="false">已读</a-tag>
          </div>
          <div v-if="!agentMessages.length" class="empty-block">还没有 AI 跨部门协作消息。</div>
        </template>
      </a-spin>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { ClockCircleOutlined, ReloadOutlined, RobotOutlined } from '@ant-design/icons-vue';
import { api } from '@/api/client';
import type { Notice, Reminder } from '@/api/types';
import { fmtDateTime } from '@/utils/format';
import { notify } from '@/utils/notify';
import PageHeader from '@/components/PageHeader.vue';

const loading = ref(false);
const error = ref('');
const tab = ref('all');
const notices = ref<Notice[]>([]);
const reminders = ref<Reminder[]>([]);
const agentMessages = ref<Array<Record<string, any>>>([]);

const unreadCount = computed(() => notices.value.filter((notice) => !notice.read).length);
const filteredNotices = computed(() => (tab.value === 'unread' ? notices.value.filter((notice) => !notice.read) : notices.value));

function noticeTypeLabel(type: string): string {
  return {
    approval: '审批',
    plan: '计划变更',
    project: '项目变更',
    member: '成员变更',
    scheduled_reminder: '定时提醒',
    ai_proactive: 'AI 日报',
    team_memory: '团队记忆',
    team_review: '团队回顾',
  }[type] || type || '通知';
}

async function loadAll(): Promise<void> {
  loading.value = true;
  const results = await Promise.allSettled([
    api<Notice[]>('GET', '/me/notifications'),
    api<Reminder[]>('GET', '/me/reminders'),
    api<Array<Record<string, any>>>('GET', '/me/agent-messages'),
  ]);
  if (results[0].status === 'fulfilled') notices.value = results[0].value || [];
  if (results[1].status === 'fulfilled') reminders.value = results[1].value || [];
  if (results[2].status === 'fulfilled') agentMessages.value = results[2].value || [];
  const failed = results.find((item) => item.status === 'rejected') as PromiseRejectedResult | undefined;
  error.value = failed ? (failed.reason as Error).message : '';
  loading.value = false;
}

async function markRead(notice: Notice): Promise<void> {
  try {
    await api('PATCH', `/me/notifications/${notice.id}`);
    notice.read = true;
  } catch (e) {
    notify.error(e);
  }
}

async function readAll(): Promise<void> {
  try {
    await api('POST', '/me/notifications/read-all');
    notices.value = notices.value.map((notice) => ({ ...notice, read: true }));
    notify.success('已全部标为已读');
  } catch (e) {
    notify.error(e);
  }
}

async function markAgentRead(message: Record<string, any>): Promise<void> {
  try {
    await api('PATCH', `/me/agent-messages/${message.id}`);
    message.readAt = new Date().toISOString();
  } catch (e) {
    notify.error(e);
  }
}

onMounted(loadAll);
</script>

<style scoped>
.notif-panel {
  padding: 0 16px 8px;
}
.notif-tabs :deep(.ant-tabs-nav) {
  margin-bottom: 0;
}
.notice {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 16px 8px;
  border-bottom: 1px solid var(--tm-border);
}
.notice:last-child {
  border-bottom: 0;
}
.notice-dot {
  width: 7px;
  height: 7px;
  margin-top: 8px;
  border-radius: 50%;
  background: var(--tm-border-strong);
  flex: none;
}
.notice.unread .notice-dot {
  background: var(--tm-primary);
  box-shadow: 0 0 0 3px var(--tm-primary-soft);
}
.notice-body {
  flex: 1;
  min-width: 0;
}
.notice-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 600;
  font-size: 13px;
}
.mini {
  font-size: 10px;
  line-height: 16px;
}
.notice-text {
  margin-top: 2px;
  color: var(--tm-text-2);
  font-size: 12.5px;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.notice-time {
  margin-top: 4px;
  color: var(--tm-muted);
  font-size: 11px;
}
.reminder-icon,
.agent-icon {
  display: grid;
  place-items: center;
  width: 28px;
  height: 28px;
  flex: none;
  border-radius: 8px;
  background: var(--tm-primary-soft);
  color: var(--tm-primary);
}
</style>
