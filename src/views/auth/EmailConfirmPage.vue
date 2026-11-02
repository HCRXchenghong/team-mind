<template>
  <div class="confirm">
    <div class="card">
      <div class="brand"><BrandLogo /></div>
      <a-spin :spinning="status === 'loading'">
        <a-result
          v-if="status === 'success'"
          status="success"
          title="邮箱已更新"
          sub-title="请使用新邮箱重新登录，旧会话已失效。"
        >
          <template #extra>
            <a-button type="primary" @click="router.replace('/login')">去登录</a-button>
          </template>
        </a-result>
        <a-result
          v-else-if="status === 'error'"
          status="error"
          title="验证失败"
          :sub-title="message || '链接无效或已过期，请回到设置页重新发送确认邮件。'"
        >
          <template #extra>
            <a-button @click="router.replace('/login')">返回登录</a-button>
          </template>
        </a-result>
        <div v-else class="loading">
          <a-spin />
          <p>正在确认邮箱…</p>
        </div>
      </a-spin>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { api, clearToken } from '@/api/client';
import BrandLogo from '@/components/BrandLogo.vue';

const route = useRoute();
const router = useRouter();
const status = ref<'loading' | 'success' | 'error'>('loading');
const message = ref('');

onMounted(async () => {
  const token = String(route.query.token || '');
  if (!token) {
    status.value = 'error';
    message.value = '链接缺少确认 token。';
    return;
  }
  try {
    await api('POST', '/me/email-change/confirm', { token });
    clearToken();
    status.value = 'success';
  } catch (error) {
    status.value = 'error';
    message.value = (error as Error).message;
  }
});
</script>

<style scoped>
.confirm {
  display: grid;
  place-items: center;
  min-height: 100vh;
  padding: 24px;
  background: var(--tm-bg);
}
.card {
  width: 100%;
  max-width: 440px;
  padding: 30px;
  background: var(--tm-panel);
  border: 1px solid var(--tm-border);
  border-radius: 14px;
  box-shadow: var(--tm-shadow-lg);
}
.brand {
  display: flex;
  align-items: center;
  gap: 9px;
  margin-bottom: 20px;
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
.loading {
  padding: 40px 0;
  text-align: center;
  color: var(--tm-muted);
}
</style>
