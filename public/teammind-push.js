self.addEventListener('push', (event) => {
  let payload = {};
  try {
    payload = event.data ? event.data.json() : {};
  } catch (_) {
    payload = {};
  }
  const title = typeof payload.title === 'string' ? payload.title : 'TeamMind 新通知';
  const body = typeof payload.body === 'string' ? payload.body : '你有一条新的通知，请登录查看。';
  const target = typeof payload.url === 'string' ? payload.url : '/teammind/notifications';
  event.waitUntil(self.registration.showNotification(title, {
    body,
    icon: '/logo.svg',
    badge: '/logo.svg',
    tag: 'teammind-inbox',
    data: { target },
  }));
});

self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  let path = '/teammind/notifications';
  try {
    const parsed = new URL(event.notification.data?.target || path, self.location.origin);
    if (parsed.origin === self.location.origin) path = parsed.pathname + parsed.search;
  } catch (_) {
    // Keep the same-origin inbox fallback.
  }
  event.waitUntil(clients.matchAll({ type: 'window', includeUncontrolled: true }).then((windows) => {
    for (const windowClient of windows) {
      if (windowClient.url.startsWith(self.location.origin) && 'focus' in windowClient) {
        windowClient.navigate(path);
        return windowClient.focus();
      }
    }
    return clients.openWindow(path);
  }));
});
