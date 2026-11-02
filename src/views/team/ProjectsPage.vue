<template>
  <div class="page">
    <PageHeader title="项目管理" :subtitle="`共 ${total} 个项目 · ${session.isCompany ? '按部门组织' : '团队共享'}`">
      <template #actions>
        <a-button :loading="loading" @click="load"><reload-outlined /> 刷新</a-button>
        <a-button type="primary" @click="openCreate"><plus-outlined /> 创建项目</a-button>
      </template>
    </PageHeader>

    <a-alert v-if="error" type="error" show-icon :message="error" closable @close="error = ''" />

    <div class="panel pad">
      <div class="filters">
        <a-input-search v-model:value="filters.q" placeholder="搜索项目名称" allow-clear style="width: 230px" @search="applyFilters" />
        <a-select v-model:value="filters.status" placeholder="全部状态" allow-clear style="width: 140px" @change="applyFilters">
          <a-select-option v-for="item in PROJECT_STATUS" :key="item.value" :value="item.value">{{ item.label }}</a-select-option>
        </a-select>
        <a-select
          v-if="session.isCompany"
          v-model:value="filters.departmentId"
          placeholder="全部部门"
          allow-clear
          style="width: 160px"
          @change="applyFilters"
        >
          <a-select-option v-for="d in departments" :key="d.id" :value="d.id">{{ d.name }}</a-select-option>
        </a-select>
        <a-button type="text" @click="resetFilters">重置</a-button>
      </div>
    </div>

    <div class="panel">
      <a-table
        :data-source="items"
        :columns="columns"
        :loading="loading"
        row-key="id"
        size="middle"
        :scroll="{ x: 1050 }"
        :pagination="{
          current: page,
          pageSize,
          total,
          showSizeChanger: false,
          showTotal: (value: number) => `共 ${value} 条`,
        }"
        @change="onTableChange"
      >
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'name'">
            <div class="proj-name">{{ record.name }}</div>
            <div class="proj-desc">{{ record.description || '暂无描述' }}</div>
          </template>
          <template v-else-if="column.key === 'status'">
            <StatusTag kind="project" :value="record.status" dot />
          </template>
          <template v-else-if="column.key === 'department'">
            <span class="muted">{{ record.departmentName || '—' }}</span>
          </template>
          <template v-else-if="column.key === 'progress'">
            <div class="progress-cell">
              <a-progress :percent="record.progress || 0" size="small" :show-info="false" />
              <span class="pct">{{ record.progress || 0 }}%</span>
            </div>
          </template>
          <template v-else-if="column.key === 'dates'">
            <div class="dates">
              <span>{{ fmtDate(record.startDate) }}</span>
              <span class="muted">→</span>
              <span>{{ fmtDate(record.dueDate) }}</span>
            </div>
          </template>
          <template v-else-if="column.key === 'actions'">
            <div class="table-actions">
              <a-button type="link" size="small" @click="router.push({ path: '/teammind/tasks', query: { projectId: record.id } })">任务</a-button>
              <a-button type="link" size="small" @click="router.push({ path: '/teammind/gantt', query: { projectId: record.id } })">甘特</a-button>
              <a-button type="link" size="small" @click="openProposal(record)">申请变更</a-button>
              <a-button v-if="canEdit(record)" type="link" size="small" @click="openEdit(record)">编辑</a-button>
              <a-popconfirm v-if="session.isAdmin" title="删除项目会同时影响其下任务，确认？" ok-text="删除" @confirm="removeProject(record)">
                <a-button type="link" size="small" danger>删除</a-button>
              </a-popconfirm>
            </div>
          </template>
        </template>
      </a-table>
    </div>

    <a-modal v-model:open="editOpen" :title="draft.id ? '编辑项目' : '创建项目'" :confirm-loading="saving" width="620px" @ok="saveProject">
      <a-form layout="vertical" :model="draft">
        <div class="form-grid">
          <a-form-item label="项目名称" required><a-input v-model:value="draft.name" :maxlength="200" /></a-form-item>
          <a-form-item label="状态">
            <a-select v-model:value="draft.status">
              <a-select-option v-for="s in PROJECT_STATUS" :key="s.value" :value="s.value">{{ s.label }}</a-select-option>
            </a-select>
          </a-form-item>
          <a-form-item class="wide" label="描述">
            <a-textarea v-model:value="draft.description" :rows="3" :maxlength="10000" />
          </a-form-item>
          <a-form-item label="开始日期"><a-date-picker v-model:value="draft.startDate" value-format="YYYY-MM-DD" style="width: 100%" /></a-form-item>
          <a-form-item label="截止日期"><a-date-picker v-model:value="draft.dueDate" value-format="YYYY-MM-DD" style="width: 100%" /></a-form-item>
          <a-form-item v-if="session.isCompany" class="wide" label="所属部门">
            <a-select v-model:value="draft.departmentId" allow-clear placeholder="不指定部门">
              <a-select-option v-for="d in departments" :key="d.id" :value="d.id">{{ d.name }}</a-select-option>
            </a-select>
          </a-form-item>
        </div>
      </a-form>
    </a-modal>

    <a-modal v-model:open="proposalOpen" title="项目变更提案" :confirm-loading="saving" width="620px" @ok="submitProposal">
      <a-alert type="info" show-icon message="审批通过前，正式项目不会改变。" style="margin-bottom: 14px" />
      <a-form layout="vertical" :model="proposal">
        <div class="form-grid">
          <a-form-item label="项目名称"><a-input v-model:value="proposal.name" /></a-form-item>
          <a-form-item label="状态">
            <a-select v-model:value="proposal.status">
              <a-select-option v-for="s in PROJECT_STATUS" :key="s.value" :value="s.value">{{ s.label }}</a-select-option>
            </a-select>
          </a-form-item>
          <a-form-item class="wide" label="描述"><a-textarea v-model:value="proposal.description" :rows="2" /></a-form-item>
          <a-form-item label="开始日期"><a-date-picker v-model:value="proposal.startDate" value-format="YYYY-MM-DD" style="width: 100%" /></a-form-item>
          <a-form-item label="截止日期"><a-date-picker v-model:value="proposal.dueDate" value-format="YYYY-MM-DD" style="width: 100%" /></a-form-item>
          <a-form-item v-if="session.isCompany" label="部门">
            <a-select v-model:value="proposal.departmentId" allow-clear>
              <a-select-option v-for="d in departments" :key="d.id" :value="d.id">{{ d.name }}</a-select-option>
            </a-select>
          </a-form-item>
          <a-form-item class="wide" label="变更原因" required>
            <a-textarea v-model:value="proposal.reason" :rows="2" placeholder="至少 5 个字" />
          </a-form-item>
          <a-form-item class="wide" label="影响说明">
            <a-textarea v-model:value="proposal.impactSummary" :rows="2" />
          </a-form-item>
        </div>
      </a-form>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons-vue';
import { api, cleanParams } from '@/api/client';
import type { Department, PageResult, Project } from '@/api/types';
import { useSession } from '@/store/session';
import { fmtDate } from '@/utils/format';
import { PROJECT_STATUS } from '@/utils/labels';
import { notify } from '@/utils/notify';
import PageHeader from '@/components/PageHeader.vue';
import StatusTag from '@/components/StatusTag.vue';

const route = useRoute();
const router = useRouter();
const session = useSession();

const loading = ref(false);
const saving = ref(false);
const error = ref('');
const items = ref<Project[]>([]);
const departments = ref<Department[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = 20;

const filters = reactive({
  q: '',
  status: undefined as string | undefined,
  departmentId: String(route.query.departmentId || '') || undefined,
});

const columns = computed(() => [
  { title: '项目', key: 'name', dataIndex: 'name', width: 300 },
  { title: '状态', key: 'status', width: 104 },
  ...(session.isCompany ? [{ title: '部门', key: 'department', width: 120 }] : []),
  { title: '进度', key: 'progress', width: 150 },
  { title: '起止日期', key: 'dates', width: 190 },
  { title: '操作', key: 'actions', width: 300, fixed: 'right' as const },
]);

function canEdit(project: any): boolean {
  if (session.isAdmin) return true;
  if (!session.isLead) return false;
  if (!project.departmentId) return false;
  const dept = departments.value.find((d) => d.id === project.departmentId);
  return dept?.leadUserId === session.user?.id;
}

async function loadDepartments(): Promise<void> {
  if (!session.activeId) return;
  try {
    const result = await api<{ items: Department[] }>('GET', `/teams/${session.activeId}/departments`);
    departments.value = result.items;
  } catch {
    departments.value = [];
  }
}

async function load(): Promise<void> {
  if (!session.activeId) return;
  loading.value = true;
  try {
    const teamId = session.activeId;
    const result = await api<PageResult<Project>>(
      'GET',
      `/teams/${teamId}/projects`,
      undefined,
      cleanParams({ ...filters, q: filters.q.trim(), page: page.value, pageSize }),
    );
    if (teamId !== session.activeId) return;
    items.value = result.items;
    total.value = result.total;
    error.value = '';
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}

function applyFilters(): void {
  page.value = 1;
  load();
}

function resetFilters(): void {
  filters.q = '';
  filters.status = undefined;
  filters.departmentId = undefined;
  page.value = 1;
  load();
}

function onTableChange(pagination: { current?: number }): void {
  page.value = pagination.current || 1;
  load();
}

/* 新建 / 编辑 */
const editOpen = ref(false);
const draft = reactive<Record<string, any>>({
  id: '',
  name: '',
  description: '',
  status: 'planning',
  startDate: null,
  dueDate: null,
  departmentId: undefined,
});

function openCreate(): void {
  Object.assign(draft, {
    id: '',
    name: '',
    description: '',
    status: 'planning',
    startDate: null,
    dueDate: null,
    departmentId: undefined,
  });
  editOpen.value = true;
}

function openEdit(project: any): void {
  Object.assign(draft, {
    ...project,
    startDate: project.startDate || null,
    dueDate: project.dueDate || null,
    departmentId: project.departmentId || undefined,
  });
  editOpen.value = true;
}

async function saveProject(): Promise<void> {
  if (!draft.name.trim()) {
    notify.error('请输入项目名称');
    return;
  }
  saving.value = true;
  try {
    const body = {
      name: draft.name,
      description: draft.description,
      status: draft.status,
      startDate: draft.startDate || '',
      dueDate: draft.dueDate || '',
      departmentId: draft.departmentId || '',
      ...(draft.id ? { version: draft.version } : {}),
    };
    if (draft.id) {
      await api('PATCH', `/teams/${session.activeId}/projects/${draft.id}`, body);
      notify.success('项目已保存');
    } else {
      await api('POST', `/teams/${session.activeId}/projects`, body);
      notify.success('项目已创建');
    }
    editOpen.value = false;
    await Promise.all([load(), loadDepartments()]);
  } catch (e) {
    notify.error(e);
  } finally {
    saving.value = false;
  }
}

async function removeProject(project: any): Promise<void> {
  try {
    await api('DELETE', `/teams/${session.activeId}/projects/${project.id}`);
    notify.success('项目已删除');
    await load();
  } catch (e) {
    notify.error(e);
  }
}

/* 提案 */
const proposalOpen = ref(false);
const proposal = reactive<Record<string, any>>({
  projectId: '',
  name: '',
  description: '',
  status: 'planning',
  startDate: null,
  dueDate: null,
  departmentId: undefined,
  reason: '',
  impactSummary: '',
});

function openProposal(project: any): void {
  Object.assign(proposal, {
    projectId: project.id,
    name: project.name,
    description: project.description,
    status: project.status,
    startDate: project.startDate || null,
    dueDate: project.dueDate || null,
    departmentId: project.departmentId || undefined,
    reason: '',
    impactSummary: '',
  });
  proposalOpen.value = true;
}

async function submitProposal(): Promise<void> {
  if (proposal.reason.trim().length < 5) {
    notify.error('请填写至少 5 个字的变更原因');
    return;
  }
  saving.value = true;
  try {
    await api('POST', `/teams/${session.activeId}/project-proposals`, {
      projectId: proposal.projectId,
      name: proposal.name,
      description: proposal.description,
      status: proposal.status,
      startDate: proposal.startDate || '',
      dueDate: proposal.dueDate || '',
      departmentId: proposal.departmentId || '',
      reason: proposal.reason,
      impactSummary: proposal.impactSummary,
    });
    proposalOpen.value = false;
    notify.success('提案已提交，等待审批');
  } catch (e) {
    notify.error(e);
  } finally {
    saving.value = false;
  }
}

watch(
  () => session.activeId,
  () => {
    page.value = 1;
    loadDepartments();
    load();
  },
);

onMounted(() => {
  loadDepartments();
  load();
});
</script>

<style scoped>
.pad {
  padding: 14px 16px;
}
.proj-name {
  font-weight: 600;
}
.proj-desc {
  margin-top: 2px;
  color: var(--tm-muted);
  font-size: 11.5px;
  max-width: 420px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.progress-cell {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 110px;
}
.progress-cell :deep(.ant-progress) {
  flex: 1;
  margin: 0;
}
.pct {
  width: 36px;
  text-align: right;
  color: var(--tm-muted);
  font-size: 11.5px;
  font-variant-numeric: tabular-nums;
}
.dates {
  display: flex;
  gap: 6px;
  font-size: 12px;
  color: var(--tm-text-2);
  white-space: nowrap;
}
</style>
