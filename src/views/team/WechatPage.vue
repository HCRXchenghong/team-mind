<template>
  <div class="page">
    <PageHeader title="微信渠道" subtitle="把 TeamMind 接到微信，成员可以直接在会话里使用 AI 与任务工具">
      <template #actions>
        <a-button :loading="loading" @click="load"><reload-outlined /> 刷新状态</a-button>
      </template>
    </PageHeader>

    <a-alert v-if="error" type="error" show-icon :message="error" closable @close="error = ''" />
    <a-alert
      type="info"
      show-icon
      message="主动消息依赖最近一次微信会话上下文"
      description="绑定后请先在微信里给机器人发送一条消息；系统会保存联系人与上下文，用于定时提醒和主动通知。长时间未互动、Gateway 重启或换绑后，建议重新发一条消息，再用「发送测试消息」确认通道可用。"
    />
    <a-alert
      v-if="status.mode === 'unconfigured'"
      type="warning"
      show-icon
      message="微信渠道尚未配置"
      description="请联系平台管理员在运行配置里填写 Gateway 与 Bridge Token。"
    />

    <div class="split-grid">
      <section class="panel">
        <div class="panel-head"><h3>渠道状态</h3><a-tag :color="status.mode === 'real' ? 'success' : 'default'">{{ modeLabel }}</a-tag></div>
        <div class="panel-pad">
          <a-descriptions :column="1" size="small" bordered>
            <a-descriptions-item label="接入模式">{{ modeLabel }}</a-descriptions-item>
            <a-descriptions-item label="消息服务">
              <a-tag :color="status.messagingReady ? 'success' : 'warning'">{{ status.messagingReady ? '可用' : '未就绪' }}</a-tag>
            </a-descriptions-item>
            <a-descriptions-item label="AI 服务">{{ status.aiMode || '未知' }}</a-descriptions-item>
            <a-descriptions-item label="当前组织">{{ session.activeTeam?.name || '—' }}</a-descriptions-item>
          </a-descriptions>

          <a-alert v-if="status.mode === 'real' && !status.messagingReady" type="info" show-icon style="margin-top: 14px"
            message="消息服务未就绪，扫码可能无法完成，请检查 Gateway。" />
        </div>
      </section>

      <section class="panel">
        <div class="panel-head">
          <h3>我的绑定</h3>
          <a-button v-if="!bindings.length && status.mode !== 'unconfigured'" type="primary" size="small" @click="startScan">扫码绑定</a-button>
        </div>
        <div class="panel-pad">
          <div v-for="binding in bindings" :key="binding.id" class="binding">
            <div class="binding-main">
              <div class="binding-name"><wechat-outlined /> {{ binding.provider || '微信' }}</div>
              <div class="muted binding-meta">
                状态：{{ binding.status }} · 绑定时间：{{ fmtDateTime(binding.boundAt) }}
                <span v-if="binding.lastInboundAt"> · 最近消息 {{ fmtDateTime(binding.lastInboundAt) }}</span>
              </div>
              <div class="binding-tags">
                <a-tag :color="binding.hasContext ? 'success' : 'warning'" :bordered="false">
                  {{ binding.hasContext ? '主动消息可用' : '需先发一条消息' }}
                </a-tag>
                <span v-if="!binding.hasContext" class="muted hint">
                  在微信里给机器人发一条消息后，这里会变为可用
                </span>
              </div>
            </div>
            <div class="binding-actions">
              <a-tooltip :title="binding.hasContext ? '立即发送一条测试文本，验证主动消息通道' : '请先在微信里给机器人发一条消息'">
                <a-button
                  size="small"
                  :loading="testingId === binding.id"
                  :disabled="!binding.hasContext"
                  @click="sendTest(binding)"
                >
                  发送测试消息
                </a-button>
              </a-tooltip>
              <a-popconfirm title="解绑后需重新扫码才能使用微信渠道，确认？" @confirm="unbind(binding.id)">
                <a-button size="small" danger ghost>解绑</a-button>
              </a-popconfirm>
            </div>
          </div>
          <div v-if="!bindings.length" class="empty-block small">
            还没有绑定微信{{ status.mode === 'unconfigured' ? '（渠道未配置）' : '' }}
          </div>
        </div>
      </section>
    </div>

    <section v-if="scan.id" class="panel scan">
      <div class="scan-qr">
        <qrcode-vue v-if="scan.qrContent" :value="scan.qrContent" :size="200" :margin="1" />
        <div v-else class="qr-placeholder">二维码生成中…</div>
      </div>
      <div class="scan-body">
        <div class="scan-head">
          <h3>扫码绑定微信</h3>
          <a-tag color="blue">{{ scanStatusLabel }}</a-tag>
        </div>
        <p class="muted">使用微信扫描左侧二维码并确认登录；若微信提示验证码，请填写到下方。</p>
        <a-typography-text v-if="scan.qrContent" copyable class="qr-content">{{ scan.qrContent }}</a-typography-text>

        <div v-if="needCode" class="scan-code">
          <a-input v-model:value="code" placeholder="输入微信显示的验证码" style="width: 220px" @press-enter="submitCode" />
          <a-button type="primary" :loading="verifying" @click="submitCode">确认验证码</a-button>
        </div>

        <div class="scan-actions">
          <a-button @click="cancelScan">取消扫码</a-button>
          <span class="muted">有效期至 {{ fmtDateTime(scan.expiresAt) }}</span>
        </div>
      </div>
    </section>

    <section class="panel">
      <div class="panel-head"><h3>使用说明</h3></div>
      <div class="panel-pad tips">
        <div><b>1. 绑定</b><span>扫码确认后，微信会与当前 TeamMind 账号关联；一个微信只能绑定一个账号。</span></div>
        <div><b>2. 对话</b><span>在微信里发消息即可调用 AI 助手；任务、审批等工具需要按次授权。</span></div>
        <div><b>3. 解绑</b><span>解绑后消息不再路由到你的账号，可随时重新扫码。</span></div>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { ReloadOutlined, WechatOutlined } from '@ant-design/icons-vue';
import QrcodeVue from 'qrcode.vue';
import { api } from '@/api/client';
import { useSession } from '@/store/session';
import { fmtDateTime } from '@/utils/format';
import { notify } from '@/utils/notify';
import PageHeader from '@/components/PageHeader.vue';

interface WechatStatus {
  mode?: string;
  messagingReady?: boolean;
  aiMode?: string;
  bindings?: Array<Record<string, any>>;
}

const session = useSession();
const loading = ref(false);
const error = ref('');
const status = ref<WechatStatus>({ bindings: [] });
const scan = ref<Record<string, any>>({});
const code = ref('');
const verifying = ref(false);
const testingId = ref('');
let timer: number | undefined;

const bindings = computed(() => status.value.bindings || []);
const modeLabel = computed(() => {
  if (status.value.mode === 'real') return '真实服务';
  if (status.value.mode === 'unconfigured' || !status.value.mode) return '未配置';
  return status.value.mode;
});
const needCode = computed(() => ['scaned', 'verify_code_blocked'].includes(String(scan.value.status)));
const scanStatusLabel = computed(() => {
  const map: Record<string, string> = {
    pending: '等待扫码',
    scaned: '已扫码，待确认',
    verify_code_blocked: '需要验证码',
    confirmed: '已确认',
    confirmed_simulated: '已确认（模拟）',
    expired: '已过期',
    cancelled: '已取消',
    failed: '失败',
  };
  return map[String(scan.value.status)] || String(scan.value.status || '等待扫码');
});

async function load(): Promise<void> {
  loading.value = true;
  try {
    status.value = await api<WechatStatus>('GET', '/me/channels/wechat');
    error.value = '';
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}

async function startScan(): Promise<void> {
  try {
    scan.value = await api<Record<string, any>>('POST', '/me/channels/wechat/sessions', { teamId: session.activeId });
    code.value = '';
    stopPolling();
    timer = window.setInterval(poll, 2500);
  } catch (e) {
    notify.error(e);
  }
}

async function poll(): Promise<void> {
  if (!scan.value.id) return;
  try {
    scan.value = await api<Record<string, any>>('GET', `/me/channels/wechat/sessions/${scan.value.id}`);
    if (['confirmed', 'confirmed_simulated', 'expired', 'cancelled', 'failed'].includes(String(scan.value.status))) {
      stopPolling();
      if (String(scan.value.status).startsWith('confirmed')) {
        notify.success('微信已绑定');
        scan.value = {};
        await load();
      }
    }
  } catch (e) {
    stopPolling();
    notify.error(e);
  }
}

function stopPolling(): void {
  if (timer) window.clearInterval(timer);
  timer = undefined;
}

async function submitCode(): Promise<void> {
  if (!code.value.trim()) {
    notify.error('请输入验证码');
    return;
  }
  verifying.value = true;
  try {
    await api('POST', `/me/channels/wechat/sessions/${scan.value.id}/verify`, { code: code.value.trim() });
    notify.success('验证码已提交');
    await poll();
  } catch (e) {
    notify.error(e);
  } finally {
    verifying.value = false;
  }
}

async function cancelScan(): Promise<void> {
  stopPolling();
  const id = scan.value.id;
  scan.value = {};
  if (!id) return;
  try {
    await api('DELETE', `/me/channels/wechat/sessions/${id}`);
  } catch {
    /* 会话可能已过期 */
  }
}

async function sendTest(binding: Record<string, any>): Promise<void> {
  testingId.value = String(binding.id);
  try {
    await api('POST', '/me/channels/wechat/test');
    notify.success('测试消息已发送，请在微信里查收');
  } catch (e) {
    notify.error(e);
  } finally {
    testingId.value = '';
  }
}

async function unbind(id: string): Promise<void> {
  try {
    await api('DELETE', `/me/channels/wechat/${id}`);
    notify.success('已解绑');
    await load();
  } catch (e) {
    notify.error(e);
  }
}

watch(() => session.activeId, load);
onMounted(load);
onBeforeUnmount(stopPolling);
</script>

<style scoped>
.binding {
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: 8px;
  padding: 12px 0;
  border-bottom: 1px solid var(--tm-border);
}
.binding:last-child {
  border-bottom: 0;
}
.binding-name {
  display: flex;
  align-items: center;
  gap: 6px;
  font-weight: 600;
}
.binding-meta {
  margin-top: 2px;
  font-size: 11.5px;
}
.binding-tags {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  margin-top: 6px;
}
.binding-tags .hint {
  font-size: 11px;
}
.binding-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 6px;
  flex: none;
}
.scan {
  display: flex;
  gap: 28px;
  padding: 22px;
  flex-wrap: wrap;
}
.scan-qr {
  width: 200px;
  height: 200px;
  padding: 6px;
  border: 1px solid var(--tm-border);
  border-radius: 10px;
  background: #fff;
  flex: none;
}
.qr-placeholder {
  display: grid;
  place-items: center;
  height: 100%;
  color: #98a0ae;
  background: #fff;
  font-size: 12px;
}
.scan-body {
  flex: 1;
  min-width: 260px;
}
.scan-head {
  display: flex;
  align-items: center;
  gap: 10px;
}
.scan-head h3 {
  margin: 0;
  font-size: 16px;
}
.qr-content {
  display: block;
  max-width: 520px;
  margin: 6px 0 12px;
  font-size: 11px;
  word-break: break-all;
  color: var(--tm-muted);
}
.scan-code {
  display: flex;
  gap: 8px;
  margin: 10px 0;
}
.scan-actions {
  display: flex;
  align-items: center;
  gap: 14px;
  margin-top: 12px;
  font-size: 11.5px;
}
.empty-block.small {
  padding: 24px;
}
.tips {
  display: grid;
  gap: 12px;
  color: var(--tm-muted);
  font-size: 12.5px;
}
.tips b {
  display: inline-block;
  min-width: 52px;
  color: var(--tm-text);
}
.tips span {
  display: inline;
}
</style>
