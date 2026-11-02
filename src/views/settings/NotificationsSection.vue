<template>
  <div class="section">
    <a-row :gutter="24">
      <a-col :xs="24" :md="12">
        <div class="block">
          <div class="title-row">
            <h3>通知偏好</h3>
            <a-button size="small" :loading="saving" @click="savePrefs">保存</a-button>
          </div>
          <a-spin :spinning="loading">
            <div class="switch-row"><span>站内通知</span><a-switch v-model:checked="prefs.enabled" /></div>
            <div class="switch-row"><span>邮件通知</span><a-switch v-model:checked="prefs.emailEnabled" /></div>
            <div class="switch-row"><span>浏览器推送</span><a-switch v-model:checked="prefs.pushEnabled" /></div>
            <div class="switch-row"><span>原生推送（移动端）</span><a-switch v-model:checked="prefs.nativePushEnabled" /></div>
            <div class="switch-row">
              <span>
                微信提醒
                <span class="muted switch-hint">绑定微信并产生过会话后生效</span>
              </span>
              <a-switch v-model:checked="prefs.wechatEnabled" />
            </div>

            <a-divider style="margin: 14px 0" />
            <h4>免打扰</h4>
            <div class="inline-row">
              <a-time-picker v-model:value="prefs.quietStart" format="HH:mm" value-format="HH:mm" placeholder="开始" />
              <span class="muted">至</span>
              <a-time-picker v-model:value="prefs.quietEnd" format="HH:mm" value-format="HH:mm" placeholder="结束" />
            </div>
            <div class="field">
              <label>静音类型</label>
              <a-select v-model:value="prefs.mutedTypes" mode="tags" placeholder="选择或输入通知类型" :options="typeOptions" />
            </div>
            <div class="field">
              <label>摘要间隔（分钟，0 为关闭）</label>
              <a-input-number v-model:value="prefs.digestMinutes" :min="0" :max="1440" style="width: 160px" />
            </div>
          </a-spin>
        </div>

        <div class="block">
          <h3>推送通道</h3>
          <a-spin :spinning="channelsLoading">
            <div class="channel">
              <div>
                <div class="ch-name">邮件 <a-tag :color="channels.emailConfigured ? 'success' : 'default'" :bordered="false">{{ channels.emailConfigured ? '已配置' : '未配置' }}</a-tag></div>
                <div class="muted ch-meta">投递到注册邮箱，可在上方开关。</div>
              </div>
            </div>
            <div class="channel">
              <div>
                <div class="ch-name">
                  浏览器 Web Push
                  <a-tag :color="channels.pushConfigured ? 'success' : 'default'" :bordered="false">{{ channels.pushConfigured ? '已配置' : '未配置' }}</a-tag>
                  <a-tag v-if="channels.pushSubscriptions" :bordered="false">本设备订阅 {{ channels.pushSubscriptions }} 个</a-tag>
                </div>
                <div class="muted ch-meta">需要浏览器授权；VAPID 公钥由服务端下发。</div>
              </div>
              <a-button size="small" :disabled="!channels.pushConfigured" :loading="pushLoading" @click="subscribePush">订阅此设备</a-button>
            </div>
            <div class="channel">
              <div>
                <div class="ch-name">原生推送 <a-tag :bordered="false">{{ channels.nativePushConfigured ? '已配置' : '未配置' }}</a-tag></div>
                <div class="muted ch-meta">移动端/桌面壳使用，Web 端无需操作。</div>
              </div>
            </div>
          </a-spin>
        </div>
      </a-col>

      <a-col :xs="24" :md="12">
        <div class="block">
          <div class="title-row">
            <h3>定时提醒</h3>
            <a-button size="small" type="primary" ghost @click="openReminder">创建提醒</a-button>
          </div>
          <div v-for="reminder in reminders" :key="reminder.id" class="reminder">
            <div>
              <div class="rem-title">{{ reminder.title }}</div>
              <div class="muted rem-meta">{{ reminder.body || '无正文' }} · {{ fmtDateTime(reminder.runAt) }}</div>
            </div>
            <div class="rem-actions">
              <a-tag :color="reminder.status === 'pending' ? 'processing' : 'default'" :bordered="false">{{ reminderStatus(reminder.status) }}</a-tag>
              <a-popconfirm title="取消这个提醒？" @confirm="cancelReminder(reminder)">
                <a-button type="link" size="small" danger>取消</a-button>
              </a-popconfirm>
            </div>
          </div>
          <div v-if="!reminders.length" class="empty-block small">还没有提醒</div>
        </div>

        <div class="block">
          <h3>投递历史</h3>
          <a-table
            :data-source="deliveries"
            :columns="[
              { title: '类型', dataIndex: 'type', width: 110 },
              { title: '渠道', dataIndex: 'channel', width: 90 },
              { title: '状态', dataIndex: 'status', width: 90 },
              { title: '时间', dataIndex: 'createdAt', width: 160 },
            ]"
            :pagination="{ pageSize: 8, showSizeChanger: false }"
            size="small"
            row-key="id"
          >
            <template #bodyCell="{ column, record }">
              <template v-if="column.dataIndex === 'type'">
                <span>{{ notificationTypeLabel(record.type) }}</span>
              </template>
              <template v-else-if="column.dataIndex === 'createdAt'">
                <span class="muted">{{ fmtDateTime(record.createdAt) }}</span>
              </template>
              <template v-else-if="column.dataIndex === 'status'">
                <a-tag :color="record.status === 'sent' || record.status === 'delivered' ? 'success' : record.status === 'failed' ? 'error' : 'default'">
                  {{ deliveryStatus(record.status) }}
                </a-tag>
              </template>
            </template>
          </a-table>
          <div v-if="!deliveries.length" class="empty-block small">暂无投递记录</div>
        </div>
      </a-col>
    </a-row>

    <a-modal v-model:open="reminderOpen" title="创建定时提醒" :confirm-loading="saving" @ok="createReminder">
      <a-form layout="vertical">
        <a-form-item label="标题" required><a-input v-model:value="reminderDraft.title" :maxlength="120" /></a-form-item>
        <a-form-item label="正文"><a-textarea v-model:value="reminderDraft.body" :rows="2" /></a-form-item>
        <a-form-item label="执行时间" required>
          <a-date-picker v-model:value="reminderDraft.runAt" show-time format="YYYY-MM-DD HH:mm" style="width: 100%" />
        </a-form-item>
      </a-form>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue';
import dayjs, { type Dayjs } from 'dayjs';
import { api } from '@/api/client';
import type { Reminder } from '@/api/types';
import { useSession } from '@/store/session';
import { fmtDateTime } from '@/utils/format';
import { notify } from '@/utils/notify';

interface Prefs {
  enabled: boolean;
  emailEnabled: boolean;
  pushEnabled: boolean;
  nativePushEnabled: boolean;
  wechatEnabled: boolean;
  quietStart: string;
  quietEnd: string;
  mutedTypes: string[];
  digestMinutes: number;
}

const session = useSession();
const loading = ref(false);
const saving = ref(false);
const channelsLoading = ref(false);
const pushLoading = ref(false);
const prefs = reactive<Prefs>({
  enabled: true,
  emailEnabled: false,
  pushEnabled: false,
  nativePushEnabled: false,
  wechatEnabled: true,
  quietStart: '',
  quietEnd: '',
  mutedTypes: [],
  digestMinutes: 0,
});
const typeOptions = [
  { label: '审批', value: 'approval' },
  { label: '计划变更', value: 'plan' },
  { label: '项目变更', value: 'project' },
  { label: '成员变更', value: 'member' },
  { label: '定时提醒', value: 'scheduled_reminder' },
  { label: 'AI 日报', value: 'ai_proactive' },
  { label: '团队记忆', value: 'team_memory' },
  { label: '团队回顾', value: 'team_review' },
];
const channels = ref<Record<string, any>>({});
const reminders = ref<Reminder[]>([]);
const deliveries = ref<Array<Record<string, any>>>([]);
const reminderOpen = ref(false);
const reminderDraft = reactive<{ title: string; body: string; runAt: Dayjs | undefined }>({ title: '', body: '', runAt: undefined });

async function loadPrefs(): Promise<void> {
  loading.value = true;
  try {
    const result = await api<Prefs & { mutedTypes?: string[] }>('GET', '/me/notification-preferences');
    Object.assign(prefs, {
      ...result,
      mutedTypes: result.mutedTypes || [],
      quietStart: result.quietStart || '',
      quietEnd: result.quietEnd || '',
    });
  } catch (e) {
    notify.error(e);
  } finally {
    loading.value = false;
  }
}

async function savePrefs(): Promise<void> {
  saving.value = true;
  try {
    await api('PATCH', '/me/notification-preferences', {
      enabled: prefs.enabled,
      emailEnabled: prefs.emailEnabled,
      pushEnabled: prefs.pushEnabled,
      nativePushEnabled: prefs.nativePushEnabled,
      quietStart: prefs.quietStart || '',
      quietEnd: prefs.quietEnd || '',
      mutedTypes: prefs.mutedTypes,
      digestMinutes: prefs.digestMinutes,
    });
    notify.success('通知偏好已保存');
  } catch (e) {
    notify.error(e);
  } finally {
    saving.value = false;
  }
}

function vapidKeyToBytes(key: string): BufferSource {
  const raw = atob(key.replace(/-/g, '+').replace(/_/g, '/') + '='.repeat((4 - (key.length % 4)) % 4));
  return Uint8Array.from(raw, (char) => char.charCodeAt(0)) as unknown as BufferSource;
}

async function loadChannels(): Promise<void> {
  channelsLoading.value = true;
  try {
    channels.value = await api<Record<string, any>>('GET', '/me/notification-channels');
    prefs.pushEnabled = Boolean(channels.value.pushEnabled);
  } catch (e) {
    notify.error(e);
  } finally {
    channelsLoading.value = false;
  }
}

async function subscribePush(): Promise<void> {
  if (!('serviceWorker' in navigator) || !('PushManager' in window)) {
    notify.error('当前浏览器不支持 Web Push');
    return;
  }
  const publicKey = String(channels.value.pushPublicKey || '');
  if (!publicKey) {
    notify.error('服务端未下发 VAPID 公钥');
    return;
  }
  pushLoading.value = true;
  try {
    const registration = await navigator.serviceWorker.register('/sw.js');
    const existing = await registration.pushManager.getSubscription();
    const subscription =
      existing ||
      (await registration.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: vapidKeyToBytes(publicKey),
      }));
    await api('POST', '/me/notification-channels/push-subscriptions', subscription.toJSON());
    notify.success('此设备已订阅推送');
    await loadChannels();
  } catch (e) {
    notify.error(e);
  } finally {
    pushLoading.value = false;
  }
}

async function loadReminders(): Promise<void> {
  try {
    reminders.value = await api<Reminder[]>('GET', '/me/reminders');
  } catch {
    reminders.value = [];
  }
}

function openReminder(): void {
  reminderDraft.title = '';
  reminderDraft.body = '';
  reminderDraft.runAt = dayjs().add(1, 'hour');
  reminderOpen.value = true;
}

async function createReminder(): Promise<void> {
  if (!reminderDraft.title.trim() || !reminderDraft.runAt) {
    notify.error('请填写标题和执行时间');
    return;
  }
  saving.value = true;
  try {
    await api('POST', '/me/reminders', {
      teamId: session.activeId || '',
      title: reminderDraft.title.trim(),
      body: reminderDraft.body,
      runAt: reminderDraft.runAt.toISOString(),
    });
    reminderOpen.value = false;
    notify.success('提醒已创建');
    await loadReminders();
  } catch (e) {
    notify.error(e);
  } finally {
    saving.value = false;
  }
}

async function cancelReminder(reminder: Reminder): Promise<void> {
  try {
    await api('DELETE', `/me/reminders/${reminder.id}`);
    notify.success('提醒已取消');
    await loadReminders();
  } catch (e) {
    notify.error(e);
  }
}

function notificationTypeLabel(type: string): string {
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

function reminderStatus(status: string): string {
  return { pending: '待发送', sent: '已发送', failed: '失败', cancelled: '已取消' }[status] || status;
}

function deliveryStatus(status: string): string {
  return { sent: '已发送', delivered: '已投递', queued: '排队中', retry: '重试中', failed: '失败', sending: '发送中' }[status] || status;
}

async function loadDeliveries(): Promise<void> {
  try {
    deliveries.value = await api<Array<Record<string, any>>>('GET', '/me/notification-deliveries');
  } catch {
    deliveries.value = [];
  }
}

onMounted(async () => {
  await Promise.all([loadPrefs(), loadChannels(), loadReminders(), loadDeliveries()]);
});
</script>

<style scoped>
.block {
  margin-bottom: 28px;
}
h3 {
  margin: 0 0 10px;
  font-size: 14px;
}
h4 {
  margin: 0 0 8px;
  font-size: 12.5px;
  color: var(--tm-text-2);
}
.title-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 10px;
}
.title-row h3 {
  margin: 0;
}
.switch-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 7px 0;
  font-size: 12.5px;
}
.switch-hint {
  margin-left: 6px;
  font-size: 11px;
}
.inline-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 12px;
}
.field {
  margin-bottom: 10px;
}
.field label {
  display: block;
  margin-bottom: 5px;
  color: var(--tm-muted);
  font-size: 11.5px;
}
.channel {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 11px 0;
  border-bottom: 1px solid var(--tm-border);
}
.channel:last-child {
  border-bottom: 0;
}
.ch-name {
  display: flex;
  align-items: center;
  gap: 6px;
  font-weight: 500;
  font-size: 12.5px;
}
.ch-meta {
  margin-top: 2px;
  font-size: 11px;
}
.reminder {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 0;
  border-bottom: 1px solid var(--tm-border);
}
.reminder:last-child {
  border-bottom: 0;
}
.rem-title {
  font-size: 12.5px;
  font-weight: 500;
}
.rem-meta {
  margin-top: 2px;
  font-size: 11px;
}
.rem-actions {
  display: flex;
  align-items: center;
  gap: 4px;
  flex: none;
}
.empty-block.small {
  padding: 18px;
  font-size: 12px;
}
</style>
