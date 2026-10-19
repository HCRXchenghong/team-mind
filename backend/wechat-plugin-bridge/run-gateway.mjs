import crypto from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { spawn } from "node:child_process";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

const root = path.dirname(fileURLToPath(import.meta.url));
const require = createRequire(import.meta.url);
const [nodeMajor, nodeMinor] = process.versions.node.split(".").map(Number);
const supportedNode = (nodeMajor === 24 && nodeMinor >= 16) || (nodeMajor >= 26 && (nodeMajor > 26 || nodeMinor >= 1));
if (!supportedNode) throw new Error("OpenClaw 2026.10.4 requires Node.js >=24.16,<25 or >=26.1");
const envPath = process.env.TEAMMIND_ENV_FILE || path.resolve(root, "../../.env");
if (fs.existsSync(envPath)) {
  const allowed = new Set(["WECHAT_GATEWAY_BRIDGE_TOKEN", "WECHAT_GATEWAY_PORT", "OPENCLAW_STATE_DIR"]);
  for (const line of fs.readFileSync(envPath, "utf8").split(/\r?\n/)) {
    const match = line.match(/^\s*([A-Z0-9_]+)\s*=\s*(.*?)\s*$/);
    if (!match || !allowed.has(match[1]) || process.env[match[1]]) continue;
    let value = match[2];
    if ((value.startsWith('"') && value.endsWith('"')) || (value.startsWith("'") && value.endsWith("'"))) value = value.slice(1, -1);
    process.env[match[1]] = value;
  }
}
let openclawRoot = path.dirname(require.resolve("openclaw"));
while (openclawRoot !== path.dirname(openclawRoot)) {
  const packagePath = path.join(openclawRoot, "package.json");
  if (fs.existsSync(packagePath) && JSON.parse(fs.readFileSync(packagePath, "utf8")).name === "openclaw") break;
  openclawRoot = path.dirname(openclawRoot);
}
if (!fs.existsSync(path.join(openclawRoot, "openclaw.mjs"))) throw new Error("Could not locate the pinned OpenClaw CLI");
const runtimeBase = process.env.XDG_RUNTIME_DIR || "/dev/shm";
const stateDir = process.env.OPENCLAW_STATE_DIR || path.join(runtimeBase, `teammind-openclaw-weixin-${process.getuid?.() ?? "service"}`);
const port = Number.parseInt(process.env.WECHAT_GATEWAY_PORT || "18793", 10);
if (!Number.isInteger(port) || port < 1024 || port > 65535) throw new Error("WECHAT_GATEWAY_PORT is invalid");

const bridgeToken = process.env.WECHAT_GATEWAY_BRIDGE_TOKEN || "";
if (Buffer.byteLength(bridgeToken) < 32) throw new Error("WECHAT_GATEWAY_BRIDGE_TOKEN must contain at least 32 bytes");

fs.mkdirSync(stateDir, { recursive: true, mode: 0o700 });
fs.chmodSync(stateDir, 0o700);
const configPath = path.join(stateDir, "openclaw.json");
const config = {
  gateway: {
    mode: "local",
    bind: "loopback",
    port,
    auth: { mode: "token", token: crypto.randomBytes(32).toString("base64url") },
  },
  plugins: {
    enabled: true,
    allow: ["openclaw-weixin"],
    load: { paths: [path.join(root, "node_modules/@tencent-weixin/openclaw-weixin")] },
    entries: {
      "openclaw-weixin": { enabled: true },
    },
  },
};
fs.writeFileSync(configPath, `${JSON.stringify(config, null, 2)}\n`, { mode: 0o600 });
fs.chmodSync(configPath, 0o600);

const child = spawn(process.execPath, [path.join(openclawRoot, "openclaw.mjs"), "gateway", "run", "--allow-unconfigured", "--bind", "loopback", "--port", String(port)], {
  cwd: root,
  stdio: "inherit",
  env: {
    ...process.env,
    OPENCLAW_STATE_DIR: stateDir,
    OPENCLAW_CONFIG_PATH: configPath,
    OPENCLAW_LOG_LEVEL: "error",
  },
});

for (const signal of ["SIGINT", "SIGTERM"]) {
  process.on(signal, () => child.kill(signal));
}
child.on("exit", (code, signal) => {
  process.exitCode = signal ? 1 : (code ?? 1);
});
