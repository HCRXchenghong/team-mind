import axios, { type AxiosRequestConfig } from 'axios';

const TOKEN_KEY = 'teammind.accessToken';

export const getToken = (): string =>
  sessionStorage.getItem(TOKEN_KEY) || localStorage.getItem(TOKEN_KEY) || '';

export function setToken(value: string, remember = false): void {
  sessionStorage.removeItem(TOKEN_KEY);
  localStorage.removeItem(TOKEN_KEY);
  (remember ? localStorage : sessionStorage).setItem(TOKEN_KEY, value);
}

export function clearToken(): void {
  sessionStorage.removeItem(TOKEN_KEY);
  localStorage.removeItem(TOKEN_KEY);
}

export const apiBase = (): string =>
  String(import.meta.env.VITE_API_BASE_URL || '/api/v1').replace(/\/$/, '');

export const http = axios.create({
  baseURL: apiBase(),
  timeout: 60000,
});

http.interceptors.request.use((config) => {
  const token = getToken();
  if (token) config.headers.Authorization = `Bearer ${token}`;
  return config;
});

let redirecting = false;
function redirectToLogin(): void {
  if (redirecting || location.pathname.startsWith('/login')) return;
  redirecting = true;
  const redirect = encodeURIComponent(location.pathname + location.search);
  location.assign(`/login?redirect=${redirect}`);
}

http.interceptors.response.use(
  (response) => {
    const body = response.data;
    if (body && typeof body === 'object' && 'code' in body && body.code !== 'OK') {
      return Promise.reject(new ApiError(body.message || '服务器响应无效', body.code, response.status));
    }
    return response;
  },
  (error) => {
    const status = error.response?.status as number | undefined;
    const data = error.response?.data as { code?: string; message?: string } | undefined;
    if (status === 401 || data?.code === 'SESSION_EXPIRED' || data?.code === 'UNAUTHENTICATED') {
      clearToken();
      redirectToLogin();
    }
    const message =
      data?.message ||
      (error.code === 'ECONNABORTED'
        ? '请求超时，请稍后重试'
        : error.message === 'Network Error'
          ? '网络不可用，请检查连接'
          : '请求失败，请稍后重试');
    return Promise.reject(new ApiError(message, data?.code || 'REQUEST_FAILED', status || 0));
  },
);

export class ApiError extends Error {
  code: string;
  status: number;
  constructor(message: string, code = 'REQUEST_FAILED', status = 0) {
    super(message);
    this.name = 'ApiError';
    this.code = code;
    this.status = status;
  }
}

type Method = 'GET' | 'POST' | 'PATCH' | 'PUT' | 'DELETE';

export async function api<T = unknown>(
  method: Method,
  path: string,
  data?: unknown,
  params?: Record<string, unknown>,
  config?: AxiosRequestConfig,
): Promise<T> {
  const response = await http.request<{ code: string; data: T }>({
    method,
    url: path,
    data,
    params,
    ...config,
  });
  return response.data.data;
}

/** 去掉空字符串 / null / undefined 的查询参数，避免后端把空值当筛选条件。 */
export function cleanParams(params: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== '') out[key] = value;
  });
  return out;
}

/** SSE 流式请求：不走 axios，保留原生 ReadableStream 以便逐字渲染。 */
export async function streamFetch(
  path: string,
  body: unknown,
  signal?: AbortSignal,
): Promise<Response> {
  const token = getToken();
  const response = await fetch(`${apiBase()}${path}`, {
    method: 'POST',
    signal,
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: JSON.stringify(body),
  });
  if (!response.ok) {
    let message = `请求失败（HTTP ${response.status}）`;
    try {
      const data = (await response.json()) as { message?: string };
      if (data.message) message = data.message;
    } catch {
      /* ignore */
    }
    throw new ApiError(message, 'STREAM_FAILED', response.status);
  }
  return response;
}
