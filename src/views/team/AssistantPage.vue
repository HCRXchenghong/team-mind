<template>
  <div class="page assistant-page">
    <PageHeader title="AI 助手" :subtitle="aiSubtitle">
      <template #actions>
        <a-button size="small" @click="filesOpen = true">工作区文件</a-button>
        <a-button size="small" :disabled="!current" @click="openSummary">会话摘要</a-button>
        <a-button type="primary" @click="openCreate"><plus-outlined /> 新建对话</a-button>
      </template>
    </PageHeader>

    <a-alert v-if="!aiStatus.configured" type="warning" show-icon message="AI 服务尚未配置" description="请联系管理员在 AI 管理里添加供应商，配置后即可开始对话。" />

    <div class="assistant panel">
      <aside class="conv-side">
        <div class="conv-head">
          <span>对话</span>
          <a-button type="text" size="small" @click="loadConversations"><reload-outlined /></a-button>
        </div>
        <div class="conv-list">
          <div
            v-for="conversation in conversations"
            :key="conversation.id"
            class="conv"
            :class="{ active: current?.id === conversation.id }"
            @click="select(conversation)"
          >
            <div class="conv-main">
              <div class="conv-title">{{ conversation.title || '新对话' }}</div>
              <div class="conv-meta">
                <a-tag v-if="conversation.teamId" :bordered="false" color="blue" class="tiny">团队</a-tag>
                <span>{{ fmtDateTime(conversation.updatedAt) }}</span>
              </div>
            </div>
            <a-popconfirm title="删除这段对话？" @confirm="removeConversation(conversation)">
              <a-button type="text" size="small" class="conv-del" @click.stop><delete-outlined /></a-button>
            </a-popconfirm>
          </div>
          <div v-if="!conversations.length" class="empty-block small">还没有对话</div>
        </div>
        <div class="conv-foot">
          <a-checkbox v-model:checked="autoTools">自动工具</a-checkbox>
          <a-checkbox v-model:checked="allowWeb">允许联网</a-checkbox>
        </div>
      </aside>

      <main class="chat">
        <template v-if="current">
          <div class="chat-head">
            <div>
              <div class="chat-title">{{ current.title || '新对话' }}</div>
              <div class="chat-meta">
                <a-tag v-if="current.teamId" color="blue" :bordered="false">团队上下文</a-tag>
                <a-tag v-else :bordered="false">私人对话</a-tag>
                <span class="muted">{{ messages.length }} 条消息</span>
              </div>
            </div>
            <div class="chat-actions">
              <a-button size="small" @click="openExtract" :disabled="!current.teamId">提取记忆候选</a-button>
              <a-button size="small" @click="openAgent('agent')">智能体执行</a-button>
              <a-button size="small" @click="openAgent('collaborate')">多智能体协作</a-button>
            </div>
          </div>

          <a-alert
            v-if="current.teamId && !teamConfirmed"
            type="warning"
            show-icon
            class="confirm-bar"
            message="这段对话使用团队上下文"
            description="发送前请确认：你允许把当前组织上下文发送给 AI。"
          >
            <template #action>
              <a-button size="small" type="primary" @click="teamConfirmed = true">我确认</a-button>
            </template>
          </a-alert>

          <div ref="scrollRef" class="messages">
            <div v-for="message in messages" :key="message.id" class="message" :class="message.role === 'assistant' ? 'ai' : 'user'">
              <div class="bubble">
                <div class="bubble-head">
                  <span>{{ message.role === 'user' ? '我' : 'TeamMind AI' }}</span>
                  <span class="muted">{{ fmtDateTime(message.createdAt) }}</span>
                </div>
                <div v-if="message.role === 'assistant'" class="markdown" v-html="renderMarkdown(message.content)" />
                <div v-else class="plain">{{ message.content }}</div>
                <div v-if="message.role === 'assistant' && message.id && !message.id.startsWith('tmp')" class="bubble-actions">
                  <a-button type="link" size="small" @click="openMemory(message)">保存为记忆候选</a-button>
                </div>
              </div>
            </div>
            <div v-if="!messages.length" class="empty-block">
              开始一段新对话。可以问任务进度、让它总结项目风险，或基于团队上下文起草计划。
            </div>
          </div>

          <div class="composer">
            <a-textarea
              v-model:value="input"
              :auto-size="{ minRows: 2, maxRows: 6 }"
              placeholder="输入消息，Ctrl/⌘ + Enter 发送"
              :disabled="sending"
              @keydown="onKeydown"
            />
            <div class="composer-foot">
              <span class="muted">AI 可能会出错，请核对重要信息。</span>
              <div class="composer-actions">
                <a-button v-if="sending" danger @click="stopStream">停止</a-button>
                <a-button type="primary" :loading="sending" :disabled="!input.trim()" @click="send">发送</a-button>
              </div>
            </div>
          </div>
        </template>

        <div v-else class="chat-empty">
          <robot-outlined class="big-icon" />
          <h2>选择或创建一段对话</h2>
          <p class="muted">私人对话不携带组织数据；带团队上下文的对话需要你明确确认。</p>
          <a-button type="primary" @click="openCreate">新建对话</a-button>
        </div>
      </main>
    </div>

    <!-- 新建对话 -->
    <a-modal v-model:open="createOpen" title="新建对话" :confirm-loading="creating" @ok="createConversation">
      <a-form layout="vertical">
        <a-form-item label="对话类型">
          <a-radio-group v-model:value="newConversation.scope">
            <a-radio value="personal">私人对话</a-radio>
            <a-radio value="team">使用当前组织上下文</a-radio>
          </a-radio-group>
        </a-form-item>
        <a-alert v-if="newConversation.scope === 'team'" type="warning" show-icon message="创建后，发送的消息可能包含当前组织的项目与任务上下文。" />
      </a-form>
    </a-modal>

    <!-- 摘要 -->
    <a-modal v-model:open="summaryOpen" title="会话摘要" :footer="null">
      <a-spin :spinning="summaryLoading">
        <p class="summary-text">{{ summary || '暂无摘要' }}</p>
      </a-spin>
      <a-button type="primary" ghost :loading="summaryLoading" @click="generateSummary">重新生成</a-button>
    </a-modal>

    <!-- 记忆候选 -->
    <a-modal v-model:open="memoryOpen" title="保存为记忆候选" :confirm-loading="saving" @ok="saveMemory">
      <a-form layout="vertical">
        <a-form-item label="类别">
          <a-select v-model:value="memoryForm.category">
            <a-select-option v-for="item in MEMORY_CATEGORY" :key="item.value" :value="item.value">{{ item.label }}</a-select-option>
          </a-select>
        </a-form-item>
        <a-form-item label="内容" required>
          <a-textarea v-model:value="memoryForm.content" :rows="5" />
        </a-form-item>
        <p class="muted" style="margin: 0; font-size: 11.5px">候选记忆需要确认后才会进入个人记忆，不会自动影响团队知识。</p>
      </a-form>
    </a-modal>

    <!-- 智能体 -->
    <a-modal v-model:open="agentOpen" :title="agentMode === 'agent' ? '智能体执行' : '多智能体协作'" :confirm-loading="agentLoading" width="640px" @ok="runAgent">
      <a-form layout="vertical">
        <a-form-item label="任务指令" required>
          <a-textarea v-model:value="agentForm.instruction" :rows="4" placeholder="例如：梳理本周阻塞任务，给出处理建议" />
        </a-form-item>
        <template v-if="agentMode === 'agent'">
          <a-form-item label="本次授权">
            <a-space direction="vertical">
              <a-checkbox v-if="current?.teamId" v-model:checked="agentForm.confirmTeam">确认使用团队上下文</a-checkbox>
              <a-checkbox v-model:checked="agentForm.allowWebAccess">允许本次联网</a-checkbox>
              <a-checkbox v-model:checked="agentForm.allowCodeExecution">允许本次 Python 执行</a-checkbox>
              <a-checkbox v-model:checked="agentForm.allowShellExecution">允许本次 Shell 执行</a-checkbox>
              <a-checkbox v-model:checked="agentForm.allowTeamKnowledgeCandidate">允许生成团队知识候选</a-checkbox>
              <a-checkbox v-model:checked="agentForm.persistWorkspaceFiles">保存本次生成的文件到工作区</a-checkbox>
            </a-space>
          </a-form-item>
          <a-form-item label="使用工作区文件">
            <a-select v-model:value="agentForm.workspaceFileIds" mode="multiple" placeholder="选择文件（可选）">
              <a-select-option v-for="file in workspaceFiles" :key="file.id" :value="file.id">{{ file.name }}</a-select-option>
            </a-select>
          </a-form-item>
          <a-form-item label="附加临时文本文件（可选）">
            <a-input-group compact>
              <a-input v-model:value="tempFile.name" placeholder="文件名，例如 notes.md" style="width: 40%" />
            </a-input-group>
            <a-textarea v-model:value="tempFile.content" :rows="3" placeholder="文件内容，仅本次使用" style="margin-top: 8px" />
          </a-form-item>
        </template>
        <template v-else>
          <a-alert v-if="current?.teamId" type="warning" show-icon message="多智能体协作会读取团队上下文，请确认。" />
          <a-form-item label="授权">
            <a-space direction="vertical">
              <a-checkbox v-if="current?.teamId" v-model:checked="agentForm.confirmTeam">确认使用团队上下文</a-checkbox>
              <a-checkbox v-model:checked="agentForm.allowWebAccess">允许本次联网</a-checkbox>
            </a-space>
          </a-form-item>
        </template>
      </a-form>
    </a-modal>

    <a-modal v-model:open="resultOpen" title="执行结果" :footer="null" width="720px">
      <pre class="result">{{ result }}</pre>
    </a-modal>

    <!-- 工作区文件 -->
    <a-drawer v-model:open="filesOpen" title="个人工作区文件" :width="640">
      <a-alert type="info" show-icon message="文件保存在个人加密工作区，仅支持 UTF-8 文本，单文件 ≤ 64 KiB。" />
      <div class="file-actions">
        <a-button type="primary" size="small" @click="openFileEditor()">新建文本文件</a-button>
        <a-button size="small" @click="pickFile">导入本地文本</a-button>
        <input ref="fileInput" type="file" accept=".txt,.md,.csv,.json,.py,.js,.ts,.html,.css,text/*" style="display: none" @change="importFile" />
      </div>
      <a-spin :spinning="filesLoading">
        <div v-for="file in workspaceFiles" :key="file.id" class="file">
          <div>
            <div class="file-name">{{ file.name }}</div>
            <div class="muted file-meta">{{ file.size }} 字节 · {{ fmtDateTime(file.updatedAt) }}</div>
          </div>
          <div class="table-actions">
            <a-button type="link" size="small" @click="openFileEditor(file)">打开</a-button>
            <a-popconfirm title="永久删除该文件？" @confirm="removeFile(file)">
              <a-button type="link" size="small" danger>删除</a-button>
            </a-popconfirm>
          </div>
        </div>
        <div v-if="!workspaceFiles.length" class="empty-block small">工作区还没有文件</div>
      </a-spin>
    </a-drawer>

    <a-modal v-model:open="fileEditOpen" :title="fileDraft.id ? '编辑文件' : '新建文件'" :confirm-loading="saving" width="640px" @ok="saveFile">
      <a-form layout="vertical">
        <a-form-item label="文件名"><a-input v-model:value="fileDraft.name" placeholder="notes.md" /></a-form-item>
        <a-form-item label="UTF-8 内容">
          <a-textarea v-model:value="fileDraft.content" :rows="14" placeholder="文本内容" />
        </a-form-item>
      </a-form>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref } from 'vue';
import { DeleteOutlined, PlusOutlined, ReloadOutlined, RobotOutlined } from '@ant-design/icons-vue';
import { marked } from 'marked';
import DOMPurify from 'dompurify';
import { api, streamFetch } from '@/api/client';
import type { AiStatus, ChatMessage, Conversation, WorkspaceFile } from '@/api/types';
import { useSession } from '@/store/session';
import { fmtDateTime } from '@/utils/format';
import { MEMORY_CATEGORY } from '@/utils/labels';
import { notify } from '@/utils/notify';
import PageHeader from '@/components/PageHeader.vue';

const session = useSession();
const conversations = ref<Conversation[]>([]);
const current = ref<Conversation | null>(null);
const messages = ref<ChatMessage[]>([]);
const input = ref('');
const sending = ref(false);
const autoTools = ref(true);
const allowWeb = ref(false);
const teamConfirmed = ref(false);
const aiStatus = ref<AiStatus>({
  provider: '',
  configured: false,
  agentTools: false,
  sandboxAvailable: false,
  shellAvailable: false,
  webAvailable: false,
  workspaceAvailable: false,
});
const scrollRef = ref<HTMLElement | null>(null);
let controller: AbortController | undefined;

const aiSubtitle = computed(() =>
  aiStatus.value.configured
    ? `供应商池已就绪 · ${aiStatus.value.agentTools ? '工具可用' : '仅对话'} · ${current.value?.teamId ? '团队上下文' : '私人对话'}`
    : '尚未配置 AI 供应商',
);

function renderMarkdown(text: string): string {
  const html = marked.parse(text || '', { async: false }) as string;
  return DOMPurify.sanitize(html);
}

async function loadStatus(): Promise<void> {
  try {
    aiStatus.value = await api<AiStatus>('GET', '/me/ai/status');
  } catch {
    /* 状态接口失败不阻塞对话 */
  }
}

async function loadConversations(): Promise<void> {
  try {
    conversations.value = await api<Conversation[]>('GET', '/me/conversations');
    if (!current.value && conversations.value.length) await select(conversations.value[0]);
  } catch (e) {
    notify.error(e);
  }
}

async function select(conversation: Conversation): Promise<void> {
  current.value = conversation;
  teamConfirmed.value = !conversation.teamId;
  messages.value = [];
  try {
    messages.value = await api<ChatMessage[]>('GET', `/me/conversations/${conversation.id}/messages`);
    scrollToBottom();
  } catch (e) {
    notify.error(e);
  }
}

function scrollToBottom(): void {
  nextTick(() => {
    if (scrollRef.value) scrollRef.value.scrollTop = scrollRef.value.scrollHeight;
  });
}

/* 新建 */
const createOpen = ref(false);
const creating = ref(false);
const newConversation = reactive({ scope: 'personal' as 'personal' | 'team' });

function openCreate(): void {
  newConversation.scope = 'personal';
  createOpen.value = true;
}

async function createConversation(): Promise<void> {
  creating.value = true;
  try {
    const isTeam = newConversation.scope === 'team';
    const result = await api<{ id: string }>('POST', '/me/conversations', isTeam ? { teamId: session.activeId, confirmTeam: true } : {});
    createOpen.value = false;
    await loadConversations();
    const created = conversations.value.find((item) => item.id === result.id);
    if (created) await select(created);
  } catch (e) {
    notify.error(e);
  } finally {
    creating.value = false;
  }
}

async function removeConversation(conversation: Conversation): Promise<void> {
  try {
    await api('DELETE', `/me/conversations/${conversation.id}`);
    if (current.value?.id === conversation.id) {
      current.value = null;
      messages.value = [];
    }
    await loadConversations();
    notify.success('对话已删除');
  } catch (e) {
    notify.error(e);
  }
}

/* 发送 */
function onKeydown(event: KeyboardEvent): void {
  if ((event.metaKey || event.ctrlKey) && event.key === 'Enter') {
    event.preventDefault();
    send();
  }
}

async function send(): Promise<void> {
  if (!current.value || !input.value.trim() || sending.value) return;
  if (current.value.teamId && !teamConfirmed.value) {
    notify.error('请先确认团队上下文');
    return;
  }
  const content = input.value.trim();
  input.value = '';
  const conversationId = current.value.id;
  messages.value.push({ id: `tmp-user-${Date.now()}`, role: 'user', content });
  messages.value.push({ id: `tmp-reply-${Date.now()}`, role: 'assistant', content: '' });
  sending.value = true;
  scrollToBottom();

  try {
    controller = new AbortController();
    const response = await streamFetch(
      `/me/conversations/${conversationId}/stream`,
      {
        content,
        confirmTeam: Boolean(current.value.teamId && teamConfirmed.value),
        autoTools: autoTools.value,
        allowWebAccess: allowWeb.value,
      },
      controller.signal,
    );
    const reader = response.body?.getReader();
    if (!reader) throw new Error('服务未返回流');
    const decoder = new TextDecoder();
    let buffer = '';
    while (true) {
      const { value, done } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      const blocks = buffer.split('\n\n');
      buffer = blocks.pop() || '';
      for (const block of blocks) {
        const event = block.match(/^event:\s*(.+)$/m)?.[1];
        const dataLine = block
          .split('\n')
          .filter((line) => line.startsWith('data:'))
          .map((line) => line.slice(5).trim())
          .join('\n');
        if (!dataLine) continue;
        let payload: Record<string, any>;
        try {
          payload = JSON.parse(dataLine);
        } catch {
          continue;
        }
        if (event === 'delta' && current.value?.id === conversationId) {
          const last = messages.value[messages.value.length - 1];
          last.content += payload.content || '';
          scrollToBottom();
        }
        if (event === 'error') throw new Error(payload.message || 'AI 响应失败');
      }
    }
    await select(current.value as Conversation);
    await loadConversations();
  } catch (e) {
    if ((e as Error).name !== 'AbortError') notify.error(e);
  } finally {
    sending.value = false;
    controller = undefined;
  }
}

function stopStream(): void {
  controller?.abort();
  sending.value = false;
}

/* 摘要 */
const summaryOpen = ref(false);
const summary = ref('');
const summaryLoading = ref(false);

async function openSummary(): Promise<void> {
  if (!current.value) return;
  summaryOpen.value = true;
  summaryLoading.value = true;
  try {
    const result = await api<{ summary?: string } | string>('GET', `/me/conversations/${current.value.id}/summary`);
    summary.value = typeof result === 'string' ? result : result.summary || '';
  } catch {
    summary.value = '';
  } finally {
    summaryLoading.value = false;
  }
}

async function generateSummary(): Promise<void> {
  if (!current.value) return;
  summaryLoading.value = true;
  try {
    const result = await api<{ summary?: string } | string>('POST', `/me/conversations/${current.value.id}/summary`);
    summary.value = typeof result === 'string' ? result : result.summary || '';
  } catch (e) {
    notify.error(e);
  } finally {
    summaryLoading.value = false;
  }
}

/* 记忆候选 */
const memoryOpen = ref(false);
const saving = ref(false);
const memoryForm = reactive({ category: 'experience', content: '', messageId: '' });

function openMemory(message: ChatMessage): void {
  memoryForm.category = 'experience';
  memoryForm.content = message.content.slice(0, 4000);
  memoryForm.messageId = message.id;
  memoryOpen.value = true;
}

async function saveMemory(): Promise<void> {
  if (!current.value || !memoryForm.content.trim()) {
    notify.error('请填写记忆内容');
    return;
  }
  saving.value = true;
  try {
    await api('POST', `/me/conversations/${current.value.id}/messages/${memoryForm.messageId}/memory-candidate`, {
      category: memoryForm.category,
      content: memoryForm.content.trim(),
    });
    memoryOpen.value = false;
    notify.success('已保存为记忆候选');
  } catch (e) {
    notify.error(e);
  } finally {
    saving.value = false;
  }
}

async function openExtract(): Promise<void> {
  if (!current.value?.teamId) return;
  const confirmed = await new Promise<boolean>((resolve) => {
    if (teamConfirmed.value) return resolve(true);
    resolve(window.confirm('提取记忆候选会读取本次对话的团队上下文，确认继续？'));
  });
  if (!confirmed) return;
  try {
    await api('POST', `/me/conversations/${current.value.id}/extract-memory-candidates`, { confirmTeam: true });
    notify.success('已提取记忆候选');
  } catch (e) {
    notify.error(e);
  }
}

/* 智能体 */
const agentOpen = ref(false);
const agentLoading = ref(false);
const agentMode = ref<'agent' | 'collaborate'>('agent');
const resultOpen = ref(false);
const result = ref('');
const agentForm = reactive({
  instruction: '',
  confirmTeam: false,
  allowWebAccess: false,
  allowCodeExecution: false,
  allowShellExecution: false,
  allowTeamKnowledgeCandidate: false,
  persistWorkspaceFiles: false,
  workspaceFileIds: [] as string[],
});
const tempFile = reactive({ name: '', content: '' });

function openAgent(mode: 'agent' | 'collaborate'): void {
  if (!current.value) return;
  agentMode.value = mode;
  agentForm.instruction = '';
  agentForm.confirmTeam = !current.value.teamId;
  agentForm.allowWebAccess = false;
  agentForm.allowCodeExecution = false;
  agentForm.allowShellExecution = false;
  agentForm.allowTeamKnowledgeCandidate = false;
  agentForm.persistWorkspaceFiles = false;
  agentForm.workspaceFileIds = [];
  tempFile.name = '';
  tempFile.content = '';
  agentOpen.value = true;
}

async function runAgent(): Promise<void> {
  if (!current.value || !agentForm.instruction.trim()) {
    notify.error('请填写任务指令');
    return;
  }
  if (current.value.teamId && !agentForm.confirmTeam) {
    notify.error('请确认使用团队上下文');
    return;
  }
  agentLoading.value = true;
  try {
    const isAgent = agentMode.value === 'agent';
    const body: Record<string, unknown> = isAgent
      ? {
          ...agentForm,
          files: tempFile.name.trim() ? [{ name: tempFile.name.trim(), content: tempFile.content }] : [],
        }
      : {
          instruction: agentForm.instruction,
          confirmTeam: agentForm.confirmTeam,
          allowWebAccess: agentForm.allowWebAccess,
        };
    const response = await api<Record<string, any>>(
      'POST',
      `/me/conversations/${current.value.id}/${agentMode.value}`,
      body,
    );
    agentOpen.value = false;
    result.value = JSON.stringify(response, null, 2);
    resultOpen.value = true;
    await select(current.value);
  } catch (e) {
    notify.error(e);
  } finally {
    agentLoading.value = false;
  }
}

/* 工作区文件 */
const filesOpen = ref(false);
const filesLoading = ref(false);
const workspaceFiles = ref<WorkspaceFile[]>([]);
const fileInput = ref<HTMLInputElement | null>(null);
const fileEditOpen = ref(false);
const fileDraft = reactive({ id: '', name: '', content: '' });

async function loadFiles(): Promise<void> {
  filesLoading.value = true;
  try {
    const result = await api<{ files?: WorkspaceFile[] }>('GET', '/me/agent/workspace/files');
    workspaceFiles.value = result.files || [];
  } catch (e) {
    notify.error(e);
  } finally {
    filesLoading.value = false;
  }
}

function openFileEditor(file?: WorkspaceFile): void {
  Object.assign(fileDraft, { id: file?.id || '', name: file?.name || '', content: '' });
  fileEditOpen.value = true;
  if (file) {
    api<{ name: string; content: string }>('GET', `/me/agent/workspace/files/${file.id}`)
      .then((detail) => {
        fileDraft.name = detail.name;
        fileDraft.content = detail.content || '';
      })
      .catch((e) => notify.error(e));
  }
}

async function saveFile(): Promise<void> {
  if (!fileDraft.name.trim()) {
    notify.error('请填写文件名');
    return;
  }
  if (new Blob([fileDraft.content]).size > 65536) {
    notify.error('文件超过 64 KiB');
    return;
  }
  saving.value = true;
  try {
    if (fileDraft.id) {
      await api('PUT', `/me/agent/workspace/files/${fileDraft.id}`, { name: fileDraft.name, content: fileDraft.content });
    } else {
      await api('POST', '/me/agent/workspace/files', { name: fileDraft.name, content: fileDraft.content });
    }
    fileEditOpen.value = false;
    notify.success('文件已保存');
    await loadFiles();
  } catch (e) {
    notify.error(e);
  } finally {
    saving.value = false;
  }
}

async function removeFile(file: WorkspaceFile): Promise<void> {
  try {
    await api('DELETE', `/me/agent/workspace/files/${file.id}`);
    await loadFiles();
  } catch (e) {
    notify.error(e);
  }
}

function pickFile(): void {
  fileInput.value?.click();
}

async function importFile(event: Event): Promise<void> {
  const target = event.target as HTMLInputElement;
  const file = target.files?.[0];
  if (!file) return;
  if (file.size > 65536) {
    notify.error('文件超过 64 KiB');
    target.value = '';
    return;
  }
  Object.assign(fileDraft, { id: '', name: file.name, content: await file.text() });
  fileEditOpen.value = true;
  target.value = '';
}

onMounted(async () => {
  await loadStatus();
  await loadConversations();
  await loadFiles();
});
onBeforeUnmount(() => controller?.abort());
</script>

<style scoped>
.assistant {
  display: grid;
  grid-template-columns: 272px minmax(0, 1fr);
  min-height: 640px;
  overflow: hidden;
}
.conv-side {
  display: flex;
  flex-direction: column;
  border-right: 1px solid var(--tm-border);
  background: var(--tm-bg-soft);
  min-height: 0;
}
.conv-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 14px;
  font-size: 12px;
  font-weight: 600;
  color: var(--tm-muted);
  border-bottom: 1px solid var(--tm-border);
}
.conv-list {
  flex: 1;
  overflow-y: auto;
  padding: 6px;
}
.conv {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 9px 10px;
  border-radius: 7px;
  cursor: pointer;
}
.conv:hover {
  background: var(--tm-panel);
}
.conv.active {
  background: var(--tm-panel);
  box-shadow: inset 0 0 0 1px var(--tm-border);
}
.conv-main {
  flex: 1;
  min-width: 0;
}
.conv-title {
  font-size: 12.5px;
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.conv-meta {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 2px;
  color: var(--tm-muted);
  font-size: 10.5px;
}
.tiny {
  font-size: 9.5px;
  line-height: 15px;
  padding-inline: 5px;
}
.conv-del {
  opacity: 0;
}
.conv:hover .conv-del {
  opacity: 1;
}
.conv-foot {
  display: flex;
  gap: 10px;
  padding: 10px 14px;
  border-top: 1px solid var(--tm-border);
  font-size: 11.5px;
}
.chat {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
}
.chat-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding: 14px 18px;
  border-bottom: 1px solid var(--tm-border);
  flex-wrap: wrap;
}
.chat-title {
  font-weight: 650;
  font-size: 14px;
}
.chat-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 4px;
  font-size: 11.5px;
}
.chat-actions {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}
.confirm-bar {
  margin: 10px 18px 0;
}
.messages {
  flex: 1;
  overflow-y: auto;
  padding: 16px 18px;
  min-height: 340px;
  max-height: 58vh;
  background: var(--tm-panel);
}
.message {
  display: flex;
  margin-bottom: 14px;
}
.message.ai {
  align-items: flex-start;
}
.message.user {
  justify-content: flex-end;
}
.bubble {
  max-width: 84%;
  padding: 10px 13px;
  border-radius: 10px;
  background: var(--tm-panel-2);
  font-size: 12.5px;
  line-height: 1.7;
}
.message.user .bubble {
  background: var(--tm-primary-soft);
}
.bubble-head {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 4px;
  font-size: 11px;
  font-weight: 600;
}
.plain {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.markdown {
  overflow-wrap: anywhere;
}
.markdown :deep(pre) {
  overflow-x: auto;
  padding: 10px;
  border-radius: 6px;
  background: rgba(0, 0, 0, 0.06);
  font-size: 12px;
}
.markdown :deep(code) {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 11.5px;
}
.markdown :deep(p) {
  margin: 0 0 8px;
}
.markdown :deep(table) {
  width: 100%;
  margin: 6px 0 10px;
  border-collapse: collapse;
  font-size: 12px;
}
.markdown :deep(th),
.markdown :deep(td) {
  padding: 5px 8px;
  border: 1px solid var(--tm-border);
  text-align: left;
}
.markdown :deep(th) {
  background: var(--tm-panel-2);
  font-weight: 600;
}
.markdown :deep(ul),
.markdown :deep(ol) {
  margin: 4px 0 8px;
  padding-left: 20px;
}
.markdown :deep(p:last-child) {
  margin-bottom: 0;
}
.bubble-actions {
  margin-top: 4px;
  opacity: 0;
}
.bubble:hover .bubble-actions {
  opacity: 1;
}
.composer {
  padding: 12px 18px 14px;
  border-top: 1px solid var(--tm-border);
}
.composer-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-top: 8px;
  font-size: 11px;
}
.composer-actions {
  display: flex;
  gap: 8px;
}
.chat-empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 60px 20px;
}
.chat-empty h2 {
  margin: 6px 0 0;
  font-size: 18px;
}
.big-icon {
  font-size: 42px;
  color: var(--tm-primary);
  opacity: 0.7;
}
.summary-text {
  min-height: 120px;
  max-height: 52vh;
  overflow: auto;
  padding: 12px;
  border-radius: 8px;
  background: var(--tm-panel-2);
  white-space: pre-wrap;
  font-size: 12.5px;
}
.result {
  max-height: 62vh;
  overflow: auto;
  margin: 0;
  padding: 12px;
  border-radius: 8px;
  background: var(--tm-panel-2);
  font-size: 11.5px;
  line-height: 1.6;
}
.file-actions {
  display: flex;
  gap: 8px;
  margin: 14px 0;
}
.file {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 0;
  border-bottom: 1px solid var(--tm-border);
}
.file-name {
  font-weight: 600;
  font-size: 12.5px;
}
.file-meta {
  font-size: 11px;
}
.empty-block.small {
  padding: 20px;
  font-size: 12px;
}

@media (max-width: 900px) {
  .assistant {
    grid-template-columns: minmax(0, 1fr);
  }
  .conv-side {
    border-right: 0;
    border-bottom: 1px solid var(--tm-border);
    max-height: 220px;
  }
  .messages {
    max-height: none;
  }
}
</style>
