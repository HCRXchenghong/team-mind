import type { CapacitorConfig } from '@capacitor/cli';

const allowInsecureLocalApi = process.env.ALLOW_INSECURE_NATIVE_API === '1';

const config: CapacitorConfig = {
  appId: 'com.teammind.workspace',
  appName: 'TeamMind',
  webDir: 'dist',
  server: {
    androidScheme: allowInsecureLocalApi ? 'http' : 'https',
    iosScheme: 'capacitor',
  },
  plugins: {
    FirebaseMessaging: {
      presentationOptions: ['badge', 'sound', 'alert'],
    },
  },
};

export default config;
