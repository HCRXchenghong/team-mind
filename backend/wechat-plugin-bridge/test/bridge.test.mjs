import assert from "node:assert/strict";
import { Readable } from "node:stream";
import test from "node:test";

const bridgeToken = "unit-test-bridge-token-0123456789abcdef";
process.env.WECHAT_GATEWAY_BRIDGE_TOKEN = bridgeToken;
const { registerTeamMindWechatBridge } = await import("../node_modules/@tencent-weixin/openclaw-weixin/dist/teammind-bridge.mjs");

function createRoute() {
  let handler;
  registerTeamMindWechatBridge({ registerHttpRoute(route) { handler = route.handler; } });
  return handler;
}

async function invoke(handler, path, { body = {}, token = bridgeToken, remote = "127.0.0.1", method = "POST" } = {}) {
  const request = Readable.from([Buffer.from(JSON.stringify(body))]);
  request.url = path;
  request.method = method;
  request.headers = token == null ? {} : { authorization: `Bearer ${token}` };
  request.socket = { remoteAddress: remote };
  let status;
  let result;
  const listeners = new Map();
  const response = {
    writableEnded: false,
    destroyed: false,
    once(event, callback) { listeners.set(event, callback); },
    off(event) { listeners.delete(event); },
    writeHead(code) { status = code; },
    end(payload) { this.writableEnded = true; result = payload ? JSON.parse(payload) : undefined; },
  };
  const handled = await handler(request, response);
  return { handled, status, body: result };
}

test("bridge rejects unauthenticated, non-loopback, and malformed channel calls", async () => {
  const route = createRoute();
  assert.equal((await invoke(route, "/teammind-wechat-bridge/v1/channel/updates", { token: "wrong" })).status, 404);
  assert.equal((await invoke(route, "/teammind-wechat-bridge/v1/channel/updates", { remote: "192.0.2.1" })).status, 404);
  assert.equal((await invoke(route, "/teammind-wechat-bridge/v1/channel/updates", { body: { accountId: "a".repeat(32), baseUrl: "http://127.0.0.1/", token: "x".repeat(32), cursor: "" } })).status, 400);
  assert.equal((await invoke(route, "/teammind-wechat-bridge/v1/sessions/start", { body: { sessionKey: "not-an-id" } })).status, 400);
  assert.equal((await invoke(route, "/teammind-wechat-bridge/v1/admin/config")).status, 404);
});

test("Tencent plugin API adapter handles updates and sends text through the bridge", async (t) => {
  const route = createRoute();
  const requests = [];
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });
  globalThis.fetch = async (url, options) => {
    requests.push({ url: String(url), options });
    if (String(url).endsWith("/ilink/bot/getupdates")) {
      return new Response(JSON.stringify({ ret: 0, errcode: 0, get_updates_buf: "next", msgs: [{ message_id: 18446744073709551615n.toString(), from_user_id: "sender", message_type: 1, context_token: "ctx", item_list: [{ type: 1, text_item: { text: "hello" } }] }] }), { status: 200 });
    }
    if (String(url).endsWith("/ilink/bot/sendmessage")) {
      return new Response(JSON.stringify({ ret: 0, message_id: "sent-1" }), { status: 200 });
    }
    throw new Error("unexpected test network request");
  };

  const update = await invoke(route, "/teammind-wechat-bridge/v1/channel/updates", {
    body: { accountId: "a".repeat(32), token: "private-channel-token-123", baseUrl: "https://ilinkai.weixin.qq.com", cursor: "before" },
  });
  assert.equal(update.status, 200);
  assert.equal(update.body.get_updates_buf, "next");
  assert.equal(update.body.msgs[0].message_id, "18446744073709551615");
  assert.equal(requests[0].options.headers.Authorization, "Bearer private-channel-token-123");
  assert.match(requests[0].url, /ilink\/bot\/getupdates$/);

  const sent = await invoke(route, "/teammind-wechat-bridge/v1/channel/send-text", {
    body: { accountId: "a".repeat(32), token: "private-channel-token-123", baseUrl: "https://ilinkai.weixin.qq.com", to: "sender", contextToken: "ctx", clientId: "teammind-test-1", text: "reply" },
  });
  assert.equal(sent.status, 200);
  assert.equal(sent.body.messageId, "sent-1");
  const outgoing = JSON.parse(requests[1].options.body);
  assert.equal(outgoing.msg.to_user_id, "sender");
  assert.equal(outgoing.msg.context_token, "ctx");
  assert.equal(outgoing.msg.item_list[0].text_item.text, "reply");
});

test("cancel aborts the Tencent QR long poll and removes the temporary session", async (t) => {
  const route = createRoute();
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });
  let statusPollStarted;
  const started = new Promise((resolve) => { statusPollStarted = resolve; });
  let statusPollAborted = false;
  globalThis.fetch = async (url, options) => {
    if (String(url).includes("get_bot_qrcode?")) {
      return new Response(JSON.stringify({ qrcode: "temporary-secret", qrcode_img_content: "https://wechat.example/qr" }), { status: 200 });
    }
    if (String(url).includes("get_qrcode_status?")) {
      statusPollStarted();
      return await new Promise((resolve, reject) => {
        options.signal.addEventListener("abort", () => {
          statusPollAborted = true;
          const error = new Error("aborted");
          error.name = "AbortError";
          reject(error);
        }, { once: true });
      });
    }
    throw new Error("unexpected test network request");
  };
  const sessionKey = "b".repeat(32);
  const qr = await invoke(route, "/teammind-wechat-bridge/v1/sessions/start", { body: { sessionKey } });
  assert.equal(qr.status, 201);
  assert.equal(qr.body.qrContent, "https://wechat.example/qr");
  const pollPromise = invoke(route, "/teammind-wechat-bridge/v1/sessions/poll", { body: { sessionKey } });
  await started;
  const cancelled = await invoke(route, "/teammind-wechat-bridge/v1/sessions/cancel", { body: { sessionKey } });
  assert.deepEqual(cancelled.body, { cancelled: true });
  await pollPromise;
  assert.equal(statusPollAborted, true);
  assert.equal((await invoke(route, "/teammind-wechat-bridge/v1/sessions/poll", { body: { sessionKey } })).body.status, "missing");
});

test("confirmed Tencent login returns identity credentials once and removes the temporary session", async (t) => {
  const route = createRoute();
  const originalFetch = globalThis.fetch;
  t.after(() => { globalThis.fetch = originalFetch; });
  const requests = [];
  globalThis.fetch = async (url, options) => {
    requests.push({ url: String(url), options });
    if (String(url).includes("get_bot_qrcode?")) {
      return new Response(JSON.stringify({ qrcode: "temporary-qr-secret", qrcode_img_content: "https://wechat.example/qr" }), { status: 200 });
    }
    if (String(url).includes("get_qrcode_status?")) {
      return new Response(JSON.stringify({
        status: "confirmed",
        bot_token: "temporary-channel-token",
        ilink_bot_id: "confirmed-channel-account",
        ilink_user_id: "confirmed-scanner-user",
        baseurl: "https://hk.weixin.qq.com",
      }), { status: 200 });
    }
    throw new Error("unexpected test network request");
  };

  const sessionKey = "c".repeat(32);
  const qr = await invoke(route, "/teammind-wechat-bridge/v1/sessions/start", { body: { sessionKey } });
  assert.equal(qr.status, 201);
  assert.equal(qr.body.qrContent, "https://wechat.example/qr");

  const confirmed = await invoke(route, "/teammind-wechat-bridge/v1/sessions/poll", { body: { sessionKey } });
  assert.equal(confirmed.status, 200);
  assert.deepEqual(confirmed.body, {
    status: "confirmed",
    botToken: "temporary-channel-token",
    accountId: "confirmed-channel-account",
    userId: "confirmed-scanner-user",
    baseUrl: "https://hk.weixin.qq.com",
  });
  assert.match(requests[0].url, /ilink\/bot\/get_bot_qrcode/);
  assert.match(requests[1].url, /ilink\/bot\/get_qrcode_status/);
  assert.equal((await invoke(route, "/teammind-wechat-bridge/v1/sessions/poll", { body: { sessionKey } })).body.status, "missing");
});
