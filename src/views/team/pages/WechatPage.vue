<template
  ><section class="tm-page"
    ><header class="tm-header"
      ><div
        ><h1>微信渠道绑定</h1
        ><p>按腾讯维护的 OpenClaw 微信渠道扫码流程连接</p></div
      ><a-button @click="load">刷新状态</a-button></header
    ><div v-if="error" class="tm-error" role="alert">{{ error }}</div
    ><div v-if="mode === 'mock'" class="tm-warning"
      >本地模拟渠道：不能用于真实微信扫码或消息收发。</div
    ><div v-if="mode === 'unconfigured'" class="tm-warning"
      >微信通道未配置。服务端需设置 WECHAT_MODE=real 和
      WECHAT_ENCRYPTION_KEY；还需部署消息通道进程。</div
    ><div v-if="!messagingReady" class="tm-warning"
      >微信消息 worker 未连接或无有效绑定；扫码后仍需启动 worker
      并检查状态。</div
    ><div v-else class="tm-warning"
      >消息 worker
      正在轮询。此状态只表示进程活跃，真实微信消息收发需扫码后实测。</div
    ><div v-if="aiMode === 'mock'" class="tm-warning"
      >AI 使用本地模拟模式，不是真实模型服务；微信入站消息不会收到模拟回复。</div
    ><section class="tm-panel"
      ><h2>已登记渠道</h2
      ><div v-if="!bindings.length" class="tm-empty">尚无已确认绑定</div
      ><div v-for="b in bindings" :key="b.id" class="tm-row"
        ><span>{{ b.provider }} · {{ b.status }} · 团队 {{ b.teamId }}</span
        ><a-button status="danger" size="small" @click="unbind(b.id)"
          >解绑</a-button
        ></div
      ></section
    ><section class="tm-panel"
      ><h2>连接微信</h2
      ><p
        >手机浏览器上请在另一台设备展示二维码，再用微信扫码并在手机上确认。网页不会接收渠道令牌。</p
      ><a-button
        type="primary"
        :loading="busy"
        :disabled="mode === 'unconfigured'"
        @click="start"
        >{{ session ? '刷新二维码' : '连接微信' }}</a-button
      ><div v-if="session" class="tm-qr"
        ><img v-if="qrData" :src="qrData" alt="微信渠道二维码" /><p
          >状态：{{ statusText }} · 到期
          {{ formatClock(session.expiresAt, user.timezone) }}</p
        ><p v-if="session.status === 'status_unknown'" class="tm-error"
          >服务器未确认当前扫码状态。请重新连接获取新二维码，再继续扫码。</p
        ><p v-if="session.error" class="tm-error">{{ session.error }}</p
        ><div v-if="session.status === 'need_verifycode'" class="tm-toolbar"
          ><a-input
            v-model="verifyCode"
            placeholder="输入微信显示的验证码"
          /><a-button @click="verify">提交验证码</a-button></div
        ><a-button v-if="session.simulated" @click="mockConfirm"
          >模拟确认（测试专用）</a-button
        ><a-button @click="cancel">取消</a-button></div
      ></section
    ></section
  ></template
>
<script setup lang="ts">
  import { useUserStore } from '@/store';
  import { formatClock } from '@/utils/time';
  import { ref, onMounted, onBeforeUnmount, watch } from 'vue';
  import { Modal, Message } from '@arco-design/web-vue';
  import QRCode from 'qrcode';
  import { api } from '@/api/workspace';
  import { useWorkspaceStore } from '@/stores/workspace';
  import { useTeamRequest } from '@/utils/teamRequest';
  const user = useUserStore();
  const store = useWorkspaceStore();
  const teamRequest = useTeamRequest(() => store.activeTeam.id);
  type Session = {
    id: string;
    status: string;
    qrContent: string;
    expiresAt: string;
    simulated: boolean;
    error: string;
  };
  const mode = ref('unconfigured');
  const aiMode = ref('mock');
  const bindings = ref<
    { id: string; provider: string; status: string; teamId: string }[]
  >([]);
  const messagingReady = ref(false);
  const session = ref<Session | null>(null);
  const qrData = ref('');
  const verifyCode = ref('');
  const busy = ref(false);
  const error = ref('');
  let timer: number | undefined;
  const labels: Record<string, string> = {
    initializing: '初始化',
    wait: '等待扫码',
    scaned: '已扫码，等待手机确认',
    need_verifycode: '需要验证码',
    verify_code_blocked: '验证码受限',
    scaned_but_redirect: '渠道重定向',
    binded_redirect: '已绑定到渠道实例，未取得新凭据',
    confirming: '正在安全保存绑定',
    confirmed: '渠道确认成功',
    confirmed_simulated: '模拟确认，仅测试',
    expired: '二维码过期',
    failed: '连接失败',
    cancelled: '已取消',
    status_unknown: '状态未知',
  };
  const statusText = ref('');
  let sessionTeamId = '';
  let startGeneration = 0;
  async function load() {
    const request = teamRequest.begin();
    try {
      const x = await api<{
        mode: string;
        aiMode: string;
        bindings: typeof bindings.value;
        messagingReady: boolean;
      }>('GET', '/me/channels/wechat');
      if (!teamRequest.isCurrent(request)) return;
      mode.value = x.mode;
      aiMode.value = x.aiMode;
      bindings.value = x.bindings;
      messagingReady.value = x.messagingReady;
      error.value = '';
    } catch (e) {
      if (!teamRequest.isCurrent(request)) return;
      error.value = (e as Error).message;
    }
  }
  async function start() {
    const teamId = store.activeTeam.id;
    if (!teamId) {
      error.value = '请先创建或加入团队';
      return;
    }
    const generation = ++startGeneration;
    if (session.value) await cancel();
    if (generation !== startGeneration || store.activeTeam.id !== teamId) return;
    busy.value = true;
    error.value = '';
    try {
      const result = await api<Session>(
        'POST',
        '/me/channels/wechat/sessions',
        { teamId }
      );
      if (store.activeTeam.id !== teamId) {
        await api('DELETE', `/me/channels/wechat/sessions/${result.id}`).catch(() => undefined);
        return;
      }
      session.value = result;
      sessionTeamId = teamId;
      qrData.value = await QRCode.toDataURL(result.qrContent, {
        width: 260,
        margin: 2,
      });
      if (store.activeTeam.id !== teamId || session.value?.id !== result.id) return;
      statusText.value = labels[result.status] || result.status;
      timer = window.setInterval(poll, 1800);
    } catch (e) {
      if (generation === startGeneration && store.activeTeam.id === teamId)
        error.value = (e as Error).message;
    } finally {
      if (generation === startGeneration) busy.value = false;
    }
  }
  async function poll() {
    if (!session.value) return;
    const sessionId = session.value.id;
    const teamId = sessionTeamId;
    if (!teamId || store.activeTeam.id !== teamId) return;
    try {
      const x = await api<Session>(
        'GET',
        `/me/channels/wechat/sessions/${sessionId}`
      );
      if (store.activeTeam.id !== teamId || session.value?.id !== sessionId) return;
      session.value = x;
      statusText.value = labels[x.status] || x.status;
      error.value = '';
      if (x.status === 'confirmed' || x.status === 'confirmed_simulated') {
        window.clearInterval(timer);
        await load();
      }
      if (
        ['expired', 'failed', 'cancelled', 'binded_redirect'].includes(x.status)
      )
        window.clearInterval(timer);
    } catch (e) {
      if (store.activeTeam.id !== teamId || session.value?.id !== sessionId) return;
      error.value = (e as Error).message;
      session.value = { ...session.value, status: 'status_unknown' };
      statusText.value = labels.status_unknown;
      window.clearInterval(timer);
      timer = undefined;
    }
  }
  async function verify() {
    if (!session.value) return;
    const sessionId = session.value.id;
    const teamId = sessionTeamId;
    try {
      await api(
        'POST',
        `/me/channels/wechat/sessions/${sessionId}/verify`,
        { code: verifyCode.value }
      );
      if (store.activeTeam.id === teamId && session.value?.id === sessionId)
        Message.info('验证码已提交，等待渠道确认');
    } catch (e) {
      if (store.activeTeam.id === teamId && session.value?.id === sessionId)
        error.value = (e as Error).message;
    }
  }
  async function mockConfirm() {
    if (!session.value) return;
    const sessionId = session.value.id;
    const teamId = sessionTeamId;
    try {
      await api(
        'POST',
        `/me/channels/wechat/sessions/${sessionId}/mock-confirm`
      );
      if (store.activeTeam.id !== teamId || session.value?.id !== sessionId) return;
      await poll();
    } catch (e) {
      if (store.activeTeam.id === teamId && session.value?.id === sessionId)
        error.value = (e as Error).message;
    }
  }
  async function cancel() {
    window.clearInterval(timer);
    const current = session.value;
    session.value = null;
    sessionTeamId = '';
    qrData.value = '';
    if (current) {
      try {
        await api('DELETE', `/me/channels/wechat/sessions/${current.id}`);
      } catch {
        /* service may have restarted */
      }
    }
  }
  function unbind(id: string) {
    Modal.confirm({
      title: '解绑微信渠道？',
      content: '解绑后该平台账号不再关联此渠道。',
      onOk: async () => {
        try {
          await api('DELETE', `/me/channels/wechat/${id}`);
          await load();
        } catch (e) {
          Message.error((e as Error).message);
        }
      },
    });
  }
  watch(() => store.activeTeam.id, (id, previous) => {
    if (id === previous) return;
    startGeneration += 1;
    busy.value = false;
    bindings.value = [];
    error.value = '';
    if (session.value && sessionTeamId !== id) void cancel();
    load();
  });
  onMounted(load);
  onBeforeUnmount(() => {
    window.clearInterval(timer);
    if (session.value && session.value.status !== 'confirmed') void cancel();
  });
</script>
