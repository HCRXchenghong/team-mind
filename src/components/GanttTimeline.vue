<template>
  <div ref="wrapRef" class="gantt" :class="{ compact, editable, linking: !!linking }">
    <div class="g-months">
      <div class="g-corner sticky-left">
        <span class="muted">{{ tasks.length }} 个任务</span>
      </div>
      <div class="g-month-track" :style="{ width: `${totalWidth}px` }">
        <div v-for="month in months" :key="month.key" class="g-month" :style="{ width: `${month.days * DAY}px` }">
          {{ month.label }}
        </div>
      </div>
    </div>

    <div class="g-days">
      <div class="g-corner sticky-left small">任务 / 负责人</div>
      <div class="g-day-track" :style="{ width: `${totalWidth}px` }">
        <div
          v-for="day in days"
          :key="day.iso"
          class="g-day"
          :class="{ today: day.today, weekend: day.weekend }"
          :title="day.iso"
        >
          {{ day.day }}
        </div>
      </div>
    </div>

    <div ref="bodyRef" class="g-body">
      <svg class="g-links" :width="LEFT + totalWidth" :height="Math.max(1, tasks.length * ROW_H)" aria-hidden="true">
        <defs>
          <marker id="g-arrow" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="6" markerHeight="6" orient="auto">
            <path d="M0,0 L8,4 L0,8 z" fill="currentColor" />
          </marker>
        </defs>
        <path
          v-for="link in dependencyPaths"
          :key="link.key"
          class="g-link-path"
          :d="link.d"
          marker-end="url(#g-arrow)"
          @click.stop="$emit('unlink', { taskId: link.targetId, dependsOnTaskId: link.sourceId })"
        >
          <title>{{ link.label }}</title>
        </path>
        <path v-if="tempLinkPath" class="g-link-path temp" :d="tempLinkPath" />
      </svg>

      <div
        v-for="task in tasks"
        :key="task.id"
        class="g-row"
        :class="{ 'link-target': linkHover === task.id, 'is-dragging': preview?.id === task.id }"
        :data-task-id="task.id"
        @click="onRowClick(task)"
      >
        <div class="g-left sticky-left">
          <div class="g-title" :title="task.title">
            <span v-if="task.milestone" class="milestone-dot" title="里程碑" />
            {{ task.title }}
          </div>
          <div class="g-meta">
            <span>{{ task.assigneeName || '未分配' }}</span>
            <span>·</span>
            <span>{{ task.progress || 0 }}%</span>
            <span v-if="depCount(task.id)" class="dep">· 前置 {{ depCount(task.id) }}</span>
          </div>
        </div>
        <div class="g-track" :style="{ width: `${totalWidth}px` }">
          <span
            v-for="day in days"
            :key="day.iso"
            class="g-tick"
            :class="{ today: day.today, weekend: day.weekend }"
          />
          <template v-if="barStyle(task)">
            <div
              class="g-bar"
              :class="barClass(task)"
              :style="barStyle(task) || undefined"
              :title="`${task.title} · ${fmtDate(displayDates(task).start)} → ${fmtDate(displayDates(task).due)}`"
              @pointerdown="onBarDown($event, task, 'move')"
              @click.stop="onBarClick(task)"
            >
              <span v-if="task.milestone" class="diamond">◆</span>
              <span class="bar-progress" :style="{ width: `${Math.min(100, task.progress || 0)}%` }" />
              <span v-if="editable" class="g-handle left" @pointerdown.stop="onBarDown($event, task, 'start')" />
              <span v-if="editable" class="g-handle right" @pointerdown.stop="onBarDown($event, task, 'end')" />
              <span
                v-if="editable"
                class="g-link-dot"
                title="拖到另一个任务，建立依赖关系"
                @pointerdown.stop="startLink($event, task)"
              />
            </div>
            <div
              v-if="preview?.id === task.id"
              class="g-drag-tip"
              :style="{ left: dragTipLeft(task) }"
            >
              {{ fmtDate(preview.start) }} → {{ fmtDate(preview.due) }}
            </div>
          </template>
          <span v-else class="g-unscheduled">未排期</span>
        </div>
      </div>

      <div v-if="!tasks.length" class="g-empty muted">当前筛选范围没有任务</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue';
import dayjs, { type Dayjs } from 'dayjs';
import type { Task } from '@/api/types';
import { fmtDate } from '@/utils/format';

const props = withDefaults(
  defineProps<{
    tasks: Task[];
    from?: string;
    to?: string;
    dependencies?: Record<string, string[]>;
    compact?: boolean;
    editable?: boolean;
  }>(),
  { from: '', to: '', dependencies: () => ({}), compact: false, editable: false },
);

const emit = defineEmits<{
  (event: 'select', task: Task): void;
  (event: 'reschedule', payload: { task: Task; startDate: string; dueDate: string }): void;
  (event: 'link', payload: { from: string; to: string }): void;
  (event: 'unlink', payload: { taskId: string; dependsOnTaskId: string }): void;
}>();

const DAY = 30;
const LEFT = 248;
const ROW_H = 48;
const MAX_DAYS = 120;

const wrapRef = ref<HTMLElement | null>(null);
const bodyRef = ref<HTMLElement | null>(null);
const preview = ref<{ id: string; start: string; due: string } | null>(null);
const linking = ref<{ from: string; x: number; y: number } | null>(null);
const linkHover = ref('');

let dragState: {
  id: string;
  mode: 'move' | 'start' | 'end';
  startX: number;
  origStart: Dayjs;
  origDue: Dayjs;
  moved: boolean;
} | null = null;
let suppressClick = false;

const rangeStart = computed(() => {
  if (props.from) return dayjs(props.from).startOf('day');
  const dates = props.tasks
    .map((task) => task.startDate || task.dueDate)
    .filter(Boolean)
    .sort();
  const first = dates[0];
  return first ? dayjs(first).startOf('day') : dayjs().startOf('day');
});

const rangeEnd = computed(() => {
  const start = rangeStart.value;
  if (props.to) {
    const end = dayjs(props.to).startOf('day');
    return end.isBefore(start) ? start.add(14, 'day') : end;
  }
  const dates = props.tasks
    .map((task) => task.dueDate || task.startDate)
    .filter(Boolean)
    .sort();
  const last = dates[dates.length - 1];
  const end = last ? dayjs(last).startOf('day') : start.add(14, 'day');
  const padded = end.add(7, 'day');
  return padded.diff(start, 'day') > MAX_DAYS ? start.add(MAX_DAYS, 'day') : padded;
});

const dayCount = computed(() => Math.max(1, rangeEnd.value.diff(rangeStart.value, 'day') + 1));

const days = computed(() => {
  const start = rangeStart.value;
  return Array.from({ length: dayCount.value }, (_, index) => {
    const date = start.add(index, 'day');
    return {
      iso: date.format('YYYY-MM-DD'),
      day: date.date(),
      weekend: [0, 6].includes(date.day()),
      today: date.isSame(dayjs(), 'day'),
    };
  });
});

const totalWidth = computed(() => dayCount.value * DAY);

const months = computed(() => {
  const start = rangeStart.value;
  const result: Array<{ key: string; label: string; days: number }> = [];
  let cursor = 0;
  while (cursor < dayCount.value) {
    const date = start.add(cursor, 'day');
    const span = Math.min(date.endOf('month').diff(date, 'day') + 1, dayCount.value - cursor);
    result.push({ key: date.format('YYYY-MM'), label: date.format('YYYY 年 M 月'), days: span });
    cursor += span;
  }
  return result;
});

function depCount(taskId: string): number {
  return props.dependencies[taskId]?.length || 0;
}

function displayDates(task: Task): { start: string; due: string } {
  if (preview.value?.id === task.id) return { start: preview.value.start, due: preview.value.due };
  return { start: task.startDate || task.dueDate || '', due: task.dueDate || task.startDate || '' };
}

function barClass(task: Task): string[] {
  return [`s-${task.status}`, task.milestone ? 'is-milestone' : ''];
}

function barStyle(task: Task): Record<string, string> | null {
  const dates = displayDates(task);
  if (!dates.start) return null;
  const from = dayjs(dates.start).startOf('day');
  const to = dayjs(dates.due || dates.start).startOf('day');
  const left = Math.max(0, from.diff(rangeStart.value, 'day')) * DAY;
  const span = Math.max(1, to.diff(from, 'day') + 1);
  const maxSpan = dayCount.value - left / DAY;
  const width = Math.max(14, Math.min(span, maxSpan) * DAY - 6);
  return { left: `${left + 3}px`, width: `${width}px` };
}

function dragTipLeft(task: Task): string {
  return barStyle(task)?.left || '0px';
}

function geometry(task: Task): { left: number; width: number } | null {
  const style = barStyle(task);
  if (!style) return null;
  return { left: Number.parseFloat(style.left), width: Number.parseFloat(style.width) };
}

/* ---------- 依赖连线 ---------- */
const dependencyPaths = computed(() => {
  const index = new Map(props.tasks.map((task, position) => [task.id, position]));
  const paths: Array<{ key: string; d: string; label: string; targetId: string; sourceId: string }> = [];
  Object.entries(props.dependencies).forEach(([targetId, sources]) => {
    const targetIndex = index.get(targetId);
    const target = props.tasks.find((task) => task.id === targetId);
    if (targetIndex === undefined || !target) return;
    const targetGeo = geometry(target);
    if (!targetGeo) return;
    (sources || []).forEach((sourceId) => {
      if (sourceId === targetId) return;
      const sourceIndex = index.get(sourceId);
      const source = props.tasks.find((task) => task.id === sourceId);
      if (sourceIndex === undefined || !source) return;
      const sourceGeo = geometry(source);
      if (!sourceGeo) return;
      const sx = LEFT + sourceGeo.left + sourceGeo.width;
      const sy = sourceIndex * ROW_H + ROW_H / 2;
      const tx = LEFT + targetGeo.left;
      const ty = targetIndex * ROW_H + ROW_H / 2;
      const curve = Math.max(22, Math.min(70, Math.abs(tx - sx) / 2));
      paths.push({
        key: `${sourceId}-${targetId}`,
        label: `${source.title} → ${target.title}（点击删除依赖）`,
        targetId,
        sourceId,
        d: `M ${sx} ${sy} C ${sx + curve} ${sy}, ${tx - curve} ${ty}, ${tx} ${ty}`,
      });
    });
  });
  return paths;
});

const tempLinkPath = computed(() => {
  if (!linking.value) return '';
  const source = props.tasks.find((task) => task.id === linking.value?.from);
  if (!source) return '';
  const index = props.tasks.findIndex((task) => task.id === source.id);
  const geo = geometry(source);
  if (!geo || index < 0) return '';
  const sx = LEFT + geo.left + geo.width;
  const sy = index * ROW_H + ROW_H / 2;
  const tx = linking.value.x;
  const ty = linking.value.y;
  const curve = Math.max(20, Math.min(70, Math.abs(tx - sx) / 2));
  return `M ${sx} ${sy} C ${sx + curve} ${sy}, ${tx - curve} ${ty}, ${tx} ${ty}`;
});

function startLink(event: PointerEvent, task: Task): void {
  if (!props.editable) return;
  event.preventDefault();
  event.stopPropagation();
  const rect = bodyRef.value?.getBoundingClientRect();
  if (!rect) return;
  linking.value = {
    from: task.id,
    x: event.clientX - rect.left,
    y: event.clientY - rect.top,
  };
  window.addEventListener('pointermove', onLinkMove);
  window.addEventListener('pointerup', onLinkUp);
  window.addEventListener('keydown', onLinkKey);
}

function onLinkMove(event: PointerEvent): void {
  if (!linking.value) return;
  const rect = bodyRef.value?.getBoundingClientRect();
  if (!rect) return;
  linking.value = {
    ...linking.value,
    x: event.clientX - rect.left,
    y: event.clientY - rect.top,
  };
  const element = document.elementFromPoint(event.clientX, event.clientY);
  const row = element?.closest('.g-row') as HTMLElement | null;
  const id = row?.dataset.taskId || '';
  linkHover.value = id && id !== linking.value.from ? id : '';
}

function onLinkUp(): void {
  if (linking.value && linkHover.value && linkHover.value !== linking.value.from) {
    emit('link', { from: linking.value.from, to: linkHover.value });
  }
  cleanupLink();
}

function onLinkKey(event: KeyboardEvent): void {
  if (event.key === 'Escape') cleanupLink();
}

function cleanupLink(): void {
  linking.value = null;
  linkHover.value = '';
  window.removeEventListener('pointermove', onLinkMove);
  window.removeEventListener('pointerup', onLinkUp);
  window.removeEventListener('keydown', onLinkKey);
}

/* ---------- 拖拽改期 ---------- */
function onBarDown(event: PointerEvent, task: Task, mode: 'move' | 'start' | 'end'): void {
  if (!props.editable || event.button !== 0) return;
  const dates = displayDates(task);
  if (!dates.start) return;
  event.preventDefault();
  event.stopPropagation();
  dragState = {
    id: task.id,
    mode,
    startX: event.clientX,
    origStart: dayjs(dates.start),
    origDue: dayjs(dates.due || dates.start),
    moved: false,
  };
  preview.value = { id: task.id, start: dates.start, due: dates.due || dates.start };
  window.addEventListener('pointermove', onDragMove);
  window.addEventListener('pointerup', onDragUp);
  window.addEventListener('keydown', onDragKey);
}

function onDragMove(event: PointerEvent): void {
  if (!dragState) return;
  const deltaDays = Math.round((event.clientX - dragState.startX) / DAY);
  if (Math.abs(event.clientX - dragState.startX) > 3) dragState.moved = true;
  let start = dragState.origStart;
  let due = dragState.origDue;
  if (dragState.mode === 'move') {
    start = start.add(deltaDays, 'day');
    due = due.add(deltaDays, 'day');
  } else if (dragState.mode === 'start') {
    start = start.add(deltaDays, 'day');
    if (start.isAfter(due)) start = due;
  } else {
    due = due.add(deltaDays, 'day');
    if (due.isBefore(start)) due = start;
  }
  preview.value = {
    id: dragState.id,
    start: start.format('YYYY-MM-DD'),
    due: due.format('YYYY-MM-DD'),
  };
}

function onDragUp(): void {
  if (!dragState) return;
  const state = dragState;
  const current = preview.value;
  dragState = null;
  cleanupDrag();
  preview.value = null;
  if (!current || !state.moved) return;
  const task = props.tasks.find((item) => item.id === state.id);
  if (!task) return;
  suppressClick = true;
  window.setTimeout(() => {
    suppressClick = false;
  }, 60);
  if (current.start === (task.startDate || task.dueDate) && current.due === (task.dueDate || task.startDate)) return;
  emit('reschedule', { task, startDate: current.start, dueDate: current.due });
}

function onDragKey(event: KeyboardEvent): void {
  if (event.key !== 'Escape' || !dragState) return;
  dragState = null;
  preview.value = null;
  cleanupDrag();
}

function cleanupDrag(): void {
  window.removeEventListener('pointermove', onDragMove);
  window.removeEventListener('pointerup', onDragUp);
  window.removeEventListener('keydown', onDragKey);
}

/* ---------- 点击 ---------- */
function onRowClick(task: Task): void {
  if (suppressClick) {
    suppressClick = false;
    return;
  }
  emit('select', task);
}

function onBarClick(task: Task): void {
  if (suppressClick) {
    suppressClick = false;
    return;
  }
  emit('select', task);
}

onBeforeUnmount(() => {
  cleanupDrag();
  cleanupLink();
});
</script>

<style scoped>
.gantt {
  overflow: auto;
  border: 1px solid var(--tm-border);
  border-radius: 8px;
  background: var(--tm-panel);
  max-height: 65vh;
}
.gantt.linking {
  cursor: crosshair;
}
.g-months,
.g-days,
.g-row {
  display: flex;
  min-width: max-content;
}
.g-months {
  position: sticky;
  top: 0;
  z-index: 6;
  height: 30px;
  background: var(--tm-panel-2);
  border-bottom: 1px solid var(--tm-border);
}
.g-days {
  position: sticky;
  top: 30px;
  z-index: 5;
  height: 32px;
  background: var(--tm-panel);
  border-bottom: 1px solid var(--tm-border);
}
.g-corner {
  width: 248px;
  min-width: 248px;
  display: flex;
  align-items: center;
  padding: 0 14px;
  font-size: 12px;
  background: var(--tm-panel-2);
  border-right: 1px solid var(--tm-border);
  z-index: 7;
}
.g-corner.small {
  font-size: 11px;
  color: var(--tm-muted);
  background: var(--tm-panel);
}
.sticky-left {
  position: sticky;
  left: 0;
}
.g-month-track {
  display: flex;
}
.g-month {
  flex: none;
  display: flex;
  align-items: center;
  padding-left: 10px;
  font-size: 11px;
  color: var(--tm-muted);
  border-right: 1px solid var(--tm-border);
  white-space: nowrap;
  overflow: hidden;
}
.g-day-track {
  display: flex;
}
.g-day {
  width: 30px;
  min-width: 30px;
  display: grid;
  place-items: center;
  font-size: 10.5px;
  color: var(--tm-muted);
  border-right: 1px solid var(--tm-border);
}
.g-day.weekend {
  background: color-mix(in srgb, var(--tm-panel-2) 70%, transparent);
}
.g-day.today {
  color: var(--tm-primary);
  font-weight: 700;
  background: var(--tm-primary-soft);
}
.g-body {
  position: relative;
}
.g-links {
  position: absolute;
  top: 0;
  left: 0;
  z-index: 1;
  pointer-events: none;
  color: var(--tm-primary);
  overflow: visible;
}
.g-link-path {
  fill: none;
  stroke: currentColor;
  stroke-width: 1.4;
  opacity: 0.45;
  pointer-events: stroke;
  cursor: pointer;
}
.g-link-path:hover {
  opacity: 1;
  stroke-width: 2;
}
.g-link-path.temp {
  opacity: 0.9;
  stroke-dasharray: 4 3;
  pointer-events: none;
}
.g-row {
  border-bottom: 1px solid var(--tm-border);
  cursor: pointer;
  position: relative;
  z-index: 2;
}
.g-row:last-child {
  border-bottom: 0;
}
.g-row:hover .g-left {
  background: var(--tm-panel-2);
}
.g-row.is-dragging .g-bar {
  opacity: 1;
  box-shadow: 0 4px 14px rgba(20, 28, 45, 0.28);
}
.g-row.link-target {
  background: var(--tm-primary-soft);
}
.g-row.link-target .g-left {
  background: var(--tm-primary-soft);
}
.g-left {
  width: 248px;
  min-width: 248px;
  padding: 8px 14px;
  background: var(--tm-panel);
  border-right: 1px solid var(--tm-border);
  z-index: 3;
}
.g-title {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--tm-text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.g-meta {
  display: flex;
  gap: 5px;
  margin-top: 2px;
  color: var(--tm-muted);
  font-size: 11px;
  white-space: nowrap;
}
.dep {
  color: var(--tm-primary);
}
.milestone-dot {
  display: inline-block;
  width: 6px;
  height: 6px;
  margin-right: 5px;
  background: #b54708;
  transform: rotate(45deg);
  vertical-align: 1px;
}
.g-track {
  position: relative;
  display: flex;
  height: 48px;
  flex: none;
}
.g-tick {
  width: 30px;
  min-width: 30px;
  border-right: 1px solid color-mix(in srgb, var(--tm-border) 55%, transparent);
}
.g-tick.weekend {
  background: color-mix(in srgb, var(--tm-panel-2) 45%, transparent);
}
.g-tick.today {
  background: var(--tm-primary-soft);
}
.g-bar {
  position: absolute;
  top: 15px;
  height: 18px;
  border-radius: 5px;
  background: var(--tm-primary);
  opacity: 0.92;
  display: flex;
  align-items: center;
  padding-left: 6px;
  box-shadow: 0 1px 2px rgba(20, 28, 45, 0.14);
  z-index: 3;
  user-select: none;
  touch-action: none;
}
.editable .g-bar {
  cursor: grab;
}
.g-bar:active {
  cursor: grabbing;
}
.g-bar:hover {
  opacity: 1;
  outline: 2px solid color-mix(in srgb, var(--tm-primary) 30%, transparent);
}
.g-bar.s-done {
  background: #12b76a;
}
.g-bar.s-blocked {
  background: #f04438;
}
.g-bar.s-review {
  background: #f79009;
}
.g-bar.s-todo,
.g-bar.s-cancelled {
  background: var(--tm-border-strong);
}
.g-bar.is-milestone {
  border-radius: 3px;
}
.bar-progress {
  position: absolute;
  left: 0;
  top: 0;
  bottom: 0;
  border-radius: 4px;
  background: rgba(255, 255, 255, 0.24);
  pointer-events: none;
}
.diamond {
  position: relative;
  z-index: 2;
  color: #fff;
  font-size: 10px;
}
.g-handle {
  position: absolute;
  top: -2px;
  bottom: -2px;
  width: 8px;
  border-radius: 4px;
  background: rgba(255, 255, 255, 0.7);
  border: 1px solid rgba(20, 28, 45, 0.12);
  opacity: 0;
  cursor: ew-resize;
  z-index: 4;
}
.g-handle.left {
  left: -5px;
}
.g-handle.right {
  right: -5px;
}
.g-bar:hover .g-handle {
  opacity: 1;
}
.g-link-dot {
  position: absolute;
  right: -10px;
  top: 50%;
  width: 9px;
  height: 9px;
  margin-top: -4.5px;
  border-radius: 50%;
  background: var(--tm-panel);
  border: 2px solid var(--tm-primary);
  cursor: crosshair;
  opacity: 0;
  z-index: 5;
}
.g-bar:hover .g-link-dot,
.gantt.linking .g-link-dot {
  opacity: 1;
}
@media (hover: none) {
  .editable .g-handle,
  .editable .g-link-dot {
    opacity: 0.75;
  }
}
.g-drag-tip {
  position: absolute;
  top: -12px;
  transform: translateY(-100%);
  padding: 3px 7px;
  border-radius: 5px;
  background: var(--tm-text);
  color: var(--tm-panel);
  font-size: 10.5px;
  white-space: nowrap;
  z-index: 6;
  pointer-events: none;
  box-shadow: 0 4px 12px rgba(20, 28, 45, 0.2);
}
.g-unscheduled {
  position: sticky;
  left: 260px;
  align-self: center;
  color: var(--tm-muted);
  font-size: 11px;
}
.g-empty {
  padding: 40px;
  text-align: center;
}
</style>
