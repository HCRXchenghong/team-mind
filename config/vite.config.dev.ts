import { mergeConfig } from 'vite';
import baseConfig from './vite.config.base';

export default mergeConfig({
  mode: 'development',
  server: {
    open: false,
    host: '0.0.0.0',
    fs: { strict: true },
    proxy: {
      '/api/v1': {
        target: process.env.TEAMMIND_API_PROXY || 'http://127.0.0.1:8188',
        changeOrigin: true,
      },
    },
  },
}, baseConfig);
