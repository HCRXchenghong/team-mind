import { spawnSync } from 'node:child_process';
import { existsSync } from 'node:fs';
import path from 'node:path';

const apiBase = process.env.TEAMMIND_API_BASE_URL?.trim();
if (!apiBase) {
  throw new Error('Set TEAMMIND_API_BASE_URL to the deployed TeamMind HTTPS API before building a native bundle.');
}

const parsed = new URL(apiBase);
const allowInsecure = process.env.ALLOW_INSECURE_NATIVE_API === '1';
if (parsed.protocol !== 'https:' && !(allowInsecure && ['localhost', '127.0.0.1'].includes(parsed.hostname))) {
  throw new Error('Native builds require HTTPS. Insecure localhost is allowed only with ALLOW_INSECURE_NATIVE_API=1 for development.');
}
if (!parsed.pathname.endsWith('/api/v1')) {
  throw new Error('TEAMMIND_API_BASE_URL must end with /api/v1.');
}

const target = process.env.NATIVE_TARGET || 'all';
if (!['android', 'ios', 'all'].includes(target)) throw new Error('NATIVE_TARGET must be android, ios, or all.');
const requiredFirebaseFiles = target === 'all'
  ? ['android/app/google-services.json', 'ios/App/App/GoogleService-Info.plist']
  : [target === 'android' ? 'android/app/google-services.json' : 'ios/App/App/GoogleService-Info.plist'];
const missingFirebaseFiles = requiredFirebaseFiles.filter((file) => !existsSync(path.resolve(file)));
const nativePushOptional = process.env.NATIVE_PUSH_OPTIONAL === '1';
if (missingFirebaseFiles.length && !nativePushOptional) {
  throw new Error(`Native push requires Firebase client configuration for ${target}: ${missingFirebaseFiles.join(', ')}. Obtain the files from the TeamMind Firebase project; do not commit them.`);
}

const env = {
  ...process.env,
  VITE_API_BASE_URL: apiBase.replace(/\/$/, ''),
  VITE_MOBILE_BUILD: '1',
  VITE_NATIVE_PUSH_CLIENT_CONFIGURED: missingFirebaseFiles.length ? '0' : '1',
};
const npm = process.platform === 'win32' ? 'npm.cmd' : 'npm';
const build = spawnSync(npm, ['run', 'build'], { stdio: 'inherit', env });
if (build.status !== 0) process.exit(build.status ?? 1);

const syncArgs = target === 'all' ? ['exec', '--', 'cap', 'sync'] : ['exec', '--', 'cap', 'sync', target];
const sync = spawnSync(npm, syncArgs, { stdio: 'inherit', env });
if (sync.status !== 0) process.exit(sync.status ?? 1);
