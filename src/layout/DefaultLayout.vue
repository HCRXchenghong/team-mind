<template>
  <div class="shell">
    <aside class="sider" :class="{ collapsed }">
      <div class="brand" @click="router.push('/teammind/overview')">
        <BrandLogo :show-name="!collapsed" />
      </div>

      <a-menu
        class="side-menu"
        mode="inline"
        :selected-keys="[route.path]"
        :inline-collapsed="collapsed"
        @click="onMenu"
      >
        <a-menu-item-group v-for="group in visibleMenu" :key="group.title" :title="collapsed ? '' : group.title">
          <a-menu-item v-for="item in group.items" :key="item.path">
            <template #icon><component :is="item.icon" /></template>
            <span>{{ collapsed ? '' : item.title }}</span>
          </a-menu-item>
        </a-menu-item-group>
      </a-menu>

      <div v-if="!collapsed" class="sider-foot">
        <span class="dot" />
        API {{ apiHost }}
        <span class="ver">v2.0</span>
      </div>
    </aside>

    <div class="body" :class="{ collapsed }">
      <header class="header">
        <div class="header-left">
          <a-button type="text" class="icon-btn" aria-label="切换菜单" @click="toggleSider">
            <menu-unfold-outlined v-if="collapsed || mobile" />
            <menu-fold-outlined v-else />
          </a-button>
          <a-breadcrumb class="crumb">
            <a-breadcrumb-item>TeamMind</a-breadcrumb-item>
            <a-breadcrumb-item>{{ currentGroup }}</a-breadcrumb-item>
            <a-breadcrumb-item>{{ currentTitle }}</a-breadcrumb-item>
          </a-breadcrumb>
        </div>

        <div class="header-right">
          <a-select
            v-if="session.teams.length"
            :value="session.activeId || undefined"
            class="team-select"
            size="middle"
            :dropdown-match-select-width="false"
            @change="switchTeam"
          >
            <a-select-option v-for="team in session.teams" :key="team.id" :value="team.id">
              {{ team.name }}{{ team.status === 'pending_review' ? '（待审核）' : '' }}
            </a-select-option>
          </a-select>
          <a-tooltip title="创建组织">
            <a-button class="icon-btn" @click="createOpen = true"><plus-outlined /></a-button>
          </a-tooltip>
          <a-tooltip title="搜索任务">
            <a-button class="icon-btn" @click="searchOpen = true"><search-outlined /></a-button>
          </a-tooltip>
          <a-tooltip title="通知中心">
            <a-badge :count="unread" :overflow-count="99" :offset="[-2, 2]">
              <a-button class="icon-btn" @click="router.push('/teammind/notifications')">
                <bell-outlined />
              </a-button>
            </a-badge>
          </a-tooltip>
          <a-tooltip :title="isDark ? '切换浅色主题' : '切换深色主题'">
            <a-button class="icon-btn" @click="toggleTheme">
              <bulb-filled v-if="isDark" />
              <bulb-outlined v-else />
            </a-button>
          </a-tooltip>
          <a-dropdown placement="bottomRight" trigger="click">
            <button class="account">
              <UserAvatar :name="session.user?.name" :size="28" />
              <span class="account-name">{{ session.user?.name }}</span>
              <down-outlined class="down" />
            </button>
            <template #overlay>
              <a-menu @click="onAccount">
                <a-menu-item key="profile"><user-outlined /> 个人资料</a-menu-item>
                <a-menu-item key="settings"><setting-outlined /> 账号设置</a-menu-item>
                <a-menu-divider />
                <a-menu-item key="logout"><logout-outlined /> 退出登录</a-menu-item>
              </a-menu>
            </template>
          </a-dropdown>
        </div>
      </header>

      <AppTabs v-if="!mobile" />

      <main class="main">
        <router-view :key="`${route.fullPath}-${session.activeId}`" />
        <footer class="foot">TeamMind · 协作与 AI 工作台 · 2026</footer>
      </main>
    </div>

    <a-drawer v-model:open="drawerOpen" placement="left" :width="252" :closable="false" :body-style="{ padding: 0 }">
      <div class="drawer-brand"><BrandLogo /></div>
      <a-menu mode="inline" :selected-keys="[route.path]" class="drawer-menu" @click="onMenu">
        <a-menu-item-group v-for="group in visibleMenu" :key="group.title" :title="group.title">
          <a-menu-item v-for="item in group.items" :key="item.path">
            <template #icon><component :is="item.icon" /></template>
            {{ item.title }}
          </a-menu-item>
        </a-menu-item-group>
      </a-menu>
    </a-drawer>

    <a-modal v-model:open="createOpen" title="创建组织" :confirm-loading="creating" :ok-text="'创建'" @ok="createTeam">
      <a-form layout="vertical" :model="newTeam">
        <a-form-item label="组织名称" required>
          <a-input v-model:value="newTeam.name" placeholder="例如：产品研发团队" :maxlength="80" />
        </a-form-item>
        <a-form-item label="组织类型">
          <a-radio-group v-model:value="newTeam.kind">
            <a-radio value="team">团队（单层小组）</a-radio>
            <a-radio value="company">公司（部门制）</a-radio>
          </a-radio-group>
        </a-form-item>
        <a-form-item label="描述">
          <a-textarea v-model:value="newTeam.description" :rows="2" :maxlength="200" />
        </a-form-item>
        <a-form-item label="时区">
          <a-input v-model:value="newTeam.timezone" />
        </a-form-item>
      </a-form>
    </a-modal>

    <a-modal v-model:open="searchOpen" title="搜索任务" :footer="null" @after-open-change="focusSearch">
      <a-input-search
        ref="searchRef"
        v-model:value="searchText"
        placeholder="输入任务关键词，回车搜索"
        enter-button="搜索"
        @search="submitSearch"
      />
      <p class="muted" style="margin: 10px 0 0">搜索范围：当前组织的任务标题。</p>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import {
  BellOutlined,
  DownOutlined,
  LogoutOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  BulbFilled,
  PlusOutlined,
  SearchOutlined,
  SettingOutlined,
  BulbOutlined,
  UserOutlined,
} from '@ant-design/icons-vue';
import { api, apiBase, getToken } from '@/api/client';
import { useSession } from '@/store/session';
import { isDark, toggleTheme } from '@/composables/useTheme';
import { workspaceMenu } from '@/router/menu';
import { notify } from '@/utils/notify';
import UserAvatar from '@/components/UserAvatar.vue';
import AppTabs from '@/components/AppTabs.vue';
import BrandLogo from '@/components/BrandLogo.vue';

const route = useRoute();
const router = useRouter();
const session = useSession();

const mobile = ref(window.innerWidth <= 900);
const collapsed = ref(localStorage.getItem('teammind.menuCollapsed') === '1');
const drawerOpen = ref(false);
const createOpen = ref(false);
const creating = ref(false);
const searchOpen = ref(false);
const searchText = ref('');
const searchRef = ref();
const unread = ref(0);

const newTeam = reactive({
  name: '',
  kind: 'team' as 'team' | 'company',
  description: '',
  timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || 'Asia/Shanghai',
});

const visibleMenu = computed(() =>
  workspaceMenu
    .map((group) => ({
      ...group,
      items: group.items.filter((item) => {
        if (item.superAdmin && !session.isSuperAdmin) return false;
        if (item.manage && !session.canManage) return false;
        return true;
      }),
    }))
    .filter((group) => group.items.length > 0),
);

const allItems = computed(() => workspaceMenu.flatMap((group) => group.items.map((item) => ({ ...item, group: group.title }))));
const currentItem = computed(() => allItems.value.find((item) => item.path === route.path));
const currentTitle = computed(() => String(route.meta.title || currentItem.value?.title || '工作台'));
const currentGroup = computed(() => currentItem.value?.group || '工作台');
const apiHost = computed(() => {
  const base = apiBase();
  return base.startsWith('http') ? new URL(base).host : '/api/v1';
});

function onResize(): void {
  mobile.value = window.innerWidth <= 900;
}

function toggleSider(): void {
  if (mobile.value) {
    drawerOpen.value = true;
    return;
  }
  collapsed.value = !collapsed.value;
  localStorage.setItem('teammind.menuCollapsed', collapsed.value ? '1' : '0');
}

function onMenu({ key }: { key: string | number }): void {
  drawerOpen.value = false;
  if (route.path !== key) router.push(String(key));
}

function switchTeam(value: unknown): void {
  session.select(String(value));
  router.push('/teammind/overview');
}

function onAccount({ key }: { key: string | number }): void {
  if (key === 'profile') router.push('/teammind/settings?tab=profile');
  if (key === 'settings') router.push('/teammind/settings?tab=security');
  if (key === 'logout') logout();
}

async function logout(): Promise<void> {
  await session.logout().catch(() => undefined);
  router.replace('/login');
}

async function createTeam(): Promise<void> {
  if (newTeam.name.trim().length < 2) {
    notify.error('组织名称至少 2 个字');
    return;
  }
  creating.value = true;
  try {
    const team = await api<{ id: string }>('POST', '/teams', { ...newTeam, name: newTeam.name.trim() });
    await session.load();
    session.select(team.id);
    createOpen.value = false;
    notify.success('组织已创建');
    router.push('/teammind/overview');
  } catch (error) {
    notify.error(error);
  } finally {
    creating.value = false;
  }
}

function submitSearch(): void {
  const q = searchText.value.trim();
  searchOpen.value = false;
  router.push({ path: '/teammind/tasks', query: q ? { q } : {} });
}

function focusSearch(open: boolean): void {
  if (!open) return;
  setTimeout(() => {
    const input = document.querySelector<HTMLInputElement>('.ant-modal input');
    input?.focus();
  }, 80);
}

async function loadUnread(): Promise<void> {
  if (!getToken()) return;
  try {
    unread.value = (await api<{ count: number }>('GET', '/me/notifications/unread-count')).count;
  } catch {
    unread.value = 0;
  }
}

let disposed = false;
let reconnectTimer: number | undefined;
let pollTimer: number | undefined;
let controller: AbortController | undefined;

async function startLive(): Promise<void> {
  if (disposed || !getToken()) return;
  controller = new AbortController();
  try {
    const response = await fetch(`${apiBase()}/me/events`, {
      headers: { Authorization: `Bearer ${getToken()}` },
      signal: controller.signal,
    });
    if (!response.ok || !response.body) throw new Error('实时连接不可用');
    const reader = response.body.getReader();
    while (!disposed) {
      const chunk = await reader.read();
      if (chunk.done) break;
      loadUnread();
    }
  } catch {
    /* 断线后 3 秒重连 */
  } finally {
    if (!disposed) reconnectTimer = window.setTimeout(startLive, 3000);
  }
}

onMounted(() => {
  disposed = false;
  window.addEventListener('resize', onResize);
  loadUnread();
  startLive();
  pollTimer = window.setInterval(loadUnread, 30000);
});

onBeforeUnmount(() => {
  disposed = true;
  window.removeEventListener('resize', onResize);
  clearTimeout(reconnectTimer);
  clearInterval(pollTimer);
  controller?.abort();
});

watch(() => route.fullPath, loadUnread);
watch(() => session.activeId, loadUnread);
</script>

<style scoped>
.shell {
  min-height: 100vh;
  background: var(--tm-bg);
}
.sider {
  position: fixed;
  top: 0;
  left: 0;
  bottom: 0;
  z-index: 30;
  display: flex;
  flex-direction: column;
  width: var(--tm-sider-w);
  background: var(--tm-panel);
  border-right: 1px solid var(--tm-border);
  transition: width 0.18s ease;
}
.sider.collapsed {
  width: 72px;
}
.brand {
  display: flex;
  align-items: center;
  gap: 10px;
  height: var(--tm-header-h);
  padding: 0 18px;
  cursor: pointer;
  border-bottom: 1px solid var(--tm-border);
  overflow: hidden;
}
.brand-mark {
  width: 28px;
  height: 28px;
  flex: none;
  display: grid;
  place-items: center;
  border-radius: 7px;
  background: var(--tm-primary);
  color: #fff;
  font-weight: 800;
  font-size: 15px;
}
.brand-name {
  font-size: 15.5px;
  font-weight: 700;
  letter-spacing: -0.02em;
  white-space: nowrap;
}
.side-menu {
  flex: 1;
  overflow-y: auto;
  overflow-x: hidden;
  border-inline-end: 0 !important;
  padding-top: 6px;
}
.sider-foot {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 10px 18px;
  border-top: 1px solid var(--tm-border);
  color: var(--tm-muted);
  font-size: 11px;
}
.sider-foot .dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #12b76a;
}
.sider-foot .ver {
  margin-left: auto;
}

.body {
  margin-left: var(--tm-sider-w);
  min-height: 100vh;
  display: flex;
  flex-direction: column;
  transition: margin-left 0.18s ease;
}
.body.collapsed {
  margin-left: 72px;
}
.header {
  position: sticky;
  top: 0;
  z-index: 20;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  height: var(--tm-header-h);
  padding: 0 18px 0 12px;
  background: var(--tm-panel);
  border-bottom: 1px solid var(--tm-border);
}
.header-left,
.header-right {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.crumb {
  font-size: 12px;
}
.icon-btn {
  width: 32px;
  height: 32px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: var(--tm-text-2);
}
.team-select {
  width: 168px;
}
.account {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 3px 6px 3px 3px;
  border: 0;
  border-radius: 999px;
  background: transparent;
  color: var(--tm-text);
  cursor: pointer;
  font-family: inherit;
}
.account:hover {
  background: var(--tm-panel-2);
}
.account-name {
  max-width: 110px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12.5px;
}
.down {
  font-size: 10px;
  color: var(--tm-muted);
}
.main {
  flex: 1;
  width: 100%;
  max-width: 100%;
  padding: 20px 24px 8px;
}
.foot {
  padding: 26px 0 14px;
  text-align: center;
  color: var(--tm-muted);
  font-size: 11px;
}
.drawer-brand {
  display: flex;
  align-items: center;
  gap: 10px;
  height: 60px;
  padding: 0 18px;
  font-weight: 700;
  border-bottom: 1px solid var(--tm-border);
}
.drawer-menu {
  border-inline-end: 0 !important;
}

@media (max-width: 900px) {
  .sider {
    display: none;
  }
  .body,
  .body.collapsed {
    margin-left: 0;
  }
  .crumb {
    display: none;
  }
  .main {
    padding: 14px 14px 6px;
  }
  .account-name,
  .team-select {
    display: none;
  }
}
</style>
