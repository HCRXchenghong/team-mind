<template>
  <a-avatar :size="size" :style="{ background: bg, color: fg, fontSize: size * 0.42 }" class="user-avatar">
    {{ initial }}
  </a-avatar>
</template>

<script setup lang="ts">
import { computed } from 'vue';

const props = withDefaults(defineProps<{ name?: string | null; size?: number }>(), {
  name: '',
  size: 26,
});

const PALETTE = ['#2f54eb', '#0e9384', '#b54708', '#7a5af8', '#c11574', '#175cd3', '#3b7c0f'];

const initial = computed(() => (props.name || '?').trim().slice(0, 1).toUpperCase());

const bg = computed(() => {
  const name = props.name || '';
  let hash = 0;
  for (let i = 0; i < name.length; i += 1) hash = (hash * 31 + name.charCodeAt(i)) % 997;
  return PALETTE[hash % PALETTE.length];
});

const fg = '#ffffff';
</script>

<style scoped>
.user-avatar {
  font-weight: 600;
  flex: none;
}
</style>
