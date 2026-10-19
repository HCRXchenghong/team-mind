<template>
  <div class="navbar">
    <div class="left-side">
      <a-button
        v-if="appStore.device === 'mobile'"
        type="text"
        aria-label="打开菜单"
        @click="toggleDrawerMenu"
        ><icon-menu-fold
      /></a-button>
      <img alt="TeamMind" :src="logo" />
      <strong>TeamMind</strong>
      <Menu v-if="appStore.topMenu && appStore.menu" />
    </div>
    <div class="right-side">
      <a-select
        :model-value="workspace.activeTeam.id"
        size="small"
        aria-label="切换团队"
        @change="switchTeam"
      >
        <a-option
          v-for="team in workspace.teams"
          :key="team.id"
          :value="team.id"
          >{{ team.name }} · {{ team.role }}</a-option
        >
      </a-select>
      <a-button type="text" aria-label="搜索任务" @click="searchVisible = true"
        ><icon-search
      /></a-button>
      <span class="tm-notification-link">
        <a-button
          type="text"
          :aria-label="`通知，${unreadCount} 条未读`"
          @click="router.push('/teammind/notifications')"
          ><icon-notification
        /></a-button>
        <small v-if="unreadCount" aria-hidden="true">{{
          unreadCount > 99 ? '99+' : unreadCount
        }}</small>
      </span>
      <a-button type="text" aria-label="切换主题" @click="toggleTheme"
        ><icon-moon-fill v-if="dark" /><icon-sun-fill v-else
      /></a-button>
      <a-dropdown>
        <a-button type="text" aria-label="账号菜单"
          ><icon-user />
          <span class="tm-navbar-name">{{ user.name }}</span></a-button
        >
        <template #content
          ><a-doption @click="router.push('/teammind/settings')">设置</a-doption
          ><a-doption @click="logout">退出登录</a-doption></template
        >
      </a-dropdown>
    </div>
    <a-modal v-model:visible="searchVisible" title="搜索任务" @ok="search">
      <a-input v-model="query" placeholder="任务名称" @press-enter="search" />
    </a-modal>
  </div>
</template>
<script setup lang="ts">
  import { inject, onBeforeUnmount, onMounted, ref, watch } from 'vue';
  import { useRoute, useRouter } from 'vue-router';
  import { Message } from '@arco-design/web-vue';
  import { useAppStore, useUserStore } from '@/store';
  import { useWorkspaceStore } from '@/stores/workspace';
  import { api } from '@/api/workspace';
  import logo from '@/assets/logo.svg?url';
  import Menu from '@/components/menu/index.vue';

  const appStore = useAppStore();
  const user = useUserStore();
  const workspace = useWorkspaceStore();
  const router = useRouter();
  const route = useRoute();
  const toggleDrawerMenu = inject('toggleDrawerMenu', () => {});
  function getSavedTheme() {
    try {
      return window.localStorage.getItem('teammind.theme') === 'dark';
    } catch {
      return false;
    }
  }
  const dark = ref(getSavedTheme());
  const searchVisible = ref(false);
  const query = ref('');
  const unreadCount = ref(0);
  let pollTimer: number | undefined;
  async function loadUnread() {
    try {
      const result = await api<{ count: number }>(
        'GET',
        '/me/notifications/unread-count'
      );
      unreadCount.value = result.count;
    } catch {
      unreadCount.value = 0;
    }
  }
  function switchTeam(value: unknown) {
    if (typeof value === 'string' && workspace.switchTeam(value))
      router.push('/teammind/overview');
  }
  function toggleTheme() {
    dark.value = !dark.value;
    try {
      window.localStorage.setItem('teammind.theme', dark.value ? 'dark' : 'light');
    } catch {
      // The theme still changes for this page when storage is unavailable.
    }
    appStore.toggleTheme(dark.value);
  }
  function search() {
    searchVisible.value = false;
    router.push({ path: '/teammind/tasks', query: { q: query.value } });
  }
  async function logout() {
    try {
      await user.logout();
    } catch (e) {
      Message.error((e as Error).message);
    } finally {
      await router.replace('/login');
    }
  }
  watch(() => route.fullPath, loadUnread);
  onMounted(() => {
    appStore.toggleTheme(dark.value);
    loadUnread();
    window.addEventListener('teammind:notifications-updated', loadUnread);
    pollTimer = window.setInterval(loadUnread, 30000);
  });
  onBeforeUnmount(() => {
    window.removeEventListener('teammind:notifications-updated', loadUnread);
    window.clearInterval(pollTimer);
  });
</script>
<style scoped>
  .navbar {
    height: 100%;
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 18px;
    gap: 12px;
    background: var(--color-bg-2);
    border-bottom: 1px solid var(--color-border);
  }
  .left-side,
  .right-side {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .left-side img {
    width: 28px;
    height: 28px;
  }
  .left-side strong {
    font-size: 19px;
  }
  .right-side .arco-select {
    width: 210px;
  }
  .tm-notification-link {
    position: relative;
    display: inline-flex;
  }
  .tm-notification-link small {
    position: absolute;
    top: -3px;
    right: -3px;
    min-width: 18px;
    height: 18px;
    padding: 0 3px;
    border-radius: 9px;
    background: #d92d20;
    color: white;
    font-size: 11px;
    line-height: 18px;
    text-align: center;
    pointer-events: none;
  }
  @media (max-width: 768px) {
    .navbar {
      padding: 0 8px;
    }
    .right-side {
      gap: 1px;
    }
    .right-side .arco-select {
      width: 112px;
    }
    .tm-navbar-name,
    .left-side strong {
      display: none;
    }
  }
</style>
