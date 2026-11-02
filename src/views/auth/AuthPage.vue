<template>
  <div class="auth">
    <aside class="auth-brand">
      <div class="brand-top">
        <BrandLogo light />
      </div>
      <div class="brand-copy">
        <p class="eyebrow">TEAM COLLABORATION · AI WORKSPACE</p>
        <h1>让项目、任务与 AI 协作<br />落在同一个工作台。</h1>
        <p class="lede">项目计划、任务看板、审批流、团队知识，以及一个真正懂组织上下文的 AI 助手。</p>
        <ul class="points">
          <li>任务计划变更走审批，正式计划不被悄悄改动</li>
          <li>甘特图按天排期，支持依赖、里程碑与只读分享</li>
          <li>AI 助手按次授权联网、代码与团队上下文</li>
        </ul>
      </div>
      <div class="brand-foot">
        <span>© 2026 TeamMind</span>
        <span>协作 + AI · 20–50 人团队</span>
      </div>
    </aside>

    <main class="auth-main">
      <div class="auth-card">
        <div class="mobile-brand"><BrandLogo /></div>

        <template v-if="mfaStep">
          <h2>两步验证</h2>
          <p class="sub">请输入验证器中的 6 位动态码，或使用一个恢复码。</p>
          <a-form layout="vertical" :model="mfaForm" @submit.prevent="submitMfa">
            <a-form-item label="动态码 / 恢复码">
              <a-input
                v-model:value="mfaForm.code"
                size="large"
                placeholder="6 位数字或恢复码"
                autofocus
                @press-enter="submitMfa"
              />
            </a-form-item>
            <a-button type="primary" size="large" block :loading="loading" @click="submitMfa">验证并登录</a-button>
            <a-button type="link" block class="back" @click="cancelMfa">返回账号密码</a-button>
          </a-form>
        </template>

        <template v-else-if="mode === 'login'">
          <h2>登录 TeamMind</h2>
          <p class="sub">使用工作邮箱进入你的协作工作台。</p>
          <a-form layout="vertical" :model="loginForm" @submit.prevent="submitLogin">
            <a-form-item label="邮箱">
              <a-input v-model:value="loginForm.email" size="large" placeholder="you@company.com" autocomplete="username">
                <template #prefix><mail-outlined /></template>
              </a-input>
            </a-form-item>
            <a-form-item label="密码">
              <a-input-password v-model:value="loginForm.password" size="large" placeholder="输入密码" autocomplete="current-password">
                <template #prefix><lock-outlined /></template>
              </a-input-password>
            </a-form-item>
            <div class="row-between">
              <a-checkbox v-model:checked="loginForm.remember">保持登录</a-checkbox>
              <router-link to="/forgot-password">忘记密码？</router-link>
            </div>
            <a-button type="primary" size="large" block :loading="loading" html-type="submit">登录</a-button>
          </a-form>
          <div class="switch-line">
            还没有账号？<router-link to="/register">注册</router-link>
          </div>
        </template>

        <template v-else-if="mode === 'register'">
          <template v-if="registrationChecked && !registrationEnabled">
            <h2>公开注册已关闭</h2>
            <p class="sub">当前只允许持有邀请链接的用户注册。</p>
            <a-alert
              type="info"
              show-icon
              class="notice"
              message="如何加入 TeamMind"
              description="公司管理员：使用平台超管发放的「公司邀请链接」创建公司。员工：使用公司或团队发出的「团队邀请链接」加入。"
            />
            <div class="switch-line"><router-link to="/login">返回登录</router-link></div>
          </template>
          <template v-else>
            <h2>创建账号</h2>
            <p class="sub">注册后即可加入团队；也可以使用邀请链接直接入队。</p>
            <a-form layout="vertical" :model="registerForm" @submit.prevent="submitRegister">
              <a-form-item label="姓名">
                <a-input v-model:value="registerForm.name" size="large" placeholder="你的名字" :maxlength="80" />
              </a-form-item>
              <a-form-item label="邮箱">
                <a-input v-model:value="registerForm.email" size="large" placeholder="you@company.com" autocomplete="email" />
              </a-form-item>
              <a-form-item label="密码">
                <a-input-password v-model:value="registerForm.password" size="large" placeholder="至少 12 位" autocomplete="new-password" />
              </a-form-item>
              <a-button type="primary" size="large" block :loading="loading" html-type="submit">创建账号</a-button>
            </a-form>
            <div class="switch-line">已有账号？<router-link to="/login">返回登录</router-link></div>
          </template>
        </template>

        <template v-else-if="mode === 'company'">
          <template v-if="inviteCode">
            <h2>创建公司</h2>
            <p class="sub">邀请码已从链接读取。创建后你将成为公司负责人，可以继续邀请员工。</p>
            <a-alert
              v-if="companyInvite && !companyInvite.valid"
              type="error"
              show-icon
              class="notice"
              :message="'公司邀请链接' + inviteReason(companyInvite.reason)"
            />
            <a-alert
              v-else-if="companyInvite"
              type="success"
              show-icon
              class="notice"
              :message="`邀请链接有效，剩余 ${companyInvite.remainingUses ?? 1} 次`"
            />
            <a-form layout="vertical" :model="companyForm" @submit.prevent="submitCompany">
              <a-form-item label="公司名称" required>
                <a-input v-model:value="companyForm.companyName" size="large" placeholder="例如：星河科技" :maxlength="160" />
              </a-form-item>
              <a-form-item label="组织类型">
                <a-radio-group v-model:value="companyForm.companyKind">
                  <a-radio value="company">公司（部门制）</a-radio>
                  <a-radio value="team">团队（单层小组）</a-radio>
                </a-radio-group>
              </a-form-item>
              <a-form-item label="你的姓名" required>
                <a-input v-model:value="companyForm.name" size="large" placeholder="管理员姓名" :maxlength="120" />
              </a-form-item>
              <a-form-item label="登录邮箱" required>
                <a-input v-model:value="companyForm.email" size="large" placeholder="admin@company.com" autocomplete="email" />
              </a-form-item>
              <a-form-item label="密码" required>
                <a-input-password v-model:value="companyForm.password" size="large" placeholder="至少 12 位" autocomplete="new-password" />
              </a-form-item>
              <a-button
                type="primary"
                size="large"
                block
                :loading="loading"
                :disabled="!!companyInvite && !companyInvite.valid"
                html-type="submit"
              >
                创建公司并登录
              </a-button>
            </a-form>
            <div class="switch-line"><router-link to="/login">已有账号？返回登录</router-link></div>
          </template>
          <template v-else>
            <h2>需要公司邀请链接</h2>
            <p class="sub">公司注册不公开，请使用平台超管发放的完整公司邀请链接打开此页面。</p>
            <div class="switch-line"><router-link to="/login">返回登录</router-link></div>
          </template>
        </template>

        <template v-else-if="mode === 'superadmin'">
          <template v-if="inviteCode">
            <h2>创建平台超管</h2>
            <p class="sub">邀请码已从链接读取。仅首次部署使用，创建后该链接立即失效。</p>
            <a-alert
              type="info"
              show-icon
              class="notice"
              message="这是内部邀请页面"
              description="请勿转发此链接；平台超管拥有平台级管理权限。"
            />
            <a-form layout="vertical" :model="superForm" @submit.prevent="submitSuperadmin">
              <a-form-item label="姓名">
                <a-input v-model:value="superForm.name" size="large" placeholder="你的名字" />
              </a-form-item>
              <a-form-item label="邮箱">
                <a-input v-model:value="superForm.email" size="large" placeholder="ops@company.com" />
              </a-form-item>
              <a-form-item label="密码">
                <a-input-password v-model:value="superForm.password" size="large" placeholder="至少 12 位" />
              </a-form-item>
              <a-button type="primary" size="large" block :loading="loading" html-type="submit">创建并登录</a-button>
            </a-form>
          </template>
          <template v-else>
            <h2>需要超管邀请链接</h2>
            <p class="sub">平台超管注册不公开，请使用部署管理员提供的完整邀请链接打开此页面。</p>
            <div class="switch-line"><router-link to="/login">返回登录</router-link></div>
          </template>
        </template>

        <template v-else-if="mode === 'forgot'">
          <h2>找回密码</h2>
          <p class="sub">输入注册邮箱，我们会发送重置说明。</p>
          <a-form layout="vertical" :model="forgotForm" @submit.prevent="submitForgot">
            <a-form-item label="邮箱">
              <a-input v-model:value="forgotForm.email" size="large" placeholder="you@company.com" />
            </a-form-item>
            <a-button type="primary" size="large" block :loading="loading" html-type="submit">发送重置说明</a-button>
          </a-form>
          <a-alert v-if="forgotSent" type="success" show-icon class="notice">
            如果账号存在，重置说明将发送至邮箱。
            <router-link v-if="devResetToken" :to="`/reset-password?token=${devResetToken}`">开发环境：直接重置</router-link>
          </a-alert>
          <div class="switch-line"><router-link to="/login">返回登录</router-link></div>
        </template>

        <template v-else>
          <h2>重置密码</h2>
          <p class="sub">设置一个新密码，至少 12 位。</p>
          <a-alert v-if="!resetToken" type="error" show-icon class="notice">链接缺少重置 token，请重新从邮件进入。</a-alert>
          <a-form layout="vertical" :model="resetForm" @submit.prevent="submitReset">
            <a-form-item label="新密码">
              <a-input-password v-model:value="resetForm.password" size="large" placeholder="至少 12 位" autocomplete="new-password" />
            </a-form-item>
            <a-form-item label="确认新密码">
              <a-input-password v-model:value="resetForm.password2" size="large" placeholder="再输入一次" autocomplete="new-password" />
            </a-form-item>
            <a-button type="primary" size="large" block :loading="loading" :disabled="!resetToken" html-type="submit">重置密码</a-button>
          </a-form>
          <div class="switch-line"><router-link to="/login">返回登录</router-link></div>
        </template>
      </div>
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { LockOutlined, MailOutlined } from '@ant-design/icons-vue';
import { api, setToken } from '@/api/client';
import { notify } from '@/utils/notify';
import BrandLogo from '@/components/BrandLogo.vue';

type Mode = 'login' | 'register' | 'company' | 'superadmin' | 'forgot' | 'reset';

interface TokenResponse {
  accessToken?: string;
  expiresIn?: number;
  mfaRequired?: boolean;
  challengeToken?: string;
  teamId?: string;
  resetToken?: string;
}

const route = useRoute();
const router = useRouter();
const mode = computed<Mode>(() => (route.meta.mode as Mode) || 'login');

const loading = ref(false);
const registrationEnabled = ref(false);
const registrationChecked = ref(false);
const forgotSent = ref(false);
const devResetToken = ref('');
const mfaStep = ref(false);
const challengeToken = ref('');

const loginForm = reactive({ email: '', password: '', remember: true });
const registerForm = reactive({ name: '', email: '', password: '' });
const superForm = reactive({ code: '', name: '', email: '', password: '' });
const companyForm = reactive({ companyName: '', companyKind: 'company', name: '', email: '', password: '' });
const companyInvite = ref<{ valid: boolean; reason: string; expiresAt?: string; remainingUses?: number } | null>(null);
const forgotForm = reactive({ email: '' });
const resetForm = reactive({ password: '', password2: '' });
const mfaForm = reactive({ code: '' });

const resetToken = computed(() => String(route.query.token || ''));
const inviteCode = computed(() => String(route.query.code || '').trim());

watch(
  inviteCode,
  (value) => {
    superForm.code = value;
  },
  { immediate: true },
);

function redirectTarget(): string {
  const value = String(route.query.redirect || '');
  return value.startsWith('/') && !value.startsWith('//') ? value : '/teammind/overview';
}

function finishLogin(token: string, remember: boolean): void {
  setToken(token, remember);
  const target = redirectTarget();
  router.replace(target);
}

async function submitLogin(): Promise<void> {
  if (!loginForm.email.trim() || !loginForm.password) {
    notify.error('请输入邮箱和密码');
    return;
  }
  loading.value = true;
  try {
    const result = await api<TokenResponse>('POST', '/auth/login', {
      email: loginForm.email.trim(),
      password: loginForm.password,
      remember: loginForm.remember,
    });
    if (result.mfaRequired && result.challengeToken) {
      challengeToken.value = result.challengeToken;
      mfaStep.value = true;
      mfaForm.code = '';
      return;
    }
    if (!result.accessToken) throw new Error('登录响应缺少令牌');
    finishLogin(result.accessToken, loginForm.remember);
  } catch (error) {
    notify.error(error);
  } finally {
    loading.value = false;
  }
}

async function submitMfa(): Promise<void> {
  if (!mfaForm.code.trim()) {
    notify.error('请输入动态码或恢复码');
    return;
  }
  loading.value = true;
  try {
    const result = await api<TokenResponse>('POST', '/auth/mfa/complete', {
      challengeToken: challengeToken.value,
      code: mfaForm.code.trim(),
    });
    if (!result.accessToken) throw new Error('验证失败，请重试');
    finishLogin(result.accessToken, loginForm.remember);
  } catch (error) {
    notify.error(error);
  } finally {
    loading.value = false;
  }
}

function cancelMfa(): void {
  mfaStep.value = false;
  challengeToken.value = '';
}

async function submitRegister(): Promise<void> {
  if (registerForm.password.length < 12) {
    notify.error('密码至少 12 位');
    return;
  }
  loading.value = true;
  try {
    const result = await api<TokenResponse>('POST', '/auth/register', {
      email: registerForm.email.trim(),
      name: registerForm.name.trim(),
      password: registerForm.password,
    });
    if (result?.accessToken) {
      finishLogin(result.accessToken, true);
      return;
    }
    notify.success('账号已创建，请登录');
    router.push('/login');
  } catch (error) {
    notify.error(error);
  } finally {
    loading.value = false;
  }
}

async function submitSuperadmin(): Promise<void> {
  if (!superForm.code) {
    notify.error('缺少超管邀请码，请使用完整邀请链接');
    return;
  }
  if (superForm.password.length < 12) {
    notify.error('密码至少 12 位');
    return;
  }
  loading.value = true;
  try {
    const result = await api<TokenResponse>('POST', '/auth/register-superadmin', { ...superForm });
    if (result?.accessToken) {
      finishLogin(result.accessToken, true);
      return;
    }
    notify.success('超管账号已创建，请登录');
    router.push('/login');
  } catch (error) {
    notify.error(error);
  } finally {
    loading.value = false;
  }
}

async function submitForgot(): Promise<void> {
  if (!forgotForm.email.trim()) {
    notify.error('请输入邮箱');
    return;
  }
  loading.value = true;
  try {
    const result = await api<TokenResponse>('POST', '/auth/forgot', { email: forgotForm.email.trim() });
    forgotSent.value = true;
    devResetToken.value = result?.resetToken || '';
  } catch (error) {
    notify.error(error);
  } finally {
    loading.value = false;
  }
}

async function submitReset(): Promise<void> {
  if (!resetToken.value) return;
  if (resetForm.password.length < 12) {
    notify.error('密码至少 12 位');
    return;
  }
  if (resetForm.password !== resetForm.password2) {
    notify.error('两次输入的密码不一致');
    return;
  }
  loading.value = true;
  try {
    await api('POST', '/auth/reset', { token: resetToken.value, password: resetForm.password });
    notify.success('密码已重置，请使用新密码登录');
    router.replace('/login');
  } catch (error) {
    notify.error(error);
  } finally {
    loading.value = false;
  }
}

function inviteReason(reason: string): string {
  return { invalid: '无效', expired: '已过期', revoked: '已撤销', used: '已用尽' }[reason] || '不可用';
}

async function checkCompanyInvite(): Promise<void> {
  if (!inviteCode.value) {
    companyInvite.value = null;
    return;
  }
  try {
    companyInvite.value = await api('GET', '/auth/company-invite-status', undefined, { code: inviteCode.value });
  } catch {
    companyInvite.value = { valid: false, reason: 'invalid' };
  }
}

async function submitCompany(): Promise<void> {
  if (!companyForm.companyName.trim()) {
    notify.error('请输入公司名称');
    return;
  }
  if (!companyForm.name.trim() || !companyForm.email.trim()) {
    notify.error('请填写管理员姓名和邮箱');
    return;
  }
  if (companyForm.password.length < 12) {
    notify.error('密码至少 12 位');
    return;
  }
  loading.value = true;
  try {
    const result = await api<TokenResponse>('POST', '/auth/register-company', {
      code: inviteCode.value,
      companyName: companyForm.companyName.trim(),
      companyKind: companyForm.companyKind,
      name: companyForm.name.trim(),
      email: companyForm.email.trim(),
      password: companyForm.password,
    });
    if (!result.accessToken) throw new Error('创建成功但缺少登录令牌');
    finishLogin(result.accessToken, true);
  } catch (error) {
    notify.error(error);
  } finally {
    loading.value = false;
  }
}

async function checkRegistration(): Promise<void> {
  try {
    const result = await api<{ enabled: boolean }>('GET', '/auth/registration-status');
    registrationEnabled.value = result.enabled;
  } catch {
    registrationEnabled.value = false;
  } finally {
    registrationChecked.value = true;
  }
}

onMounted(() => {
  if (mode.value === 'register') checkRegistration();
  else registrationChecked.value = true;
  if (mode.value === 'company') checkCompanyInvite();
});

watch(mode, (value) => {
  mfaStep.value = false;
  if (value === 'register') checkRegistration();
  if (value === 'company') checkCompanyInvite();
});
</script>

<style scoped>
.auth {
  display: grid;
  grid-template-columns: minmax(0, 2fr) minmax(420px, 1fr);
  min-height: 100vh;
  background: var(--tm-bg);
}
.auth-brand {
  position: relative;
  display: flex;
  flex-direction: column;
  padding: 38px 48px;
  color: #eef1ff;
  background:
    radial-gradient(900px 520px at 18% 8%, rgba(109, 139, 255, 0.4), transparent 60%),
    radial-gradient(700px 500px at 90% 100%, rgba(47, 84, 235, 0.55), transparent 62%),
    #0b1023;
  overflow: hidden;
}
.auth-brand::after {
  content: '';
  position: absolute;
  inset: 0;
  background-image: linear-gradient(rgba(255, 255, 255, 0.04) 1px, transparent 1px),
    linear-gradient(90deg, rgba(255, 255, 255, 0.04) 1px, transparent 1px);
  background-size: 56px 56px;
  mask-image: radial-gradient(circle at 40% 40%, #000 20%, transparent 75%);
  pointer-events: none;
}
.brand-top {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 16px;
  font-weight: 700;
  letter-spacing: -0.02em;
  z-index: 1;
}
.mark {
  width: 30px;
  height: 30px;
  display: grid;
  place-items: center;
  border-radius: 8px;
  background: #2f54eb;
  color: #fff;
  font-weight: 800;
}
.brand-copy {
  margin-top: auto;
  margin-bottom: 8vh;
  z-index: 1;
}
.eyebrow {
  margin: 0 0 18px;
  color: #9db2ff;
  font-size: 11px;
  letter-spacing: 0.16em;
}
.brand-copy h1 {
  margin: 0;
  font-size: clamp(30px, 3.2vw, 44px);
  line-height: 1.2;
  letter-spacing: -0.035em;
  font-weight: 650;
}
.lede {
  margin: 20px 0 0;
  max-width: 460px;
  color: #b9c4e6;
  font-size: 14px;
  line-height: 1.8;
}
.points {
  margin: 26px 0 0;
  padding: 0;
  list-style: none;
  display: grid;
  gap: 10px;
  max-width: 440px;
}
.points li {
  position: relative;
  padding-left: 20px;
  color: #c8d2ee;
  font-size: 12.5px;
}
.points li::before {
  content: '';
  position: absolute;
  left: 0;
  top: 8px;
  width: 7px;
  height: 7px;
  border-radius: 2px;
  background: #6d8bff;
}
.brand-foot {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  color: #7f8db5;
  font-size: 11px;
  z-index: 1;
}
.auth-main {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 40px 24px;
}
.auth-card {
  width: 100%;
  max-width: 392px;
}
.mobile-brand {
  display: none;
}
h2 {
  margin: 0;
  font-size: 26px;
  letter-spacing: -0.025em;
  font-weight: 650;
}
.sub {
  margin: 8px 0 26px;
  color: var(--tm-muted);
  font-size: 13px;
}
.notice {
  margin-bottom: 16px;
}
.row-between {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin: -4px 0 18px;
  font-size: 12.5px;
}
.switch-line {
  margin-top: 20px;
  text-align: center;
  color: var(--tm-muted);
  font-size: 12.5px;
}
.switch-line a {
  font-weight: 600;
}
.sep {
  margin: 0 8px;
  opacity: 0.5;
}
.back {
  margin-top: 8px;
}
.ant-form-item {
  margin-bottom: 16px;
}
:deep(.ant-input-affix-wrapper-lg),
:deep(.ant-input-lg) {
  border-radius: 8px;
}
:deep(.ant-btn-lg) {
  height: 42px;
  border-radius: 8px;
  font-weight: 600;
}

@media (max-width: 900px) {
  .auth {
    grid-template-columns: minmax(0, 1fr);
  }
  .auth-brand {
    display: none;
  }
  .auth-main {
    padding: 32px 18px;
  }
  .mobile-brand {
    display: flex;
    align-items: center;
    gap: 9px;
    margin-bottom: 30px;
    font-weight: 700;
    font-size: 15px;
  }
}
</style>
