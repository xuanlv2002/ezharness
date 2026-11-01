/* API 封装：REST + SSE。v0.2：bootstrap 模式（会话对用户隐藏）。 */
import { fetchEventSource } from '@microsoft/fetch-event-source'
import { getToken } from './auth'

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
   资源变更不在快照里——走 resource.change 事件 + <resource_change> 消息单一来源） */
export interface StatusPayload {
  now: string
  sinceLastOutputMin: number
  ctxTokens: number
  ctxWindow: number
  suggestCompact: boolean
}

/* 会话任务计划（后端 hooks.SessionPlan；plan 工具维护，输入框上方展示） */
export interface PlanProgress {
  title: string
  status: 'pending' | 'running' | 'completed'
}

export interface SessionPlan {
  goal: string
  context: string
  design: string
  progress: PlanProgress[]
  verify: string
  updatedAt?: string
}

export interface AppCallbackSpec {
  name: string
  title?: string
  description?: string
  method: string
  url: string
}

export interface AppEntry {
  name: string
  title: string
  kind: string
  mtime: string
  icon?: string // 卡片头像（同源 URL，未声明/越界时缺省）
  pinned?: boolean // 已钉到桌面小助手
  callbacks?: AppCallbackSpec[] // app.quick 声明的定时调用端点
}

/* 定时回调计划（后端 callback.Schedule 的 JSON 形状；本机时区） */
export interface CallbackSchedule {
  kind: 'once' | 'interval' | 'cron'
  at?: string // once：2026-11-01 09:00 / RFC3339
  every?: string // interval：Go duration，如 30m、2h
  cron?: string // cron：分 时 日 月 周
}

export interface CallbackSessionTarget {
  sessionId?: string // session 模式：创建任务时的叶 session ID（归档/compact 换代后失败）
  mode: 'session' | 'new'
  message: string
}

/* 创建/更新的会话目标：只有当前会话与新建会话两种，绑定 ID 由后端落值 */
export interface CallbackSessionInput {
  mode: 'session' | 'new'
  message: string
}

export interface CallbackAppTarget {
  name: string
  callback: string // app.quick callbacks[].name
}

export interface CallbackScriptTarget {
  command: string // 工作目录里执行的 shell 命令
}

/* 一次触发记录（后端 callback.Run，新的在前） */
export interface CallbackRun {
  at: number
  status: 'ok' | 'error' | 'skipped' | 'missed' | 'running' | string
  manual?: boolean
  target: string
  targetId?: string
  content?: string
  result?: string
  durationMs?: number
}

/* 定时回调任务（后端 callback.Task） */
export interface CallbackTask {
  id: string
  title: string
  description?: string
  kind: 'session' | 'app' | 'script'
  enabled: boolean
  schedule: CallbackSchedule
  session?: CallbackSessionTarget
  app?: CallbackAppTarget
  script?: CallbackScriptTarget
  createdAt: number
  updatedAt: number
  nextRunAt?: number
  lastRunAt?: number
  lastStatus?: 'ok' | 'error' | 'skipped' | 'missed' | 'running' | string
  lastResult?: string
  runCount?: number
  runs?: CallbackRun[]
}

/* 创建/更新的配置面（运行态由后端维护） */
export interface CallbackSpec {
  title: string
  description?: string
  kind: 'session' | 'app' | 'script'
  enabled?: boolean
  schedule: CallbackSchedule
  session?: CallbackSessionInput
  app?: CallbackAppTarget
  script?: CallbackScriptTarget
}

/* 状态卡用的当前会话任务摘要 */
export interface CallbackBrief {
  id: string
  title: string
  kind: string
  schedule: string
  enabled: boolean
  nextRunAt?: number
  lastStatus?: string
}

export interface McpOAuthConfig {
  enabled: boolean
  client_id?: string
  client_secret?: string
  scopes?: string[]
  metadata_url?: string
}

export interface McpServerView {
  name: string
  description: string
  transport: 'http' | 'sse' | 'stdio' | string
  endpoint: string
  command: string
  args?: string[]
  env?: Record<string, string>
  enabled: boolean
  connected: boolean
  tools: number // 页面拉取过的工具数;-1 = 会话在但未拉取,0 = 确实无工具
  headers: Record<string, string>
  allow?: string[]
  oauth: boolean // 该 server 配置了 OAuth
  authorized: boolean // OAuth 令牌有效
  oauth_config?: McpOAuthConfig // 原始配置（编辑/回存用）
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
    command?: string
    args?: string[]
    env?: Record<string, string>
    allow?: string[]
    enabled?: boolean
    oauth?: McpOAuthConfig
  }[]
}

/* McpAuthError 表示 MCP 调用需要 OAuth 认证（后端 401 + needs_auth）。 */
export class McpAuthError extends Error {
  server: string
  authUrl: string
  metadataUrl: string
  constructor(server: string, authUrl: string, metadataUrl = '') {
    super(`需要 OAuth 认证：${server}`)
    this.name = 'McpAuthError'
    this.server = server
    this.authUrl = authUrl
    this.metadataUrl = metadataUrl
  }
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

/* ReasoningEffort 是思考档位（Claude Code / Codex 分档；请求侧归一为 low/high/max）。 */
export type ReasoningEffort = '' | 'none' | 'minimal' | 'low' | 'medium' | 'high' | 'xhigh' | 'max' | 'ultra'

export interface Settings {
  systemExtra: string
  trimPercent?: number | null
  workDir?: string
  closeToTray?: boolean
  petEnabled?: boolean // 桌面悬浮球（desktop 壳启动时读取；关=不建窗，右键/设置页可切换）
  maxIterations?: number // 单轮最大模型迭代次数（0/空 = 默认 64，上限 128）
  offloadThreshold?: number // 工具结果卸载阈值（字节，0 = 默认 4096；超过即卸载到文件只留摘要）
  debugMode?: boolean // 调试模式：模型完整输入写进日志（即改即生效）
  reasoningEffort?: ReasoningEffort // 思考档位（''=默认不控制；none=关闭；其余为强度分档）
  approveMode?: '' | 'off' | 'strict' // 审批档位（''=跟随安全配置；off=全放行；strict=全审）
}

export interface ModelEntry {
  name: string
  baseUrl: string
  apiKey: string
  headers?: Record<string, string>
  enabled: boolean
  disabled?: boolean // 关闭：不可被 model_call/task 选用,也不进可用模型清单
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
  image: ModelEntry[]
  audio: ModelEntry[]
}

export interface HourStats {
  start: number // 小时起点 UnixMilli
  turns: number
  in: number
  out: number
  cache: number
  models?: Record<string, { in: number; out: number; cache: number; turns: number }>
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
  callbacks?: CallbackBrief[] // 绑定当前会话的定时回调
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

/* 全分支未决人机请求 + 各会话最近一轮完成通知（GET /api/notifications，
   通知栏轮询数据源；kind=info 是完成通知，非待处理） */
export interface NotificationGroup {
  rootId: string
  items: { callId: string; forkId?: string; kind: 'approve' | 'ask' | 'plan' | 'info' | 'mcp_auth' | 'memory'; tool: string; args?: string; ts: number }[]
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
    let msg = body || res.statusText
    try {
      msg = JSON.parse(body).error ?? msg // 后端失败统一 {"error": "…"}，取一句话
    } catch {
      /* 非 JSON 体原样 */
    }
    throw new Error(msg)
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
  builtin?: boolean
}
/* 技能详情：清单字段 + SKILL.md 正文（已剥离 frontmatter） */
export interface MemorySkillContent extends MemorySkillEntry {
  content: string
  path: string
}
export interface MemoryConfig {
  skills: { dir: string; items: MemorySkillEntry[] }
  topics: { dir: string }
}

/* 条目式长期记忆（user/soul 条目 + 项目索引） */
export interface MemoryItem {
  id: string
  text: string
  updatedAt: string
}
export interface MemoryProjectEntry {
  id: string
  name: string
  description: string
}
export interface LongtermMemory {
  user: MemoryItem[]
  soul: MemoryItem[]
  projects: MemoryProjectEntry[]
}
export interface MemoryProjectDetail extends MemoryProjectEntry {
  content: string
}

/* 会话树节点（GET /api/memory/tree）：记忆页按层渲染 */
export interface SessionNode {
  id: string
  title: string
  seedKind?: 'new' | 'fork' | 'compress' | string
  archived: boolean
  msgs: number
  createdAt: number
  targetId?: string // 向上边（压缩链游标）
  parentId?: string // 展示父节点（空 = 顶层分支）
  forkedFrom?: ForkOrigin
  lineRoot?: string
  isLeaf: boolean // 所属线的当前叶（可切换进入）
  isActiveLine: boolean
  childCount: number // 直接子代数（0 = 无展开箭头）
}

/* 会话树的一层：根层按分支分页，子层一次给全 */
export interface SessionTreePage {
  nodes: SessionNode[]
  next?: string // 根层下一页游标（空 = 已到底）
  total?: number // 根层分支总数
}

/* 调用链 span（GET /api/topics/:id/trace = trace.jsonl 原文） */
export interface TraceSpan {
  traceId: string
  spanId: string
  parentId?: string
  name: string
  kind: 'turn' | 'model' | 'tool' | 'fork' | 'compact' | string
  start: number
  end?: number
  durMs?: number
  iteration?: number
  forkId?: string
  attrs?: Record<string, unknown>
}

export type ApproveLevel = 'ask' | 'black' | 'white' | 'auto'
export interface ToolRule {
  key: string
  level: ApproveLevel
  list: string[]
}

export const api = {
  bootstrap: () => fetch('/api/bootstrap').then(json<Bootstrap>),

  status: () => fetch('/api/status').then(json<Status>),

  /* tail = 只取尾部 N 条消息（锚点回填等只要尾段的场景）；start = 截取偏移 */
  getHistory: (id: string, tail?: number) =>
    fetch(`/api/sessions/${id}${tail ? `?tail=${tail}` : ''}`).then(
      json<{
        id: string
        rootId?: string
        busy: boolean
        messages: HistoryMessage[]
        start?: number
        targetId?: string
        seedKind?: 'new' | 'fork' | 'compress' | string
        canPrev?: boolean
        forkedFrom?: ForkOrigin
        decisions?: DecisionRecord[]
        forks?: ForkSummary[]
        plan?: SessionPlan | null
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
  send: (id: string, text: string, files?: FilePayload[], fileRefs?: FileRef[]) =>
    post<{ ok: boolean }>(`/api/sessions/${id}/messages`, { text, files, refs: fileRefs }),
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

  /* 计划确认：confirm 放行落盘；feedback 带意见（走工具结果回模型） */
  decidePlan: (id: string, callId: string, action: 'confirm' | 'feedback', feedback: string) =>
    post<{ ok: boolean }>(`/api/sessions/${id}/decisions/plan`, { callId, action, feedback }),

  decideMcpAuth: (id: string, callId: string, ok: boolean, reason: string) =>
    post<{ ok: boolean }>(`/api/sessions/${id}/decisions/mcp-auth`, { callId, ok, reason }),

  listNotifications: () => fetch('/api/notifications').then(json<NotificationGroup[]>),

  /* 关闭完成通知（info 条）：服务端置空该会话的完成记录 */
  dismissNotification: (id: string, callId: string) =>
    post<{ ok: boolean }>(`/api/sessions/${id}/notifications/dismiss`, { callId }),

  getSettings: () => fetch('/api/settings').then(json<Settings>),

  saveSettings: (s: Settings) => post<{ ok: boolean }>('/api/settings', s),

  getSecurity: () => fetch('/api/security').then(json<{ rules: ToolRule[] }>),

  getApps: () => fetch('/api/apps').then(json<{ apps: AppEntry[] }>),

  /* 版本检查：后端带超时代查 GitHub 最新 release，是否可升级由前端比对本地版本 */
  checkUpdate: () => fetch('/api/app/update-check').then(json<{ latest: string; url: string }>),

  /* 启动快应用（chip 的 id 来自模型输出不可信，名单校验在后端）：返回前端
     入口路径、标题与后端终端 id；开窗由调用方执行：desktop 壳 IPC 子窗口 /
     web 新标签页 */
  openApp: (name: string) =>
    post<{ ok: boolean; path: string; title: string; termId?: string }>('/api/apps/open', { name }),

  /* 钉到桌面小助手/取消（toggle），返回新状态 */
  pinApp: (name: string) =>
    post<{ ok: boolean; pinned: boolean }>(`/api/apps/${encodeURIComponent(name)}/pin`, {}),

  /* 卸载快应用：后端停掉运行中的后端终端、删 apps/<名>/ 目录、清钉选残留 */
  uninstallApp: (name: string) =>
    fetch(`/api/apps/${encodeURIComponent(name)}`, { method: 'DELETE' }).then(json<{ ok: boolean }>),

  /* 定时回调中心：sessionId 过滤当前会话任务，app 过滤快应用关联任务 */
  listCallbacks: (sessionId?: string, app?: string) => {
    const q = new URLSearchParams()
    if (sessionId) q.set('sessionId', sessionId)
    if (app) q.set('app', app)
    const suffix = q.toString() ? `?${q}` : ''
    return fetch(`/api/callbacks${suffix}`).then(json<{ callbacks: CallbackTask[] }>)
  },

  createCallback: (c: CallbackSpec) => post<{ callback: CallbackTask }>('/api/callbacks', c),

  updateCallback: (id: string, c: Partial<CallbackSpec>) =>
    post<{ callback: CallbackTask }>(`/api/callbacks/${encodeURIComponent(id)}`, c),

  deleteCallback: (id: string) =>
    fetch(`/api/callbacks/${encodeURIComponent(id)}`, { method: 'DELETE' }).then(json<{ ok: boolean }>),

  setCallbackEnabled: (id: string, enabled: boolean) =>
    post<{ callback: CallbackTask }>(`/api/callbacks/${encodeURIComponent(id)}/enabled`, { enabled }),

  runCallback: (id: string) =>
    post<{ ok: boolean }>(`/api/callbacks/${encodeURIComponent(id)}/run`, {}),

  /* 文本文件保存（file:// 编辑器）；按绝对路径写，与 /api/workspace/file 同源 */
  saveFile: (path: string, content: string) =>
    post<{ ok: boolean }>('/api/workspace/save', { path, content }),

  getMcp: () => fetch('/api/mcp').then(json<{ servers: McpServerView[] }>),

  saveMcp: (f: McpFile) => post<{ ok: boolean }>('/api/mcp', f),

  /* connectMcp 建立连接拉工具；需要 OAuth 时后端回 401 + needs_auth → McpAuthError */
  connectMcp: async (name: string) => {
    const res = await fetch('/api/mcp/connect', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name }),
    })
    if (res.status === 401) {
      const b = (await res.json().catch(() => ({}))) as { server?: string; auth_url?: string; metadata_url?: string }
      throw new McpAuthError(b.server || name, b.auth_url || '', b.metadata_url || '')
    }
    return json<{ tools: McpToolView[] }>(res)
  },

  disconnectMcp: (name: string) => post<{ ok: boolean }>('/api/mcp/disconnect', { name }),

  /* callMcp 调用 MCP 工具（OpenAPI 面）；需要 OAuth 时后端回 401 + needs_auth，
     这里抛出携带 server/auth_url/metadata_url 的 McpAuthError 供调用方引导认证。 */
  callMcp: async (server: string, tool: string, args: unknown) => {
    const res = await fetch('/api/open/mcp/call', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ server, tool, args }),
    })
    if (res.status === 401) {
      const b = (await res.json().catch(() => ({}))) as { server?: string; auth_url?: string; metadata_url?: string }
      throw new McpAuthError(b.server || server, b.auth_url || '', b.metadata_url || '')
    }
    return json<{ result: string }>(res)
  },

  /* MCP OAuth：取授权 URL（前端用系统浏览器打开）/ 探测是否需 OAuth / 轮询状态 / 取消 / 注销。
     start/status/cancel 在 OpenAPI 面（会话凭证同样可过），probe/logout 在会话面。 */
  oauthStart: (name: string, metadataUrl = '') =>
    post<{ url: string }>('/api/open/mcp/oauth/start', metadataUrl ? { name, metadata_url: metadataUrl } : { name }),

  oauthProbe: (name: string) => post<{ oauth: boolean }>('/api/mcp/oauth/probe', { name }),

  oauthStatus: (name: string) =>
    fetch(`/api/open/mcp/oauth/status?name=${encodeURIComponent(name)}`).then(json<{ authorized: boolean; pending: boolean }>),

  oauthCancel: (name: string) => post<{ ok: boolean }>('/api/open/mcp/oauth/cancel', { name }),

  oauthLogout: (name: string) => post<{ ok: boolean }>('/api/mcp/oauth/logout', { name }),

  getModels: () => fetch('/api/models').then(json<ModelsConfig>),

  /* 按小时用量序列（模型面板趋势图;近 30 天,升序） */
  getHourlyStats: () => fetch('/api/stats/hourly').then(json<{ hours: HourStats[] }>).then((r) => r.hours || []),

  saveModels: (m: ModelsConfig) => post<{ ok: boolean }>('/api/models', m),

  /* 激活主模型（输入框切换器）：唯一启用该条目并重建 agent，下一次输入生效 */
  activateModel: (name: string) =>
    post<{ ok: boolean; model: ModelEntry }>('/api/models/activate', { name }),

  /* 审批三档（全局；''=跟随安全配置 | off=无审批 | strict=严格审批），实时生效 */
  setApproveMode: (mode: '' | 'off' | 'strict') =>
    post<{ ok: boolean; mode: string }>('/api/settings/approve-mode', { mode }),

  /* 思考强度档位（全局；请求侧实时读取，即改即生效） */
  setReasoningEffort: (effort: ReasoningEffort) =>
    post<{ ok: boolean; effort: string }>('/api/settings/reasoning', { effort }),

  /* 直调大模型（OpenAPI 面，与 mcp/call 同级） */
  callModel: (prompt: string, model = '', images: string[] = []) =>
    post<{ result: string }>('/api/open/model/call', { model, prompt, images }),

  getMemoryConfig: () => fetch('/api/memory/config').then(json<MemoryConfig>),

  /* 技能管理：zip(base64) 上传新建（名称自动推导） / 删除目录 / 启停（即时生效于 load_skill 与状态面板） */
  createSkill: (data: string) => post<{ ok: boolean }>('/api/memory/skills', { data }),

  getSkill: (id: string) =>
    fetch(`/api/memory/skills/${encodeURIComponent(id)}`).then(json<MemorySkillContent>),

  deleteSkill: (id: string) =>
    fetch(`/api/memory/skills/${encodeURIComponent(id)}`, { method: 'DELETE' }).then(json<{ ok: boolean }>),

  toggleSkill: (id: string, enabled: boolean) =>
    post<{ ok: boolean }>(`/api/memory/skills/${encodeURIComponent(id)}/enabled`, { enabled }),

  /* 会话树的一层：parent 空 = 顶层分支（before 翻页），否则该节点的直接子代 */
  getMemoryTree: (parent = '', before = '', limit = 0) => {
    const q = new URLSearchParams()
    if (parent) q.set('parent', parent)
    if (before) q.set('before', before)
    if (limit > 0) q.set('limit', String(limit))
    const qs = q.toString()
    return fetch(`/api/memory/tree${qs ? `?${qs}` : ''}`).then(json<SessionTreePage>)
  },

  /* 手动归档开关（活动/运行中的当前叶会被后端拒绝） */
  archiveSession: (id: string, archived: boolean) =>
    post<{ ok: boolean; archived: boolean }>(`/api/sessions/${id}/archive`, { archived }),

  deleteTopic: (id: string) =>
    fetch(`/api/topics/${id}`, { method: 'DELETE' }).then(json<{ ok: boolean }>),

  saveSecurity: (rules: ToolRule[]) => post<{ ok: boolean }>('/api/security', { rules }),

  /* 长期记忆（条目式，与 memory 工具同层）：全量 / 条目整组保存 / 项目详情与增删 */
  getLongterm: () => fetch('/api/memory/longterm').then(json<LongtermMemory>),

  saveLongtermItems: (target: 'user' | 'soul', items: MemoryItem[]) =>
    post<{ ok: boolean; items: MemoryItem[] }>(`/api/memory/longterm/${target}`, { items }),

  getMemoryProject: (id: string) =>
    fetch(`/api/memory/project/${encodeURIComponent(id)}`).then(json<MemoryProjectDetail>),

  saveMemoryProject: (p: { id?: string; name: string; description: string; content: string }) =>
    post<{ ok: boolean; id: string }>('/api/memory/project', p),

  deleteMemoryProject: (id: string) =>
    fetch(`/api/memory/project/${encodeURIComponent(id)}`, { method: 'DELETE' }).then(json<{ ok: boolean }>),

  /* 待确认记忆操作：确认应用 / 取消丢弃（通知栏按钮） */
  decideMemoryPending: (id: string, accept: boolean) =>
    post<{ ok: boolean; result: string }>(`/api/memory/pending/${encodeURIComponent(id)}/decide`, { accept }),

  listTopics: () => fetch('/api/topics').then(json<BranchView[]>),

  getTopic: (id: string) =>
    fetch(`/api/topics/${id}`).then(
      json<{ entry: TopicEntry; messages: HistoryMessage[]; summary?: string; systemPrompt?: string }>,
    ),

  /* 回顾调用链：该会话的 otel span 列表（无 trace.jsonl 返回空数组） */
  getTopicTrace: (id: string) => fetch(`/api/topics/${id}/trace`).then(json<TraceSpan[]>),

  /* 分支三操作：开新线 / 从源会话第 anchor 条消息（含）复制前缀分叉 / 切换分支 */
  newBranch: () => post<{ id: string }>('/api/branches/new'),

  /* 归档换代：指定分支总结归档开新篇（会话树的纵深操作，空闲时可用；rootId 空=活动） */
  compactTopic: (rootId?: string) => post<{ ok: boolean }>('/api/topics/compact', { rootId: rootId || '' }),

  forkSession: (sourceId: string, anchor: number) =>
    post<{ id: string }>(`/api/sessions/${sourceId}/fork`, { anchor }),

  activateBranch: (id: string) => post<{ id: string }>(`/api/branches/${id}/activate`),

  appConfig: () => fetch('/api/app/config').then(json<AppConfig>),

  appToken: () => fetch('/api/app/token').then(json<{ token: string }>),

  /* OpenAPI 令牌：脚本/快应用/外部程序调 /api/open/* 用的持久令牌（设置页展示/重置） */
  openapiToken: () => fetch('/api/app/openapi-token').then(json<{ token: string }>),

  resetOpenapiToken: () => post<{ token: string }>('/api/app/openapi-token/reset', {}),

  appRestart: (req: { port?: number; listen?: string; dataDir?: string }) =>
    post<{ url: string; boot: number }>('/api/app/restart', req),

  appHealth: (base = '') => fetch(`${base}/api/app/health`).then(json<{ ok: boolean; boot: number }>),
}

/* SSE 致命错误（鉴权失败/响应类型不符）：不重试，避免用同一个坏令牌刷请求 */
class SseFatalError extends Error {}

/* subscribe 建立 SSE 订阅，返回断开函数。
window 级单连接：HMR/重复订阅先关旧连接，防泄漏挤占同域连接池。
用 fetch-event-source 而不是原生 EventSource：EventSource 不能设置请求头，
过去只能把 token 拼进 URL；现在走 X-EZ-Token 头。 */
export function subscribe(id: string, onEvent: (ev: SseEvent) => void): () => void {
  const w = window as unknown as { __ezSSE?: AbortController }
  w.__ezSSE?.abort()
  const ac = new AbortController()
  w.__ezSSE = ac
  const url = `/api/sessions/${id}/events`
  const headers = { 'X-EZ-Token': getToken() }

  const connect = () => {
    void fetchEventSource(url, {
      headers,
      signal: ac.signal,
      /* 原生 EventSource 切后台不断连；库默认会在 document.hidden 时断开，
         这里保持常连，避免 agent 运行中丢事件 */
      openWhenHidden: true,
      onopen(res) {
        const ct = res.headers.get('content-type') || ''
        if (!res.ok || !ct.startsWith('text/event-stream')) {
          throw new SseFatalError(`SSE ${res.status} ${ct}`)
        }
        return Promise.resolve()
      },
      onmessage(m) {
        try {
          onEvent(JSON.parse(m.data))
        } catch {
          /* 忽略坏帧 */
        }
      },
      onclose() {
        /* 服务端正常关流（重启/换代）：稍后重连，保持原生 EventSource 语义 */
        if (!ac.signal.aborted) window.setTimeout(connect, 1000)
      },
      onerror(err) {
        /* 网络类错误交给库重连；鉴权/协议错误直接终止 */
        if (err instanceof SseFatalError) throw err
        return 1000
      },
    }).catch((err) => {
      if (!ac.signal.aborted) console.warn('[sse] 订阅终止', err)
    })
  }
  connect()
  return () => {
    ac.abort()
    if (w.__ezSSE === ac) w.__ezSSE = undefined
  }
}
