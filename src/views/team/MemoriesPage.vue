<template>
  <div class="page">
    <PageHeader title="记忆与知识" :subtitle="`${personal.length} 条个人记忆 · ${teamMemories.length} 条团队知识`">
      <template #actions>
        <a-button :loading="loading" @click="load"><reload-outlined /> 刷新</a-button>
        <a-button type="primary" @click="openCreate"><plus-outlined /> 新增记忆</a-button>
      </template>
    </PageHeader>

    <a-alert v-if="error" type="error" show-icon :message="error" closable @close="error = ''" />

    <div class="panel mem-panel">
      <a-tabs v-model:activeKey="scope" class="mem-tabs">
        <a-tab-pane key="personal" :tab="`个人记忆 ${personal.length}`" />
        <a-tab-pane key="team" :tab="`团队知识 ${teamMemories.length}`" />
      </a-tabs>

      <a-alert
        v-if="scope === 'team'"
        type="info"
        show-icon
        message="团队知识由组织管理员维护，作为 AI 助手回答团队问题的候选上下文。"
        style="margin: 12px 16px"
      />

      <a-spin :spinning="loading">
        <div v-for="memory in currentList" :key="memory.id" class="memory">
          <div class="mem-main">
            <div class="mem-head">
              <a-tag color="blue" :bordered="false">{{ categoryLabel(memory.category) }}</a-tag>
              <a-tag :color="memory.confirmed ? 'success' : 'default'" :bordered="false">
                {{ memory.confirmed ? '已确认' : '待确认' }}
              </a-tag>
              <span v-if="memory.reviewAt" class="muted meta">复核 {{ fmtDate(memory.reviewAt) }}</span>
              <span v-if="memory.expiresAt" class="muted meta">到期 {{ fmtDate(memory.expiresAt) }}</span>
            </div>
            <div class="mem-content">{{ memory.content }}</div>
            <div class="mem-foot">
              <span class="muted">更新于 {{ fmtDateTime(memory.updatedAt || memory.createdAt) }}</span>
              <a
                v-if="memory.sourceConversationId && memory.sourceMessageId"
                @click="router.push({ path: '/teammind/assistant', query: { conversationId: memory.sourceConversationId, messageId: memory.sourceMessageId } })"
              >
                查看来源对话 →
              </a>
            </div>
          </div>
          <div class="mem-actions">
            <a-button type="link" size="small" @click="openEdit(memory)">编辑</a-button>
            <a-popconfirm title="删除这条记忆？" @confirm="remove(memory)">
              <a-button type="link" size="small" danger>删除</a-button>
            </a-popconfirm>
          </div>
        </div>
        <div v-if="!currentList.length && !loading" class="empty-block">
          还没有{{ scope === 'personal' ? '个人记忆' : '团队知识' }}。AI 对话里可以随时把一段内容保存为候选。
        </div>
      </a-spin>
    </div>

    <a-modal v-model:open="editOpen" :title="draft.id ? '编辑记忆' : '新增记忆'" :confirm-loading="saving" @ok="save">
      <a-form layout="vertical" :model="draft">
        <a-form-item label="类别">
          <a-select v-model:value="draft.category">
            <a-select-option v-for="item in MEMORY_CATEGORY" :key="item.value" :value="item.value">{{ item.label }}</a-select-option>
          </a-select>
        </a-form-item>
        <a-form-item label="内容" required>
          <a-textarea v-model:value="draft.content" :rows="6" placeholder="一条清晰、可复用的事实或偏好" />
        </a-form-item>
        <div class="form-grid">
          <a-form-item label="复核日期">
            <a-date-picker v-model:value="draft.reviewAt" value-format="YYYY-MM-DD" style="width: 100%" />
          </a-form-item>
          <a-form-item label="到期日期">
            <a-date-picker v-model:value="draft.expiresAt" value-format="YYYY-MM-DD" style="width: 100%" />
          </a-form-item>
        </div>
        <a-form-item v-if="draft.id">
          <a-checkbox v-model:checked="draft.confirmed">已确认</a-checkbox>
        </a-form-item>
      </a-form>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue';
import { useRouter } from 'vue-router';
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons-vue';
import { api } from '@/api/client';
import type { Memory } from '@/api/types';
import { useSession } from '@/store/session';
import { fmtDate, fmtDateTime } from '@/utils/format';
import { labelOf, MEMORY_CATEGORY } from '@/utils/labels';
import { notify } from '@/utils/notify';
import PageHeader from '@/components/PageHeader.vue';

const router = useRouter();
const session = useSession();
const loading = ref(false);
const saving = ref(false);
const error = ref('');
const scope = ref<'personal' | 'team'>('personal');
const personal = ref<Memory[]>([]);
const teamMemories = ref<Memory[]>([]);

const currentList = computed(() => (scope.value === 'personal' ? personal.value : teamMemories.value));

function categoryLabel(value: string): string {
  return labelOf(MEMORY_CATEGORY, value);
}

function basePath(): string {
  return scope.value === 'personal' ? '/me/memories' : `/teams/${session.activeId}/memories`;
}

async function load(): Promise<void> {
  if (!session.activeId) return;
  loading.value = true;
  try {
    const teamId = session.activeId;
    const [mine, team] = await Promise.all([
      api<Memory[]>('GET', '/me/memories'),
      api<Memory[]>('GET', `/teams/${teamId}/memories`),
    ]);
    if (teamId !== session.activeId) return;
    personal.value = mine || [];
    teamMemories.value = team || [];
    error.value = '';
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}

const editOpen = ref(false);
const draft = reactive<Record<string, any>>({
  id: '',
  category: 'experience',
  content: '',
  reviewAt: null,
  expiresAt: null,
  confirmed: false,
});

function openCreate(): void {
  Object.assign(draft, { id: '', category: 'experience', content: '', reviewAt: null, expiresAt: null, confirmed: false });
  editOpen.value = true;
}

function openEdit(memory: Memory): void {
  Object.assign(draft, {
    id: memory.id,
    category: memory.category,
    content: memory.content,
    reviewAt: memory.reviewAt || null,
    expiresAt: memory.expiresAt || null,
    confirmed: memory.confirmed,
  });
  editOpen.value = true;
}

async function save(): Promise<void> {
  if (!draft.content.trim()) {
    notify.error('请填写记忆内容');
    return;
  }
  saving.value = true;
  try {
    const body: Record<string, unknown> = {
      category: draft.category,
      content: draft.content.trim(),
      reviewAt: draft.reviewAt || '',
      expiresAt: draft.expiresAt || '',
    };
    if (draft.id) {
      body.confirmed = draft.confirmed;
      await api('PATCH', `${basePath()}/${draft.id}`, body);
    } else {
      await api('POST', basePath(), body);
    }
    editOpen.value = false;
    notify.success('已保存');
    await load();
  } catch (e) {
    notify.error(e);
  } finally {
    saving.value = false;
  }
}

async function remove(memory: Memory): Promise<void> {
  try {
    await api('DELETE', `${basePath()}/${memory.id}`);
    notify.success('已删除');
    await load();
  } catch (e) {
    notify.error(e);
  }
}

watch(() => session.activeId, load);
watch(scope, () => {
  if (scope.value === 'team' && !session.isAdmin) {
    // 仍允许查看，但写入会被后端拒绝
  }
});
onMounted(load);
</script>

<style scoped>
.mem-panel {
  padding: 0 16px 8px;
}
.mem-tabs :deep(.ant-tabs-nav) {
  margin-bottom: 0;
}
.memory {
  display: flex;
  gap: 16px;
  padding: 16px 8px;
  border-bottom: 1px solid var(--tm-border);
}
.memory:last-child {
  border-bottom: 0;
}
.mem-main {
  flex: 1;
  min-width: 0;
}
.mem-head {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.meta {
  font-size: 11.5px;
}
.mem-content {
  margin-top: 8px;
  color: var(--tm-text);
  font-size: 13px;
  line-height: 1.75;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.mem-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-top: 8px;
  font-size: 11.5px;
}
.mem-actions {
  flex: none;
  display: flex;
  align-items: flex-start;
}
</style>
