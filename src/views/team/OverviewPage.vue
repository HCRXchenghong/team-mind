<template>
  <section class="tm-page">
    <header class="tm-header"
      ><div
        ><h1>团队概览</h1
        ><p>{{ workspace.activeTeam.name }} 的实时协作数据</p></div
      ><a-button @click="refresh" :loading="busy">刷新</a-button></header
    >
    <div v-if="error" class="tm-error" role="alert"
      >{{ error }} <a-button size="small" @click="refresh">重试</a-button></div
    >
    <div v-if="!workspace.activeTeam.id" class="tm-empty"
      ><p>你还没有团队。</p
      ><router-link to="/teammind/settings">创建团队</router-link></div
    >
    <div
      v-else-if="workspace.activeTeam.status === 'pending_review'"
      class="tm-warning"
      >团队正在等待平台审核。审核通过后即可使用团队功能。</div
    >
    <template v-else>
      <div class="tm-stats"
        ><article
          ><small>项目</small><strong>{{ stats.projects }}</strong
          ><router-link to="/teammind/projects">查看项目</router-link></article
        ><article
          ><small>任务</small><strong>{{ stats.tasks }}</strong
          ><router-link to="/teammind/tasks">查看任务</router-link></article
        ><article
          ><small>阻塞任务</small><strong>{{ stats.blocked }}</strong
          ><router-link to="/teammind/tasks?status=blocked"
            >处理阻塞</router-link
          ></article
        ><article
          ><small>待审批</small><strong>{{ stats.pending }}</strong
          ><router-link to="/teammind/approvals">查看审批</router-link></article
        ><article
          ><small>成员</small><strong>{{ stats.members }}</strong
          ><router-link to="/teammind/members">查看成员</router-link></article
        ></div
      >
      <section class="tm-panel"
        ><h2>近期任务</h2
        ><div v-if="tasks.length === 0" class="tm-empty"
          >暂无任务，开始创建项目和任务。</div
        ><router-link
          v-for="task in tasks"
          :key="task.id"
          :to="`/teammind/tasks?task=${task.id}`"
          class="tm-row"
          ><span>{{ task.title }}</span
          ><span
            >{{ task.status }} · {{ task.dueDate || '未排期' }}</span
          ></router-link
        ></section
      >
    </template>
  </section>
</template>
<script setup lang="ts">
  import { ref, watch } from 'vue';
  import { useWorkspaceStore } from '@/stores/workspace';
  import { api, Page, Task } from '@/api/workspace';
  import { useTeamRequest } from '@/utils/teamRequest';
  const workspace = useWorkspaceStore();
  const teamRequest = useTeamRequest(() => workspace.activeTeam.id);
  let displayedTeamId = '';
  const stats = ref({
    projects: 0,
    tasks: 0,
    blocked: 0,
    pending: 0,
    members: 0,
  });
  const tasks = ref<Task[]>([]);
  const busy = ref(false);
  const error = ref('');
  async function refresh() {
    const request = teamRequest.begin();
    const id = request.teamId;
    if (!id || workspace.activeTeam.status !== 'active') {
      stats.value = { projects: 0, tasks: 0, blocked: 0, pending: 0, members: 0 };
      tasks.value = [];
      busy.value = false;
      return;
    }
    busy.value = true;
    error.value = '';
    try {
      const [summary, taskPage] = await Promise.all([
        api<typeof stats.value>('GET', `/teams/${id}/summary`),
        api<Page<Task>>('GET', `/teams/${id}/tasks`, undefined, {
          pageSize: 5,
        }),
      ]);
      if (!teamRequest.isCurrent(request)) return;
      stats.value = summary;
      tasks.value = taskPage.items;
    } catch (e) {
      if (!teamRequest.isCurrent(request)) return;
      error.value = (e as Error).message;
    } finally {
      if (teamRequest.isCurrent(request)) busy.value = false;
    }
  }
  watch(() => workspace.activeTeam.id, (id) => {
    if (id !== displayedTeamId) {
      displayedTeamId = id;
      stats.value = { projects: 0, tasks: 0, blocked: 0, pending: 0, members: 0 };
      tasks.value = [];
      error.value = '';
    }
    refresh();
  }, { immediate: true });
</script>
