<template>
  <span class="brand-logo" :class="{ 'is-light': light }">
    <img
      v-if="showName"
      class="brand-logo-wordmark"
      :class="{ 'is-light': light }"
      src="/teammind-wordmark.png"
      alt="TeamMind"
    />
    <span v-else class="brand-logo-compact" aria-hidden="true">T</span>
  </span>
</template>

<script setup lang="ts">
withDefaults(
  defineProps<{
    showName?: boolean;
    light?: boolean;
  }>(),
  {
    showName: true,
    light: false,
  },
);
</script>

<style scoped>
.brand-logo {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  color: inherit;
  font-size: 15.5px;
  font-weight: 700;
  letter-spacing: -0.02em;
  line-height: 1;
  white-space: nowrap;
}

.brand-logo-mark {
  display: block;
  width: 30px;
  height: 30px;
  flex: none;
  object-fit: contain;
}

.brand-logo-compact {
  display: inline-grid;
  width: 30px;
  height: 30px;
  flex: none;
  place-items: center;
  color: var(--tm-primary);
  font-size: 21px;
  font-weight: 800;
  letter-spacing: -0.08em;
  line-height: 1;
}

.brand-logo-wordmark {
  display: block;
  width: 122px;
  height: auto;
  max-height: 30px;
  object-fit: contain;
}

.brand-logo-wordmark.is-light {
  filter: brightness(0) invert(1);
}
</style>
