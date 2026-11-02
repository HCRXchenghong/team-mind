<template>
  <div class="section">
    <a-row :gutter="24">
      <a-col :xs="24" :md="12">
        <div class="block">
          <h3>修改密码</h3>
          <p class="muted desc">修改成功后所有会话会失效，需要重新登录。</p>
          <a-form layout="vertical" :model="password">
            <a-form-item label="当前密码"><a-input-password v-model:value="password.current" autocomplete="current-password" /></a-form-item>
            <a-form-item label="新密码">
              <a-input-password v-model:value="password.next" placeholder="至少 12 位" autocomplete="new-password" />
            </a-form-item>
            <a-form-item label="确认新密码"><a-input-password v-model:value="password.confirm" autocomplete="new-password" /></a-form-item>
            <a-button type="primary" :loading="saving" @click="changePassword">修改密码</a-button>
          </a-form>
        </div>

        <div class="block">
          <div class="title-row">
            <h3>多因素认证（MFA）</h3>
            <a-tag :color="mfa.enabled ? 'success' : 'default'">{{ mfa.enabled ? '已启用' : '未启用' }}</a-tag>
          </div>
          <p class="muted desc">
            启用后登录需要输入验证器动态码。{{ mfa.enabled ? `剩余恢复码 ${mfa.remainingRecoveryCodes} 个。` : '建议所有管理员开启。' }}
          </p>

          <template v-if="!mfa.enabled">
            <a-button v-if="!setup" type="primary" ghost :loading="mfaLoading" @click="setupOpen = true">开始设置</a-button>
            <div v-else class="mfa-setup">
              <div class="qr"><qrcode-vue v-if="setup.otpauthUrl" :value="setup.otpauthUrl" :size="150" :margin="1" /></div>
              <div class="mfa-info">
                <p class="muted">用验证器扫描二维码，或手动输入密钥：</p>
                <a-typography-text copyable class="mono secret">{{ setup.secret }}</a-typography-text>
                <a-input v-model:value="enableCode" placeholder="输入 6 位动态码" style="width: 200px; margin-top: 10px" />
                <div class="row">
                  <a-button type="primary" :loading="mfaLoading" @click="enableMfa">启用 MFA</a-button>
                  <a-button @click="cancelSetup">取消</a-button>
                </div>
              </div>
            </div>
          </template>

          <template v-else>
            <a-button danger ghost @click="disableOpen = true">关闭 MFA</a-button>
          </template>

          <a-alert v-if="recoveryCodes.length" type="warning" show-icon style="margin-top: 14px" message="请立即保存恢复码">
            <template #description>
              <div class="codes">
                <code v-for="code in recoveryCodes" :key="code">{{ code }}</code>
              </div>
            </template>
          </a-alert>
        </div>
      </a-col>

      <a-col :xs="24" :md="12">
        <div class="block">
          <div class="title-row">
            <h3>登录设备</h3>
            <a-button size="small" :loading="sessionsLoading" @click="loadSessions">刷新</a-button>
          </div>
          <a-spin :spinning="sessionsLoading">
            <div v-for="item in sessions" :key="item.id" class="session">
              <div>
                <div class="session-title">
                  {{ item.current ? '当前设备' : item.device || item.userAgent || '登录会话' }}
                  <a-tag v-if="item.current" color="blue" :bordered="false">当前</a-tag>
                </div>
                <div class="muted session-meta">
                  最近活跃 {{ fmtDateTime(item.lastSeenAt || item.createdAt) }}
                  <span v-if="item.ip"> · {{ item.ip }}</span>
                </div>
              </div>
              <a-popconfirm v-if="!item.current" title="撤销该设备后需要重新登录，确认？" @confirm="revoke(item)">
                <a-button size="small" danger ghost>撤销</a-button>
              </a-popconfirm>
            </div>
            <div v-if="!sessions.length" class="empty-block small">暂无其他登录设备</div>
          </a-spin>
          <a-button size="small" style="margin-top: 12px" :loading="refreshing" @click="refreshSession">刷新当前会话令牌</a-button>
        </div>
      </a-col>
    </a-row>

    <a-modal v-model:open="setupOpen" title="启用多因素认证" :confirm-loading="mfaLoading" @ok="confirmSetup">
      <p class="muted" style="margin-top: 0">请输入当前密码；验证通过后会显示密钥与二维码。</p>
      <a-input-password v-model:value="setupPassword" placeholder="当前密码" @press-enter="confirmSetup" />
    </a-modal>

    <a-modal v-model:open="disableOpen" title="关闭多因素认证" :confirm-loading="mfaLoading" @ok="disableMfa">
      <a-alert type="warning" show-icon message="关闭后账号安全性会下降，确认后需要重新登录。" style="margin-bottom: 14px" />
      <a-form layout="vertical">
        <a-form-item label="当前密码"><a-input-password v-model:value="disableForm.password" /></a-form-item>
        <a-form-item label="动态码或恢复码"><a-input v-model:value="disableForm.code" /></a-form-item>
      </a-form>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue';
import { useRouter } from 'vue-router';
import QrcodeVue from 'qrcode.vue';
import { api, getToken, setToken } from '@/api/client';
import { useSession } from '@/store/session';
import { fmtDateTime } from '@/utils/format';
import { notify } from '@/utils/notify';

interface MfaState {
  enabled: boolean;
  available: boolean;
  remainingRecoveryCodes: number;
}

const router = useRouter();
const session = useSession();
const saving = ref(false);
const mfaLoading = ref(false);
const sessionsLoading = ref(false);
const refreshing = ref(false);
const mfa = ref<MfaState>({ enabled: false, available: false, remainingRecoveryCodes: 0 });
const setup = ref<{ secret: string; otpauthUrl: string } | null>(null);
const enableCode = ref('');
const recoveryCodes = ref<string[]>([]);
const disableOpen = ref(false);
const setupOpen = ref(false);
const setupPassword = ref('');
const password = reactive({ current: '', next: '', confirm: '' });
const disableForm = reactive({ password: '', code: '' });
const sessions = ref<Array<Record<string, any>>>([]);

async function loadMfa(): Promise<void> {
  try {
    mfa.value = await api<MfaState>('GET', '/me/mfa');
  } catch (e) {
    notify.error(e);
  }
}

async function confirmSetup(): Promise<void> {
  if (!setupPassword.value) {
    notify.error('请输入当前密码');
    return;
  }
  mfaLoading.value = true;
  try {
    setup.value = await api<{ secret: string; otpauthUrl: string }>('POST', '/me/mfa/setup', {
      currentPassword: setupPassword.value,
    });
    setupOpen.value = false;
    setupPassword.value = '';
  } catch (e) {
    notify.error(e);
  } finally {
    mfaLoading.value = false;
  }
}

async function enableMfa(): Promise<void> {
  if (!enableCode.value.trim()) {
    notify.error('请输入动态码');
    return;
  }
  mfaLoading.value = true;
  try {
    const result = await api<{ recoveryCodes: string[] }>('POST', '/me/mfa/enable', { code: enableCode.value.trim() });
    recoveryCodes.value = result.recoveryCodes || [];
    setup.value = null;
    enableCode.value = '';
    await loadMfa();
    notify.success('MFA 已启用，请保存恢复码');
  } catch (e) {
    notify.error(e);
  } finally {
    mfaLoading.value = false;
  }
}

async function cancelSetup(): Promise<void> {
  try {
    await api('DELETE', '/me/mfa/setup');
  } catch {
    /* 忽略 */
  }
  setup.value = null;
  enableCode.value = '';
}

async function disableMfa(): Promise<void> {
  if (!disableForm.password || !disableForm.code) {
    notify.error('请输入密码和动态码');
    return;
  }
  mfaLoading.value = true;
  try {
    await api('POST', '/me/mfa/disable', { currentPassword: disableForm.password, code: disableForm.code });
    disableOpen.value = false;
    session.reset();
    notify.success('MFA 已关闭，请重新登录');
    router.replace('/login');
  } catch (e) {
    notify.error(e);
  } finally {
    mfaLoading.value = false;
  }
}

async function changePassword(): Promise<void> {
  if (password.next.length < 12) {
    notify.error('新密码至少 12 位');
    return;
  }
  if (password.next !== password.confirm) {
    notify.error('两次输入的新密码不一致');
    return;
  }
  saving.value = true;
  try {
    await api('POST', '/auth/password', { current: password.current, password: password.next });
    session.reset();
    notify.success('密码已修改，请重新登录');
    router.replace('/login');
  } catch (e) {
    notify.error(e);
  } finally {
    saving.value = false;
  }
}

async function loadSessions(): Promise<void> {
  sessionsLoading.value = true;
  try {
    const result = await api<Array<Record<string, any>>>('GET', '/auth/sessions');
    sessions.value = (result || []).map((item) => ({ ...item, current: Boolean(item.current || item.isCurrent) }));
  } catch (e) {
    notify.error(e);
  } finally {
    sessionsLoading.value = false;
  }
}

async function revoke(item: Record<string, any>): Promise<void> {
  try {
    await api('DELETE', `/auth/sessions/${item.id}`);
    notify.success('设备已撤销');
    await loadSessions();
  } catch (e) {
    notify.error(e);
  }
}

async function refreshSession(): Promise<void> {
  refreshing.value = true;
  try {
    const result = await api<{ accessToken: string }>('POST', '/auth/refresh');
    setToken(result.accessToken, Boolean(localStorage.getItem('teammind.accessToken')));
    notify.success('会话已刷新');
    await loadSessions();
  } catch (e) {
    notify.error(e);
  } finally {
    refreshing.value = false;
  }
}

onMounted(() => {
  loadMfa();
  loadSessions();
  if (!getToken()) router.replace('/login');
});
</script>

<style scoped>
.block {
  margin-bottom: 28px;
  max-width: 560px;
}
h3 {
  margin: 0 0 8px;
  font-size: 14px;
}
.title-row {
  display: flex;
  align-items: center;
  gap: 10px;
}
.desc {
  margin: -2px 0 12px;
  font-size: 12px;
}
.mfa-setup {
  display: flex;
  gap: 18px;
  align-items: flex-start;
  flex-wrap: wrap;
}
.qr {
  width: 150px;
  height: 150px;
  padding: 6px;
  background: #fff;
  border: 1px solid var(--tm-border);
  border-radius: 10px;
}
.mfa-info {
  flex: 1;
  min-width: 200px;
}
.secret {
  font-size: 12px;
  word-break: break-all;
}
.row {
  display: flex;
  gap: 8px;
  margin-top: 10px;
}
.codes {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 6px;
  margin-top: 6px;
}
.codes code {
  padding: 4px 8px;
  border-radius: 4px;
  background: rgba(0, 0, 0, 0.06);
  font-size: 12px;
  letter-spacing: 0.06em;
}
.session {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 11px 0;
  border-bottom: 1px solid var(--tm-border);
}
.session:last-child {
  border-bottom: 0;
}
.session-title {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12.5px;
  font-weight: 500;
}
.session-meta {
  margin-top: 2px;
  font-size: 11px;
}
.empty-block.small {
  padding: 20px;
  font-size: 12px;
}
</style>
