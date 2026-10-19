const TOKEN_KEY = 'teammind.accessToken';
const getToken = () => sessionStorage.getItem(TOKEN_KEY) || localStorage.getItem(TOKEN_KEY);
const isLogin = () => !!getToken();
const setToken = (token: string, remember = false) => {
  localStorage.removeItem(TOKEN_KEY);
  sessionStorage.removeItem(TOKEN_KEY);
  (remember ? localStorage : sessionStorage).setItem(TOKEN_KEY, token);
};
const isRemembered = () => !!localStorage.getItem(TOKEN_KEY);
const clearToken = () => {
  localStorage.removeItem(TOKEN_KEY);
  sessionStorage.removeItem(TOKEN_KEY);
};
export { isLogin, getToken, setToken, clearToken, isRemembered };
