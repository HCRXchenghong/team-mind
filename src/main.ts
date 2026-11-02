import { createApp } from 'vue';
import { createPinia } from 'pinia';
import 'ant-design-vue/dist/reset.css';
import 'nprogress/nprogress.css';
import './styles/tokens.css';
import './styles/global.css';
import dayjs from 'dayjs';
import 'dayjs/locale/zh-cn';
import relativeTime from 'dayjs/plugin/relativeTime';
import App from './App.vue';
import router from './router';

dayjs.extend(relativeTime);
dayjs.locale('zh-cn');

const app = createApp(App);
app.use(createPinia());
app.use(router);
app.mount('#app');
