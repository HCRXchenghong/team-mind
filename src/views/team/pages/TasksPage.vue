<template>
  <section class="tm-page">
    <header class="tm-header"
      ><div><h1>任务中心</h1><p>可更新自己负责的任务；排期调整需要审批</p></div
      ><a-button type="primary" @click="openNew">创建任务</a-button></header
    >
    <div v-if="error" class="tm-error" role="alert"
      >{{ error }} <a-button size="small" @click="refresh">重试</a-button></div
    >
    <div class="tm-toolbar"
      ><a-input
        v-model="query"
        placeholder="搜索任务"
        allow-clear
        @press-enter="search"
      /><a-select v-model="status" @change="search"
        ><a-option value="">全部状态</a-option
        ><a-option v-for="x in statusOptions" :key="x" :value="x">{{
          x
        }}</a-option></a-select
      ><a-select v-model="projectId" @change="search"
        ><a-option value="">全部项目</a-option
        ><a-option v-for="p in projects" :key="p.id" :value="p.id">{{
          p.name
        }}</a-option></a-select
      ><a-select v-model="assigneeId" @change="search"
        ><a-option value="">全部负责人</a-option
        ><a-option value="unassigned">未分配</a-option
        ><a-option v-for="m in members" :key="m.id" :value="m.id">{{ m.name }}</a-option></a-select
      ><a-select v-model="sortBy" @change="search"
        ><a-option value="createdAt">创建时间</a-option
        ><a-option value="dueDate">截止日期</a-option
        ><a-option value="title">标题</a-option
        ><a-option value="priority">优先级</a-option
        ><a-option value="assignee">负责人</a-option></a-select
      ><a-select v-model="sortDir" @change="search"
        ><a-option value="desc">降序</a-option
        ><a-option value="asc">升序</a-option></a-select
      ><a-button @click="search">搜索</a-button></div
    >
    <div v-if="items.length === 0 && !busy" class="tm-empty"
      >暂无任务。先创建项目，再添加任务。</div
    >
    <div class="tm-card-grid"
      ><article v-for="item in items" :key="item.id" class="tm-panel"
        ><h2><span v-if="item.milestone">◆ </span>{{ item.title }}</h2
        ><p>{{ item.description || '暂无描述' }}</p
        ><div class="tm-meta"
          >负责人：{{ item.assigneeName || '未分配' }} · {{ item.status }} ·
          {{ item.priority }} · {{ item.progress }}%</div
        ><div class="tm-meta"
          >{{ item.startDate || '未排期' }} →
          {{ item.dueDate || '未排期' }}</div
        ><div class="tm-actions"
          ><a-button size="small" @click="openDetail(item)">详情</a-button
          ><a-button v-if="canEditTask(item)" size="small" @click="openPlan(item)">申请任务变更</a-button
          ><a-button
            v-if="workspace.isAdmin"
            size="small"
            status="danger"
            @click="remove(item)"
            >删除</a-button
          ></div
        ></article
      ></div
    >
    <a-pagination
      v-if="total > size"
      :total="total"
      :page-size="size"
      :current="page"
      @change="changePage"
    />
    <a-modal
      v-model:visible="createVisible"
      title="创建任务"
      :ok-loading="saving"
      @ok="create"
      ><div class="tm-form"
        ><label
          >项目<select v-model="draft.projectId"
            ><option value="">请选择</option
            ><option v-for="p in projects" :key="p.id" :value="p.id">{{
              p.name
            }}</option></select
          ></label
        ><label>标题<input v-model.trim="draft.title" maxlength="240" /></label
        ><label
          >描述<textarea
            v-model="draft.description"
            maxlength="20000"
          ></textarea></label
        ><label
          >负责人<select v-model="draft.assigneeId"
            ><option v-for="m in members.filter((x) => x.status === 'active')" :key="m.id" :value="m.id">{{
              m.name
            }}</option></select
          ></label
        ><label
          >优先级<select v-model="draft.priority"
            ><option value="low">低</option
            ><option value="medium">中</option
            ><option value="high">高</option
            ><option value="urgent">紧急</option></select
          ></label
        ><label>开始日期<input v-model="draft.startDate" type="date" /></label
        ><label>截止日期<input v-model="draft.dueDate" type="date" /></label
        ><label
          ><input v-model="draft.milestone" type="checkbox" /> 里程碑</label
        ><p v-if="formError" class="tm-error">{{ formError }}</p></div
      ></a-modal
    >
    <a-modal
      v-model:visible="detailVisible"
      title="任务详情"
      :footer="false"
      width="720px"
      ><div v-if="selected" class="tm-detail"
        ><h2>{{ selected.title }}</h2
        ><p>{{ selected.description }}</p
        ><p
          >版本 {{ selected.version }} · 负责人
          {{ selected.assigneeName || '未分配' }}</p
        ><div class="tm-form"
          ><label
            >状态<select v-model="editStatus"
              ><option v-for="x in statusOptions" :key="x" :value="x">{{
                x
              }}</option></select
            ></label
          ><label
            >进度<input
              v-model.number="editProgress"
              type="number"
              min="0"
              max="100" /></label
          ><a-button v-if="canContributeTask(selected)" type="primary" :loading="saving" @click="update"
            >保存状态</a-button
          ></div
        ><h3>任务依赖</h3
        ><div class="tm-toolbar"
          ><select v-model="dependsOn"
            ><option value="">选择任务</option
            ><option
              v-for="t in allTasks.filter((t) => t.id !== selected?.id)"
              :key="t.id"
              :value="t.id"
              >{{ t.title }}</option
            ></select
          ><a-button v-if="canContributeTask(selected)" @click="addDependency">添加依赖</a-button></div
        ><div v-for="dependencyId in dependencies" :key="dependencyId" class="tm-row"
          ><span>{{ allTasks.find((t) => t.id === dependencyId)?.title || dependencyId }}</span
          ><a-button v-if="canContributeTask(selected)" size="small" status="danger" @click="removeDependency(dependencyId)">移除依赖</a-button
          ></div
        ><h3>协作者</h3
        ><div v-if="canEditTask(selected)" class="tm-toolbar"
          ><select v-model="collaboratorId"
            ><option value="">选择有效团队成员</option
            ><option
              v-for="m in members.filter((x) => x.status === 'active' && !collaborators.some((c) => c.id === x.id))"
              :key="m.id"
              :value="m.id"
              >{{ m.name }}</option
            ></select
          ><a-button :disabled="!collaboratorId" :loading="saving" @click="addCollaborator">添加协作者</a-button></div
        ><div v-if="!collaborators.length" class="tm-meta">尚未添加协作者</div
        ><div v-for="c in collaborators" :key="c.id" class="tm-row"
          ><span>{{ c.name }} · {{ c.status === 'active' ? '有效' : '已停用' }}</span
          ><a-button v-if="canEditTask(selected)" size="small" status="danger" @click="removeCollaborator(c.id)">移除</a-button
          ></div
        ><h3>评论</h3
        ><div v-for="c in comments" :key="c.id" class="tm-row tm-comment-row"
          ><p><b>{{ c.authorName }}：</b>{{ c.body }}</p
          ><div v-if="canManageComment(c)" class="tm-actions"
            ><a-button size="small" @click="editComment(c)">编辑</a-button
            ><a-button size="small" status="danger" @click="removeComment(c)">删除</a-button></div
          ></div
        ><div class="tm-toolbar"
          ><a-input
            v-model="comment"
            placeholder="写评论"
            @press-enter="addComment"
          /><a-button @click="addComment">发送</a-button></div
        ><h3>计划历史</h3
        ><p v-for="h in history" :key="h.version"
          >版本 {{ h.version }} · {{ h.source }} ·
          {{ formatDateTime(h.createdAt, user.timezone) }}</p
        ><p v-if="formError" class="tm-error">{{ formError }}</p></div
      ></a-modal
    >
    <a-modal
      v-model:visible="commentEditVisible"
      title="编辑评论"
      :ok-loading="saving"
      @ok="saveComment"
      ><div class="tm-form"
        ><textarea v-model="commentEditBody" maxlength="10000" aria-label="评论内容"></textarea
        ><p v-if="formError" class="tm-error">{{ formError }}</p></div
      ></a-modal
    >
    <a-modal
      v-model:visible="planVisible"
      title="提交任务变更提案"
      :ok-loading="saving"
      @ok="submitPlan"
      ><div class="tm-form"
        ><p>标题、说明、优先级、负责人和排期都将在审批后一起生效；审批前正式任务与甘特图保持不变。</p
        ><label>标题<input v-model.trim="plan.title" maxlength="240" /></label
        ><label>描述<textarea v-model="plan.description" maxlength="20000"></textarea></label
        ><label
          >负责人<select v-model="plan.assigneeId"
            ><option value="">未分配</option
            ><option v-for="m in members.filter((x) => x.status === 'active')" :key="m.id" :value="m.id">{{ m.name }}</option></select
          ></label
        ><label
          >优先级<select v-model="plan.priority"
            ><option value="low">低</option
            ><option value="medium">中</option
            ><option value="high">高</option
            ><option value="urgent">紧急</option></select
          ></label
        ><label>开始日期<input v-model="plan.startDate" type="date" /></label
        ><label>截止日期<input v-model="plan.dueDate" type="date" /></label
        ><label><input v-model="plan.milestone" type="checkbox" /> 里程碑</label
        ><label
          >原因<textarea
            v-model="plan.reason"
            maxlength="5000"
          ></textarea></label
        ><label
          >影响说明<textarea
            v-model="plan.impactSummary"
            maxlength="5000"
          ></textarea></label
        ><p v-if="formError" class="tm-error">{{ formError }}</p></div
      ></a-modal
    >
  </section>
</template>
<script setup lang="ts">
  import { formatDateTime } from '@/utils/time';
  import { ref, watch } from 'vue';
  import { useRoute, useRouter } from 'vue-router';
  import { Message, Modal } from '@arco-design/web-vue';
  import { api, Member, Page, Project, Task } from '@/api/workspace';
  import { useWorkspaceStore } from '@/stores/workspace';
  import { useTeamRequest } from '@/utils/teamRequest';
  import { useUserStore } from '@/store';
  const workspace = useWorkspaceStore();
  const teamRequest = useTeamRequest(() => workspace.activeTeam.id);
  const detailRequest = useTeamRequest(() => workspace.activeTeam.id);
  let displayedTeamId = '';
  const user = useUserStore();
  const route = useRoute();
  const router = useRouter();
  const statusOptions = [
    'todo',
    'doing',
    'blocked',
    'review',
    'done',
    'cancelled',
  ];
  const items = ref<Task[]>([]);
  const allTasks = ref<Task[]>([]);
  const projects = ref<Project[]>([]);
  const members = ref<Member[]>([]);
  const query = ref(String(route.query.q || ''));
  const status = ref(String(route.query.status || ''));
  const projectId = ref(String(route.query.projectId || ''));
  const assigneeId = ref('');
  const sortBy = ref('createdAt');
  const sortDir = ref('desc');
  const page = ref(1);
  const size = 12;
  const total = ref(0);
  const error = ref('');
  const formError = ref('');
  const busy = ref(false);
  const saving = ref(false);
  const createVisible = ref(false);
  const detailVisible = ref(false);
  const commentEditVisible = ref(false);
  const planVisible = ref(false);
  const selected = ref<Task | null>(null);
  const editStatus = ref('todo');
  const editProgress = ref(0);
  const comments = ref<{ id: string; authorId: string; authorName: string; body: string }[]>([]);
  const dependencies = ref<string[]>([]);
  const collaborators = ref<{ id: string; name: string; status: string }[]>([]);
  const history = ref<{ version: number; source: string; createdAt: string }[]>(
    []
  );
  const comment = ref('');
  const commentEditId = ref('');
  const commentEditBody = ref('');
  const dependsOn = ref('');
  const collaboratorId = ref('');
  const draft = ref({
    projectId: '',
    title: '',
    description: '',
    assigneeId: '',
    priority: 'medium',
    startDate: '',
    dueDate: '',
    milestone: false,
  });
  const plan = ref({
    taskId: '',
    title: '',
    description: '',
    priority: 'medium',
    assigneeId: '',
    startDate: '',
    dueDate: '',
    milestone: false,
    reason: '',
    impactSummary: '',
  });
  async function refresh() {
    const request = teamRequest.begin();
    const id = request.teamId;
    if (!id) {
      items.value = [];
      allTasks.value = [];
      projects.value = [];
      members.value = [];
      total.value = 0;
      busy.value = false;
      return;
    }
    busy.value = true;
    error.value = '';
    try {
      const [taskData, projectData, memberData, allData] = await Promise.all([
        api<Page<Task>>('GET', `/teams/${id}/tasks`, undefined, {
          q: query.value,
          status: status.value,
          projectId: projectId.value,
          assigneeId: assigneeId.value,
          sortBy: sortBy.value,
          sortDir: sortDir.value,
          page: page.value,
          pageSize: size,
        }),
        api<Page<Project>>('GET', `/teams/${id}/projects`, undefined, {
          pageSize: 100,
        }),
        api<Member[]>('GET', `/teams/${id}/members`),
        api<Page<Task>>('GET', `/teams/${id}/tasks`, undefined, {
          pageSize: 100,
        }),
      ]);
      if (!teamRequest.isCurrent(request)) return;
      items.value = taskData.items;
      total.value = taskData.total;
      projects.value = projectData.items;
      members.value = memberData;
      allTasks.value = allData.items;
      if (route.query.task) {
        const target = String(route.query.task);
        const found =
          allTasks.value.find((t) => t.id === target) ||
          (await api<Task>('GET', `/teams/${id}/tasks/${target}`));
        if (!teamRequest.isCurrent(request)) return;
        await openDetail(found, id);
      }
    } catch (e) {
      if (!teamRequest.isCurrent(request)) return;
      error.value = (e as Error).message;
    } finally {
      if (teamRequest.isCurrent(request)) busy.value = false;
    }
  }
  function search() {
    page.value = 1;
    refresh();
  }
  function changePage(n: number) {
    page.value = n;
    refresh();
  }
  function canEditTask(item: Task | null) {
    return !!item && (workspace.isAdmin || item.createdBy === user.id || item.assigneeId === user.id);
  }
  function canContributeTask(item: Task | null) {
    return canEditTask(item) || collaborators.value.some((c) => c.id === user.id && c.status === 'active');
  }
  function openNew() {
    draft.value = {
      projectId: projects.value[0]?.id || '',
      title: '',
      description: '',
      assigneeId: members.value.find((m) => m.email === user.email)?.id || '',
      priority: 'medium',
      startDate: '',
      dueDate: '',
      milestone: false,
    };
    formError.value = '';
    createVisible.value = true;
  }
  async function create() {
    if (saving.value) return;
    if (!draft.value.title || !draft.value.projectId) {
      formError.value = '请选择项目并填写标题';
      createVisible.value = true;
      return;
    }
    saving.value = true;
    try {
      await api('POST', `/teams/${workspace.activeTeam.id}/tasks`, draft.value);
      createVisible.value = false;
      Message.success('任务已创建');
      await refresh();
    } catch (e) {
      formError.value = (e as Error).message;
      createVisible.value = true;
    } finally {
      saving.value = false;
    }
  }
  async function openDetail(t: Task, requestedTeamId = workspace.activeTeam.id) {
    const request = detailRequest.begin();
    if (!requestedTeamId || requestedTeamId !== request.teamId) return;
    selected.value = t;
    collaboratorId.value = '';
    editStatus.value = t.status;
    editProgress.value = t.progress;
    formError.value = '';
    detailVisible.value = true;
    const base = `/teams/${requestedTeamId}/tasks/${t.id}`;
    try {
      const [loadedComments, loadedDependencies, loadedHistory, loadedCollaborators] = await Promise.all([
        api<{ id: string; authorId: string; authorName: string; body: string }[]>(
          'GET',
          `${base}/comments`
        ),
        api<string[]>('GET', `${base}/dependencies`),
        api<{ version: number; source: string; createdAt: string }[]>(
          'GET',
          `${base}/history`
        ),
        api<{ id: string; name: string; status: string }[]>(
          'GET',
          `${base}/collaborators`
        ),
      ]);
      if (!detailRequest.isCurrent(request)) return;
      comments.value = loadedComments;
      dependencies.value = loadedDependencies;
      history.value = loadedHistory;
      collaborators.value = loadedCollaborators;
    } catch (e) {
      if (!detailRequest.isCurrent(request)) return;
      formError.value = (e as Error).message;
    }
  }
  async function update() {
    if (!selected.value || saving.value) return;
    saving.value = true;
    try {
      await api(
        'PATCH',
        `/teams/${workspace.activeTeam.id}/tasks/${selected.value.id}`,
        {
          status: editStatus.value,
          progress: editProgress.value,
          version: selected.value.version,
        }
      );
      Message.success('任务状态已保存');
      detailVisible.value = false;
      await refresh();
    } catch (e) {
      formError.value = (e as Error).message;
    } finally {
      saving.value = false;
    }
  }
  async function addComment() {
    if (!selected.value || !comment.value.trim()) return;
    try {
      await api(
        'POST',
        `/teams/${workspace.activeTeam.id}/tasks/${selected.value.id}/comments`,
        { body: comment.value }
      );
      comment.value = '';
      await openDetail(selected.value);
    } catch (e) {
      formError.value = (e as Error).message;
    }
  }
  function canManageComment(value: { authorId: string }) {
    return workspace.isAdmin || value.authorId === user.id;
  }
  function editComment(value: { id: string; body: string }) {
    commentEditId.value = value.id;
    commentEditBody.value = value.body;
    formError.value = '';
    commentEditVisible.value = true;
  }
  async function saveComment() {
    if (!selected.value || !commentEditId.value || saving.value) return;
    saving.value = true;
    try {
      await api(
        'PATCH',
        `/teams/${workspace.activeTeam.id}/tasks/${selected.value.id}/comments/${commentEditId.value}`,
        { body: commentEditBody.value }
      );
      commentEditVisible.value = false;
      await openDetail(selected.value);
      Message.success('评论已更新');
    } catch (e) {
      formError.value = (e as Error).message;
    } finally {
      saving.value = false;
    }
  }
  function removeComment(value: { id: string }) {
    if (!selected.value) return;
    const taskId = selected.value.id;
    Modal.confirm({
      title: '删除评论？',
      content: '删除后团队成员将无法查看，操作会被审计。',
      onOk: async () => {
        try {
          await api(
            'DELETE',
            `/teams/${workspace.activeTeam.id}/tasks/${taskId}/comments/${value.id}`
          );
          const task = selected.value;
          if (task) await openDetail(task);
          Message.success('评论已删除');
        } catch (e) {
          formError.value = (e as Error).message;
        }
      },
    });
  }
  async function addDependency() {
    if (!selected.value || !dependsOn.value) return;
    try {
      await api(
        'POST',
        `/teams/${workspace.activeTeam.id}/tasks/${selected.value.id}/dependencies`,
        { dependsOnTaskId: dependsOn.value }
      );
      dependsOn.value = '';
      await openDetail(selected.value);
    } catch (e) {
      formError.value = (e as Error).message;
    }
  }
  async function removeDependency(dependencyId: string) {
    if (!selected.value || saving.value) return;
    saving.value = true;
    try {
      await api('DELETE', `/teams/${workspace.activeTeam.id}/tasks/${selected.value.id}/dependencies`, { dependsOnTaskId: dependencyId });
      await openDetail(selected.value);
      Message.success('依赖已移除');
    } catch (e) {
      formError.value = (e as Error).message;
    } finally {
      saving.value = false;
    }
  }
  async function addCollaborator() {
    if (!selected.value || !collaboratorId.value || saving.value) return;
    saving.value = true;
    try {
      await api('POST', `/teams/${workspace.activeTeam.id}/tasks/${selected.value.id}/collaborators`, { userId: collaboratorId.value });
      collaboratorId.value = '';
      await openDetail(selected.value);
      Message.success('协作者已添加');
    } catch (e) {
      formError.value = (e as Error).message;
    } finally {
      saving.value = false;
    }
  }
  async function removeCollaborator(memberId: string) {
    if (!selected.value || saving.value) return;
    saving.value = true;
    try {
      await api('DELETE', `/teams/${workspace.activeTeam.id}/tasks/${selected.value.id}/collaborators/${memberId}`);
      await openDetail(selected.value);
      Message.success('协作者已移除');
    } catch (e) {
      formError.value = (e as Error).message;
    } finally {
      saving.value = false;
    }
  }
  function openPlan(t: Task) {
    plan.value = {
      taskId: t.id,
      title: t.title,
      description: t.description,
      priority: t.priority,
      assigneeId: t.assigneeId && members.value.some((m) => m.id === t.assigneeId && m.status === 'active') ? t.assigneeId : '',
      startDate: t.startDate || '',
      dueDate: t.dueDate || '',
      milestone: t.milestone,
      reason: '',
      impactSummary: '',
    };
    formError.value = '';
    planVisible.value = true;
  }
  async function submitPlan() {
    if (saving.value) return;
    if (!plan.value.title.trim() || plan.value.title.trim().length > 240 || plan.value.reason.trim().length < 5) {
      formError.value = '请填写不超过 240 字的标题和至少 5 字的变更原因';
      planVisible.value = true;
      return;
    }
    saving.value = true;
    try {
      await api(
        'POST',
        `/teams/${workspace.activeTeam.id}/plan-proposals`,
        plan.value
      );
      planVisible.value = false;
      Message.success('提案已提交，正式计划未更改');
      await router.push('/teammind/approvals');
    } catch (e) {
      formError.value = (e as Error).message;
      planVisible.value = true;
    } finally {
      saving.value = false;
    }
  }
  function remove(t: Task) {
    Modal.confirm({
      title: '删除任务？',
      content: '删除后团队成员将无法查看，操作会被审计。',
      onOk: async () => {
        try {
          await api(
            'DELETE',
            `/teams/${workspace.activeTeam.id}/tasks/${t.id}`
          );
          Message.success('任务已删除');
          await refresh();
        } catch (e) {
          Message.error((e as Error).message);
        }
      },
    });
  }
  watch(
    () => workspace.activeTeam.id,
    (id) => {
      if (id !== displayedTeamId) {
        displayedTeamId = id;
        items.value = [];
        allTasks.value = [];
        projects.value = [];
        members.value = [];
        comments.value = [];
        dependencies.value = [];
        collaborators.value = [];
        history.value = [];
        selected.value = null;
        error.value = '';
        detailVisible.value = false;
        createVisible.value = false;
        commentEditVisible.value = false;
        planVisible.value = false;
        detailRequest.invalidate();
      }
      page.value = 1;
      refresh();
    },
    { immediate: true }
  );
  watch(
    () => route.query.new,
    (x) => {
      if (x) openNew();
    }
  );
  watch(
    () => [
      route.query.q,
      route.query.status,
      route.query.projectId,
      route.query.task,
    ],
    () => {
      query.value = String(route.query.q || '');
      status.value = String(route.query.status || '');
      projectId.value = String(route.query.projectId || '');
      page.value = 1;
      refresh();
    }
  );
</script>
