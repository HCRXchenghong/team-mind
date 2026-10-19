<template>
  <section class="tm-page">
    <header class="tm-header">
      <div>
        <h1>项目甘特图</h1>
        <p>显示已批准的正式任务计划。待审批提案不会改变这里的内容。</p>
      </div>
      <router-link to="/teammind/approvals">查看提案</router-link>
    </header>
    <div v-if="error" class="tm-error" role="alert">
      {{ error }} <a-button size="small" @click="refresh">重试</a-button>
    </div>
    <div class="tm-toolbar">
      <a-select v-model="projectId" aria-label="项目筛选" @change="filterChanged">
        <a-option value="">全部项目</a-option>
        <a-option v-for="p in projects" :key="p.id" :value="p.id">{{ p.name }}</a-option>
      </a-select>
      <a-select v-model="status" aria-label="状态筛选" @change="filterChanged">
        <a-option value="">全部状态</a-option>
        <a-option value="todo">待开始</a-option>
        <a-option value="doing">进行中</a-option>
        <a-option value="blocked">阻塞</a-option>
        <a-option value="review">待审查</a-option>
        <a-option value="done">完成</a-option>
        <a-option value="cancelled">已取消</a-option>
      </a-select>
      <a-select v-model="assigneeId" aria-label="负责人筛选" @change="filterChanged">
        <a-option value="">全部负责人</a-option>
        <a-option value="unassigned">未分配</a-option>
        <a-option v-for="m in members" :key="m.id" :value="m.id">{{ m.name }}</a-option>
      </a-select>
      <label class="tm-gantt-date-filter">从<input v-model="dateFrom" type="date" aria-label="计划开始日期" /></label>
      <label class="tm-gantt-date-filter">到<input v-model="dateTo" type="date" aria-label="计划结束日期" /></label>
      <a-button @click="filterChanged">筛选</a-button>
      <label class="tm-gantt-date-filter">定位日期<input v-model="focusDate" type="date" aria-label="定位日期" /></label>
      <a-button @click="goToToday">今天</a-button>
      <a-button @click="locateDate">定位</a-button>
      <a-button @click="refresh">刷新</a-button>
    </div>
    <p v-if="dateError" class="tm-error" role="alert">{{ dateError }}</p>
    <div v-if="!tasks.length && !busy" class="tm-empty">
      当前筛选没有任务。清除日期、负责人或状态条件后重试。
    </div>
    <div v-if="busy" class="tm-empty" role="status">正在读取正式计划…</div>
    <div v-if="tasks.length" ref="timeline" class="tm-gantt" tabindex="0" aria-label="可横向滚动的任务甘特图">
      <div class="tm-gantt-axis">
        <strong>任务</strong>
        <div class="tm-gantt-axis-track">
          <span v-for="tick in ticks" :key="tick">{{ tick }}</span>
        </div>
        <strong>负责人和状态</strong>
      </div>
      <div v-for="t in tasks" :key="t.id" class="tm-gantt-row">
        <router-link :to="`/teammind/tasks?task=${t.id}`" class="tm-gantt-title">
          <span v-if="t.milestone" aria-label="里程碑">◆ </span>{{ t.title }}
        </router-link>
        <div class="tm-gantt-track" :aria-label="`${t.title} 进度 ${t.progress}%`">
          <span v-if="focusPosition !== null" class="tm-gantt-focus-marker" :style="{ left: focusPosition + '%' }"></span>
          <div v-if="t.startDate && t.dueDate" class="tm-gantt-bar" :class="{ 'is-blocked': t.status === 'blocked' }" :style="barStyle(t)">
            {{ t.progress }}%
          </div>
          <span v-else class="tm-gantt-unscheduled">尚未排期</span>
        </div>
        <div class="tm-gantt-meta">
          <span>{{ t.assigneeName || '未分配' }} · {{ statusLabel(t.status) }}</span>
          <span>{{ t.startDate || '—' }} ~ {{ t.dueDate || '—' }}</span>
          <span v-if="t.status === 'blocked'" class="tm-gantt-blocked">当前阻塞</span>
          <span v-if="dependencies[t.id]?.length" class="tm-gantt-dependencies">
            依赖：{{ dependencyTitles(t.id) }}
          </span>
        </div>
      </div>
    </div>
    <a-pagination v-if="total > pageSize" :total="total" :page-size="pageSize" :current="page" @change="changePage" />
  </section>
</template>

<script setup lang="ts">
  import { computed, nextTick, ref, watch } from 'vue';
  import { useRoute } from 'vue-router';
  import { api, Member, Page, Project, Task } from '@/api/workspace';
  import { useWorkspaceStore } from '@/stores/workspace';
  import { useTeamRequest } from '@/utils/teamRequest';
  const store = useWorkspaceStore();
  const teamRequest = useTeamRequest(() => store.activeTeam.id);
  let displayedTeamId = '';
  const route = useRoute();
  const projectId = ref(String(route.query.projectId || ''));
  const status = ref('');
  const assigneeId = ref('');
  const dateFrom = ref('');
  const dateTo = ref('');
  const focusDate = ref(localDate(new Date()));
  const projects = ref<Project[]>([]);
  const members = ref<Member[]>([]);
  const tasks = ref<Task[]>([]);
  const dependencies = ref<Record<string, string[]>>({});
  const error = ref('');
  const dateError = ref('');
  const busy = ref(false);
  const page = ref(1);
  const pageSize = 50;
  const total = ref(0);
  const timeline = ref<HTMLElement | null>(null);

  function localDate(value: Date) {
    return `${value.getFullYear()}-${String(value.getMonth() + 1).padStart(2, '0')}-${String(value.getDate()).padStart(2, '0')}`;
  }
  function statusLabel(value: string) {
    return ({ todo: '待开始', doing: '进行中', blocked: '阻塞', review: '待审查', done: '完成', cancelled: '已取消' } as Record<string, string>)[value] || value;
  }
  function filterChanged() {
    page.value = 1;
    refresh();
  }
  async function refresh() {
    const request = teamRequest.begin();
    const teamId = request.teamId;
    if (!teamId) {
      projects.value = [];
      members.value = [];
      tasks.value = [];
      dependencies.value = {};
      total.value = 0;
      busy.value = false;
      return;
    }
    if (dateFrom.value && dateTo.value && dateTo.value < dateFrom.value) {
      dateError.value = '结束日期不能早于开始日期';
      busy.value = false;
      return;
    }
    dateError.value = '';
    error.value = '';
    busy.value = true;
    try {
      const [projectData, memberData, taskData] = await Promise.all([
        api<Page<Project>>('GET', `/teams/${teamId}/projects`, undefined, { pageSize: 100 }),
        api<Member[]>('GET', `/teams/${teamId}/members`),
        api<Page<Task>>('GET', `/teams/${teamId}/tasks`, undefined, {
          projectId: projectId.value,
          status: status.value,
          assigneeId: assigneeId.value,
          dateFrom: dateFrom.value,
          dateTo: dateTo.value,
          sortBy: 'dueDate',
          sortDir: 'asc',
          page: page.value,
          pageSize,
        }),
      ]);
      if (!teamRequest.isCurrent(request)) return;
      const loaded = await Promise.all(taskData.items.map(async (t) => [t.id, await api<string[]>('GET', `/teams/${teamId}/tasks/${t.id}/dependencies`)] as const));
      if (!teamRequest.isCurrent(request)) return;
      projects.value = projectData.items;
      members.value = memberData;
      tasks.value = taskData.items;
      total.value = taskData.total;
      dependencies.value = Object.fromEntries(loaded);
    } catch (e) {
      if (!teamRequest.isCurrent(request)) return;
      error.value = (e as Error).message;
    } finally {
      if (teamRequest.isCurrent(request)) busy.value = false;
    }
  }
  const dates = computed(() => tasks.value.flatMap((t) => [t.startDate, t.dueDate]).filter(Boolean) as string[]);
  const minTime = computed(() => dates.value.length ? Math.min(...dates.value.map((d) => Date.parse(`${d}T00:00:00`))) : 0);
  const maxTime = computed(() => dates.value.length ? Math.max(...dates.value.map((d) => Date.parse(`${d}T00:00:00`))) : minTime.value + 86400000);
  const span = computed(() => Math.max(86400000, maxTime.value - minTime.value + 86400000));
  const ticks = computed(() => {
    if (!dates.value.length) return [];
    return Array.from({ length: 5 }, (_, i) => localDate(new Date(minTime.value + (span.value - 1) * i / 4)));
  });
  const focusPosition = computed(() => {
    if (!focusDate.value || !dates.value.length) return null;
    const position = ((Date.parse(`${focusDate.value}T00:00:00`) - minTime.value) / span.value) * 100;
    return position >= 0 && position <= 100 ? position : null;
  });
  function barStyle(t: Task) {
    const left = ((Date.parse(`${t.startDate}T00:00:00`) - minTime.value) / span.value) * 100;
    const width = ((Date.parse(`${t.dueDate}T00:00:00`) - Date.parse(`${t.startDate}T00:00:00`) + 86400000) / span.value) * 100;
    return { left: `${left}%`, width: `${Math.max(1, width)}%` };
  }
  function dependencyTitles(taskId: string) {
    return dependencies.value[taskId].map((id) => tasks.value.find((t) => t.id === id)?.title || '列表外任务').join('、');
  }
  async function changePage(value: number) { page.value = value; await refresh(); }
  function goToToday() { focusDate.value = localDate(new Date()); locateDate(); }
  async function locateDate() {
    await nextTick();
    if (!timeline.value || focusPosition.value === null) return;
    const trackWidth = timeline.value.scrollWidth - timeline.value.clientWidth;
    timeline.value.scrollTo({ left: Math.max(0, trackWidth * focusPosition.value / 100 - timeline.value.clientWidth / 2), behavior: 'smooth' });
  }
  watch(() => store.activeTeam.id, (id) => {
    if (id !== displayedTeamId) {
      displayedTeamId = id;
      projects.value = [];
      members.value = [];
      tasks.value = [];
      dependencies.value = {};
      error.value = '';
    }
    page.value = 1;
    refresh();
  }, { immediate: true });
  watch(() => route.query.projectId, (value) => { projectId.value = String(value || ''); filterChanged(); });
</script>
