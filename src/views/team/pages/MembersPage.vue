<template
  ><section class="tm-page"
    ><header class="tm-header"
      ><div
        ><h1>团队成员</h1
        ><p>{{ store.activeTeam.name }} 的有效成员与邀请</p></div
      ><a-button
        v-if="store.isAdmin"
        type="primary"
        @click="inviteVisible = true"
        >邀请成员</a-button
      ></header
    ><div v-if="error" class="tm-error">{{ error }}</div
    ><div v-if="!members.length" class="tm-empty">暂无成员</div
    ><article v-for="m in members" :key="m.id" class="tm-panel tm-member"
      ><div
        ><h2>{{ m.name }}</h2
        ><p>{{ m.email }} · {{ m.role }} · {{ m.status }}</p></div
      ><div v-if="store.isAdmin && m.id !== user.id" class="tm-actions"
        ><a-button size="small" @click="edit(m)">角色与状态</a-button></div
      ></article
    ><section v-if="store.isAdmin" class="tm-panel"
      ><h2>邀请链接</h2
      ><div v-if="!invitations.length" class="tm-empty">暂无邀请</div
      ><div v-for="i in invitations" :key="i.id" class="tm-row"
        ><span
          >{{ i.usedCount }}/{{ i.maxUses }} 次 ·
          {{
            i.revoked
              ? '已撤销'
              : `到期 ${formatDateTime(i.expiresAt, user.timezone)}`
          }}</span
        ><a-button
          v-if="!i.revoked"
          size="small"
          status="danger"
          @click="revoke(i.id)"
          >撤销</a-button
        ></div
      ></section
    ><a-modal
      v-model:visible="inviteVisible"
      title="创建邀请"
      :ok-loading="saving"
      @ok="createInvite"
      ><div class="tm-form"
        ><label
          >有效小时数<input
            v-model.number="invite.expiresInHours"
            type="number"
            min="1"
            max="720" /></label
        ><label
          >最多使用次数<input
            v-model.number="invite.maxUses"
            type="number"
            min="1"
            max="100" /></label
        ><p v-if="formError" class="tm-error">{{ formError }}</p></div
      ></a-modal
    ><a-modal
      v-model:visible="linkVisible"
      title="邀请链接（仅显示一次）"
      :footer="false"
      ><p>请复制并发送给成员。未登录者可注册后接受。</p
      ><input :value="inviteLink" readonly class="tm-link" /><a-button
        @click="copy"
        >复制链接</a-button
      ></a-modal
    ><a-modal
      v-model:visible="memberVisible"
      title="修改成员"
      :ok-loading="saving"
      @ok="saveMember"
      ><div class="tm-form"
        ><p>{{ selected?.name }}</p
        ><label
          >角色<select v-model="memberRole"
            ><option value="member">成员</option
            ><option value="team_admin">管理员</option
            ><option
              v-if="store.activeTeam.role === 'team_owner'"
              value="team_owner"
              >转移所有权</option
            ></select
          ></label
        ><label
          >状态<select v-model="memberStatus"
            ><option value="active">正常</option
            ><option value="suspended">停用</option
            ><option value="left">移除</option></select
          ></label
        ><p v-if="formError" class="tm-error">{{ formError }}</p></div
      ></a-modal
    ></section
  ></template
>
<script setup lang="ts">
  import { formatDateTime } from '@/utils/time';
  import { ref, watch } from 'vue';
  import { Message, Modal } from '@arco-design/web-vue';
  import { api, Member } from '@/api/workspace';
  import { useWorkspaceStore } from '@/stores/workspace';
  import { useTeamRequest } from '@/utils/teamRequest';
  import { useUserStore } from '@/store';
  const store = useWorkspaceStore();
  const teamRequest = useTeamRequest(() => store.activeTeam.id);
  let displayedTeamId = '';
  const user = useUserStore();
  const members = ref<Member[]>([]);
  const invitations = ref<
    {
      id: string;
      usedCount: number;
      maxUses: number;
      revoked: boolean;
      expiresAt: string;
    }[]
  >([]);
  const error = ref('');
  const formError = ref('');
  const inviteVisible = ref(false);
  const memberVisible = ref(false);
  const linkVisible = ref(false);
  const inviteLink = ref('');
  const saving = ref(false);
  const invite = ref({ expiresInHours: 168, maxUses: 1 });
  const selected = ref<Member | null>(null);
  const memberRole = ref('member');
  const memberStatus = ref('active');
  async function refresh() {
    const request = teamRequest.begin();
    const id = request.teamId;
    if (!id) {
      members.value = [];
      invitations.value = [];
      return;
    }
    try {
      const loadedMembers = await api<Member[]>('GET', `/teams/${id}/members`);
      if (!teamRequest.isCurrent(request)) return;
      const loadedInvitations = store.isAdmin
        ? await api<{ id: string; usedCount: number; maxUses: number; revoked: boolean; expiresAt: string }[]>('GET', `/teams/${id}/invitations`)
        : [];
      if (!teamRequest.isCurrent(request)) return;
      members.value = loadedMembers;
      invitations.value = loadedInvitations;
      error.value = '';
    } catch (e) {
      if (!teamRequest.isCurrent(request)) return;
      error.value = (e as Error).message;
    }
  }
  async function createInvite() {
    if (saving.value) return;
    saving.value = true;
    try {
      const result = await api<{ token: string }>(
        'POST',
        `/teams/${store.activeTeam.id}/invitations`,
        invite.value
      );
      inviteLink.value = `${location.origin}/teammind/join/${result.token}`;
      inviteVisible.value = false;
      linkVisible.value = true;
      await refresh();
    } catch (e) {
      formError.value = (e as Error).message;
      inviteVisible.value = true;
    } finally {
      saving.value = false;
    }
  }
  async function copy() {
    let copied = false;
    try {
      await navigator.clipboard.writeText(inviteLink.value);
      copied = true;
    } catch { /* Safari and embedded browsers may block the async clipboard API. */ }
    if (!copied) {
      const input = document.querySelector<HTMLInputElement>('.tm-link');
      if (input) {
        input.focus();
        input.select();
        copied = document.execCommand('copy');
      }
    }
    if (copied) Message.success('链接已复制');
    else Message.error('复制失败，请手动选择链接');
  }
  function revoke(id: string) {
    Modal.confirm({
      title: '撤销邀请？',
      content: '撤销后链接立即失效。',
      onOk: async () => {
        try {
          await api(
            'DELETE',
            `/teams/${store.activeTeam.id}/invitations/${id}`
          );
          Message.success('邀请已撤销');
          await refresh();
        } catch (e) {
          Message.error((e as Error).message);
        }
      },
    });
  }
  function edit(m: Member) {
    selected.value = m;
    memberRole.value = m.role;
    memberStatus.value = m.status;
    formError.value = '';
    memberVisible.value = true;
  }
  async function saveMember() {
    if (!selected.value || saving.value) return;
    const action = async () => {
      saving.value = true;
      try {
        await api(
          'PATCH',
          `/teams/${store.activeTeam.id}/members/${selected.value?.id}`,
          { role: memberRole.value, status: memberStatus.value }
        );
        memberVisible.value = false;
        Message.success('成员已更新');
        await store.load();
        await refresh();
      } catch (e) {
        formError.value = (e as Error).message;
        memberVisible.value = true;
      } finally {
        saving.value = false;
      }
    };
    if (memberStatus.value !== 'active' || memberRole.value === 'team_owner') {
      Modal.confirm({
        title: '确认高影响操作',
        content: '成员状态或所有权将立即改变，并记录审计。',
        onOk: action,
      });
    } else await action();
  }
  watch(() => store.activeTeam.id, (id) => {
    if (id !== displayedTeamId) {
      displayedTeamId = id;
      members.value = [];
      invitations.value = [];
      error.value = '';
      inviteVisible.value = false;
      memberVisible.value = false;
      linkVisible.value = false;
      selected.value = null;
      inviteLink.value = '';
    }
    refresh();
  }, { immediate: true });
</script>
