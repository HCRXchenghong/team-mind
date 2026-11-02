<template>
  <div class="join">
    <div class="join-card">
      <div class="brand"><BrandLogo /></div>
      <template v-if="done">
        <a-result status="success" title="已加入组织" sub-title="现在可以进入团队工作台了。">
          <template #extra>
            <a-button type="primary" @click="router.replace('/teammind/overview')">进入工作台</a-button>
          </template>
        </a-result>
      </template>
      <template v-else>
        <h1>加入组织</h1>
        <p class="sub">接受邀请后即可进入团队工作台。邀请链接一次性使用，过期请联系管理员重新生成。</p>

        <a-alert v-if="error" type="error" show-icon class="alert">{{ error }}</a-alert>

        <template v-if="loggedIn">
          <a-button type="primary" size="large" block :loading="loading" @click="accept">接受邀请</a-button>
        </template>
        <template v-else>
          <a-form layout="vertical" :model="form">
            <a-form-item label="姓名"><a-input v-model:value="form.name" size="large" placeholder="你的名字" /></a-form-item>
            <a-form-item label="邮箱"><a-input v-model:value="form.email" size="large" placeholder="you@company.com" /></a-form-item>
            <a-form-item label="密码">
              <a-input-password v-model:value="form.password" size="large" placeholder="至少 12 位" />
            </a-form-item>
            <a-button type="primary" size="large" block :loading="loading" @click="register">注册并接受邀请</a-button>
          </a-form>
          <div class="switch">已有账号？<router-link :to="`/login?redirect=${encodeURIComponent(route.fullPath)}`">先登录</router-link></div>
        </template>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { api, getToken, setToken } from '@/api/client';
import { useSession } from '@/store/session';
import { notify } from '@/utils/notify';
import BrandLogo from '@/components/BrandLogo.vue';

const route = useRoute();
const router = useRouter();
const session = useSession();
const token = String(route.params.token || '');
const loggedIn = !!getToken();
const loading = ref(false);
const done = ref(false);
const error = ref('');
const form = reactive({ name: '', email: '', password: '' });

async function accept(): Promise<void> {
  loading.value = true;
  error.value = '';
  try {
    await api('POST', '/invitations/accept', { token });
    await session.load();
    done.value = true;
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}

async function register(): Promise<void> {
  if (form.password.length < 12) {
    notify.error('密码至少 12 位');
    return;
  }
  loading.value = true;
  error.value = '';
  try {
    const result = await api<{ accessToken?: string }>('POST', '/auth/register-invited', { ...form, token });
    if (result?.accessToken) setToken(result.accessToken, true);
    done.value = true;
    if (!result?.accessToken) setTimeout(() => router.replace('/login'), 800);
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}
</script>

<style scoped>
.join {
  display: grid;
  place-items: center;
  min-height: 100vh;
  padding: 24px;
  background:
    radial-gradient(800px 400px at 20% 0%, rgba(47, 84, 235, 0.12), transparent 60%),
    var(--tm-bg);
}
.join-card {
  width: 100%;
  max-width: 420px;
  padding: 32px;
  background: var(--tm-panel);
  border: 1px solid var(--tm-border);
  border-radius: 14px;
  box-shadow: var(--tm-shadow-lg);
}
.brand {
  display: flex;
  align-items: center;
  gap: 9px;
  margin-bottom: 26px;
  font-weight: 700;
}
.mark {
  width: 28px;
  height: 28px;
  display: grid;
  place-items: center;
  border-radius: 7px;
  background: var(--tm-primary);
  color: #fff;
  font-weight: 800;
}
h1 {
  margin: 0;
  font-size: 24px;
  letter-spacing: -0.02em;
}
.sub {
  margin: 8px 0 22px;
  color: var(--tm-muted);
  font-size: 12.5px;
  line-height: 1.7;
}
.alert {
  margin-bottom: 16px;
}
.switch {
  margin-top: 18px;
  text-align: center;
  color: var(--tm-muted);
  font-size: 12.5px;
}
.ant-form-item {
  margin-bottom: 14px;
}
</style>
