<template
  ><section class="tm-page"
    ><div class="tm-panel"
      ><h1>加入团队</h1><p>此邀请会加入你的当前 TeamMind 账号。</p
      ><p v-if="error" class="tm-error" role="alert">{{ error }}</p
      ><p v-if="done">{{ done }}</p
      ><a-button v-if="!done" type="primary" :loading="busy" @click="accept"
        >接受邀请</a-button
      ><router-link v-else to="/teammind/overview">进入团队</router-link></div
    ></section
  ></template
>
<script setup lang="ts">
  import { ref } from 'vue';
  import { useRoute } from 'vue-router';
  import { api } from '@/api/workspace';
  import { useWorkspaceStore } from '@/stores/workspace';
  const route = useRoute();
  const store = useWorkspaceStore();
  const busy = ref(false);
  const error = ref('');
  const done = ref('');
  async function accept() {
    busy.value = true;
    error.value = '';
    try {
      const result = await api<{ teamId: string; alreadyMember: boolean }>(
        'POST',
        '/invitations/accept',
        { token: String(route.params.token || '') }
      );
      await store.load();
      store.switchTeam(result.teamId);
      done.value = result.alreadyMember
        ? '你已经是该团队成员。'
        : '已成功加入团队。';
    } catch (e) {
      error.value = (e as Error).message;
    } finally {
      busy.value = false;
    }
  }
</script>
