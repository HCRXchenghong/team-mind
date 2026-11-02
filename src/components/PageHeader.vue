<template>
  <div class="page-head">
    <div class="titles">
      <h1>{{ title }}</h1>
      <p v-if="subtitle" class="sub">{{ subtitle }}</p>
    </div>
    <div class="actions">
      <slot name="actions" />
    </div>
  </div>
</template>

<script setup lang="ts">
defineProps<{ title: string; subtitle?: string }>();
</script>

<style scoped>
.titles {
  min-width: 0;
}
.actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
</style>
