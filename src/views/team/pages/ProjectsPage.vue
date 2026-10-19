<template>
  <section class="tm-page">
    <header class="tm-header"
      ><div><h1>项目管理</h1><p>项目数据保存在当前团队</p></div
      ><a-button v-if="workspace.isAdmin" type="primary" @click="openNew"
        >创建项目</a-button
      ></header
    >
    <div v-if="error" class="tm-error" role="alert"
      >{{ error }} <a-button size="small" @click="refresh">重试</a-button></div
    >
    <div class="tm-toolbar"
      ><a-input
        v-model="query"
        placeholder="搜索项目"
        allow-clear
        @press-enter="search"
      /><a-select v-model="status" @change="search"
        ><a-option value="">全部状态</a-option
        ><a-option value="planning">规划中</a-option
        ><a-option value="active">进行中</a-option
        ><a-option value="paused">暂停</a-option
        ><a-option value="completed">完成</a-option></a-select
      ><a-button @click="search">搜索</a-button></div
    >
    <div v-if="items.length === 0 && !busy" class="tm-empty">暂无项目</div>
    <div class="tm-card-grid"
      ><article v-for="item in items" :key="item.id" class="tm-panel"
        ><h2>{{ item.name }}</h2
        ><p>{{ item.description || '暂无描述' }}</p
        ><p
          >状态：{{ item.status }} · 进度：{{ item.progress }}% · 截止：{{
            item.dueDate || '未设置'
          }}</p
        ><div class="tm-actions"
          ><a-button
            size="small"
            @click="router.push(`/teammind/tasks?projectId=${item.id}`)"
            >查看任务</a-button
          ><a-button
            size="small"
            @click="router.push(`/teammind/gantt?projectId=${item.id}`)"
            >甘特图</a-button
          ><a-button v-if="workspace.isAdmin" size="small" @click="edit(item)"
            >编辑</a-button
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
      v-model:visible="visible"
      :title="draft.id ? '编辑项目' : '创建项目'"
      :ok-loading="saving"
      @ok="save"
      @cancel="visible = false"
      ><div class="tm-form"
        ><label
          >名称<input
            v-model.trim="draft.name"
            maxlength="200"
            required /></label
        ><label
          >描述<textarea
            v-model="draft.description"
            maxlength="10000"
          ></textarea></label
        ><label
          >状态<select v-model="draft.status"
            ><option value="planning">规划中</option
            ><option value="active">进行中</option
            ><option value="paused">暂停</option
            ><option value="completed">完成</option
            ><option value="archived">归档</option></select
          ></label
        ><label>开始日期<input v-model="draft.startDate" type="date" /></label
          ><label
            >截止日期<input
              v-model="draft.dueDate"
              type="date" /></label
        ><p v-if="formError" class="tm-error">{{ formError }}</p></div
      ></a-modal
    >
  </section>
</template>
<script setup lang="ts">
  import { ref, watch } from 'vue';
  import { useRouter } from 'vue-router';
  import { Modal, Message } from '@arco-design/web-vue';
  import { api, Page, Project } from '@/api/workspace';
  import { useWorkspaceStore } from '@/stores/workspace';
  import { useTeamRequest } from '@/utils/teamRequest';
  const workspace = useWorkspaceStore();
  const teamRequest = useTeamRequest(() => workspace.activeTeam.id);
  let displayedTeamId = '';
  const router = useRouter();
  const items = ref<Project[]>([]);
  const query = ref('');
  const status = ref('');
  const page = ref(1);
  const size = 12;
  const total = ref(0);
  const busy = ref(false);
  const saving = ref(false);
  const error = ref('');
  const formError = ref('');
  const visible = ref(false);
  const draft = ref({
    id: '',
    name: '',
    description: '',
    status: 'planning',
    startDate: '',
    dueDate: '',
  });
  async function refresh() {
    const request = teamRequest.begin();
    const id = request.teamId;
    if (!id) {
      items.value = [];
      total.value = 0;
      busy.value = false;
      return;
    }
    busy.value = true;
    error.value = '';
    try {
      const data = await api<Page<Project>>(
        'GET',
        `/teams/${id}/projects`,
        undefined,
        {
          q: query.value,
          status: status.value,
          page: page.value,
          pageSize: size,
        }
      );
      if (!teamRequest.isCurrent(request)) return;
      items.value = data.items;
      total.value = data.total;
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
  function changePage(value: number) {
    page.value = value;
    refresh();
  }
  function openNew() {
    draft.value = {
      id: '',
      name: '',
      description: '',
      status: 'planning',
      startDate: '',
      dueDate: '',
    };
    formError.value = '';
    visible.value = true;
  }
  function edit(item: Project) {
    draft.value = {
      id: item.id,
      name: item.name,
      description: item.description,
      status: item.status,
      startDate: item.startDate || '',
      dueDate: item.dueDate || '',
    };
    formError.value = '';
    visible.value = true;
  }
  async function save() {
    if (saving.value) return;
    if (!draft.value.name) {
      formError.value = '请输入项目名称';
      visible.value = true;
      return;
    }
    if (
      draft.value.startDate &&
      draft.value.dueDate &&
      draft.value.dueDate < draft.value.startDate
    ) {
      formError.value = '截止日期不能早于开始日期';
      visible.value = true;
      return;
    }
    saving.value = true;
    try {
      const id = workspace.activeTeam.id;
      if (draft.value.id)
        await api('PATCH', `/teams/${id}/projects/${draft.value.id}`, {
          name: draft.value.name,
          description: draft.value.description,
          status: draft.value.status,
          startDate: draft.value.startDate,
          dueDate: draft.value.dueDate,
        });
      else
        await api('POST', `/teams/${id}/projects`, {
          name: draft.value.name,
          description: draft.value.description,
          status: draft.value.status,
          startDate: draft.value.startDate,
          dueDate: draft.value.dueDate,
        });
      visible.value = false;
      Message.success('项目已保存');
      await refresh();
    } catch (e) {
      formError.value = (e as Error).message;
      visible.value = true;
    } finally {
      saving.value = false;
    }
  }
  function remove(item: Project) {
    Modal.confirm({
      title: '删除项目？',
      content: '只能删除没有任务的项目。此操作会记录审计。',
      onOk: async () => {
        try {
          await api(
            'DELETE',
            `/teams/${workspace.activeTeam.id}/projects/${item.id}`
          );
          Message.success('项目已删除');
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
        total.value = 0;
        error.value = '';
        visible.value = false;
      }
      page.value = 1;
      refresh();
    },
    { immediate: true }
  );
</script>
