<template>
  <main class="auth-screen">
    <section class="auth-card">
      <router-link to="/login" class="auth-brand">TeamMind</router-link>
      <h1>{{ title }}</h1>
      <p v-if="notice" role="status">{{ notice }}</p>
      <p v-if="error" class="auth-error" role="alert">{{ error }}</p>
      <form @submit.prevent="submit">
        <label v-if="mode !== 'reset'">邮箱<input v-model.trim="form.email" type="email" autocomplete="email" required maxlength="254" /></label>
        <label v-if="mode === 'register'">姓名<input v-model.trim="form.name" autocomplete="name" required minlength="2" maxlength="120" /></label>
        <label v-if="mode !== 'forgot'">{{ mode === 'reset' ? '新密码' : '密码' }}<input v-model="form.password" type="password" :autocomplete="mode === 'register' ? 'new-password' : 'new-password'" required minlength="12" maxlength="72" /></label>
        <button type="submit" :disabled="busy">{{ busy ? '处理中…' : title }}</button>
      </form>
      <router-link to="/login">返回登录</router-link>
    </section>
  </main>
</template>
<script setup lang="ts">
import { computed, reactive, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import axios from 'axios';
const route = useRoute(); const router = useRouter();
const mode = computed(() => route.name === 'register' ? 'register' : route.name === 'forgot' ? 'forgot' : 'reset');
const title = computed(() => ({ register: '注册账号', forgot: '找回密码', reset: '重置密码' }[mode.value]));
const form = reactive({ email: '', name: '', password: '' });
const busy = ref(false); const error = ref(''); const notice = ref('');
async function submit() {
  if (busy.value) return;
  busy.value = true; error.value = ''; notice.value = '';
  try {
    if (mode.value === 'register') {
      await axios.post('/auth/register', form);
      await router.replace({ path: '/login', query: { redirect: route.query.redirect || '/teammind/overview' } });
    } else if (mode.value === 'forgot') {
      const response = await axios.post('/auth/forgot', { email: form.email });
      const resetToken = response.data.resetToken;
      notice.value = resetToken ? '开发模式：已生成一次性重置链接。' : '如果账号存在，重置说明将发送到该邮箱。';
      if (resetToken) await router.replace(`/reset-password?token=${encodeURIComponent(resetToken)}`);
    } else {
      await axios.post('/auth/reset', { token: String(route.query.token || ''), password: form.password });
      await router.replace('/login');
    }
  } catch (e) { error.value = (e as Error).message; }
  finally { busy.value = false; }
}
</script>
<style scoped>
.auth-screen { min-height: 100dvh; display: grid; place-items: center; padding: 24px; background: #f2f3f5; }
.auth-card { width: min(100%, 420px); padding: 32px; background: white; border-radius: 12px; box-shadow: 0 10px 35px #17255412; }
.auth-brand { font-weight: 700; font-size: 22px; }
h1 { margin: 22px 0; font-size: 25px; }
label { display: block; margin: 16px 0; font-size: 14px; }
input { display: block; width: 100%; box-sizing: border-box; min-height: 44px; margin-top: 7px; padding: 9px; border: 1px solid #b8c2cc; border-radius: 6px; font-size: 16px; }
button { width: 100%; min-height: 44px; margin: 12px 0; color: white; background: #165dff; border: 0; border-radius: 6px; font-size: 16px; }
button:disabled { opacity: .65; }
.auth-error { color: #b42318; }
</style>
