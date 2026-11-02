<template>
  <div class="page">
    <PageHeader title="AI 管理" subtitle="配置 OpenAI 兼容供应商池：优先级、模型列表、熔断与连通性测试">
      <template #actions>
        <a-select v-if="session.isSuperAdmin" v-model:value="pool" style="width: 150px" @change="load">
          <a-select-option value="platform">平台池</a-select-option>
          <a-select-option value="team">当前组织池</a-select-option>
        </a-select>
        <a-button :loading="loading" @click="load"><reload-outlined /> 刷新</a-button>
        <a-button type="primary" @click="openCreate"><plus-outlined /> 新增供应商</a-button>
      </template>
    </PageHeader>

    <a-alert v-if="error" type="error" show-icon :message="error" closable @close="error = ''" />
    <a-alert
      type="info"
      show-icon
      message="调用顺序按优先级从小到大；失败会累计熔断，冷却结束后自动恢复。API Key 只写不读。"
      style="margin-bottom: 0"
    />

    <div class="panel">
      <a-table :data-source="providers" :columns="columns" :loading="loading" row-key="id" size="middle" :pagination="false" :scroll="{ x: 1120 }">
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'name'">
            <div class="name">{{ record.name }}</div>
            <div class="muted url">{{ record.baseUrl }}</div>
            <div v-if="pool === 'platform' && record.teamName" class="muted url">归属：{{ record.teamName }}</div>
          </template>
          <template v-else-if="column.key === 'models'">
            <div class="models">
              <a-tag v-for="model in (record.models || []).slice(0, 3)" :key="model" :bordered="false">{{ model }}</a-tag>
              <span v-if="(record.models || []).length > 3" class="muted">+{{ record.models.length - 3 }}</span>
              <span v-if="!(record.models || []).length" class="muted">未配置模型</span>
            </div>
          </template>
          <template v-else-if="column.key === 'status'">
            <a-tag :color="record.enabled ? 'success' : 'default'">{{ record.enabled ? '已启用' : '已停用' }}</a-tag>
            <a-tag v-if="isCooling(record)" color="error">熔断中</a-tag>
            <a-tag v-else-if="record.failureCount > 0" color="warning" :bordered="false">失败 {{ record.failureCount }}</a-tag>
          </template>
          <template v-else-if="column.key === 'priority'">
            <span class="mono">{{ record.priority }}</span>
          </template>
          <template v-else-if="column.key === 'key'">
            <a-tag :color="record.hasKey ? 'green' : 'default'" :bordered="false">{{ record.hasKey ? '已配置' : '无密钥' }}</a-tag>
          </template>
          <template v-else-if="column.key === 'lastError'">
            <span class="error-text">{{ record.lastError || '—' }}</span>
          </template>
          <template v-else-if="column.key === 'actions'">
            <div class="table-actions">
              <a-button type="link" size="small" :loading="testingId === record.id" @click="testProvider(record)">测试</a-button>
              <a-button type="link" size="small" @click="openEdit(record)">编辑</a-button>
              <a-popconfirm title="删除后该供应商不再参与调度，确认？" @confirm="remove(record)">
                <a-button type="link" size="small" danger>删除</a-button>
              </a-popconfirm>
            </div>
          </template>
        </template>
      </a-table>
      <div v-if="!providers.length && !loading" class="empty-block">
        还没有供应商。新增一个 OpenAI 兼容服务（Base URL + API Key + 模型列表）后即可使用。
      </div>
    </div>

    <a-modal v-model:open="editOpen" :title="draft.id ? '编辑供应商' : '新增供应商'" :confirm-loading="saving" width="640px" @ok="save">
      <a-form layout="vertical" :model="draft">
        <div class="form-grid">
          <a-form-item label="名称" required><a-input v-model:value="draft.name" placeholder="例如：MiMo 主池" /></a-form-item>
          <a-form-item v-if="pool === 'platform'" label="归属组织（留空为全平台）">
            <a-select v-model:value="draft.teamId" allow-clear show-search placeholder="全平台" option-filter-prop="label">
              <a-select-option v-for="team in session.teams" :key="team.id" :value="team.id" :label="team.name">{{ team.name }}</a-select-option>
            </a-select>
          </a-form-item>
          <a-form-item class="wide" label="Base URL" required>
            <a-input v-model:value="draft.baseUrl" placeholder="https://api.example.com/v1" />
          </a-form-item>
          <a-form-item class="wide" :label="draft.id ? '新 API Key（留空保持原值）' : 'API Key'">
            <a-input-password v-model:value="draft.apiKey" :placeholder="draft.id ? '留空保持' : '至少 8 位'" autocomplete="new-password" />
          </a-form-item>
          <a-form-item class="wide" label="模型列表">
            <a-select
              v-model:value="draft.models"
              mode="tags"
              placeholder="输入模型名后回车，例如 mimo-v2.6-flash"
              :token-separators="[',', ' ']"
              style="width: 100%"
            >
              <a-select-option v-for="model in modelOptions" :key="model" :value="model">{{ model }}</a-select-option>
            </a-select>
            <div class="model-actions">
              <a-button size="small" :loading="fetchingModels" @click="fetchModels">从供应商获取模型</a-button>
              <span class="muted">会使用当前填写的 Base URL 与 Key 请求 /models。</span>
            </div>
          </a-form-item>
          <a-form-item label="优先级">
            <a-input-number v-model:value="draft.priority" :min="0" :max="999" style="width: 100%" />
          </a-form-item>
          <a-form-item label="启用">
            <a-switch v-model:checked="draft.enabled" />
          </a-form-item>
        </div>
      </a-form>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue';
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons-vue';
import { api } from '@/api/client';
import type { AiProvider } from '@/api/types';
import { useSession } from '@/store/session';
import { notify } from '@/utils/notify';
import PageHeader from '@/components/PageHeader.vue';

const session = useSession();
const loading = ref(false);
const saving = ref(false);
const error = ref('');
const pool = ref<'platform' | 'team'>(session.isSuperAdmin ? 'platform' : 'team');
const providers = ref<AiProvider[]>([]);
const testingId = ref('');
const fetchingModels = ref(false);
const modelOptions = ref<string[]>([]);

const columns = computed(() => [
  { title: '供应商', key: 'name', width: 280 },
  { title: '模型', key: 'models', width: 240 },
  { title: '状态', key: 'status', width: 140 },
  { title: '优先级', key: 'priority', width: 80 },
  { title: '密钥', key: 'key', width: 80 },
  { title: '最近错误', key: 'lastError', width: 150, ellipsis: true },
  { title: '操作', key: 'actions', width: 150, fixed: 'right' as const },
]);

function basePath(): string {
  return pool.value === 'platform' ? '/platform/ai' : `/teams/${session.activeId}/ai`;
}

function isCooling(provider: any): boolean {
  return Boolean(provider.cooldownUntil && new Date(provider.cooldownUntil).getTime() > Date.now());
}

async function load(): Promise<void> {
  loading.value = true;
  try {
    providers.value = await api<AiProvider[]>('GET', `${basePath()}/providers`);
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
  name: '',
  baseUrl: '',
  apiKey: '',
  models: [] as string[],
  enabled: true,
  priority: 100,
  teamId: undefined,
});

function openCreate(): void {
  Object.assign(draft, {
    id: '',
    name: '',
    baseUrl: '',
    apiKey: '',
    models: [],
    enabled: true,
    priority: 100,
    teamId: undefined,
  });
  modelOptions.value = [];
  editOpen.value = true;
}

function openEdit(provider: any): void {
  Object.assign(draft, {
    id: provider.id,
    name: provider.name,
    baseUrl: provider.baseUrl,
    apiKey: '',
    models: [...(provider.models || [])],
    enabled: provider.enabled,
    priority: provider.priority,
    teamId: provider.teamId || undefined,
  });
  modelOptions.value = [...(provider.models || [])];
  editOpen.value = true;
}

async function save(): Promise<void> {
  if (!draft.name.trim() || !draft.baseUrl.trim()) {
    notify.error('请填写名称和 Base URL');
    return;
  }
  if (!draft.id && String(draft.apiKey).length < 8) {
    notify.error('请填写有效的 API Key');
    return;
  }
  if (!draft.models.length) {
    notify.error('至少配置一个模型');
    return;
  }
  saving.value = true;
  try {
    const body: Record<string, unknown> = {
      name: draft.name.trim(),
      baseUrl: draft.baseUrl.trim(),
      models: draft.models,
      enabled: draft.enabled,
      priority: draft.priority,
    };
    if (draft.apiKey) body.apiKey = draft.apiKey;
    if (pool.value === 'platform' && draft.teamId) body.teamId = draft.teamId;
    if (draft.id) await api('PATCH', `${basePath()}/providers/${draft.id}`, body);
    else await api('POST', `${basePath()}/providers`, body);
    editOpen.value = false;
    notify.success('供应商已保存');
    await load();
  } catch (e) {
    notify.error(e);
  } finally {
    saving.value = false;
  }
}

async function fetchModels(): Promise<void> {
  if (!draft.baseUrl.trim()) {
    notify.error('请先填写 Base URL');
    return;
  }
  fetchingModels.value = true;
  try {
    const result = await api<{ models?: string[] } | string[]>('POST', `${basePath()}/models`, {
      baseUrl: draft.baseUrl.trim(),
      apiKey: draft.apiKey || '',
      id: draft.id || '',
    });
    const list = Array.isArray(result) ? result : result.models || [];
    modelOptions.value = list;
    if (list.length) {
      draft.models = Array.from(new Set([...draft.models, ...list]));
      notify.success(`已获取 ${list.length} 个模型`);
    } else {
      notify.info('供应商没有返回模型，请手动填写');
    }
  } catch (e) {
    notify.error(e);
  } finally {
    fetchingModels.value = false;
  }
}

async function testProvider(provider: any): Promise<void> {
  testingId.value = provider.id;
  try {
    const result = await api<{ models?: string[] }>('POST', `${basePath()}/providers/${provider.id}/test`);
    notify.success(`连接正常${result?.models?.length ? `，可用模型 ${result.models.length} 个` : ''}`);
  } catch (e) {
    notify.error(e);
  } finally {
    testingId.value = '';
  }
}

async function remove(provider: any): Promise<void> {
  try {
    await api('DELETE', `${basePath()}/providers/${provider.id}`);
    notify.success('供应商已删除');
    await load();
  } catch (e) {
    notify.error(e);
  }
}

watch(pool, () => load());
watch(() => session.activeId, () => {
  if (pool.value === 'team') load();
});
onMounted(load);
</script>

<style scoped>
.name {
  font-weight: 600;
}
.url {
  font-size: 11.5px;
  word-break: break-all;
}
.models {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
.error-text {
  color: #b54708;
  font-size: 11.5px;
}
.model-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 8px;
  font-size: 11.5px;
}
</style>
