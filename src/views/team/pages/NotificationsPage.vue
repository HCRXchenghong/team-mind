<template
  ><section class="tm-page"
    ><header class="tm-header"
      ><div><h1>通知中心</h1><p>与你有关的团队通知</p></div
      ><div class="tm-actions"
        ><a-button :loading="loading" @click="refresh">刷新</a-button
        ><a-button
          type="primary"
          :loading="readingAll"
          :disabled="readingAll || !items.some((item) => !item.read)"
          @click="readAll"
          >全部已读</a-button
        ></div
      ></header
    ><div v-if="error" class="tm-error" role="alert"
      >{{ error }} <a-button size="small" @click="refresh">重试</a-button></div
    ><section class="tm-panel"
      ><h2>通知偏好</h2
      ><div class="tm-toolbar"
        ><label
          ><input v-model="prefs.enabled" type="checkbox" /> 接收通知</label
        ><label
          ><input v-model="prefs.emailEnabled" type="checkbox" :disabled="!channels.emailConfigured && !prefs.emailEnabled" /> 邮件通知</label
        ><label
          ><input v-model="prefs.pushEnabled" type="checkbox" :disabled="!channels.pushConfigured && !prefs.pushEnabled" /> 浏览器推送</label
        ><label v-if="isNativeApp"
          ><input v-model="prefs.nativePushEnabled" type="checkbox" :disabled="!channels.nativePushConfigured && !prefs.nativePushEnabled" /> 原生推送</label
        ><label
          >免打扰开始 <input v-model="prefs.quietStart" type="time" /></label
        ><label>结束 <input v-model="prefs.quietEnd" type="time" /></label
        ><a-button
          :loading="savingPrefs"
          :disabled="!prefsLoaded || savingPrefs"
          @click="savePrefs"
          >保存偏好</a-button
        ></div
      ><div class="tm-toolbar tm-push-actions">
        <span v-if="!channels.emailConfigured" class="tm-channel-status">邮件未配置；请联系部署管理员配置 SMTP。</span>
        <span v-if="!channels.pushConfigured" class="tm-channel-status">浏览器推送未配置；请管理员配置 VAPID 密钥、有效的联系地址（mailto 或 HTTPS）和订阅加密密钥。</span>
        <span v-else class="tm-channel-status">此账号已有 {{ channels.pushSubscriptions }} 个浏览器推送设备。</span>
        <a-button :loading="pushBusy" :disabled="pushBusy || !channels.pushConfigured" @click="enablePush">在此设备启用推送</a-button>
        <a-button :loading="pushBusy" :disabled="pushBusy || !currentPushSubscription" status="danger" @click="disablePush">关闭此设备推送</a-button>
      </div
      ><p v-if="!isNativeApp" class="tm-channel-status">移动端使用浏览器 Web Push。Android Chrome 需允许通知；iPhone/iPad 通常需将网站加入主屏幕，并在 iOS 16.4 或更新版本中允许通知。网页推送不需要安装 App。</p
      ><p v-else class="tm-channel-status">这是原生 App 推送，使用设备系统通知权限；网页浏览器订阅与此处的原生设备令牌分别管理。</p
      ><div v-if="isNativeApp" class="tm-toolbar tm-push-actions">
        <span v-if="!channels.nativePushConfigured" class="tm-channel-status">原生推送未配置；管理员需配置 Firebase 服务账号、Android Firebase 文件和 iOS APNs。</span>
        <span v-else class="tm-channel-status">此账号有 {{ channels.nativeDevices }} 个原生设备。设备令牌只加密保存在服务端。</span>
        <span v-if="!nativeClientPushConfigured" class="tm-channel-status">此 App 构建未包含平台 Firebase 配置；请重新构建已配置的 Android/iOS 包。</span>
        <a-button :loading="nativePushBusy" :disabled="nativePushBusy || !channels.nativePushConfigured || !nativeClientPushConfigured" @click="enableNativePush">在此 App 启用原生推送</a-button>
        <article v-for="device in nativeDevices" :key="device.id" class="tm-native-device">
          <span>{{ device.platform === 'ios' ? 'iOS' : 'Android' }} · {{ device.status === 'active' ? '已启用' : '已撤销' }} · {{ formatDateTime(device.updatedAt, user.timezone) }}</span>
          <a-button size="small" status="danger" :loading="revokingNativeDevice === device.id" :disabled="device.status !== 'active' || !!revokingNativeDevice" @click="revokeNativeDevice(device)">撤销</a-button>
        </article>
      </div
      ></section
    ><section class="tm-panel tm-reminders"
      ><div class="tm-reminder-heading"
        ><div
          ><h2>定时提醒</h2
          ><p
            >保存到服务器后，即使退出页面或服务重启，提醒仍会按时送达站内收件箱。时间按
            {{ user.timezone || 'Asia/Shanghai' }} 解释。</p
          ></div
        ></div
      ><form class="tm-reminder-form" @submit.prevent="createReminder"
        ><label
          >提醒范围<select v-model="reminderScope" :disabled="savingReminder"
            ><option value="personal">个人</option
            ><option v-if="workspace.activeTeam.id" value="team"
              >当前团队：{{ workspace.activeTeam.name }}</option
            ></select
          ></label
        ><label
          >提醒时间<input
            v-model="reminderForm.runAt"
            type="datetime-local"
            required
            :disabled="savingReminder" /></label
        ><label
          >标题<input
            v-model="reminderForm.title"
            maxlength="160"
            required
            :disabled="savingReminder"
            placeholder="例如：检查本周计划" /></label
        ><label
          >内容（可选）<textarea
            v-model="reminderForm.body"
            maxlength="2000"
            rows="2"
            :disabled="savingReminder"
            placeholder="添加提醒说明"
          /></label
        ><div class="tm-reminder-submit"
          ><span v-if="reminderError" class="tm-error" role="alert">{{
            reminderError
          }}</span
          ><a-button
            type="primary"
            html-type="submit"
            :loading="savingReminder"
            :disabled="savingReminder"
            >创建提醒</a-button
          ></div
        ></form
      ><div v-if="remindersLoading" class="tm-empty" role="status"
        >正在读取定时提醒…</div
      ><div v-else-if="!reminders.length && !reminderLoadError" class="tm-empty"
        >暂无定时提醒</div
      ><div v-else-if="reminders.length" class="tm-reminder-list"
        ><article
          v-for="reminder in reminders"
          :key="reminder.id"
          class="tm-reminder-row"
          ><div
            ><strong>{{ reminder.title }}</strong
            ><p v-if="reminder.body">{{ reminder.body }}</p
            ><small
              >{{ reminder.teamId ? '团队提醒' : '个人提醒' }} ·
              {{ formatDateTime(reminder.runAt, user.timezone) }}</small
            ></div
          ><div class="tm-reminder-actions"
            ><span>{{ reminderStatus(reminder.status) }}</span
            ><a-button
              v-if="reminder.status === 'scheduled'"
              size="small"
              status="danger"
              :loading="cancellingReminder === reminder.id"
              :disabled="!!cancellingReminder"
              @click="cancelReminder(reminder)"
              >取消</a-button
            ></div
          ></article
        ></div
      ><div v-if="reminderLoadError" class="tm-error" role="alert"
        >{{ reminderLoadError }}
        <a-button size="small" @click="loadReminders">重试</a-button></div
      ></section
    ><section class="tm-panel tm-delivery-panel"
      ><h2>最近投递记录</h2
      ><p class="tm-channel-status" role="status"
        >站内通知、邮件和浏览器推送分别按你的偏好投递。外发内容不包含通知正文；未配置的通道会明确记录为未投递。</p
      ><div v-if="!deliveries.length" class="tm-empty">暂无投递记录</div
      ><div v-else class="tm-delivery-list"
        ><article
          v-for="delivery in deliveries"
          :key="delivery.id"
          class="tm-delivery-row"
          ><div
            ><strong>{{ delivery.type }}</strong
            ><small>{{
              delivery.channel === 'in_app' ? '站内通知' : delivery.channel
            }}</small></div
          ><div
            ><span :class="'status-' + delivery.status">{{
              deliveryStatus(delivery.status)
            }}</span
            ><small>{{
              formatDateTime(delivery.createdAt, user.timezone)
            }}</small></div
          ><p v-if="delivery.reason">{{
            deliveryReason(delivery.reason)
          }}</p></article
        ></div
      ></section
    ><div v-if="loading && !items.length" class="tm-empty">正在加载通知…</div
    ><div v-else-if="!items.length" class="tm-empty">暂无通知</div
    ><article v-for="n in items" :key="n.id" class="tm-panel"
      ><h2>{{ n.title }} <small v-if="!n.read">未读</small></h2
      ><p>{{ n.body }}</p
      ><p>{{ formatDateTime(n.createdAt, user.timezone) }}</p
      ><a-button
        v-if="!n.read"
        size="small"
        :loading="markingId === n.id"
        @click="mark(n)"
        >标记已读</a-button
      ></article
    ></section
  ></template
>
<script setup lang="ts">
  import { useUserStore } from '@/store';
  import { formatDateTime, localDateTimeToUTC } from '@/utils/time';
  import { ref, onMounted, onUnmounted, watch } from 'vue';
  import { Capacitor } from '@capacitor/core';
  import type { PluginListenerHandle } from '@capacitor/core';
  import { FirebaseMessaging } from '@/native/firebaseMessaging';
  import { registerNativeDeviceToken } from '@/native/deviceRegistration';
  import { Message } from '@arco-design/web-vue';
  import { api } from '@/api/workspace';
  import type {
    Notification as NotificationItem,
    NotificationDelivery,
    Reminder,
  } from '@/api/workspace';
  import { useWorkspaceStore } from '@/stores/workspace';
  const user = useUserStore();
  const workspace = useWorkspaceStore();
  type NativeDevice = { id: string; platform: 'android' | 'ios'; status: 'active' | 'revoked'; createdAt: string; updatedAt: string };
  const isNativeApp = Capacitor.isNativePlatform();
  const nativeClientPushConfigured = import.meta.env.VITE_NATIVE_PUSH_CLIENT_CONFIGURED === '1';
  const nativeDevices = ref<NativeDevice[]>([]);
  const nativePushBusy = ref(false);
  const revokingNativeDevice = ref('');
  const nativeListeners: PluginListenerHandle[] = [];
  const items = ref<NotificationItem[]>([]);
  const deliveries = ref<NotificationDelivery[]>([]);
  const reminders = ref<Reminder[]>([]);
  const error = ref('');
  const loading = ref(false);
  const savingPrefs = ref(false);
  const prefsLoaded = ref(false);
  const channels = ref({ emailConfigured: false, pushConfigured: false, pushPublicKey: '', pushSubscriptions: 0, nativePushConfigured: false, nativePushEnabled: false, nativeDevices: 0 });
  const pushBusy = ref(false);
  const currentPushSubscription = ref<PushSubscription | null>(null);
  const readingAll = ref(false);
  const markingId = ref('');
  const reminderScope = ref('personal');
  const reminderForm = ref({ runAt: '', title: '', body: '' });
  const reminderError = ref('');
  const reminderLoadError = ref('');
  const remindersLoading = ref(false);
  const savingReminder = ref(false);
  const cancellingReminder = ref('');
  const prefs = ref({ enabled: true, emailEnabled: false, pushEnabled: false, nativePushEnabled: false, quietStart: '', quietEnd: '' });
  async function refresh() {
    if (loading.value) return;
    loading.value = true;
    try {
      [items.value, prefs.value, deliveries.value, channels.value] = await Promise.all([
        api<NotificationItem[]>('GET', '/me/notifications'),
        api<typeof prefs.value>('GET', '/me/notification-preferences'),
        api<NotificationDelivery[]>('GET', '/me/notification-deliveries'),
        api<typeof channels.value>('GET', '/me/notification-channels'),
      ]);
      if (isNativeApp) nativeDevices.value = await api<NativeDevice[]>('GET', '/me/notification-channels/native-devices');
      prefsLoaded.value = true;
      currentPushSubscription.value = await readCurrentPushSubscription();
      error.value = '';
      window.dispatchEvent(new Event('teammind:notifications-updated'));
    } catch (e) {
      error.value = (e as Error).message;
    } finally {
      loading.value = false;
    }
  }
  async function savePrefs() {
    if (!prefsLoaded.value || savingPrefs.value) return;
    savingPrefs.value = true;
    try {
      await api('PATCH', '/me/notification-preferences', prefs.value);
      Message.success('偏好已保存');
    } catch (e) {
      Message.error((e as Error).message);
    } finally {
      savingPrefs.value = false;
    }
  }
  function decodeVapidKey(value: string) {
    const padding = '='.repeat((4 - (value.length % 4)) % 4);
    const raw = atob((value + padding).replace(/-/g, '+').replace(/_/g, '/'));
    return Uint8Array.from(raw, (character) => character.charCodeAt(0));
  }
  async function readCurrentPushSubscription() {
    if (!('serviceWorker' in navigator) || !('PushManager' in window)) return null;
    try {
      const registration = await navigator.serviceWorker.getRegistration('/');
      return registration ? registration.pushManager.getSubscription() : null;
    } catch {
      return null;
    }
  }
  async function enablePush() {
    if (pushBusy.value) return;
    if (!window.isSecureContext || !('serviceWorker' in navigator) || !('PushManager' in window) || !('Notification' in window)) {
      Message.error('此浏览器不支持推送，或页面尚未通过 HTTPS/localhost 安全连接打开。');
      return;
    }
    pushBusy.value = true;
    try {
      const permission = window.Notification.permission === 'default' ? await window.Notification.requestPermission() : window.Notification.permission;
      if (permission !== 'granted') throw new Error('浏览器通知权限未允许。');
      const registration = await navigator.serviceWorker.register('/teammind-push.js', { scope: '/' });
      let subscription = await registration.pushManager.getSubscription();
      if (!subscription) subscription = await registration.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: decodeVapidKey(channels.value.pushPublicKey) });
      await api('POST', '/me/notification-channels/push-subscriptions', subscription.toJSON());
      currentPushSubscription.value = subscription;
      await refresh();
      Message.success('此设备的浏览器推送已绑定。勾选“浏览器推送”并保存偏好后开始接收。');
    } catch (e) {
      Message.error((e as Error).message || '启用浏览器推送失败，请重试。');
    } finally {
      pushBusy.value = false;
    }
  }
  async function disablePush() {
    if (pushBusy.value || !currentPushSubscription.value) return;
    pushBusy.value = true;
    try {
      await api('DELETE', '/me/notification-channels/push-subscriptions', { endpoint: currentPushSubscription.value.endpoint });
      await currentPushSubscription.value.unsubscribe();
      currentPushSubscription.value = null;
      prefs.value.pushEnabled = false;
      await api('PATCH', '/me/notification-preferences', prefs.value);
      await refresh();
      Message.success('此设备的推送订阅已撤销。');
    } catch (e) {
      Message.error((e as Error).message || '关闭推送失败，请重试。');
    } finally {
      pushBusy.value = false;
    }
  }
  function nativePlatformName(): 'android' | 'ios' | '' {
    const platform = Capacitor.getPlatform();
    return platform === 'android' || platform === 'ios' ? platform : '';
  }
  async function saveNativeToken(deviceToken: string) {
    const platform = nativePlatformName();
    if (!platform || !deviceToken || !channels.value.nativePushConfigured) return;
    await registerNativeDeviceToken(deviceToken, true);
    nativeDevices.value = await api<NativeDevice[]>('GET', '/me/notification-channels/native-devices');
    channels.value.nativeDevices = nativeDevices.value.filter((device) => device.status === 'active').length;
  }
  async function enableNativePush() {
    if (nativePushBusy.value || !isNativeApp) return;
    nativePushBusy.value = true;
    try {
      const support = await FirebaseMessaging.isSupported();
      if (!support.isSupported) throw new Error('此设备不支持 Firebase 推送。');
      const before = await FirebaseMessaging.checkPermissions();
      const permission = before.receive === 'granted' ? before : await FirebaseMessaging.requestPermissions();
      if (permission.receive !== 'granted') throw new Error('系统通知权限未允许。请在系统设置中开启后重试。');
      if (Capacitor.getPlatform() === 'android') {
        await FirebaseMessaging.createChannel({ id: 'teammind_notifications', name: 'TeamMind 通知', importance: 4, vibration: true, visibility: 0 });
      }
      const { token: deviceToken } = await FirebaseMessaging.getToken();
      if (!deviceToken) throw new Error('Firebase 没有返回设备令牌，请检查 Firebase 客户端配置和网络。');
      await saveNativeToken(deviceToken);
      prefs.value.nativePushEnabled = true;
      await api('PATCH', '/me/notification-preferences', prefs.value);
      await refresh();
      Message.success('此 App 的原生推送已启用。');
    } catch (e) {
      Message.error((e as Error).message || '原生推送启用失败，请重试。');
    } finally {
      nativePushBusy.value = false;
    }
  }
  async function revokeNativeDevice(device: NativeDevice) {
    if (revokingNativeDevice.value || device.status !== 'active') return;
    revokingNativeDevice.value = device.id;
    try {
      await api('DELETE', `/me/notification-channels/native-devices/${device.id}`);
      if (nativeDevices.value.filter((entry) => entry.status === 'active').length <= 1) {
        prefs.value.nativePushEnabled = false;
        await api('PATCH', '/me/notification-preferences', prefs.value);
      }
      await refresh();
      Message.success('原生设备推送已撤销。');
    } catch (e) {
      Message.error((e as Error).message || '撤销失败，请重试。');
    } finally {
      revokingNativeDevice.value = '';
    }
  }
  async function loadReminders() {
    if (remindersLoading.value) return;
    remindersLoading.value = true;
    try {
      reminders.value = await api<Reminder[]>('GET', '/me/reminders');
      reminderLoadError.value = '';
    } catch (e) {
      reminderLoadError.value = (e as Error).message;
    } finally {
      remindersLoading.value = false;
    }
  }
  async function createReminder() {
    if (savingReminder.value) return;
    const runAt = localDateTimeToUTC(
      reminderForm.value.runAt,
      user.timezone || 'Asia/Shanghai'
    );
    if (!runAt) {
      reminderError.value = '时间无效或因夏令时调整不存在，请换一个时间';
      return;
    }
    savingReminder.value = true;
    reminderError.value = '';
    try {
      await api('POST', '/me/reminders', {
        teamId: reminderScope.value === 'team' ? workspace.activeTeam.id : '',
        title: reminderForm.value.title,
        body: reminderForm.value.body,
        runAt,
      });
      reminderForm.value = { runAt: '', title: '', body: '' };
      await loadReminders();
      Message.success('提醒已保存');
    } catch (e) {
      reminderError.value = (e as Error).message;
    } finally {
      savingReminder.value = false;
    }
  }
  async function cancelReminder(reminder: Reminder) {
    if (cancellingReminder.value) return;
    cancellingReminder.value = reminder.id;
    try {
      await api('DELETE', `/me/reminders/${reminder.id}`);
      await loadReminders();
    } catch (e) {
      Message.error((e as Error).message);
    } finally {
      cancellingReminder.value = '';
    }
  }
  function reminderStatus(status: Reminder['status']) {
    return {
      scheduled: '待执行',
      running: '执行中',
      succeeded: '已处理',
      failed: '失败',
      paused: '已暂停',
      cancelled: '已取消',
    }[status];
  }
  async function mark(n: NotificationItem) {
    if (markingId.value) return;
    markingId.value = n.id;
    try {
      await api('PATCH', `/me/notifications/${n.id}`);
      await refresh();
    } catch (e) {
      Message.error((e as Error).message);
    } finally {
      markingId.value = '';
    }
  }
  async function readAll() {
    if (readingAll.value) return;
    readingAll.value = true;
    try {
      await api('POST', '/me/notifications/read-all');
      await refresh();
    } catch (e) {
      Message.error((e as Error).message);
    } finally {
      readingAll.value = false;
    }
  }
  function deliveryStatus(status: NotificationDelivery['status']) {
    return {
      queued: '排队中',
      delivered: '已送达',
      suppressed: '按偏好暂缓',
      failed: '失败待重试',
      sending: '发送中',
      retry: '等待重试',
    }[status];
  }
  function deliveryReason(reason: string) {
    return (
      {
        user_disabled: '你关闭了通知',
        recipient_inactive: '接收账号当前不可用',
        channel_not_configured: '管理员尚未配置此通道',
        no_active_subscription: '没有可用的浏览器推送设备',
        subscription_unavailable: '推送设备订阅已撤销',
        subscription_expired: '浏览器推送订阅已失效',
        email_unavailable: '邮件服务暂时不可用',
        push_unavailable: '浏览器推送服务暂时不可用',
        no_active_device: '没有可用的原生设备',
        device_unavailable: '原生设备推送令牌已撤销',
        device_revoked: '原生设备推送已撤销',
        device_token_expired: '原生设备令牌已失效，请在 App 中重新启用',
        device_decryption_failed: '原生设备凭据无法解密，请重新绑定设备',
        native_push_unavailable: '原生推送服务暂时不可用',
      }[reason] || '暂未投递'
    );
  }
  watch(
    () => workspace.activeTeam.id,
    (id) => {
      if (!id && reminderScope.value === 'team')
        reminderScope.value = 'personal';
    }
  );
  onMounted(() => {
    refresh();
    loadReminders();
    if (isNativeApp) {
      FirebaseMessaging.addListener('notificationReceived', () => {
        window.dispatchEvent(new Event('teammind:notifications-updated'));
      }).then((listener) => nativeListeners.push(listener)).catch(() => undefined);
    }
  });
  onUnmounted(() => { nativeListeners.forEach((listener) => { void listener.remove(); }); });
</script>
<style scoped>
  .tm-reminder-heading h2 {
    margin-bottom: 4px;
  }
  .tm-reminder-heading p {
    margin: 0 0 14px;
    color: var(--color-text-2);
  }
  .tm-reminder-form {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 12px;
    margin-bottom: 16px;
  }
  .tm-reminder-form label {
    display: grid;
    gap: 6px;
    min-width: 0;
  }
  .tm-reminder-form input,
  .tm-reminder-form select,
  .tm-reminder-form textarea {
    width: 100%;
    min-width: 0;
    padding: 9px 10px;
    border: 1px solid var(--color-border-2);
    border-radius: 6px;
    color: var(--color-text-1);
    background: var(--color-bg-2);
    font: inherit;
  }
  .tm-reminder-form textarea {
    resize: vertical;
  }
  .tm-reminder-form label:nth-child(4),
  .tm-reminder-submit {
    grid-column: 1 / -1;
  }
  .tm-reminder-submit {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }
  .tm-reminder-list {
    display: grid;
    gap: 8px;
  }
  .tm-reminder-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 12px;
    border: 1px solid var(--color-border-2);
    border-radius: 8px;
  }
  .tm-reminder-row strong {
    overflow-wrap: anywhere;
  }
  .tm-reminder-row p {
    margin: 5px 0;
    overflow-wrap: anywhere;
  }
  .tm-reminder-row small {
    color: var(--color-text-2);
  }
  .tm-reminder-actions {
    display: flex;
    flex: none;
    align-items: center;
    gap: 8px;
  }
  .tm-delivery-list {
    display: grid;
    gap: 8px;
  }
  .tm-delivery-row {
    display: grid;
    grid-template-columns: minmax(100px, 1fr) minmax(130px, auto);
    gap: 8px 16px;
    padding: 12px;
    border: 1px solid var(--color-border-2);
    border-radius: 8px;
  }
  .tm-delivery-row strong,
  .tm-delivery-row small {
    display: block;
  }
  .tm-delivery-row small {
    margin-top: 4px;
    color: var(--color-text-2);
  }
  .tm-delivery-row p {
    grid-column: 1 / -1;
    margin: 0;
    color: var(--color-text-2);
  }
  .status-delivered {
    color: #16803c;
  }
  .status-suppressed {
    color: #925f00;
  }
  .status-failed {
    color: #c22f37;
  }
  .tm-channel-status {
    margin: 0 0 14px;
    color: var(--color-text-2);
  }
  .tm-native-device {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    width: 100%;
    padding: 8px 0;
    border-top: 1px solid var(--color-border-2);
  }
  @media (max-width: 520px) {
    .tm-delivery-row {
      grid-template-columns: 1fr;
    }
    .tm-delivery-row p {
      grid-column: auto;
    }
    .tm-reminder-form {
      grid-template-columns: 1fr;
    }
    .tm-reminder-form label:nth-child(4),
    .tm-reminder-submit {
      grid-column: auto;
    }
    .tm-reminder-row {
      align-items: flex-start;
      flex-direction: column;
    }
    .tm-reminder-actions {
      width: 100%;
      justify-content: space-between;
    }
  }
</style>
