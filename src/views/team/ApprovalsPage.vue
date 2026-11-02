<template>
  <div class="page">
    <PageHeader title="审批中心" :subtitle="`待处理 ${pendingCount} 条 · 计划 / 项目 / 成员身份变更`">
      <template #actions>
        <a-button :loading="loading" @click="loadAll"><reload-outlined /> 刷新</a-button>
      </template>
    </PageHeader>

    <a-alert v-if="error" type="error" show-icon :message="error" closable @close="error = ''" />

    <div class="panel">
      <div class="tabs-head">
        <a-tabs v-model:activeKey="activeType" class="type-tabs">
          <a-tab-pane v-for="tab in tabs" :key="tab.key" :tab="`${tab.label}${tab.key === 'plan' ? ` (${pendingOf('plan')})` : ''}`" />
        </a-tabs>
        <a-radio-group v-model:value="scope" button-style="solid" size="small">
          <a-radio-button value="pending">待处理</a-radio-button>
          <a-radio-button value="resolved">已处理</a-radio-button>
        </a-radio-group>
      </div>

      <a-table
        :data-source="filtered"
        :columns="columns"
        :loading="loading"
        row-key="id"
        size="middle"
        :pagination="{ pageSize: 20, showSizeChanger: false }"
        :scroll="{ x: 900 }"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'target'">
            <div class="target">{{ targetOf(record) }}</div>
            <div class="muted small">{{ typeHint(record) }}</div>
          </template>
          <template v-else-if="column.key === 'author'">
            <span class="who"><UserAvatar :name="record.proposedBy || record.proposedByName" :size="20" />{{ authorOf(record) }}</span>
          </template>
          <template v-else-if="column.key === 'reason'">
            <div class="reason">{{ record.reason || '—' }}</div>
            <div v-if="record.impactSummary" class="muted small">{{ record.impactSummary }}</div>
          </template>
          <template v-else-if="column.key === 'status'">
            <StatusTag kind="proposal" :value="record.status" dot />
          </template>
          <template v-else-if="column.key === 'createdAt'">
            <span class="muted">{{ fmtDateTime(record.createdAt) }}</span>
          </template>
          <template v-else-if="column.key === 'actions'">
            <a-button type="link" size="small" @click="openDetail(record)">查看</a-button>
            <a-button v-if="record.status === 'pending' && session.canManage" type="link" size="small" @click="openDecision(record)">处理</a-button>
            <a-button
              v-if="['rejected', 'needs_info'].includes(record.status)"
              type="link"
              size="small"
              @click="openResubmit(record)"
            >
              重新提交
            </a-button>
          </template>
        </template>
      </a-table>
      <div v-if="!filtered.length && !loading" class="empty-block">当前没有{{ scope === 'pending' ? '待处理' : '已处理' }}的提案</div>
    </div>

    <!-- 详情 -->
    <a-drawer v-model:open="detailOpen" title="提案详情" :width="620">
      <template v-if="current">
        <div class="d-head">
          <div>
            <h2>{{ targetOf(current) }}</h2>
            <div class="d-meta">
              <StatusTag kind="proposal" :value="current.status" dot />
              <span class="muted">{{ authorOf(current) }} · {{ fmtDateTime(current.createdAt) }}</span>
            </div>
          </div>
          <a-button v-if="current.status === 'pending' && session.canManage" type="primary" @click="openDecision(current)">处理</a-button>
        </div>

        <a-descriptions :column="1" size="small" bordered class="block">
          <a-descriptions-item label="变更原因">{{ current.reason || '—' }}</a-descriptions-item>
          <a-descriptions-item label="影响说明">{{ current.impactSummary || '—' }}</a-descriptions-item>
          <a-descriptions-item v-if="current.resolvedAt" label="处理时间">{{ fmtDateTime(current.resolvedAt) }}</a-descriptions-item>
        </a-descriptions>

        <h3 class="sub-title">{{ current.before ? '变更对比' : '提案内容' }}</h3>
        <a-table
          :data-source="snapshotRows(current)"
          :columns="[
            { title: '字段', dataIndex: 'field', width: 130 },
            { title: '当前值', dataIndex: 'before' },
            { title: '提案值', dataIndex: 'after' },
          ]"
          :pagination="false"
          size="small"
          row-key="field"
        >
          <template #bodyCell="{ column, record }">
            <template v-if="column.dataIndex === 'after'">
              <span class="after">{{ record.after }}</span>
            </template>
          </template>
        </a-table>

        <div v-if="['rejected', 'needs_info'].includes(current.status)" class="resubmit-hint">
          <a-alert type="warning" show-icon message="提案需要修改后重新提交，通过前不会生效。" />
          <a-button type="primary" ghost @click="openResubmit(current)">重新提交</a-button>
        </div>
      </template>
    </a-drawer>

    <!-- 处理 -->
    <a-modal v-model:open="decisionOpen" title="处理提案" :confirm-loading="saving" width="620px" @ok="submitDecision">
      <a-form layout="vertical">
        <a-form-item label="处理结果">
          <a-radio-group v-model:value="decision.decision">
            <a-radio value="approved">通过</a-radio>
            <a-radio value="edited_approved">调整后通过</a-radio>
            <a-radio value="needs_info">要求补充</a-radio>
            <a-radio value="rejected">驳回</a-radio>
          </a-radio-group>
        </a-form-item>
        <a-form-item label="说明">
          <a-textarea
            v-model:value="decision.reason"
            :rows="3"
            :placeholder="decision.decision === 'rejected' || decision.decision === 'needs_info' ? '驳回或要求补充时必填，至少 5 个字' : '可填写审批备注'"
          />
        </a-form-item>

        <template v-if="decision.decision === 'edited_approved'">
          <a-divider orientation="left" plain>调整提案字段</a-divider>
          <template v-if="activeType === 'plan'">
            <div class="form-grid">
              <a-form-item label="标题"><a-input v-model:value="decision.title" /></a-form-item>
              <a-form-item label="优先级">
                <a-select v-model:value="decision.priority">
                  <a-select-option v-for="p in TASK_PRIORITY" :key="p.value" :value="p.value">{{ p.label }}</a-select-option>
                </a-select>
              </a-form-item>
              <a-form-item label="状态">
                <a-select v-model:value="decision.status">
                  <a-select-option v-for="s in TASK_STATUS" :key="s.value" :value="s.value">{{ s.label }}</a-select-option>
                </a-select>
              </a-form-item>
              <a-form-item label="进度"><a-input-number v-model:value="decision.progress" :min="0" :max="100" style="width: 100%" /></a-form-item>
            </div>
          </template>
          <template v-else-if="activeType === 'project'">
            <div class="form-grid">
              <a-form-item label="名称"><a-input v-model:value="decision.name" /></a-form-item>
              <a-form-item label="状态">
                <a-select v-model:value="decision.status">
                  <a-select-option v-for="s in PROJECT_STATUS" :key="s.value" :value="s.value">{{ s.label }}</a-select-option>
                </a-select>
              </a-form-item>
            </div>
          </template>
          <template v-else>
            <div class="form-grid">
              <a-form-item label="角色">
                <a-select v-model:value="decision.role" allow-clear>
                  <a-select-option v-for="r in MEMBER_ROLE" :key="r.value" :value="r.value">{{ r.label }}</a-select-option>
                </a-select>
              </a-form-item>
              <a-form-item label="状态">
                <a-select v-model:value="decision.status" allow-clear>
                  <a-select-option v-for="s in MEMBER_STATUS" :key="s.value" :value="s.value">{{ s.label }}</a-select-option>
                </a-select>
              </a-form-item>
            </div>
          </template>
        </template>
      </a-form>
    </a-modal>

    <!-- 重新提交 -->
    <a-modal v-model:open="resubmitOpen" title="重新提交提案" :confirm-loading="saving" width="620px" @ok="submitResubmit">
      <a-alert type="info" show-icon message="重新提交会回到待处理队列，审批通过前不会改变正式数据。" style="margin-bottom: 14px" />
      <a-form layout="vertical">
        <a-form-item label="变更原因" required>
          <a-textarea v-model:value="resubmit.reason" :rows="2" placeholder="至少 5 个字" />
        </a-form-item>
        <a-form-item label="影响说明" required>
          <a-textarea v-model:value="resubmit.impactSummary" :rows="2" placeholder="至少 3 个字" />
        </a-form-item>
      </a-form>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue';
import { ReloadOutlined } from '@ant-design/icons-vue';
import { api } from '@/api/client';
import { useSession } from '@/store/session';
import { fmtDateTime } from '@/utils/format';
import {
  labelOf,
  MEMBER_ROLE,
  MEMBER_STATUS,
  PROJECT_STATUS,
  TASK_PRIORITY,
  TASK_STATUS,
} from '@/utils/labels';
import { notify } from '@/utils/notify';
import PageHeader from '@/components/PageHeader.vue';
import StatusTag from '@/components/StatusTag.vue';
import UserAvatar from '@/components/UserAvatar.vue';

type ProposalType = 'plan' | 'project' | 'member';
type AnyProposal = Record<string, any>;

const session = useSession();
const loading = ref(false);
const saving = ref(false);
const error = ref('');
const activeType = ref<ProposalType>('plan');
const scope = ref<'pending' | 'resolved'>('pending');
const lists = reactive<Record<ProposalType, AnyProposal[]>>({ plan: [], project: [], member: [] });

const tabs: Array<{ key: ProposalType; label: string }> = [
  { key: 'plan', label: '任务计划' },
  { key: 'project', label: '项目变更' },
  { key: 'member', label: '成员身份' },
];

const columns = [
  { title: '对象', key: 'target', width: 240 },
  { title: '发起人', key: 'author', width: 140 },
  { title: '原因 / 影响', key: 'reason', width: 300 },
  { title: '状态', key: 'status', width: 100 },
  { title: '提交时间', key: 'createdAt', width: 150 },
  { title: '操作', key: 'actions', width: 150, fixed: 'right' as const },
];

const pathOf = (type: ProposalType): string => `/teams/${session.activeId}/${type}-proposals`;

const currentList = computed(() => lists[activeType.value] || []);
const filtered = computed(() => currentList.value.filter((item) => (scope.value === 'pending' ? item.status === 'pending' : item.status !== 'pending')));
const pendingCount = computed(() => (['plan', 'project', 'member'] as ProposalType[]).reduce((sum, type) => sum + pendingOf(type), 0));

function pendingOf(type: ProposalType): number {
  return (lists[type] || []).filter((item) => item.status === 'pending').length;
}

function targetOf(item: AnyProposal): string {
  if (activeType.value === 'plan') return item.taskTitle || '任务计划变更';
  if (activeType.value === 'project') return item.projectName || '项目变更';
  return item.memberName || '成员身份变更';
}

function authorOf(item: AnyProposal): string {
  return item.proposedByName || item.proposedBy || '成员';
}

function typeHint(item: AnyProposal): string {
  if (activeType.value === 'plan') return `任务计划 · 版本 ${item.taskVersion ?? '—'}`;
  if (activeType.value === 'project') return `项目变更 · 版本 ${item.projectVersion ?? '—'}`;
  return '成员角色 / 状态变更';
}

function humanize(key: string): string {
  const map: Record<string, string> = {
    title: '标题',
    description: '描述',
    priority: '优先级',
    status: '状态',
    progress: '进度',
    assigneeId: '负责人',
    startDate: '开始日期',
    dueDate: '截止日期',
    milestone: '里程碑',
    name: '名称',
    departmentId: '部门',
    role: '角色',
  };
  return map[key] || key;
}

function displayValue(key: string, value: unknown): string {
  if (value === null || value === undefined || value === '') return '—';
  if (key === 'priority') return labelOf(TASK_PRIORITY, String(value));
  if (key === 'status' && activeType.value === 'project') return labelOf(PROJECT_STATUS, String(value));
  if (key === 'status' && activeType.value === 'plan') return labelOf(TASK_STATUS, String(value));
  if (key === 'status' && activeType.value === 'member') return labelOf(MEMBER_STATUS, String(value));
  if (key === 'role') return labelOf(MEMBER_ROLE, String(value));
  if (key === 'milestone') return value ? '是' : '否';
  if (key === 'progress') return `${value}%`;
  return String(value);
}

function snapshotRows(item: AnyProposal): Array<{ field: string; before: string; after: string }> {
  const proposed = item.proposed || {};
  const before = item.before || {};
  const rows: Array<{ field: string; before: string; after: string }> = [];
  Object.keys(proposed).forEach((key) => {
    const after = displayValue(key, proposed[key]);
    const old = item.before ? displayValue(key, before[key]) : '—';
    if (!item.before || old !== after) rows.push({ field: humanize(key), before: item.before ? old : '—', after });
  });
  return rows;
}

async function loadAll(): Promise<void> {
  if (!session.activeId) return;
  loading.value = true;
  try {
    const teamId = session.activeId;
    const [plan, project, member] = await Promise.all([
      api<AnyProposal[]>('GET', `/teams/${teamId}/plan-proposals`),
      api<AnyProposal[]>('GET', `/teams/${teamId}/project-proposals`),
      api<AnyProposal[]>('GET', `/teams/${teamId}/member-proposals`),
    ]);
    if (teamId !== session.activeId) return;
    lists.plan = plan || [];
    lists.project = project || [];
    lists.member = member || [];
    error.value = '';
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}

/* 详情 */
const detailOpen = ref(false);
const current = ref<AnyProposal | null>(null);
function openDetail(item: AnyProposal): void {
  current.value = item;
  detailOpen.value = true;
}

/* 处理 */
const decisionOpen = ref(false);
const decision = reactive<Record<string, any>>({
  decision: 'approved',
  reason: '',
  title: '',
  priority: 'medium',
  status: '',
  progress: 0,
  name: '',
  role: undefined,
});

function openDecision(item: AnyProposal): void {
  current.value = item;
  const proposed = item.proposed || {};
  Object.assign(decision, {
    decision: 'approved',
    reason: '',
    title: proposed.title || '',
    priority: proposed.priority || 'medium',
    status: proposed.status || '',
    progress: proposed.progress ?? 0,
    name: proposed.name || '',
    role: proposed.role || undefined,
  });
  decisionOpen.value = true;
}

async function submitDecision(): Promise<void> {
  if (!current.value) return;
  if (['rejected', 'needs_info'].includes(decision.decision) && decision.reason.trim().length < 5) {
    notify.error('驳回或要求补充时，请填写至少 5 个字的说明');
    return;
  }
  saving.value = true;
  try {
    const body: Record<string, unknown> = { decision: decision.decision, reason: decision.reason };
    if (decision.decision === 'edited_approved') {
      if (activeType.value === 'plan') {
        Object.assign(body, {
          title: decision.title,
          priority: decision.priority,
          status: decision.status,
          progress: decision.progress,
        });
      } else if (activeType.value === 'project') {
        Object.assign(body, { name: decision.name, status: decision.status });
      } else {
        Object.assign(body, { role: decision.role || '', status: decision.status || '' });
      }
    }
    await api('POST', `${pathOf(activeType.value)}/${current.value.id}/decision`, body);
    decisionOpen.value = false;
    detailOpen.value = false;
    notify.success('提案已处理');
    await loadAll();
  } catch (e) {
    notify.error(e);
  } finally {
    saving.value = false;
  }
}

/* 重新提交 */
const resubmitOpen = ref(false);
const resubmit = reactive({ reason: '', impactSummary: '' });
function openResubmit(item: AnyProposal): void {
  current.value = item;
  resubmit.reason = '';
  resubmit.impactSummary = '';
  resubmitOpen.value = true;
}

async function submitResubmit(): Promise<void> {
  if (!current.value) return;
  if (resubmit.reason.trim().length < 5 || resubmit.impactSummary.trim().length < 3) {
    notify.error('请填写至少 5 个字的原因和至少 3 个字的影响说明');
    return;
  }
  saving.value = true;
  try {
    await api('POST', `${pathOf(activeType.value)}/${current.value.id}/resubmit`, {
      reason: resubmit.reason,
      impactSummary: resubmit.impactSummary,
      ...(current.value.proposed || {}),
    });
    resubmitOpen.value = false;
    detailOpen.value = false;
    notify.success('提案已重新提交');
    await loadAll();
  } catch (e) {
    notify.error(e);
  } finally {
    saving.value = false;
  }
}

watch(
  () => session.activeId,
  () => {
    detailOpen.value = false;
    loadAll();
  },
);

onMounted(loadAll);
</script>

<style scoped>
.tabs-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 4px 16px 0;
  flex-wrap: wrap;
}
.type-tabs {
  flex: 1;
  min-width: 260px;
  margin-bottom: -16px;
}
.type-tabs :deep(.ant-tabs-nav) {
  margin-bottom: 0;
}
.target {
  font-weight: 600;
}
.small {
  font-size: 11.5px;
}
.who {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.reason {
  font-size: 12.5px;
  color: var(--tm-text-2);
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.d-head {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  padding: 8px 0 16px;
}
.d-head h2 {
  margin: 0 0 6px;
  font-size: 18px;
  letter-spacing: -0.02em;
}
.d-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  font-size: 12px;
}
.block {
  margin-bottom: 18px;
}
.sub-title {
  margin: 0 0 8px;
  font-size: 13px;
  font-weight: 600;
}
.after {
  color: var(--tm-primary);
  font-weight: 500;
}
.resubmit-hint {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-top: 18px;
  flex-wrap: wrap;
}
.resubmit-hint :deep(.ant-alert) {
  flex: 1;
  min-width: 240px;
}
</style>
