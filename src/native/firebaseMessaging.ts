import { registerPlugin } from '@capacitor/core';
import type { FirebaseMessagingPlugin } from '@capacitor-firebase/messaging/dist/esm/definitions';

// The native runtime provides this plugin. Web Push stays on the browser's
// PushManager path and does not bundle Firebase's JavaScript Messaging SDK.
export const FirebaseMessaging = registerPlugin<FirebaseMessagingPlugin>('FirebaseMessaging');
