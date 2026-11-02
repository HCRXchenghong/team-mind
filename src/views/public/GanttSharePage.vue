<template>
  <div class="share">
    <header class="share-head">
      <div class="brand"><BrandLogo /></div>
      <div class="head-right">
        <a-tag color="blue">只读分享</a-tag>
        <span class="muted">更新于 {{ fmtDateTime(data?.updatedAt) }}</span>
      </div>
    </header>

    <main class="share-main">
      <a-spin :spinning="loading">
        <a-result v-if="error" status="warning" title="链接不可用" :sub-title="error">
          <template #extra><a-button @click="router.replace('/login')">进入 TeamMind</a-button></template>
        </a-result>

        <template v-else-if="data">
          <div class="share-title">
            <div>
              <p class="eyebrow">公开甘特图 · 只读</p>
              <h1>{{ data.team?.name || '团队计划' }}</h1>
              <p class="muted">
                {{ data.team?.description || '该视图只展示排期所需字段，不含成员邮箱、任务描述与评论。' }}
              </p>
            </div>
            <div class="share-actions">
              <a-select v-model:value="projectFilter" style="width: 170px" @change="reload">
                <a-select-option value="">全部项目</a-select-option>
                <a-select-option v-for="p in data.projects || []" :key="p.id" :value="p.id">{{ p.name }}</a-select-option>
              </a-select>
              <a-select v-model:value="statusFilter" style="width: 130px">
                <a-select-option value="">全部状态</a-select-option>
                <a-select-option v-for="s in TASK_STATUS" :key="s.value" :value="s.value">{{ s.label }}</a-select-option>
              </a-select>
              <a-button @click="locateMonth">定位本月</a-button>
            </div>
          </div>

          <div class="stat-line">
            <span><b>{{ filteredTasks.length }}</b> 个任务</span>
            <span><b>{{ data.projects?.length || 0 }}</b> 个项目</span>
            <span><b>{{ Object.keys(data.dependencies || {}).length }}</b> 组依赖</span>
            <span class="muted">{{ data.team?.timezone || 'Asia/Shanghai' }}</span>
          </div>

          <GanttTimeline :tasks="filteredTasks" :dependencies="data.dependencies" :from="rangeFrom" :to="rangeTo" />
        </template>
      </a-spin>
    </main>

    <footer class="share-foot">
      <span>由 TeamMind 生成 · 只读链接</span>
      <router-link to="/login">进入工作台</router-link>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import dayjs from 'dayjs';
import { api } from '@/api/client';
import type { Task } from '@/api/types';
import { fmtDateTime } from '@/utils/format';
import { TASK_STATUS } from '@/utils/labels';
import GanttTimeline from '@/components/GanttTimeline.vue';
import BrandLogo from '@/components/BrandLogo.vue';

interface ShareData {
  team?: { name?: string; description?: string; timezone?: string };
  scope?: string;
  projects?: Array<{ id: string; name: string; status?: string }>;
  tasks?: Task[];
  dependencies?: Record<string, string[]>;
  updatedAt?: string;
}

const route = useRoute();
const router = useRouter();
const loading = ref(true);
const error = ref('');
const data = ref<ShareData | null>(null);
const projectFilter = ref('');
const statusFilter = ref('');
const rangeFrom = ref('');
const rangeTo = ref('');

const filteredTasks = computed(() => {
  const list = data.value?.tasks || [];
  return list.filter((task) => {
    if (projectFilter.value && task.projectId !== projectFilter.value) return false;
    if (statusFilter.value && task.status !== statusFilter.value) return false;
    return true;
  });
});

async function reload(): Promise<void> {
  loading.value = true;
  error.value = '';
  try {
    const token = String(route.params.token || '');
    data.value = await api<ShareData>('GET', `/public/gantt/${token}`);
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}

function locateMonth(): void {
  rangeFrom.value = dayjs().startOf('month').format('YYYY-MM-DD');
  rangeTo.value = dayjs().endOf('month').format('YYYY-MM-DD');
}

onMounted(reload);
</script>

<style scoped>
.share {
  min-height: 100vh;
  background: var(--tm-bg);
}
.share-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: 58px;
  padding: 0 24px;
  background: var(--tm-panel);
  border-bottom: 1px solid var(--tm-border);
}
.brand {
  display: flex;
  align-items: center;
  gap: 9px;
  font-weight: 700;
}
.mark {
  width: 26px;
  height: 26px;
  display: grid;
  place-items: center;
  border-radius: 7px;
  background: var(--tm-primary);
  color: #fff;
  font-weight: 800;
  font-size: 13px;
}
.head-right {
  display: flex;
  align-items: center;
  gap: 12px;
  font-size: 12px;
}
.share-main {
  width: min(1440px, calc(100% - 40px));
  margin: 0 auto;
  padding: 26px 0 40px;
}
.share-title {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
  margin-bottom: 12px;
}
.eyebrow {
  margin: 0 0 4px;
  color: var(--tm-muted);
  font-size: 11px;
  letter-spacing: 0.1em;
  text-transform: uppercase;
}
h1 {
  margin: 0 0 6px;
  font-size: 24px;
  letter-spacing: -0.02em;
}
.share-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.stat-line {
  display: flex;
  gap: 20px;
  padding: 10px 0 14px;
  color: var(--tm-text-2);
  font-size: 12px;
}
.stat-line b {
  color: var(--tm-text);
}
.share-foot {
  display: flex;
  justify-content: space-between;
  width: min(1440px, calc(100% - 40px));
  margin: 0 auto;
  padding: 14px 0 30px;
  color: var(--tm-muted);
  font-size: 11.5px;
  border-top: 1px solid var(--tm-border);
}
</style>
