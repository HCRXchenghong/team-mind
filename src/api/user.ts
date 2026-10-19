import axios from 'axios';
export interface LoginData { email: string; password: string; remember: boolean }
export interface LoginRes {
  accessToken?: string;
  expiresIn: number;
  mfaRequired?: boolean;
  challengeToken?: string;
  remember?: boolean;
}
export function login(data: LoginData) { return axios.post<LoginRes>('/auth/login', data); }
export function logout() { return axios.post('/auth/logout'); }
export function getUserInfo() { return axios.get('/me'); }
export function getMenuList() { return Promise.reject(new Error('服务端菜单未启用')); }
