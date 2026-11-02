<template>
  <a-tag :color="color" class="status-tag">
    <span v-if="dot" class="dot" />
    {{ text }}
  </a-tag>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import {
  labelOf,
  priorityColor,
  PROJECT_STATUS,
  projectStatusColor,
  proposalStatusColor,
  PROPOSAL_STATUS_LABEL,
  TASK_PRIORITY,
  TASK_STATUS,
  taskStatusColor,
} from '@/utils/labels';

const props = withDefaults(
  defineProps<{
    kind: 'task' | 'priority' | 'project' | 'proposal';
    value?: string | null;
    dot?: boolean;
  }>(),
  { value: '', dot: false },
);

const color = computed(() => {
  switch (props.kind) {
    case 'priority':
      return priorityColor(props.value || '');
    case 'project':
      return projectStatusColor(props.value || '');
    case 'proposal':
      return proposalStatusColor(props.value || '');
    default:
      return taskStatusColor(props.value || '');
  }
});

const text = computed(() => {
  switch (props.kind) {
    case 'priority':
      return labelOf(TASK_PRIORITY, props.value);
    case 'project':
      return labelOf(PROJECT_STATUS, props.value);
    case 'proposal':
      return PROPOSAL_STATUS_LABEL[props.value || ''] || props.value || '—';
    default:
      return labelOf(TASK_STATUS, props.value);
  }
});
</script>

<style scoped>
.status-tag {
  margin-inline-end: 0;
  border-radius: 999px;
  font-size: 11.5px;
  line-height: 20px;
  padding-inline: 8px;
}
.dot {
  display: inline-block;
  width: 5px;
  height: 5px;
  margin-right: 5px;
  border-radius: 50%;
  background: currentColor;
  vertical-align: 1px;
}
</style>
