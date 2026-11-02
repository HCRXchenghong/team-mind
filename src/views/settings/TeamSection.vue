<template>
  <div class="section">
    <a-empty v-if="!session.activeTeam" description="当前没有组织" />
    <template v-else>
      <a-alert v-if="!session.isAdmin" type="info" show-icon message="只有组织管理员可以修改组织设置与权限策略。" style="margin-bottom: 16px" />

      <a-row :gutter="24">
        <a-col :xs="24" :md="12">
          <div class="block">
            <h3>组织信息</h3>
            <a-form layout="vertical" :model="team" :disabled="!session.isAdmin">
              <a-form-item label="组织名称"><a-input v-model:value="team.name" :maxlength="160" /></a-form-item>
              <a-form-item label="描述"><a-textarea v-model:value="team.description" :rows="3" :maxlength="2000" /></a-form-item>
              <a-form-item label="时区"><a-input v-model:value="team.timezone" placeholder="Asia/Shanghai" /></a-form-item>
              <a-button type="primary" :disabled="!session.isAdmin" :loading="saving" @click="saveTeam">保存组织</a-button>
            </a-form>
          </div>
        </a-col>

        <a-col :xs="24" :md="12">
          <div class="block">
            <h3>组织类型</h3>
            <div class="kind-box">
              <div>
                <div class="kind-name">{{ session.isCompany ? '公司版' : '团队版' }}</div>
                <div class="muted kind-desc">
                  {{ session.isCompany ? '启用部门、部门负责人与部门级数据隔离。' : '单层小组，不启用部门；适合 20 人左右团队。' }}
                </div>
              </div>
              <a-button v-if="session.isAdmin" size="small" ghost @click="convertOpen = true">
                转换为{{ session.isCompany ? '团队版' : '公司版' }}
              </a-button>
            </div>
            <p class="muted warn">转换会影响部门数据的展示与权限范围，请确认后再操作。</p>
          </div>

          <div class="block">
            <h3>权限隔离</h3>
            <a-form layout="vertical" :model="policy" :disabled="!session.isAdmin">
              <a-form-item label="AI 使用范围">
                <a-select v-model:value="policy.aiAccess">
                  <a-select-option value="all">所有成员</a-select-option>
                  <a-select-option value="leads">负责人及以上</a-select-option>
                  <a-select-option value="admins">仅管理员</a-select-option>
                </a-select>
              </a-form-item>
              <a-form-item label="微信渠道使用范围">
                <a-select v-model:value="policy.wechatAccess">
                  <a-select-option value="all">所有成员</a-select-option>
                  <a-select-option value="leads">负责人及以上</a-select-option>
                  <a-select-option value="admins">仅管理员</a-select-option>
                </a-select>
              </a-form-item>
              <a-form-item label="数据隔离">
                <a-select v-model:value="policy.dataIsolation">
                  <a-select-option value="shared">全组织共享</a-select-option>
                  <a-select-option value="units">按部门 / 小组隔离</a-select-option>
                </a-select>
              </a-form-item>
              <a-button type="primary" ghost :disabled="!session.isAdmin" :loading="policySaving" @click="savePolicy">保存策略</a-button>
            </a-form>
          </div>
        </a-col>
      </a-row>
    </template>

    <a-modal v-model:open="convertOpen" title="转换组织类型" :confirm-loading="saving" @ok="convertKind">
      <a-alert
        type="warning"
        show-icon
        :message="`确认把组织转换为${session.isCompany ? '团队版' : '公司版'}？`"
        description="现有部门数据会保留，但展示方式与权限范围会改变；此操作需要重新加载组织信息。"
      />
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue';
import { api } from '@/api/client';
import { useSession } from '@/store/session';
import { notify } from '@/utils/notify';

const session = useSession();
const saving = ref(false);
const policySaving = ref(false);
const convertOpen = ref(false);
const team = reactive({ name: '', description: '', timezone: 'Asia/Shanghai' });
const policy = reactive({ aiAccess: 'all', wechatAccess: 'all', dataIsolation: 'shared' });

async function loadTeam(): Promise<void> {
  if (!session.activeId) return;
  try {
    const result = await api<{ name: string; description: string; timezone: string }>('GET', `/teams/${session.activeId}`);
    Object.assign(team, {
      name: result.name || '',
      description: result.description || '',
      timezone: result.timezone || 'Asia/Shanghai',
    });
  } catch (e) {
    notify.error(e);
  }
}

async function loadPolicy(): Promise<void> {
  if (!session.activeId) return;
  try {
    const result = await api<{ aiAccess: string; wechatAccess: string; dataIsolation: string }>(
      'GET',
      `/teams/${session.activeId}/access-policy`,
    );
    Object.assign(policy, {
      aiAccess: result.aiAccess || 'all',
      wechatAccess: result.wechatAccess || 'all',
      dataIsolation: result.dataIsolation || 'shared',
    });
  } catch {
    /* 非管理员也可能可读；失败保持默认 */
  }
}

async function saveTeam(): Promise<void> {
  if (!team.name.trim()) {
    notify.error('请输入组织名称');
    return;
  }
  saving.value = true;
  try {
    await api('PATCH', `/teams/${session.activeId}`, {
      name: team.name.trim(),
      description: team.description,
      timezone: team.timezone.trim(),
    });
    await session.load();
    notify.success('组织信息已保存');
  } catch (e) {
    notify.error(e);
  } finally {
    saving.value = false;
  }
}

async function savePolicy(): Promise<void> {
  policySaving.value = true;
  try {
    await api('PATCH', `/teams/${session.activeId}/access-policy`, { ...policy });
    notify.success('权限策略已保存');
  } catch (e) {
    notify.error(e);
  } finally {
    policySaving.value = false;
  }
}

async function convertKind(): Promise<void> {
  saving.value = true;
  try {
    await api('PATCH', `/teams/${session.activeId}/kind`, { kind: session.isCompany ? 'team' : 'company' });
    convertOpen.value = false;
    await session.load();
    notify.success('组织类型已转换');
    await loadTeam();
  } catch (e) {
    notify.error(e);
  } finally {
    saving.value = false;
  }
}

onMounted(async () => {
  await Promise.all([loadTeam(), loadPolicy()]);
});
</script>

<style scoped>
.block {
  margin-bottom: 26px;
  max-width: 560px;
}
h3 {
  margin: 0 0 10px;
  font-size: 14px;
}
.kind-box {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 14px;
  border: 1px solid var(--tm-border);
  border-radius: 8px;
}
.kind-name {
  font-weight: 600;
  font-size: 13px;
}
.kind-desc {
  margin-top: 2px;
  font-size: 11.5px;
  max-width: 340px;
}
.warn {
  margin-top: 8px;
  font-size: 11px;
}
</style>
