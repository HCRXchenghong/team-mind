<template>
  <section class="tm-page">
    <header class="tm-header">
      <div>
        <h1>AI 助手</h1>
        <p v-if="status.simulated">本地模拟模式：回复不来自大模型</p>
        <p v-else-if="status.configured"
          >模型已配置；实际连接状态以发送结果为准</p
        >
        <p v-else>模型服务未配置；请由部署管理员设置 AI Provider 凭据</p>
      </div>
      <a-button @click="newChat">新对话</a-button>
    </header>
    <div v-if="error" class="tm-error" role="alert">{{ error }}</div>
    <section class="tm-panel tm-conversations">
      <h2>历史对话</h2>
      <div v-if="!visibleConversations.length" class="tm-empty">暂无对话</div>
      <div v-else class="tm-conversation-list">
        <button
          v-for="conversation in visibleConversations"
          :key="conversation.id"
          type="button"
          class="tm-conversation"
          :disabled="sending"
          :class="{ active: conversation.id === conversationId }"
          @click="openConversation(conversation)"
        >
          <span>{{ conversation.title || '未命名对话' }}</span>
          <small
            >{{ conversation.teamId ? '当前团队' : '私人' }} ·
            {{ formatDateTime(conversation.updatedAt, user.timezone) }}</small
          >
        </button>
      </div>
    </section>
    <div class="tm-panel tm-chat" aria-live="polite">
      <div v-if="conversationId" class="tm-chat-actions">
        <a-button
          v-if="conversationTeamId && conversationTeamId === store.activeTeam.id"
          :loading="toolRunning"
          :disabled="sending || toolRunning || !confirmTeam"
          @click="runTeamTaskLookup"
          >查询当前团队任务（只读工具）</a-button
        >
        <p v-if="conversationTeamId && !confirmTeam" class="tm-meta"
          >查看团队任务前，请重新确认本次团队上下文。</p
        >
        <section v-if="toolResult" class="tm-agent-result" role="status">
          <strong>只读工具返回 {{ toolResult.count }} 条任务</strong>
          <p v-if="!toolResult.items.length" class="tm-meta">当前团队暂无任务。</p>
          <article v-for="task in toolResult.items" :key="task.id">
            <b>{{ task.title }}</b><span>{{ task.status }} · {{ task.priority }} · {{ task.progress }}%<template v-if="task.dueDate"> · 截止 {{ task.dueDate }}</template></span>
          </article>
        </section>
        <a-button
          v-if="status.configured && !status.simulated"
          :loading="extracting"
          :disabled="sending || extracting"
          @click="extractMemoryCandidates"
          >从当前对话提取候选</a-button
        >
        <a-popconfirm
          v-if="conversationId"
          content="永久删除此对话及消息？来源于此对话的未确认记忆候选也会删除。"
          ok-text="永久删除"
          cancel-text="取消"
          @ok="deleteConversation"
        >
          <a-button status="danger" :loading="conversationDeleting" :disabled="sending || agentRunning || toolRunning || conversationDeleting">删除当前对话</a-button>
        </a-popconfirm>
      <p v-if="status.configured && !status.simulated" class="tm-meta">
        智能体可自主选择团队内只读查询、按需提交待审批的任务、项目或成员变更，或在你单独授权后使用断网临时沙箱运行 Python/Shell。成员晋升管理员须所有者审批，正式数据不会绕过审批直接修改。
        {{ status.sandboxAvailable ? 'Python 沙箱已启用。' : 'Python 沙箱未启用。' }}
        {{ status.shellAvailable ? 'Shell 沙箱已启用。' : 'Shell 沙箱未启用。' }}
      </p>
        <p v-else-if="status.simulated" class="tm-meta">
          模拟模式不运行自动提取；可手动选择消息保存为待确认候选。
        </p>
      </div>
      <div v-if="!messages.length" class="tm-empty"
        >开始一个新对话。团队上下文需要显式确认。</div
      >
      <div
        v-for="message in messages"
        :key="message.id"
        :data-message-id="message.id"
        class="tm-chat-message"
        :class="message.role"
      >
        <b>{{
          message.role === 'user'
            ? '我'
            : message.provider === 'mock'
            ? '模拟助手'
            : 'AI 助手'
        }}</b>
        <p>{{ message.content }}</p>
        <a-button
          v-if="message.role === 'user' || message.role === 'assistant'"
          size="mini"
          @click="openMemoryCandidate(message)"
          >保存为记忆候选</a-button
        >
      </div>
      <div v-if="sending" class="tm-chat-message assistant" aria-live="polite">
        <b>{{ status.simulated ? '模拟助手' : 'AI 助手' }}</b>
        <p>{{ streamingText || '正在生成…' }}</p>
      </div>
      <section v-if="agentResult" class="tm-agent-result" role="status">
        <strong>{{ agentRunLabel(agentResult) }}</strong>
        <p>{{ agentResult.reply }}</p>
        <ol v-if="agentResult.steps && agentResult.steps.length > 1" class="tm-agent-steps">
          <li v-for="(step, index) in agentResult.steps" :key="`${step.tool}-${index}`">
            <b>{{ agentToolLabel(step.tool) }}</b>
            <span v-if="step.result?.proposalId">已创建待审批提案 {{ step.result.proposalId }}。</span>
            <span v-else-if="typeof step.result?.count === 'number'">查询到 {{ step.result.count }} 条当前团队任务。</span>
            <span v-else>已完成。</span>
          </li>
        </ol>
        <p v-if="agentResult.result?.proposalId">
          已创建待审批提案 {{ agentResult.result.proposalId }}。审批通过前正式任务计划不会改变。
        </p>
        <div v-if="agentResult.result?.memoryId" class="tm-agent-memory-candidate">
          <p>已创建待确认团队知识候选。确认前不会进入 AI 知识上下文。</p>
          <a-button size="small" @click="router.push({ path: '/teammind/memories', query: { tab: 'team' } })">打开团队知识并审核</a-button>
        </div>
        <p v-else-if="typeof agentResult.result?.count === 'number'">
          工具查询到 {{ agentResult.result.count }} 条当前团队任务。
        </p>
        <pre v-if="agentResult.result?.stdout">{{ agentResult.result.stdout }}</pre>
        <details v-if="agentResult.result?.files && Object.keys(agentResult.result.files).length">
          <summary>查看沙箱输出文件</summary>
          <article v-for="(content, name) in agentResult.result.files" :key="name">
            <div class="tm-agent-file-heading">
              <b>{{ name }}</b>
              <div class="tm-agent-file-actions">
                <a-button size="small" @click="downloadAgentFile(name, content)">下载</a-button>
                <a-button v-if="status.workspaceAvailable" size="small" :loading="workspaceSaving === name" :disabled="!!workspaceSaving" @click="saveWorkspaceFile(name, content)">保存到私人工作区</a-button>
              </div>
            </div>
            <pre>{{ content }}</pre>
          </article>
        </details>
        <p v-if="agentResult.result?.persisted" class="tm-meta">生成文件已加密保存到你的私人工作区。</p>
      </section>
    </div>
    <form class="tm-chat-compose" @submit.prevent="send">
      <label v-if="store.activeTeam.id">
        <input v-model="confirmTeam" type="checkbox" />
        确认使用当前团队（{{ store.activeTeam.name }}）上下文
      </label>
      <p v-else class="tm-meta">当前没有团队，可使用私人对话。</p>
      <label
        v-if="store.isAdmin && store.activeTeam.id && (!conversationTeamId || conversationTeamId === store.activeTeam.id)"
        class="tm-agent-consent"
      >
        <input
          v-model="allowTeamKnowledgeCandidate"
          type="checkbox"
          :disabled="!status.agentTools || !confirmTeam || agentRunning || sending"
        />
        本次允许智能体创建一条待确认团队知识候选（仅管理员/所有者；不会自动进入 AI 上下文）
      </label>
      <textarea
        v-model="prompt"
        placeholder="输入消息"
        maxlength="12000"
      ></textarea>
      <fieldset class="tm-sandbox-options">
        <legend>隔离代码与命令执行</legend>
        <label>
          <input v-model="allowCodeExecution" type="checkbox" :disabled="!status.agentTools || !status.sandboxAvailable || agentRunning || sending" />
          本次允许模型运行 Python（断网临时沙箱）
        </label>
        <label>
          <input v-model="allowShellExecution" type="checkbox" :disabled="!status.agentTools || !status.shellAvailable || agentRunning || sending" />
          本次允许模型运行 Shell 命令（断网临时沙箱）
        </label>
        <label>
          添加文本文件（可选）
          <input ref="sandboxFileInput" type="file" accept=".txt,.csv,.json,.md,text/plain,text/csv,application/json,text/markdown" multiple :disabled="(!allowCodeExecution && !allowShellExecution) || agentRunning || sending" @change="addSandboxFiles" />
        </label>
        <small>每种执行权限都按本次单独授权。脚本只可访问所选输入文件和临时工作区，不能访问服务器文件、密钥或网络。单文件最多 64 KiB、共 12 个且总计不超过 256 KiB；临时文件运行后删除。</small>
        <small v-if="!status.sandboxAvailable && !status.shellAvailable">当前服务没有启用隔离沙箱（需非 root 运行并安装 bubblewrap、prlimit 及对应运行时）。</small>
        <small v-if="status.simulated">模拟模式不会调用工具或执行代码。</small>
        <section class="tm-workspace-files" aria-labelledby="tm-workspace-title">
          <div class="tm-agent-file-heading">
            <strong id="tm-workspace-title">我的私人持久工作区</strong>
            <a-button size="small" :loading="workspaceLoading" :disabled="workspaceLoading" @click="loadWorkspaceFiles">刷新</a-button>
          </div>
          <p class="tm-meta">文件只属于当前账号，服务端加密后保存在 MySQL。选中的文件只有在你单独授权本次 Python 或 Shell 沙箱后才会提供给工具。</p>
          <p v-if="!status.workspaceAvailable" class="tm-meta">服务端尚未配置工作区加密密钥，持久文件功能不可用。</p>
          <template v-else>
            <label class="tm-workspace-import">
              添加 UTF-8 文本文件到私人工作区
              <input
                type="file"
                accept=".txt,.csv,.json,.md,text/plain,text/csv,application/json,text/markdown"
                multiple
                :disabled="!!workspaceSaving || workspaceLoading"
                @change="importWorkspaceFiles"
              />
            </label>
            <small>支持 TXT、CSV、JSON、Markdown；每个文件最多 64 KiB，最多 12 个文件，总量 256 KiB。上传后加密保存到 MySQL。</small>
            <p v-if="!workspaceFiles.length && !workspaceLoading" class="tm-meta">私人工作区为空。你可以将临时输入或沙箱结果单独保存。</p>
            <div v-for="file in workspaceFiles" :key="file.id" class="tm-agent-upload">
              <label class="tm-workspace-file-select">
                <input v-model="workspaceSelectedIDs" type="checkbox" :value="file.id" :disabled="(!allowCodeExecution && !allowShellExecution) || agentRunning || sending" />
                <span>{{ file.name }} · {{ file.size }} 字节</span>
              </label>
              <a-button size="small" :loading="workspaceSaving === `read:${file.id}`" :disabled="!!workspaceSaving || !status.workspaceAvailable" @click="editWorkspaceFile(file.id)">查看/编辑</a-button>
              <a-popconfirm content="永久删除这份私人工作区文件？" ok-text="删除" cancel-text="取消" @ok="deleteWorkspaceFile(file.id)">
                <a-button size="small" status="danger" :disabled="!!workspaceSaving || agentRunning">删除</a-button>
              </a-popconfirm>
            </div>
            <label class="tm-workspace-consent">
              <input v-model="persistWorkspaceOutput" type="checkbox" :disabled="(!allowCodeExecution && !allowShellExecution) || agentRunning || sending || !status.agentTools" />
              本次另行授权：将沙箱生成文件保存到私人工作区
            </label>
          </template>
          <div v-for="file in sandboxFiles" :key="`save-${file.name}`" class="tm-agent-upload">
            <span>临时输入：{{ file.name }}</span>
            <a-button size="small" :loading="workspaceSaving === file.name" :disabled="!!workspaceSaving || !status.workspaceAvailable" @click="saveWorkspaceFile(file.name, file.content)">保存到工作区</a-button>
          </div>
          <p v-if="workspaceError" class="tm-error" role="alert">{{ workspaceError }}</p>
        </section>
        <div v-for="file in sandboxFiles" :key="file.name" class="tm-agent-upload">
          <span>{{ file.name }} · {{ file.size }} 字节</span>
          <a-button size="small" :disabled="agentRunning" @click="removeSandboxFile(file.name)">移除</a-button>
        </div>
        <p v-if="sandboxFileError" class="tm-error" role="alert">{{ sandboxFileError }}</p>
      </fieldset>
      <a-button
        type="primary"
        html-type="submit"
        :loading="sending"
        :disabled="sending || !prompt.trim() || !status.configured"
        >发送</a-button
      >
      <a-button
        v-if="status.configured && !status.simulated"
        :loading="agentRunning"
        :disabled="agentRunning || sending || !prompt.trim() || (!!conversationTeamId && !confirmTeam) || (sandboxFiles.length > 0 && !allowCodeExecution && !allowShellExecution)"
        @click="runAgent"
        >运行智能体（最多 3 步顺序工作流）</a-button
      >
    </form>
    <a-modal
      v-model:visible="candidateModal"
      title="保存记忆候选"
      :ok-loading="candidateSaving"
      @ok="saveMemoryCandidate"
    >
      <div class="tm-form">
        <p class="tm-meta">内容将保存为仅你可见的待确认个人记忆，并保留此对话来源。</p>
        <label>类别
          <select v-model="candidateCategory">
            <option value="profile">个人资料</option>
            <option value="experience">经验</option>
            <option value="current_work">当前工作</option>
            <option value="operational_knowledge">工作知识</option>
          </select>
        </label>
        <label>候选内容<textarea v-model="candidateContent" maxlength="10000"></textarea></label>
        <p v-if="candidateError" class="tm-error" role="alert">{{ candidateError }}</p>
      </div>
    </a-modal>
    <a-modal
      v-model:visible="workspaceEditorOpen"
      title="编辑私人工作区文件"
      :ok-loading="workspaceEditorSaving"
      :on-before-ok="saveWorkspaceFileEdit"
      @cancel="workspaceEditorOpen = false"
    >
      <div class="tm-form">
        <label>文件名<input v-model.trim="workspaceDraft.name" maxlength="160" required /></label>
        <label>UTF-8 文本内容<textarea v-model="workspaceDraft.content" rows="12" spellcheck="false"></textarea></label>
        <small>仅支持 .txt、.csv、.json、.md；单文件不得超过 64 KiB，个人工作区总量不得超过 256 KiB。</small>
        <p v-if="workspaceEditorError" class="tm-error" role="alert">{{ workspaceEditorError }}</p>
      </div>
    </a-modal>
  </section>
</template>
<script setup lang="ts">
  import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue';
  import { Message } from '@arco-design/web-vue';
  import { api } from '@/api/workspace';
  import { useWorkspaceStore } from '@/stores/workspace';
  import { useUserStore } from '@/store';
  import { useRoute, useRouter } from 'vue-router';
  import { formatDateTime } from '@/utils/time';
  import { clearToken, getToken } from '@/utils/auth';

  type Conversation = {
    id: string;
    teamId: string;
    title: string;
    updatedAt: string;
  };
  type ChatMessage = {
    id: string;
    role: string;
    content: string;
    provider?: string;
  };
  type AgentTask = { id: string; title: string; status: string; priority: string; progress: number; dueDate: string | null };
  type WorkspaceFile = { id: string; name: string; size: number; updatedAt: string };
  const store = useWorkspaceStore();
  const user = useUserStore();
  const route = useRoute();
  const router = useRouter();
  const conversationId = ref('');
  const conversationTeamId = ref('');
  const confirmTeam = ref(false);
  const status = ref({ provider: 'mock', configured: false, simulated: true, agentTools: false, sandboxAvailable: false, shellAvailable: false, workspaceAvailable: false });
  const prompt = ref('');
  const allowCodeExecution = ref(false);
  const allowShellExecution = ref(false);
  const allowTeamKnowledgeCandidate = ref(false);
  const sandboxFileInput = ref<HTMLInputElement | null>(null);
  const sandboxFiles = ref<{ name: string; content: string; size: number }[]>([]);
  const sandboxFileError = ref('');
  const workspaceFiles = ref<WorkspaceFile[]>([]);
  const workspaceSelectedIDs = ref<string[]>([]);
  const persistWorkspaceOutput = ref(false);
  const workspaceLoading = ref(false);
  const workspaceSaving = ref('');
  const workspaceError = ref('');
  const workspaceEditorOpen = ref(false);
  const workspaceEditorSaving = ref(false);
  const workspaceEditorError = ref('');
  const workspaceDraft = ref({ id: '', name: '', content: '' });
  const sending = ref(false);
  const error = ref('');
  const conversations = ref<Conversation[]>([]);
  const messages = ref<ChatMessage[]>([]);
  const streamingText = ref('');
  const candidateModal = ref(false);
  const candidateSaving = ref(false);
  const candidateError = ref('');
  const candidateMessage = ref<ChatMessage | null>(null);
  const candidateCategory = ref('profile');
  const candidateContent = ref('');
  const extracting = ref(false);
  const toolRunning = ref(false);
  const agentRunning = ref(false);
  const conversationDeleting = ref(false);
  type AgentStepResult = { stdout?: string; files?: Record<string, string>; proposalId?: string; memoryId?: string; status?: string; count?: number; persisted?: boolean };
  const agentResult = ref<{ tool: string; reply: string; result?: AgentStepResult; steps?: Array<{ tool: string; result?: AgentStepResult }> } | null>(null);
  const toolResult = ref<{ count: number; items: AgentTask[] } | null>(null);
  let loadGeneration = 0;
  let messageGeneration = 0;
  let sendGeneration = 0;
  const visibleConversations = computed(() =>
    conversations.value.filter(
      (item) => !item.teamId || item.teamId === store.activeTeam.id
    )
  );
  async function addSandboxFiles(event: Event) {
    const input = event.target as HTMLInputElement;
    const selected = Array.from(input.files || []);
    sandboxFileError.value = '';
    try {
      if (sandboxFiles.value.length + selected.length > 12) throw new Error('最多添加 12 个文本文件。');
      const existing = new Set(sandboxFiles.value.map((file) => file.name));
      const loaded = [] as { name: string; content: string; size: number }[];
      let total = sandboxFiles.value.reduce((sum, file) => sum + file.size, 0);
      for (const file of selected) {
        if (file.size > 64 * 1024) throw new Error(`${file.name} 超过 64 KiB。`);
        if (existing.has(file.name) || loaded.some((item) => item.name === file.name)) throw new Error(`文件名重复：${file.name}`);
        const name = file.name;
        if (!name || name.length > 160 || /[\\/\r\n\t]/.test(name) || name.toLowerCase() === 'main.py' || name === '.' || name === '..') throw new Error(`文件名不适用于临时工作区：${name}`);
        total += file.size;
        if (total > 256 * 1024) throw new Error('所选文件总大小不能超过 256 KiB。');
        const content = new TextDecoder('utf-8', { fatal: true }).decode(await file.arrayBuffer());
        loaded.push({ name, content, size: file.size });
      }
      sandboxFiles.value = [...sandboxFiles.value, ...loaded];
    } catch (e) {
      sandboxFileError.value = (e as Error).message || '无法读取文件，请选择 UTF-8 文本文件。';
    } finally {
      if (input) input.value = '';
    }
  }
  function removeSandboxFile(name: string) {
    sandboxFiles.value = sandboxFiles.value.filter((file) => file.name !== name);
    sandboxFileError.value = '';
  }
  function downloadAgentFile(name: string, content: string) {
    const blobUrl = URL.createObjectURL(new Blob([content], { type: 'text/plain;charset=utf-8' }));
    const anchor = document.createElement('a');
    anchor.href = blobUrl;
    anchor.download = name.split('/').pop() || 'teammind-output.txt';
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
    window.setTimeout(() => URL.revokeObjectURL(blobUrl), 1000);
  }
  async function loadWorkspaceFiles() {
    if (!status.value.workspaceAvailable || workspaceLoading.value) return;
    workspaceLoading.value = true;
    workspaceError.value = '';
    try {
      const result = await api<{ files: WorkspaceFile[]; configured: boolean }>('GET', '/me/agent/workspace/files');
      workspaceFiles.value = result.files;
      workspaceSelectedIDs.value = workspaceSelectedIDs.value.filter((id) => result.files.some((file) => file.id === id));
    } catch (e) {
      workspaceError.value = (e as Error).message || '无法读取私人工作区。';
    } finally {
      workspaceLoading.value = false;
    }
  }
  async function saveWorkspaceFile(name: string, content: string) {
    if (!status.value.workspaceAvailable || workspaceSaving.value) return;
    workspaceSaving.value = name;
    workspaceError.value = '';
    try {
      await api('POST', '/me/agent/workspace/files', { name, content });
      await loadWorkspaceFiles();
      Message.success(`已加密保存 ${name}`);
    } catch (e) {
      workspaceError.value = (e as Error).message || '保存工作区文件失败。';
    } finally {
      workspaceSaving.value = '';
    }
  }
  async function importWorkspaceFiles(event: Event) {
    const input = event.target as HTMLInputElement;
    const selected = Array.from(input.files || []);
    input.value = '';
    if (!selected.length || !status.value.workspaceAvailable || workspaceSaving.value) return;
    workspaceError.value = '';
    for (const file of selected) {
      if (file.size > 64 * 1024) {
        workspaceError.value = `${file.name} 超过 64 KiB，未保存。`;
        break;
      }
      try {
        const content = new TextDecoder('utf-8', { fatal: true }).decode(await file.arrayBuffer());
        workspaceSaving.value = file.name;
        await api('POST', '/me/agent/workspace/files', { name: file.name, content });
      } catch (e) {
        workspaceError.value = (e as Error).message || `${file.name} 保存失败。`;
        break;
      } finally {
        workspaceSaving.value = '';
      }
    }
    await loadWorkspaceFiles();
    if (!workspaceError.value) Message.success(`已保存 ${selected.length} 个私人工作区文件`);
  }
  async function editWorkspaceFile(fileID: string) {
    workspaceSaving.value = `read:${fileID}`;
    workspaceError.value = '';
    try {
      const file = await api<{ id: string; name: string; content: string }>('GET', `/me/agent/workspace/files/${fileID}`);
      workspaceDraft.value = { id: file.id, name: file.name, content: file.content };
      workspaceEditorError.value = '';
      workspaceEditorOpen.value = true;
    } catch (e) {
      workspaceError.value = (e as Error).message || '无法读取工作区文件。';
    } finally {
      workspaceSaving.value = '';
    }
  }
  async function saveWorkspaceFileEdit(): Promise<boolean> {
    if (workspaceEditorSaving.value) return false;
    if (!workspaceDraft.value.name.trim() || new TextEncoder().encode(workspaceDraft.value.content).length > 64 * 1024) {
      workspaceEditorError.value = '请填写文件名，且内容不得超过 64 KiB。';
      return false;
    }
    workspaceEditorSaving.value = true;
    workspaceEditorError.value = '';
    try {
      await api('PUT', `/me/agent/workspace/files/${workspaceDraft.value.id}`, {
        name: workspaceDraft.value.name.trim(), content: workspaceDraft.value.content,
      });
      await loadWorkspaceFiles();
      Message.success('私人工作区文件已更新');
      return true;
    } catch (e) {
      workspaceEditorError.value = (e as Error).message || '更新工作区文件失败。';
      return false;
    } finally {
      workspaceEditorSaving.value = false;
    }
  }
  async function deleteWorkspaceFile(fileID: string) {
    if (workspaceSaving.value || agentRunning.value) return;
    workspaceSaving.value = `delete:${fileID}`;
    workspaceError.value = '';
    try {
      await api('DELETE', `/me/agent/workspace/files/${fileID}`);
      workspaceFiles.value = workspaceFiles.value.filter((file) => file.id !== fileID);
      workspaceSelectedIDs.value = workspaceSelectedIDs.value.filter((id) => id !== fileID);
      Message.success('私人工作区文件已删除');
    } catch (e) {
      workspaceError.value = (e as Error).message || '删除工作区文件失败。';
    } finally {
      workspaceSaving.value = '';
    }
  }
  async function load() {
    const request = ++loadGeneration;
    try {
      const [loadedConversations, loadedStatus] = await Promise.all([
        api<Conversation[]>('GET', '/me/conversations'),
        api<typeof status.value>('GET', '/me/ai/status'),
      ]);
      if (request !== loadGeneration) return;
      conversations.value = loadedConversations;
      status.value = loadedStatus;
      if (loadedStatus.workspaceAvailable) await loadWorkspaceFiles();
      else workspaceFiles.value = [];
      error.value = '';
      const target = typeof route.query.conversationId === 'string' ? route.query.conversationId : '';
      const sourceMessage = typeof route.query.sourceMessageId === 'string' ? route.query.sourceMessageId : '';
      const conversation = visibleConversations.value.find((item) => item.id.toLowerCase() === target.toLowerCase());
      if (conversation && conversation.id !== conversationId.value) await openConversation(conversation, sourceMessage);
    } catch (e) {
      if (request !== loadGeneration) return;
      error.value = (e as Error).message;
    }
  }
  function newChat() {
    sendGeneration += 1;
    streamController?.abort();
    streamController = null;
    sending.value = false;
    streamingText.value = '';
    messageGeneration += 1;
    conversationId.value = '';
    conversationTeamId.value = '';
    messages.value = [];
    prompt.value = '';
    clearSandboxInputs();
    allowTeamKnowledgeCandidate.value = false;
    confirmTeam.value = false;
    toolResult.value = null;
    agentResult.value = null;
    error.value = '';
    if (route.query.conversationId || route.query.sourceMessageId) {
      const query = { ...route.query };
      delete query.conversationId;
      delete query.sourceMessageId;
      router.replace({ path: '/teammind/assistant', query });
    }
  }
  async function deleteConversation() {
    const deletingID = conversationId.value;
    if (!deletingID || conversationDeleting.value) return;
    conversationDeleting.value = true;
    error.value = '';
    try {
      await api('DELETE', `/me/conversations/${deletingID}`);
      conversations.value = conversations.value.filter((item) => item.id !== deletingID);
      if (conversationId.value === deletingID) newChat();
      Message.success('对话和消息已删除；已确认记忆保留但移除了来源，未确认候选已清除。');
    } catch (e) {
      error.value = (e as Error).message;
    } finally {
      conversationDeleting.value = false;
    }
  }
  function clearSandboxInputs() {
    sandboxFiles.value = [];
    allowCodeExecution.value = false;
    allowShellExecution.value = false;
    persistWorkspaceOutput.value = false;
    workspaceSelectedIDs.value = [];
    sandboxFileError.value = '';
    if (sandboxFileInput.value) sandboxFileInput.value.value = '';
  }
  let streamController: AbortController | null = null;
  async function streamMessage(conversation: string, content: string, confirmedTeam: boolean) {
    const controller = new AbortController();
    streamController = controller;
    const timeout = window.setTimeout(() => controller.abort(), 50000);
    try {
      const base = (import.meta.env.VITE_API_BASE_URL || '/api/v1').replace(/\/+$/, '');
      const response = await fetch(
        `${base}/me/conversations/${conversation}/stream`,
        {
          method: 'POST',
          signal: controller.signal,
          headers: {
            'Content-Type': 'application/json',
            Authorization: `Bearer ${getToken() || ''}`,
          },
          body: JSON.stringify({ content, confirmTeam: confirmedTeam }),
        }
      );
      if (!response.ok) {
        const failure = await response.json().catch(() => ({}));
        if (response.status === 401) {
          clearToken();
          window.location.assign(`/login?redirect=${encodeURIComponent(window.location.pathname + window.location.search)}`);
        }
        throw new Error(failure.message || 'AI 请求失败，请重试');
      }
      if (!response.body) throw new Error('浏览器不支持流式响应');
      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let buffer = '';
      let completed = false;
      while (true) {
        const { value, done } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true }).replace(/\r\n/g, '\n');
        let boundary = buffer.indexOf('\n\n');
        while (boundary >= 0) {
          const frame = buffer.slice(0, boundary);
          buffer = buffer.slice(boundary + 2);
          const event = frame.split('\n').find((line) => line.startsWith('event:'))?.slice(6).trim();
          const dataLine = frame.split('\n').find((line) => line.startsWith('data:'));
          if (event && dataLine) {
            const data = JSON.parse(dataLine.slice(5).trim());
            if (event === 'delta') streamingText.value += String(data.content || '');
            if (event === 'error') throw new Error(data.message || 'AI 请求失败，请重试');
            if (event === 'done') completed = true;
          }
          boundary = buffer.indexOf('\n\n');
        }
      }
      if (!completed) throw new Error('AI 连接中断，回复未保存，请重试');
    } catch (e) {
      if (controller.signal.aborted) throw new Error('AI 请求超时或已取消，请重试');
      throw e;
    } finally {
      window.clearTimeout(timeout);
      if (streamController === controller) streamController = null;
    }
  }
  async function openConversation(conversation: Conversation, sourceMessageId = '') {
    const selectedTeamId = store.activeTeam.id;
    if (conversation.teamId && conversation.teamId !== selectedTeamId) return;
    const request = ++messageGeneration;
    error.value = '';
    try {
      const loadedMessages = await api<ChatMessage[]>(
        'GET',
        `/me/conversations/${conversation.id}/messages`
      );
      if (request !== messageGeneration || selectedTeamId !== store.activeTeam.id) return;
      messages.value = loadedMessages;
      conversationId.value = conversation.id;
      conversationTeamId.value = conversation.teamId || '';
      confirmTeam.value = false;
      allowTeamKnowledgeCandidate.value = false;
      toolResult.value = null;
      clearSandboxInputs();
      agentResult.value = null;
      if (sourceMessageId) {
        await nextTick();
        const source = Array.from(document.querySelectorAll<HTMLElement>('[data-message-id]')).find((item) => item.dataset.messageId?.toLowerCase() === sourceMessageId.toLowerCase());
        source?.scrollIntoView({ behavior: 'smooth', block: 'center' });
      }
    } catch (e) {
      if (request !== messageGeneration || selectedTeamId !== store.activeTeam.id) return;
      error.value = (e as Error).message;
    }
  }
  function openMemoryCandidate(message: ChatMessage) {
    if (!conversationId.value || !message.id || message.id.startsWith('local-')) {
      error.value = '请先等待消息保存完成，再创建记忆候选。';
      return;
    }
    candidateMessage.value = message;
    candidateCategory.value = message.role === 'assistant' ? 'operational_knowledge' : 'profile';
    candidateContent.value = message.content;
    candidateError.value = '';
    candidateModal.value = true;
  }
  async function saveMemoryCandidate() {
    if (candidateSaving.value) return;
    if (!candidateMessage.value || !candidateContent.value.trim()) {
      candidateError.value = '候选内容不能为空。';
      return;
    }
    candidateSaving.value = true;
    try {
      await api('POST', `/me/conversations/${conversationId.value}/messages/${candidateMessage.value.id}/memory-candidate`, {
        category: candidateCategory.value,
        content: candidateContent.value.trim(),
      });
      candidateModal.value = false;
      error.value = '';
      Message.success('记忆候选已保存，待你确认');
    } catch (e) {
      candidateError.value = (e as Error).message;
    } finally {
      candidateSaving.value = false;
    }
  }
  async function extractMemoryCandidates() {
    if (extracting.value || !conversationId.value || !status.value.configured || status.value.simulated) return;
    if (conversationTeamId.value && !confirmTeam.value) {
      error.value = '请先确认将当前团队对话发送给模型。';
      return;
    }
    extracting.value = true;
    error.value = '';
    try {
      const result = await api<{ created: Array<{ id: string }>; duplicates: number; simulated: boolean }>(
        'POST',
        `/me/conversations/${conversationId.value}/extract-memory-candidates`,
        { confirmTeam: confirmTeam.value }
      );
      if (!result.created.length) {
        Message.info(result.duplicates ? '候选内容已存在，无新记录。' : '本次对话没有发现明确的个人记忆。');
      } else {
        Message.success(`已保存 ${result.created.length} 条待确认候选，请到“记忆与知识”核对。`);
      }
    } catch (e) {
      error.value = (e as Error).message;
    } finally {
      extracting.value = false;
    }
  }
  async function runTeamTaskLookup() {
    if (toolRunning.value || !conversationId.value || !conversationTeamId.value) return;
    if (!confirmTeam.value || conversationTeamId.value !== store.activeTeam.id) {
      error.value = '请确认当前团队上下文后再运行只读工具。';
      return;
    }
    toolRunning.value = true;
    error.value = '';
    try {
      const result = await api<{ count: number; items: AgentTask[] }>(
        'POST',
        '/me/agent/tools/execute',
        { tool: 'list_team_tasks', teamId: conversationTeamId.value, confirmTeam: true, limit: 10 }
      );
      toolResult.value = result;
    } catch (e) {
      error.value = (e as Error).message;
    } finally {
      toolRunning.value = false;
    }
  }
  function agentToolLabel(tool: string) {
    return ({ list_team_members: '读取当前团队成员', list_team_tasks: '读取当前团队任务', list_team_projects: '读取当前团队项目', propose_task_change: '创建待审批计划提案', propose_project_change: '创建待审批项目提案', propose_member_change: '创建待审批成员提案', create_team_memory_candidate: '创建待确认团队知识候选', run_python: '断网临时 Python 沙箱', run_shell: '断网临时 Shell 沙箱' } as Record<string, string>)[tool] || '无工具调用';
  }
  function agentRunLabel(result: { tool: string; steps?: Array<{ tool: string }> }) {
    return result.steps && result.steps.length > 1
      ? `智能体完成 ${result.steps.length} 步顺序工作流`
      : `智能体工具执行：${agentToolLabel(result.tool)}`;
  }
  async function runAgent() {
    const instruction = prompt.value.trim();
    if (!instruction || agentRunning.value || sending.value || !status.value.agentTools) return;
    const selectedTeamId = store.activeTeam.id;
    const generation = messageGeneration;
    const authorizeCodeExecution = allowCodeExecution.value;
    const authorizeShellExecution = allowShellExecution.value;
    const authorizeTeamKnowledge = allowTeamKnowledgeCandidate.value;
    const authorizeWorkspacePersistence = persistWorkspaceOutput.value;
    // Consent applies to one request only; a retry needs another explicit click.
    allowCodeExecution.value = false;
    allowShellExecution.value = false;
    allowTeamKnowledgeCandidate.value = false;
    persistWorkspaceOutput.value = false;
    const useConfirmedTeam = !!conversationTeamId.value && confirmTeam.value;
    if (conversationTeamId.value && !useConfirmedTeam) {
      error.value = '请重新确认当前团队上下文后再运行智能体。';
      return;
    }
    agentRunning.value = true;
    error.value = '';
    agentResult.value = null;
    try {
      if (!conversationId.value) {
        const created = await api<{ id: string }>('POST', '/me/conversations', {
          teamId: confirmTeam.value ? selectedTeamId : '',
          confirmTeam: !!(confirmTeam.value && selectedTeamId),
        });
        if (generation !== messageGeneration || selectedTeamId !== store.activeTeam.id) return;
        conversationId.value = created.id;
        conversationTeamId.value = confirmTeam.value ? selectedTeamId : '';
      }
      const result = await api<{ provider: string; model: string; reply: string; tool: string; result?: AgentStepResult; steps?: Array<{ tool: string; result?: AgentStepResult }> }>(
        'POST',
        `/me/conversations/${conversationId.value}/agent`,
        { instruction, confirmTeam: !!(conversationTeamId.value && confirmTeam.value), allowCodeExecution: authorizeCodeExecution, allowShellExecution: authorizeShellExecution, allowTeamKnowledgeCandidate: authorizeTeamKnowledge, persistWorkspaceFiles: authorizeWorkspacePersistence, workspaceFileIds: workspaceSelectedIDs.value, files: sandboxFiles.value.map(({ name, content }) => ({ name, content })) }
      );
      if (generation !== messageGeneration || selectedTeamId !== store.activeTeam.id) return;
      agentResult.value = result;
      prompt.value = '';
      sandboxFiles.value = [];
      workspaceSelectedIDs.value = [];
      allowCodeExecution.value = false;
      allowShellExecution.value = false;
      allowTeamKnowledgeCandidate.value = false;
      sandboxFileError.value = '';
      messages.value = await api<ChatMessage[]>('GET', `/me/conversations/${conversationId.value}/messages`);
      await load();
      Message.success(result.steps?.length && result.steps.length > 1 ? `智能体完成 ${result.steps.length} 步顺序工作流` : result.tool ? `已运行：${agentToolLabel(result.tool)}` : '智能体已完成回复');
    } catch (e) {
      error.value = (e as Error).message;
    } finally {
      agentRunning.value = false;
    }
  }
  async function send() {
    if (!prompt.value.trim() || sending.value || !status.value.configured)
      return;
    allowTeamKnowledgeCandidate.value = false;
    sending.value = true;
    const sendRequest = ++sendGeneration;
    const selectedTeamId = store.activeTeam.id;
    const request = messageGeneration;
    streamingText.value = '';
    let content = prompt.value.trim();
    let confirmedTeam = confirmTeam.value;
    error.value = '';
    try {
      if (!conversationId.value) {
        const created = await api<{ id: string }>('POST', '/me/conversations', {
          teamId: confirmedTeam ? selectedTeamId : '',
          confirmTeam: confirmedTeam,
        });
        if (request !== messageGeneration || selectedTeamId !== store.activeTeam.id) return;
        conversationId.value = created.id;
        conversationTeamId.value = confirmedTeam ? selectedTeamId : '';
      }
      await streamMessage(conversationId.value, content, confirmedTeam);
      if (request !== messageGeneration || selectedTeamId !== store.activeTeam.id) return;
      const answer = streamingText.value;
      const stamp = Date.now();
      messages.value = [
        ...messages.value,
        { id: `local-user-${stamp}`, role: 'user', content, provider: '' },
        { id: `local-assistant-${stamp}`, role: 'assistant', content: answer, provider: status.value.provider },
      ];
      prompt.value = '';
      streamingText.value = '';
      try {
        const loadedMessages = await api<ChatMessage[]>(
          'GET',
          `/me/conversations/${conversationId.value}/messages`
        );
        if (request !== messageGeneration || selectedTeamId !== store.activeTeam.id) return;
        messages.value = loadedMessages;
      } catch {
        if (request === messageGeneration && selectedTeamId === store.activeTeam.id)
          error.value = '回复已保存，但历史记录暂时无法刷新。请点击历史对话或重新打开本页。';
      }
      await load();
    } catch (e) {
      if (request !== messageGeneration || selectedTeamId !== store.activeTeam.id) return;
      error.value = (e as Error).message;
      if (!streamingText.value) streamingText.value = '';
    } finally {
      if (sendRequest === sendGeneration) sending.value = false;
    }
  }
  watch(
    () => store.activeTeam.id,
    () => {
      loadGeneration += 1;
      sendGeneration += 1;
      sending.value = false;
      newChat();
      load();
    }
  );
  watch(() => route.query.conversationId, (value) => {
    if (typeof value !== 'string') return;
    const conversation = visibleConversations.value.find((item) => item.id.toLowerCase() === value.toLowerCase());
    if (!conversation) return;
    const sourceMessage = typeof route.query.sourceMessageId === 'string' ? route.query.sourceMessageId : '';
    openConversation(conversation, sourceMessage);
  });
  onMounted(load);
  onUnmounted(() => {
    sendGeneration += 1;
    streamController?.abort();
    clearSandboxInputs();
  });
</script>
<style scoped>
  .tm-conversations {
    margin-bottom: 16px;
  }
  .tm-agent-result { display: grid; gap: 8px; margin: 12px 0; padding: 12px; border: 1px solid var(--color-border-2); border-radius: 8px; }
  .tm-agent-memory-candidate { display: grid; justify-items: start; gap: 8px; }
  .tm-agent-consent { display: flex; align-items: flex-start; gap: 8px; }
  .tm-agent-result article { display: grid; gap: 3px; overflow-wrap: anywhere; }
  .tm-agent-steps { display: grid; gap: 6px; margin: 0; padding-left: 22px; }
  .tm-agent-steps li { display: grid; gap: 3px; overflow-wrap: anywhere; }
  .tm-agent-file-heading { display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap; }
  .tm-agent-file-actions { display: flex; gap: 8px; flex-wrap: wrap; }
  .tm-sandbox-options { display: grid; gap: 8px; min-width: 0; margin: 0; padding: 12px; border: 1px solid var(--color-border-2); border-radius: 8px; }
  .tm-workspace-files { display: grid; gap: 8px; min-width: 0; padding: 12px; border: 1px solid var(--color-border-2); border-radius: 8px; }
  .tm-workspace-file-select { display: flex; align-items: center; gap: 8px; min-width: 0; overflow-wrap: anywhere; }
  .tm-workspace-consent { display: flex; align-items: flex-start; gap: 8px; }
  .tm-agent-upload { display: flex; align-items: center; justify-content: space-between; gap: 8px; overflow-wrap: anywhere; }
  .tm-agent-result span { color: var(--color-text-2); font-size: 13px; }
  .tm-chat-actions {
    margin-bottom: 12px;
  }
  .tm-conversation-list {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(min(100%, 240px), 1fr));
    gap: 8px;
  }
  .tm-conversation {
    min-height: 50px;
    padding: 9px 12px;
    border: 1px solid var(--color-border-2);
    border-radius: 6px;
    background: var(--color-bg-2);
    color: var(--color-text-1);
    text-align: left;
    cursor: pointer;
  }
  .tm-conversation.active {
    border-color: #165dff;
    background: #e8f3ff;
  }
  .tm-conversation span,
  .tm-conversation small {
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .tm-conversation small {
    color: var(--color-text-2);
  }
</style>
