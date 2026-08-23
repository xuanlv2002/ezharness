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
}

export interface SseEvent {
  type: string
  ts: number
  iter: number
  forkId?: string
  data?: any
}

/* agent_status 状态栏载荷（后端 hooks.StatusData 的 JSON 形状） */
export interface StatusPayload {
  now: string
  sinceLastOutputMin: number
  ctxTokens: number
  ctxWindow: number
  suggestCompact: boolean
  changes?: string[]
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
  dataDir: string
  boot: number
  paths: PathEntry[]
  memory: { longterm: string; skills: string; topics: string }
}

export interface Settings {
  systemExtra: string
  compactThreshold?: number | null
}

export interface ModelEntry {
  name: string
  baseUrl: string
  apiKey: string
  headers?: Record<string, string>
  enabled: boolean
  tokens: number
  cost: number
}

export interface ModelsConfig {
  main: ModelEntry[]
  vision: ModelEntry[]
  image: ModelEntry[]
  audio: ModelEntry[]
}

export interface Status {
  model: string
  sessionId: string
  sessionMsgs: number
  busy: boolean
  contextTokens: number
  daysServed: number
  cacheHitRate: number
  totalTokens: number
  turns: number
  tools: string[]
  mcpServers: string[]
  topicsCount: number
}

export interface TopicEntry {
  id: string
  title: string
  summary: string
  createdAt: number
  msgs: number
}

export interface Usage {
  PromptTokens: number
  CompletionTokens: number
  CachedTokens: number
}

export interface Bootstrap {
  sessionId: string
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
export interface MemoryTopicEntry {
  id: string
  title: string
  summary: string
  createdAt: number
  msgs: number
  path?: string
  kind?: string
}
export interface MemoryConfig {
  longterm: { dir: string; harnessMd: MemoryFileInfo | null; files: MemoryFileInfo[] }
  skills: { dir: string; items: MemorySkillEntry[] }
  topics: { dir: string; items: MemoryTopicEntry[] }
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
        busy: boolean
        messages: HistoryMessage[]
        prevSession?: string
        prevTitle?: string
        decisions?: DecisionRecord[]
      }>,
    ),

  /* compact 链上一会话（懒加载）；无上级返回 null */
  getPrev: (id: string) =>
    fetch(`/api/sessions/${id}/prev`).then(async (r) =>
      r.status === 204 ? null : ((await r.json()) as {
        id: string
        title?: string
        summary?: string
        messages: HistoryMessage[]
        prevSession?: string
      }),
    ),

  send: (id: string, text: string) => post<{ ok: boolean }>(`/api/sessions/${id}/messages`, { text }),

  cancel: (id: string) => post<{ ok: boolean }>(`/api/sessions/${id}/cancel`),

  decideApprove: (id: string, callId: string, approve: boolean, reason: string) =>
    post<{ ok: boolean }>(`/api/sessions/${id}/decisions/approve`, { callId, approve, reason }),

  decideAnswer: (id: string, callId: string, input: string) =>
    post<{ ok: boolean }>(`/api/sessions/${id}/decisions/answer`, { callId, input }),

  decidePlan: (
    id: string,
    callId: string,
    kind: 'execute' | 'reject' | 'revise',
    input: string,
  ) => post<{ ok: boolean }>(`/api/sessions/${id}/decisions/plan`, { callId, kind, input }),

  summarize: (id: string) => post<{ text: string }>(`/api/sessions/${id}/summary`),

  getSettings: () => fetch('/api/settings').then(json<Settings>),

  saveSettings: (s: Settings) => post<{ ok: boolean }>('/api/settings', s),

  getSecurity: () => fetch('/api/security').then(json<{ rules: ToolRule[] }>),

  getApps: () => fetch('/api/apps').then(json<{ apps: AppEntry[] }>),

  getMcp: () => fetch('/api/mcp').then(json<{ servers: McpServerView[] }>),

  saveMcp: (f: McpFile) => post<{ ok: boolean }>('/api/mcp', f),

  connectMcp: (name: string) => post<{ tools: McpToolView[] }>('/api/mcp/connect', { name }),

  disconnectMcp: (name: string) => post<{ ok: boolean }>('/api/mcp/disconnect', { name }),

  callMcp: (server: string, tool: string, args: unknown) =>
    post<{ result: string }>('/api/mcp/call', { server, tool, args }),

  getModels: () => fetch('/api/models').then(json<ModelsConfig>),

  saveModels: (m: ModelsConfig) => post<{ ok: boolean }>('/api/models', m),

  getMemoryConfig: () => fetch('/api/memory/config').then(json<MemoryConfig>),

  deleteTopic: (id: string) =>
    fetch(`/api/topics/${id}`, { method: 'DELETE' }).then(json<{ ok: boolean }>),

  saveSecurity: (rules: ToolRule[]) => post<{ ok: boolean }>('/api/security', { rules }),

  getMemory: () => fetch('/api/memory').then(json<{ content: string }>),

  saveMemory: (content: string) => post<{ ok: boolean }>('/api/memory', { content }),

  listTopics: () => fetch('/api/topics').then(json<TopicEntry[]>),

  getTopic: (id: string) =>
    fetch(`/api/topics/${id}`).then(json<{ id: string; messages: HistoryMessage[] }>),

  resumeTopic: (id: string) =>
    post<{ id: string; messages: HistoryMessage[] }>(`/api/topics/${id}/resume`),

  appConfig: () => fetch('/api/app/config').then(json<AppConfig>),

  appRestart: (req: { port?: number; dataDir?: string }) =>
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
