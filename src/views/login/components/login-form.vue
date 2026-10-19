<template>
  <div class="login-form-wrapper">
    <div class="login-form-title">登录 TeamMind</div>
    <div class="login-form-sub-title">使用你的团队账号继续</div>
    <p v-if="route.query.emailChanged === '1'" class="login-form-success-msg" role="status">邮箱已验证并更新，请使用新邮箱重新登录。</p>
    <p v-if="route.query.accountDeleted === '1'" class="login-form-success-msg" role="status">账号已删除，私人数据已清除。团队共享记录和安全审计以匿名形式保留。</p>
    <p v-if="errorMessage" class="login-form-error-msg" role="alert">{{
      errorMessage
    }}</p>
    <a-form
      v-if="!challengeToken"
      :model="form"
      class="login-form"
      layout="vertical"
      @submit="handleSubmit"
    >
      <a-form-item
        field="email"
        label="邮箱"
        :rules="[
          { required: true, message: '请输入邮箱' },
          { type: 'email', message: '请输入有效邮箱' },
        ]"
        :validate-trigger="['blur', 'change']"
      >
        <a-input
          v-model="form.email"
          autocomplete="username"
          placeholder="name@example.com"
          :max-length="254"
        />
      </a-form-item>
      <a-form-item
        field="password"
        label="密码"
        :rules="[{ required: true, message: '请输入密码' }]"
        :validate-trigger="['blur', 'change']"
      >
        <a-input-password
          v-model="form.password"
          autocomplete="current-password"
          placeholder="输入密码"
        />
      </a-form-item>
      <div class="login-form-password-actions">
        <a-checkbox v-model="form.remember">保持登录</a-checkbox>
        <router-link
          :to="{
            path: '/forgot-password',
            query: { redirect: route.query.redirect },
          }"
          >忘记密码？</router-link
        >
      </div>
      <a-button
        class="login-submit-real"
        type="primary"
        html-type="submit"
        long
        :loading="loading"
        :disabled="loading"
        >登录</a-button
      >
      <div class="login-form-register-btn"
        >还没有账号？<router-link
          :to="{ path: '/register', query: { redirect: route.query.redirect } }"
          >注册账号</router-link
        ></div
      >
    </a-form>
    <div v-else class="login-form-mfa">
      <p>账号 {{ form.email }} 已启用多因素认证。请输入身份验证器中的 6 位动态验证码，或输入一个未使用的恢复码。</p>
      <p v-if="mfaError" class="login-form-error-msg" role="alert">{{ mfaError }}</p>
      <a-form :model="mfaForm" layout="vertical" @submit="handleMFA">
        <a-form-item field="code" label="动态验证码或恢复码" :rules="[{ required: true, message: '请输入动态验证码或恢复码' }]">
          <a-input v-model="mfaForm.code" autocomplete="one-time-code" inputmode="numeric" :max-length="64" />
        </a-form-item>
        <a-button type="primary" html-type="submit" long :loading="loading" :disabled="loading">{{ loading ? '验证中…' : '验证并登录' }}</a-button>
      </a-form>
      <button class="login-form-mfa-reset" type="button" :disabled="loading" @click="restartLogin">返回账号密码</button>
    </div>
  </div>
</template>
<script lang="ts" setup>
  import { ref, reactive } from 'vue';
  import { useRouter, useRoute } from 'vue-router';
  import { useUserStore } from '@/store';
  import { useWorkspaceStore } from '@/stores/workspace';
  import { api } from '@/api/workspace';
  import { setToken } from '@/utils/auth';
  const router = useRouter();
  const route = useRoute();
  const userStore = useUserStore();
  const workspace = useWorkspaceStore();
  const loading = ref(false);
  const errorMessage = ref('');
  const challengeToken = ref('');
  const mfaError = ref('');
  const mfaForm = reactive({ code: '' });
  const form = reactive({ email: '', password: '', remember: false });
  async function handleSubmit({
    errors,
  }: {
    errors: Record<string, unknown> | undefined;
  }) {
    if (errors || loading.value) return;
    loading.value = true;
    errorMessage.value = '';
    try {
      const result = await userStore.login(form);
      if (result?.mfaRequired) {
        if (!result.challengeToken) throw new Error('服务器未返回多因素验证请求，请重新登录');
        challengeToken.value = result.challengeToken;
        mfaForm.code = '';
        return;
      }
      await workspace.load();
      const target = String(route.query.redirect || '/teammind/overview');
      await router.replace(
        target.startsWith('/') && !target.startsWith('//')
          ? target
          : '/teammind/overview'
      );
    } catch (error) {
      errorMessage.value = (error as Error).message;
    } finally {
      loading.value = false;
    }
  }
  async function handleMFA({ errors }: { errors: Record<string, unknown> | undefined }) {
    if (errors || loading.value || !challengeToken.value) return;
    loading.value = true;
    mfaError.value = '';
    try {
      const result = await api<{ accessToken: string; remember: boolean }>(
        'POST',
        '/auth/mfa/complete',
        { challengeToken: challengeToken.value, code: mfaForm.code }
      );
      setToken(result.accessToken, result.remember);
      await userStore.info();
      await workspace.load();
      const target = String(route.query.redirect || '/teammind/overview');
      await router.replace(target.startsWith('/') && !target.startsWith('//') ? target : '/teammind/overview');
    } catch (error) {
      mfaError.value = (error as Error).message;
    } finally {
      loading.value = false;
    }
  }
  function restartLogin() {
    challengeToken.value = '';
    mfaForm.code = '';
    form.password = '';
    mfaError.value = '';
  }
</script>
<style scoped>
  .login-form-wrapper {
    width: min(100%, 360px);
    padding: 16px;
  }
  .login-form-title {
    font-size: 26px;
    font-weight: 650;
    color: var(--color-text-1);
  }
  .login-form-sub-title {
    margin: 8px 0 20px;
    color: var(--color-text-2);
    font-size: 15px;
  }
  .login-form-error-msg {
    padding: 10px;
    color: #b42318;
    background: #fff0ee;
    border-radius: 5px;
  }
  .login-form-success-msg {
    padding: 10px;
    color: #0f7b43;
    background: #e9f8ef;
    border-radius: 5px;
  }
  .login-form-mfa { margin-top: 16px; color: var(--color-text-1); line-height: 1.6; }
  .login-form-mfa-reset { min-height: 44px; margin-top: 12px; padding: 0; color: #165dff; background: transparent; border: 0; font: inherit; cursor: pointer; }
  .login-form-password-actions {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 20px;
  }
  .login-submit-real.arco-btn-primary,
  .login-submit-real.arco-btn-primary.arco-btn-loading,
  .login-submit-real.arco-btn-primary.arco-btn-disabled {
    min-height: 44px;
    color: #fff;
    background: #165dff;
    border: 1px solid #165dff;
    opacity: 1;
    font-size: 16px;
    cursor: pointer;
  }
  .login-submit-real.arco-btn-primary:hover {
    color: #fff;
    background: #0e42d2;
    border-color: #0e42d2;
  }
  .login-submit-real.arco-btn-primary:active {
    background: #0b36ab;
    border-color: #0b36ab;
  }
  .login-submit-real.arco-btn-primary.arco-btn-loading,
  .login-submit-real.arco-btn-primary.arco-btn-disabled {
    background: #375eb0;
    border-color: #375eb0;
    cursor: progress;
  }
  .login-submit-real.arco-btn-primary:focus-visible {
    outline: 3px solid #82b1ff;
    outline-offset: 2px;
  }
  .login-form-register-btn {
    margin-top: 18px;
    text-align: center;
    font-size: 14px;
  }
</style>
