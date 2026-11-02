<template>
  <div class="section">
    <a-row :gutter="24">
      <a-col :xs="24" :md="14">
        <div class="block">
          <h3>基本资料</h3>
          <a-form layout="vertical" :model="form">
            <a-form-item label="姓名">
              <a-input v-model:value="form.name" :maxlength="80" />
            </a-form-item>
            <a-form-item label="时区">
              <a-input v-model:value="form.timezone" placeholder="Asia/Shanghai" />
              <div class="muted hint">用于日期展示与提醒时间，例如 Asia/Shanghai、Europe/London。</div>
            </a-form-item>
            <a-form-item label="邮箱">
              <a-input :value="session.user?.email" disabled />
            </a-form-item>
            <a-button type="primary" :loading="saving" @click="saveProfile">保存资料</a-button>
          </a-form>
        </div>

        <div class="block">
          <h3>修改邮箱</h3>
          <p class="muted desc">新邮箱需要通过邮件确认，确认后旧会话会失效，需要重新登录。</p>
          <a-form layout="vertical" :model="emailForm">
            <a-form-item label="新邮箱"><a-input v-model:value="emailForm.email" placeholder="new@company.com" /></a-form-item>
            <a-form-item label="当前密码"><a-input-password v-model:value="emailForm.currentPassword" /></a-form-item>
            <a-button :loading="emailSaving" @click="changeEmail">发送确认邮件</a-button>
          </a-form>
        </div>
      </a-col>

      <a-col :xs="24" :md="10">
        <div class="profile-card">
          <UserAvatar :name="session.user?.name" :size="56" />
          <div class="profile-name">{{ session.user?.name }}</div>
          <div class="muted">{{ session.user?.email }}</div>
          <div class="profile-tags">
            <a-tag v-if="session.isSuperAdmin" color="purple">平台超管</a-tag>
            <a-tag v-if="session.isAdmin" color="blue">组织管理员</a-tag>
            <a-tag v-if="session.isLead" color="cyan">部门负责人</a-tag>
            <a-tag v-if="!session.canManage" :bordered="false">成员</a-tag>
          </div>
          <a-divider />
          <div class="kv"><span>当前组织</span><b>{{ session.activeTeam?.name || '—' }}</b></div>
          <div class="kv"><span>组织类型</span><b>{{ session.isCompany ? '公司' : '团队' }}</b></div>
          <div class="kv"><span>组织成员</span><b>{{ session.activeTeam?.members || 0 }}</b></div>
        </div>
      </a-col>
    </a-row>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue';
import { api } from '@/api/client';
import { useSession } from '@/store/session';
import { notify } from '@/utils/notify';
import UserAvatar from '@/components/UserAvatar.vue';

const session = useSession();
const saving = ref(false);
const emailSaving = ref(false);
const form = reactive({ name: '', timezone: '' });
const emailForm = reactive({ email: '', currentPassword: '' });

function fill(): void {
  form.name = session.user?.name || '';
  form.timezone = session.user?.timezone || 'Asia/Shanghai';
}

async function saveProfile(): Promise<void> {
  if (!form.name.trim()) {
    notify.error('请输入姓名');
    return;
  }
  saving.value = true;
  try {
    await api('PATCH', '/me', { name: form.name.trim(), timezone: form.timezone.trim() });
    await session.load();
    notify.success('资料已保存');
  } catch (e) {
    notify.error(e);
  } finally {
    saving.value = false;
  }
}

async function changeEmail(): Promise<void> {
  if (!emailForm.email.trim() || !emailForm.currentPassword) {
    notify.error('请填写新邮箱和当前密码');
    return;
  }
  emailSaving.value = true;
  try {
    await api('POST', '/me/email-change', { email: emailForm.email.trim(), currentPassword: emailForm.currentPassword });
    notify.success('确认邮件已发送，请到新邮箱查收');
    emailForm.email = '';
    emailForm.currentPassword = '';
  } catch (e) {
    notify.error(e);
  } finally {
    emailSaving.value = false;
  }
}

onMounted(fill);
</script>

<style scoped>
.block {
  margin-bottom: 28px;
  max-width: 520px;
}
h3 {
  margin: 0 0 12px;
  font-size: 14px;
}
.hint {
  margin-top: 4px;
  font-size: 11.5px;
}
.desc {
  margin: -4px 0 12px;
  font-size: 12px;
}
.profile-card {
  padding: 22px;
  border-radius: 10px;
  background: var(--tm-panel-2);
}
.profile-name {
  margin-top: 10px;
  font-size: 17px;
  font-weight: 650;
}
.profile-tags {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
  margin-top: 10px;
}
.kv {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  padding: 5px 0;
  font-size: 12px;
  color: var(--tm-muted);
}
.kv b {
  color: var(--tm-text);
  font-weight: 500;
}
</style>
