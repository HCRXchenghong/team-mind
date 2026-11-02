<template>
  <div class="section">
    <a-row :gutter="24">
      <a-col :xs="24" :md="12">
        <div class="block">
          <h3>导出个人数据</h3>
          <p class="muted desc">导出你在 TeamMind 的个人资料、组织成员关系、记忆与设置，格式为 JSON。</p>
          <a-button :loading="exporting" @click="exportData"><download-outlined /> 导出 JSON</a-button>
        </div>
      </a-col>

      <a-col :xs="24" :md="12">
        <div class="block danger-zone">
          <h3>删除账号</h3>
          <p class="muted desc">
            删除后无法恢复：个人资料、会话、记忆与工作区文件会被清理。若你是组织所有者，请先转移所有权。
          </p>
          <a-button danger @click="openDelete">删除我的账号</a-button>
        </div>
      </a-col>
    </a-row>

    <a-modal v-model:open="deleteOpen" title="删除账号" :confirm-loading="deleting" ok-text="永久删除" cancel-text="取消" @ok="deleteAccount">
      <a-alert type="error" show-icon message="这是不可撤销的操作。" style="margin-bottom: 14px" />
      <a-form layout="vertical">
        <a-form-item label="当前密码"><a-input-password v-model:value="form.currentPassword" /></a-form-item>
        <a-form-item label="MFA 动态码（如已启用）"><a-input v-model:value="form.mfaCode" /></a-form-item>
        <a-form-item label="输入“删除账号”以确认">
          <a-input v-model:value="form.confirmation" placeholder="删除账号" />
        </a-form-item>
      </a-form>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue';
import { useRouter } from 'vue-router';
import { DownloadOutlined } from '@ant-design/icons-vue';
import { api } from '@/api/client';
import { useSession } from '@/store/session';
import { downloadFile } from '@/utils/format';
import { notify } from '@/utils/notify';

const router = useRouter();
const session = useSession();
const exporting = ref(false);
const deleting = ref(false);
const deleteOpen = ref(false);
const form = reactive({ currentPassword: '', mfaCode: '', confirmation: '' });

async function exportData(): Promise<void> {
  exporting.value = true;
  try {
    const data = await api<Record<string, unknown>>('GET', '/me/data-export');
    downloadFile('teammind-personal-data.json', JSON.stringify(data, null, 2), 'application/json;charset=utf-8');
    notify.success('导出已开始');
  } catch (e) {
    notify.error(e);
  } finally {
    exporting.value = false;
  }
}

function openDelete(): void {
  form.currentPassword = '';
  form.mfaCode = '';
  form.confirmation = '';
  deleteOpen.value = true;
}

async function deleteAccount(): Promise<void> {
  if (form.confirmation !== '删除账号') {
    notify.error('请准确输入“删除账号”');
    return;
  }
  if (!form.currentPassword) {
    notify.error('请输入当前密码');
    return;
  }
  deleting.value = true;
  try {
    await api('POST', '/me/account', {
      currentPassword: form.currentPassword,
      mfaCode: form.mfaCode,
      confirmation: form.confirmation,
    });
    session.reset();
    notify.success('账号已删除');
    router.replace('/login');
  } catch (e) {
    notify.error(e);
  } finally {
    deleting.value = false;
  }
}
</script>

<style scoped>
.block {
  margin-bottom: 26px;
  max-width: 520px;
}
h3 {
  margin: 0 0 8px;
  font-size: 14px;
}
.desc {
  margin: -2px 0 14px;
  font-size: 12px;
  line-height: 1.7;
}
.danger-zone {
  padding: 18px;
  border: 1px solid rgba(240, 68, 56, 0.28);
  border-radius: 10px;
  background: rgba(240, 68, 56, 0.03);
}
.danger-zone h3 {
  color: #d92d20;
}
</style>
