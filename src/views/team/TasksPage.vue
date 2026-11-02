<template>
  <div class="page">
    <PageHeader title="任务中心" :subtitle="`共 ${total} 个任务 · 当前页阻塞 ${blockedCount} 个`">
      <template #actions>
        <a-button :loading="loading" @click="reload"><reload-outlined /> 刷新</a-button>
        <a-button type="primary" @click="openCreate"><plus-outlined /> 新建任务</a-button>
      </template>
    </PageHeader>

    <a-alert v-if="error" type="error" show-icon :message="error" closable @close="error = ''" />

    <div class="panel pad">
      <div class="filters">
        <a-input-search
          v-model:value="filters.q"
          placeholder="搜索任务标题"
          allow-clear
          style="width: 220px"
          @search="applyFilters"
        />
        <a-select v-model:value="filters.status" placeholder="全部状态" allow-clear style="width: 130px" @change="applyFilters">
          <a-select-option v-for="item in TASK_STATUS" :key="item.value" :value="item.value">{{ item.label }}</a-select-option>
        </a-select>
        <a-select v-model:value="filters.projectId" placeholder="全部项目" allow-clear style="width: 170px" @change="applyFilters">
          <a-select-option v-for="p in projects" :key="p.id" :value="p.id">{{ p.name }}</a-select-option>
        </a-select>
        <a-select
          v-if="session.isCompany"
          v-model:value="filters.departmentId"
          placeholder="全部部门"
          allow-clear
          style="width: 150px"
          @change="applyFilters"
        >
          <a-select-option v-for="d in departments" :key="d.id" :value="d.id">{{ d.name }}</a-select-option>
        </a-select>
        <a-select v-model:value="filters.assigneeId" placeholder="全部负责人" allow-clear style="width: 150px" @change="applyFilters">
          <a-select-option value="unassigned">未分配</a-select-option>
          <a-select-option v-for="m in members" :key="m.id" :value="m.id">{{ m.name }}</a-select-option>
        </a-select>
        <a-select v-model:value="filters.sortBy" style="width: 128px" @change="applyFilters">
          <a-select-option value="createdAt">创建时间</a-select-option>
          <a-select-option value="dueDate">截止日期</a-select-option>
          <a-select-option value="priority">优先级</a-select-option>
          <a-select-option value="title">标题</a-select-option>
        </a-select>
        <a-tooltip :title="filters.sortDir === 'DESC' ? '当前降序' : '当前升序'">
          <a-button @click="toggleSortDir">{{ filters.sortDir === 'DESC' ? '↓' : '↑' }}</a-button>
        </a-tooltip>
        <a-button :type="mineOnly ? 'primary' : 'default'" @click="applyMine">我的任务</a-button>
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
        :scroll="{ x: 1080 }"
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
          <template v-if="column.key === 'title'">
            <div class="cell-title">
              <button class="link-btn" @click="openDetail(record)">{{ record.title }}</button>
              <a-tag v-if="record.milestone" color="purple" :bordered="false" class="ms-tag">里程碑</a-tag>
            </div>
            <div class="cell-sub">{{ projectName(record.projectId) }} · #{{ record.id.slice(0, 6).toUpperCase() }}</div>
          </template>
          <template v-else-if="column.key === 'status'">
            <StatusTag kind="task" :value="record.status" dot />
          </template>
          <template v-else-if="column.key === 'priority'">
            <StatusTag kind="priority" :value="record.priority" />
          </template>
          <template v-else-if="column.key === 'assignee'">
            <span class="who"><UserAvatar :name="record.assigneeName" :size="20" />{{ record.assigneeName || '未分配' }}</span>
          </template>
          <template v-else-if="column.key === 'department'">
            <span class="muted">{{ record.departmentName || '—' }}</span>
          </template>
          <template v-else-if="column.key === 'progress'">
            <div class="progress-cell">
              <a-progress :percent="record.progress" size="small" :show-info="false" />
              <span class="pct">{{ record.progress }}%</span>
            </div>
          </template>
          <template v-else-if="column.key === 'dueDate'">
            <span :class="{ overdue: isOverdue(record) }">{{ fmtDate(record.dueDate) }}</span>
          </template>
          <template v-else-if="column.key === 'actions'">
            <div class="table-actions">
              <a-button type="link" size="small" @click="openDetail(record)">详情</a-button>
              <a-button v-if="canEditTask" type="link" size="small" @click="openEdit(record)">编辑</a-button>
              <a-button type="link" size="small" @click="openProposal(record)">提案</a-button>
              <a-popconfirm v-if="session.isAdmin" title="确认删除该任务？" ok-text="删除" cancel-text="取消" @confirm="removeTask(record)">
                <a-button type="link" size="small" danger>删除</a-button>
              </a-popconfirm>
            </div>
          </template>
        </template>
      </a-table>
    </div>

    <!-- 新建 / 编辑 -->
    <a-modal
      v-model:open="editOpen"
      :title="draft.id ? '编辑任务' : '新建任务'"
      :confirm-loading="saving"
      width="640px"
      @ok="saveTask"
    >
      <a-form layout="vertical" :model="draft">
        <div class="form-grid">
          <a-form-item label="所属项目" required>
            <a-select v-model:value="draft.projectId" :disabled="!!draft.id" placeholder="选择项目" show-search option-filter-prop="label">
              <a-select-option v-for="p in projects" :key="p.id" :value="p.id" :label="p.name">{{ p.name }}</a-select-option>
            </a-select>
          </a-form-item>
          <a-form-item label="任务标题" required>
            <a-input v-model:value="draft.title" :maxlength="240" placeholder="例如：补齐审批流批量处理" />
          </a-form-item>
          <a-form-item label="描述" class="wide">
            <a-textarea v-model:value="draft.description" :rows="3" :maxlength="20000" placeholder="背景、验收标准、注意事项" />
          </a-form-item>
          <a-form-item label="负责人">
            <a-select v-model:value="draft.assigneeId" allow-clear placeholder="未分配">
              <a-select-option v-for="m in members" :key="m.id" :value="m.id">{{ m.name }}</a-select-option>
            </a-select>
          </a-form-item>
          <a-form-item label="优先级">
            <a-select v-model:value="draft.priority">
              <a-select-option v-for="p in TASK_PRIORITY" :key="p.value" :value="p.value">{{ p.label }}</a-select-option>
            </a-select>
          </a-form-item>
          <a-form-item v-if="draft.id" label="状态">
            <a-select v-model:value="draft.status">
              <a-select-option v-for="s in TASK_STATUS" :key="s.value" :value="s.value">{{ s.label }}</a-select-option>
            </a-select>
          </a-form-item>
          <a-form-item v-if="draft.id" label="进度">
            <a-input-number v-model:value="draft.progress" :min="0" :max="100" style="width: 100%" />
          </a-form-item>
          <a-form-item label="开始日期">
            <a-date-picker v-model:value="draft.startDate" value-format="YYYY-MM-DD" style="width: 100%" />
          </a-form-item>
          <a-form-item label="截止日期">
            <a-date-picker v-model:value="draft.dueDate" value-format="YYYY-MM-DD" style="width: 100%" />
          </a-form-item>
          <a-form-item>
            <a-checkbox v-model:checked="draft.milestone">标记为里程碑</a-checkbox>
          </a-form-item>
        </div>
      </a-form>
    </a-modal>

    <!-- 详情抽屉 -->
    <a-drawer v-model:open="detailOpen" title="任务详情" :width="720" :body-style="{ padding: '0 20px 24px' }">
      <template v-if="selected">
        <div class="detail-head">
          <div>
            <h2>{{ selected.title }}</h2>
            <div class="detail-meta">
              <StatusTag kind="task" :value="selected.status" dot />
              <StatusTag kind="priority" :value="selected.priority" />
              <span class="muted">{{ projectName(selected.projectId) }}</span>
              <span class="muted">· 版本 {{ selected.version }}</span>
            </div>
          </div>
          <a-button size="small" @click="openEdit(selected)">编辑</a-button>
        </div>

        <p class="description">{{ selected.description || '暂无描述' }}</p>

        <a-descriptions :column="2" size="small" bordered class="desc">
          <a-descriptions-item label="负责人">{{ selected.assigneeName || '未分配' }}</a-descriptions-item>
          <a-descriptions-item label="部门">{{ selected.departmentName || '—' }}</a-descriptions-item>
          <a-descriptions-item label="开始">{{ fmtDate(selected.startDate) }}</a-descriptions-item>
          <a-descriptions-item label="截止">{{ fmtDate(selected.dueDate) }}</a-descriptions-item>
          <a-descriptions-item label="进度">{{ selected.progress }}%</a-descriptions-item>
          <a-descriptions-item label="里程碑">{{ selected.milestone ? '是' : '否' }}</a-descriptions-item>
        </a-descriptions>

        <div class="status-row">
          <a-select v-model:value="detailStatus" style="width: 140px">
            <a-select-option v-for="s in TASK_STATUS" :key="s.value" :value="s.value">{{ s.label }}</a-select-option>
          </a-select>
          <a-input-number v-model:value="detailProgress" :min="0" :max="100" style="width: 110px" addon-after="%" />
          <a-button type="primary" :loading="saving" @click="updateStatus">保存状态</a-button>
          <span class="muted" style="font-size: 11.5px">只会更新状态与进度；计划字段变更请走提案。</span>
        </div>

        <a-tabs v-model:activeKey="detailTab" size="small" class="detail-tabs">
          <a-tab-pane key="comments" :tab="`评论 ${comments.length}`">
            <div class="comment-list">
              <div v-for="comment in comments" :key="comment.id" class="comment">
                <UserAvatar :name="comment.authorName" :size="24" />
                <div class="comment-body">
                  <div class="comment-head">
                    <b>{{ comment.authorName || '成员' }}</b>
                    <span class="muted">{{ fmtDateTime(comment.createdAt) }}</span>
                    <span class="comment-actions">
                      <a-button type="link" size="small" @click="openCommentEdit(comment)">编辑</a-button>
                      <a-popconfirm title="删除这条评论？" @confirm="removeComment(comment.id)">
                        <a-button type="link" size="small" danger>删除</a-button>
                      </a-popconfirm>
                    </span>
                  </div>
                  <div class="comment-text">{{ comment.body }}</div>
                </div>
              </div>
              <div v-if="!comments.length" class="empty-block small">还没有评论</div>
            </div>
            <div class="comment-input">
              <a-textarea v-model:value="commentText" :rows="2" placeholder="写下评论，支持多行" />
              <a-button type="primary" :disabled="!commentText.trim()" @click="addComment">发送</a-button>
            </div>
          </a-tab-pane>

          <a-tab-pane key="deps" :tab="`依赖 ${dependencies.length}`">
            <div class="dep-list">
              <a-tag v-for="id in dependencies" :key="id" closable color="blue" @close="removeDependency(id)">
                {{ taskName(id) }}
              </a-tag>
              <span v-if="!dependencies.length" class="muted">尚未设置前置任务</span>
            </div>
            <div class="dep-add">
              <a-select v-model:value="dependencyPick" placeholder="选择前置任务" show-search style="width: 260px" option-filter-prop="label">
                <a-select-option
                  v-for="t in allTasks.filter((x) => x.id !== selected?.id && !dependencies.includes(x.id))"
                  :key="t.id"
                  :value="t.id"
                  :label="t.title"
                >
                  {{ t.title }}
                </a-select-option>
              </a-select>
              <a-button :disabled="!dependencyPick" @click="addDependency">添加依赖</a-button>
            </div>
          </a-tab-pane>

          <a-tab-pane key="people" :tab="`协作者 ${collaborators.length}`">
            <div class="dep-list">
              <a-tag v-for="m in collaborators" :key="m.id" closable @close="removeCollaborator(m.id)">{{ m.name }}</a-tag>
              <span v-if="!collaborators.length" class="muted">还没有协作者</span>
            </div>
            <div class="dep-add">
              <a-select v-model:value="collaboratorPick" placeholder="选择成员" show-search style="width: 220px" option-filter-prop="label">
                <a-select-option
                  v-for="m in members.filter((x) => !collaborators.some((c) => c.id === x.id))"
                  :key="m.id"
                  :value="m.id"
                  :label="m.name"
                >
                  {{ m.name }}
                </a-select-option>
              </a-select>
              <a-button :disabled="!collaboratorPick" @click="addCollaborator">添加协作者</a-button>
            </div>
          </a-tab-pane>

          <a-tab-pane key="history" :tab="`历史 ${history.length}`">
            <a-timeline class="history">
              <a-timeline-item v-for="item in history" :key="`${item.version}-${item.createdAt}`">
                <b>版本 {{ item.version }}</b>
                <span class="muted"> · {{ item.source || 'update' }} · {{ fmtDateTime(item.createdAt) }}</span>
              </a-timeline-item>
            </a-timeline>
            <div v-if="!history.length" class="empty-block small">暂无版本记录</div>
          </a-tab-pane>
        </a-tabs>
      </template>
    </a-drawer>

    <a-modal v-model:open="commentOpen" title="编辑评论" :confirm-loading="saving" @ok="saveComment">
      <a-textarea v-model:value="commentDraft" :rows="4" />
    </a-modal>

    <!-- 计划变更提案 -->
    <a-modal v-model:open="proposalOpen" title="任务计划变更提案" :confirm-loading="saving" width="620px" @ok="submitProposal">
      <a-alert type="info" show-icon message="审批通过前不会修改正式计划" style="margin-bottom: 14px" />
      <a-form layout="vertical" :model="proposal">
        <div class="form-grid">
          <a-form-item label="任务标题"><a-input v-model:value="proposal.title" /></a-form-item>
          <a-form-item label="优先级">
            <a-select v-model:value="proposal.priority">
              <a-select-option v-for="p in TASK_PRIORITY" :key="p.value" :value="p.value">{{ p.label }}</a-select-option>
            </a-select>
          </a-form-item>
          <a-form-item label="状态">
            <a-select v-model:value="proposal.status">
              <a-select-option v-for="s in TASK_STATUS" :key="s.value" :value="s.value">{{ s.label }}</a-select-option>
            </a-select>
          </a-form-item>
          <a-form-item label="进度">
            <a-input-number v-model:value="proposal.progress" :min="0" :max="100" style="width: 100%" />
          </a-form-item>
          <a-form-item label="负责人">
            <a-select v-model:value="proposal.assigneeId" allow-clear>
              <a-select-option v-for="m in members" :key="m.id" :value="m.id">{{ m.name }}</a-select-option>
            </a-select>
          </a-form-item>
          <a-form-item label="开始日期"><a-date-picker v-model:value="proposal.startDate" value-format="YYYY-MM-DD" style="width: 100%" /></a-form-item>
          <a-form-item label="截止日期"><a-date-picker v-model:value="proposal.dueDate" value-format="YYYY-MM-DD" style="width: 100%" /></a-form-item>
          <a-form-item class="wide" label="变更原因" required>
            <a-textarea v-model:value="proposal.reason" :rows="2" placeholder="至少 5 个字" />
          </a-form-item>
          <a-form-item class="wide" label="影响说明">
            <a-textarea v-model:value="proposal.impactSummary" :rows="2" placeholder="对排期、依赖方或交付的影响" />
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
import type { Department, Member, PageResult, Project, Task } from '@/api/types';
import { useSession } from '@/store/session';
import { fmtDate, fmtDateTime } from '@/utils/format';
import { TASK_PRIORITY, TASK_STATUS } from '@/utils/labels';
import { notify } from '@/utils/notify';
import PageHeader from '@/components/PageHeader.vue';
import StatusTag from '@/components/StatusTag.vue';
import UserAvatar from '@/components/UserAvatar.vue';

const route = useRoute();
const router = useRouter();
const session = useSession();

const loading = ref(false);
const saving = ref(false);
const error = ref('');
const items = ref<Task[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(20);
const projects = ref<Project[]>([]);
const members = ref<Member[]>([]);
const departments = ref<Department[]>([]);
const allTasks = ref<Task[]>([]);

const filters = reactive({
  q: '',
  status: undefined as string | undefined,
  projectId: undefined as string | undefined,
  departmentId: undefined as string | undefined,
  assigneeId: undefined as string | undefined,
  sortBy: 'createdAt',
  sortDir: 'DESC' as 'ASC' | 'DESC',
});
const mineOnly = ref(false);

const columns = computed(() => {
  const cols = [
    { title: '任务', key: 'title', dataIndex: 'title', width: 280 },
    { title: '状态', key: 'status', width: 100 },
    { title: '优先级', key: 'priority', width: 92 },
    { title: '负责人', key: 'assignee', width: 120 },
    ...(session.isCompany ? [{ title: '部门', key: 'department', width: 110 }] : []),
    { title: '进度', key: 'progress', width: 140 },
    { title: '截止', key: 'dueDate', dataIndex: 'dueDate', width: 100 },
    { title: '操作', key: 'actions', width: 190, fixed: 'right' as const },
  ];
  return cols;
});

const blockedCount = computed(() => items.value.filter((task) => task.status === 'blocked').length);
const canEditTask = computed(() => session.isAdmin || session.isLead);

function projectName(id: string): string {
  return projects.value.find((p) => p.id === id)?.name || '未归属项目';
}

function taskName(id: string): string {
  return allTasks.value.find((t) => t.id === id)?.title || `任务 ${id.slice(0, 6)}`;
}

function isOverdue(task: any): boolean {
  if (!task.dueDate || task.status === 'done' || task.status === 'cancelled') return false;
  return new Date(task.dueDate).getTime() < Date.now() - 86400000;
}

async function loadMeta(): Promise<void> {
  if (!session.activeId) return;
  const teamId = session.activeId;
  const [projectPage, memberList, deptPage, taskPage] = await Promise.all([
    api<PageResult<Project>>('GET', `/teams/${teamId}/projects`, undefined, { pageSize: 100 }),
    api<Member[]>('GET', `/teams/${teamId}/members`),
    api<{ items: Department[] }>('GET', `/teams/${teamId}/departments`),
    api<PageResult<Task>>('GET', `/teams/${teamId}/tasks`, undefined, { pageSize: 100, sortBy: 'createdAt', sortDir: 'DESC' }),
  ]);
  if (teamId !== session.activeId) return;
  projects.value = projectPage.items;
  members.value = memberList;
  departments.value = deptPage.items;
  allTasks.value = taskPage.items;
}

async function load(): Promise<void> {
  if (!session.activeId) return;
  loading.value = true;
  try {
    const teamId = session.activeId;
    const result = await api<PageResult<Task>>(
      'GET',
      `/teams/${teamId}/tasks`,
      undefined,
      cleanParams({
        q: filters.q.trim(),
        status: filters.status,
        projectId: filters.projectId,
        departmentId: filters.departmentId,
        assigneeId: filters.assigneeId,
        sortBy: filters.sortBy,
        sortDir: filters.sortDir,
        page: page.value,
        pageSize: pageSize.value,
      }),
    );
    if (teamId !== session.activeId) return;
    items.value = result.items;
    total.value = result.total;
    error.value = '';

    const taskId = String(route.query.taskId || '');
    if (taskId && !detailOpen.value) {
      const found = result.items.find((task) => task.id === taskId) || (await api<Task>('GET', `/teams/${teamId}/tasks/${taskId}`));
      if (teamId === session.activeId) openDetail(found);
    }
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}

async function reload(): Promise<void> {
  await Promise.all([loadMeta(), load()]);
}

function onTableChange(pagination: { current?: number }): void {
  page.value = pagination.current || 1;
  load();
}

function applyFilters(): void {
  page.value = 1;
  load();
}

function toggleSortDir(): void {
  filters.sortDir = filters.sortDir === 'DESC' ? 'ASC' : 'DESC';
  applyFilters();
}

function applyMine(): void {
  mineOnly.value = !mineOnly.value;
  filters.assigneeId = mineOnly.value ? session.user?.id : undefined;
  applyFilters();
}

function resetFilters(): void {
  mineOnly.value = false;
  Object.assign(filters, {
    q: '',
    status: undefined,
    projectId: undefined,
    departmentId: undefined,
    assigneeId: undefined,
    sortBy: 'createdAt',
    sortDir: 'DESC',
  });
  page.value = 1;
  load();
}

/* ---------- 新建 / 编辑 ---------- */
const editOpen = ref(false);
const draft = reactive<Record<string, any>>({
  id: '',
  projectId: undefined,
  title: '',
  description: '',
  assigneeId: undefined,
  priority: 'medium',
  status: 'todo',
  progress: 0,
  startDate: null,
  dueDate: null,
  milestone: false,
  version: 0,
});

function openCreate(): void {
  Object.assign(draft, {
    id: '',
    projectId: filters.projectId || projects.value[0]?.id,
    title: '',
    description: '',
    assigneeId: session.user?.id,
    priority: 'medium',
    status: 'todo',
    progress: 0,
    startDate: null,
    dueDate: null,
    milestone: false,
    version: 0,
  });
  editOpen.value = true;
}

function openEdit(task: any): void {
  Object.assign(draft, {
    ...task,
    startDate: task.startDate || null,
    dueDate: task.dueDate || null,
  });
  editOpen.value = true;
}

async function saveTask(): Promise<void> {
  if (!draft.projectId) {
    notify.error('请选择所属项目');
    return;
  }
  if (!String(draft.title || '').trim()) {
    notify.error('请填写任务标题');
    return;
  }
  saving.value = true;
  try {
    if (draft.id) {
      await api('PATCH', `/teams/${session.activeId}/tasks/${draft.id}`, {
        version: draft.version,
        title: draft.title,
        description: draft.description,
        priority: draft.priority,
        status: draft.status,
        progress: draft.progress,
        assigneeId: draft.assigneeId || '',
        startDate: draft.startDate || '',
        dueDate: draft.dueDate || '',
        milestone: !!draft.milestone,
      });
      notify.success('任务已保存');
    } else {
      await api('POST', `/teams/${session.activeId}/tasks`, {
        projectId: draft.projectId,
        title: draft.title,
        description: draft.description,
        assigneeId: draft.assigneeId || '',
        priority: draft.priority,
        startDate: draft.startDate || '',
        dueDate: draft.dueDate || '',
        milestone: !!draft.milestone,
      });
      notify.success('任务已创建');
    }
    editOpen.value = false;
    await reload();
  } catch (e) {
    notify.error(e);
  } finally {
    saving.value = false;
  }
}

async function removeTask(task: any): Promise<void> {
  try {
    await api('DELETE', `/teams/${session.activeId}/tasks/${task.id}`);
    notify.success('任务已删除');
    await load();
  } catch (e) {
    notify.error(e);
  }
}

/* ---------- 详情 ---------- */
const detailOpen = ref(false);
const detailTab = ref('comments');
const selected = ref<Task | null>(null);
const detailStatus = ref('todo');
const detailProgress = ref(0);
const comments = ref<any[]>([]);
const dependencies = ref<string[]>([]);
const collaborators = ref<Member[]>([]);
const history = ref<Array<{ version: number; source: string; createdAt: string }>>([]);
const commentText = ref('');
const commentOpen = ref(false);
const commentDraft = ref('');
const commentTargetId = ref('');
const dependencyPick = ref<string | undefined>();
const collaboratorPick = ref<string | undefined>();

async function openDetail(task: any): Promise<void> {
  detailOpen.value = true;
  detailTab.value = 'comments';
  selected.value = task;
  detailStatus.value = task.status;
  detailProgress.value = task.progress;
  const teamId = session.activeId;
  const base = `/teams/${teamId}/tasks/${task.id}`;
  try {
    const [full, commentList, depList, collabList, historyList] = await Promise.all([
      api<Task>('GET', base),
      api<any[]>('GET', `${base}/comments`),
      api<string[]>('GET', `${base}/dependencies`),
      api<Member[]>('GET', `${base}/collaborators`),
      api<any[]>('GET', `${base}/history`),
    ]);
    if (selected.value?.id !== task.id) return;
    selected.value = full;
    detailStatus.value = full.status;
    detailProgress.value = full.progress;
    comments.value = commentList;
    dependencies.value = depList || [];
    collaborators.value = collabList || [];
    history.value = historyList || [];
  } catch (e) {
    notify.error(e);
  }
}

async function updateStatus(): Promise<void> {
  if (!selected.value) return;
  saving.value = true;
  try {
    await api('PATCH', `/teams/${session.activeId}/tasks/${selected.value.id}`, {
      version: selected.value.version,
      status: detailStatus.value,
      progress: detailProgress.value,
    });
    notify.success('状态已更新');
    await openDetail(selected.value);
    await load();
  } catch (e) {
    notify.error(e);
  } finally {
    saving.value = false;
  }
}

async function addComment(): Promise<void> {
  if (!selected.value || !commentText.value.trim()) return;
  try {
    await api('POST', `/teams/${session.activeId}/tasks/${selected.value.id}/comments`, { body: commentText.value.trim() });
    commentText.value = '';
    await openDetail(selected.value);
  } catch (e) {
    notify.error(e);
  }
}

function openCommentEdit(comment: any): void {
  commentTargetId.value = comment.id;
  commentDraft.value = comment.body;
  commentOpen.value = true;
}

async function saveComment(): Promise<void> {
  if (!selected.value || !commentDraft.value.trim()) return;
  saving.value = true;
  try {
    await api('PATCH', `/teams/${session.activeId}/tasks/${selected.value.id}/comments/${commentTargetId.value}`, {
      body: commentDraft.value.trim(),
    });
    commentOpen.value = false;
    await openDetail(selected.value);
  } catch (e) {
    notify.error(e);
  } finally {
    saving.value = false;
  }
}

async function removeComment(id: string): Promise<void> {
  if (!selected.value) return;
  try {
    await api('DELETE', `/teams/${session.activeId}/tasks/${selected.value.id}/comments/${id}`);
    await openDetail(selected.value);
  } catch (e) {
    notify.error(e);
  }
}

async function addDependency(): Promise<void> {
  if (!selected.value || !dependencyPick.value) return;
  try {
    await api('POST', `/teams/${session.activeId}/tasks/${selected.value.id}/dependencies`, {
      dependsOnTaskId: dependencyPick.value,
    });
    dependencyPick.value = undefined;
    await openDetail(selected.value);
  } catch (e) {
    notify.error(e);
  }
}

async function removeDependency(id: string): Promise<void> {
  if (!selected.value) return;
  try {
    await api('DELETE', `/teams/${session.activeId}/tasks/${selected.value.id}/dependencies`, { dependsOnTaskId: id });
    await openDetail(selected.value);
  } catch (e) {
    notify.error(e);
  }
}

async function addCollaborator(): Promise<void> {
  if (!selected.value || !collaboratorPick.value) return;
  try {
    await api('POST', `/teams/${session.activeId}/tasks/${selected.value.id}/collaborators`, { userId: collaboratorPick.value });
    collaboratorPick.value = undefined;
    await openDetail(selected.value);
  } catch (e) {
    notify.error(e);
  }
}

async function removeCollaborator(id: string): Promise<void> {
  if (!selected.value) return;
  try {
    await api('DELETE', `/teams/${session.activeId}/tasks/${selected.value.id}/collaborators/${id}`);
    await openDetail(selected.value);
  } catch (e) {
    notify.error(e);
  }
}

/* ---------- 提案 ---------- */
const proposalOpen = ref(false);
const proposal = reactive<Record<string, any>>({
  taskId: '',
  title: '',
  description: '',
  priority: 'medium',
  status: 'todo',
  progress: 0,
  assigneeId: undefined,
  startDate: null,
  dueDate: null,
  milestone: false,
  reason: '',
  impactSummary: '',
});

function openProposal(task: any): void {
  Object.assign(proposal, {
    taskId: task.id,
    title: task.title,
    description: task.description,
    priority: task.priority,
    status: task.status,
    progress: task.progress,
    assigneeId: task.assigneeId || undefined,
    startDate: task.startDate || null,
    dueDate: task.dueDate || null,
    milestone: task.milestone,
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
    detailOpen.value = false;
    reload();
  },
);

watch(
  () => route.query.q,
  (value) => {
    const q = String(value || '');
    if (q !== filters.q) {
      filters.q = q;
      page.value = 1;
      load();
    }
  },
);

watch(
  () => route.query.projectId,
  (value) => {
    const id = String(value || '') || undefined;
    if (id !== filters.projectId) {
      filters.projectId = id;
      page.value = 1;
      load();
    }
  },
);

watch(
  () => route.query.taskId,
  async (value) => {
    const taskId = String(value || '');
    if (!taskId || !session.activeId) return;
    try {
      const task = items.value.find((item) => item.id === taskId) || (await api<Task>('GET', `/teams/${session.activeId}/tasks/${taskId}`));
      openDetail(task);
    } catch (e) {
      notify.error(e);
    }
  },
);

onMounted(async () => {
  filters.q = String(route.query.q || '');
  filters.projectId = String(route.query.projectId || '') || undefined;
  await reload();
});
</script>

<style scoped>
.pad {
  padding: 14px 16px;
}
.cell-title {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.link-btn {
  border: 0;
  background: transparent;
  padding: 0;
  font: inherit;
  font-weight: 600;
  color: var(--tm-text);
  cursor: pointer;
  text-align: left;
}
.link-btn:hover {
  color: var(--tm-primary);
}
.ms-tag {
  font-size: 10.5px;
  line-height: 18px;
}
.cell-sub {
  margin-top: 2px;
  color: var(--tm-muted);
  font-size: 11.5px;
}
.who {
  display: inline-flex;
  align-items: center;
  gap: 6px;
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
  color: var(--tm-muted);
  font-size: 11.5px;
  text-align: right;
  font-variant-numeric: tabular-nums;
}
.overdue {
  color: #d92d20;
}
.detail-head {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  padding: 18px 0 8px;
}
.detail-head h2 {
  margin: 0;
  font-size: 20px;
  letter-spacing: -0.02em;
}
.detail-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  margin-top: 8px;
  font-size: 12px;
}
.description {
  margin: 4px 0 14px;
  color: var(--tm-text-2);
  white-space: pre-wrap;
  font-size: 13px;
}
.desc {
  margin-bottom: 16px;
}
.status-row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  padding: 12px;
  border-radius: 8px;
  background: var(--tm-panel-2);
}
.detail-tabs {
  margin-top: 8px;
}
.comment-list {
  display: flex;
  flex-direction: column;
  gap: 14px;
  padding: 4px 0 12px;
}
.comment {
  display: flex;
  gap: 10px;
}
.comment-body {
  flex: 1;
  min-width: 0;
}
.comment-head {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
}
.comment-head .muted {
  font-size: 11px;
}
.comment-actions {
  margin-left: auto;
}
.comment-text {
  margin-top: 3px;
  color: var(--tm-text-2);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.comment-input {
  display: flex;
  gap: 8px;
  align-items: flex-end;
  padding-top: 8px;
  border-top: 1px solid var(--tm-border);
}
.dep-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  padding: 4px 0 14px;
  min-height: 34px;
}
.dep-add {
  display: flex;
  gap: 8px;
}
.history {
  padding-top: 8px;
}
.empty-block.small {
  padding: 20px;
  font-size: 12px;
}
</style>
