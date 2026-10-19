import { Capacitor } from '@capacitor/core';
import { api } from '@/api/workspace';
import { getToken } from '@/utils/auth';

const registeredDevices = new Set<string>();
const registrationsInFlight = new Map<string, Promise<void>>();

export function registerNativeDeviceToken(deviceToken: string, force = false): Promise<void> {
  const platform = Capacitor.getPlatform();
  const session = getToken();
  if (!session || !deviceToken || (platform !== 'android' && platform !== 'ios')) return Promise.resolve();
  const identity = `${platform}:${session}:${deviceToken}`;
  if (!force && registeredDevices.has(identity)) return Promise.resolve();
  const current = registrationsInFlight.get(identity);
  if (current) return current;
  const permission = force
    ? Promise.resolve(true)
    : api<{ nativePushEnabled: boolean }>('GET', '/me/notification-preferences').then((preferences) => preferences.nativePushEnabled);
  const registration = permission.then((enabled) => {
    if (!enabled) return;
    return api('POST', '/me/notification-channels/native-devices', { platform, token: deviceToken }).then(() => {
      registeredDevices.add(identity);
    });
  })
    .finally(() => registrationsInFlight.delete(identity));
  registrationsInFlight.set(identity, registration);
  return registration;
}
