/* API 封装：REST + SSE。v0.2：bootstrap 模式（会话对用户隐藏）。 */

export interface HistoryMessage {
  role: string
  content: string
  tool_call_id?: string
  tool_calls?: { ID: string; Name: string; Args: string }[]
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

export interface Settings {
  apiKey: string
  model: string
  baseUrl: string
  systemExtra: string
  rotateThreshold: number
  shell?: string
}

export interface Status {
  model: string
  sessionId: string
  sessionMsgs: number
  busy: boolean
  contextTokens: number
  rotateThreshold: number
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
      json<{ id: string; busy: boolean; messages: HistoryMessage[] }>,
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

  saveSecurity: (rules: ToolRule[]) => post<{ ok: boolean }>('/api/security', { rules }),

  getMemory: () => fetch('/api/memory').then(json<{ content: string }>),

  saveMemory: (content: string) => post<{ ok: boolean }>('/api/memory', { content }),

  listTopics: () => fetch('/api/topics').then(json<TopicEntry[]>),

  getTopic: (id: string) =>
    fetch(`/api/topics/${id}`).then(json<{ id: string; messages: HistoryMessage[] }>),

  resumeTopic: (id: string) =>
    post<{ id: string; messages: HistoryMessage[] }>(`/api/topics/${id}/resume`),

  appConfig: () => fetch('/api/app/config').then(json<{ port: number; dataDir: string }>),

  appRestart: (req: { port?: number; dataDir?: string }) =>
    post<{ url: string; boot: number }>('/api/app/restart', req),

  appHealth: (base = '') => fetch(`${base}/api/app/health`).then(json<{ ok: boolean; boot: number }>),
}

/* subscribe 建立 SSE 订阅，返回断开函数。 */
export function subscribe(id: string, onEvent: (ev: SseEvent) => void): () => void {
  const es = new EventSource(`/api/sessions/${id}/events`)
  es.onmessage = (m) => {
    try {
      onEvent(JSON.parse(m.data))
    } catch {
      /* 忽略坏帧 */
    }
  }
  return () => es.close()
}
