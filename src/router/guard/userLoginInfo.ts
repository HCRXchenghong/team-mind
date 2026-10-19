import type { Router, LocationQueryRaw } from 'vue-router';
import NProgress from 'nprogress'; // progress bar

import { useUserStore } from '@/store';
import { useWorkspaceStore } from '@/stores/workspace';
import { isLogin } from '@/utils/auth';

export default function setupUserLoginInfoGuard(router: Router) {
  router.beforeEach(async (to, from, next) => {
    NProgress.start();
    const userStore = useUserStore();
    if (isLogin()) {
      if (!userStore.role) {
        try {
          await userStore.info();
          await useWorkspaceStore().load();
        } catch (error) {
          userStore.logoutCallBack();
          if (to.name === 'login') {
            next();
            return;
          }
          next({
            name: 'login',
            query: {
              redirect: to.fullPath,
            } as LocationQueryRaw,
          });
          return;
        }
      }

      // A native app or a browser opened at `/` is routed to `/login` before
      // this guard runs. A valid remembered token must restore the session and
      // leave the login page instead of showing an empty login form.
      if (to.name === 'login') {
        const requested = String(to.query.redirect || '/teammind/overview');
        const requestedPath = requested.split(/[?#]/, 1)[0];
        const authPaths = new Set([
          '/login',
          '/register',
          '/forgot-password',
          '/reset-password',
          '/verify-email-change',
        ]);
        next(
          requested.startsWith('/') &&
            !requested.startsWith('//') &&
            !authPaths.has(requestedPath)
            ? requested
            : '/teammind/overview'
        );
        return;
      }
      next();
    } else {
      if (['login', 'register', 'forgot', 'reset', 'verify-email-change'].includes(String(to.name))) {
        next();
        return;
      }
      next({
        name: 'login',
        query: {
          redirect: to.fullPath,
        } as LocationQueryRaw,
      });
    }
  });
}
