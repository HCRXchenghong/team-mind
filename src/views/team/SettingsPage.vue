<template>
  <div class="page">
    <PageHeader title="设置" subtitle="个人资料、账号安全、通知、AI 日报、组织、数据与审计">
      <template #actions>
        <a-tag v-if="session.activeTeam" :bordered="false">{{ session.activeTeam.name }}</a-tag>
      </template>
    </PageHeader>

    <div class="panel settings-panel">
      <a-tabs :active-key="activeTab" class="settings-tabs" @change="onTabChange">
        <a-tab-pane key="profile" tab="个人资料"><ProfileSection /></a-tab-pane>
        <a-tab-pane key="security" tab="账号安全"><SecuritySection /></a-tab-pane>
        <a-tab-pane key="notifications" tab="通知与提醒"><NotificationsSection /></a-tab-pane>
        <a-tab-pane key="ai" tab="AI 日报"><AiProactiveSection /></a-tab-pane>
        <a-tab-pane key="team" tab="组织设置"><TeamSection /></a-tab-pane>
        <a-tab-pane key="data" tab="个人数据"><DataSection /></a-tab-pane>
        <a-tab-pane key="audit" tab="审计日志"><AuditSection /></a-tab-pane>
      </a-tabs>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useSession } from '@/store/session';
import PageHeader from '@/components/PageHeader.vue';
import ProfileSection from '@/views/settings/ProfileSection.vue';
import SecuritySection from '@/views/settings/SecuritySection.vue';
import NotificationsSection from '@/views/settings/NotificationsSection.vue';
import AiProactiveSection from '@/views/settings/AiProactiveSection.vue';
import TeamSection from '@/views/settings/TeamSection.vue';
import DataSection from '@/views/settings/DataSection.vue';
import AuditSection from '@/views/settings/AuditSection.vue';

const route = useRoute();
const router = useRouter();
const session = useSession();

const tabs = ['profile', 'security', 'notifications', 'ai', 'team', 'data', 'audit'];
const activeTab = computed(() => {
  const value = String(route.query.tab || 'profile');
  return tabs.includes(value) ? value : 'profile';
});

function onTabChange(key: string | number): void {
  router.replace({ path: '/teammind/settings', query: { tab: String(key) } });
}
</script>

<style scoped>
.settings-panel {
  padding: 4px 20px 24px;
}
.settings-tabs :deep(.ant-tabs-nav) {
  margin-bottom: 20px;
}
</style>
