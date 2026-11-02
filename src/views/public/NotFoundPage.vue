<template>
  <div class="notfound">
    <div class="brand"><BrandLogo /></div>
    <a-result status="404" title="页面不存在" sub-title="地址可能已变更，或者这个页面不属于当前工作台。">
      <template #extra>
        <a-button type="primary" @click="router.replace('/teammind/overview')">返回工作台</a-button>
        <a-button @click="router.back()">返回上一页</a-button>
      </template>
    </a-result>
  </div>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router';
import BrandLogo from '@/components/BrandLogo.vue';
const router = useRouter();
</script>

<style scoped>
.notfound {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  min-height: 100vh;
  background: var(--tm-bg);
}
.brand {
  display: flex;
  align-items: center;
  gap: 9px;
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
</style>
