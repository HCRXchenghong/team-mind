<template>
  <main class="email-confirm-screen">
    <section class="email-confirm-card" aria-labelledby="email-confirm-title">
      <router-link to="/login" class="brand">TeamMind</router-link>
      <h1 id="email-confirm-title">验证新邮箱</h1>
      <p v-if="busy" role="status">正在验证邮箱，请稍候…</p>
      <p v-else-if="success" class="success" role="status">邮箱已验证并更新，所有已登录设备已退出。请使用新邮箱重新登录。</p>
      <p v-else class="error" role="alert">{{ error || '验证链接无效或已过期。' }}</p>
      <div class="actions">
        <button v-if="!success && token" type="button" :disabled="busy" @click="confirm">重试验证</button>
        <router-link v-if="success || !token" to="/login">返回登录</router-link>
        <router-link v-if="!success" to="/teammind/settings">返回设置</router-link>
      </div>
    </section>
  </main>
</template>

<script setup lang="ts">
  import { onMounted, ref } from 'vue';
  import { useRoute, useRouter } from 'vue-router';
  import { api } from '@/api/workspace';

  const route = useRoute();
  const router = useRouter();
  const token = ref('');
  const busy = ref(false);
  const success = ref(false);
  const error = ref('');

  async function confirm() {
    if (busy.value || !token.value) return;
    busy.value = true;
    error.value = '';
    try {
      await api<{ changed: boolean; sessionsRevoked: boolean }>(
        'POST',
        '/me/email-change/confirm',
        { token: token.value }
      );
      success.value = true;
      token.value = '';
      await router.replace({ path: '/login', query: { emailChanged: '1' } });
    } catch (cause) {
      error.value = (cause as Error).message || '邮箱验证失败，请检查链接后重试。';
    } finally {
      busy.value = false;
    }
  }

  onMounted(() => {
    const prefix = '#verifyEmailToken=';
    if (!route.hash.startsWith(prefix)) {
      error.value = '验证链接缺少令牌，请从邮箱中的最新验证邮件重新打开。';
      return;
    }
    try {
      token.value = decodeURIComponent(route.hash.slice(prefix.length));
    } catch {
      error.value = '验证链接格式无效，请重新发起邮箱变更。';
      return;
    }
    // Keep the bearer token out of the address bar and browser history before
    // making any request. It remains only in this component's memory for retry.
    void router.replace({ path: route.path, query: route.query, hash: '' }).then(confirm);
  });
</script>

<style scoped>
  .email-confirm-screen { min-height: 100dvh; display: grid; place-items: center; padding: 24px; background: #f2f3f5; }
  .email-confirm-card { width: min(100%, 440px); padding: 32px; border-radius: 12px; background: #fff; box-shadow: 0 10px 35px #17255412; }
  .brand { color: #165dff; font-size: 20px; font-weight: 700; }
  h1 { margin: 22px 0; font-size: 24px; }
  p { line-height: 1.65; overflow-wrap: anywhere; }
  .success { color: #087443; }
  .error { color: #b42318; }
  .actions { display: flex; flex-wrap: wrap; gap: 16px; align-items: center; margin-top: 24px; }
  button { min-height: 44px; padding: 0 18px; color: #fff; background: #165dff; border: 0; border-radius: 6px; font: inherit; cursor: pointer; }
  button:disabled { opacity: .65; cursor: wait; }
  @media (max-width: 480px) { .email-confirm-card { padding: 24px; } }
</style>
