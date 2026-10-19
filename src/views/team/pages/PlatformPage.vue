<template
  ><section class="tm-page"
    ><header class="tm-header"
      ><div
        ><h1>平台管理</h1
        ><p>仅平台超管可审核团队；私人对话和个人记忆不可见</p></div
      ><a-button :loading="loading" @click="refresh">刷新</a-button></header
    ><div v-if="error" class="tm-error">{{ error }}</div
    ><div v-if="loading && !items.length" class="tm-empty">正在加载团队…</div
    ><div v-else-if="!items.length" class="tm-empty">暂无团队记录</div
    ><article v-for="t in items" :key="t.id" class="tm-panel"
      ><h2>{{ t.name }}</h2
      ><p>{{ t.status }} · {{ formatDateTime(t.createdAt, user.timezone) }}</p
      ><p v-if="t.reviewReason">审核说明：{{ t.reviewReason }}</p
      ><div v-if="t.status === 'pending_review'" class="tm-actions"
        ><a-button
          type="primary"
          :loading="reviewingId === t.id"
          :disabled="!!reviewingId"
          @click="review(t, 'approved')"
          >批准</a-button
        ><a-button
          status="danger"
          :loading="reviewingId === t.id && reviewingDecision === 'rejected'"
          :disabled="!!reviewingId"
          @click="review(t, 'rejected')"
          >拒绝</a-button
        ></div
      ></article
    ><section class="tm-panel tm-platform-audit"
      ><h2>最近平台审核记录</h2
      ><div v-if="!auditRows.length" class="tm-empty">暂无审核记录</div
      ><article
        v-for="row in auditRows"
        :key="row.id"
        class="tm-platform-audit-row"
        ><strong
          >{{
            row.action === 'team.review.approved' ? '批准团队' : '拒绝团队'
          }}
          · {{ row.teamName }}</strong
        ><span
          >{{ formatDateTime(row.createdAt, user.timezone) }} · 操作人
          {{ row.actorId }}</span
        ><p v-if="row.reason">审核说明：{{ row.reason }}</p></article
      ></section
    ><section class="tm-panel tm-security-alerts"
      ><div class="tm-security-heading"
        ><div
          ><h2>接口限流告警</h2
          ><p
            >只展示已触发的认证或 API 来源限流；来源以不可逆标识显示，不记录原始 IP。</p
          ></div
        ><a-button
          :loading="alertsLoading"
          :disabled="alertsLoading"
          @click="loadSecurityAlerts"
          >刷新告警</a-button
        ></div
      ><div v-if="alertsError" class="tm-error" role="alert">{{
        alertsError
      }}</div
      ><div
        v-else-if="!alertsLoading && !securityAlerts.length"
        class="tm-empty"
        >暂无活动的接口限流告警</div
      ><article
        v-for="alert in securityAlerts"
        :key="alert.sourceRef"
        class="tm-platform-audit-row"
        ><strong>请求来源已临时限流</strong
        ><span
          >来源标识 {{ alert.sourceRef }} · 窗口请求数
          {{ alert.requestCount }}</span
        ><span
          >解除时间
          {{ formatDateTime(alert.blockedUntil, user.timezone) }}</span
        ></article
      ></section
    ><a-modal
      v-model:visible="reviewVisible"
      :title="reviewingDecision === 'approved' ? '批准团队' : '拒绝团队'"
      :ok-loading="!!reviewingId"
      @ok="submitReview"
      ><div class="tm-form"
        ><p>{{ selected?.name }}</p
        ><label
          >审核说明<textarea
            v-model="reviewReason"
            maxlength="2000"
            rows="4"
          ></textarea></label
        ><p v-if="reviewError" class="tm-error" role="alert">{{
          reviewError
        }}</p></div
      ></a-modal
    ></section
  ></template
>
<script setup lang="ts">
  import { useUserStore } from '@/store';
  import { formatDateTime } from '@/utils/time';
  import { ref, onMounted } from 'vue';
  import { Message } from '@arco-design/web-vue';
  import { api } from '@/api/workspace';
  type T = {
    id: string;
    name: string;
    status: string;
    createdAt: string;
    reviewReason?: string;
  };
  type Audit = {
    id: number;
    actorId: string;
    action: string;
    teamName: string;
    reason: string;
    createdAt: string;
  };
  type SecurityAlert = {
    sourceRef: string;
    requestCount: number;
    blockedUntil: string;
  };
  const user = useUserStore();
  const items = ref<T[]>([]);
  const auditRows = ref<Audit[]>([]);
  const securityAlerts = ref<SecurityAlert[]>([]);
  const error = ref('');
  const loading = ref(false);
  const alertsLoading = ref(false);
  const alertsError = ref('');
  const reviewingId = ref('');
  const reviewingDecision = ref('');
  const reviewVisible = ref(false);
  const reviewError = ref('');
  const reviewReason = ref('');
  const selected = ref<T | null>(null);
  async function refresh() {
    if (loading.value) return;
    loading.value = true;
    try {
      [items.value, auditRows.value, securityAlerts.value] = await Promise.all([
        api<T[]>('GET', '/platform/teams'),
        api<Audit[]>('GET', '/platform/audit-log'),
        api<SecurityAlert[]>('GET', '/platform/security-alerts'),
      ]);
      error.value = '';
    } catch (e) {
      error.value = (e as Error).message;
    } finally {
      loading.value = false;
    }
  }
  async function loadSecurityAlerts() {
    if (alertsLoading.value) return;
    alertsLoading.value = true;
    alertsError.value = '';
    try {
      securityAlerts.value = await api<SecurityAlert[]>(
        'GET',
        '/platform/security-alerts'
      );
    } catch (e) {
      alertsError.value = (e as Error).message;
    } finally {
      alertsLoading.value = false;
    }
  }
  function review(t: T, decision: string) {
    selected.value = t;
    reviewingDecision.value = decision;
    reviewReason.value = '';
    reviewError.value = '';
    reviewVisible.value = true;
  }
  async function submitReview() {
    if (!selected.value || reviewingId.value) return;
    if (
      reviewingDecision.value === 'rejected' &&
      reviewReason.value.trim().length < 5
    ) {
      reviewError.value = '拒绝时请填写至少 5 个字的原因';
      return;
    }
    reviewingId.value = selected.value.id;
    reviewError.value = '';
    try {
      await api('POST', `/platform/teams/${selected.value.id}/decision`, {
        decision: reviewingDecision.value,
        reason: reviewReason.value.trim(),
      });
      reviewVisible.value = false;
      Message.success('审核已完成');
      await refresh();
    } catch (e) {
      reviewError.value = (e as Error).message;
    } finally {
      reviewingId.value = '';
    }
  }
  onMounted(refresh);
</script>
<style scoped>
  .tm-platform-audit-row {
    display: grid;
    gap: 4px;
    padding: 10px 0;
    border-bottom: 1px solid var(--color-border-2);
    overflow-wrap: anywhere;
  }
  .tm-platform-audit-row:last-child {
    border-bottom: 0;
  }
  .tm-platform-audit-row span {
    color: var(--color-text-2);
    font-size: 13px;
  }
</style>
