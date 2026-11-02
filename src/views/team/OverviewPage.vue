<template>
  <div class="page">
    <PageHeader :title="teamName" :subtitle="subtitle">
      <template #actions>
        <a-button :loading="loading" @click="load"><reload-outlined /> 刷新</a-button>
        <a-button type="primary" @click="router.push('/teammind/tasks')">进入任务中心</a-button>
      </template>
    </PageHeader>

    <a-alert v-if="error" type="error" show-icon :message="error" />

    <div v-if="!session.activeTeam" class="panel empty-block">
      <a-empty description="还没有组织">
        <template #description>
          <p style="margin: 0 0 8px">当前账号还没有加入任何组织。</p>
          <p class="muted" style="margin: 0">点击顶栏「+」创建组织，或通过邀请链接加入团队。</p>
        </template>
      </a-empty>
    </div>

    <a-alert
      v-else-if="session.teamStatus === 'pending_review'"
      type="warning"
      show-icon
      message="组织等待审核"
      description="平台审核通过后即可使用工作台。你可以先浏览页面，数据将在通过后可用。"
    />

    <template v-else>
      <div class="stats">
        <div v-for="item in stats" :key="item.label" class="stat panel">
          <div class="stat-top">
            <span class="stat-label">{{ item.label }}</span>
            <span class="stat-icon" :style="{ color: item.color, background: `${item.color}14` }">
              <component :is="item.icon" />
            </span>
          </div>
          <div class="stat-value">{{ item.value }}</div>
          <div class="stat-hint">{{ item.hint }}</div>
        </div>
      </div>

      <div class="split-grid">
        <section class="panel">
          <div class="panel-head">
            <h3>近期任务</h3>
            <router-link to="/teammind/tasks">查看全部 →</router-link>
          </div>
          <a-table
            :data-source="tasks"
            :columns="taskColumns"
            :loading="loading"
            :pagination="false"
            row-key="id"
            size="middle"
            :scroll="{ x: 620 }"
            :custom-row="(record: Task) => ({ onClick: () => router.push({ path: '/teammind/tasks', query: { taskId: record.id } }) })"
          >
            <template #bodyCell="{ column, record }">
              <template v-if="column.key === 'title'">
                <div class="task-title">{{ record.title }}</div>
                <div class="muted" style="font-size: 11.5px">{{ record.projectName || projectName(record.projectId) }}</div>
              </template>
              <template v-else-if="column.key === 'status'">
                <StatusTag kind="task" :value="record.status" dot />
              </template>
              <template v-else-if="column.key === 'assignee'">
                <span class="who"><UserAvatar :name="record.assigneeName" :size="20" />{{ record.assigneeName || '未分配' }}</span>
              </template>
              <template v-else-if="column.key === 'progress'">
                <a-progress :percent="record.progress" size="small" :show-info="false" style="width: 90px" />
              </template>
              <template v-else-if="column.key === 'dueDate'">
                <span :class="{ danger: isOverdue(record.dueDate, record.status) }">{{ fmtDate(record.dueDate) }}</span>
              </template>
            </template>
          </a-table>
          <div v-if="!tasks.length && !loading" class="empty-block">还没有任务，去任务中心创建一个。</div>
        </section>

        <section class="panel">
          <div class="panel-head">
            <h3>{{ session.isCompany ? '部门概况' : '小组概况' }}</h3>
            <router-link v-if="session.canManage" :to="session.isCompany ? '/teammind/departments' : '/teammind/departments'">
              管理 →
            </router-link>
          </div>
          <a-list :data-source="departments" size="small" class="dept-list">
            <template #renderItem="{ item }">
              <a-list-item>
                <a-list-item-meta>
                  <template #title>
                    <span class="dept-name">{{ item.name }}</span>
                  </template>
                  <template #description>
                    <span class="muted">{{ item.memberCount }} 位成员 · {{ item.projectCount }} 个项目</span>
                  </template>
                </a-list-item-meta>
                <template #actions>
                  <a-button type="link" size="small" @click="router.push({ path: '/teammind/tasks', query: { departmentId: item.id } })">
                    任务
                  </a-button>
                </template>
              </a-list-item>
            </template>
          </a-list>
          <div v-if="!departments.length" class="empty-block small">
            {{ session.isCompany ? '还没有部门' : '还没有小组' }}
          </div>
        </section>
      </div>

      <section class="panel quick">
        <div class="quick-item" @click="router.push('/teammind/tasks')">
          <span class="qi-icon"><check-square-outlined /></span>
          <div><b>新建任务</b><span>拆解本周工作，指派负责人</span></div>
        </div>
        <div class="quick-item" @click="router.push('/teammind/gantt')">
          <span class="qi-icon"><calendar-outlined /></span>
          <div><b>排期与甘特</b><span>看依赖、里程碑和交付节奏</span></div>
        </div>
        <div class="quick-item" @click="router.push('/teammind/assistant')">
          <span class="qi-icon"><robot-outlined /></span>
          <div><b>问 AI 助手</b><span>基于团队上下文回答问题</span></div>
        </div>
        <div class="quick-item" @click="router.push('/teammind/approvals')">
          <span class="qi-icon"><audit-outlined /></span>
          <div><b>待审批</b><span>处理计划、项目与成员变更</span></div>
        </div>
      </section>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useRouter } from 'vue-router';
import {
  AuditOutlined,
  CalendarOutlined,
  CheckCircleOutlined,
  CheckSquareOutlined,
  FolderOutlined,
  ReloadOutlined,
  RobotOutlined,
  StopOutlined,
  TeamOutlined,
  UnorderedListOutlined,
} from '@ant-design/icons-vue';
import { api, cleanParams } from '@/api/client';
import type { Department, PageResult, Project, Task } from '@/api/types';
import { useSession } from '@/store/session';
import { fmtDate } from '@/utils/format';
import PageHeader from '@/components/PageHeader.vue';
import StatusTag from '@/components/StatusTag.vue';
import UserAvatar from '@/components/UserAvatar.vue';

interface Summary {
  projects: number;
  tasks: number;
  blocked: number;
  pending: number;
  members: number;
  departments: number;
  role: string;
}

const router = useRouter();
const session = useSession();
const loading = ref(false);
const error = ref('');
const summary = ref<Summary>({ projects: 0, tasks: 0, blocked: 0, pending: 0, members: 0, departments: 0, role: 'member' });
const tasks = ref<Task[]>([]);
const departments = ref<Department[]>([]);
const projects = ref<Project[]>([]);

const teamName = computed(() => session.activeTeam?.name || '欢迎使用 TeamMind');
const subtitle = computed(() => {
  const team = session.activeTeam;
  if (!team) return '创建或加入一个组织，开始协作';
  return `${team.kind === 'company' ? '公司版' : '团队版'} · ${team.members} 位成员 · ${session.user?.name || ''}`;
});

const stats = computed(() => [
  { label: '项目', value: summary.value.projects, hint: '进行中的项目与计划', color: '#2f54eb', icon: FolderOutlined },
  { label: '任务', value: summary.value.tasks, hint: `其中 ${summary.value.blocked} 个阻塞`, color: '#0e9384', icon: UnorderedListOutlined },
  { label: '待审批', value: summary.value.pending, hint: '计划 / 项目 / 成员提案', color: '#b54708', icon: AuditOutlined },
  { label: '成员', value: summary.value.members, hint: `${summary.value.departments} 个${session.isCompany ? '部门' : '小组'}`, color: '#7a5af8', icon: TeamOutlined },
]);

const taskColumns = [
  { title: '任务', key: 'title', dataIndex: 'title', width: 240 },
  { title: '状态', key: 'status', width: 96 },
  { title: '负责人', key: 'assignee', width: 110 },
  { title: '进度', key: 'progress', width: 110 },
  { title: '截止', key: 'dueDate', dataIndex: 'dueDate', width: 96 },
];

function isOverdue(due: string | null, status: string): boolean {
  if (!due || status === 'done' || status === 'cancelled') return false;
  return new Date(due).getTime() < Date.now() - 86400000;
}

function projectName(id: string): string {
  return projects.value.find((project) => project.id === id)?.name || '未归属项目';
}

async function load(): Promise<void> {
  if (!session.activeId || session.teamStatus !== 'active') return;
  loading.value = true;
  try {
    const teamId = session.activeId;
    const [s, taskPage, deptPage, projectPage] = await Promise.all([
      api<Summary>('GET', `/teams/${teamId}/summary`),
      api<PageResult<Task>>('GET', `/teams/${teamId}/tasks`, undefined, cleanParams({ pageSize: 5, sortBy: 'createdAt', sortDir: 'DESC' })),
      api<{ items: Department[] }>('GET', `/teams/${teamId}/departments`),
      api<PageResult<Project>>('GET', `/teams/${teamId}/projects`, undefined, { pageSize: 100 }),
    ]);
    if (teamId !== session.activeId) return;
    summary.value = s;
    tasks.value = taskPage.items;
    departments.value = deptPage.items;
    projects.value = projectPage.items;
    error.value = '';
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}

watch(() => session.activeId, load);
onMounted(load);
</script>

<style scoped>
.stats {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}
.stat {
  padding: 16px 18px;
}
.stat-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.stat-label {
  color: var(--tm-muted);
  font-size: 12px;
}
.stat-icon {
  display: grid;
  place-items: center;
  width: 28px;
  height: 28px;
  border-radius: 7px;
  font-size: 14px;
}
.stat-value {
  margin-top: 8px;
  font-size: 28px;
  font-weight: 650;
  letter-spacing: -0.03em;
  line-height: 1.1;
  font-variant-numeric: tabular-nums;
}
.stat-hint {
  margin-top: 4px;
  color: var(--tm-muted);
  font-size: 11.5px;
}
.task-title {
  font-weight: 600;
  color: var(--tm-text);
}
.who {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.danger {
  color: #d92d20;
}
.dept-list {
  padding: 4px 8px 8px;
}
.dept-name {
  font-weight: 600;
}
.empty-block.small {
  padding: 24px;
}
.quick {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  padding: 6px;
}
.quick-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px;
  border-radius: 8px;
  cursor: pointer;
}
.quick-item:hover {
  background: var(--tm-panel-2);
}
.qi-icon {
  display: grid;
  place-items: center;
  width: 34px;
  height: 34px;
  border-radius: 9px;
  background: var(--tm-primary-soft);
  color: var(--tm-primary);
  font-size: 15px;
}
.quick-item b {
  display: block;
  font-size: 12.5px;
}
.quick-item span {
  color: var(--tm-muted);
  font-size: 11.5px;
}
:deep(.ant-table-tbody > tr) {
  cursor: pointer;
}

@media (max-width: 1100px) {
  .stats {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .quick {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
@media (max-width: 640px) {
  .quick {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
