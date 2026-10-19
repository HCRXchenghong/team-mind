<template>
  <section class="tm-page">
    <header class="tm-header">
      <div><h1>设置</h1><p>个人资料、账号安全和团队设置</p></div>
    </header>

    <datalist id="teamind-timezones">
      <option v-for="zone in commonTimezones" :key="zone" :value="zone" />
    </datalist>

    <div class="tm-card-grid">
      <section class="tm-panel">
        <h2>个人资料</h2>
        <form class="tm-form" @submit.prevent="saveProfile">
          <label>邮箱<input :value="user.email" type="email" disabled /></label>
          <label
            >姓名<input
              v-model.trim="profile.name"
              autocomplete="name"
              required
              minlength="2"
              maxlength="120"
              :disabled="profileSaving"
          /></label>
          <label
            >时区
            <input
              v-model.trim="profile.timezone"
              list="teamind-timezones"
              autocomplete="off"
              required
              maxlength="80"
              :disabled="profileSaving"
              :aria-invalid="!validTimezone(profile.timezone)"
              aria-describedby="profile-timezone-help profile-timezone-error"
            />
            <small
              v-if="!validTimezone(profile.timezone)"
              id="profile-timezone-error"
              class="tm-field-error"
              >请输入有效的 IANA 时区名称。</small
            >
            <small id="profile-timezone-help"
              >使用 IANA 时区名称，例如 Asia/Shanghai。当前值：{{
                profile.timezone || '未填写'
              }}。</small
            >
          </label>
          <p v-if="profileError" class="tm-error" role="alert">{{
            profileError
          }}</p>
          <a-button
            type="primary"
            html-type="submit"
            :loading="profileSaving"
            :disabled="profileSaving"
            >保存资料</a-button
          >
        </form>
      </section>

      <section class="tm-panel">
        <h2>更改邮箱</h2>
        <p class="tm-data-export-note">当前邮箱：{{ user.email }}。需要验证新邮箱并输入当前密码；确认后所有已登录设备都会退出。</p>
        <form class="tm-form" @submit.prevent="requestEmailChange">
          <label>新邮箱<input v-model.trim="emailChange.email" type="email" autocomplete="email" required maxlength="254" :disabled="emailChangeSaving" /></label>
          <label>当前密码<input v-model="emailChange.currentPassword" type="password" autocomplete="current-password" required maxlength="72" :disabled="emailChangeSaving" /></label>
          <p v-if="emailChangeNotice" class="tm-success" role="status">{{ emailChangeNotice }}</p>
          <p v-if="emailChangeLink" class="tm-field-note">本地验收链接：<router-link :to="emailChangeLink">验证新邮箱</router-link></p>
          <p v-if="emailChangeError" class="tm-error" role="alert">{{ emailChangeError }}</p>
          <a-button type="primary" html-type="submit" :loading="emailChangeSaving" :disabled="emailChangeSaving">发送验证邮件</a-button>
        </form>
      </section>

      <section class="tm-panel">
        <h2>创建团队</h2>
        <form class="tm-form" @submit.prevent="createTeam">
          <label
            >团队名称<input
              v-model.trim="newTeam.name"
              autocomplete="organization"
              required
              minlength="2"
              maxlength="160"
              :disabled="teamCreating"
          /></label>
          <label
            >描述<textarea
              v-model="newTeam.description"
              maxlength="2000"
              :disabled="teamCreating"
            />
          </label>
          <label
            >默认时区<input
              v-model.trim="newTeam.timezone"
              list="teamind-timezones"
              autocomplete="off"
              required
              maxlength="80"
              :disabled="teamCreating"
              :aria-invalid="!validTimezone(newTeam.timezone)"
              aria-describedby="team-create-timezone-error"
          /></label>
          <small
            v-if="!validTimezone(newTeam.timezone)"
            id="team-create-timezone-error"
            class="tm-field-error"
            >请输入有效的 IANA 时区名称。</small
          >
          <p v-if="teamCreateError" class="tm-error" role="alert">{{
            teamCreateError
          }}</p>
          <a-button
            type="primary"
            html-type="submit"
            :loading="teamCreating"
            :disabled="teamCreating"
            >创建团队</a-button
          >
        </form>
      </section>

      <section v-if="store.activeTeam.id" class="tm-panel">
        <h2>当前团队</h2>
        <p>你的角色：{{ store.activeTeam.role }}</p>
        <form v-if="store.isAdmin" class="tm-form" @submit.prevent="saveTeam">
          <label
            >名称<input
              v-model.trim="team.name"
              required
              minlength="2"
              maxlength="160"
              :disabled="teamSaving"
          /></label>
          <label
            >描述<textarea
              v-model="team.description"
              maxlength="2000"
              :disabled="teamSaving"
            />
          </label>
          <label
            >时区
            <input
              v-model.trim="team.timezone"
              list="teamind-timezones"
              autocomplete="off"
              required
              maxlength="80"
              :disabled="teamSaving"
              :aria-invalid="!validTimezone(team.timezone)"
              aria-describedby="team-timezone-help team-timezone-error"
            />
            <small
              v-if="!validTimezone(team.timezone)"
              id="team-timezone-error"
              class="tm-field-error"
              >请输入有效的 IANA 时区名称。</small
            >
            <small id="team-timezone-help"
              >使用 IANA 时区名称，例如 Asia/Shanghai。当前值：{{
                team.timezone || '未填写'
              }}。</small
            >
          </label>
          <p v-if="teamError" class="tm-error" role="alert">{{ teamError }}</p>
          <a-button
            type="primary"
            html-type="submit"
            :loading="teamSaving"
            :disabled="teamSaving"
            >保存团队设置</a-button
          >
        </form>
        <p v-else class="tm-empty">只有团队管理员和所有者可以修改团队设置。</p>
      </section>

      <section class="tm-panel">
        <h2>修改密码</h2>
        <form class="tm-form" @submit.prevent="changePassword">
          <label
            >当前密码<input
              v-model="password.current"
              type="password"
              autocomplete="current-password"
              required
              maxlength="72"
              :disabled="passwordSaving"
          /></label>
          <label
            >新密码<input
              v-model="password.password"
              type="password"
              autocomplete="new-password"
              required
              minlength="12"
              maxlength="72"
              :disabled="passwordSaving"
          /></label>
          <small>新密码至少 12 个字符。保存后，所有已登录设备都会退出。</small>
          <p v-if="passwordError" class="tm-error" role="alert">{{
            passwordError
          }}</p>
          <a-button
            html-type="submit"
            :loading="passwordSaving"
            :disabled="passwordSaving"
            >修改并退出所有会话</a-button
          >
        </form>
      </section>

      <section class="tm-panel">
        <h2>多因素认证</h2>
        <p class="tm-data-export-note" v-if="mfaStatus.enabled">已启用基于时间的一次性验证码。最近一次启用或关闭会撤销其他有效会话。</p>
        <p class="tm-data-export-note" v-else>使用身份验证器应用生成登录验证码。恢复码只显示一次，请保存在安全位置。</p>
        <p v-if="!mfaStatus.available" class="tm-field-note">服务端尚未配置 MFA 加密密钥；请管理员配置后再启用。</p>
        <p v-if="mfaError" class="tm-error" role="alert">{{ mfaError }}</p>
        <p v-if="mfaNotice" class="tm-success" role="status">{{ mfaNotice }}</p>
        <div v-if="mfaLoading" class="tm-empty" role="status">正在读取多因素认证状态…</div>
        <template v-else-if="mfaStatus.enabled">
          <p class="tm-field-note">剩余恢复码：{{ mfaStatus.remainingRecoveryCodes }}</p>
          <div v-if="mfaRecoveryCodes.length" class="tm-recovery-codes" role="status">
            <strong>请现在保存这些一次性恢复码</strong>
            <code v-for="code in mfaRecoveryCodes" :key="code">{{ code }}</code>
            <a-button @click="downloadRecoveryCodes">下载恢复码</a-button>
          </div>
          <form class="tm-form" @submit.prevent="disableMFA">
            <label>当前密码<input v-model="mfaPassword" type="password" autocomplete="current-password" required maxlength="72" :disabled="mfaSaving" /></label>
            <label>动态验证码或未使用的恢复码<input v-model="mfaCode" autocomplete="one-time-code" required maxlength="64" :disabled="mfaSaving" /></label>
            <a-button status="danger" html-type="submit" :loading="mfaSaving" :disabled="mfaSaving">关闭并退出所有设备</a-button>
          </form>
        </template>
        <template v-else-if="mfaSetup.secret">
          <p>使用身份验证器扫描此二维码，或手动输入下方密钥：</p>
          <img v-if="mfaQR" class="tm-mfa-qr" :src="mfaQR" alt="TeamMind 多因素认证设置二维码" />
          <code class="tm-mfa-secret">{{ mfaSetup.secret }}</code>
          <form class="tm-form" @submit.prevent="enableMFA">
            <label>身份验证器验证码<input v-model="mfaCode" inputmode="numeric" autocomplete="one-time-code" required minlength="6" maxlength="6" :disabled="mfaSaving" /></label>
            <a-button type="primary" html-type="submit" :loading="mfaSaving" :disabled="mfaSaving">验证并启用</a-button>
            <a-button :disabled="mfaSaving" @click="cancelMFASetup">取消设置</a-button>
          </form>
        </template>
        <form v-else class="tm-form" @submit.prevent="beginMFASetup">
          <label>当前密码<input v-model="mfaPassword" type="password" autocomplete="current-password" required maxlength="72" :disabled="mfaSaving || !mfaStatus.available" /></label>
          <a-button type="primary" html-type="submit" :loading="mfaSaving" :disabled="mfaSaving || !mfaStatus.available">开始设置</a-button>
          <a-button :disabled="mfaLoading" @click="loadMFA">刷新状态</a-button>
        </form>
      </section>

      <section class="tm-panel">
        <h2>个人数据副本</h2>
        <p class="tm-data-export-note"
          >下载当前账号资料、私人对话与记忆、通知、提醒、会话记录和相关设置的
          JSON
          副本。团队共享项目与任务、密码哈希、登录令牌、微信凭据和推送订阅密钥不包含在文件中。</p
        >
        <p v-if="exportError" class="tm-error" role="alert">{{
          exportError
        }}</p>
        <a-button
          type="primary"
          :loading="exportingData"
          :disabled="exportingData"
          @click="downloadPersonalData"
          >下载我的数据副本</a-button
        >
      </section>

      <section class="tm-panel tm-account-delete-panel">
        <h2>删除账号</h2>
        <p class="tm-data-export-note">删除会立即撤销所有会话并清除私人对话、记忆、通知、提醒、登录凭据和渠道绑定。团队项目、任务、评论与审批历史会保留，并以“已注销用户”匿名显示；相关团队审计记录继续保留。操作不可撤销。删除前请先下载需要保留的数据副本。</p>
        <form class="tm-form" @submit.prevent="deleteAccount">
          <label>当前密码<input v-model="accountDeletion.currentPassword" type="password" autocomplete="current-password" required maxlength="72" :disabled="accountDeletionSaving" /></label>
          <label v-if="mfaStatus.enabled">动态验证码或未使用的恢复码<input v-model="accountDeletion.mfaCode" autocomplete="one-time-code" required maxlength="64" :disabled="accountDeletionSaving" /></label>
          <label>确认文字<input v-model="accountDeletion.confirmation" autocomplete="off" required minlength="4" maxlength="4" :disabled="accountDeletionSaving" aria-describedby="account-deletion-confirm-help" /></label>
          <small id="account-deletion-confirm-help">输入“删除账号”以确认。</small>
          <p v-if="accountDeletionError" class="tm-error" role="alert">{{ accountDeletionError }}</p>
          <a-button status="danger" html-type="submit" :loading="accountDeletionSaving" :disabled="accountDeletionSaving">永久删除账号</a-button>
        </form>
      </section>

      <section class="tm-panel">
        <div class="tm-audit-heading">
          <div><h2>有效会话</h2><p>撤销后，该设备的访问令牌立即失效。</p></div>
          <div class="tm-actions">
            <a-button
              :loading="sessionsLoading"
              :disabled="sessionsLoading"
              @click="loadSessions"
              >刷新会话</a-button
            >
            <a-button
              :loading="sessionRefreshing"
              :disabled="sessionRefreshing || sessionsLoading"
              @click="refreshSession"
              >续期当前会话</a-button
            >
          </div>
        </div>
        <p v-if="sessionError" class="tm-error" role="alert"
          >{{ sessionError }}
          <a-button size="small" @click="loadSessions">重试</a-button></p
        >
        <div
          v-if="sessionsLoading && !sessions.length"
          class="tm-empty"
          role="status"
          >正在加载会话…</div
        >
        <div v-else-if="!sessions.length" class="tm-empty"
          >当前没有其他有效会话。</div
        >
        <div v-for="session in sessions" :key="session.id" class="tm-row">
          <span
            >{{ formatDateTime(session.createdAt, user.timezone)
            }}{{ session.current ? ' · 当前设备' : '' }} · 到期
            {{ formatDateTime(session.expiresAt, user.timezone) }}</span
          >
          <a-button
            size="small"
            status="danger"
            :loading="revokingSessionId === session.id"
            :disabled="!!revokingSessionId || sessionRefreshing"
            @click="revoke(session)"
            >撤销</a-button
          >
        </div>
      </section>

      <section v-if="store.isAdmin" class="tm-panel tm-audit-panel">
        <div class="tm-audit-heading"
          ><h2>团队审计记录</h2
          ><a-button
            :loading="auditLoading"
            :disabled="auditLoading"
            @click="loadAudit"
            >刷新记录</a-button
          ></div
        >
        <p v-if="auditError" class="tm-error" role="alert"
          >{{ auditError }}
          <a-button size="small" @click="loadAudit">重试</a-button></p
        >
        <div
          v-if="auditLoading && !auditRows.length"
          class="tm-empty"
          role="status"
          >正在加载审计记录…</div
        >
        <div v-else-if="!auditRows.length && !auditError" class="tm-empty"
          >暂无团队审计记录</div
        >
        <article v-for="row in auditRows" :key="row.id" class="tm-audit-row">
          <strong>{{ row.action }}</strong
          ><span
            >{{ row.resourceType }} · {{ row.resourceId || '无资源 ID' }}</span
          >
          <small
            >操作人 {{ row.actorId }} ·
            {{ formatDateTime(row.createdAt, user.timezone) }}</small
          >
        </article>
      </section>
    </div>
  </section>
</template>

<script setup lang="ts">
  import { formatDateTime } from '@/utils/time';
  import { ref, watch, onMounted } from 'vue';
  import { Message, Modal } from '@arco-design/web-vue';
  import { useRouter } from 'vue-router';
  import { setToken, isRemembered, clearToken } from '@/utils/auth';
  import { api } from '@/api/workspace';
  import { useWorkspaceStore } from '@/stores/workspace';
  import { useUserStore } from '@/store';
  import QRCode from 'qrcode';

  const commonTimezones = [
    'Asia/Shanghai',
    'Asia/Tokyo',
    'Asia/Singapore',
    'Asia/Dubai',
    'Europe/London',
    'Europe/Paris',
    'America/Los_Angeles',
    'America/New_York',
    'Australia/Sydney',
    'UTC',
  ];
  const store = useWorkspaceStore();
  const user = useUserStore();
  const router = useRouter();
  const profile = ref({
    name: user.name || '',
    timezone: user.timezone || 'Asia/Shanghai',
  });
  const team = ref({ name: '', description: '', timezone: 'Asia/Shanghai' });
  const newTeam = ref({ name: '', description: '', timezone: 'Asia/Shanghai' });
  const password = ref({ current: '', password: '' });
  const emailChange = ref({ email: '', currentPassword: '' });
  const emailChangeSaving = ref(false);
  const emailChangeError = ref('');
  const emailChangeNotice = ref('');
  const emailChangeLink = ref('');
  const mfaStatus = ref({ enabled: false, available: true, remainingRecoveryCodes: 0 });
  const mfaSetup = ref({ secret: '', otpauthUrl: '' });
  const mfaQR = ref('');
  const mfaCode = ref('');
  const mfaPassword = ref('');
  const mfaRecoveryCodes = ref<string[]>([]);
  const mfaError = ref('');
  const mfaNotice = ref('');
  const mfaLoading = ref(false);
  const mfaSaving = ref(false);
  const exportingData = ref(false);
  const exportError = ref('');
  const accountDeletion = ref({ currentPassword: '', mfaCode: '', confirmation: '' });
  const accountDeletionSaving = ref(false);
  const accountDeletionError = ref('');
  const sessions = ref<
    { id: string; createdAt: string; expiresAt: string; current: boolean }[]
  >([]);
  const auditRows = ref<
    {
      id: number;
      actorId: string;
      action: string;
      resourceType: string;
      resourceId: string;
      createdAt: string;
    }[]
  >([]);
  const profileError = ref('');
  const teamCreateError = ref('');
  const teamError = ref('');
  const passwordError = ref('');
  const sessionError = ref('');
  const auditError = ref('');
  const profileSaving = ref(false);
  const teamCreating = ref(false);
  const teamSaving = ref(false);
  const passwordSaving = ref(false);
  const sessionsLoading = ref(false);
  const sessionRefreshing = ref(false);
  const revokingSessionId = ref('');
  const auditLoading = ref(false);
  let auditGeneration = 0;

  function validTimezone(value: string) {
    if (!value.trim()) return false;
    try {
      new Intl.DateTimeFormat('en', { timeZone: value.trim() });
      return true;
    } catch {
      return false;
    }
  }

  async function saveProfile() {
    if (profileSaving.value) return;
    profileError.value = '';
    if (profile.value.name.trim().length < 2) {
      profileError.value = '姓名至少 2 个字符';
      return;
    }
    if (!validTimezone(profile.value.timezone)) {
      profileError.value = '时区无效，请输入有效的 IANA 时区名称';
      return;
    }
    profileSaving.value = true;
    try {
      await api('PATCH', '/me', profile.value);
      user.setInfo({
        name: profile.value.name.trim(),
        timezone: profile.value.timezone.trim(),
      });
      Message.success('资料已保存');
    } catch (e) {
      profileError.value = (e as Error).message;
    } finally {
      profileSaving.value = false;
    }
  }

  async function createTeam() {
    if (teamCreating.value) return;
    teamCreateError.value = '';
    if (newTeam.value.name.trim().length < 2) {
      teamCreateError.value = '团队名称至少 2 个字符';
      return;
    }
    if (!validTimezone(newTeam.value.timezone)) {
      teamCreateError.value = '时区无效，请输入有效的 IANA 时区名称';
      return;
    }
    teamCreating.value = true;
    try {
      const created = await api<{ id: string }>(
        'POST',
        '/teams',
        newTeam.value
      );
      await store.load();
      store.switchTeam(created.id.toUpperCase());
      newTeam.value = { name: '', description: '', timezone: 'Asia/Shanghai' };
      Message.success('团队已创建');
    } catch (e) {
      teamCreateError.value = (e as Error).message;
    } finally {
      teamCreating.value = false;
    }
  }

  async function saveTeam() {
    if (teamSaving.value) return;
    teamError.value = '';
    if (team.value.name.trim().length < 2) {
      teamError.value = '团队名称至少 2 个字符';
      return;
    }
    if (!validTimezone(team.value.timezone)) {
      teamError.value = '时区无效，请输入有效的 IANA 时区名称';
      return;
    }
    teamSaving.value = true;
    try {
      await api('PATCH', `/teams/${store.activeTeam.id}`, team.value);
      await store.load();
      Message.success('团队设置已保存');
    } catch (e) {
      teamError.value = (e as Error).message;
    } finally {
      teamSaving.value = false;
    }
  }

  async function changePassword() {
    if (passwordSaving.value) return;
    passwordError.value = '';
    if (!password.value.current) {
      passwordError.value = '请输入当前密码';
      return;
    }
    if (
      password.value.password.length < 12 ||
      password.value.password.length > 72
    ) {
      passwordError.value = '新密码长度需为 12–72 个字符';
      return;
    }
    passwordSaving.value = true;
    try {
      await api('POST', '/auth/password', password.value);
      user.logoutCallBack();
      await router.replace('/login');
    } catch (e) {
      passwordError.value = (e as Error).message;
    } finally {
      passwordSaving.value = false;
    }
  }

  async function requestEmailChange() {
    if (emailChangeSaving.value) return;
    emailChangeError.value = '';
    emailChangeNotice.value = '';
    emailChangeLink.value = '';
    emailChangeSaving.value = true;
    try {
      const result = await api<{ message: string; verificationToken?: string }>(
        'POST',
        '/me/email-change',
        emailChange.value
      );
      emailChangeNotice.value = result.message;
      emailChange.value.currentPassword = '';
      if (result.verificationToken) {
        emailChangeLink.value = `/verify-email-change#verifyEmailToken=${encodeURIComponent(result.verificationToken)}`;
      }
    } catch (e) {
      emailChangeError.value = (e as Error).message;
    } finally {
      emailChangeSaving.value = false;
    }
  }

  async function loadMFA() {
    if (mfaLoading.value) return;
    mfaLoading.value = true;
    mfaError.value = '';
    try {
      mfaStatus.value = await api('GET', '/me/mfa');
    } catch (e) {
      mfaError.value = (e as Error).message;
    } finally {
      mfaLoading.value = false;
    }
  }

  async function beginMFASetup() {
    if (mfaSaving.value) return;
    mfaSaving.value = true;
    mfaError.value = '';
    mfaNotice.value = '';
    try {
      const setup = await api<{ secret: string; otpauthUrl: string }>(
        'POST',
        '/me/mfa/setup',
        { currentPassword: mfaPassword.value }
      );
      mfaSetup.value = setup;
      mfaPassword.value = '';
      mfaCode.value = '';
      mfaQR.value = await QRCode.toDataURL(setup.otpauthUrl, { width: 220, margin: 2 });
    } catch (e) {
      mfaError.value = (e as Error).message;
    } finally {
      mfaSaving.value = false;
    }
  }

  async function cancelMFASetup() {
    if (mfaSaving.value) return;
    mfaSaving.value = true;
    mfaError.value = '';
    try {
      await api('DELETE', '/me/mfa/setup');
      mfaSetup.value = { secret: '', otpauthUrl: '' };
      mfaQR.value = '';
      mfaCode.value = '';
      mfaNotice.value = '多因素认证设置已取消';
      await loadMFA();
    } catch (e) {
      mfaError.value = (e as Error).message;
    } finally {
      mfaSaving.value = false;
    }
  }

  async function enableMFA() {
    if (mfaSaving.value) return;
    mfaSaving.value = true;
    mfaError.value = '';
    mfaNotice.value = '';
    try {
      const result = await api<{ enabled: boolean; recoveryCodes: string[] }>(
        'POST',
        '/me/mfa/enable',
        { code: mfaCode.value }
      );
      mfaStatus.value = { ...mfaStatus.value, enabled: result.enabled, remainingRecoveryCodes: result.recoveryCodes.length };
      mfaRecoveryCodes.value = result.recoveryCodes;
      mfaSetup.value = { secret: '', otpauthUrl: '' };
      mfaQR.value = '';
      mfaCode.value = '';
      mfaNotice.value = '多因素认证已启用。请保存下方恢复码；离开此页后将无法再次查看。';
      Message.success('多因素认证已启用');
    } catch (e) {
      mfaError.value = (e as Error).message;
    } finally {
      mfaSaving.value = false;
    }
  }

  function disableMFA() {
    if (mfaSaving.value) return;
    Modal.confirm({
      title: '关闭多因素认证并退出所有设备？',
      content: '必须提供当前密码和动态验证码或未使用的恢复码。关闭后将撤销所有登录会话。',
      onOk: async () => {
        mfaSaving.value = true;
        mfaError.value = '';
        try {
          await api('POST', '/me/mfa/disable', { currentPassword: mfaPassword.value, code: mfaCode.value });
          user.logoutCallBack();
          await router.replace('/login');
        } catch (e) {
          mfaError.value = (e as Error).message;
          throw e;
        } finally {
          mfaSaving.value = false;
        }
      },
    });
  }

  function downloadRecoveryCodes() {
    if (!mfaRecoveryCodes.value.length) return;
    const file = new Blob([`TeamMind 多因素认证恢复码\n请妥善保管，每个恢复码只能使用一次。\n\n${mfaRecoveryCodes.value.join('\n')}\n`], { type: 'text/plain;charset=utf-8' });
    const url = URL.createObjectURL(file);
    const link = document.createElement('a');
    link.href = url;
    link.download = 'teammind-mfa-recovery-codes.txt';
    link.click();
    URL.revokeObjectURL(url);
  }

  async function downloadPersonalData() {
    if (exportingData.value) return;
    exportingData.value = true;
    exportError.value = '';
    try {
      const data = await api<Record<string, unknown>>('GET', '/me/data-export');
      const file = new Blob([JSON.stringify(data, null, 2)], {
        type: 'application/json;charset=utf-8',
      });
      const url = URL.createObjectURL(file);
      const link = document.createElement('a');
      link.href = url;
      link.download = 'teammind-personal-data.json';
      link.hidden = true;
      document.body.append(link);
      link.click();
      link.remove();
      window.setTimeout(() => URL.revokeObjectURL(url), 1000);
      Message.success('个人数据副本已生成并下载');
    } catch (e) {
      exportError.value = (e as Error).message;
    } finally {
      exportingData.value = false;
    }
  }

  async function deleteAccount() {
    if (accountDeletionSaving.value) return;
    accountDeletionError.value = '';
    if (accountDeletion.value.confirmation !== '删除账号') {
      accountDeletionError.value = '请输入“删除账号”以确认。';
      return;
    }
    accountDeletionSaving.value = true;
    try {
      await api('POST', '/me/account', accountDeletion.value);
      clearToken();
      store.clear();
      user.resetInfo();
      Message.success('账号及私人数据已删除，所有会话已撤销。团队共享记录已匿名保留。');
      await router.replace({ path: '/login', query: { accountDeleted: '1' } });
    } catch (e) {
      accountDeletionError.value = (e as Error).message;
    } finally {
      accountDeletionSaving.value = false;
    }
  }

  async function refreshSession() {
    if (sessionRefreshing.value || sessionsLoading.value) return;
    sessionRefreshing.value = true;
    sessionError.value = '';
    try {
      const x = await api<{ accessToken: string }>('POST', '/auth/refresh');
      setToken(x.accessToken, isRemembered());
      Message.success('会话已续期');
      await loadSessions();
    } catch (e) {
      sessionError.value = (e as Error).message;
    } finally {
      sessionRefreshing.value = false;
    }
  }

  async function loadSessions() {
    if (sessionsLoading.value) return;
    sessionsLoading.value = true;
    sessionError.value = '';
    try {
      sessions.value = await api('GET', '/auth/sessions');
    } catch (e) {
      sessionError.value = (e as Error).message;
    } finally {
      sessionsLoading.value = false;
    }
  }

  function revoke(session: { id: string; current: boolean }) {
    if (revokingSessionId.value) return;
    Modal.confirm({
      title: session.current ? '撤销当前会话并退出？' : '撤销此会话？',
      content: session.current
        ? '当前设备会立即退出登录。'
        : '该设备的访问令牌将立即失效。',
      onOk: async () => {
        revokingSessionId.value = session.id;
        sessionError.value = '';
        try {
          await api('DELETE', `/auth/sessions/${session.id}`);
          Message.success('会话已撤销');
          if (session.current) {
            user.logoutCallBack();
            await router.replace('/login');
          } else {
            await loadSessions();
          }
        } catch (e) {
          sessionError.value = (e as Error).message;
        } finally {
          revokingSessionId.value = '';
        }
      },
    });
  }

  async function loadAudit() {
    const generation = ++auditGeneration;
    const teamId = store.activeTeam.id;
    if (!teamId || !store.isAdmin) {
      auditRows.value = [];
      auditError.value = '';
      auditLoading.value = false;
      return;
    }
    auditLoading.value = true;
    try {
      const rows = await api<typeof auditRows.value>(
        'GET',
        `/teams/${teamId}/audit-log`
      );
      if (generation !== auditGeneration || teamId !== store.activeTeam.id)
        return;
      auditRows.value = rows;
      auditError.value = '';
    } catch (e) {
      if (generation !== auditGeneration || teamId !== store.activeTeam.id)
        return;
      auditError.value = (e as Error).message;
    } finally {
      if (generation === auditGeneration) auditLoading.value = false;
    }
  }

  watch(
    () => store.activeTeam.id,
    () => {
      team.value = {
        name: store.activeTeam.name,
        description: store.activeTeam.description,
        timezone: store.activeTeam.timezone,
      };
      teamError.value = '';
    },
    { immediate: true }
  );
  watch(() => [store.activeTeam.id, store.activeTeam.role], loadAudit, {
    immediate: true,
  });
  onMounted(() => {
    void loadSessions();
    void loadMFA();
  });
</script>

<style scoped>
  .tm-form small,
  .tm-audit-heading p,
  .tm-data-export-note {
    color: var(--color-text-2);
  }
  .tm-form .tm-field-error {
    color: #c22f37;
  }
  .tm-success {
    color: #0f7b43;
  }
  .tm-field-note {
    color: var(--color-text-2);
  }
  .tm-mfa-qr { display: block; width: min(220px, 100%); height: auto; margin: 12px auto; }
  .tm-mfa-secret { display: block; padding: 12px; overflow-wrap: anywhere; background: var(--color-fill-2); }
  .tm-recovery-codes { display: grid; gap: 8px; max-width: 420px; margin: 16px 0; padding: 16px; border: 1px solid var(--color-warning-6); border-radius: 8px; }
  .tm-recovery-codes code { font-size: 16px; letter-spacing: .04em; }
  .tm-audit-heading {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 12px;
  }
  .tm-audit-heading h2,
  .tm-audit-heading p {
    margin-top: 0;
  }
  .tm-audit-heading p {
    margin-bottom: 0;
  }
  .tm-audit-row {
    display: grid;
    gap: 4px;
    padding: 10px 0;
    border-bottom: 1px solid var(--color-border-2);
    overflow-wrap: anywhere;
  }
  .tm-audit-row:last-child {
    border-bottom: 0;
  }
  .tm-audit-row small {
    color: var(--color-text-2);
  }
  @media (max-width: 600px) {
    .tm-audit-heading {
      align-items: flex-start;
      flex-direction: column;
    }
    .tm-audit-heading .tm-actions {
      width: 100%;
      flex-wrap: wrap;
    }
    .tm-audit-heading .tm-actions > * {
      flex: 1;
    }
  }
</style>
