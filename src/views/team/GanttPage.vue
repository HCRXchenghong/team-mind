<template>
  <div class="page">
    <PageHeader title="甘特图" :subtitle="`${tasks.length} 个任务 · ${projectName || '全部项目'} · 最长 120 天`">
      <template #actions>
        <a-button @click="exportCsv"><download-outlined /> 导出 CSV</a-button>
        <a-button :loading="insightLoading" @click="openInsights"><robot-outlined /> 排期体检</a-button>
        <a-button type="primary" ghost :disabled="!session.canManage" @click="openShare"><share-alt-outlined /> 只读分享</a-button>
      </template>
    </PageHeader>

    <a-alert v-if="error" type="error" show-icon :message="error" closable @close="error = ''" />

    <div class="panel pad">
      <div class="filters">
        <a-select v-model:value="filters.projectId" placeholder="全部项目" allow-clear style="width: 170px" @change="loadTasks">
          <a-select-option v-for="p in projects" :key="p.id" :value="p.id">{{ p.name }}</a-select-option>
        </a-select>
        <a-select
          v-if="session.isCompany"
          v-model:value="filters.departmentId"
          placeholder="全部部门"
          allow-clear
          style="width: 150px"
          @change="loadTasks"
        >
          <a-select-option v-for="d in departments" :key="d.id" :value="d.id">{{ d.name }}</a-select-option>
        </a-select>
        <a-select v-model:value="filters.assigneeId" placeholder="全部负责人" allow-clear style="width: 150px" @change="loadTasks">
          <a-select-option v-for="m in members" :key="m.id" :value="m.id">{{ m.name }}</a-select-option>
        </a-select>
        <a-select v-model:value="filters.status" placeholder="全部状态" allow-clear style="width: 130px" @change="loadTasks">
          <a-select-option v-for="s in TASK_STATUS" :key="s.value" :value="s.value">{{ s.label }}</a-select-option>
        </a-select>
        <a-date-picker v-model:value="filters.dateFrom" placeholder="开始" value-format="YYYY-MM-DD" style="width: 130px" @change="loadTasks" />
        <span class="muted">→</span>
        <a-date-picker v-model:value="filters.dateTo" placeholder="结束" value-format="YYYY-MM-DD" style="width: 130px" @change="loadTasks" />
        <a-button @click="locateToday">今天</a-button>
        <a-button type="text" @click="resetFilters">重置</a-button>
      </div>
    </div>

    <div class="edit-hint">
      <span class="hint-title">排期编辑</span>
      <span>拖拽任务条改期</span>
      <span>·</span>
      <span>拖动右侧圆点到另一任务建立依赖</span>
      <span>·</span>
      <span>点击连线删除依赖</span>
      <span class="hint-tail">{{ canDirectEdit ? '直接保存' : '成员改期会转为计划提案' }}</span>
    </div>

    <a-spin :spinning="loading">
      <GanttTimeline
        :tasks="tasks"
        :dependencies="dependencies"
        :from="filters.dateFrom"
        :to="filters.dateTo"
        editable
        @select="onSelect"
        @reschedule="onReschedule"
        @link="onLink"
        @unlink="onUnlink"
      />
    </a-spin>

    <div class="legend">
      <span><i class="lg doing" />进行中</span>
      <span><i class="lg review" />待审查</span>
      <span><i class="lg blocked" />阻塞</span>
      <span><i class="lg done" />已完成</span>
      <span><i class="lg todo" />待开始</span>
      <span class="muted">点击任务条可查看详情；依赖关系在任务详情里维护。</span>
    </div>

    <!-- 任务详情 -->
    <a-drawer v-model:open="detailOpen" title="任务详情" :width="560">
      <template v-if="current">
        <div class="d-title">{{ current.title }}</div>
        <div class="d-meta">
          <StatusTag kind="task" :value="current.status" dot />
          <StatusTag kind="priority" :value="current.priority" />
          <span class="muted">{{ current.assigneeName || '未分配' }}</span>
        </div>
        <a-descriptions :column="1" bordered size="small" style="margin-top: 16px">
          <a-descriptions-item label="起止">{{ fmtDate(current.startDate) }} → {{ fmtDate(current.dueDate) }}</a-descriptions-item>
          <a-descriptions-item label="进度">{{ current.progress }}%</a-descriptions-item>
          <a-descriptions-item label="里程碑">{{ current.milestone ? '是' : '否' }}</a-descriptions-item>
          <a-descriptions-item label="前置任务">
            <span v-if="!dependencies[current.id]?.length" class="muted">无</span>
            <a-tag v-for="id in dependencies[current.id] || []" :key="id">{{ taskTitle(id) }}</a-tag>
          </a-descriptions-item>
        </a-descriptions>
        <p class="muted" style="margin-top: 12px">{{ current.description || '暂无描述' }}</p>
        <a-button type="primary" @click="router.push({ path: '/teammind/tasks', query: { taskId: current.id } })">在任务中心打开</a-button>
      </template>
    </a-drawer>

    <!-- AI 排期体检 -->
    <a-modal v-model:open="insightOpen" title="AI 排期体检" :footer="null" width="680px">
      <a-spin :spinning="insightLoading">
        <div class="insight-meta">
          <a-tag color="blue">{{ insightTaskCount }} 个任务</a-tag>
          <span class="muted">{{ projectName || '全部项目' }}</span>
        </div>
        <div class="insight-body">{{ insight || '正在生成分析…' }}</div>
      </a-spin>
    </a-modal>

    <!-- 只读分享 -->
    <a-modal v-model:open="shareOpen" title="甘特图只读分享" :confirm-loading="sharing" width="600px" @ok="saveShare">
      <a-alert type="info" show-icon message="公开链接无需登录，只展示任务标题、状态、负责人、起止和进度。" style="margin-bottom: 14px" />
      <a-form layout="vertical">
        <a-form-item label="分享范围">
          <a-radio-group v-model:value="share.scopeType" button-style="solid" @change="loadShare">
            <a-radio-button value="all">整个组织</a-radio-button>
            <a-radio-button value="project">单个项目</a-radio-button>
            <a-radio-button value="unit">单个部门 / 小组</a-radio-button>
          </a-radio-group>
        </a-form-item>
        <a-form-item v-if="share.scopeType === 'project'" label="项目">
          <a-select v-model:value="share.projectId" placeholder="选择项目" @change="loadShare">
            <a-select-option v-for="p in projects" :key="p.id" :value="p.id">{{ p.name }}</a-select-option>
          </a-select>
        </a-form-item>
        <a-form-item v-if="share.scopeType === 'unit'" :label="session.isCompany ? '部门' : '小组'">
          <a-select v-model:value="share.unitId" placeholder="选择范围" @change="loadShare">
            <a-select-option v-for="d in departments" :key="d.id" :value="d.id">{{ d.name }}</a-select-option>
          </a-select>
        </a-form-item>
        <a-form-item label="分享状态">
          <a-switch v-model:checked="share.enabled" checked-children="已启用" un-checked-children="未启用" />
        </a-form-item>
        <a-form-item v-if="share.token" label="公开链接">
          <a-input-group compact>
            <a-input :value="publicLink" readonly style="width: calc(100% - 76px)" />
            <a-button @click="copyLink">复制</a-button>
          </a-input-group>
        </a-form-item>
        <a-form-item v-if="share.token" label="链接轮换">
          <a-button danger ghost :loading="rotating" @click="rotateLink"><reload-outlined /> 轮换链接（旧链接立即失效）</a-button>
        </a-form-item>
      </a-form>
      <p class="muted" style="margin: 0; font-size: 11.5px">只有组织管理员可以开启、关闭或轮换分享。</p>
    </a-modal>

    <!-- 成员改期提案 -->
    <a-modal
      v-model:open="proposalOpen"
      title="提交排期变更提案"
      :confirm-loading="saving"
      width="560px"
      @ok="submitProposal"
    >
      <a-alert
        type="info"
        show-icon
        message="你的角色不能直接修改正式计划，审批通过后才会生效。"
        style="margin-bottom: 14px"
      />
      <a-form layout="vertical">
        <a-form-item label="任务"><a-input :value="proposalTitle" disabled /></a-form-item>
        <a-form-item label="新排期">
          <div class="date-row">
            <a-date-picker v-model:value="proposal.startDate" value-format="YYYY-MM-DD" style="width: 100%" />
            <span class="muted">→</span>
            <a-date-picker v-model:value="proposal.dueDate" value-format="YYYY-MM-DD" style="width: 100%" />
          </div>
        </a-form-item>
        <a-form-item label="变更原因" required>
          <a-textarea v-model:value="proposal.reason" :rows="2" placeholder="至少 5 个字" />
        </a-form-item>
        <a-form-item label="影响说明">
          <a-textarea v-model:value="proposal.impactSummary" :rows="2" placeholder="对依赖方、交付节奏的影响" />
        </a-form-item>
      </a-form>
    </a-modal>

    <!-- 删除依赖确认 -->
    <a-modal v-model:open="unlinkOpen" title="删除依赖" :confirm-loading="saving" ok-text="删除依赖" @ok="confirmUnlink">
      <p style="margin: 0">
        确认删除「{{ unlinkState?.sourceTitle }}」→「{{ unlinkState?.targetTitle }}」的依赖关系？
      </p>
      <p class="muted" style="margin: 8px 0 0; font-size: 12px">删除后甘特图不再显示这条连线，任务本身不会被修改。</p>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import {
  DownloadOutlined,
  ReloadOutlined,
  RobotOutlined,
  ShareAltOutlined,
} from '@ant-design/icons-vue';
import { api, cleanParams } from '@/api/client';
import type { Department, Member, PageResult, Project, Task } from '@/api/types';
import { useSession } from '@/store/session';
import { downloadFile, fmtDate, toCsv } from '@/utils/format';
import { labelOf, TASK_STATUS } from '@/utils/labels';
import { notify } from '@/utils/notify';
import PageHeader from '@/components/PageHeader.vue';
import StatusTag from '@/components/StatusTag.vue';
import GanttTimeline from '@/components/GanttTimeline.vue';

const route = useRoute();
const router = useRouter();
const session = useSession();

const loading = ref(false);
const error = ref('');
const tasks = ref<Task[]>([]);
const dependencies = ref<Record<string, string[]>>({});
const projects = ref<Project[]>([]);
const members = ref<Member[]>([]);
const departments = ref<Department[]>([]);
const filters = reactive({
  projectId: String(route.query.projectId || '') || undefined,
  departmentId: String(route.query.departmentId || '') || undefined,
  assigneeId: undefined as string | undefined,
  status: undefined as string | undefined,
  dateFrom: undefined as string | undefined,
  dateTo: undefined as string | undefined,
});

const projectName = computed(() => projects.value.find((p) => p.id === filters.projectId)?.name || '');

async function loadMeta(): Promise<void> {
  if (!session.activeId) return;
  const teamId = session.activeId;
  const [projectPage, memberList, deptPage] = await Promise.all([
    api<PageResult<Project>>('GET', `/teams/${teamId}/projects`, undefined, { pageSize: 100 }),
    api<Member[]>('GET', `/teams/${teamId}/members`),
    api<{ items: Department[] }>('GET', `/teams/${teamId}/departments`),
  ]);
  if (teamId !== session.activeId) return;
  projects.value = projectPage.items;
  members.value = memberList;
  departments.value = deptPage.items;
}

async function loadDependencies(list: Task[]): Promise<void> {
  const map: Record<string, string[]> = {};
  const chunk = 10;
  for (let i = 0; i < list.length; i += chunk) {
    const slice = list.slice(i, i + chunk);
    await Promise.all(
      slice.map(async (task) => {
        try {
          map[task.id] = await api<string[]>('GET', `/teams/${session.activeId}/tasks/${task.id}/dependencies`);
        } catch {
          map[task.id] = [];
        }
      }),
    );
  }
  dependencies.value = map;
}

async function loadTasks(): Promise<void> {
  if (!session.activeId) return;
  loading.value = true;
  try {
    const teamId = session.activeId;
    const result = await api<PageResult<Task>>(
      'GET',
      `/teams/${teamId}/tasks`,
      undefined,
      cleanParams({
        gantt: 1,
        projectId: filters.projectId,
        departmentId: filters.departmentId,
        assigneeId: filters.assigneeId,
        status: filters.status,
        dateFrom: filters.dateFrom || '',
        dateTo: filters.dateTo || '',
        page: 1,
        pageSize: 100,
      }),
    );
    if (teamId !== session.activeId) return;
    tasks.value = result.items;
    error.value = '';
    await loadDependencies(result.items);
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}

const canDirectEdit = computed(() => session.isAdmin || session.isLead);

/* 拖拽改期 */
const proposalOpen = ref(false);
const proposalTitle = ref('');
const saving = ref(false);
const proposal = reactive<Record<string, any>>({
  taskId: '',
  title: '',
  description: '',
  priority: 'medium',
  status: 'todo',
  progress: 0,
  assigneeId: undefined,
  startDate: undefined as string | undefined,
  dueDate: undefined as string | undefined,
  milestone: false,
  reason: '',
  impactSummary: '',
});

async function onReschedule(payload: { task: Task; startDate: string; dueDate: string }): Promise<void> {
  const { task, startDate, dueDate } = payload;
  if (canDirectEdit.value) {
    try {
      await api('PATCH', `/teams/${session.activeId}/tasks/${task.id}`, {
        version: task.version,
        startDate,
        dueDate,
      });
      notify.success(`已调整为 ${startDate} → ${dueDate}`);
      await loadTasks();
    } catch (e) {
      notify.error(e);
    }
    return;
  }
  Object.assign(proposal, {
    taskId: task.id,
    title: task.title,
    description: task.description,
    priority: task.priority,
    status: task.status,
    progress: task.progress,
    assigneeId: task.assigneeId || undefined,
    startDate,
    dueDate,
    milestone: task.milestone,
    reason: '',
    impactSummary: '',
  });
  proposalTitle.value = task.title;
  proposalOpen.value = true;
}

async function submitProposal(): Promise<void> {
  if (proposal.reason.trim().length < 5) {
    notify.error('请填写至少 5 个字的变更原因');
    return;
  }
  saving.value = true;
  try {
    await api('POST', `/teams/${session.activeId}/plan-proposals`, {
      taskId: proposal.taskId,
      title: proposal.title,
      description: proposal.description,
      priority: proposal.priority,
      status: proposal.status,
      progress: proposal.progress,
      assigneeId: proposal.assigneeId || '',
      startDate: proposal.startDate || '',
      dueDate: proposal.dueDate || '',
      milestone: !!proposal.milestone,
      reason: proposal.reason,
      impactSummary: proposal.impactSummary,
    });
    proposalOpen.value = false;
    notify.success('排期变更提案已提交');
  } catch (e) {
    notify.error(e);
  } finally {
    saving.value = false;
  }
}

/* 依赖连线 */
async function onLink(payload: { from: string; to: string }): Promise<void> {
  if (payload.from === payload.to) return;
  if ((dependencies.value[payload.to] || []).includes(payload.from)) {
    notify.info('这两个任务之间已经有依赖了');
    return;
  }
  try {
    await api('POST', `/teams/${session.activeId}/tasks/${payload.to}/dependencies`, {
      dependsOnTaskId: payload.from,
    });
    const current = dependencies.value[payload.to] || [];
    dependencies.value = {
      ...dependencies.value,
      [payload.to]: Array.from(new Set([...current, payload.from])),
    };
    notify.success('依赖已建立');
  } catch (e) {
    notify.error(e);
  }
}

const unlinkOpen = ref(false);
const unlinkState = ref<{ taskId: string; dependsOnTaskId: string; sourceTitle: string; targetTitle: string } | null>(null);

function onUnlink(payload: { taskId: string; dependsOnTaskId: string }): void {
  const source = tasks.value.find((task) => task.id === payload.dependsOnTaskId);
  const target = tasks.value.find((task) => task.id === payload.taskId);
  unlinkState.value = {
    ...payload,
    sourceTitle: source?.title || payload.dependsOnTaskId.slice(0, 6),
    targetTitle: target?.title || payload.taskId.slice(0, 6),
  };
  unlinkOpen.value = true;
}

async function confirmUnlink(): Promise<void> {
  if (!unlinkState.value) return;
  saving.value = true;
  try {
    await api('DELETE', `/teams/${session.activeId}/tasks/${unlinkState.value.taskId}/dependencies`, {
      dependsOnTaskId: unlinkState.value.dependsOnTaskId,
    });
    const current = dependencies.value[unlinkState.value.taskId] || [];
    dependencies.value = {
      ...dependencies.value,
      [unlinkState.value.taskId]: current.filter((id) => id !== unlinkState.value?.dependsOnTaskId),
    };
    unlinkOpen.value = false;
    notify.success('依赖已删除');
  } catch (e) {
    notify.error(e);
  } finally {
    saving.value = false;
  }
}

function resetFilters(): void {
  Object.assign(filters, {
    projectId: undefined,
    departmentId: undefined,
    assigneeId: undefined,
    status: undefined,
    dateFrom: undefined,
    dateTo: undefined,
  });
  loadTasks();
}

function locateToday(): void {
  const now = new Date();
  const from = new Date(now.getFullYear(), now.getMonth(), 1);
  const to = new Date(now.getFullYear(), now.getMonth() + 1, 0);
  const fmt = (date: Date): string => date.toISOString().slice(0, 10);
  filters.dateFrom = fmt(from);
  filters.dateTo = fmt(to);
  loadTasks();
  window.scrollTo({ top: 0, behavior: 'smooth' });
}

function taskTitle(id: string): string {
  return tasks.value.find((task) => task.id === id)?.title || id.slice(0, 6);
}

/* 详情 */
const detailOpen = ref(false);
const current = ref<Task | null>(null);
function onSelect(task: Task): void {
  current.value = task;
  detailOpen.value = true;
}

/* CSV */
function exportCsv(): void {
  if (!tasks.value.length) {
    notify.info('当前范围没有任务可导出');
    return;
  }
  const rows: Array<Array<string | number>> = [['任务', '项目', '状态', '负责人', '开始', '截止', '进度', '里程碑']];
  tasks.value.forEach((task) => {
    rows.push([
      task.title,
      projects.value.find((p) => p.id === task.projectId)?.name || '',
      labelOf(TASK_STATUS, task.status),
      task.assigneeName || '未分配',
      task.startDate || '',
      task.dueDate || '',
      `${task.progress}%`,
      task.milestone ? '是' : '否',
    ]);
  });
  downloadFile(`teammind-gantt-${new Date().toISOString().slice(0, 10)}.csv`, `\uFEFF${toCsv(rows)}`, 'text/csv;charset=utf-8');
}

/* AI 体检 */
const insightOpen = ref(false);
const insightLoading = ref(false);
const insight = ref('');
const insightTaskCount = ref(0);
async function openInsights(): Promise<void> {
  insightOpen.value = true;
  insightLoading.value = true;
  insight.value = '';
  try {
    const result = await api<{ analysis: string; taskCount: number }>('POST', `/teams/${session.activeId}/gantt-insights`, {
      projectId: filters.projectId || '',
      status: filters.status || '',
      assigneeId: filters.assigneeId || '',
      dateFrom: filters.dateFrom || '',
      dateTo: filters.dateTo || '',
    });
    insight.value = result.analysis || '暂无分析结果';
    insightTaskCount.value = result.taskCount || tasks.value.length;
  } catch (e) {
    insight.value = (e as Error).message;
  } finally {
    insightLoading.value = false;
  }
}

/* 分享 */
const shareOpen = ref(false);
const sharing = ref(false);
const rotating = ref(false);
const share = reactive({
  scopeType: 'all' as 'all' | 'project' | 'unit',
  projectId: undefined as string | undefined,
  unitId: undefined as string | undefined,
  enabled: false,
  token: '',
});
const publicLink = computed(() => (share.token ? `${location.origin}/share/gantt/${share.token}` : ''));

function shareParams(): Record<string, string> {
  if (share.scopeType === 'project' && share.projectId) return { projectId: share.projectId };
  if (share.scopeType === 'unit' && share.unitId) return { unitId: share.unitId };
  return {};
}

async function loadShare(): Promise<void> {
  if (!session.activeId) return;
  try {
    const result = await api<{ enabled: boolean; scope: string; token?: string }>(
      'GET',
      `/teams/${session.activeId}/gantt-share`,
      undefined,
      shareParams(),
    );
    share.enabled = result.enabled;
    share.token = result.token || '';
  } catch (e) {
    share.enabled = false;
    share.token = '';
    if ((e as Error).message && !(e as Error).message.includes('不存在')) notify.error(e);
  }
}

async function openShare(): Promise<void> {
  shareOpen.value = true;
  share.scopeType = filters.projectId ? 'project' : filters.departmentId ? 'unit' : 'all';
  share.projectId = filters.projectId;
  share.unitId = filters.departmentId;
  await loadShare();
}

async function saveShare(): Promise<void> {
  if (share.scopeType === 'project' && !share.projectId) {
    notify.error('请选择要分享的项目');
    return;
  }
  if (share.scopeType === 'unit' && !share.unitId) {
    notify.error('请选择要分享的部门/小组');
    return;
  }
  sharing.value = true;
  try {
    await api('PATCH', `/teams/${session.activeId}/gantt-share`, {
      enabled: share.enabled,
      ...(share.scopeType === 'project' ? { projectId: share.projectId } : {}),
      ...(share.scopeType === 'unit' ? { unitId: share.unitId } : {}),
    });
    await loadShare();
    notify.success('分享设置已更新');
  } catch (e) {
    notify.error(e);
  } finally {
    sharing.value = false;
  }
}

async function rotateLink(): Promise<void> {
  rotating.value = true;
  try {
    await api('PATCH', `/teams/${session.activeId}/gantt-share`, { enabled: true, rotate: true, ...shareParams() });
    await loadShare();
    notify.success('链接已轮换，旧链接已失效');
  } catch (e) {
    notify.error(e);
  } finally {
    rotating.value = false;
  }
}

async function copyLink(): Promise<void> {
  try {
    await navigator.clipboard.writeText(publicLink.value);
    notify.success('链接已复制');
  } catch {
    notify.error('复制失败，请手动选择链接');
  }
}

watch(
  () => session.activeId,
  async () => {
    await loadMeta();
    await loadTasks();
  },
);

onMounted(async () => {
  await loadMeta();
  await loadTasks();
});
</script>

<style scoped>
.pad {
  padding: 14px 16px;
}
.edit-hint {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
  padding: 8px 12px;
  border-radius: 8px;
  border: 1px dashed var(--tm-border-strong);
  color: var(--tm-muted);
  font-size: 11.5px;
  background: var(--tm-panel-2);
}
.hint-title {
  color: var(--tm-primary);
  font-weight: 600;
}
.hint-tail {
  margin-left: auto;
  color: var(--tm-text-2);
}
.date-row {
  display: grid;
  grid-template-columns: 1fr auto 1fr;
  gap: 8px;
  align-items: center;
}
.legend {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 14px;
  color: var(--tm-text-2);
  font-size: 11.5px;
}
.lg {
  display: inline-block;
  width: 10px;
  height: 10px;
  border-radius: 3px;
  margin-right: 5px;
  vertical-align: -1px;
}
.lg.doing {
  background: #2f54eb;
}
.lg.review {
  background: #f79009;
}
.lg.blocked {
  background: #f04438;
}
.lg.done {
  background: #12b76a;
}
.lg.todo {
  background: var(--tm-border-strong);
}
.d-title {
  font-size: 18px;
  font-weight: 650;
  margin-bottom: 8px;
}
.d-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  font-size: 12px;
}
.insight-meta {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 12px;
}
.insight-body {
  max-height: 52vh;
  overflow: auto;
  padding: 14px;
  border-radius: 8px;
  background: var(--tm-panel-2);
  white-space: pre-wrap;
  font-size: 12.5px;
  line-height: 1.8;
  color: var(--tm-text-2);
}
</style>
