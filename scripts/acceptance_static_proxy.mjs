import fs from 'node:fs';
import http from 'node:http';
import path from 'node:path';

const root = path.resolve(process.env.TEAMMIND_STATIC_ROOT || 'dist');
const listenHost = process.env.TEAMMIND_STATIC_HOST || '127.0.0.1';
const listenPort = Number(process.env.TEAMMIND_STATIC_PORT || 5240);
const apiHost = process.env.TEAMMIND_API_HOST || '127.0.0.1';
const apiPort = Number(process.env.TEAMMIND_API_PORT || 8240);
const isLoopback = apiHost === '::1' || /^127(?:\.\d{1,3}){3}$/.test(apiHost);
if (!Number.isInteger(listenPort) || listenPort < 1024 || listenPort > 65535) throw new Error('验收网页端口无效');
if (!Number.isInteger(apiPort) || apiPort < 1024 || apiPort > 65535 || !isLoopback) throw new Error('验收 API 必须使用有效端口和回环地址');
if (!fs.existsSync(path.join(root, 'index.html'))) throw new Error('静态构建目录中没有 index.html');

const mime = new Map([
  ['.html', 'text/html; charset=utf-8'], ['.js', 'text/javascript; charset=utf-8'],
  ['.css', 'text/css; charset=utf-8'], ['.svg', 'image/svg+xml'], ['.png', 'image/png'],
  ['.webp', 'image/webp'], ['.ico', 'image/x-icon'], ['.woff', 'font/woff'],
  ['.woff2', 'font/woff2'], ['.json', 'application/json; charset=utf-8'],
]);
const securityHeaders = {
  'X-Content-Type-Options': 'nosniff',
  'Referrer-Policy': 'strict-origin-when-cross-origin',
  'X-Frame-Options': 'DENY',
  'Permissions-Policy': 'camera=(), microphone=(), geolocation=()',
};

const server = http.createServer((request, response) => {
  const requestUrl = new URL(request.url || '/', `http://${listenHost}:${listenPort}`);
  if (requestUrl.pathname === '/api/v1' || requestUrl.pathname.startsWith('/api/v1/')) {
    const headers = { ...request.headers, host: `${apiHost}:${apiPort}` };
    delete headers.origin;
    delete headers.referer;
    const upstream = http.request({ hostname: apiHost, port: apiPort, method: request.method, path: requestUrl.pathname + requestUrl.search, headers }, (upstreamResponse) => {
      response.writeHead(upstreamResponse.statusCode || 502, { ...securityHeaders, ...upstreamResponse.headers });
      upstreamResponse.pipe(response);
    });
    upstream.on('error', () => {
      if (!response.headersSent) response.writeHead(502, { ...securityHeaders, 'Content-Type': 'application/json; charset=utf-8' });
      response.end('{"code":"API_UNAVAILABLE","message":"本地验收 API 暂时不可用"}');
    });
    request.pipe(upstream);
    return;
  }

  let decodedPath;
  try { decodedPath = decodeURIComponent(requestUrl.pathname); }
  catch { response.writeHead(400, securityHeaders); response.end(); return; }
  const target = path.resolve(root, `.${decodedPath}`);
  if (target !== root && !target.startsWith(root + path.sep)) { response.writeHead(403, securityHeaders); response.end(); return; }
  const fileExists = fs.existsSync(target) && fs.statSync(target).isFile();
  if (!fileExists && path.extname(decodedPath)) { response.writeHead(404, securityHeaders); response.end(); return; }
  const filePath = fileExists ? target : path.join(root, 'index.html');
  const cache = filePath.endsWith('index.html') ? 'no-cache' : 'public, max-age=31536000, immutable';
  response.writeHead(200, { ...securityHeaders, 'Content-Type': mime.get(path.extname(filePath)) || 'application/octet-stream', 'Cache-Control': cache });
  if (request.method === 'HEAD') { response.end(); return; }
  fs.createReadStream(filePath).pipe(response);
});

server.listen(listenPort, listenHost, () => process.stdout.write(`验收网页已启动：http://${listenHost}:${listenPort}\n`));
for (const signal of ['SIGINT', 'SIGTERM']) process.on(signal, () => server.close(() => process.exit(0)));
