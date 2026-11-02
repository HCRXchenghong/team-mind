import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.dirname(fileURLToPath(import.meta.url));
const packagePath = path.join(root, "node_modules/@tencent-weixin/openclaw-weixin/package.json");
const modulePath = path.join(root, "node_modules/@tencent-weixin/openclaw-weixin/dist/src/auth/login-qr.js");
const apiPath = path.join(root, "node_modules/@tencent-weixin/openclaw-weixin/dist/src/api/api.js");
const entryPath = path.join(root, "node_modules/@tencent-weixin/openclaw-weixin/dist/index.js");
const bridgeSourcePath = path.join(root, "bridge-plugin/index.mjs");
const bridgeTargetPath = path.join(root, "node_modules/@tencent-weixin/openclaw-weixin/dist/teammind-bridge.mjs");
const pkg = JSON.parse(fs.readFileSync(packagePath, "utf8"));
if (pkg.version !== "2.4.10") {
  throw new Error(`Expected Tencent Weixin plugin 2.4.10, found ${pkg.version}`);
}

let apiSource = fs.readFileSync(apiPath, "utf8");
const getStart = apiSource.indexOf("export async function apiGetFetch(params) {");
const getEnd = apiSource.indexOf("\n}\n/**\n * Combine an internal timeout controller", getStart);
if (getStart < 0 || getEnd < 0) throw new Error("Tencent Weixin GET adapter does not match the reviewed 2.4.10 build");
let getFunction = apiSource.slice(getStart, getEnd + 2);
if (!getFunction.includes("external: params.abortSignal")) {
  const setup = `    const t = controller != null && timeoutMs != null
        ? setTimeout(() => controller.abort(), timeoutMs)
        : undefined;
    try {`;
  const setupWithAbort = `    const t = controller != null && timeoutMs != null
        ? setTimeout(() => controller.abort(), timeoutMs)
        : undefined;
    const { signal, cleanup } = combineAbortSignals({ internal: controller, external: params.abortSignal });
    try {`;
  if (!getFunction.includes(setup)) throw new Error("Tencent Weixin GET timeout boundary changed; refusing to patch");
  getFunction = getFunction.replace(setup, setupWithAbort);
  getFunction = getFunction.replace('...(controller ? { signal: controller.signal } : {}),', '...(signal ? { signal } : {}),');
  getFunction = getFunction.replace(`        throw err;
    }
}`, `        throw err;
    }
    finally {
        cleanup();
    }
}`);
  apiSource = apiSource.slice(0, getStart) + getFunction + apiSource.slice(getEnd + 2);
  fs.writeFileSync(apiPath, apiSource, { mode: 0o600 });
}

let source = fs.readFileSync(modulePath, "utf8");
source = source.replace(
  "async function pollQRStatus(apiBaseUrl, qrcode, verifyCode) {",
  "async function pollQRStatus(apiBaseUrl, qrcode, verifyCode, abortSignal, reportNetworkErrors = false) {",
);
if (!source.includes("if (reportNetworkErrors) throw err;")) {
  const fallback = '        logger.warn(`pollQRStatus: network/gateway error, will retry: ${String(err)}`);\n        return { status: "wait" };';
  if (!source.includes(fallback)) throw new Error("Tencent Weixin QR poller error handling does not match the reviewed 2.4.10 build");
  source = source.replace(fallback, `        if (reportNetworkErrors) throw err;\n${fallback}`);
}
source = source.replace(
  "timeoutMs: QR_LONG_POLL_TIMEOUT_MS,\n            label: \"pollQRStatus\",",
  "timeoutMs: QR_LONG_POLL_TIMEOUT_MS,\n            abortSignal,\n            label: \"pollQRStatus\",",
);
if (!source.includes("export async function pollWeixinLoginOnce")) {
  const insertionPoint = "export async function waitForWeixinLogin(opts) {";
  if (!source.includes(insertionPoint)) {
    throw new Error("Tencent Weixin login module does not match the reviewed 2.4.10 build");
  }
  const bridgeExports = `
// TeamMind bridge: one protocol poll per authenticated local request. This
// avoids the upstream CLI waiter's stdin prompt and keeps tokens out of its
// on-disk OpenClaw account store.
export async function pollWeixinLoginOnce({ sessionKey, verifyCode }) {
    const login = activeLogins.get(sessionKey);
    if (!login) return { status: "missing" };
    if (!isLoginFresh(login)) {
        activeLogins.delete(sessionKey);
        return { status: "expired" };
    }
    login.currentApiBaseUrl ??= FIXED_BASE_URL;
    if (typeof verifyCode === "string" && verifyCode.trim()) {
        login.pendingVerifyCode = verifyCode.trim();
    }
    const pollController = new AbortController();
    login.pollController = pollController;
    let response;
    try {
        response = await pollQRStatus(login.currentApiBaseUrl, login.qrcode, login.pendingVerifyCode, pollController.signal, true);
    } finally {
        if (login.pollController === pollController) delete login.pollController;
    }
    login.status = response.status;
    if (response.status === "scaned" && login.pendingVerifyCode) login.pendingVerifyCode = undefined;
    if (response.status === "verify_code_blocked") login.pendingVerifyCode = undefined;
    if (response.status === "scaned_but_redirect") {
        const host = response.redirect_host || "";
        if (host && /^[A-Za-z0-9.-]+\\.weixin\\.qq\\.com$/.test(host)) login.currentApiBaseUrl = "https://" + host;
        return { status: response.status };
    }
    if (response.status === "expired") {
        activeLogins.delete(sessionKey);
        return { status: "expired" };
    }
    if (response.status === "binded_redirect") {
        activeLogins.delete(sessionKey);
        return { status: "already_bound" };
    }
    if (response.status === "confirmed") {
        activeLogins.delete(sessionKey);
        if (!response.bot_token || !response.ilink_bot_id || !response.ilink_user_id) return { status: "failed" };
        return {
            status: "confirmed",
            botToken: response.bot_token,
            accountId: response.ilink_bot_id,
            userId: response.ilink_user_id,
            baseUrl: response.baseurl || login.currentApiBaseUrl || FIXED_BASE_URL,
        };
    }
    if (!["wait", "scaned", "need_verifycode", "verify_code_blocked"].includes(response.status)) {
        activeLogins.delete(sessionKey);
        return { status: "failed" };
    }
    return { status: response.status };
  }
export function cancelWeixinLogin(sessionKey) {
    const login = activeLogins.get(sessionKey);
    if (!login) return false;
    login.pollController?.abort();
    return activeLogins.delete(sessionKey);
}

`;
  source = source.replace(insertionPoint, bridgeExports + insertionPoint);
}
if (!source.includes("async function pollQRStatus(apiBaseUrl, qrcode, verifyCode, abortSignal, reportNetworkErrors = false)")) {
  throw new Error("Tencent Weixin QR poller does not match the reviewed 2.4.10 build");
}
if (!source.includes("            abortSignal,\n            label: \"pollQRStatus\",")) {
  throw new Error("Could not apply cancellation support to Tencent Weixin QR poller");
}
if (!source.includes("reportNetworkErrors = false") || !source.includes("if (reportNetworkErrors) throw err;")) {
  throw new Error("Could not apply explicit Tencent channel error reporting");
}
const qrLog = "logger.info(`二维码链接: ${qrResponse.qrcode_img_content}`);";
source = source.replace(qrLog, "logger.info(`二维码内容已获取，长度=${qrResponse.qrcode_img_content?.length ?? 0}`);");
fs.writeFileSync(modulePath, source, { mode: 0o600 });

let entry = fs.readFileSync(entryPath, "utf8");
const entryMarker = 'import { registerTeamMindWechatBridge } from "./teammind-bridge.mjs";';
if (!entry.includes(entryMarker)) {
  const importAnchor = 'import { weixinPlugin } from "./src/channel.js";';
  const registerAnchor = "api.registerChannel({ plugin: weixinPlugin });";
  if (!entry.includes(importAnchor) || !entry.includes(registerAnchor)) {
    throw new Error("Tencent Weixin entry point does not match the reviewed 2.4.10 build");
  }
  entry = entry.replace(importAnchor, `${entryMarker}\n${importAnchor}`);
  entry = entry.replace(registerAnchor, `${registerAnchor}\n        registerTeamMindWechatBridge(api);`);
  fs.writeFileSync(entryPath, entry, { mode: 0o600 });
}
fs.copyFileSync(bridgeSourcePath, bridgeTargetPath);
fs.chmodSync(bridgeTargetPath, 0o600);
