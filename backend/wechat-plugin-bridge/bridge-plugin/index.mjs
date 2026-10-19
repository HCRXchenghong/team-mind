import { timingSafeEqual } from "node:crypto";
import { isIP } from "node:net";
import { startWeixinLoginWithQr, pollWeixinLoginOnce, cancelWeixinLogin } from "./src/auth/login-qr.js";

const expectedToken = process.env.WECHAT_GATEWAY_BRIDGE_TOKEN || "";
const tokenBuffer = Buffer.from(expectedToken);
const MAX_BODY_BYTES = 32 * 1024;
const SESSION_KEY_PATTERN = /^[a-f0-9]{32}$/i;
const DEFAULT_BASE_URL = "https://ilinkai.weixin.qq.com";

function validBaseUrl(raw) {
  if (typeof raw !== "string" || raw.length > 256) return null;
  let parsed;
  try { parsed = new URL(raw); } catch { return null; }
  if (parsed.protocol !== "https:" || parsed.username || parsed.password || parsed.port || parsed.search || parsed.hash || !["", "/"].includes(parsed.pathname)) return null;
  const host = parsed.hostname.toLowerCase();
  if (host !== "ilinkai.weixin.qq.com" && !/^[a-z0-9.-]+\.weixin\.qq\.com$/.test(host)) return null;
  return `https://${host}`;
}

function validSecret(raw) {
  return typeof raw === "string" && raw.length >= 16 && raw.length <= 8192;
}

function isLoopback(address = "") {
  const normalized = address.startsWith("::ffff:") ? address.slice(7) : address;
  if (normalized === "::1" || normalized === "127.0.0.1") return true;
  return isIP(normalized) === 4 && normalized.startsWith("127.");
}

function authorized(request) {
  const candidate = Buffer.from((request.headers.authorization || "").replace(/^Bearer /, ""));
  return tokenBuffer.length >= 32 && candidate.length === tokenBuffer.length && timingSafeEqual(candidate, tokenBuffer);
}

async function readBody(request) {
  const chunks = [];
  let size = 0;
  for await (const chunk of request) {
    size += chunk.length;
    if (size > MAX_BODY_BYTES) throw new Error("request body too large");
    chunks.push(chunk);
  }
  return JSON.parse(Buffer.concat(chunks).toString("utf8") || "{}");
}

function send(response, status, body) {
  response.writeHead(status, {
    "content-type": "application/json; charset=utf-8",
    "cache-control": "no-store",
    "x-content-type-options": "nosniff",
  });
  response.end(JSON.stringify(body));
}

export default {
  id: "teammind-wechat-bridge",
  name: "TeamMind WeChat Bridge",
  description: "Authenticated loopback endpoints for TeamMind QR sessions",
  register(api) {
    registerTeamMindWechatBridge(api);
  },
};

export function registerTeamMindWechatBridge(api) {
  if (tokenBuffer.length < 32) throw new Error("WECHAT_GATEWAY_BRIDGE_TOKEN must contain at least 32 bytes");
  // Use the pinned Tencent plugin's own API adapter for message polling and
  // sending. The Go worker never speaks the WeChat protocol directly.
  const pluginApi = import("./src/api/api.js");
  api.registerHttpRoute({
      path: "/teammind-wechat-bridge/v1",
      match: "prefix",
      auth: "plugin",
      handler: async (request, response) => {
        const url = new URL(request.url || "/", "http://127.0.0.1");
        if (!url.pathname.startsWith("/teammind-wechat-bridge/v1/")) return false;
        if (!isLoopback(request.socket.remoteAddress) || !authorized(request)) {
          send(response, 404, { error: "not_found" });
          return true;
        }
        if (request.method !== "POST") {
          send(response, 405, { error: "method_not_allowed" });
          return true;
        }
        try {
          const input = await readBody(request);
          const route = url.pathname.slice("/teammind-wechat-bridge/v1/".length);
          if (route === "sessions/start") {
            if (typeof input.sessionKey !== "string" || !SESSION_KEY_PATTERN.test(input.sessionKey)) {
              send(response, 400, { error: "invalid_session" });
              return true;
            }
            const started = await startWeixinLoginWithQr({ accountId: input.sessionKey, force: true, apiBaseUrl: "https://ilinkai.weixin.qq.com" });
            if (!started.qrcodeUrl) {
              send(response, 503, { error: "channel_unavailable" });
              return true;
            }
            send(response, 201, { sessionKey: started.sessionKey, qrContent: started.qrcodeUrl, status: "wait" });
            return true;
          }
          if (route === "sessions/poll") {
            if (typeof input.sessionKey !== "string" || !SESSION_KEY_PATTERN.test(input.sessionKey) || (input.verifyCode != null && typeof input.verifyCode !== "string")) {
              send(response, 400, { error: "invalid_session" });
              return true;
            }
            send(response, 200, await pollWeixinLoginOnce({ sessionKey: input.sessionKey, verifyCode: input.verifyCode }));
            return true;
          }
          if (route === "sessions/cancel") {
            if (typeof input.sessionKey !== "string" || !SESSION_KEY_PATTERN.test(input.sessionKey)) {
              send(response, 400, { error: "invalid_session" });
              return true;
            }
            send(response, 200, { cancelled: cancelWeixinLogin(input.sessionKey) });
            return true;
          }
          if (route === "channel/updates") {
            const baseUrl = validBaseUrl(input.baseUrl || DEFAULT_BASE_URL);
            if (!validSecret(input.token) || !baseUrl || typeof input.cursor !== "string" || input.cursor.length > 8192 || typeof input.accountId !== "string" || !SESSION_KEY_PATTERN.test(input.accountId)) {
              send(response, 400, { error: "invalid_channel_request" });
              return true;
            }
            const controller = new AbortController();
            const onAbort = () => controller.abort();
            const onResponseClose = () => { if (!response.writableEnded) controller.abort(); };
            request.once("aborted", onAbort);
            response.once("close", onResponseClose);
            try {
              const { getUpdates } = await pluginApi;
              const result = await getUpdates({ baseUrl, token: input.token, get_updates_buf: input.cursor, timeoutMs: 35000, abortSignal: controller.signal });
              const messages = Array.isArray(result.msgs) ? result.msgs.slice(0, 100).map((message) => ({
                message_id: String(message.message_id ?? ""),
                from_user_id: String(message.from_user_id ?? ""),
                group_id: String(message.group_id ?? ""),
                message_type: Number(message.message_type ?? 0),
                context_token: String(message.context_token ?? ""),
                item_list: Array.isArray(message.item_list) ? message.item_list.slice(0, 32).map((item) => ({
                  type: Number(item.type ?? 0),
                  ...(typeof item.text_item?.text === "string" ? { text_item: { text: item.text_item.text.slice(0, 12000) } } : {}),
                })) : [],
              })) : [];
              send(response, 200, { ret: Number(result.ret ?? 0), errcode: Number(result.errcode ?? 0), msgs: messages, get_updates_buf: String(result.get_updates_buf ?? input.cursor).slice(0, 8192) });
            } finally {
              request.off("aborted", onAbort);
              response.off("close", onResponseClose);
            }
            return true;
          }
          if (route === "channel/send-text") {
            const baseUrl = validBaseUrl(input.baseUrl || DEFAULT_BASE_URL);
            if (!validSecret(input.token) || !baseUrl || typeof input.accountId !== "string" || !SESSION_KEY_PATTERN.test(input.accountId) || typeof input.to !== "string" || input.to.length < 1 || input.to.length > 1024 || typeof input.text !== "string" || input.text.length < 1 || input.text.length > 12000 || (input.contextToken != null && (typeof input.contextToken !== "string" || input.contextToken.length > 8192)) || typeof input.clientId !== "string" || !/^[A-Za-z0-9._:-]{1,160}$/.test(input.clientId)) {
              send(response, 400, { error: "invalid_channel_request" });
              return true;
            }
            const { sendMessage } = await pluginApi;
            const result = await sendMessage({
              baseUrl,
              token: input.token,
              body: { msg: {
                from_user_id: "",
                to_user_id: input.to,
                client_id: input.clientId,
                message_type: 2,
                message_state: 2,
                item_list: [{ type: 1, text_item: { text: input.text } }],
                ...(input.contextToken ? { context_token: input.contextToken } : {}),
              } },
            });
            send(response, 200, { messageId: result.message_id == null ? "" : String(result.message_id), ret: Number(result.ret ?? 0) });
            return true;
          }
          send(response, 404, { error: "not_found" });
          return true;
        } catch {
          if (response.destroyed) return true;
          send(response, 503, { error: "channel_unavailable" });
          return true;
        }
      },
  });
}
