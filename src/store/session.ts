import { computed, ref } from 'vue';
import { defineStore } from 'pinia';
import { api, clearToken, getToken } from '@/api/client';
import type { Team, User } from '@/api/types';

const TEAM_KEY = 'teammind.teamId';

export const useSession = defineStore('session', () => {
  const user = ref<User | null>(null);
  const teams = ref<Team[]>([]);
  const activeId = ref<string>(localStorage.getItem(TEAM_KEY) || '');
  const loaded = ref(false);
  const loading = ref(false);

  const activeTeam = computed(() => teams.value.find((team) => team.id === activeId.value) || null);
  const isAdmin = computed(() => ['team_admin', 'team_owner'].includes(activeTeam.value?.role || ''));
  const isOwner = computed(() => activeTeam.value?.role === 'team_owner');
  const isLead = computed(() => Boolean(activeTeam.value?.isLead));
  const isSuperAdmin = computed(() => Boolean(user.value?.superAdmin));
  const isCompany = computed(() => activeTeam.value?.kind === 'company');
  const canManage = computed(() => isAdmin.value || isLead.value || isSuperAdmin.value);
  const teamStatus = computed(() => activeTeam.value?.status || '');

  async function load(): Promise<void> {
    if (loading.value) return;
    loading.value = true;
    try {
      const [me, list] = await Promise.all([
        api<User>('GET', '/me'),
        api<Team[]>('GET', '/me/teams'),
      ]);
      user.value = me;
      teams.value = list;
      loaded.value = true;
      if (!list.some((team) => team.id === activeId.value)) {
        select(list[0]?.id || '');
      }
    } finally {
      loading.value = false;
    }
  }

  function select(id: string): void {
    activeId.value = id;
    if (id) localStorage.setItem(TEAM_KEY, id);
    else localStorage.removeItem(TEAM_KEY);
  }

  function reset(): void {
    user.value = null;
    teams.value = [];
    activeId.value = '';
    loaded.value = false;
    clearToken();
    localStorage.removeItem(TEAM_KEY);
  }

  async function logout(): Promise<void> {
    try {
      if (getToken()) await api('POST', '/auth/logout');
    } catch {
      /* 令牌可能已失效，忽略 */
    } finally {
      reset();
    }
  }

  return {
    user,
    teams,
    activeId,
    activeTeam,
    loaded,
    loading,
    isAdmin,
    isOwner,
    isLead,
    isSuperAdmin,
    isCompany,
    canManage,
    teamStatus,
    load,
    select,
    reset,
    logout,
  };
});
