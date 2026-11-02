<template>
  <div class="page">
    <PageHeader
      :title="organizationalUnit"
      :subtitle="`${items.length} 个${unitShort} · 项目与成员按${unitShort}归属`"
    >
      <template #actions>
        <a-button :loading="loading" @click="load"><reload-outlined /> 刷新</a-button>
        <a-button type="primary" @click="openCreate"><plus-outlined /> 新建{{ unitShort }}</a-button>
      </template>
    </PageHeader>

    <a-alert v-if="error" type="error" show-icon :message="error" closable @close="error = ''" />
    <a-alert
      v-if="!session.isCompany"
      type="info"
      show-icon
      message="当前是团队版：部门以“小组”呈现，用于轻量归属与负责人授权。"
    />

    <div class="panel">
      <a-table :data-source="items" :columns="columns" :loading="loading" row-key="id" size="middle" :pagination="false">
        <template #bodyCell="{ column, record }">
          <template v-if="column.key === 'name'">
            <div class="name">{{ record.name }}</div>
            <div class="muted desc">{{ record.description || '暂无描述' }}</div>
          </template>
          <template v-else-if="column.key === 'lead'">
            <span v-if="record.leadName" class="who"><UserAvatar :name="record.leadName" :size="20" />{{ record.leadName }}</span>
            <span v-else class="muted">未指定</span>
          </template>
          <template v-else-if="column.key === 'members'">
            <a @click="router.push({ path: '/teammind/members', query: { departmentId: record.id } })">{{ record.memberCount }} 人</a>
          </template>
          <template v-else-if="column.key === 'projects'">
            <a @click="router.push({ path: '/teammind/projects', query: { departmentId: record.id } })">{{ record.projectCount }} 个</a>
          </template>
          <template v-else-if="column.key === 'actions'">
            <div class="table-actions">
              <a-button type="link" size="small" @click="openEdit(record)">编辑</a-button>
              <a-popconfirm title="确认删除？删除后成员与项目的归属会保留但不再关联该部门。" ok-text="删除" @confirm="remove(record)">
                <a-button type="link" size="small" danger>删除</a-button>
              </a-popconfirm>
            </div>
          </template>
        </template>
      </a-table>
      <div v-if="!items.length && !loading" class="empty-block">还没有{{ unitShort }}，点击右上角创建一个。</div>
    </div>

    <a-modal v-model:open="editOpen" :title="draft.id ? `编辑${unitShort}` : `新建${unitShort}`" :confirm-loading="saving" @ok="save">
      <a-form layout="vertical" :model="draft">
        <a-form-item :label="`${unitShort}名称`" required><a-input v-model:value="draft.name" :maxlength="80" /></a-form-item>
        <a-form-item label="描述"><a-textarea v-model:value="draft.description" :rows="2" :maxlength="2000" /></a-form-item>
        <a-form-item label="负责人">
          <a-select v-model:value="draft.leadUserId" allow-clear placeholder="选择成员" show-search option-filter-prop="label">
            <a-select-option v-for="m in members" :key="m.id" :value="m.id" :label="m.name">{{ m.name }} · {{ m.email }}</a-select-option>
          </a-select>
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
import type { Department, Member } from '@/api/types';
import { useSession } from '@/store/session';
import { notify } from '@/utils/notify';
import PageHeader from '@/components/PageHeader.vue';
import UserAvatar from '@/components/UserAvatar.vue';

const router = useRouter();
const session = useSession();
const loading = ref(false);
const saving = ref(false);
const error = ref('');
const items = ref<Department[]>([]);
const members = ref<Member[]>([]);

const unitShort = computed(() => (session.isCompany ? '部门' : '小组'));
const organizationalUnit = computed(() => (session.isCompany ? '部门管理' : '小组管理'));

const columns = computed(() => [
  { title: unitShort.value, key: 'name', width: 300 },
  { title: '负责人', key: 'lead', width: 160 },
  { title: '成员', key: 'members', width: 100 },
  { title: '项目', key: 'projects', width: 100 },
  { title: '操作', key: 'actions', width: 130 },
]);

async function load(): Promise<void> {
  if (!session.activeId) return;
  loading.value = true;
  try {
    const teamId = session.activeId;
    const [deptPage, memberList] = await Promise.all([
      api<{ items: Department[] }>('GET', `/teams/${teamId}/departments`),
      api<Member[]>('GET', `/teams/${teamId}/members`),
    ]);
    if (teamId !== session.activeId) return;
    items.value = deptPage.items || [];
    members.value = memberList || [];
    error.value = '';
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}

const editOpen = ref(false);
const draft = reactive<Record<string, any>>({ id: '', name: '', description: '', leadUserId: undefined });

function openCreate(): void {
  Object.assign(draft, { id: '', name: '', description: '', leadUserId: undefined });
  editOpen.value = true;
}

function openEdit(item: any): void {
  Object.assign(draft, {
    id: item.id,
    name: item.name,
    description: item.description,
    leadUserId: item.leadUserId || undefined,
  });
  editOpen.value = true;
}

async function save(): Promise<void> {
  if (!draft.name.trim()) {
    notify.error(`请填写${unitShort.value}名称`);
    return;
  }
  saving.value = true;
  try {
    const body = {
      name: draft.name.trim(),
      description: draft.description || '',
      leadUserId: draft.leadUserId || '',
    };
    if (draft.id) await api('PATCH', `/teams/${session.activeId}/departments/${draft.id}`, body);
    else await api('POST', `/teams/${session.activeId}/departments`, body);
    editOpen.value = false;
    notify.success('已保存');
    await load();
  } catch (e) {
    notify.error(e);
  } finally {
    saving.value = false;
  }
}

async function remove(item: any): Promise<void> {
  try {
    await api('DELETE', `/teams/${session.activeId}/departments/${item.id}`);
    notify.success('已删除');
    await load();
  } catch (e) {
    notify.error(e);
  }
}

watch(() => session.activeId, load);
onMounted(load);
</script>

<style scoped>
.name {
  font-weight: 600;
}
.desc {
  margin-top: 2px;
  font-size: 11.5px;
}
.who {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
</style>
