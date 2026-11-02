<template>
  <div class="section">
    <div class="head">
      <div>
        <h3>组织审计日志</h3>
        <p class="muted desc">记录组织内的关键操作，仅管理员可见，最多保留最近 100 条/页。</p>
      </div>
      <a-button size="small" :loading="loading" @click="load">刷新</a-button>
    </div>

    <a-alert v-if="error" type="error" show-icon :message="error" closable @close="error = ''" />

    <a-table
      :data-source="items"
      :columns="columns"
      :loading="loading"
      row-key="id"
      size="middle"
      :pagination="{
        current: page,
        pageSize,
        total,
        showSizeChanger: false,
        showTotal: (value: number) => `共 ${value} 条`,
      }"
      @change="onChange"
    >
      <template #bodyCell="{ column, record }">
        <template v-if="column.key === 'action'">
          <a-tag :bordered="false">{{ record.action }}</a-tag>
        </template>
        <template v-else-if="column.key === 'resource'">
          <span class="muted">{{ record.resourceType || '—' }}</span>
        </template>
        <template v-else-if="column.key === 'reason'">
          <span class="reason">{{ record.reason || record.afterSummary || '—' }}</span>
        </template>
        <template v-else-if="column.key === 'createdAt'">
          <span class="muted">{{ fmtDateTime(record.createdAt) }}</span>
        </template>
      </template>
    </a-table>
    <div v-if="!items.length && !loading" class="empty-block">暂无审计记录</div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref, watch } from 'vue';
import { api } from '@/api/client';
import type { AuditEntry, PageResult } from '@/api/types';
import { useSession } from '@/store/session';
import { fmtDateTime } from '@/utils/format';

const session = useSession();
const loading = ref(false);
const error = ref('');
const items = ref<AuditEntry[]>([]);
const page = ref(1);
const pageSize = 20;
const total = ref(0);

const columns = [
  { title: '操作', key: 'action', dataIndex: 'action', width: 200 },
  { title: '对象类型', key: 'resource', width: 120 },
  { title: '原因 / 摘要', key: 'reason', dataIndex: 'reason' },
  { title: '时间', key: 'createdAt', width: 170 },
];

async function load(): Promise<void> {
  if (!session.activeId) return;
  loading.value = true;
  try {
    const result = await api<PageResult<AuditEntry>>('GET', `/teams/${session.activeId}/audit-log`, undefined, {
      page: page.value,
      pageSize,
    });
    items.value = result.items || [];
    total.value = result.total || 0;
    error.value = '';
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}

function onChange(pagination: { current?: number }): void {
  page.value = pagination.current || 1;
  load();
}

watch(() => session.activeId, () => {
  page.value = 1;
  load();
});
onMounted(load);
</script>

<style scoped>
.head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 14px;
}
h3 {
  margin: 0;
  font-size: 14px;
}
.desc {
  margin: 4px 0 0;
  font-size: 12px;
}
.reason {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  color: var(--tm-text-2);
  font-size: 12px;
}
</style>
