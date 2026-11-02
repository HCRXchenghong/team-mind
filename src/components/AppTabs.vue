<template>
  <div class="tabbar">
    <div class="tabs-scroll">
      <div
        v-for="tab in tabs"
        :key="tab.path"
        class="tab"
        :class="{ active: route.path === tab.path }"
        @click="go(tab.path)"
      >
        <span class="tab-title">{{ tab.title }}</span>
        <close-outlined v-if="tab.closable" class="close" @click.stop="close(tab.path)" />
      </div>
    </div>
    <a-dropdown trigger="click">
      <a-button type="text" size="small" class="more">
        <more-outlined />
      </a-button>
      <template #overlay>
        <a-menu @click="onMenu">
          <a-menu-item key="close-current">关闭当前页</a-menu-item>
          <a-menu-item key="close-others">关闭其他页</a-menu-item>
          <a-menu-item key="close-all">关闭全部</a-menu-item>
        </a-menu>
      </template>
    </a-dropdown>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { CloseOutlined, MoreOutlined } from '@ant-design/icons-vue';
import { useSession } from '@/store/session';

interface Tab {
  path: string;
  title: string;
  closable: boolean;
}

const HOME = '/teammind/overview';
const route = useRoute();
const router = useRouter();
const session = useSession();
const tabs = ref<Tab[]>([{ path: HOME, title: '总览', closable: false }]);

watch(
  () => route.path,
  (path) => {
    if (!path.startsWith('/teammind/')) return;
    const title = String(route.meta.title || '工作台');
    if (!tabs.value.some((tab) => tab.path === path)) {
      tabs.value.push({ path, title, closable: path !== HOME });
    }
  },
  { immediate: true },
);

watch(
  () => session.activeId,
  () => {
    tabs.value = [{ path: HOME, title: '总览', closable: false }];
  },
);

function go(path: string): void {
  if (route.path !== path) router.push(path);
}

function close(path: string): void {
  const index = tabs.value.findIndex((tab) => tab.path === path);
  if (index < 0 || path === HOME) return;
  tabs.value.splice(index, 1);
  if (route.path === path) {
    const next = tabs.value[Math.max(0, index - 1)];
    router.push(next ? next.path : HOME);
  }
}

function onMenu({ key }: { key: string | number }): void {
  if (key === 'close-current') close(route.path);
  if (key === 'close-others') {
    tabs.value = tabs.value.filter((tab) => tab.path === HOME || tab.path === route.path);
  }
  if (key === 'close-all') {
    tabs.value = [{ path: HOME, title: '总览', closable: false }];
    router.push(HOME);
  }
}
</script>

<style scoped>
.tabbar {
  display: flex;
  align-items: center;
  height: var(--tm-tabs-h);
  padding: 0 12px 0 16px;
  background: var(--tm-panel);
  border-bottom: 1px solid var(--tm-border);
}
.tabs-scroll {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 1;
  min-width: 0;
  overflow-x: auto;
  scrollbar-width: none;
}
.tabs-scroll::-webkit-scrollbar {
  display: none;
}
.tab {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 26px;
  padding: 0 9px;
  border-radius: 6px;
  color: var(--tm-muted);
  font-size: 12px;
  cursor: pointer;
  white-space: nowrap;
  border: 1px solid transparent;
}
.tab:hover {
  background: var(--tm-panel-2);
  color: var(--tm-text-2);
}
.tab.active {
  background: var(--tm-primary-soft);
  border-color: transparent;
  color: var(--tm-primary);
  font-weight: 600;
}
.close {
  font-size: 10px;
  opacity: 0.6;
  border-radius: 3px;
}
.close:hover {
  opacity: 1;
  background: rgba(0, 0, 0, 0.08);
}
.more {
  color: var(--tm-muted);
}
</style>
