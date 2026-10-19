import { computed, ref } from 'vue';
import { defineStore } from 'pinia';
import { api, Team } from '@/api/workspace';

export const useWorkspaceStore = defineStore('workspace', () => {
  const teams = ref<Team[]>([]);
  const selectedId = ref(sessionStorage.getItem('teammind.teamId') || '');
  const loaded = ref(false);
  const activeTeam = computed<Team>(() => teams.value.find((item) => item.id === selectedId.value) || teams.value[0] || { id: '', name: '尚未加入团队', description: '', timezone: 'Asia/Shanghai', role: 'member', status: 'active', members: 0 });
  const isAdmin = computed(() => ['team_admin', 'team_owner'].includes(activeTeam.value.role));
  async function load() {
    teams.value = await api<Team[]>('GET', '/me/teams');
    if (!teams.value.some((item) => item.id === selectedId.value)) selectedId.value = teams.value[0]?.id || '';
    loaded.value = true;
  }
  function switchTeam(id: string) {
    if (!teams.value.some((item) => item.id === id)) return false;
    selectedId.value = id;
    sessionStorage.setItem('teammind.teamId', id);
    return true;
  }
  function clear() { teams.value = []; selectedId.value = ''; loaded.value = false; sessionStorage.removeItem('teammind.teamId'); }
  return { data: computed(() => ({ teams: teams.value })), teams, activeTeam, isAdmin, loaded, load, switchTeam, clear };
});
export default useWorkspaceStore;
