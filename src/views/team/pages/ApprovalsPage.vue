<template
  ><section class="tm-page"
    ><header class="tm-header"
      ><div><h1>审批中心</h1><p>批准后才写入正式项目或任务计划</p></div
      ><a-button @click="refresh">刷新</a-button></header
    ><div v-if="error" class="tm-error">{{ error }}</div
    ><section class="tm-approval-group">
      <h2>项目变更提案</h2>
      <p>AI 和团队成员提交的变更只会在管理员批准后更新正式项目。</p>
      <div v-if="!projectItems.length" class="tm-empty">暂无项目变更提案</div>
      <article v-for="p in projectItems" :key="p.id" class="tm-panel">
        <h3>{{ p.projectName }}</h3>
        <p>状态：{{ proposalStatusLabel(p.status) }} · 项目版本：{{ p.projectVersion }}</p>
        <p>变更原因：{{ p.reason }}</p>
        <p>影响说明：{{ p.impactSummary }}</p>
        <p>建议名称：{{ p.proposed.name }}</p>
        <p>建议状态：{{ projectStatusLabel(p.proposed.status) }}</p>
        <p>建议周期：{{ p.proposed.startDate || '未设置' }} → {{ p.proposed.dueDate || '未设置' }}</p>
        <div v-if="p.status === 'pending' && store.isAdmin" class="tm-actions">
          <a-button type="primary" @click="openProject(p, 'approved')">批准</a-button>
          <a-button @click="openProject(p, 'edited_approved')">修改后批准</a-button>
          <a-button @click="openProject(p, 'needs_info')">要求补充信息</a-button>
          <a-button status="danger" @click="openProject(p, 'rejected')">拒绝</a-button>
        </div>
        <div v-if="p.status === 'needs_info' && p.proposedBy === user.id" class="tm-actions">
          <a-button type="primary" @click="openProject(p, 'resubmit')">补充后重新提交</a-button>
        </div>
      </article>
    </section>
    <section class="tm-approval-group">
      <h2>成员身份变更</h2>
      <p>智能体或团队成员可提出角色与状态变更；所有者变更仍须走团队所有权转移流程。批准前不会更改成员权限。</p>
      <div v-if="!memberItems.length" class="tm-empty">暂无成员身份变更提案</div>
      <article v-for="p in memberItems" :key="p.id" class="tm-panel">
        <h3>{{ p.memberName }}</h3>
        <p>提交人：{{ p.proposedByName }} · 状态：{{ proposalStatusLabel(p.status) }}</p>
        <p>当前：{{ memberRoleLabel(p.before.role) }} / {{ memberStatusLabel(p.before.status) }}</p>
        <p>建议：{{ memberRoleLabel(p.proposed.role) }} / {{ memberStatusLabel(p.proposed.status) }}</p>
        <p>变更原因：{{ p.reason }}</p>
        <p>影响说明：{{ p.impactSummary }}</p>
        <p v-if="memberNeedsOwner(p) && store.activeTeam.role !== 'team_owner'" class="tm-meta">此提案涉及管理员权限或管理员成员，需团队所有者审批。</p>
        <div v-if="canDecideMember(p)" class="tm-actions">
          <a-button v-if="canApproveMember(p)" type="primary" @click="openMember(p, 'approved')">批准</a-button>
          <a-button v-if="canApproveMember(p)" @click="openMember(p, 'edited_approved')">修改后批准</a-button>
          <a-button @click="openMember(p, 'needs_info')">要求补充信息</a-button>
          <a-button status="danger" @click="openMember(p, 'rejected')">拒绝</a-button>
        </div>
        <div v-if="p.status === 'needs_info' && p.proposedBy === user.id" class="tm-actions">
          <a-button type="primary" @click="openMember(p, 'resubmit')">补充后重新提交</a-button>
        </div>
      </article>
    </section>
    <div v-if="!items.length" class="tm-empty">暂无任务计划提案</div
    ><article v-for="p in items" :key="p.id" class="tm-panel"
      ><h2>{{ p.taskTitle }}</h2
      ><p>状态：{{ p.status }} · 任务版本：{{ p.taskVersion }}</p
      ><p>变更原因：{{ p.reason }}</p
      ><p>影响说明：{{ p.impactSummary || '未填写' }}</p
      ><p>建议标题：{{ p.proposed.title || p.taskTitle }}</p
      ><p>建议描述：{{ p.proposed.description || '无' }}</p
      ><p>状态：{{ statusLabel(p.proposed.status) }} · 进度：{{ p.proposed.progress ?? 0 }}%</p
      ><p>优先级：{{ priorityLabel(p.proposed.priority) }} · 负责人：{{ members.find((m) => m.id === p.proposed.assigneeId)?.name || (p.proposed.assigneeId ? '成员不可用' : '未分配') }} · {{ p.proposed.milestone ? '里程碑' : '普通任务' }}</p
      ><p
        >建议排期：{{ p.proposed.startDate || '未设置' }} →
        {{ p.proposed.dueDate || '未设置' }}</p
      ><div v-if="p.status === 'pending' && store.isAdmin" class="tm-actions"
        ><a-button type="primary" @click="open(p, 'approved')">批准</a-button
        ><a-button @click="open(p, 'edited_approved')">修改后批准</a-button
        ><a-button @click="open(p, 'needs_info')">要求补充信息</a-button
        ><a-button status="danger" @click="open(p, 'rejected')"
          >拒绝</a-button
        ></div
      ><div
        v-if="p.status === 'needs_info' && p.proposedBy === user.id"
        class="tm-actions"
        ><a-button type="primary" @click="open(p, 'resubmit')"
          >补充后重新提交</a-button
        ></div
      ></article
    ><a-modal
      v-model:visible="visible"
      title="处理提案"
      :ok-loading="saving"
      @ok="decide"
      ><div class="tm-form"
        ><p>操作：{{ decision }}</p
        ><template
          v-if="decision === 'edited_approved' || decision === 'resubmit'"
          ><label>标题<input v-model.trim="change.title" maxlength="240" /></label
          ><label>描述<textarea v-model="change.description" maxlength="20000"></textarea></label
          ><label>状态<select v-model="change.status"><option value="todo">待处理</option><option value="doing">进行中</option><option value="blocked">受阻</option><option value="review">待验收</option><option value="done">已完成</option><option value="cancelled">已取消</option></select></label
          ><label>进度（0–100）<input v-model.number="change.progress" type="number" min="0" max="100" /></label
          ><label>负责人<select v-model="change.assigneeId"><option value="">未分配</option><option v-for="m in activeMembers" :key="m.id" :value="m.id">{{ m.name }}</option></select></label
          ><label>优先级<select v-model="change.priority"><option value="low">低</option><option value="medium">中</option><option value="high">高</option><option value="urgent">紧急</option></select></label
          ><label>开始日期<input v-model="change.startDate" type="date" /></label
          ><label>截止日期<input v-model="change.dueDate" type="date" /></label
          ><label><input v-model="change.milestone" type="checkbox" /> 里程碑</label></template
        ><label
          >说明<textarea v-model="reason" maxlength="5000"></textarea></label
        ><p v-if="formError" class="tm-error">{{ formError }}</p></div
      ></a-modal
    ><a-modal v-model:visible="projectVisible" title="处理项目变更提案" :ok-loading="projectSaving" @ok="decideProject">
      <div class="tm-form">
        <p>操作：{{ decision }}</p>
        <template v-if="decision === 'edited_approved' || decision === 'resubmit'">
          <label>项目名称<input v-model.trim="projectChange.name" maxlength="200" /></label>
          <label>项目描述<textarea v-model="projectChange.description" maxlength="10000"></textarea></label>
          <label>项目状态<select v-model="projectChange.status"><option value="planning">规划中</option><option value="active">进行中</option><option value="paused">暂停</option><option value="completed">已完成</option><option value="archived">已归档</option></select></label>
          <label>开始日期<input v-model="projectChange.startDate" type="date" /></label>
          <label>截止日期<input v-model="projectChange.dueDate" type="date" /></label>
        </template>
        <label>审批说明<textarea v-model="projectReason" maxlength="5000"></textarea></label>
        <p v-if="projectFormError" class="tm-error">{{ projectFormError }}</p>
      </div>
    </a-modal
    ><a-modal v-model:visible="memberVisible" title="处理成员身份提案" :ok-loading="memberSaving" @ok="decideMember">
      <div class="tm-form">
        <p>成员：{{ memberSelected?.memberName }} · 操作：{{ decision }}</p>
        <template v-if="decision === 'edited_approved' || decision === 'resubmit'">
          <label>角色<select v-model="memberChange.role"><option value="member">普通成员</option><option v-if="store.activeTeam.role === 'team_owner' || memberChange.role === 'team_admin'" value="team_admin">团队管理员</option></select></label>
          <label>状态<select v-model="memberChange.status"><option value="active">有效</option><option value="suspended">已停用</option></select></label>
        </template>
        <label>审批说明<textarea v-model="memberReason" maxlength="5000"></textarea></label>
        <p v-if="memberFormError" class="tm-error">{{ memberFormError }}</p>
      </div>
    </a-modal
    ></section
  ></template
>
<script setup lang="ts">
  import { computed, ref, watch } from 'vue';
  import { Message } from '@arco-design/web-vue';
  import { api, Member, MemberProposal, Proposal, ProjectProposal } from '@/api/workspace';
  import { useWorkspaceStore } from '@/stores/workspace';
  import { useTeamRequest } from '@/utils/teamRequest';
  import { useUserStore } from '@/store';
  const store = useWorkspaceStore();
  const teamRequest = useTeamRequest(() => store.activeTeam.id);
  let displayedTeamId = '';
  const user = useUserStore();
  const items = ref<Proposal[]>([]);
  const projectItems = ref<ProjectProposal[]>([]);
  const memberItems = ref<MemberProposal[]>([]);
  const members = ref<Member[]>([]);
  const activeMembers = computed(() => members.value.filter((m) => m.status === 'active'));
  const error = ref('');
  const formError = ref('');
  const visible = ref(false);
  const saving = ref(false);
  const selected = ref<Proposal | null>(null);
  const decision = ref('approved');
  const reason = ref('');
  const change = ref({ title: '', description: '', priority: 'medium', status: 'todo', progress: 0, assigneeId: '', startDate: '', dueDate: '', milestone: false });
  const projectVisible = ref(false);
  const projectSaving = ref(false);
  const projectSelected = ref<ProjectProposal | null>(null);
  const projectReason = ref('');
  const projectFormError = ref('');
  const projectChange = ref({ name: '', description: '', status: 'planning', startDate: '', dueDate: '' });
  const memberVisible = ref(false);
  const memberSaving = ref(false);
  const memberSelected = ref<MemberProposal | null>(null);
  const memberReason = ref('');
  const memberFormError = ref('');
  const memberChange = ref({ role: 'member', status: 'active' });
  function priorityLabel(value: string) { return ({ low: '低', medium: '中', high: '高', urgent: '紧急' } as Record<string, string>)[value] || value || '中'; }
  function statusLabel(value?: string) { return ({ todo: '待处理', doing: '进行中', blocked: '受阻', review: '待验收', done: '已完成', cancelled: '已取消' } as Record<string, string>)[value || 'todo'] || value || '待处理'; }
  function projectStatusLabel(value?: string) { return ({ planning: '规划中', active: '进行中', paused: '暂停', completed: '已完成', archived: '已归档' } as Record<string, string>)[value || 'planning'] || value || '规划中'; }
  function proposalStatusLabel(value: string) { return ({ pending: '待审批', approved: '已批准', rejected: '已拒绝', needs_info: '需补充', stale: '版本过期', withdrawn: '已撤回' } as Record<string, string>)[value] || value; }
  function memberRoleLabel(value: string) { return value === 'team_owner' ? '团队所有者' : value === 'team_admin' ? '团队管理员' : '普通成员'; }
  function memberStatusLabel(value: string) { return value === 'active' ? '有效' : value === 'suspended' ? '已停用' : value; }
  function memberNeedsOwner(p: MemberProposal) { return p.before.role !== 'member' || p.proposed.role === 'team_admin'; }
  function canDecideMember(p: MemberProposal) { return p.status === 'pending' && store.isAdmin; }
  function canApproveMember(p: MemberProposal) { return canDecideMember(p) && (!memberNeedsOwner(p) || store.activeTeam.role === 'team_owner'); }
  async function refresh() {
    const request = teamRequest.begin();
    const id = request.teamId;
    if (!id) {
      items.value = [];
      memberItems.value = [];
      members.value = [];
      return;
    }
    try {
      const [loadedItems, loadedMembers, loadedProjectItems, loadedMemberItems] = await Promise.all([
        api<Proposal[]>('GET', `/teams/${id}/plan-proposals`),
        api<Member[]>('GET', `/teams/${id}/members`),
        api<ProjectProposal[]>('GET', `/teams/${id}/project-proposals`),
        api<MemberProposal[]>('GET', `/teams/${id}/member-proposals`),
      ]);
      if (!teamRequest.isCurrent(request)) return;
      items.value = loadedItems;
      members.value = loadedMembers;
      projectItems.value = loadedProjectItems;
      memberItems.value = loadedMemberItems;
      error.value = '';
    } catch (e) {
      if (!teamRequest.isCurrent(request)) return;
      error.value = (e as Error).message;
    }
  }
  function open(p: Proposal, d: string) {
    selected.value = p;
    decision.value = d;
    reason.value = '';
    change.value = {
      title: p.proposed.title || p.taskTitle,
      description: p.proposed.description || '',
      priority: p.proposed.priority || 'medium',
      status: p.proposed.status || 'todo',
      progress: p.proposed.progress ?? 0,
      assigneeId: p.proposed.assigneeId || '',
      startDate: p.proposed.startDate || '',
      dueDate: p.proposed.dueDate || '',
      milestone: p.proposed.milestone || false,
    };
    formError.value = '';
    visible.value = true;
  }
  async function decide() {
    if (!selected.value || saving.value) return;
    saving.value = true;
    try {
      if (decision.value === 'resubmit')
        await api(
          'POST',
          `/teams/${store.activeTeam.id}/plan-proposals/${selected.value.id}/resubmit`,
          {
            reason: reason.value,
            impactSummary: selected.value.impactSummary,
            ...change.value,
          }
        );
      else
        await api(
          'POST',
          `/teams/${store.activeTeam.id}/plan-proposals/${selected.value.id}/decision`,
          {
            decision: decision.value,
            reason: reason.value,
            ...change.value,
          }
        );
      visible.value = false;
      Message.success('审批已完成');
      await refresh();
    } catch (e) {
      formError.value = (e as Error).message;
      visible.value = true;
    } finally {
      saving.value = false;
    }
  }
  function openProject(p: ProjectProposal, nextDecision: string) {
    projectSelected.value = p;
    decision.value = nextDecision;
    projectReason.value = '';
    projectChange.value = { name: p.proposed.name, description: p.proposed.description, status: p.proposed.status, startDate: p.proposed.startDate || '', dueDate: p.proposed.dueDate || '' };
    projectFormError.value = '';
    projectVisible.value = true;
  }
  async function decideProject() {
    const proposal = projectSelected.value;
    if (!proposal || projectSaving.value) return;
    if ((decision.value === 'needs_info' || decision.value === 'rejected') && projectReason.value.trim().length < 5) {
      projectFormError.value = '请填写至少 5 个字的说明';
      return;
    }
    if ((decision.value === 'edited_approved' || decision.value === 'resubmit') && !projectChange.value.name.trim()) {
      projectFormError.value = '项目名称不能为空';
      return;
    }
    projectSaving.value = true;
    try {
      const path = `/teams/${store.activeTeam.id}/project-proposals/${proposal.id}/${decision.value === 'resubmit' ? 'resubmit' : 'decision'}`;
      if (decision.value === 'resubmit') {
        await api('POST', path, { projectId: proposal.projectId, reason: projectReason.value, impactSummary: proposal.impactSummary, ...projectChange.value });
      } else {
        await api('POST', path, { decision: decision.value, reason: projectReason.value, ...projectChange.value });
      }
      projectVisible.value = false;
      Message.success('项目提案已处理');
      await refresh();
    } catch (e) {
      projectFormError.value = (e as Error).message;
    } finally {
      projectSaving.value = false;
    }
  }
  function openMember(p: MemberProposal, nextDecision: string) {
    memberSelected.value = p;
    decision.value = nextDecision;
    memberReason.value = '';
    memberChange.value = { role: p.proposed.role, status: p.proposed.status };
    memberFormError.value = '';
    memberVisible.value = true;
  }
  async function decideMember() {
    const proposal = memberSelected.value;
    if (!proposal || memberSaving.value) return;
    if ((decision.value === 'needs_info' || decision.value === 'rejected') && memberReason.value.trim().length < 5) {
      memberFormError.value = '请填写至少 5 个字的说明';
      return;
    }
    memberSaving.value = true;
    try {
      const path = `/teams/${store.activeTeam.id}/member-proposals/${proposal.id}/${decision.value === 'resubmit' ? 'resubmit' : 'decision'}`;
      if (decision.value === 'resubmit') {
        await api('POST', path, { ...memberChange.value, reason: memberReason.value, impactSummary: proposal.impactSummary });
      } else {
        await api('POST', path, { decision: decision.value, reason: memberReason.value, ...memberChange.value });
      }
      memberVisible.value = false;
      Message.success('成员提案已处理');
      await refresh();
    } catch (e) {
      memberFormError.value = (e as Error).message;
    } finally {
      memberSaving.value = false;
    }
  }
  watch(() => store.activeTeam.id, (id) => {
    if (id !== displayedTeamId) {
      displayedTeamId = id;
      items.value = [];
      projectItems.value = [];
      memberItems.value = [];
      members.value = [];
      error.value = '';
      visible.value = false;
      projectVisible.value = false;
      memberVisible.value = false;
      selected.value = null;
    }
    refresh();
  }, { immediate: true });
</script>
