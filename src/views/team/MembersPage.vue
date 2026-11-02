<template>
  <div class="page">
    <PageHeader title="成员管理" :subtitle="`${members.length} 位成员 · ${activeCount} 位正常`">
      <template #actions>
        <a-button :loading="loading" @click="load"><reload-outlined /> 刷新</a-button>
        <a-button type="primary" @click="inviteOpen = true"><link-outlined /> 创建邀请</a-button>
      </template>
    </PageHeader>

    <a-alert v-if="error" type="error" show-icon :message="error" closable @close="error = ''" />

    <div class="panel">
      <a-tabs v-model:activeKey="tab" class="member-tabs">
        <a-tab-pane key="members" :tab="`成员 ${members.length}`">
          <div class="filters pad-x">
            <a-input-search v-model:value="search" placeholder="搜索姓名或邮箱" allow-clear style="width: 220px" />
            <a-select v-model:value="deptFilter" placeholder="全部部门" allow-clear style="width: 160px">
              <a-select-option v-for="d in departments" :key="d.id" :value="d.id">{{ d.name }}</a-select-option>
            </a-select>
            <a-select v-model:value="roleFilter" placeholder="全部角色" allow-clear style="width: 140px">
              <a-select-option v-for="r in MEMBER_ROLE" :key="r.value" :value="r.value">{{ r.label }}</a-select-option>
            </a-select>
          </div>
          <a-table
            :data-source="filteredMembers"
            :columns="memberColumns"
            :loading="loading"
            row-key="id"
            size="middle"
            :pagination="{ pageSize: 20, showSizeChanger: false }"
            :scroll="{ x: 900 }"
          >
            <template #bodyCell="{ column, record }">
              <template v-if="column.key === 'name'">
                <div class="who">
                  <UserAvatar :name="record.name" :size="28" />
                  <div>
                    <div class="m-name">{{ record.name }}<a-tag v-if="record.id === session.user?.id" :bordered="false" class="me">我</a-tag></div>
                    <div class="muted m-mail">{{ record.email }}</div>
                  </div>
                </div>
              </template>
              <template v-else-if="column.key === 'role'">
                <a-tag :color="record.role === 'team_owner' ? 'purple' : record.role === 'team_admin' ? 'blue' : 'default'">
                  {{ labelOf(MEMBER_ROLE, record.role) }}
                </a-tag>
              </template>
              <template v-else-if="column.key === 'status'">
                <a-tag :color="record.status === 'active' ? 'success' : record.status === 'suspended' ? 'warning' : 'default'">
                  {{ labelOf(MEMBER_STATUS, record.status) }}
                </a-tag>
                <a-tag v-if="record.role === 'member' && isLead(record)" color="cyan" :bordered="false">负责人</a-tag>
              </template>
              <template v-else-if="column.key === 'department'">
                <span class="muted">{{ record.departmentName || '—' }}</span>
              </template>
              <template v-else-if="column.key === 'joinedAt'">
                <span class="muted">{{ fmtDate(record.joinedAt) }}</span>
              </template>
              <template v-else-if="column.key === 'actions'">
                <div class="table-actions">
                  <a-button type="link" size="small" @click="openEdit(record)">编辑</a-button>
                  <a-button type="link" size="small" @click="openProposal(record)">变更提案</a-button>
                </div>
              </template>
            </template>
          </a-table>
        </a-tab-pane>

        <a-tab-pane key="invites" :tab="`邀请链接 ${invitations.length}`">
          <div class="pad-x invite-head">
            <span class="muted">邀请链接一次性或多次使用，可随时撤销。</span>
            <a-button size="small" @click="loadInvitations">刷新</a-button>
          </div>
          <a-table :data-source="invitations" :columns="inviteColumns" row-key="id" size="small" :pagination="false">
            <template #bodyCell="{ column, record }">
              <template v-if="column.key === 'link'">
                <a class="mono link" :href="inviteLink(record)" target="_blank" rel="noopener">{{ inviteLink(record) }}</a>
              </template>
              <template v-else-if="column.key === 'uses'">
                {{ record.usedCount ?? record.uses ?? 0 }} / {{ record.maxUses ?? '∞' }}
              </template>
              <template v-else-if="column.key === 'expires'">
                <span class="muted">{{ fmtDateTime(record.expiresAt) }}</span>
              </template>
              <template v-else-if="column.key === 'state'">
                <a-tag :color="record.revokedAt ? 'default' : isExpired(record) ? 'warning' : 'success'">
                  {{ record.revokedAt ? '已撤销' : isExpired(record) ? '已过期' : '有效' }}
                </a-tag>
              </template>
              <template v-else-if="column.key === 'actions'">
                <a-button type="link" size="small" :disabled="!!record.revokedAt" @click="copyInvite(record)">复制</a-button>
                <a-popconfirm v-if="!record.revokedAt" title="撤销后该链接立即失效，确认？" @confirm="revoke(record)">
                  <a-button type="link" size="small" danger>撤销</a-button>
                </a-popconfirm>
              </template>
            </template>
          </a-table>
          <div v-if="!invitations.length" class="empty-block">还没有邀请链接</div>
        </a-tab-pane>
      </a-tabs>
    </div>

    <!-- 编辑成员 -->
    <a-modal v-model:open="editOpen" title="编辑成员" :confirm-loading="saving" @ok="saveMember">
      <a-alert
        v-if="editTarget?.id === session.user?.id"
        type="warning"
        show-icon
        message="这是你自己的账号"
        description="修改自己的角色可能导致权限变化，请谨慎操作。"
        style="margin-bottom: 14px"
      />
      <a-form layout="vertical">
        <a-form-item label="成员">
          <a-input :value="editTarget ? `${editTarget.name} · ${editTarget.email}` : ''" disabled />
        </a-form-item>
        <a-form-item label="角色">
          <a-select v-model:value="edit.role">
            <a-select-option v-for="r in MEMBER_ROLE" :key="r.value" :value="r.value">{{ r.label }}</a-select-option>
          </a-select>
        </a-form-item>
        <a-form-item label="状态">
          <a-select v-model:value="edit.status">
            <a-select-option v-for="s in MEMBER_STATUS" :key="s.value" :value="s.value">{{ s.label }}</a-select-option>
          </a-select>
        </a-form-item>
        <a-form-item v-if="session.isCompany" label="部门">
          <a-select v-model:value="edit.departmentId" allow-clear placeholder="不指定部门">
            <a-select-option v-for="d in departments" :key="d.id" :value="d.id">{{ d.name }}</a-select-option>
          </a-select>
        </a-form-item>
      </a-form>
    </a-modal>

    <!-- 身份变更提案 -->
    <a-modal v-model:open="proposalOpen" title="成员身份变更提案" :confirm-loading="saving" @ok="submitProposal">
      <a-alert type="info" show-icon message="审批通过前，成员角色与状态不会改变。" style="margin-bottom: 14px" />
      <a-form layout="vertical">
        <a-form-item label="成员"><a-input :value="proposalTarget ? proposalTarget.name : ''" disabled /></a-form-item>
        <div class="form-grid">
          <a-form-item label="角色">
            <a-select v-model:value="proposal.role" allow-clear placeholder="保持不变">
              <a-select-option v-for="r in MEMBER_ROLE" :key="r.value" :value="r.value">{{ r.label }}</a-select-option>
            </a-select>
          </a-form-item>
          <a-form-item label="状态">
            <a-select v-model:value="proposal.status" allow-clear placeholder="保持不变">
              <a-select-option v-for="s in MEMBER_STATUS" :key="s.value" :value="s.value">{{ s.label }}</a-select-option>
            </a-select>
          </a-form-item>
        </div>
        <a-form-item label="变更原因" required><a-textarea v-model:value="proposal.reason" :rows="2" placeholder="至少 5 个字" /></a-form-item>
        <a-form-item label="影响说明" required><a-textarea v-model:value="proposal.impactSummary" :rows="2" placeholder="至少 3 个字" /></a-form-item>
      </a-form>
    </a-modal>

    <!-- 创建邀请 -->
    <a-modal v-model:open="inviteOpen" title="创建邀请链接" :confirm-loading="saving" @ok="createInvite">
      <a-form layout="vertical">
        <div class="form-grid">
          <a-form-item label="有效期（小时）">
            <a-input-number v-model:value="invite.expiresInHours" :min="1" :max="720" style="width: 100%" />
          </a-form-item>
          <a-form-item label="最多使用次数">
            <a-input-number v-model:value="invite.maxUses" :min="1" :max="100" style="width: 100%" />
          </a-form-item>
        </div>
        <p class="muted" style="margin: 0; font-size: 11.5px">默认 72 小时有效、最多 10 次。创建后可复制链接发给成员。</p>
      </a-form>
    </a-modal>

    <a-modal v-model:open="resultOpen" title="邀请链接已生成" :footer="null">
      <a-input-group compact>
        <a-input :value="generatedLink" readonly style="width: calc(100% - 76px)" />
        <a-button type="primary" @click="copyGenerated">复制</a-button>
      </a-input-group>
      <p class="muted" style="margin: 10px 0 0">链接格式：/join/&lt;token&gt;，成员打开后注册或登录即可加入。</p>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue';
import { useRoute } from 'vue-router';
import { LinkOutlined, ReloadOutlined } from '@ant-design/icons-vue';
import { api } from '@/api/client';
import type { Department, Member } from '@/api/types';
import { useSession } from '@/store/session';
import { fmtDate, fmtDateTime } from '@/utils/format';
import { labelOf, MEMBER_ROLE, MEMBER_STATUS } from '@/utils/labels';
import { notify } from '@/utils/notify';
import PageHeader from '@/components/PageHeader.vue';
import UserAvatar from '@/components/UserAvatar.vue';

const route = useRoute();
const session = useSession();
const loading = ref(false);
const saving = ref(false);
const error = ref('');
const tab = ref('members');
const members = ref<Member[]>([]);
const departments = ref<Department[]>([]);
const invitations = ref<Array<Record<string, any>>>([]);

const search = ref('');
const deptFilter = ref<string | undefined>(String(route.query.departmentId || '') || undefined);
const roleFilter = ref<string | undefined>();

const memberColumns = computed(() => [
  { title: '成员', key: 'name', width: 260 },
  { title: '角色', key: 'role', width: 110 },
  { title: '状态', key: 'status', width: 140 },
  ...(session.isCompany ? [{ title: '部门', key: 'department', width: 120 }] : []),
  { title: '加入时间', key: 'joinedAt', width: 120 },
  { title: '操作', key: 'actions', width: 150, fixed: 'right' as const },
]);

const inviteColumns = [
  { title: '链接', key: 'link', dataIndex: 'token', ellipsis: true },
  { title: '使用', key: 'uses', width: 90 },
  { title: '有效期至', key: 'expires', width: 150 },
  { title: '状态', key: 'state', width: 90 },
  { title: '操作', key: 'actions', width: 120 },
];

const activeCount = computed(() => members.value.filter((m) => m.status === 'active').length);
const filteredMembers = computed(() =>
  members.value.filter((member) => {
    if (search.value && !`${member.name}${member.email}`.toLowerCase().includes(search.value.toLowerCase())) return false;
    if (deptFilter.value && member.departmentId !== deptFilter.value) return false;
    if (roleFilter.value && member.role !== roleFilter.value) return false;
    return true;
  }),
);

function isLead(member: any): boolean {
  return departments.value.some((dept) => dept.leadUserId === member.id);
}

function inviteLink(inv: Record<string, any>): string {
  const token = String(inv.token || inv.id || '');
  return token ? `${location.origin}/join/${token}` : '';
}

function isExpired(inv: Record<string, any>): boolean {
  return inv.expiresAt ? new Date(inv.expiresAt).getTime() < Date.now() : false;
}

async function load(): Promise<void> {
  if (!session.activeId) return;
  loading.value = true;
  try {
    const teamId = session.activeId;
    const [memberList, deptPage] = await Promise.all([
      api<Member[]>('GET', `/teams/${teamId}/members`),
      api<{ items: Department[] }>('GET', `/teams/${teamId}/departments`),
    ]);
    if (teamId !== session.activeId) return;
    members.value = memberList || [];
    departments.value = deptPage.items || [];
    error.value = '';
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}

async function loadInvitations(): Promise<void> {
  if (!session.activeId) return;
  try {
    const result = await api<Array<Record<string, any>>>('GET', `/teams/${session.activeId}/invitations`);
    invitations.value = result || [];
  } catch (e) {
    notify.error(e);
  }
}

/* 编辑 */
const editOpen = ref(false);
const editTarget = ref<Member | null>(null);
const edit = reactive<Record<string, any>>({ role: 'member', status: 'active', departmentId: undefined });

function openEdit(member: any): void {
  editTarget.value = member;
  Object.assign(edit, { role: member.role, status: member.status, departmentId: member.departmentId || undefined });
  editOpen.value = true;
}

async function saveMember(): Promise<void> {
  if (!editTarget.value) return;
  saving.value = true;
  try {
    await api('PATCH', `/teams/${session.activeId}/members/${editTarget.value.id}`, {
      role: edit.role,
      status: edit.status,
      departmentId: session.isCompany ? edit.departmentId || '' : '',
    });
    editOpen.value = false;
    notify.success('成员已更新');
    await load();
  } catch (e) {
    notify.error(e);
  } finally {
    saving.value = false;
  }
}

/* 提案 */
const proposalOpen = ref(false);
const proposalTarget = ref<Member | null>(null);
const proposal = reactive<Record<string, any>>({ role: undefined, status: undefined, reason: '', impactSummary: '' });

function openProposal(member: any): void {
  proposalTarget.value = member;
  Object.assign(proposal, { role: undefined, status: undefined, reason: '', impactSummary: '' });
  proposalOpen.value = true;
}

async function submitProposal(): Promise<void> {
  if (!proposalTarget.value) return;
  if (!proposal.role && !proposal.status) {
    notify.error('至少选择一项角色或状态变更');
    return;
  }
  if (proposal.reason.trim().length < 5 || proposal.impactSummary.trim().length < 3) {
    notify.error('请填写至少 5 个字的原因和至少 3 个字的影响说明');
    return;
  }
  saving.value = true;
  try {
    await api('POST', `/teams/${session.activeId}/member-proposals`, {
      memberId: proposalTarget.value.id,
      role: proposal.role || '',
      status: proposal.status || '',
      reason: proposal.reason,
      impactSummary: proposal.impactSummary,
    });
    proposalOpen.value = false;
    notify.success('提案已提交，等待审批');
  } catch (e) {
    notify.error(e);
  } finally {
    saving.value = false;
  }
}

/* 邀请 */
const inviteOpen = ref(false);
const resultOpen = ref(false);
const generatedLink = ref('');
const invite = reactive({ expiresInHours: 72, maxUses: 10 });

async function createInvite(): Promise<void> {
  saving.value = true;
  try {
    const result = await api<{ token: string }>('POST', `/teams/${session.activeId}/invitations`, {
      expiresInHours: invite.expiresInHours,
      maxUses: invite.maxUses,
    });
    generatedLink.value = `${location.origin}/join/${result.token}`;
    inviteOpen.value = false;
    resultOpen.value = true;
    await loadInvitations();
  } catch (e) {
    notify.error(e);
  } finally {
    saving.value = false;
  }
}

async function copyGenerated(): Promise<void> {
  await navigator.clipboard.writeText(generatedLink.value);
  notify.success('链接已复制');
}

async function copyInvite(inv: Record<string, any>): Promise<void> {
  const link = inviteLink(inv);
  if (!link) return;
  await navigator.clipboard.writeText(link);
  notify.success('链接已复制');
}

async function revoke(inv: Record<string, any>): Promise<void> {
  try {
    await api('DELETE', `/teams/${session.activeId}/invitations/${inv.id}`);
    notify.success('邀请已撤销');
    await loadInvitations();
  } catch (e) {
    notify.error(e);
  }
}

watch(tab, (value) => {
  if (value === 'invites') loadInvitations();
});
watch(() => session.activeId, load);
onMounted(async () => {
  await load();
  await loadInvitations();
});
</script>

<style scoped>
.member-tabs :deep(.ant-tabs-nav) {
  padding: 0 16px;
  margin-bottom: 0;
}
.pad-x {
  padding: 14px 16px;
}
.invite-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.who {
  display: flex;
  align-items: center;
  gap: 10px;
}
.m-name {
  font-weight: 600;
}
.me {
  margin-left: 6px;
  font-size: 10px;
  line-height: 16px;
}
.m-mail {
  font-size: 11.5px;
}
.link {
  font-size: 11.5px;
  word-break: break-all;
}
</style>
