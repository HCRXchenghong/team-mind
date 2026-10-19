import axios from 'axios';
import { clearToken, getToken } from '@/utils/auth';

axios.defaults.baseURL = import.meta.env.VITE_API_BASE_URL || '/api/v1';
axios.defaults.timeout = 20000;
axios.interceptors.request.use((config) => {
  const token = getToken();
  if (token) {
    config.headers = config.headers || {};
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});
axios.interceptors.response.use(
  (response) => {
    if (response.data?.code !== 'OK') return Promise.reject(new Error(response.data?.message || '服务器响应无效'));
    return response.data;
  },
  (error) => {
    const code = error.response?.data?.code;
    if (code === 'SESSION_EXPIRED' || code === 'UNAUTHENTICATED') {
      clearToken();
      if (window.location.pathname !== '/login') window.location.assign(`/login?redirect=${encodeURIComponent(window.location.pathname + window.location.search)}`);
    }
    const message = error.response?.data?.message || (error.code === 'ECONNABORTED' ? '请求超时，请重试' : error.response ? '请求失败，请重试' : '无法连接服务器，请检查网络或服务状态');
    return Promise.reject(new Error(message));
  }
);
export interface HttpResponse<T = unknown> { status: number; msg: string; code: number; data: T }
