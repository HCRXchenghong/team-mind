<template>
  <div class="page">
    <PageHeader title="平台控制台" subtitle="组织审核、平台审计、安全告警与运行配置（仅平台超管）">
      <template #actions>
        <a-button :loading="loading" @click="loadAll"><reload-outlined /> 刷新</a-button>
      </template>
    </PageHeader>

    <a-alert v-if="error" type="error" show-icon :message="error" closable @close="error = ''" />

    <div class="stats">
      <div class="stat panel">
        <div class="label">组织总数</div>
        <div class="value">{{ overview.teams?.total || 0 }}</div>
        <div class="hint">已接入 TeamMind 的组织</div>
      </div>
      <div class="stat panel">
        <div class="label">待审核</div>
        <div class="value warn">{{ overview.teams?.pending || 0 }}</div>
        <div class="hint">需要平台确认后使用</div>
      </div>
      <div class="stat panel">
        <div class="label">活跃组织</div>
        <div class="value ok">{{ overview.teams?.active || 0 }}</div>
        <div class="hint">状态为 active</div>
      </div>
      <div class="stat panel">
        <div class="label">安全告警</div>
        <div class="value danger">{{ overview.alerts?.active || 0 }}</div>
        <div class="hint">异常登录 / 限流封禁</div>
      </div>
    </div>

    <div class="panel platform-panel">
      <a-tabs v-model:activeKey="tab" class="platform-tabs">
        <a-tab-pane key="teams" tab="组织审核">
          <div class="filters pad">
            <a-input-search v-model:value="teamFilter.q" placeholder="搜索组织名称" allow-clear style="width: 220px" @search="loadTeams" />
            <a-select v-model:value="teamFilter.status" placeholder="全部状态" allow-clear style="width: 140px" @change="loadTeams">
              <a-select-option value="pending_review">待审核</a-select-option>
              <a-select-option value="active">已激活</a-select-option>
              <a-select-option value="suspended">已停用</a-select-option>
            </a-select>
            <a-select v-model:value="teamFilter.kind" placeholder="全部类型" allow-clear style="width: 130px" @change="loadTeams">
              <a-select-option value="team">团队</a-select-option>
              <a-select-option value="company">公司</a-select-option>
            </a-select>
          </div>
          <a-table
            :data-source="teams"
            :columns="teamColumns"
            :loading="teamsLoading"
            row-key="id"
            size="middle"
            :scroll="{ x: 980 }"
            :pagination="{
              current: teamPage,
              pageSize: 20,
              total: teamTotal,
              showSizeChanger: false,
              showTotal: (value: number) => `共 ${value} 条`,
            }"
            @change="onTeamPage"
          >
            <template #bodyCell="{ column, record }">
              <template v-if="column.key === 'name'">
                <div class="t-name">{{ record.name }}</div>
                <div class="muted t-sub">{{ record.kind === 'company' ? '公司' : '团队' }} · {{ record.id.slice(0, 8) }}</div>
              </template>
              <template v-else-if="column.key === 'kind'">
                <a-tag :color="record.kind === 'company' ? 'purple' : 'blue'" :bordered="false">{{ record.kind === 'company' ? '公司' : '团队' }}</a-tag>
              </template>
              <template v-else-if="column.key === 'status'">
                <a-tag :color="statusColor(record.status)">{{ statusLabel(record.status) }}</a-tag>
              </template>
              <template v-else-if="column.key === 'counts'">
                <span class="muted">{{ record.memberCount ?? 0 }} 成员 · {{ record.projectCount ?? 0 }} 项目</span>
              </template>
              <template v-else-if="column.key === 'createdAt'">
                <span class="muted">{{ fmtDateTime(record.createdAt) }}</span>
              </template>
              <template v-else-if="column.key === 'actions'">
                <div class="table-actions">
                  <a-button v-if="record.status === 'pending_review'" type="link" size="small" @click="openDecision(record, 'approved')">通过</a-button>
                  <a-button v-if="record.status === 'pending_review'" type="link" size="small" danger @click="openDecision(record, 'rejected')">驳回</a-button>
                  <span v-if="record.status !== 'pending_review'" class="muted" style="font-size: 11.5px">已处理</span>
                </div>
              </template>
            </template>
          </a-table>
        </a-tab-pane>

        <a-tab-pane key="audit" tab="平台审计">
          <div class="pad">
            <a-table
              :data-source="audit"
              :columns="[
                { title: '组织', dataIndex: 'teamName', width: 180 },
                { title: '操作', dataIndex: 'action', width: 220 },
                { title: '原因 / 摘要', dataIndex: 'reason' },
                { title: '时间', dataIndex: 'createdAt', width: 170 },
              ]"
              size="small"
              row-key="id"
              :pagination="{ pageSize: 12, showSizeChanger: false }"
            >
              <template #bodyCell="{ column, record }">
                <template v-if="column.dataIndex === 'createdAt'">
                  <span class="muted">{{ fmtDateTime(record.createdAt) }}</span>
                </template>
                <template v-else-if="column.dataIndex === 'reason'">
                  <span class="muted">{{ record.reason || record.afterSummary || '—' }}</span>
                </template>
              </template>
            </a-table>
          </div>
        </a-tab-pane>

        <a-tab-pane key="alerts" tab="安全告警">
          <div class="pad">
            <a-table
              :data-source="alerts"
              :columns="[
                { title: '来源', dataIndex: 'sourceRef' },
                { title: '请求次数', dataIndex: 'requestCount', width: 120 },
                { title: '封禁至', dataIndex: 'blockedUntil', width: 190 },
              ]"
              size="small"
              row-key="sourceRef"
              :pagination="false"
            >
              <template #bodyCell="{ column, record }">
                <template v-if="column.dataIndex === 'blockedUntil'">
                  <span class="muted">{{ fmtDateTime(record.blockedUntil) }}</span>
                </template>
              </template>
            </a-table>
            <div v-if="!alerts.length" class="empty-block small">当前没有安全告警</div>
          </div>
        </a-tab-pane>

        <a-tab-pane key="companyInvites" tab="公司邀请">
          <div class="invite-head pad">
            <div>
              <div class="invite-title">用邀请码创建公司</div>
              <div class="muted invite-sub">
                关闭公开注册后，只有持有完整公司邀请链接的人才能创建公司并成为负责人；员工仍由公司/团队的成员邀请链接加入。
              </div>
            </div>
            <a-button type="primary" @click="openCompanyInvite"><plus-outlined /> 生成公司邀请</a-button>
          </div>
          <a-table
            :data-source="companyInvites"
            :columns="companyInviteColumns"
            :loading="companyInvitesLoading"
            row-key="id"
            size="middle"
            :pagination="false"
          >
            <template #bodyCell="{ column, record }">
              <template v-if="column.key === 'status'">
                <a-tag :color="companyInviteColor(record.status)" :bordered="false">{{ companyInviteLabel(record.status) }}</a-tag>
              </template>
              <template v-else-if="column.key === 'uses'">
                <span>{{ record.usedCount }} / {{ record.maxUses }}</span>
              </template>
              <template v-else-if="column.key === 'expiresAt'">
                <span class="muted">{{ fmtDateTime(record.expiresAt) }}</span>
              </template>
              <template v-else-if="column.key === 'note'">
                <span class="muted">{{ record.note || '—' }}</span>
              </template>
              <template v-else-if="column.key === 'createdAt'">
                <span class="muted">{{ fmtDateTime(record.createdAt) }}</span>
              </template>
              <template v-else-if="column.key === 'actions'">
                <a-popconfirm
                  v-if="record.status === 'active'"
                  title="撤销后该链接立即失效，确定？"
                  @confirm="revokeCompanyInvite(record)"
                >
                  <a-button size="small" type="link" danger>撤销</a-button>
                </a-popconfirm>
                <span v-else class="muted" style="font-size: 11.5px">—</span>
              </template>
            </template>
          </a-table>
          <div class="muted invite-foot">邀请码仅创建时展示一次；丢失后请重新生成，不会影响已创建的公司。</div>
        </a-tab-pane>

        <a-tab-pane key="settings" tab="运行配置">
          <div class="pad config">
            <a-alert type="info" show-icon message="密钥类字段只写不读：留空表示保持现有值。" style="margin-bottom: 16px" />
            <a-form layout="vertical" :model="settings">
              <div class="config-grid">
                <div class="config-block">
                  <h4>注册与站点</h4>
                  <a-form-item label="开放注册">
                    <a-switch v-model:checked="settings.registrationEnabled" />
                    <div class="muted config-hint">关闭后公开注册页显示“已关闭”，但公司邀请链接和团队邀请链接仍可使用。</div>
                  </a-form-item>
                  <a-form-item label="公开 URL">
                    <a-input v-model:value="settings.publicUrl" placeholder="https://team.example.com" />
                  </a-form-item>
                </div>

                <div class="config-block">
                  <h4>SMTP 邮件</h4>
                  <a-form-item label="主机"><a-input v-model:value="settings.smtpHost" /></a-form-item>
                  <a-form-item label="端口"><a-input v-model:value="settings.smtpPort" /></a-form-item>
                  <a-form-item label="用户"><a-input v-model:value="settings.smtpUser" /></a-form-item>
                  <a-form-item label="发件人"><a-input v-model:value="settings.smtpFrom" /></a-form-item>
                  <a-form-item :label="`SMTP 密码${settings.smtpPasswordSet ? '（已配置，留空保持）' : ''}`">
                    <a-input-password v-model:value="smtpPassword" />
                  </a-form-item>
                  <a-form-item label="跳过 TLS 校验">
                    <a-switch v-model:checked="settings.smtpTlsInsecure" />
                  </a-form-item>
                </div>

                <div class="config-block">
                  <h4>语音 STT</h4>
                  <a-form-item label="服务">
                    <a-select v-model:value="settings.sttProvider" allow-clear placeholder="关闭">
                      <a-select-option value="mimo">MiMo</a-select-option>
                      <a-select-option value="openai">OpenAI</a-select-option>
                    </a-select>
                  </a-form-item>
                  <a-form-item label="Base URL"><a-input v-model:value="settings.sttBaseUrl" /></a-form-item>
                  <a-form-item label="模型"><a-input v-model:value="settings.sttModel" /></a-form-item>
                  <a-form-item label="语言"><a-input v-model:value="settings.sttLanguage" placeholder="zh" /></a-form-item>
                  <a-form-item :label="`API Key${settings.sttApiKeySet ? '（已配置，留空保持）' : ''}`">
                    <a-input-password v-model:value="sttApiKey" />
                  </a-form-item>
                </div>

                <div class="config-block">
                  <h4>推送与微信</h4>
                  <a-form-item label="VAPID Subject">
                    <a-input v-model:value="settings.vapidSubject" placeholder="mailto:ops@example.com" />
                  </a-form-item>
                  <a-form-item label="微信模式">
                    <a-select v-model:value="settings.wechatMode">
                      <a-select-option value="unconfigured">关闭</a-select-option>
                      <a-select-option value="real">真实服务</a-select-option>
                    </a-select>
                  </a-form-item>
                  <a-form-item label="Gateway URL"><a-input v-model:value="settings.wechatGatewayUrl" /></a-form-item>
                  <a-form-item :label="`Bridge Token${settings.wechatTokenSet ? '（已配置，留空保持）' : ''}`">
                    <a-input-password v-model:value="wechatBridgeToken" />
                  </a-form-item>
                </div>
              </div>

              <a-button type="primary" :loading="saving" @click="saveSettings">保存配置</a-button>
            </a-form>
          </div>
        </a-tab-pane>
      </a-tabs>
    </div>

    <a-modal v-model:open="decisionOpen" :title="decisionType === 'approved' ? '审核通过' : '驳回组织'" :confirm-loading="saving" @ok="submitDecision">
      <a-form layout="vertical">
        <a-form-item label="组织"><a-input :value="decisionTeam?.name" disabled /></a-form-item>
        <a-form-item label="原因 / 备注">
          <a-textarea v-model:value="decisionReason" :rows="3" :placeholder="decisionType === 'rejected' ? '请说明驳回原因' : '可填写备注'" />
        </a-form-item>
      </a-form>
    </a-modal>

    <a-modal
      v-model:open="companyInviteOpen"
      title="生成公司邀请链接"
      :confirm-loading="inviteSaving"
      ok-text="生成链接"
      width="520px"
      @ok="createCompanyInvite"
    >
      <a-form layout="vertical">
        <a-form-item label="有效期（小时）">
          <a-input-number v-model:value="inviteExpires" :min="1" :max="720" style="width: 100%" />
        </a-form-item>
        <a-form-item label="可用次数">
          <a-input-number v-model:value="inviteMaxUses" :min="1" :max="100" style="width: 100%" />
          <div class="muted invite-field-hint">1 次表示只能创建 1 个公司；每次创建都会消耗一次。</div>
        </a-form-item>
        <a-form-item label="备注（可选）">
          <a-input v-model:value="inviteNote" :maxlength="160" placeholder="例如：星河科技 / 渠道伙伴" />
        </a-form-item>
      </a-form>
      <a-alert
        type="info"
        show-icon
        message="对方会先创建公司负责人账号，创建后自动加入并登录。"
      />
    </a-modal>

    <a-modal v-model:open="companyInviteGeneratedOpen" title="公司邀请链接已生成" :footer="null" width="640px">
      <a-alert
        type="warning"
        show-icon
        message="请立即复制完整链接"
        description="邀请码仅显示这一次，关闭后无法再次查看；链接过期、用尽或撤销后失效。"
        style="margin-bottom: 14px"
      />
      <a-input :value="companyInviteLink" readonly style="margin-bottom: 10px" />
      <a-button type="primary" block @click="copyCompanyInvite(companyInviteLink)">复制完整邀请链接</a-button>
      <div class="muted invite-generated-meta">
        有效期至 {{ generatedInvite ? fmtDateTime(generatedInvite.expiresAt) : '—' }} · 可用 {{ generatedInvite?.maxUses || 0 }} 次
      </div>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue';
import { PlusOutlined, ReloadOutlined } from '@ant-design/icons-vue';
import { api, cleanParams } from '@/api/client';
import { fmtDateTime } from '@/utils/format';
import { notify } from '@/utils/notify';
import PageHeader from '@/components/PageHeader.vue';

interface TeamRow {
  id: string;
  name: string;
  kind: string;
  status: string;
  ownerName?: string;
  memberCount?: number;
  projectCount?: number;
  createdAt?: string;
}

const loading = ref(false);
const saving = ref(false);
const error = ref('');
const tab = ref('teams');
const overview = ref<Record<string, any>>({ teams: {}, alerts: {} });
const teams = ref<TeamRow[]>([]);
const teamTotal = ref(0);
const teamPage = ref(1);
const teamsLoading = ref(false);
const teamFilter = reactive({ q: '', status: undefined as string | undefined, kind: undefined as string | undefined });
const audit = ref<Array<Record<string, any>>>([]);
const alerts = ref<Array<Record<string, any>>>([]);
const settings = reactive<Record<string, any>>({});
const smtpPassword = ref('');
const sttApiKey = ref('');
const wechatBridgeToken = ref('');

const companyInvites = ref<Array<Record<string, any>>>([]);
const companyInvitesLoading = ref(false);
const companyInviteOpen = ref(false);
const companyInviteGeneratedOpen = ref(false);
const inviteSaving = ref(false);
const companyInviteLink = ref('');
const generatedInvite = ref<Record<string, any> | null>(null);
const inviteExpires = ref(168);
const inviteMaxUses = ref(1);
const inviteNote = ref('');
const companyInviteColumns = [
  { title: '状态', key: 'status', width: 100 },
  { title: '使用情况', key: 'uses', width: 110 },
  { title: '过期时间', key: 'expiresAt', width: 170 },
  { title: '备注', key: 'note' },
  { title: '创建时间', key: 'createdAt', width: 170 },
  { title: '操作', key: 'actions', width: 90 },
];

const teamColumns = [
  { title: '组织', key: 'name', width: 240 },
  { title: '类型', key: 'kind', width: 90 },
  { title: '状态', key: 'status', width: 110 },
  { title: '创建人', key: 'owner', dataIndex: 'ownerName', width: 130 },
  { title: '规模', key: 'counts', width: 170 },
  { title: '创建时间', key: 'createdAt', width: 160 },
  { title: '操作', key: 'actions', width: 130, fixed: 'right' as const },
];

function statusLabel(status: string): string {
  return { active: '已激活', pending_review: '待审核', suspended: '已停用' }[status] || status;
}
function statusColor(status: string): string {
  return { active: 'success', pending_review: 'processing', suspended: 'default' }[status] || 'default';
}

async function loadOverview(): Promise<void> {
  try {
    overview.value = await api<Record<string, any>>('GET', '/platform/overview');
  } catch (e) {
    error.value = (e as Error).message;
  }
}

async function loadTeams(): Promise<void> {
  teamsLoading.value = true;
  try {
    const result = await api<{ items: TeamRow[]; total: number }>(
      'GET',
      '/platform/teams',
      undefined,
      cleanParams({ ...teamFilter, q: teamFilter.q.trim(), page: teamPage.value, pageSize: 20 }),
    );
    teams.value = result.items || [];
    teamTotal.value = result.total || 0;
  } catch (e) {
    notify.error(e);
  } finally {
    teamsLoading.value = false;
  }
}

function onTeamPage(pagination: { current?: number }): void {
  teamPage.value = pagination.current || 1;
  loadTeams();
}

async function loadAudit(): Promise<void> {
  try {
    const result = await api<{ items: Array<Record<string, any>> }>('GET', '/platform/audit-log', undefined, {
      page: 1,
      pageSize: 100,
    });
    audit.value = result.items || [];
  } catch {
    audit.value = [];
  }
}

async function loadAlerts(): Promise<void> {
  try {
    alerts.value = await api<Array<Record<string, any>>>('GET', '/platform/security-alerts');
  } catch {
    alerts.value = [];
  }
}

async function loadSettings(): Promise<void> {
  try {
    Object.assign(settings, await api<Record<string, any>>('GET', '/platform/settings'));
  } catch (e) {
    notify.error(e);
  }
}

function companyInviteLabel(status: string): string {
  return { active: '可邀请', used: '已用尽', expired: '已过期', revoked: '已撤销' }[status] || status;
}
function companyInviteColor(status: string): string {
  return { active: 'success', used: 'default', expired: 'default', revoked: 'error' }[status] || 'default';
}

async function loadCompanyInvites(): Promise<void> {
  companyInvitesLoading.value = true;
  try {
    companyInvites.value = await api<Array<Record<string, any>>>('GET', '/platform/company-invites');
  } catch (e) {
    notify.error(e);
  } finally {
    companyInvitesLoading.value = false;
  }
}

function openCompanyInvite(): void {
  inviteExpires.value = 168;
  inviteMaxUses.value = 1;
  inviteNote.value = '';
  companyInviteOpen.value = true;
}

async function createCompanyInvite(): Promise<void> {
  inviteSaving.value = true;
  try {
    const result = await api<Record<string, any>>('POST', '/platform/company-invites', {
      expiresInHours: inviteExpires.value,
      maxUses: inviteMaxUses.value,
      note: inviteNote.value,
    });
    companyInviteOpen.value = false;
    generatedInvite.value = result;
    companyInviteLink.value = `${window.location.origin}${result.path}`;
    companyInviteGeneratedOpen.value = true;
    await loadCompanyInvites();
  } catch (e) {
    notify.error(e);
  } finally {
    inviteSaving.value = false;
  }
}

async function revokeCompanyInvite(row: Record<string, any>): Promise<void> {
  try {
    await api('DELETE', `/platform/company-invites/${row.id}`);
    notify.success('邀请已撤销');
    await loadCompanyInvites();
  } catch (e) {
    notify.error(e);
  }
}

async function copyCompanyInvite(text: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(text);
    notify.success('完整邀请链接已复制');
  } catch {
    notify.error('复制失败，请手动选择链接复制');
  }
}

async function loadAll(): Promise<void> {
  loading.value = true;
  await Promise.all([loadOverview(), loadTeams(), loadAudit(), loadAlerts(), loadSettings(), loadCompanyInvites()]);
  loading.value = false;
}

const decisionOpen = ref(false);
const decisionType = ref<'approved' | 'rejected'>('approved');
const decisionTeam = ref<TeamRow | null>(null);
const decisionReason = ref('');

function openDecision(team: any, type: 'approved' | 'rejected'): void {
  decisionTeam.value = team;
  decisionType.value = type;
  decisionReason.value = '';
  decisionOpen.value = true;
}

async function submitDecision(): Promise<void> {
  if (!decisionTeam.value) return;
  if (decisionType.value === 'rejected' && decisionReason.value.trim().length < 5) {
    notify.error('请填写至少 5 个字的驳回原因');
    return;
  }
  saving.value = true;
  try {
    await api('POST', `/platform/teams/${decisionTeam.value.id}/decision`, {
      decision: decisionType.value,
      reason: decisionReason.value,
    });
    decisionOpen.value = false;
    notify.success('审核已提交');
    await Promise.all([loadOverview(), loadTeams(), loadAudit()]);
  } catch (e) {
    notify.error(e);
  } finally {
    saving.value = false;
  }
}

async function saveSettings(): Promise<void> {
  saving.value = true;
  try {
    const body: Record<string, unknown> = {};
    [
      'registrationEnabled',
      'publicUrl',
      'smtpHost',
      'smtpPort',
      'smtpUser',
      'smtpFrom',
      'smtpTlsInsecure',
      'sttProvider',
      'sttBaseUrl',
      'sttModel',
      'sttLanguage',
      'vapidSubject',
      'wechatMode',
      'wechatGatewayUrl',
    ].forEach((key) => {
      if (settings[key] !== undefined) body[key] = settings[key];
    });
    if (smtpPassword.value) body.smtpPassword = smtpPassword.value;
    if (sttApiKey.value) body.sttApiKey = sttApiKey.value;
    if (wechatBridgeToken.value) body.wechatBridgeToken = wechatBridgeToken.value;
    await api('PATCH', '/platform/settings', body);
    smtpPassword.value = '';
    sttApiKey.value = '';
    wechatBridgeToken.value = '';
    notify.success('配置已保存');
    await loadSettings();
  } catch (e) {
    notify.error(e);
  } finally {
    saving.value = false;
  }
}

onMounted(loadAll);
</script>

<style scoped>
.stats {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}
.stat {
  padding: 16px 18px;
}
.label {
  color: var(--tm-muted);
  font-size: 12px;
}
.value {
  margin-top: 6px;
  font-size: 26px;
  font-weight: 650;
  letter-spacing: -0.03em;
  font-variant-numeric: tabular-nums;
}
.value.warn {
  color: #b54708;
}
.value.ok {
  color: #027a48;
}
.value.danger {
  color: #d92d20;
}
.hint {
  margin-top: 2px;
  color: var(--tm-muted);
  font-size: 11px;
}
.platform-panel {
  padding: 0 16px 12px;
}
.config-hint {
  margin-top: 6px;
  font-size: 11px;
  line-height: 1.6;
}
.invite-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  padding: 8px 0 14px;
}
.invite-title {
  font-size: 13.5px;
  font-weight: 600;
  margin-bottom: 3px;
}
.invite-sub {
  max-width: 760px;
  font-size: 11.5px;
  line-height: 1.65;
}
.invite-foot {
  padding: 10px 2px 2px;
  font-size: 11px;
}
.invite-field-hint,
.invite-generated-meta {
  margin-top: 4px;
  font-size: 11px;
}
.invite-generated-meta {
  margin-top: 10px;
  text-align: center;
}
.platform-tabs :deep(.ant-tabs-nav) {
  margin-bottom: 0;
}
.pad {
  padding: 14px 4px;
}
.t-name {
  font-weight: 600;
}
.t-sub {
  font-size: 11px;
}
.config {
  max-width: 1100px;
}
.config-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 32px;
}
.config-block h4 {
  margin: 8px 0 12px;
  font-size: 13px;
}
.empty-block.small {
  padding: 18px;
  font-size: 12px;
}
@media (max-width: 900px) {
  .stats {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .config-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
