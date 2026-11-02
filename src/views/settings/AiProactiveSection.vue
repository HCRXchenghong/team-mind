<template>
  <div class="ai-proactive">
    <div class="section-head">
      <div>
        <h3>AI 定时问候 / 日报</h3>
        <p class="muted">
          到点后 AI 会汇总当前组织的任务、阻塞与审批，生成一段内容并发送站内通知；绑定微信且会话可用时会同步发到微信。
        </p>
      </div>
      <a-button type="primary" @click="openCreate"><plus-outlined /> 新建计划</a-button>
    </div>

    <a-alert
      type="info"
      show-icon
      message="这是「主动生成」任务，不会调用工具修改数据"
      description="内容基于生成时的团队快照；未配置 AI 或模型失败时会自动退回模板摘要。可在通知偏好里单独关闭 AI 日报提醒。"
      style="margin-bottom: 14px"
    />

    <a-spin :spinning="loading">
      <a-table
        v-if="jobs.length"
        :columns="columns"
        :data-source="jobs"
        row-key="id"
        :pagination="false"
        size="middle"
        :scroll="{ x: 860 }"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'plan'">
            <div class="plan-line">
              <a-tag :color="record.kind === 'greeting' ? 'green' : 'blue'" :bordered="false">
                {{ record.kind === 'greeting' ? '问候' : '日报' }}
              </a-tag>
              <span class="plan-name">{{ kindName(record.kind) }}</span>
            </div>
            <div v-if="record.prompt" class="muted plan-prompt" :title="record.prompt">{{ record.prompt }}</div>
          </template>

          <template v-else-if="column.key === 'schedule'">
            <div>{{ frequencyText(record) }}</div>
            <div class="muted tiny">{{ record.timezone }}</div>
          </template>

          <template v-else-if="column.key === 'next'">
            <span v-if="record.status === 'paused'" class="muted">已暂停</span>
            <span v-else>{{ fmtDateTime(record.nextRunAt) }}</span>
          </template>

          <template v-else-if="column.key === 'last'">
            <div v-if="record.lastRunAt" class="last-line">
              <a-tag :color="lastColor(record.lastStatus)" :bordered="false" class="mini">
                {{ lastLabel(record.lastStatus) }}
              </a-tag>
              <span class="muted tiny">{{ fmtDateTime(record.lastRunAt) }}</span>
            </div>
            <span v-else class="muted">尚未生成</span>
            <div v-if="record.lastError" class="muted tiny error-text" :title="record.lastError">
              {{ record.lastError }}
            </div>
          </template>

          <template v-else-if="column.key === 'actions'">
            <div class="row-actions">
              <a-button size="small" :loading="runningId === record.id" @click="runNow(record)">立即生成</a-button>
              <a-button v-if="record.lastBody" size="small" type="link" @click="showResult(record)">查看</a-button>
              <a-button size="small" type="link" @click="openEdit(record)">编辑</a-button>
              <a-popconfirm
                :title="record.enabled ? '暂停后不再自动生成，确定？' : '恢复后按计划自动生成，确定？'"
                @confirm="toggle(record)"
              >
                <a-button size="small" type="link">{{ record.enabled ? '暂停' : '启用' }}</a-button>
              </a-popconfirm>
              <a-popconfirm title="删除后历史内容保留在通知里，确定删除计划？" @confirm="remove(record)">
                <a-button size="small" type="link" danger>删除</a-button>
              </a-popconfirm>
            </div>
          </template>
        </template>
      </a-table>

      <a-empty v-else description="还没有 AI 定时计划">
        <a-button type="primary" @click="openCreate">创建第一个计划</a-button>
      </a-empty>
    </a-spin>

    <a-modal
      v-model:open="modalOpen"
      :title="editingId ? '编辑 AI 计划' : '新建 AI 计划'"
      :confirm-loading="saving"
      width="620px"
      @ok="save"
    >
      <a-form layout="vertical">
        <a-form-item label="内容类型">
          <a-radio-group v-model:value="form.kind" button-style="solid">
            <a-radio-button value="daily_report">工作日报</a-radio-button>
            <a-radio-button value="greeting">主动问候</a-radio-button>
          </a-radio-group>
        </a-form-item>

        <a-form-item label="频率">
          <a-select v-model:value="form.cadence" :options="cadenceOptions" style="width: 100%" />
        </a-form-item>

        <a-form-item v-if="form.cadence === 'weekly'" label="星期几">
          <a-select v-model:value="form.weekday" :options="weekdayOptions" style="width: 100%" />
        </a-form-item>

        <a-form-item v-if="form.cadence === 'once'" label="执行时间">
          <a-date-picker
            v-model:value="form.onceAt"
            show-time
            format="YYYY-MM-DD HH:mm"
            style="width: 100%"
          />
        </a-form-item>
        <a-form-item v-else label="执行时间">
          <a-time-picker v-model:value="form.time" format="HH:mm" :minute-step="5" style="width: 100%" />
        </a-form-item>

        <a-form-item label="补充要求（可选）">
          <a-textarea
            v-model:value="form.prompt"
            :rows="3"
            :maxlength="1000"
            show-count
            placeholder="例如：多关注交付风险；用更轻松的语气；控制在 200 字以内"
          />
        </a-form-item>

        <a-alert
          type="info"
          show-icon
          :message="`时区：${timezone} · 生成后可在通知中心查看，微信通道可用时同步推送`"
        />
      </a-form>
    </a-modal>

    <a-modal v-model:open="resultOpen" :title="result?.lastTitle || '最近生成内容'" footer="" width="640px">
      <div class="result-meta muted">
        {{ result?.lastRunAt ? fmtDateTime(result.lastRunAt) : '' }}
        <a-tag :color="lastColor(result?.lastStatus || '')" :bordered="false" class="mini">{{ lastLabel(result?.lastStatus || '') }}</a-tag>
      </div>
      <pre class="result-body">{{ result?.lastBody || '暂无内容' }}</pre>
      <a-alert
        v-if="result?.lastError"
        type="warning"
        show-icon
        :message="`生成时使用了兜底模板：${result.lastError}`"
        style="margin-top: 12px"
      />
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import dayjs, { type Dayjs } from 'dayjs';
import { PlusOutlined } from '@ant-design/icons-vue';
import { api } from '@/api/client';
import type { AiProactiveJob } from '@/api/types';
import { useSession } from '@/store/session';
import { fmtDateTime } from '@/utils/format';
import { notify } from '@/utils/notify';

const session = useSession();
const loading = ref(false);
const saving = ref(false);
const jobs = ref<AiProactiveJob[]>([]);
const runningId = ref('');

const columns = [
  { title: '计划', key: 'plan', width: 220 },
  { title: '频率', key: 'schedule', width: 150 },
  { title: '下次运行', key: 'next', width: 150 },
  { title: '最近生成', key: 'last', width: 170 },
  { title: '操作', key: 'actions', width: 260 },
];

const cadenceOptions = [
  { label: '每天', value: 'daily' },
  { label: '每个工作日', value: 'weekdays' },
  { label: '每周', value: 'weekly' },
  { label: '仅一次', value: 'once' },
];
const weekdayOptions = [
  { label: '周一', value: 1 },
  { label: '周二', value: 2 },
  { label: '周三', value: 3 },
  { label: '周四', value: 4 },
  { label: '周五', value: 5 },
  { label: '周六', value: 6 },
  { label: '周日', value: 7 },
];

const timezone = computed(() => session.user?.timezone || 'Asia/Shanghai');

const modalOpen = ref(false);
const editingId = ref('');
const editingEnabled = ref(true);
const resultOpen = ref(false);
const result = ref<AiProactiveJob | null>(null);
const form = reactive<{
  kind: 'daily_report' | 'greeting';
  cadence: 'daily' | 'weekdays' | 'weekly' | 'once';
  weekday: number;
  time: Dayjs;
  onceAt: Dayjs | undefined;
  prompt: string;
}>({
  kind: 'daily_report',
  cadence: 'daily',
  weekday: 1,
  time: timeValue('09:00'),
  onceAt: undefined,
  prompt: '',
});

function timeValue(value: string): Dayjs {
  const [hour, minute] = (value || '09:00').split(':').map((part) => Number(part));
  return dayjs().hour(Number.isFinite(hour) ? hour : 9).minute(Number.isFinite(minute) ? minute : 0).second(0);
}

function kindName(kind: string): string {
  return kind === 'greeting' ? '主动问候' : '工作日报';
}

function frequencyText(job: any): string {
  if (job.cadence === 'once') return `仅一次 ${job.time}`;
  if (job.cadence === 'weekdays') return `工作日 ${job.time}`;
  if (job.cadence === 'weekly') return `每${weekdayOptions.find((item) => item.value === job.weekday)?.label || '周'} ${job.time}`;
  return `每天 ${job.time}`;
}

function lastLabel(status: string): string {
  if (status === 'ai') return 'AI 生成';
  if (status === 'template') return '模板兜底';
  if (status === 'failed') return '失败';
  return status || '—';
}

function lastColor(status: string): string {
  if (status === 'ai') return 'success';
  if (status === 'template') return 'warning';
  if (status === 'failed') return 'error';
  return 'default';
}

async function load(): Promise<void> {
  loading.value = true;
  try {
    jobs.value = await api<AiProactiveJob[]>('GET', '/me/ai-proactive');
  } catch (e) {
    notify.error(e);
  } finally {
    loading.value = false;
  }
}

function openCreate(): void {
  editingId.value = '';
  editingEnabled.value = true;
  Object.assign(form, {
    kind: 'daily_report',
    cadence: 'daily',
    weekday: 1,
    time: timeValue('09:00'),
    onceAt: undefined,
    prompt: '',
  });
  modalOpen.value = true;
}

function openEdit(job: any): void {
  editingId.value = job.id;
  editingEnabled.value = job.enabled;
  Object.assign(form, {
    kind: job.kind,
    cadence: job.cadence,
    weekday: job.weekday || 1,
    time: timeValue(job.time || '09:00'),
    onceAt: job.cadence === 'once' ? dayjs(job.nextRunAt) : undefined,
    prompt: job.prompt || '',
  });
  modalOpen.value = true;
}

function buildPayload(enabled: boolean): Record<string, any> {
  const base: Record<string, any> = {
    kind: form.kind,
    cadence: form.cadence,
    weekday: form.weekday,
    timezone: timezone.value,
    prompt: form.prompt,
    teamId: session.activeId,
    enabled,
  };
  if (form.cadence === 'once') {
    base.runAt = form.onceAt ? form.onceAt.toISOString() : '';
  } else {
    base.time = form.time ? form.time.format('HH:mm') : '09:00';
  }
  return base;
}

async function save(): Promise<void> {
  if (form.cadence === 'weekly' && !form.weekday) {
    notify.error('请选择星期几');
    return;
  }
  if (form.cadence === 'once' && !form.onceAt) {
    notify.error('请选择执行时间');
    return;
  }
  saving.value = true;
  try {
    if (editingId.value) {
      await api('PATCH', `/me/ai-proactive/${editingId.value}`, buildPayload(editingEnabled.value));
      notify.success('计划已更新');
    } else {
      await api('POST', '/me/ai-proactive', buildPayload(true));
      notify.success('计划已创建');
    }
    modalOpen.value = false;
    await load();
  } catch (e) {
    notify.error(e);
  } finally {
    saving.value = false;
  }
}

async function toggle(job: any): Promise<void> {
  try {
    await api('PATCH', `/me/ai-proactive/${job.id}`, {
      kind: job.kind,
      cadence: job.cadence,
      weekday: job.weekday,
      time: job.time,
      timezone: job.timezone,
      prompt: job.prompt,
      teamId: job.teamId,
      enabled: !job.enabled,
      runAt: job.cadence === 'once' ? job.nextRunAt : undefined,
    });
    notify.success(job.enabled ? '已暂停' : '已启用');
    await load();
  } catch (e) {
    notify.error(e);
  }
}

async function runNow(job: any): Promise<void> {
  runningId.value = job.id;
  try {
    await api('POST', `/me/ai-proactive/${job.id}/run`);
    notify.success('已加入生成队列，稍后到通知中心查看');
    window.setTimeout(() => {
      void load();
      runningId.value = '';
    }, 5000);
  } catch (e) {
    notify.error(e);
    runningId.value = '';
  }
}

async function remove(job: any): Promise<void> {
  try {
    await api('DELETE', `/me/ai-proactive/${job.id}`);
    notify.success('计划已删除');
    await load();
  } catch (e) {
    notify.error(e);
  }
}

function showResult(job: any): void {
  result.value = job;
  resultOpen.value = true;
}

onMounted(load);
</script>

<style scoped>
.section-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 14px;
}
.section-head h3 {
  margin: 0 0 4px;
  font-size: 14px;
}
.section-head p {
  margin: 0;
  max-width: 760px;
  font-size: 12px;
  line-height: 1.6;
}
.plan-line {
  display: flex;
  align-items: center;
  gap: 6px;
}
.plan-name {
  font-weight: 600;
  font-size: 12.5px;
}
.plan-prompt {
  margin-top: 2px;
  font-size: 11px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 260px;
}
.tiny {
  font-size: 11px;
}
.last-line {
  display: flex;
  align-items: center;
  gap: 6px;
}
.error-text {
  margin-top: 2px;
  color: var(--tm-danger, #f04438);
  max-width: 220px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.row-actions {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0 2px;
}
.result-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
  font-size: 12px;
}
.result-body {
  margin: 0;
  padding: 14px;
  border-radius: 8px;
  background: var(--tm-panel-2);
  border: 1px solid var(--tm-border);
  white-space: pre-wrap;
  word-break: break-word;
  font-family: inherit;
  font-size: 12.5px;
  line-height: 1.75;
  max-height: 50vh;
  overflow: auto;
}
@media (max-width: 700px) {
  .section-head {
    flex-direction: column;
  }
}
</style>
