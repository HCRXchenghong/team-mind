<template
  ><section class="tm-page"
    ><header class="tm-header"
      ><div
        ><h1>记忆与知识</h1
        ><p>个人记忆仅本人可见；团队知识面向当前团队成员</p></div
      ><a-button
        v-if="tab === 'personal' || (store.activeTeam.id && store.isAdmin)"
        type="primary"
        @click="openNew"
        >添加</a-button
      ></header
    ><div v-if="error" class="tm-error" role="alert"
      >{{ error }} <a-button size="small" @click="refresh">重试</a-button></div
    ><a-tabs v-model:active-key="tab"
      ><a-tab-pane key="personal" title="个人记忆" /><a-tab-pane
        key="team"
        title="团队知识" /></a-tabs
    ><div v-if="tab === 'team' && !store.activeTeam.id" class="tm-empty"
      >加入团队后可以查看团队知识。</div
    ><div v-else-if="!visibleItems.length" class="tm-empty">暂无内容</div
    ><article v-for="m in visibleItems" :key="m.id" class="tm-panel"
      ><h2
        >{{ m.category }}
        <small>{{ m.confirmed ? '已确认' : '待确认' }}</small>
        <small v-if="memoryExpired(m)" class="tm-expired">已到期</small>
        <small v-else-if="memoryReviewDue(m)" class="tm-review-due">需要复核</small></h2
      ><p>{{ m.content }}</p
      ><p v-if="m.sourceMessageId" class="tm-meta"
        >来源对话 · {{ m.sourceRole === 'assistant' ? 'AI 回复' : '你的消息' }}<br />{{ m.sourceExcerpt }}</p
      ><a-button v-if="m.sourceConversationId && m.sourceMessageId" size="small" @click="viewSource(m)"
        >查看来源对话</a-button
      ><p v-if="m.reviewAt || m.expiresAt" class="tm-meta"
        >{{ m.reviewAt ? `复核日期：${formatDate(m.reviewAt)}` : '' }}{{ m.reviewAt && m.expiresAt ? ' · ' : '' }}{{ m.expiresAt ? `到期日期：${formatDate(m.expiresAt)}` : '' }}</p
      ><div class="tm-actions"
        ><a-button
          v-if="tab === 'personal' || store.isAdmin"
          size="small"
          @click="edit(m)"
          >编辑/确认</a-button
        ><a-button
          v-if="tab === 'personal' || store.isAdmin"
          size="small"
          status="danger"
          @click="remove(m)"
          >删除</a-button
        ></div
      ></article
    ><a-modal
      v-model:visible="modal"
      :title="draft.id ? '编辑内容' : '添加内容'"
      :ok-loading="saving"
      @ok="save"
      ><div class="tm-form"
        ><label
          >类别<select v-if="tab === 'personal'" v-model="draft.category"
            ><option value="profile">个人资料</option
            ><option value="experience">经验</option
            ><option value="current_work">当前工作</option
            ><option value="operational_knowledge">工作知识</option></select
          ><input v-else v-model.trim="draft.category" maxlength="80" /></label
        ><label
          >内容<textarea
            v-model="draft.content"
            maxlength="10000"
          ></textarea></label
        ><label>复核日期<input v-model="draft.reviewAt" type="date" /></label><label>到期日期<input v-model="draft.expiresAt" type="date" /></label
        ><label v-if="draft.id"
          ><input v-model="draft.confirmed" type="checkbox" /> 确认内容</label
        ><p v-if="formError" class="tm-error" role="alert">{{
          formError
        }}</p></div
      ></a-modal
    ></section
  ></template
>
<script setup lang="ts">
  import { ref, computed, watch } from 'vue';
  import { Modal, Message } from '@arco-design/web-vue';
  import { api, Memory } from '@/api/workspace';
  import { useWorkspaceStore } from '@/stores/workspace';
  import { useUserStore } from '@/store';
  import { useTeamRequest } from '@/utils/teamRequest';
  import { useRoute, useRouter } from 'vue-router';
  const store = useWorkspaceStore();
  const user = useUserStore();
  const route = useRoute();
  const router = useRouter();
  const teamRequest = useTeamRequest(() => store.activeTeam.id);
  let displayedTeamId = '';
  const tab = ref(route.query.tab === 'team' ? 'team' : 'personal');
  const personal = ref<Memory[]>([]);
  const team = ref<Memory[]>([]);
  const visibleItems = computed(() =>
    tab.value === 'personal' ? personal.value : team.value
  );
  const error = ref('');
  const formError = ref('');
  const modal = ref(false);
  const saving = ref(false);
  const draft = ref({
    id: '',
    category: 'profile',
    content: '',
    confirmed: false,
    reviewAt: '',
    expiresAt: '',
  });
  function base() {
    return tab.value === 'personal'
      ? '/me/memories'
      : `/teams/${store.activeTeam.id}/memories`;
  }
  async function refresh() {
    const request = teamRequest.begin();
    try {
      const loadedPersonal = await api<Memory[]>('GET', '/me/memories');
      if (!teamRequest.isCurrent(request)) return;
      const loadedTeam = request.teamId
        ? await api<Memory[]>('GET', `/teams/${request.teamId}/memories`)
        : [];
      if (!teamRequest.isCurrent(request)) return;
      personal.value = loadedPersonal;
      team.value = loadedTeam;
      error.value = '';
    } catch (e) {
      if (!teamRequest.isCurrent(request)) return;
      error.value = (e as Error).message;
    }
  }
  function openNew() {
    draft.value = {
      id: '',
      category: tab.value === 'personal' ? 'profile' : 'team_decision',
      content: '',
      confirmed: false,
      reviewAt: '',
      expiresAt: '',
    };
    formError.value = '';
    modal.value = true;
  }
  function edit(m: Memory) {
    draft.value = { ...m, reviewAt: dateOnly(m.reviewAt), expiresAt: dateOnly(m.expiresAt) };
    formError.value = '';
    modal.value = true;
  }
  async function save() {
    if (saving.value) return;
    if (!draft.value.category.trim() || !draft.value.content.trim()) {
      formError.value = '请填写类别和内容';
      modal.value = true;
      return;
    }
    saving.value = true;
    try {
      if (draft.value.id)
        await api('PATCH', `${base()}/${draft.value.id}`, {
          category: draft.value.category,
          content: draft.value.content,
          confirmed: draft.value.confirmed,
          reviewAt: draft.value.reviewAt,
          expiresAt: draft.value.expiresAt,
        });
      else
        await api('POST', base(), {
          category: draft.value.category,
          content: draft.value.content,
          reviewAt: draft.value.reviewAt,
          expiresAt: draft.value.expiresAt,
        });
      modal.value = false;
      Message.success('内容已保存');
      await refresh();
    } catch (e) {
      formError.value = (e as Error).message;
      modal.value = true;
    } finally {
      saving.value = false;
    }
  }
  function dateOnly(value?: string | null) {
    return value ? value.slice(0, 10) : '';
  }
  function formatDate(value: string) {
    return dateOnly(value);
  }
  function memoryExpired(memory: Memory) {
    return Boolean(memory.expiresAt && dateOnly(memory.expiresAt) < today());
  }
  function memoryReviewDue(memory: Memory) {
    return Boolean(memory.reviewAt && dateOnly(memory.reviewAt) <= today());
  }
  function today() {
    return new Intl.DateTimeFormat('en-CA', { timeZone: user.timezone || 'Asia/Shanghai' }).format(new Date());
  }
  function viewSource(memory: Memory) {
    if (!memory.sourceConversationId || !memory.sourceMessageId) return;
    router.push({ path: '/teammind/assistant', query: { conversationId: memory.sourceConversationId, sourceMessageId: memory.sourceMessageId } });
  }
  function remove(m: Memory) {
    Modal.confirm({
      title: '删除内容？',
      content: '删除后将无法再查看。',
      onOk: async () => {
        try {
          await api('DELETE', `${base()}/${m.id}`);
          await refresh();
        } catch (e) {
          Message.error((e as Error).message);
        }
      },
    });
  }
  watch(() => store.activeTeam.id, (id) => {
    if (id !== displayedTeamId) {
      displayedTeamId = id;
      team.value = [];
      error.value = '';
      modal.value = false;
    }
    if (id && route.query.tab === 'team') tab.value = 'team';
    refresh();
  }, { immediate: true });
  watch(() => route.query.tab, (value) => {
    if (value === 'team' && store.activeTeam.id) tab.value = 'team';
    else if (value === 'personal') tab.value = 'personal';
  });
</script>
