/* API 封装：REST + SSE。v0.2：bootstrap 模式（会话对用户隐藏）。 */

/* 人机决策持久化记录（后端 hooks.DecisionRecord 的 JSON 形状） */
export interface DecisionRecord {
  callId: string
  kind: 'approve' | 'ask' | 'plan'
  resolution: string
  ts: number
}

export interface HistoryMessage {
  role: string
  content: string
  tool_call_id?: string
  tool_calls?: { ID: string; Name: string; Args: string | Record<string, unknown> }[]
  err?: string
  reasoning?: string
  images?: ImagePayload[]
}

/* 多模态图片输入（内嵌 base64，后端 types.ImagePart）——旧会话历史展示用 */
export interface ImagePayload {
  mimeType: string
  data: string
}

/* 附件引用载荷（一切皆资源：发送只传工作目录内路径，不传内容） */
export interface FilePayload {
  name: string
  path: string
}

/* 文件引用载荷（统一进 <reference_file> 记录）：items 空 = 整文件
引用（附件 chips），有 = 文件页标注（片段+行号+备注） */
export interface FileRefItem {
  sel: string
  note: string
  from: number
  to: number
}
export interface FileRef {
  path: string
  items: FileRefItem[]
}

export interface SseEvent {
  type: string
  ts: number
  iter: number
  forkId?: string
  data?: any
}

/* fork 分身摘要（history 响应；后端 hooks.ForkSummary） */
export interface ForkSummary {
  id: string
  task: string
  answer?: string
  stopReason?: string
  iterations: number
}

/* agent_status 状态栏载荷（后端 hooks.StatusData 的 JSON 形状，SSE snapshot 用；
   注入消息历史的正文是中文语义化文本，历史重建走关键词识别；
   资源变更不在快照里——走 res.change 事件 + <res_change> 消息单一来源） */
export interface StatusPayload {
  now: string
  sinceLastOutputMin: number
  ctxTokens: number
  ctxWindow: number
  suggestCompact: boolean
}

export interface AppEntry {
  name: string
  title: string
  kind: string
  mtime: string
}

export interface McpServerView {
  name: string
  description: string
  transport: 'http' | 'stdio' | string
  endpoint: string
  enabled: boolean
  connected: boolean
  tools: number
  headers: Record<string, string>
  allow?: string[]
}

export interface McpToolView {
  name: string
  description: string
  args_schema?: any
}

export interface McpFile {
  servers: {
    name: string
    description?: string
    type: string
    url?: string
    headers?: Record<string, string>
    args?: string[]
    allow?: string[]
    enabled?: boolean
  }[]
}

export interface PathEntry {
  label: string
  file: string
  path: string
}

export interface AppConfig {
  port: number
  listen: string
  dataDir: string
  boot: number
  paths: PathEntry[]
  memory: { longterm: string; skills: string; topics: string }
}

export interface Settings {
  systemExtra: string
  trimPercent?: number | null
  workDir?: string
  closeToTray?: boolean
  maxIterations?: number // 单轮最大模型迭代次数（0/空 = 默认 12）
}

export interface ModelEntry {
  name: string
  baseUrl: string
  apiKey: string
  headers?: Record<string, string>
  enabled: boolean
  vision?: boolean // 支持多模态视觉输入；false 时带图请求自动省略图片
  inTokens?: number // 累计输入 tokens（含缓存命中）
  outTokens?: number // 累计输出 tokens
  cacheTokens?: number // 累计缓存命中 tokens（输入子集）
  cost?: number
  contextWindow?: number
  protocol?: string // '' | 'openai'（默认）| 'responses' | 'anthropic'
}

export interface ModelsConfig {
  main: ModelEntry[]
  vision: ModelEntry[]
  image: ModelEntry[]
  audio: ModelEntry[]
}

export interface Status {
  model: string
  modelVision: boolean
  sessionId: string
  sessionMsgs: number
  busy: boolean
  contextTokens: number
  contextWindow: number
  trimPercent: number
  cacheHitRate: number
  promptTokens: number
  turns: number
  tools: string[]
  mcpServers: string[]
  skills: string[]
  topicsCount: number
}

/* fork 线的分叉源展示元数据（后端 hooks.ForkOrigin） */
export interface ForkOrigin {
  sourceId?: string
  title?: string
  anchor?: number
}

/* 分支（线）索引条目：id=线根 session ID（稳定），leafId=当前叶 */
export interface TopicEntry {
  id: string
  leafId?: string
  title: string
  summary?: string
  createdAt: number
  updatedAt?: number
  msgs: number
  kind?: 'new' | 'fork' | string
  origin?: ForkOrigin
}

/* 全分支未决人机请求（GET /api/notifications，通知栏轮询数据源） */
export interface NotificationGroup {
  rootId: string
  items: { callId: string; forkId?: string; kind: 'approve' | 'ask'; tool: string; args?: string; ts: number }[]
}

/* 分支面板条目（GET /api/topics 响应，含运行态合成） */
export interface BranchView extends TopicEntry {
  running?: boolean
  waiting?: boolean
  archiving?: boolean
  active?: boolean
}

export interface Usage {
  PromptTokens: number
  CompletionTokens: number
  CachedTokens: number
}

export interface Bootstrap {
  sessionId: string // = 分支根 ID（前端路由/SSE 订阅键，compact 换代不变）
  leafId?: string
  branches?: BranchView[]
  settings: Settings
  status: Status
  memoryExists: boolean
}

const json = async <T>(res: Response): Promise<T> => {
  if (!res.ok) {
    const body = await res.text().catch(() => '')
    throw new Error(`${res.status}: ${body || res.statusText}`)
  }
  return res.json()
}

const post = <T>(url: string, body?: unknown) =>
  fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  }).then(json<T>)

export interface MemoryFileInfo {
  name: string
  size: number
  mtime: string
}
export interface MemorySkillEntry {
  id: string
  name: string
  desc: string
  enabled: boolean
}
export interface MemoryConfig {
  longterm: { dir: string; harnessMd: MemoryFileInfo | null; files: MemoryFileInfo[] }
  skills: { dir: string; items: MemorySkillEntry[] }
  topics: { dir: string }
}

/* 会话树节点（GET /api/memory/tree）：记忆页整树渲染 */
export interface SessionNode {
  id: string
  title: string
  seedKind?: 'new' | 'fork' | 'compress' | string
  archived: boolean
  msgs: number
  createdAt: number
  targetId?: string // 向上边（组树用）
  forkedFrom?: ForkOrigin
  lineRoot?: string
  isLeaf: boolean // 所属线的当前叶（可切换进入）
  isActiveLine: boolean
}

export type ApproveLevel = 'ask' | 'black' | 'white' | 'auto'
export interface ToolRule {
  tool: string
  level: ApproveLevel
  list: string[]
}

export const api = {
  bootstrap: () => fetch('/api/bootstrap').then(json<Bootstrap>),

  status: () => fetch('/api/status').then(json<Status>),

  getHistory: (id: string) =>
    fetch(`/api/sessions/${id}`).then(
      json<{
        id: string
        rootId?: string
        busy: boolean
        messages: HistoryMessage[]
        targetId?: string
        seedKind?: 'new' | 'fork' | 'compress' | string
        canPrev?: boolean
        forkedFrom?: ForkOrigin
        decisions?: DecisionRecord[]
        forks?: ForkSummary[]
      }>,
    ),

  /* fork 分身详情（抽屉懒加载）：增量消息 + 主库决策记录 */
  getFork: (id: string, fid: string) =>
    fetch(`/api/sessions/${id}/forks/${fid}`).then(
      json<{ id: string; messages: HistoryMessage[]; decisions?: DecisionRecord[] }>,
    ),

  /* compact 链上一会话（懒加载）；无上级返回 null */
  getPrev: (id: string) =>
    fetch(`/api/sessions/${id}/prev`).then(async (r) =>
      r.status === 204 ? null : ((await r.json()) as {
        id: string
        title?: string
        summary?: string
        messages: HistoryMessage[]
        forks?: ForkSummary[]
        prevSession?: string
      }),
    ),

  /* 发送消息：附件/引用统一为路径（<reference_file> 记录告知模型） */
  send: (id: string, text: string, files?: FilePayload[], refs?: FileRef[]) =>
    post<{ ok: boolean }>(`/api/sessions/${id}/messages`, { text, files, refs }),
  /* 附件暂存（拖入即落盘 tmp/ 拿真身路径）：multipart 多文件一次上 */
  stash: async (files: File[]): Promise<string[]> => {
    const form = new FormData()
    for (const f of files) form.append('file', f, f.name)
    const r = await fetch('/api/workspace/stash', { method: 'POST', body: form })
    if (!r.ok) throw new Error((await r.json().catch(() => null) as { error?: string } | null)?.error || r.statusText)
    return ((await r.json()) as { files?: string[] }).files ?? []
  },
  /* 二进制写回（画板图片原地保存：一切皆资源，编辑即写回真身） */
  saveBin: async (path: string, file: File): Promise<void> => {
    const form = new FormData()
    form.append('path', path)
    form.append('file', file, file.name)
    const r = await fetch('/api/workspace/save-bin', { method: 'POST', body: form })
    if (!r.ok) throw new Error((await r.json().catch(() => null) as { error?: string } | null)?.error || r.statusText)
  },

  cancel: (id: string) => post<{ ok: boolean }>(`/api/sessions/${id}/cancel`),

  decideApprove: (id: string, callId: string, approve: boolean, reason: string) =>
    post<{ ok: boolean }>(`/api/sessions/${id}/decisions/approve`, { callId, approve, reason }),

  decideAnswer: (id: string, callId: string, input: string) =>
    post<{ ok: boolean }>(`/api/sessions/${id}/decisions/answer`, { callId, input }),

  listNotifications: () => fetch('/api/notifications').then(json<NotificationGroup[]>),

  summarize: (id: string) => post<{ text: string }>(`/api/sessions/${id}/summary`),

  getSettings: () => fetch('/api/settings').then(json<Settings>),

  saveSettings: (s: Settings) => post<{ ok: boolean }>('/api/settings', s),

  getSecurity: () => fetch('/api/security').then(json<{ rules: ToolRule[] }>),

  getApps: () => fetch('/api/apps').then(json<{ apps: AppEntry[] }>),

  /* 桌面壳为快应用开独立子窗口（浏览器访问 503，调用方回落新标签页） */
  openApp: (name: string) => post<{ ok: boolean }>('/api/apps/open', { name }),

  /* 工作目录文本文件保存（file:// 编辑器）；沙箱同 /api/workspace/file */
  saveFile: (path: string, content: string) =>
    post<{ ok: boolean }>('/api/workspace/save', { path, content }),

  getMcp: () => fetch('/api/mcp').then(json<{ servers: McpServerView[] }>),

  saveMcp: (f: McpFile) => post<{ ok: boolean }>('/api/mcp', f),

  connectMcp: (name: string) => post<{ tools: McpToolView[] }>('/api/mcp/connect', { name }),

  disconnectMcp: (name: string) => post<{ ok: boolean }>('/api/mcp/disconnect', { name }),

  callMcp: (server: string, tool: string, args: unknown) =>
    post<{ result: string }>('/api/mcp/call', { server, tool, args }),

  getModels: () => fetch('/api/models').then(json<ModelsConfig>),

  saveModels: (m: ModelsConfig) => post<{ ok: boolean }>('/api/models', m),

  getMemoryConfig: () => fetch('/api/memory/config').then(json<MemoryConfig>),

  /* 技能管理：zip(base64) 上传新建（名称自动推导） / 删除目录 / 启停（即时生效于 load_skill 与状态面板） */
  createSkill: (data: string) => post<{ ok: boolean }>('/api/memory/skills', { data }),

  deleteSkill: (id: string) =>
    fetch(`/api/memory/skills/${encodeURIComponent(id)}`, { method: 'DELETE' }).then(json<{ ok: boolean }>),

  toggleSkill: (id: string, enabled: boolean) =>
    post<{ ok: boolean }>(`/api/memory/skills/${encodeURIComponent(id)}/enabled`, { enabled }),

  /* 完整会话树（全部世代与分叉，记忆页渲染） */
  getMemoryTree: () => fetch('/api/memory/tree').then(json<SessionNode[]>),

  /* 手动归档开关（活动/运行中的当前叶会被后端拒绝） */
  archiveSession: (id: string, archived: boolean) =>
    post<{ ok: boolean; archived: boolean }>(`/api/sessions/${id}/archive`, { archived }),

  deleteTopic: (id: string) =>
    fetch(`/api/topics/${id}`, { method: 'DELETE' }).then(json<{ ok: boolean }>),

  saveSecurity: (rules: ToolRule[]) => post<{ ok: boolean }>('/api/security', { rules }),

  getMemory: () => fetch('/api/memory').then(json<{ content: string }>),

  saveMemory: (content: string) => post<{ ok: boolean }>('/api/memory', { content }),

  listTopics: () => fetch('/api/topics').then(json<BranchView[]>),

  getTopic: (id: string) =>
    fetch(`/api/topics/${id}`).then(
      json<{ entry: TopicEntry; messages: HistoryMessage[]; summary?: string }>,
    ),

  resumeTopic: (id: string) => post<{ id: string }>(`/api/topics/${id}/resume`),

  /* 分支三操作：开新线 / 从源会话第 anchor 条消息（含）复制前缀分叉 / 切换分支 */
  newBranch: () => post<{ id: string }>('/api/branches/new'),

  /* 归档换代：指定分支总结归档开新篇（会话树的纵深操作，空闲时可用；rootId 空=活动） */
  compactTopic: (rootId?: string) => post<{ ok: boolean }>('/api/topics/compact', { rootId: rootId || '' }),

  forkSession: (sourceId: string, anchor: number) =>
    post<{ id: string }>(`/api/sessions/${sourceId}/fork`, { anchor }),

  activateBranch: (id: string) => post<{ id: string }>(`/api/branches/${id}/activate`),

  appConfig: () => fetch('/api/app/config').then(json<AppConfig>),

  appRestart: (req: { port?: number; listen?: string; dataDir?: string }) =>
    post<{ url: string; boot: number }>('/api/app/restart', req),

  appHealth: (base = '') => fetch(`${base}/api/app/health`).then(json<{ ok: boolean; boot: number }>),
}

/* subscribe 建立 SSE 订阅，返回断开函数。
window 级单连接：HMR/重复订阅先关旧连接，防泄漏挤占同域连接池。 */
export function subscribe(id: string, onEvent: (ev: SseEvent) => void): () => void {
  const w = window as unknown as { __ezSSE?: EventSource }
  w.__ezSSE?.close()
  const es = new EventSource(`/api/sessions/${id}/events`)
  w.__ezSSE = es
  es.onmessage = (m) => {
    try {
      onEvent(JSON.parse(m.data))
    } catch {
      /* 忽略坏帧 */
    }
  }
  return () => {
    es.close()
    if (w.__ezSSE === es) w.__ezSSE = undefined
  }
}
