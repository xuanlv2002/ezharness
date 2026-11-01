/* 全局状态机：bootstrap + SSE 事件归约（时间线块 / fork / 通知 / 生命体征），Svelte 5 runes。 */

import {
  api,
  subscribe,
  type BranchView,
  type DecisionRecord,
  type FilePayload,
  type FileRef,
  type ForkSummary,
  type HistoryMessage,
  type ImagePayload,
  type ModelEntry,
  type PlanProgress,
  type ReasoningEffort,
  type Settings,
  type SessionPlan,
  type SseEvent,
  type Status,
  type StatusPayload,
} from './api'
import { parseCallbackMessage, type CallbackMessageMeta } from './callbackMessage'
import { filePane } from './filePaneState'
import { openExternal } from './desktop'
import pkg from '../../package.json'

/* newerVersion：a 是否比 b 新（v 前缀可有可无，按点分段数值比较，缺段补 0）。
   本地版本 = pkg.version（与页脚同源）；设置页版本检查共用。 */
export function newerVersion(a: string, b: string): boolean {
  const parts = (s: string) =>
    s
      .trim()
      .replace(/^v/, '')
      .split('.')
      .map((n) => parseInt(n, 10) || 0)
  const x = parts(a)
  const y = parts(b)
  for (let i = 0; i < Math.max(x.length, y.length); i++) {
    const d = (x[i] ?? 0) - (y[i] ?? 0)
    if (d !== 0) return d > 0
  }
  return false
}

/* 输入框附件（一切皆资源）：path = 真身路径（引用，发送不复制）；
   空 = 拖入暂存中占位 */
export interface Attachment {
  name: string
  path?: string
}
export type { FileRef }

export interface ToolBlockData {
  id: string
  name: string
  args: string
  result: string
  err: string
  state: 'building' | 'running' | 'done' // building＝模型流式构造参数中
  live?: string // 执行中的实时输出尾部（terminal 的 tool.output 增量累积）
  decision?: string // 人机决策徽标（已批准/已拒绝…，刷新后由 decisions.jsonl 重建）
}

/* ForkState 是分身聊天框的数据模型：blocks 与主时间线同构，
实时事件归约与存档回放共用 buildBlocks。owner=所属会话 ID（fork 存档
在所属库的 forks/ 下，compact 链上的历代库分身懒加载按 owner 取）。 */
export interface ForkState {
  id: string
  owner: string
  task: string
  status: 'running' | 'done'
  blocks: Block[]
  answer: string
  stopReason: string
  loaded: boolean
}

export interface DecisionData {
  id: string
  dtype: 'approve' | 'ask' | 'plan' | 'mcp-auth'
  name: string
  args: string
  question: string
  options: string[]
  /* plan：待确认方案五段（只读展示；解析失败的兜底为空，由卡片提示） */
  goal: string
  context: string
  design: string
  progress: PlanProgress[]
  verify: string
  forkId: string
  resolved: boolean
  resolution: string
  busy?: boolean // mcp-auth：授权进行中（去授权已点，等待浏览器完成）
  err?: string // mcp-auth：最近一次授权错误（展示用，留在卡上供重试）
}

export interface NoticeData {
  id: string // 决策回传键（工具调用 ID）
  rootId: string // 所属分支根（跳转与决策端点路由键）
  kind: 'approve' | 'ask' | 'plan' | 'info' | 'callback' | 'mcp_auth' | 'memory'
  source: string // 'agent'、分支名或 fork 标识
  forkId: string // 非空＝分身请求：跳转打开分身抽屉而非主时间线
  title: string
  detail: string
  time: string
  status: 'pending' | 'done'
  resolution: string
  target: string // 跳转目标：decision-<id> / memory-view / callback-session:<root> / callback-task:<id>
  local?: boolean // 本地生成（非后端轮询来源）：refreshNotices 不覆盖、dismiss 不回传
}

/* 消息锚点（分叉定位）：owner=消息所属 session ID（leaf 或上翻出的旧世代），
   msgIdx=该会话 messages 数组下标；分叉复制 [0, msgIdx]（含选中消息） */
export type Block = { uid: number } & (
  | {
      kind: 'user'
      text: string
      images?: ImagePayload[]
      files?: { name: string; path?: string }[]
      /* 文件引用 chips（文件页「添加到对话」→<reference_file>）：count
         = 标注条数（历史重建不还原片段全文，chip 点击回跳文件页） */
      fileRefs?: { path: string; count: number }[]
      /* 定时回调消息：标签元数据（text 已去掉包装，只留任务正文） */
      callback?: CallbackMessageMeta
      owner?: string
      msgIdx?: number
    }
  | { kind: 'assistant'; text: string; reasoning: string; streaming: boolean; owner?: string; msgIdx?: number }
  /* hidden：task 调用一旦有分身入口卡，通用工具卡就让位（entry 卡是唯一呈现） */
  | ({ kind: 'tool' } & ToolBlockData & { hidden?: boolean })
  | { kind: 'fork'; forkId: string }
  | { kind: 'decision' } & DecisionData
  | { kind: 'note'; text: string }
  | { kind: 'status'; text: string; data: StatusPayload | null }
  | { kind: 'resourcechange'; items: string[] }
  | { kind: 'endtick'; icon: string; title: string }
  | { kind: 'imgload'; paths: string[]; notes?: string[]; images?: ImagePayload[] }
)

export interface TotalUsage {
  prompt: number
  completion: number
  cached: number
}

/* 工作区抽屉的工具页（互斥单选）：终端 / 资源查看 / 共享浏览器 */
export type DrawerTool = 'term' | 'file' | 'browser'

/* 路径取文件名（chips 展示用；支持 / 与 \ 两种分隔符） */
function baseName(p: string): string {
  const i = Math.max(p.lastIndexOf('/'), p.lastIndexOf('\\'))
  return i >= 0 ? p.slice(i + 1) : p
}

/* 空白画板底图（画板默认画布尺寸）——画笔钮新建草稿时先落盘用 */
function blankBoardPng(): Promise<File> {
  const c = document.createElement('canvas')
  c.width = 960
  c.height = 600
  const ctx = c.getContext('2d')
  if (ctx) {
    ctx.fillStyle = '#fff'
    ctx.fillRect(0, 0, c.width, c.height)
  }
  return new Promise((resolve, reject) => {
    c.toBlob((b) => {
      if (b) resolve(new File([b], '画板.png', { type: 'image/png' }))
      else reject(new Error('空白画板生成失败'))
    }, 'image/png')
  })
}

/* 解析 <agent_status> 的 JSON 载荷；中文语义化文本返回 null */
function parseStatus(content: string): StatusPayload | null {
  const open = '<agent_status>'
  const close = '</agent_status>'
  const i = content.indexOf(open)
  if (i < 0) return null
  const j = content.indexOf(close, i)
  if (j < 0) return null
  try {
    return JSON.parse(content.slice(i + open.length, j).trim())
  } catch {
    return null
  }
}

/* 解析 <end_reason> 字段拼一行收尾文案（与 turn_end 实时收尾同款格式，
   历史回放与实时两条路径的 endtick 文案一致；无结束原因字段返回空） */
function endReasonText(content: string): string {
  const body = content.match(/<end_reason>([\s\S]*?)<\/end_reason>/)?.[1] ?? ''
  const get = (k: string) => body.match(new RegExp(`${k}：\\s*(.+)`))?.[1]?.trim() ?? ''
  const reason = get('结束原因')
  if (!reason) return ''
  const iters = get('运行轮次')
  const dur = get('运行时长')
  const hm = get('结束时间').slice(11, 16) // YYYY-MM-DD HH:MM:SS → HH:MM
  const errd = get('错误详情')
  return `${reason}${errd ? `：${errd}` : ''} · ${iters || '?'} 轮${dur ? ` · ${dur}` : ''}${hm ? ` · ${hm}` : ''}`
}

/* 轮次收尾小图标（按结束原因语义选形，悬浮 title 显示详情） */
function endIcon(text: string): string {
  if (text.includes('正常') || text.includes('completed')) return '✓'
  if (text.includes('取消') || text.includes('手动停止') || text.includes('cancelled')) return '⏹'
  if (text.includes('迭代') || text.includes('max_iterations')) return '↻'
  if (text.includes('错误') || text.includes('中止') || text.includes('error')) return '⚠'
  return '·'
}

/* 提取 <context_trim> 摘要为一行（整理分割线文案；四节结构化摘要从【已完成】节起截取，完整进度在会话目录 progress.md） */
function trimText(content: string): string {
  const m = content.match(/【已完成】\s*([\s\S]*?)<\/context_trim>/)
  const summary = (m?.[1] ?? '').trim().replace(/\s+/g, ' ')
  const brief = summary.length > 160 ? summary.slice(0, 160) + '…' : summary
  return `上下文已整理：此前的对话折叠为摘要（完整进度见会话目录 progress.md）。${brief}`
}

class AppStore {
  /* activeId = 分支根 ID（稳定：compact 换代不变，SSE 订阅/路由键）；
     leafId = 当前叶 session ID（分身存档 owner、上翻游标起点） */
  activeId = $state('')
  leafId = $state('')
  /* 打开的会话标签（rootId 身份；关闭会停掉该线正在跑的一轮，会话本身
     留在列表）；空即空状态（activeId 同步为空）。drafts 是每标签输入框草稿 */
  tabs = $state<{ rootId: string }[]>([])
  drafts = $state<Record<string, string>>({})
  branches = $state<BranchView[]>([])
  blocks = $state<Block[]>([])
  forks = $state<Record<string, ForkState>>({})
  notices = $state<NoticeData[]>([])
  /* 本轮已处理的记忆待确认操作（op id → 结果）：通知栏与聊天记忆卡共用的确认入口，
     本地记录结果给卡片落定状态（后端返回后待确认项即消失） */
  memoryDecisions = $state<Record<string, 'accept' | 'cancel'>>({})
  jumpView = $state<'' | 'chat' | 'memory' | 'callbacks'>('') // 通知跳页请求（App 消费后清空）
  /* 定时回调通知要展开的任务（回调页消费后清空） */
  callbackFocus = $state('')
  jumpAnchor = $state('') // 顶栏搜索跳转目标（view:anchor，目标视图消费后清空）
  lastTool = $state('')
  busy = $state(false)
  archivingRootId = $state('') // 归档进行中的分支（线根 ID，空=无）：锁该分支输入与按钮
  lastStatus = $state('')
  tick = $state(0)
  /* 分身抽屉：当前打开的分身与待定位的决策卡（通知跳转用） */
  activeForkId = $state('')
  jumpDecision = $state('')
  /* 通知跳转主时间线锚点（ChatView effect 消费滚动后清空；tick 依赖供
  跨分支切换后块加载完成重试） */
  jumpMain = $state('')
  /* 待回流附件（查看器「添加到对话」/拖入暂存）：ChatView 消费进输入框
     附件（同路径替换去重，否则追加） */
  pendingAttachments = $state<Attachment[] | null>(null)
  /* 文件页「添加到对话」的待回流引用（路径+标注片段，行号已算好）；
     ChatView 消费进输入框引用 chips（同 path 替换去重） */
  pendingFileRef = $state<FileRef | null>(null)
  /* 快速开始示例的待填指令：输入框消费后清空并聚焦，不自动发送 */
  pendingPrompt = $state<string | null>(null)
  /* 图片写回后的缩略图版本（按路径 bump：同 URL 的 img 立即换 src，
     含历史 chips——引用同一资源，处处显示最新；跨会话由 HTTP 304 兜底） */
  imgVer = $state<Record<string, number>>({})
  /* 共享终端抽屉(独立于画板 overlay,与聊天并存):收起仅滑出,
     WS/xterm 常驻保活。termFocus 是外部请求定位的终端 id
     （supper_url term:// 点击入口；TerminalTab 消费后清空） */
  termDrawerOpen = $state(false)
  termFocus = $state('')
  /* drawerTool 是抽屉内工具页（终端/文件/浏览器互斥显隐，多 pane 常驻
     保活）；fileFocus 是待打开的文件路径（file:// 入口，ResourcePane 消费后清空） */
  drawerTool = $state<DrawerTool>('term')
  /* 已弹出为独立窗口的工具页：mini 图标/AI 拉开抽屉均改为聚焦窗口 */
  popoutTools = $state<DrawerTool[]>([])
  fileFocus = $state('')
  private boardTag = ''
  /* 模型调用进行中（model_start→model_end），思考指示用 */
  modelActive = $state(false)
  /* 模型请求失败正在重试（modelretry.retry 事件；model_end/turn_end 清空） */
  retryInfo = $state<{ attempt: number; maxAttempts: number; delayMs: number; err: string } | null>(null)
  /* 迭代上限软收尾进行中（final_wrap 事件置位；新轮/切分支清空） */
  wrapPhase = $state(false)
  /* 本轮已进行的迭代数（SSE 帧 iter 实时更新；轮末/切分支清零）——长任务实时看进度 */
  currentIter = $state(0)

  status = $state<Status | null>(null)
  /* 最新 agent_status 快照（status.snapshot 事件实时更新，右上角水位条数据源） */
  live = $state<StatusPayload | null>(null)
  /* 本轮资源变更条目（resource.change 事件更新，右上角 StatusCard 数据源；
     loop_start 清空——事件按需推送，不清会滞留上一轮的旧变更） */
  liveChanges = $state<string[]>([])
  /* 当前分支的任务计划（plan.update 事件 / history 恢复；输入框上方面板数据源） */
  plan = $state<SessionPlan | null>(null)
  /* main 槽模型清单（输入框模型切换器；打开时懒加载） */
  mainModels = $state<ModelEntry[]>([])
  private modelsFetched = false
  settings = $state<Settings | null>(null)
  total = $state<TotalUsage>({ prompt: 0, completion: 0, cached: 0 })
  /* 版本检查（启动静默查一次 + 设置页手动查共用）：updateAvailable 非空 =
     有新版本，侧栏设置钮与设置页版本区的红点、前往下载入口都由它驱动；
     updateChecked 标记首查已完成（设置页首屏直接给"已是最新"结论） */
  updateAvailable = $state<{ latest: string; url: string } | null>(null)
  updateChecked = $state(false)

  /* 懒加载：compact 链上是否还有旧会话可翻、是否正在加载 */
  hasPrev = $state(false)
  loadingPrev = $state(false)
  /* 最近一次下拉拉出的块 uid 集合（展开动画用，渲染层命中加 class） */
  batchIds = $state<Set<number>>(new Set())
  private prevCursor = '' // 已翻到的会话 ID（沿 prevSession 链继续上翻）

  private unsub: (() => void) | null = null
  private uidSeq = 0
  /* 工具触发的抽屉自动拉开:免审调用延迟 ~1s 打开(tool_start 先于
  approve.request 到达,1s 内无审批请求即视为免审直接执行);进入审批
  则等用户批准(decision.resolved=已批准)才打开——未批准时命令不会
  运行,提前弹出只是打扰。只保留"创建类"操作(新终端/新浏览器标签):
  term_action send 等复用已有资源的操作不拉——用户收起抽屉后不该被普通
  消息触发的工具又弹开。 */
  private autoOpenDrawers = new Map<string, DrawerTool>([
    ['term_tab:new', 'term'],
    ['browser_tab:open', 'browser'],
  ])
  private toolOpenTimers = new Map<string, ReturnType<typeof setTimeout>>()
  private toolApprovals = new Map<string, DrawerTool>()
  /* 本轮本地已 push 的 user 块（send 时记录，turn_end/replay.sync 清除）：
  loop_start 到达时同文本跳过（实时路径防双 push）；SSE 重连回放时按 uid
  截断本地本轮块，让整轮回放帧干净重建（防 user/回复块重复） */
  private pendingUserUid = 0
  private pendingUserText = ''
  /* 上一轮通知轮询是否有未决项（待审批角标联动分支拉取的收尾依据） */
  private noticesHadPending = false

  /* tab 条实际高度（折行后变高）：对话页两侧悬浮列 top 跟随它避让 */
  tabbarH = $state(0)
  /* 抽屉展开时主列用户拖拽宽（px；0 = 默认公式 42vw 保底 480），抽屉吃剩余全部 */
  chatWidth = $state(0)

  private nuid(): number {
    return ++this.uidSeq
  }

  /* ── 启动 ── */

  async bootstrap() {
    const b = await api.bootstrap()
    this.activate(b.sessionId)
    this.branches = b.branches ?? []
    this.settings = b.settings
    this.status = b.status
    this.tabs = b.sessionId ? [{ rootId: b.sessionId }] : []
    await this.loadHistory()
    this.unsub?.()
    this.unsub = subscribe(this.activeId, (ev) => this.apply(ev))
    this.refreshNotices()
    void this.refreshUpdate() // 启动静默查版本：有新版 → 侧栏设置钮红点
    setInterval(() => {
      // 未决通知出没联动分支拉取（待审批角标数据源）：出现期间持续拉、
      // 消失后补拉一轮收尾；后台标签活跃照旧拉
      void this.refreshNotices().then(() => {
        const pending = this.notices.some((n) => n.status === 'pending')
        if (this.bgTabsBusy() || pending || this.noticesHadPending) void this.refreshBranches()
        this.noticesHadPending = pending
      })
    }, 3000)
  }

  /* bgTabsBusy：任一非活动标签在跑或待审批（决定 3s 轮询要不要拉分支）。 */
  private bgTabsBusy(): boolean {
    return this.tabs.some((t) => {
      if (t.rootId === this.activeId) return false
      const b = this.branches.find((x) => x.id === t.rootId)
      return !!b && !!(b.running || b.waiting)
    })
  }

  /* refreshUpdate 查一次最新版本：网络失败静默（启动路径不提示，设置页
  手动路径由调用方展示 err）。返回 {err, latest} 供设置页渲染结论。 */
  async refreshUpdate(): Promise<{ err: string; latest: boolean }> {
    try {
      const r = await api.checkUpdate()
      this.updateChecked = true
      if (newerVersion(r.latest, pkg.version)) {
        this.updateAvailable = r
        return { err: '', latest: false }
      }
      this.updateAvailable = null
      return { err: '', latest: true }
    } catch (e) {
      return { err: (e as Error).message, latest: false }
    }
  }

  async refreshStatus() {
    try {
      this.status = await api.status()
    } catch {
      /* 静默 */
    }
  }

  /* 刷新分支列表（运行/等待指示：后台分支的事件不经当前 SSE，靠拉取） */
  async refreshBranches() {
    try {
      this.branches = await api.listTopics()
    } catch {
      /* 静默 */
    }
  }

  private resubscribe() {
    this.unsub?.()
    this.unsub = null
    if (!this.activeId) return // 空状态不开 SSE（避免空 id 的请求）
    this.unsub = subscribe(this.activeId, (ev) => this.apply(ev))
  }

  /* resetView 清空时间线与分支相关标志（切标签/关闭最后一个标签共用）。 */
  private resetView() {
    this.blocks = []
    this.forks = {}
    this.activeForkId = ''
    this.busy = false
    this.lastStatus = ''
    this.pendingUserUid = 0
    this.pendingUserText = ''
    // 分支切换：输出/工具指示与实时水位不跨分支（原分支的 turn_end
    // 已收不到——单 SSE 只订阅当前分支，残留标志会永远挂着）
    this.modelActive = false
    this.retryInfo = null
    this.wrapPhase = false
    this.currentIter = 0
    this.lastTool = ''
    this.live = null
    this.liveChanges = []
    this.plan = null
    this.hasPrev = false
    this.loadingPrev = false
    this.batchIds = new Set()
  }

  private async loadHistory() {
    this.resetView()
    if (!this.activeId) return // 空状态：无会话可载
    try {
      const s = await api.getHistory(this.activeId)
      this.leafId = s.id
      // 分身摘要重建（骨架，过程详情打开抽屉时懒加载）；存档挂在所属 session 目录下
      for (const f of s.forks ?? []) {
        this.forks[f.id] = {
          id: f.id,
          owner: s.id,
          task: f.task,
          status: 'done',
          blocks: [],
          answer: f.answer || '',
          stopReason: f.stopReason || '',
          loaded: false,
        }
      }
      this.blocks = this.buildBlocks(s.messages, s.decisions, s.forks ?? [], s.id)
      this.plan = s.plan ?? null
      this.busy = s.busy
      // 切回运行中的分支：model_start/tool_start 等增量事件不重放，用 busy 兜底
      // 点亮活动指示（否则"思考中"整段消失、像没在执行）；后续事件到达自然接管
      this.modelActive = s.busy
      this.prevCursor = s.id
      // fork 就是 fork：对话内容不标注来源（体内副本自包含）；
      // 来源信息只在记忆页的会话树上展示
      // 上翻余量由后端判定（fork 换源后算：源无上级则不可翻）
      this.hasPrev = !!s.canPrev
    } catch {
      /* 网络异常时保底空时间线 */
    }
  }

  /* 下拉懒加载：沿 compact 链拉出上一会话，一次一个；底部带压缩归档标记 */
  async loadPrev() {
    if (!this.prevCursor || this.loadingPrev) return
    this.loadingPrev = true
    try {
      const res = await api.getPrev(this.prevCursor)
      if (!res) {
        this.hasPrev = false
      } else {
        // 历代库的分身摘要建骨架（懒加载按所属库 ID 取详情）
        for (const fk of res.forks ?? []) {
          if (!this.forks[fk.id]) {
            this.forks[fk.id] = {
              id: fk.id,
              owner: res.id,
              task: fk.task,
              status: 'done',
              blocks: [],
              answer: fk.answer || '',
              stopReason: fk.stopReason || '',
              loaded: false,
            }
          }
        }
        const prevBlocks = this.buildBlocks(res.messages, undefined, res.forks ?? [], res.id)
        const sep: Block = {
          kind: 'note',
          uid: this.nuid(),
          text: `⇪ 上下文已压缩归档：${res.title || ''}`,
        }
        this.batchIds = new Set(prevBlocks.map((b) => b.uid))
        this.blocks = [...prevBlocks, sep, ...this.blocks]
        // 游标 = 已翻到的会话（下次取它的上一级）；可否继续由其上级是否存在决定
        this.prevCursor = res.id
        this.hasPrev = !!res.prevSession
      }
    } catch {
      this.hasPrev = false
    } finally {
      this.loadingPrev = false
    }
  }

  /* 历史重建：user/assistant/tool 消息序列，tool_calls 展开为工具块；决策记录映射为徽标。
     forks 摘要按 task 调用顺序插分身入口卡（forkID 升序与调用序一致）。
     owner=消息所属 session ID，与各消息下标配对供分叉定位。 */
  private buildBlocks(messages: HistoryMessage[], decisions?: DecisionRecord[], forks?: ForkSummary[], owner?: string, msgStart = 0): Block[] {
    const dmap = new Map((decisions || []).map((d) => [d.callId, d.resolution]))
    const forkQueue = [...(forks || [])]
    const out: Block[] = []
    /* 工具结果回填索引：大会话里按 call id 反查块（原先每条约 O(n) 全表扫） */
    const toolByID = new Map<string, Extract<Block, { kind: 'tool' }>>()
    for (let mi = 0; mi < messages.length; mi++) {
      const m = messages[mi]
      if (m.role === 'user') {
        const cbMsg = parseCallbackMessage(m.content)
        if (cbMsg) {
          out.push({
            kind: 'user', uid: this.nuid(), text: cbMsg.body, callback: cbMsg.meta,
            images: m.images, owner, msgIdx: msgStart + mi,
          })
          continue
        }
        const d = parseStatus(m.content) // JSON 载荷
        if (d) {
          // 状态记录仅水位异常时入时间线，平时只在右上角
          if (d.suggestCompact) {
            out.push({ kind: 'status', uid: this.nuid(), text: m.content, data: d })
          }
        } else if (m.content.includes('<agent_status>')) {
          // 中文语义化文本；仅水位异常行进时间线
          // （文案是"建议调用 trim_context 整理上下文"，关键词取"整理上下文"）
          if (m.content.includes('整理上下文')) {
            out.push({ kind: 'status', uid: this.nuid(), text: m.content, data: null })
          }
        } else if (m.content.includes('<resource_change>')) {
          // 资源变更记录：remind 变更段按需插入（实时由 resource.change 事件渲染；
          // available_ 清单行不带 "- " 前缀，正则天然忽略，只取变更条目）
          const items = [...m.content.matchAll(/^- (.+)$/gm)].map((x) => x[1].trim()).filter(Boolean)
          if (items.length) out.push({ kind: 'resourcechange', uid: this.nuid(), items })
        } else if (m.content.includes('<reference_file>')) {
          /* 引用记录独立成块（与用户输入各一条消息，不合并）。标签体是
          纯 JSON：refs[].items 空 = 整文件引用（附件 chips），非空 =
          带标注引用（引用 chips，count=标注条数；片段全文不还原，点击
          回跳文件页读真身）。损坏载荷不渲染。 */
          const inner = m.content.replace(/^[\s\S]*?<reference_file>|<\/reference_file>[\s\S]*$/g, '').trim()
          try {
            const payload = JSON.parse(inner) as { refs?: { path: string; items?: unknown[] }[] }
            const refsAll = payload.refs ?? []
            const refFiles = refsAll.filter((r) => !r.items?.length).map((r) => ({ name: baseName(r.path), path: r.path }))
            const refMarked = refsAll
              .filter((r) => r.items?.length)
              .map((r) => ({ path: r.path, count: r.items!.length }))
            if (refFiles.length || refMarked.length) {
              out.push({ kind: 'user', uid: this.nuid(), text: '', files: refFiles, fileRefs: refMarked })
            }
          } catch {
            /* 损坏记录不渲染 */
          }
        } else if (m.content.includes('<image_loaded>')) {
          // read_file/截图 图片已进上下文：渲染为缩略图小行（paths 在标签体内，
          // 每行一个，紧随其后的“目的：…”行是该图当时的看图目的；m.images 是
          // 当时进上下文的 base64 快照——磁盘文件后续改/删不影响展示，点击开
          // 快照画板草稿）
          const inner = m.content.replace(/^[\s\S]*?<image_loaded>|<\/image_loaded>[\s\S]*$/g, '')
          const paths: string[] = []
          const notes: string[] = []
          for (const raw of inner.split('\n')) {
            const line = raw.trim()
            if (!line) continue
            if (line.startsWith('目的：') && paths.length) {
              notes[paths.length - 1] = line.slice(3)
              continue
            }
            paths.push(line)
            notes.push('')
          }
          out.push({ kind: 'imgload', uid: this.nuid(), paths, notes, images: m.images })
        } else if (m.content.includes('<end_reason>')) {
          const detail = endReasonText(m.content)
          if (detail) out.push({ kind: 'endtick', uid: this.nuid(), icon: endIcon(detail), title: detail })
        } else if (m.content.includes('<context_trim')) {
          out.push({ kind: 'note', uid: this.nuid(), text: trimText(m.content) })
        } else if (m.content.includes('<loop_guard>')) {
          // 循环护栏提醒：小字条呈现（标签体是人话文本）
          const inner = m.content.replace(/^[\s\S]*?<loop_guard>|<\/loop_guard>[\s\S]*$/g, '').trim()
          if (inner) out.push({ kind: 'note', uid: this.nuid(), text: inner })
        } else if (!m.content.trim() && !m.images?.length) {
          // 空输入（纯附件轮，引用已由上一块独立呈现）：不渲染
        } else {
          out.push({ kind: 'user', uid: this.nuid(), text: m.content, images: m.images, owner, msgIdx: msgStart + mi })
        }
      } else if (m.role === 'assistant') {
        if (m.content || m.reasoning) {
          out.push({
            kind: 'assistant',
            uid: this.nuid(),
            text: m.content,
            reasoning: m.reasoning || '',
            streaming: false,
            owner,
            msgIdx: msgStart + mi,
          })
        }
        // 展开工具调用：名称与参数来自 tool_calls（Args 序列化后是嵌套对象，非字符串）
        for (const tc of m.tool_calls || []) {
          const tb: Extract<Block, { kind: 'tool' }> = {
            kind: 'tool',
            uid: this.nuid(),
            id: tc.ID || '',
            name: tc.Name || '',
            args: typeof tc.Args === 'string' ? tc.Args : JSON.stringify(tc.Args ?? ''),
            result: '',
            err: '',
            state: 'done',
            decision: dmap.get(tc.ID || ''),
          }
          // task 调用紧随分身入口卡（真实启动的 fork 才有摘要：审批拒绝/失败的不插）
          const started = tc.Name === 'task' && forkQueue.length > 0
          if (started) tb.hidden = true
          out.push(tb)
          if (tb.id) toolByID.set(tb.id, tb)
          if (started) {
            const f = forkQueue.shift()!
            out.push({ kind: 'fork', uid: this.nuid(), forkId: f.id })
          }
        }
      } else if (m.role === 'tool') {
        // 按调用 ID 回填结果到对应工具块
        const target = m.tool_call_id ? toolByID.get(m.tool_call_id) : undefined
        if (target) {
          target.result = m.content || ''
          target.err = m.err || ''
        } else {
          const orphan: Extract<Block, { kind: 'tool' }> = {
            kind: 'tool',
            uid: this.nuid(),
            id: m.tool_call_id || '',
            name: '',
            args: '',
            result: m.content || '',
            err: m.err || '',
            state: 'done',
          }
          out.push(orphan)
          if (orphan.id) toolByID.set(orphan.id, orphan)
        }
      }
    }
    return out
  }

  /* backfillForkAnchors 用历史重建的尾段回填本地块的分叉锚点（owner/msgIdx）：
  实时轮的块是本地 push 的，没有锚点（分叉按钮不显，切页重建才出现）。
  turn_end 时历史已落盘（后端 FinishRun 先于帧发布），从尾部往前按
  user/assistant 序对位回填；遇已带锚点的块即停（更早的都齐），kind 或
  文本对不上保守停止——只是按钮暂缺，切页重建自愈 */
  private async backfillForkAnchors(sessionId: string) {
    try {
      // 只要尾段：大会话不必为一轮锚点重拉全文（msgStart 把下标拉回绝对值）
      const s = await api.getHistory(sessionId, 200)
      if (sessionId !== this.activeId) return // 等待期间已切分支：别写错时间线
      const rebuilt = this.buildBlocks(s.messages, undefined, undefined, s.id, s.start ?? 0)
      let j = rebuilt.length - 1
      for (let i = this.blocks.length - 1; i >= 0 && j >= 0; i--) {
        const b = this.blocks[i]
        if (b.kind !== 'user' && b.kind !== 'assistant') continue
        if (b.msgIdx !== undefined) break
        while (j >= 0 && rebuilt[j].kind !== 'user' && rebuilt[j].kind !== 'assistant') j--
        if (j < 0) break
        const rb = rebuilt[j] as Extract<Block, { kind: 'user' | 'assistant' }>
        if (rb.kind !== b.kind || rb.text !== b.text) break
        b.owner = rb.owner
        b.msgIdx = rb.msgIdx
        j--
      }
    } catch {
      /* 拉取失败静默 */
    }
  }

  /* ── 分身抽屉 ── */

  /* ensureFork 取分身状态，不存在则建骨架（SSE 重放/异常时序兜底）。 */
  private ensureFork(fid: string): ForkState {
    if (!this.forks[fid]) {
      this.forks[fid] = {
        id: fid,
        owner: this.leafId || this.activeId,
        task: '',
        status: 'running',
        blocks: [],
        answer: '',
        stopReason: '',
        loaded: false,
      }
    }
    return this.forks[fid]
  }

  /* openFork 打开/切换分身抽屉；decisionId 非空时打开后滚动定位到该决策卡，
     切换（无 decisionId）清掉旧跳转目标。 */
  openFork(fid: string, decisionId = '') {
    this.activeForkId = fid
    this.jumpDecision = decisionId
    void this.loadForkDetail(fid)
  }

  closeFork() {
    this.activeForkId = ''
  }

  /* ── 魔法看板（图片查看器的画布引擎，草稿入口） ── */

  /* openImageDraft 打开画板（输入框画笔钮 / 续编 chip / 消息内 base64 图片）：
     先把画布落盘到工作目录 tmp 拿真身路径，再按普通图片资源打开——草稿
     就是一张 tmp 图片（"磁盘是唯一真身"），跨窗口（拖出为独立窗口/弹回
     抽屉）随之自然成立。source 无 = 空白画板底图。 */
  async openImageDraft(source: File | null = null) {
    try {
      const file = source ?? (await blankBoardPng())
      const [path] = await api.stash([file])
      if (!path) throw new Error('未取到落盘路径')
      this.openFileAt(path)
    } catch (e) {
      this.lastStatus = `画板打开失败：${(e as Error).message}`
    }
  }

  /* bumpImg 图片写回后按路径递增缩略图版本（同 URL 的 img 立即换 src，
     含聊天历史 chips——引用同一资源，处处显示最新）。 */
  bumpImg(path: string) {
    this.imgVer = { ...this.imgVer, [path]: (this.imgVer[path] ?? 0) + 1 }
  }

  /* openBase64Draft 把无路径的内存图片（消息内 base64）落盘成真身再打开：
     dataURL → File → openImageDraft。 */
  async openBase64Draft(src: string, name: string) {
    try {
      const r = await fetch(src)
      if (!r.ok) throw new Error('read fail')
      const blob = await r.blob()
      await this.openImageDraft(new File([blob], name || 'image.png', { type: blob.type || 'image/png' }))
    } catch {
      this.lastStatus = '图片读取失败，未能打开画板'
    }
  }

  /* yieldDrawer 工具页已弹出为独立窗口时把抽屉让出来：同一份内容只有一处
     能上屏——抽屉与独立窗口同时显示同一工具，两边都会上报内容区 rect 去
     抢那个唯一的视图，最后上报的赢、另一个白屏（改窗口大小/点 mini 图标
     能让它再上报一次赢回来，现象就是"白一下又好了"）。 */
  private yieldDrawer(tool: DrawerTool) {
    if (this.drawerTool === tool) this.termDrawerOpen = false
  }

  /* openDrawer 拉开抽屉到指定工具页（AI 工具实际执行时调用；
     抽屉与聊天并存，不打断当前视图）；工具已弹出则聚焦窗口。 */
  private openDrawer(tool: DrawerTool) {
    if (this.popoutTools.includes(tool)) {
      ;(window as any).ez?.popout?.open(tool)
      this.yieldDrawer(tool)
      return
    }
    this.drawerTool = tool
    this.termDrawerOpen = true
  }

  /* openTermAt 拉开终端抽屉并定位到指定终端（supper_url term:// 点击；
     首次打开时 WS hello 到达后由 TerminalTab 消费定位）。终端已弹出则
     定位请求转发到窗口。 */
  openTermAt(id: string) {
    if (this.popoutTools.includes('term')) {
      ;(window as any).ez?.popout?.signal('term', 'term', id)
      this.yieldDrawer('term')
      return
    }
    this.termFocus = id
    this.drawerTool = 'term'
    this.termDrawerOpen = true
  }

  /* openFileAt 拉开抽屉文件页并打开指定文件（supper_url file:// 点击）。
     文件夹交给系统资源管理器直接打开;资源页已弹出则打开请求转发到窗口。 */
  async openFileAt(path: string) {
    if (!path) return
    if (await (window as any).ez?.window?.openDir?.(path)) return
    if (this.popoutTools.includes('file')) {
      ;(window as any).ez?.popout?.signal('file', 'file', path)
      this.yieldDrawer('file')
      return
    }
    this.fileFocus = path
    this.drawerTool = 'file'
    this.termDrawerOpen = true
  }

  /* openBrowserAt 拉开浏览器抽屉页并定位到指定标签（supper_url browser://
     点击）；浏览器是 desktop 资产，web 端提示降级。已弹出则聚焦窗口。 */
  openBrowserAt(id: string) {
    if (!id) return
    const ez = (window as any).ez
    if (!ez?.browser) {
      this.lastStatus = '浏览器仅桌面端可用'
      return
    }
    ez.browser.focusTab(id)
    if (this.popoutTools.includes('browser')) {
      ez.popout.open('browser')
      this.yieldDrawer('browser')
    } else {
      this.drawerTool = 'browser'
      this.termDrawerOpen = true
    }
  }

  closeTermDrawer() {
    this.termDrawerOpen = false
  }

  /* 抽屉自动展开匹配：先按 工具名:action 精确查（browser_tab:open），再按工具名查 */
  private drawerFor(name: string, argsText: string): DrawerTool | undefined {
    let action = ''
    try {
      action = String(JSON.parse(argsText)?.action || '')
    } catch {
      /* args 非对象 */
    }
    return this.autoOpenDrawers.get(action ? `${name}:${action}` : name) || this.autoOpenDrawers.get(name)
  }

  /* 工具入口 mini 钮的开关语义：开着且已是该工具页 → 收起；
     否则切到该工具页并拉开；已弹出为窗口 → 聚焦窗口 */
  toggleDrawerTool(t: DrawerTool) {
    if (this.popoutTools.includes(t)) {
      ;(window as any).ez?.popout?.open(t)
      this.yieldDrawer(t)
      return
    }
    if (this.termDrawerOpen && this.drawerTool === t) this.termDrawerOpen = false
    else {
      this.drawerTool = t
      this.termDrawerOpen = true
    }
  }

  /* ── 弹出窗口协同 ── */

  /* syncPopoutTools 启动时向主进程同步已弹出的工具（页面重载后恢复认知） */
  syncPopoutTools() {
    const popoutApi = (window as any).ez?.popout
    if (!popoutApi) return
    void popoutApi.tools().then((list: DrawerTool[]) => {
      this.popoutTools = list
    })
  }

  /* toolPoppedOut 记录工具已弹出（mini 图标/AI 拉开抽屉改聚焦窗口），
     并让抽屉退出该工具页——同一份内容只在一处上屏（两处同时显示同一工具
     会互相抢那个唯一视图，表现是其中一个白屏并来回闪） */
  toolPoppedOut(tool: DrawerTool) {
    if (!this.popoutTools.includes(tool)) this.popoutTools = [...this.popoutTools, tool]
    this.yieldDrawer(tool)
  }

  /* popoutClosed 弹窗关闭 = 工具回流抽屉：重开抽屉到该工具页，
     资源页带回窗口期间的最新状态 */
  popoutClosed(view: string, stateJson: string | null) {
    this.popoutTools = this.popoutTools.filter((t) => t !== view)
    this.drawerTool = view as DrawerTool
    this.termDrawerOpen = true
    if (view === 'file' && stateJson) filePane.restore(stateJson)
  }

  /* loadForkDetail 懒加载存档详情（已结束分身的执行记录重建）；运行中的
     走实时流。实时已累积过内容的分身不覆盖（避免 uid 全换导致折叠态重置）。 */
  private async loadForkDetail(fid: string) {
    const f = this.forks[fid]
    if (!f || f.loaded || f.status === 'running') return
    f.loaded = true
    try {
      const r = await api.getFork(f.owner || this.activeId, fid)
      if (f.blocks.length === 0) f.blocks = this.buildBlocks(r.messages, r.decisions)
    } catch {
      f.loaded = false // 失败可重试
    }
  }

  /* ── 审批模式 / 模型切换（输入框工具条） ── */

  /* 审批三档（全局；工具审批实时读取，即改即生效） */
  async setApproveMode(mode: '' | 'off' | 'strict') {
    if (!this.settings || (this.settings.approveMode ?? '') === mode) return
    const prev = this.settings.approveMode ?? ''
    this.settings = { ...this.settings, approveMode: mode }
    try {
      await api.setApproveMode(mode)
    } catch (e) {
      this.settings = { ...this.settings, approveMode: prev }
      this.lastStatus = `审批档位切换失败：${(e as Error).message}`
    }
  }

  /* 思考档位切换（全局；请求侧实时读取，即改即生效） */
  async setReasoningEffort(effort: ReasoningEffort) {
    if (!this.settings || (this.settings.reasoningEffort ?? '') === effort) return
    const prev = this.settings.reasoningEffort ?? ''
    this.settings = { ...this.settings, reasoningEffort: effort }
    try {
      await api.setReasoningEffort(effort)
    } catch (e) {
      this.settings = { ...this.settings, reasoningEffort: prev }
      this.lastStatus = `思考档位切换失败：${(e as Error).message}`
    }
  }

  /* main 槽模型清单（切换器首次打开时拉取） */
  async ensureModels() {
    if (this.modelsFetched) return
    try {
      const m = await api.getModels()
      this.mainModels = m.main ?? []
      this.modelsFetched = true
    } catch {
      /* 下次打开再试 */
    }
  }

  /* 模型清单失效（设置页增删改后调用）：清缓存，下次打开切换器重新拉取 */
  invalidateModels() {
    this.modelsFetched = false
  }

  /* 切换主模型（下一次输入生效）：状态栏变更行提示（与资源变更同通道，
     下轮 loop_start 清空——提示寿命与"待生效"窗口一致） */
  async activateModel(name: string) {
    if (!name || name === this.status?.model) return
    try {
      const { model: e } = await api.activateModel(name)
      if (this.status) {
        this.status.model = e.name
        this.status.modelVision = !!e.vision
      }
      this.liveChanges = [
        ...this.liveChanges.filter((x) => !x.startsWith('切换模型')),
        `切换模型到 ${e.name}（${e.vision ? '支持视觉' : '不支持视觉'}）`,
      ]
    } catch (err) {
      this.lastStatus = `模型切换失败：${(err as Error).message}`
    }
  }

  /* ── 发送 / 取消 ── */

  /* 打断式发送：运行中再来指令 = 先终止当前轮（等引擎真正退出，含工具树杀），
     再执行新指令；等待超时则放弃并提示。附件/引用统一为工作目录路径
    （<reference_file> 记录告知模型）——附件进框时已落盘 tmp，发送只传路径。 */
  async send(text: string, attachments: Attachment[] = [], fileRefs: FileRef[] = []) {
    if (!this.activeId || (!text.trim() && !attachments.length && !fileRefs.length)) return
    if (attachments.some((a) => !a.path)) {
      this.lastStatus = '附件仍在暂存中，稍候再发送'
      return
    }
    if (this.busy) {
      this.lastStatus = '正在终止当前轮…'
      await this.cancel()
      if (!(await this.waitIdle(8000))) {
        this.lastStatus = '当前轮未能及时终止，请稍后重试'
        return
      }
    }
    const final: FilePayload[] = attachments.map((a) => ({ name: a.name, path: a.path! }))
    /* 引用与用户输入各成一块（与消息历史同构：reference_file 记录是独立
    user 消息）；pendingUserUid 记第一块 uid——replay.sync 截断时两块一起切 */
    const turnFiles = final.map((a) => ({ name: a.name, path: a.path }))
    const turnFileRefs = fileRefs.map((r) => ({ path: r.path, count: r.items.length }))
    let firstUid = 0
    if (turnFiles.length || turnFileRefs.length) {
      firstUid = this.nuid()
      this.blocks.push({ kind: 'user', uid: firstUid, text: '', files: turnFiles, fileRefs: turnFileRefs })
    }
    if (text) {
      const uid = this.nuid()
      this.blocks.push({ kind: 'user', uid, text })
      if (!firstUid) firstUid = uid
    }
    this.pendingUserUid = firstUid
    this.pendingUserText = text
    this.busy = true
    this.lastStatus = ''
    try {
      await api.send(this.activeId, text, final, fileRefs)
      void this.refreshBranches() // 首次发言落线索引 + 运行指示
    } catch (e) {
      this.busy = false
      this.lastStatus = `发送失败：${(e as Error).message}`
    }
  }

  /* 轮询等待轮结束（turn_end 置 busy=false）；超时返回 false。 */
  private waitIdle(timeoutMs: number): Promise<boolean> {
    return new Promise((resolve) => {
      const t0 = Date.now()
      const timer = setInterval(() => {
        if (!this.busy) {
          clearInterval(timer)
          resolve(true)
        } else if (Date.now() - t0 > timeoutMs) {
          clearInterval(timer)
          resolve(false)
        }
      }, 100)
    })
  }

  async cancel() {
    if (!this.activeId) return
    await api.cancel(this.activeId).catch(() => {})
  }

  /* ── 设置 / 话题 ── */

  async saveSettings(s: Settings) {
    await api.saveSettings(s)
    this.settings = { ...(this.settings ?? {}), ...s } // 设置页按需提交局部字段，合并而非替换（否则会抹掉思考/审批档）
    this.lastStatus = '设置已保存（模型与提示即时生效）'
    await this.refreshStatus()
  }

  /* ── 分支三操作 ── */

  /* 切换分支：状态/审批/水位随切换（SSE 重订阅时 ReplayFrames 重建
     时间线与未决审批；后台分支的轮不因切换取消） */
  async switchBranch(rootId: string) {
    if (rootId === this.activeId) return
    try {
      const r = await api.activateBranch(rootId)
      this.activate(r.id)
      await this.loadHistory()
      this.resubscribe()
      await this.refreshStatus()
      await this.refreshBranches()
    } catch (e) {
      this.lastStatus = `切换分支失败：${(e as Error).message}`
    }
  }

  /* 开新线（New） */
  async newBranch() {
    try {
      const r = await api.newBranch()
      this.activate(r.id)
      await this.loadHistory()
      this.resubscribe()
      await this.refreshStatus()
      await this.refreshBranches()
    } catch (e) {
      this.lastStatus = `新建分支失败：${(e as Error).message}`
    }
  }

  /* openTab：已打开=激活；未打开=切换成功后登记（切换失败不落标签，
     避免悬空条目——switchBranch 内部捕获不外抛，以 activeId 判成败）。 */
  async openTab(rootId: string) {
    if (rootId !== this.activeId) await this.switchBranch(rootId)
    if (this.activeId === rootId && !this.tabs.some((t) => t.rootId === rootId)) {
      this.tabs = [...this.tabs, { rootId }]
    }
  }

  /* openSession 打开会话并切到对话页（通知与回调记录跳转共用）。 */
  async openSession(rootId: string) {
    if (!rootId) return
    await this.openTab(rootId)
    if (this.activeId === rootId) this.jumpView = 'chat'
  }

  /* openCallbackTask 从回调消息卡跳到对应任务详情。 */
  openCallbackTask(taskId: string) {
    if (!taskId) return
    this.callbackFocus = taskId
    this.jumpView = 'callbacks'
  }

  /* closeTab：关标签即停会话（关掉的线在跑就取消当前轮，别让它悄悄在
     列表里继续跑）；关活动标签激活右邻（退化左邻，浏览器惯例）；最后
     一个 → 空状态（退订 SSE、清视图——会话本身留在列表，仅停轮）。 */
  async closeTab(rootId: string) {
    const i = this.tabs.findIndex((t) => t.rootId === rootId)
    if (i < 0) return
    this.tabs = this.tabs.filter((t) => t.rootId !== rootId)
    delete this.drafts[rootId]
    void api.cancel(rootId).catch(() => {})
    if (rootId !== this.activeId) return
    const neighbor = this.tabs[i]?.rootId || this.tabs[i - 1]?.rootId || ''
    if (neighbor) {
      await this.switchBranch(neighbor)
      return
    }
    this.unsub?.()
    this.unsub = null
    this.activate('')
    this.leafId = ''
    this.resetView()
  }

  /* closeOtherTabs：只保留指定标签，其余全关（活动标签被关则激活它）；
     被关的线一并停轮。 */
  async closeOtherTabs(rootId: string) {
    for (const t of this.tabs) {
      if (t.rootId === rootId) continue
      delete this.drafts[t.rootId]
      void api.cancel(t.rootId).catch(() => {})
    }
    this.tabs = this.tabs.filter((t) => t.rootId === rootId)
    if (this.activeId && this.activeId !== rootId) await this.switchBranch(rootId)
  }

  /* closeRightTabs：关掉指定标签右侧的全部标签（被关的线一并停轮）；
     活动标签在其中则激活该标签。 */
  async closeRightTabs(rootId: string) {
    const i = this.tabs.findIndex((t) => t.rootId === rootId)
    if (i < 0) return
    const removed = this.tabs.slice(i + 1)
    this.tabs = this.tabs.slice(0, i + 1)
    for (const t of removed) {
      delete this.drafts[t.rootId]
      void api.cancel(t.rootId).catch(() => {})
    }
    if (removed.some((t) => t.rootId === this.activeId)) await this.switchBranch(rootId)
  }

  /* newChatTab：开新线并落为新标签。 */
  async newChatTab() {
    await this.newBranch()
    if (this.activeId && !this.tabs.some((t) => t.rootId === this.activeId)) {
      this.tabs = [...this.tabs, { rootId: this.activeId }]
    }
  }

  /* 归档换代（分支列表行操作）：指定分支总结归档开新篇，线不变叶子换代。
     期间可自由切换/新建分支对话——锁只作用于被归档分支自身 */
  async compactTopic(rootId?: string) {
    const id = rootId || this.activeId
    if (this.archivingRootId) return
    const active = id === this.activeId
    if (active && this.busy) {
      this.lastStatus = '会话运行中，稍后再归档'
      return
    }
    this.archivingRootId = id
    if (active) this.lastStatus = '正在归档话题…'
    try {
      await api.compactTopic(rootId)
      if (active) {
        await this.loadHistory()
        await this.refreshStatus()
      }
      await this.refreshBranches()
      if (active) this.lastStatus = ''
    } catch (e) {
      if (active) this.lastStatus = `归档失败：${(e as Error).message}`
    } finally {
      this.archivingRootId = ''
    }
  }

  /* 从任意消息分叉（Copy）：复制源会话 [0, msgIdx]（含选中消息）开新线，
     落为新标签 */
  async forkFrom(owner: string, msgIdx: number) {
    try {
      const r = await api.forkSession(owner, msgIdx + 1)
      this.activate(r.id)
      await this.loadHistory()
      this.resubscribe()
      await this.refreshStatus()
      await this.refreshBranches()
      if (!this.tabs.some((t) => t.rootId === this.activeId)) {
        this.tabs = [...this.tabs, { rootId: this.activeId }]
      }
    } catch (e) {
      this.lastStatus = `分叉失败：${(e as Error).message}`
    }
  }

  /* ── 决策回传（时间线卡） ── */

  /* markToolDecision 对应工具卡打决策徽标（与 decisions.jsonl 重建同源）；
  分身工具卡在分身块数组里。通知栏条目由全局轮询收敛，不在此处理。 */
  private markToolDecision(id: string, resolution: string) {
    for (const bs of [this.blocks, ...Object.values(this.forks).map((f) => f.blocks)]) {
      const t = bs.find((b) => b.kind === 'tool' && b.id === id)
      if (t && t.kind === 'tool') {
        t.decision = resolution
        break
      }
    }
  }

  async decideApprove(block: DecisionData, approve: boolean, reason: string) {
    if (!this.activeId) return
    block.resolved = true
    block.resolution = approve ? '已批准' : reason ? `已拒绝：${reason}` : '已拒绝'
    this.markToolDecision(block.id, block.resolution)
    this.removeResolvedDecisions(block.id)
    await api.decideApprove(this.activeId, block.id, approve, reason).catch(() => {})
  }

  async decideAnswer(block: DecisionData, input: string) {
    if (!this.activeId) return
    block.resolved = true
    block.resolution = input || '(未回答)'
    this.markToolDecision(block.id, block.resolution)
    this.removeResolvedDecisions(block.id)
    await api.decideAnswer(this.activeId, block.id, input).catch(() => {})
  }

  /* decidePlan 计划确认卡：confirm 放行，feedback 带意见；
     与 approve 同款先乐观结算卡片再回传，失败静默（通知栏轮询兜底） */
  async decidePlan(block: DecisionData, action: 'confirm' | 'feedback', feedback: string) {
    if (!this.activeId) return
    const fb = feedback.trim()
    block.resolved = true
    block.resolution = action === 'confirm' ? '已确认计划' : fb ? `已反馈：${fb}` : '已反馈'
    this.markToolDecision(block.id, block.resolution)
    this.removeResolvedDecisions(block.id)
    await api.decidePlan(this.activeId, block.id, action, fb).catch(() => {})
  }

  /* ── MCP OAuth 授权（会话内 = 时间线里的决策卡，同 approve/ask） ── */

  private mcpAuthTimers: Record<string, ReturnType<typeof setInterval>> = {}
  private mcpAuthRejects: Record<string, (e: Error) => void> = {}
  private mcpAuthMeta: Record<string, string> = {} // callID → RFC9728 资源元数据地址（发现授权服务器用）

  /*
  decideMcpAuth 决策卡上的动作：授权 = 取授权 URL 用系统浏览器打开、轮询至完成
  后结算为「已授权」（工具卡随即重试出结果）；取消/失败 = 结算为失败（工具卡显示
  错误结果）。结算即移除决策卡（与 approve 一致：结果落在工具卡上）。
  */
  async decideMcpAuth(block: DecisionData, authorize: boolean, reason: string) {
    if (!this.activeId) return
    if (!authorize) {
      this.stopMcpAuthPoll(block.id)
      await api.oauthCancel(block.name).catch(() => {})
      this.finishMcpAuth(block, false, reason || '已取消', '') // 徽标显示"已取消"；线上理由留空（后端给规范措辞）
      return
    }
    if (block.busy) return // 去授权进行中，忽略重复点击
    block.busy = true
    block.err = ''
    try {
      const { url } = await api.oauthStart(block.name, this.mcpAuthMeta[block.id] || '')
      if (!url) throw new Error('未获得授权地址（无法发现授权服务器）')
      openExternal(url)
      await this.pollMcpAuthorized(block.id, block.name)
      this.finishMcpAuth(block, true, '已授权')
    } catch (e) {
      block.busy = false
      this.finishMcpAuth(block, false, (e as Error).message || '授权失败')
    }
  }

  /* finishMcpAuth 结算一张授权卡：打工具卡徽标、移除决策卡、回传决策解除阻塞。
     resolution 是徽标文案；reason 是回传后端的理由（默认同徽标，取消时留空）。
     幂等：取消路径与轮询回调可能各触发一次。 */
  private finishMcpAuth(block: DecisionData, ok: boolean, resolution: string, reason = resolution) {
    if (block.resolved) return
    this.stopMcpAuthPoll(block.id)
    delete this.mcpAuthMeta[block.id]
    block.resolved = true
    block.resolution = resolution
    this.markToolDecision(block.id, resolution)
    this.removeResolvedDecisions(block.id)
    void api.decideMcpAuth(this.activeId, block.id, ok, ok ? '' : reason).catch(() => {})
    void this.refreshNotices()
  }

  /* pollMcpAuthorized 轮询授权状态直至完成（5 分钟超时）；按 callID 记时器与
     reject 句柄，取消时能立刻结束等待。 */
  private pollMcpAuthorized(id: string, server: string): Promise<void> {
    return new Promise((resolve, reject) => {
      const started = Date.now()
      this.mcpAuthRejects[id] = reject
      this.mcpAuthTimers[id] = setInterval(async () => {
        if (Date.now() - started > 5 * 60_000) {
          this.clearMcpAuthPoll(id)
          reject(new Error('授权超时'))
          return
        }
        try {
          const { authorized } = await api.oauthStatus(server)
          if (authorized) {
            this.clearMcpAuthPoll(id)
            resolve()
          }
        } catch {
          /* 后端不可达：下一轮重试 */
        }
      }, 1500)
    })
  }

  /* clearMcpAuthPoll 内部终态：清时器但不动 Promise（由调用方 resolve/reject）。 */
  private clearMcpAuthPoll(id: string) {
    const t = this.mcpAuthTimers[id]
    if (t) {
      clearInterval(t)
      delete this.mcpAuthTimers[id]
    }
    delete this.mcpAuthRejects[id]
  }

  /* stopMcpAuthPoll 外部取消：清时器并让等待中的 Promise 立刻以失败结束。 */
  private stopMcpAuthPoll(id: string) {
    const t = this.mcpAuthTimers[id]
    if (t) {
      clearInterval(t)
      delete this.mcpAuthTimers[id]
    }
    const reject = this.mcpAuthRejects[id]
    if (reject) {
      delete this.mcpAuthRejects[id]
      reject(new Error('已取消'))
    }
  }

  /* stopMcpAuthPolls 清空全部授权轮询（轮结束/切会话时）。 */
  private stopMcpAuthPolls() {
    for (const k of Object.keys(this.mcpAuthTimers)) this.stopMcpAuthPoll(k)
  }

  /* resolveNoticeMcpAuth 通知栏处理授权：直接跑流程并回传决策（不依赖时间线卡）。 */
  private async resolveNoticeMcpAuth(n: NoticeData, authorize: boolean) {
    const meta = this.mcpAuthMeta[n.id] || ''
    delete this.mcpAuthMeta[n.id]
    if (!authorize) {
      this.stopMcpAuthPoll(n.id)
      await api.oauthCancel(n.title).catch(() => {})
      await api.decideMcpAuth(n.rootId, n.id, false, '').catch(() => {})
      void this.refreshNotices()
      return
    }
    try {
      const { url } = await api.oauthStart(n.title, meta)
      if (!url) throw new Error('未获得授权地址（无法发现授权服务器）')
      openExternal(url)
      await this.pollMcpAuthorized(n.id, n.title)
      await api.decideMcpAuth(n.rootId, n.id, true, '').catch(() => {})
    } catch (e) {
      await api.oauthCancel(n.title).catch(() => {})
      await api.decideMcpAuth(n.rootId, n.id, false, (e as Error).message || '授权失败').catch(() => {})
    }
    void this.refreshNotices()
  }

  /* removeResolvedDecisions 已决决策卡整体移除：审批结果以工具卡徽标呈现，
     询问的回答即工具卡结果，时间线不留独立的决策卡副本。 */
  private removeResolvedDecisions(id: string) {
    const strip = (bs: Block[]) => {
      const i = bs.findIndex((b) => b.kind === 'decision' && b.id === id)
      if (i >= 0) bs.splice(i, 1)
    }
    strip(this.blocks)
    for (const f of Object.values(this.forks)) strip(f.blocks)
  }

  /* ── 全局通知（跨分支轮询：服务端 pending 是唯一真相，全量替换） ── */

  /* refreshNotices 拉取全分支未决请求重建通知栏（3s 轮询 + 决策后手动刷）。
  内容未变化时不替换数组引用——避免每轮 reconcile 引发的列表视觉抖动。 */
  async refreshNotices() {
    try {
      const groups = await api.listNotifications()
      const out: NoticeData[] = []
      for (const g of groups) {
        const name = this.branches.find((b) => b.id === g.rootId)?.title || ''
        for (const it of g.items) {
          let question = ''
          let info = ''
          let jump = ''
          try {
            const a = it.args ? JSON.parse(it.args) : {}
            /* 计划通知用「目标」当摘要（计划参数里没有 question） */
            question = a.question || (it.kind === 'plan' && typeof a.goal === 'string' ? a.goal : '')
            info = a.detail || ''
            jump = typeof a.target === 'string' ? a.target : ''
          } catch {
            /* 非法 JSON 忽略 */
          }
          const done = it.kind === 'info' // 完成通知（非待处理）
          out.push({
            id: it.callId,
            rootId: g.rootId,
            kind: it.kind,
            source: g.rootId === 'callbacks' ? '定时回调' : it.forkId ? '分身' : name || 'agent',
            forkId: it.forkId || '',
            title: it.kind === 'plan' ? '任务计划待确认' : it.tool,
            detail: done ? info : question,
            time: new Date(it.ts || Date.now()).toTimeString().slice(0, 5),
            status: done ? 'done' : 'pending',
            resolution: done ? '已完成' : '',
            target: done || it.kind === 'callback' ? jump : it.kind === 'memory' ? '' : `decision-${it.callId}`,
          })
        }
      }
      const local = this.notices.filter((n) => n.local)
      const remote = this.notices.filter((n) => !n.local)
      const same =
        out.length === remote.length &&
        out.every((n, i) => {
          const c = remote[i]
          return c.id === n.id && c.rootId === n.rootId && c.detail === n.detail && c.time === n.time
        })
      if (!same) this.notices = [...local, ...out]
      // 当前会话的完成通知即刻已读（正在看它，不必刷红点）
      this.readCompletions(this.activeId)
    } catch {
      /* 后端不可达静默（下一轮重试） */
    }
  }

  /* resolveNoticeGlobal 通知栏内联决策：按通知携带的分支直接回传（不依赖
  当前分支的时间线）。乐观移除在前（点击即消失），失败则刷新恢复真实状态。 */
  async resolveNoticeGlobal(n: NoticeData, action: string, input?: string) {
    // 记忆待确认：确认应用/取消丢弃（全局端点，非分支决策）
    if (n.kind === 'memory') {
      const accept = action === 'accept'
      this.notices = this.notices.filter((x) => x.id !== n.id)
      this.memoryDecisions[n.id] = accept ? 'accept' : 'cancel'
      try {
        const r = await api.decideMemoryPending(n.id, accept)
        this.lastStatus = r.result
      } catch (e) {
        delete this.memoryDecisions[n.id]
        this.lastStatus = `记忆操作处理失败：${(e as Error).message}`
      }
      await this.refreshNotices()
      return
    }
    // MCP 授权：通知栏直接跑流程并回传决策（多为后台分支，无时间线卡）
    if (n.kind === 'mcp_auth') {
      void this.resolveNoticeMcpAuth(n, action === 'authorize')
      return
    }
    this.notices = this.notices.filter((x) => !(x.id === n.id && x.rootId === n.rootId))
    try {
      if (n.kind === 'approve') await api.decideApprove(n.rootId, n.id, action === 'approve', '')
      else if (n.kind === 'plan') await api.decidePlan(n.rootId, n.id, 'confirm', '')
      else await api.decideAnswer(n.rootId, n.id, input ?? '')
    } catch {
      await this.refreshNotices()
    }
  }

  /* jumpToNoticeRef 跳转到通知来源（外部窗口传入的裸标识）：回调通知
  按 target 打开会话或回调页任务，其他跨分支先切换（等待历史加载），
  分身请求打开抽屉定位，主时间线经 jumpMain 锚点由 ChatView 滚动。 */
  async jumpToNoticeRef(ref: Pick<NoticeData, 'rootId' | 'forkId' | 'id' | 'target'>) {
    const target = ref.target || ''
    if (target.startsWith('callback-session:')) {
      // 会话回调投递成功：直接打开触发的会话
      await this.openSession(target.slice('callback-session:'.length))
      return
    }
    if (target.startsWith('callback-task:')) {
      // 快应用/脚本/失败回调：打开回调页并展开该任务
      this.callbackFocus = target.slice('callback-task:'.length)
      this.jumpView = 'callbacks'
      return
    }
    if (target === 'memory-view') {
      // 本地记忆通知：打开记忆页（用户查看即确认，关闭通知条即已读）
      this.jumpView = 'memory'
      return
    }
    if (ref.rootId === 'callbacks') {
      // 无任务锚点的回调通知：打开回调页
      this.jumpView = 'callbacks'
      return
    }
    if (ref.rootId) await this.openSession(ref.rootId)
    if (!ref.target) return // 完成通知：切到该会话即可，无时间线锚点
    if (ref.forkId) {
      this.openFork(ref.forkId, ref.id)
      return
    }
    this.jumpMain = ref.target
  }

  /* jumpToNotice 通知栏内跳转：委托裸标识版本。 */
  async jumpToNotice(n: NoticeData) {
    await this.jumpToNoticeRef(n)
  }

  dismissNotice(id: string) {
    const n = this.notices.find((x) => x.id === id)
    this.notices = this.notices.filter((x) => x.id !== id)
    // 完成通知（info）在服务端置空——否则下一轮轮询又拉回来；本地通知无服务端对应
    if ((n?.kind === 'info' || n?.kind === 'callback') && !n.local) {
      void api.dismissNotification(n.rootId, n.id).catch(() => this.refreshNotices())
    }
  }

  /* activate 切换活动会话（分支三操作/分叉/新建/引导都走它）：该会话的完成
     通知随之视为已读——"打开对话=已读"，与入口无关 */
  private activate(rootId: string) {
    this.activeId = rootId
    this.readCompletions(rootId)
  }

  /* readCompletions 把某会话的完成通知标记已读（本地移除 + 服务端置空） */
  private readCompletions(rootId: string) {
    if (!rootId) return
    for (const n of [...this.notices]) {
      if (n.kind === 'info' && n.rootId === rootId) this.dismissNotice(n.id)
    }
  }

  /* ── 事件归约 ── */

  apply(ev: SseEvent) {
    this.tick++
    // 迭代数实时跟随（仅主轮帧；分身帧带 forkId，其轮次不能覆盖主轮进度）
    if (!ev.forkId && typeof ev.iter === 'number' && ev.iter > 0) this.currentIter = ev.iter
    switch (ev.type) {
      case 'loop.guard': {
        // 循环护栏：时间线实时小字条（历史重建由 <loop_guard> 消息渲染，回放不重发）
        const d = ev.data as { tool?: string; count?: number; note?: string } | undefined
        if (!ev.forkId) {
          const text = d?.note || `工具 ${d?.tool ?? ''} 重复调用 ${d?.count ?? '?'} 次，已提醒模型停止重复；无效可停止本轮`
          this.blocks.push({ kind: 'note', uid: this.nuid(), text })
        }
        break
      }
      case 'replay.sync': {
        // SSE 建连首帧（后端权威运行态）：无运行轮时复位 busy（轮在断线
        // 窗口内结束会错过 turn_end 而卡"运行中"）；有运行轮时截断本地
        // 本轮块（到 pendingUserUid 含），让随后整轮回放帧干净重建
        const active = !!(ev.data as { turnActive?: boolean } | undefined)?.turnActive
        if (!active) {
          this.busy = false
          this.modelActive = false
          this.lastTool = ''
        } else if (this.pendingUserUid) {
          const i = this.blocks.findIndex((b) => b.uid === this.pendingUserUid)
          if (i >= 0) this.blocks = this.blocks.slice(0, i)
        }
        this.pendingUserUid = 0
        this.pendingUserText = ''
        break
      }
      case 'loop_start': {
        this.currentIter = 0
        /* 回放重建：本轮 user 输入（实时路径 send 已本地 push，同文本去重）。
        主轮带引用时载荷是对象 {text, files, fileRefs}（后端 Publish 附带——
        本轮消息要等轮结束才并入历史，运行中切回分支全靠回放帧重建 chips） */
        const d = ev.data
        const obj = d && typeof d === 'object' ? d : null
        const text = typeof d === 'string' ? d : (obj?.text ?? '')
        const cbMsg = parseCallbackMessage(text)
        // 新一轮开始：清空上一轮的资源变更（右上角只挂本轮）（resource.change 按需推送不自动清）
        if (!ev.forkId) this.liveChanges = []
        if (ev.forkId) {
          // 分身输入进分身聊天框（含任务包装前缀，即分身收到的原文）
          const f = this.ensureFork(ev.forkId)
          const last = f.blocks[f.blocks.length - 1]
          if (text && !(last && last.kind === 'user' && last.text === text)) {
            f.blocks.push({ kind: 'user', uid: this.nuid(), text })
          }
          break
        }
        // 本地已 push 过本轮输入（含实时与重放截断后的重建）才跳过——
        // 按 pendingUserText 标记判重（断线重连时尾部已是模型输出，末块
        // 比对会误判重复）；重建 push 后同样记录标记（多次重连的截断依据）。纯附件轮
        // （text 空）按附件存在 + pendingUserUid 判：空文本无法作去重键。
        // 重建与 send 同构：引用块 + 输入块各一条
        const turnFiles = obj?.files
        const turnFileRefs = obj?.fileRefs?.map((r: { path: string; items?: unknown[] }) => ({
          path: r.path,
          count: r.items?.length ?? 0,
        }))
        const rebuildText = !!text && text !== this.pendingUserText
        const rebuildRefs = (!!turnFiles?.length || !!turnFileRefs?.length) && !this.pendingUserUid
        if (rebuildText || rebuildRefs) {
          let firstUid = 0
          if (rebuildRefs) {
            firstUid = this.nuid()
            this.blocks.push({ kind: 'user', uid: firstUid, text: '', files: turnFiles, fileRefs: turnFileRefs })
          }
          if (rebuildText) {
            const uid = this.nuid()
            if (cbMsg) this.blocks.push({ kind: 'user', uid, text: cbMsg.body, callback: cbMsg.meta })
            else this.blocks.push({ kind: 'user', uid, text })
            if (!firstUid) firstUid = uid
          }
          this.pendingUserUid = firstUid
          this.pendingUserText = text
        }
        break
      }
      case 'decision.resolved': {
        // 回放纠正：已决决策卡直接移除（结果在工具卡上可见），徽标同步；
        // 通知栏由全局轮询收敛，不经此路径
        const d = ev.data || {}
        if (d.id) {
          this.markToolDecision(d.id, d.resolution || '')
          this.removeResolvedDecisions(d.id)
          // 分支 waiting 标记即刻收敛（tab 待审批角标不残留到轮末）
          void this.refreshBranches()
        }
        // term_*/browser_* 审批通过 → 现在才拉开对应抽屉页（拒绝则什么都不做）
        if (d.id && this.toolApprovals.has(d.id)) {
          const drawer = this.toolApprovals.get(d.id)
          this.toolApprovals.delete(d.id)
          if ((d.resolution || '').startsWith('已批准') && drawer) this.openDrawer(drawer)
        }
        break
      }
      case 'model_start': {
        this.modelActive = true
        this.retryInfo = null // 新一次调用开始，旧重试信息作废
        break
      }
      case 'final_wrap': {
        // 达到迭代上限，正在做总结收尾调用（与普通迭代无区别，须显式标识）
        this.wrapPhase = true
        break
      }
      case 'modelretry.retry': {
        const d = ev.data as { attempt?: number; maxAttempts?: number; delayMs?: number; err?: string } | null
        if (d) {
          this.retryInfo = {
            attempt: d.attempt ?? 0,
            maxAttempts: d.maxAttempts ?? 0,
            delayMs: d.delayMs ?? 0,
            err: d.err ?? '',
          }
        }
        break
      }
      case 'tool_chunk': {
        // 流式工具调用增量：按 index 分桶累积成 building 态工具块
        const d = ev.data || {}
        const key = `b-${ev.forkId || 'm'}-${d.index ?? 0}`
        const bs = ev.forkId ? this.ensureFork(ev.forkId).blocks : this.blocks
        const t = bs.find((b) => b.kind === 'tool' && b.id === key && b.state === 'building')
        if (t && t.kind === 'tool') {
          if (d.nameDelta) t.name += d.nameDelta
          if (d.argsDelta) t.args += d.argsDelta
        } else {
          bs.push({
            kind: 'tool',
            uid: this.nuid(),
            id: key,
            name: d.nameDelta || '',
            args: d.argsDelta || '',
            result: '',
            err: '',
            state: 'building',
          })
        }
        break
      }
      case 'model_chunk':
      case 'reasoning_chunk': {
        const delta: string = typeof ev.data === 'string' ? ev.data : ''
        if (!delta) break
        const bs = ev.forkId ? this.ensureFork(ev.forkId).blocks : this.blocks
        this.appendDelta(bs, delta, ev.type === 'model_chunk')
        break
      }
      case 'stream.snapshot': {
        /* 回放重建：in-flight 流的累积快照（只进回放缓存，实时订阅者收增量）。
        正文/思考替换当前流式块（重连纠偏，之后的增量继续追加），构造期
        工具块按快照重建 building 态（tool_start 到达时按名认领） */
        const d = ev.data || {}
        const bs = ev.forkId ? this.ensureFork(ev.forkId).blocks : this.blocks
        if (d.content || d.reasoning) {
          const last = bs[bs.length - 1]
          if (last && last.kind === 'assistant' && last.streaming) {
            last.text = d.content || ''
            last.reasoning = d.reasoning || ''
          } else {
            bs.push({
              kind: 'assistant',
              uid: this.nuid(),
              text: d.content || '',
              reasoning: d.reasoning || '',
              streaming: true,
            })
          }
        }
        for (const t of d.tools ?? []) {
          const key = `b-${ev.forkId || 'm'}-${t.index ?? 0}`
          const ex = bs.find((b) => b.kind === 'tool' && b.id === key && b.state === 'building')
          if (ex && ex.kind === 'tool') {
            ex.name = t.name || ''
            ex.args = t.args || ''
          } else {
            bs.push({
              kind: 'tool',
              uid: this.nuid(),
              id: key,
              name: t.name || '',
              args: t.args || '',
              result: '',
              err: '',
              state: 'building',
            })
          }
        }
        break
      }
      case 'model_end': {
        this.modelActive = false
        this.retryInfo = null
        // 关闭本次调用所属块流的流式态（fork 关自己的）：否则下一轮正文
        // 会追加进工具调用前的旧流式块（思考直调工具时正文顺序错乱）
        const bs = ev.forkId ? this.ensureFork(ev.forkId).blocks : this.blocks
        const last = this.lastStreaming(bs)
        if (last) {
          last.streaming = false
          /* 慢消费丢帧时增量正文可能不全：以聚合帧的完整内容为准校准 */
          if (ev.data?.content) last.text = ev.data.content
          if (ev.data?.reasoning) last.reasoning = ev.data.reasoning
        } else if (!ev.forkId && (ev.data?.content || ev.data?.reasoning)) {
          // 回放重建：无流式块时按聚合帧补完整回复（实时路径 chunk 已建块）
          this.blocks.push({
            kind: 'assistant',
            uid: this.nuid(),
            text: ev.data.content || '',
            reasoning: ev.data.reasoning || '',
            streaming: false,
          })
        }
        if (!ev.forkId) {
          const u = ev.data?.usage
          if (u && this.status) {
            this.status.contextTokens = u.PromptTokens || 0
          }
        }
        break
      }
      case 'tool_start': {
        const d = ev.data || {}
        const args = typeof d.args === 'string' ? d.args : JSON.stringify(d.args ?? '')
        // 认领流式构造期（building）的同名块：换真实 callID、完整 args、转执行态
        const bs = ev.forkId ? this.ensureFork(ev.forkId).blocks : this.blocks
        // 去重：决策路径已补插过同 id 工具卡时只补名参（防重复块乱序）
        const dup = bs.find((b) => b.kind === 'tool' && b.id === d.id && b.state !== 'building')
        if (dup && dup.kind === 'tool') {
          if (!dup.name) dup.name = d.name || ''
          if (!dup.args) dup.args = args
          break
        }
        const t = bs.find((b) => b.kind === 'tool' && b.state === 'building' && b.name === d.name)
        if (t && t.kind === 'tool') {
          t.id = d.id || ''
          t.args = args
          t.state = 'running'
        } else {
          bs.push({
            kind: 'tool', uid: this.nuid(), id: d.id || '', name: d.name || '',
            args, result: '', err: '', state: 'running',
          })
        }
        if (!ev.forkId) this.lastTool = d.name || ''
        // AI 用共享工作区工具(终端/浏览器):延迟拉开对应抽屉页(见
        // autoOpenDrawers 注释——审批路径由 approve.request 取消计时,
        // 批准后才拉;名单外的查询类不拉)
        const drawer = this.drawerFor(d.name || '', args)
        if (drawer && d.id && !this.toolApprovals.has(d.id)) {
          const id = d.id
          this.toolOpenTimers.get(id) && clearTimeout(this.toolOpenTimers.get(id))
          this.toolOpenTimers.set(
            id,
            setTimeout(() => {
              this.toolOpenTimers.delete(id)
              this.openDrawer(drawer)
            }, 1000),
          )
        }
        break
      }
      case 'tool.output': {
        /* 工具执行中的实时输出（terminal 边跑边发）：按 callId 追加到
           运行中的工具块，只留末尾 20000 字符（实时窗口只显示尾部） */
        const d = ev.data || {}
        if (!d.callId || !d.text) break
        const bs = ev.forkId ? this.ensureFork(ev.forkId).blocks : this.blocks
        const t = bs.find((b) => b.kind === 'tool' && b.id === d.callId && b.state === 'running')
        if (t && t.kind === 'tool') {
          t.live = ((t.live || '') + d.text).slice(-20000)
        }
        break
      }
      case 'tool_end': {
        const d = ev.data || {}
        const bs = ev.forkId ? this.ensureFork(ev.forkId).blocks : this.blocks
        const findTool = (fn: (b: Extract<Block, { kind: 'tool' }>) => boolean) =>
          bs.find((b): b is Extract<Block, { kind: 'tool' }> => b.kind === 'tool' && fn(b))
        let t = findTool((b) => b.id === d.callId)
        // tool_start 丢失时按构建期同名块认领，避免卡片永远停在「构造中」
        if (!t) t = findTool((b) => b.state === 'building' && !!d.name && b.name === d.name)
        if (t) {
          t.id = d.callId || t.id
          t.result = d.content || ''
          t.err = d.err || ''
          t.state = 'done'
        } else {
          // 起始帧彻底丢失：补一张完成态卡片，至少保留执行痕迹与结果
          bs.push({
            kind: 'tool',
            uid: this.nuid(),
            id: d.callId || '',
            name: d.name || '',
            args: '',
            result: d.content || '',
            err: d.err || '',
            state: 'done',
          })
        }
        // 工具结果携带图片加载标记：实时渲染缩略图行（历史路径走
        // <image_loaded> user 消息，两条路径渲染同一 imgload 块）
        const marks = [...String(d.content || '').matchAll(/<image_loaded path="([^"]*)"(?: note="([^"]*)")?\s*\/>/g)]
        const loadedPaths = marks.map((m) => m[1])
        if (loadedPaths.length) {
          bs.push({ kind: 'imgload', uid: this.nuid(), paths: loadedPaths, notes: marks.map((m) => m[2] || '') })
        }
        // 记忆待确认：立即拉一次通知数据，聊天记忆卡与通知栏同源显示
        if (d.name === 'memory' && String(d.content || '').includes('"pendingId"')) void this.refreshNotices()
        if (!ev.forkId) this.lastTool = ''
        break
      }
      case 'task.start': {
        const d = ev.data || {}
        const fid = d.id || ev.forkId || ''
        const f = this.ensureFork(fid)
        f.task = d.task || ''
        f.status = 'running'
        // 入口卡紧跟对应的 task 工具卡（callId 精确匹配，回退最后一张未让位的 task 卡）；
        // 工具卡随即让位——分身入口卡是 task 的唯一呈现，避免两行重复摘要
        let ti = this.blocks.findLastIndex((b) => b.kind === 'tool' && b.id === d.callId)
        if (ti < 0) {
          ti = this.blocks.findLastIndex((b) => b.kind === 'tool' && b.name === 'task' && !b.hidden)
        }
        const card: Block = { kind: 'fork', uid: this.nuid(), forkId: fid }
        if (ti >= 0) {
          const tb = this.blocks[ti]
          if (tb.kind === 'tool') tb.hidden = true
          this.blocks.splice(ti + 1, 0, card)
        } else this.blocks.push(card)
        break
      }
      case 'task.end': {
        const fid = ev.forkId || ev.data?.id || ''
        const f = this.forks[fid]
        if (f) {
          f.status = 'done'
          f.answer = ev.data?.answer || ''
          f.stopReason = ev.data?.stopReason || ''
        }
        break
      }
      case 'mcp.auth.request': {
        const d = ev.data || {}
        const id = d.id || ''
        const server = d.server || ''
        if (!id || !server) break
        this.mcpAuthMeta[id] = d.metadata_url || ''
        // 与 approve.request 同构：工具卡补插 + 决策卡紧随其后，成时间线里一条记录
        const bs = ev.forkId ? this.ensureFork(ev.forkId).blocks : this.blocks
        if (bs.some((b) => b.kind === 'decision' && b.id === id)) break // 重连重放去重
        if (!bs.some((b) => b.kind === 'tool' && b.id === id)) {
          bs.push({ kind: 'tool', uid: this.nuid(), id, name: 'mcp_router', args: '', result: '', err: '', state: 'running' })
        }
        const card: Block = {
          kind: 'decision',
          uid: this.nuid(),
          id,
          dtype: 'mcp-auth',
          name: server,
          args: '',
          question: '',
          options: [],
          goal: '',
          context: '',
          design: '',
          progress: [],
          verify: '',
          forkId: ev.forkId || '',
          resolved: false,
          resolution: '',
        }
        const ti = bs.findIndex((b) => b.kind === 'tool' && b.id === id)
        if (ti >= 0) bs.splice(ti + 1, 0, card)
        else bs.push(card)
        void this.refreshNotices()
        break
      }
      case 'approve.request':
      case 'askuser.request':
      case 'plan.request': {
        const d = ev.data || {}
        const id = d.id || ''
        let args = d.args
        if (typeof args !== 'string') args = JSON.stringify(args ?? {})
        // term_*/browser_* 进入审批：取消免审弹板计时，等批准后再弹
        if (ev.type === 'approve.request' && this.toolOpenTimers.has(id)) {
          clearTimeout(this.toolOpenTimers.get(id))
          this.toolOpenTimers.delete(id)
          this.toolApprovals.set(id, this.drawerFor(d.name || '', args) ?? 'term')
        }
        // 分身请求路由进分身聊天框（不进主时间线）；bs=目标块数组
        const bs = ev.forkId ? this.ensureFork(ev.forkId).blocks : this.blocks
        // 去重：SSE 断线重连会重放 pending 帧
        if (!id || bs.some((b) => b.kind === 'decision' && b.id === id)) break
        // 工具卡补插：刷新/重放时本轮快照未含此调用（turn 未落盘），
        // 决策卡之前补一个执行中的工具卡
        if (!bs.some((b) => b.kind === 'tool' && b.id === id)) {
          bs.push({
            kind: 'tool',
            uid: this.nuid(),
            id,
            name: d.name || '',
            args,
            result: '',
            err: '',
            state: 'running',
          })
        }
        let question = ''
        let options: string[] = []
        let goal = ''
        let context = ''
        let design = ''
        let verify = ''
        let progress: PlanProgress[] = []
        try {
          const a = typeof args === 'string' && args ? JSON.parse(args) : {}
          question = a.question || ''
          if (Array.isArray(a.options)) options = a.options.filter((o: unknown) => typeof o === 'string')
          /* plan.request：五段式计划（目标/背景/方案/步骤/验证），解析失败留空由卡片提示 */
          goal = typeof a.goal === 'string' ? a.goal : ''
          context = typeof a.context === 'string' ? a.context : ''
          design = typeof a.design === 'string' ? a.design : ''
          verify = typeof a.verify === 'string' ? a.verify : ''
          if (Array.isArray(a.progress)) {
            progress = a.progress.filter(
              (p: any) => p && typeof p.title === 'string' && ['pending', 'running', 'completed'].includes(p.status),
            )
          }
        } catch {
          /* 非法 JSON 忽略 */
        }
        const dtype: DecisionData['dtype'] =
          ev.type === 'approve.request' ? 'approve' : ev.type === 'plan.request' ? 'plan' : 'ask'
        const card: Block = {
          kind: 'decision',
          uid: this.nuid(),
          id,
          dtype,
          name: d.name || '',
          args: typeof args === 'string' ? args : '',
          question,
          options,
          goal,
          context,
          design,
          progress,
          verify,
          forkId: ev.forkId || '',
          resolved: false,
          resolution: '',
        }
        // 决策卡紧跟对应工具卡成组展示；无对应工具卡时兜底追加末尾；
        // 通知栏由全局轮询驱动（含后台分支），不经当前分支事件流
        const ti = bs.findIndex((b) => b.kind === 'tool' && b.id === id)
        if (ti >= 0) bs.splice(ti + 1, 0, card)
        else bs.push(card)
        void this.refreshNotices()
        break
      }
      case 'error': {
        const msg = typeof ev.data === 'string' ? ev.data : JSON.stringify(ev.data ?? '')
        const bs = ev.forkId ? this.ensureFork(ev.forkId).blocks : this.blocks
        bs.push({ kind: 'assistant', uid: this.nuid(), text: msg, reasoning: '', streaming: false })
        break
      }
      case 'resource.change': {
        // 资源变更（remind 变更段推送，单一来源）：变更卡插到本轮 user 块前 +
        // 右上角 StatusCard 同步行。resource.change 不可回放（replayable 排除），
        // 断线重连靠历史 <resource_change> 消息重建。
        if (ev.forkId) break
        const items: string[] = Array.isArray(ev.data) ? ev.data : []
        if (!items.length) break
        this.liveChanges = items
        this.insertBeforeLastUser({ kind: 'resourcechange', uid: this.nuid(), items })
        // 记忆变更进通知栏（本地条目，点击打开记忆页确认）
        const memItems = items.filter((s) => /^(新增|更新|移除)(项目)?记忆 /.test(s))
        if (memItems.length) {
          this.notices = [
            {
              id: `memory-${Date.now()}`,
              rootId: this.activeId,
              kind: 'info',
              source: 'agent',
              forkId: '',
              title: '记忆已更新',
              detail: memItems.join('；'),
              time: new Date().toTimeString().slice(0, 5),
              status: 'pending',
              resolution: '',
              target: 'memory-view',
              local: true,
            },
            ...this.notices,
          ]
        }
        break
      }
      case 'status.snapshot': {
        // 分身状态快照不入主时间线、不碰主水位（分身上下文与主循环无关）
        if (ev.forkId) break
        const d = ev.data ?? null
        // 右上角实时同步：最新快照 + 上下文水位
        this.live = d
        if (d && this.status) this.status.contextTokens = d.ctxTokens || 0
        // 时间线仅水位异常时插块（send 已先本地 push user 块，插到它之前）
        if (d?.suggestCompact) {
          this.insertBeforeLastUser({ kind: 'status', uid: this.nuid(), text: '', data: d })
        }
        break
      }
      case 'plan.update': {
        // 计划是分支级状态（工具落盘后推送）；null = 已清除
        this.plan = ev.data ?? null
        break
      }
      case 'session.compacting': {
        // 水位自动压缩开始（无工具卡可见）：时间线提示压缩进行中
        const msg = typeof ev.data === 'string' ? ev.data : '上下文正在压缩归档…'
        this.blocks.push({ kind: 'note', uid: this.nuid(), text: `⇳ ${msg}` })
        break
      }
      case 'session.trimming': {
        // 水位自动整理开始：时间线提示整理进行中
        const msg = typeof ev.data === 'string' ? ev.data : '上下文正在整理…'
        this.blocks.push({ kind: 'note', uid: this.nuid(), text: `⇳ ${msg}` })
        break
      }
      case 'session.trim': {
        // 整理完成：上下文已就地折叠（marker 已入历史，重建时间线时渲染分割线）
        this.blocks.push({
          kind: 'note',
          uid: this.nuid(),
          text: `上下文已整理：早期对话折叠为摘要（${ev.data?.folded ?? '?'} 条 → 保留最近 ${ev.data?.kept ?? '?'} 条）`,
        })
        void this.refreshStatus()
        break
      }
      case 'session.compact': {
        // 归档换代：根 ID 不变（SSE/路由稳定），只换叶与上翻游标；
        // 分支列表刷新（LeafID 更新，条目数不变）
        this.live = null // 旧会话水位快照作废，状态卡按刷新后的 status 渲染
        this.liveChanges = []
        const d = ev.data || {}
        this.blocks.push({
          kind: 'note',
          uid: this.nuid(),
          text: `⇪ 话题已归档：${d.title || ''}`,
        })
        if (d.newId) {
          this.leafId = d.newId
          this.prevCursor = d.newId
          this.hasPrev = !!d.prevPath // 新叶可继续向上翻旧世代
        }
        void this.refreshStatus()
        void this.refreshBranches()
        break
      }
      case 'turn_end': {
        this.currentIter = 0
        this.busy = false
        this.modelActive = false
        this.retryInfo = null
        this.wrapPhase = false
        this.lastTool = ''
        this.pendingUserUid = 0
        this.pendingUserText = ''
        // 轮结束：残留的授权轮询随之停止（决策卡由取消/超时/完成结算移除）
        this.stopMcpAuthPolls()
        // 兜底收尾：取消路径引擎不发 model_end，流式块的打字光标须在此收掉；
        // 残留 building 工具块（模型输出了调用但引擎未执行）同样标记完成；
        // running 工具块的 tool_end 若在慢消费丢帧窗口丢掉，这里补终态
        // （否则转圈卡死——正常路径早已 done，不误伤）
        for (const bs of [this.blocks, ...Object.values(this.forks).map((f) => f.blocks)]) {
          for (const b of bs) {
            if (b.kind === 'tool' && (b.state === 'building' || b.state === 'running')) {
              if (b.state === 'running' && !b.result && !b.err) b.err = '已中断（本轮结束）'
              b.state = 'done'
            }
            if (b.kind === 'assistant' && b.streaming) b.streaming = false
          }
        }
        // 本轮已结束：当前分支残留的 pending 决策回传会被后端丢弃，标记过期；
        // 其他分支的通知不受此分支轮结束影响（全局通知，轮询各自收敛）
        for (const n of this.notices) {
          if (n.status === 'pending' && n.rootId === this.activeId) {
            n.status = 'done'
            n.resolution = '已过期'
          }
        }
        void this.refreshNotices()
        const d = ev.data || {}
        const u = d.usage
        if (u) {
          this.total = {
            prompt: this.total.prompt + (u.PromptTokens || 0),
            completion: this.total.completion + (u.CompletionTokens || 0),
            cached: this.total.cached + (u.CachedTokens || 0),
          }
        }
        this.lastStatus =
          `${d.stopReason || 'end'} · ${d.iterations ?? 0} 迭代` +
          (u ? ` · 本轮 ${u.PromptTokens}→${u.CompletionTokens} tokens（缓存 ${u.CachedTokens}）` : '')
        void this.refreshBranches() // 运行指示熄灭（后台分支靠拉取）
        // 每轮收尾：小图标实时入时间线（悬浮显示详情；持久化正文由后端 endnote 写入历史）
        {
          const secs = d.elapsedMs ? Math.round(d.elapsedMs / 1000) : 0
          const stop = d.stopReason || 'completed'
          const reason =
            stop === 'cancelled'
              ? '用户手动停止本轮'
              : stop === 'max_iterations'
                ? '达到最大迭代次数上限'
                : stop === 'error'
                  ? '执行出错中止'
                  : stop === 'aborted'
                    ? '被策略中止'
                    : `本轮结束（${stop}）`
          // 错误详情跟在原因后（endtick 超宽截断、悬浮看全文）；取消路径
          // 的 err 是 context.Canceled，无信息量不拼
          const errTxt =
            d.err && (d.stopReason || 'error') === 'error'
              ? `：${String(d.err).replace(/\s+/g, ' ').slice(0, 300)}`
              : ''
          const dur =
            secs >= 3600
              ? ` · ${Math.floor(secs / 3600)} 小时 ${Math.floor((secs % 3600) / 60)} 分钟`
              : secs >= 60
                ? ` · ${Math.floor(secs / 60)} 分 ${secs % 60} 秒`
                : secs
                  ? ` · ${secs} 秒`
                  : ''
          const title = `${reason}${errTxt} · ${d.iterations ?? 0} 轮${dur} · ${new Date().toTimeString().slice(0, 5)}`
          this.blocks.push({
            kind: 'endtick',
            uid: this.nuid(),
            icon: endIcon(reason),
            title,
          })
        }
        // 本轮已落盘：回填分叉锚点（实时轮的消息即刻可分叉，不必切页重建）
        if (!ev.forkId && this.activeId) void this.backfillForkAnchors(this.activeId)
        void this.refreshStatus()
        break
      }
    }
  }

  /* appendDelta 流式文本追加到块数组（主时间线与分身聊天框共用）。
     正文与工具同响应乱序兜底：部分 provider 按"思考→工具调用→正文"
     的顺序发增量，正文若追进旧思考块会渲染在其后工具行的上方——
     候选块之下已有本轮工具/决策行时，正文另起新块插到末尾。 */
  private appendDelta(bs: Block[], delta: string, isContent: boolean) {
    let idx = -1
    for (let i = bs.length - 1; i >= 0; i--) {
      const b = bs[i]
      if (b.kind === 'assistant') {
        if (!b.streaming) break
        idx = i
        break
      }
      if (b.kind === 'user') break
    }
    let staleGap = false
    if (idx >= 0 && isContent) {
      for (let i = idx + 1; i < bs.length; i++) {
        const k = bs[i].kind
        if (k === 'tool' || k === 'decision') {
          staleGap = true
          break
        }
        if (k === 'assistant') break
      }
    }
    if (idx < 0 || staleGap) {
      bs.push({ kind: 'assistant', uid: this.nuid(), text: '', reasoning: '', streaming: true })
      idx = bs.length - 1
    }
    const b = bs[idx] as Extract<Block, { kind: 'assistant' }>
    if (isContent) b.text += delta
    else b.reasoning += delta
  }

  /* insertBeforeLastUser 把块插到最后一个 user 块之前（status/resourcechange
     轮首系统卡的实时插入位——send 已先 push 本轮 user 块）；无 user 时尾加 */
  private insertBeforeLastUser(block: Block) {
    let idx = -1
    for (let k = this.blocks.length - 1; k >= 0; k--) {
      if (this.blocks[k].kind === 'user') {
        idx = k
        break
      }
    }
    if (idx >= 0) this.blocks.splice(idx, 0, block)
    else this.blocks.push(block)
  }

  private lastStreaming(bs: Block[]): Extract<Block, { kind: 'assistant' }> | null {
    for (let i = bs.length - 1; i >= 0; i--) {
      const b = bs[i]
      if (b.kind === 'assistant') {
        return b.streaming ? b : null
      }
      if (b.kind === 'user') return null
    }
    return null
  }
}

export const store = new AppStore()
