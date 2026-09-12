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
  type Settings,
  type SseEvent,
  type Status,
  type StatusPayload,
} from './api'

/* 输入框附件（一切皆资源）：path = 已有真身（引用，发送不复制）；
   file = 画板草稿（发送时才 stash 持久化）；两者皆空 = 拖入暂存中占位 */
export interface Attachment {
  name: string
  path?: string
  file?: File
}
export type { FileRef }

export interface ToolBlockData {
  id: string
  name: string
  args: string
  result: string
  err: string
  state: 'building' | 'running' | 'done' // building＝模型流式构造参数中
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
  dtype: 'approve' | 'ask'
  name: string
  args: string
  question: string
  options: string[]
  forkId: string
  resolved: boolean
  resolution: string
}

export interface NoticeData {
  id: string // 决策回传键（工具调用 ID）
  rootId: string // 所属分支根（跳转与决策端点路由键）
  kind: 'approve' | 'ask' | 'info'
  source: string // 'agent'、分支名或 fork 标识
  forkId: string // 非空＝分身请求：跳转打开分身抽屉而非主时间线
  title: string
  detail: string
  time: string
  status: 'pending' | 'done'
  resolution: string
  target: string // 时间线跳转锚点（decision-<id>）
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
      owner?: string
      msgIdx?: number
    }
  | { kind: 'assistant'; text: string; reasoning: string; streaming: boolean; owner?: string; msgIdx?: number }
  | { kind: 'tool' } & ToolBlockData
  | { kind: 'fork'; forkId: string }
  | { kind: 'decision' } & DecisionData
  | { kind: 'note'; text: string }
  | { kind: 'status'; text: string; data: StatusPayload | null }
  | { kind: 'reschange'; items: string[] }
  | { kind: 'endtick'; icon: string; title: string }
  | { kind: 'imgload'; paths: string[]; images: ImagePayload[] }
)

export interface TotalUsage {
  prompt: number
  completion: number
  cached: number
}

/* 路径取文件名（chips 展示用；支持 / 与 \ 两种分隔符） */
function baseName(p: string): string {
  const i = Math.max(p.lastIndexOf('/'), p.lastIndexOf('\\'))
  return i >= 0 ? p.slice(i + 1) : p
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

/* 提取 <context_trim> 摘要为一行（整理分割线文案） */
function trimText(content: string): string {
  const m = content.match(/摘要：\s*([\s\S]*?)<\/context_trim>/)
  const summary = (m?.[1] ?? '').trim().replace(/\s+/g, ' ')
  return `上下文已整理：此前的对话折叠为摘要。${summary}`
}

class AppStore {
  /* activeId = 分支根 ID（稳定：compact 换代不变，SSE 订阅/路由键）；
     leafId = 当前叶 session ID（分身存档 owner、上翻游标起点） */
  activeId = $state('')
  leafId = $state('')
  branches = $state<BranchView[]>([])
  blocks = $state<Block[]>([])
  forks = $state<Record<string, ForkState>>({})
  notices = $state<NoticeData[]>([])
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
  /* 画板草稿（图片查看器的未保存新图）：draftOpen 是「打开/续编草稿」
     请求（source = 续编底图，tag = 来源附件下标，seq 递增 = 画布重建），
     ResourcePane 消费；草稿 tab 全局唯一，重进即以 chip 当前内容重建 */
  draftOpen = $state<{ source: File | null; tag: string; seq: number } | null>(null)
  private draftSeq = 0
  /* 待回流附件（查看器「添加到对话」/拖入暂存 att.stashed）：ChatView
     消费进输入框附件（tag 匹配且身份一致时原位替换，否则追加） */
  pendingAttachments = $state<(Attachment & { tag?: string; source?: File | null })[] | null>(null)
  /* 文件页「添加到对话」的待回流引用（路径+标注片段，行号已算好）；
     ChatView 消费进输入框引用 chips（同 path 替换去重） */
  pendingFileRef = $state<FileRef | null>(null)
  /* 图片写回后的缩略图版本（按路径 bump：同 URL 的 img 立即换 src，
     含历史 chips——引用同一资源，处处显示最新；跨会话由 HTTP 304 兜底） */
  imgVer = $state<Record<string, number>>({})
  /* 共享终端抽屉(独立于画板 overlay,与聊天并存):收起仅滑出,
     WS/xterm 常驻保活。termFocus 是外部请求定位的终端 id
     （supper_url term:// 点击入口；TerminalTab 消费后清空） */
  termDrawerOpen = $state(false)
  termFocus = $state('')
  /* drawerTool 是抽屉内工具页（终端/文件互斥显隐，双 pane 常驻保活）；
     fileFocus 是待打开的文件路径（file:// 入口，ResourcePane 消费后清空） */
  drawerTool = $state<'term' | 'file'>('term')
  fileFocus = $state('')
  private boardTag = ''
  /* 模型调用进行中（model_start→model_end），思考指示用 */
  modelActive = $state(false)

  status = $state<Status | null>(null)
  /* 最新 agent_status 快照（status.snapshot 事件实时更新，右上角水位条数据源） */
  live = $state<StatusPayload | null>(null)
  /* 本轮资源变更条目（res.change 事件更新，右上角 StatusCard 数据源；
     loop_start 清空——事件按需推送，不清会滞留上一轮的旧变更） */
  liveChanges = $state<string[]>([])
  settings = $state<Settings | null>(null)
  total = $state<TotalUsage>({ prompt: 0, completion: 0, cached: 0 })

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
  运行,提前弹出只是打扰。浏览器工具不弹抽屉:共见由 desktop 壳的
  浏览器窗口承担(AI start 时自动弹出)。 */
  private autoOpenDrawers = new Map<string, 'term'>([
    ['term_start', 'term'],
    ['term_send', 'term'],
  ])
  private toolOpenTimers = new Map<string, ReturnType<typeof setTimeout>>()
  private toolApprovals = new Map<string, 'term'>()
  /* 本轮本地已 push 的 user 块（send 时记录，turn_end/replay.sync 清除）：
  loop_start 到达时同文本跳过（实时路径防双 push）；SSE 重连回放时按 uid
  截断本地本轮块，让整轮回放帧干净重建（防 user/回复块重复） */
  private pendingUserUid = 0
  private pendingUserText = ''

  private nuid(): number {
    return ++this.uidSeq
  }

  /* ── 启动 ── */

  async bootstrap() {
    const b = await api.bootstrap()
    this.activeId = b.sessionId
    this.branches = b.branches ?? []
    this.settings = b.settings
    this.status = b.status
    await this.loadHistory()
    this.unsub?.()
    this.unsub = subscribe(this.activeId, (ev) => this.apply(ev))
    this.refreshNotices()
    setInterval(() => this.refreshNotices(), 3000)
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
    this.unsub = subscribe(this.activeId, (ev) => this.apply(ev))
  }

  private async loadHistory() {
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
    this.lastTool = ''
    this.live = null
    this.liveChanges = []
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
      this.busy = s.busy
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
  private buildBlocks(messages: HistoryMessage[], decisions?: DecisionRecord[], forks?: ForkSummary[], owner?: string): Block[] {
    const dmap = new Map((decisions || []).map((d) => [d.callId, d.resolution]))
    const forkQueue = [...(forks || [])]
    const out: Block[] = []
    for (let mi = 0; mi < messages.length; mi++) {
      const m = messages[mi]
      if (m.role === 'user') {
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
        } else if (m.content.includes('<res_change>')) {
          // 资源变更记录：remind 变更段按需插入（实时由 res.change 事件渲染）
          const items = [...m.content.matchAll(/^- (.+)$/gm)].map((x) => x[1].trim()).filter(Boolean)
          if (items.length) out.push({ kind: 'reschange', uid: this.nuid(), items })
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
          // read_file 图片已进上下文：渲染为缩略图小行（paths 在标签体内，每行一个）
          const inner = m.content.replace(/^[\s\S]*?<image_loaded>|<\/image_loaded>[\s\S]*$/g, '')
          const paths = inner
            .split('\n')
            .map((l) => l.trim())
            .filter(Boolean)
          out.push({ kind: 'imgload', uid: this.nuid(), paths, images: m.images || [] })
        } else if (m.content.includes('<end_reason>')) {
          const detail = endReasonText(m.content)
          if (detail) out.push({ kind: 'endtick', uid: this.nuid(), icon: endIcon(detail), title: detail })
        } else if (m.content.includes('<context_trim')) {
          out.push({ kind: 'note', uid: this.nuid(), text: `✂️ ${trimText(m.content)}` })
        } else if (!m.content.trim() && !m.images?.length) {
          // 空输入（纯附件轮，引用已由上一块独立呈现）：不渲染
        } else {
          out.push({ kind: 'user', uid: this.nuid(), text: m.content, images: m.images, owner, msgIdx: mi })
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
            msgIdx: mi,
          })
        }
        // 展开工具调用：名称与参数来自 tool_calls（Args 序列化后是嵌套对象，非字符串）
        for (const tc of m.tool_calls || []) {
          out.push({
            kind: 'tool',
            uid: this.nuid(),
            id: tc.ID || '',
            name: tc.Name || '',
            args: typeof tc.Args === 'string' ? tc.Args : JSON.stringify(tc.Args ?? ''),
            result: '',
            err: '',
            state: 'done',
            decision: dmap.get(tc.ID || ''),
          })
          // task 调用紧随分身入口卡（真实启动的 fork 才有摘要：审批拒绝/失败的不插）
          if (tc.Name === 'task' && forkQueue.length > 0) {
            const f = forkQueue.shift()!
            out.push({ kind: 'fork', uid: this.nuid(), forkId: f.id })
          }
        }
      } else if (m.role === 'tool') {
        // 按调用 ID 回填结果到对应工具块
        const target = [...out].reverse().find((b) => b.kind === 'tool' && b.id === m.tool_call_id)
        if (target && target.kind === 'tool') {
          target.result = m.content || ''
          target.err = m.err || ''
        } else {
          out.push({
            kind: 'tool',
            uid: this.nuid(),
            id: m.tool_call_id || '',
            name: '',
            args: '',
            result: m.content || '',
            err: m.err || '',
            state: 'done',
          })
        }
      }
    }
    return out
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

  /* openDraftImage 打开画板草稿（画笔钮/草稿 chip 续编/旧 base64 图片）：
     source = 续编底图（无则空白画布），tag = 来源附件下标（「添加到
     对话」时据此替换原 chip）。seq 每次递增 = 画布重建（草稿 tab 唯一，
     重进即以 chip 当前内容重建）。 */
  openDraftImage(source: File | null = null, tag = '') {
    this.draftOpen = { source, tag, seq: ++this.draftSeq }
    this.drawerTool = 'file'
    this.termDrawerOpen = true
  }

  /* bumpImg 图片写回后按路径递增缩略图版本（同 URL 的 img 立即换 src，
     含聊天历史 chips——引用同一资源，处处显示最新）。 */
  bumpImg(path: string) {
    this.imgVer = { ...this.imgVer, [path]: (this.imgVer[path] ?? 0) + 1 }
  }

  /* openBase64Draft 把无路径的内存图片（消息内 base64，无工作目录真身）转草稿：
     dataURL → File → openDraftImage。 */
  async openBase64Draft(src: string, name: string) {
    try {
      const r = await fetch(src)
      if (!r.ok) throw new Error('read fail')
      const blob = await r.blob()
      this.openDraftImage(new File([blob], name || 'image.png', { type: blob.type || 'image/png' }))
    } catch {
      this.lastStatus = '图片读取失败，未能打开画板'
    }
  }

  /* openDrawer 拉开共享终端抽屉到终端页（AI 工具实际执行时调用；
     抽屉与聊天并存，不打断当前视图）。 */
  private openDrawer(tool: 'term') {
    this.drawerTool = tool
    this.termDrawerOpen = true
  }

  /* openTermAt 拉开终端抽屉并定位到指定终端（supper_url term:// 点击；
     首次打开时 WS hello 到达后由 TerminalTab 消费定位）。 */
  openTermAt(id: string) {
    this.termFocus = id
    this.drawerTool = 'term'
    this.termDrawerOpen = true
  }

  /* openFileAt 拉开抽屉文件页并打开指定文件（supper_url file:// 点击）。 */
  openFileAt(path: string) {
    if (!path) return
    this.fileFocus = path
    this.drawerTool = 'file'
    this.termDrawerOpen = true
  }

  /* openBrowserAt 唤起浏览器窗口并定位到指定标签（supper_url browser://
     点击）；浏览器是 desktop 资产，web 端提示降级。 */
  openBrowserAt(id: string) {
    if (!id) return
    const browserApi = (window as any).ez?.browser
    if (browserApi) browserApi.focusTab(id)
    else this.lastStatus = '浏览器窗口仅桌面端可用'
  }

  closeTermDrawer() {
    this.termDrawerOpen = false
  }

  /* 工具入口 mini 钮的开关语义：开着且已是该工具页 → 收起；
     否则切到该工具页并拉开 */
  toggleDrawerTool(t: 'term' | 'file') {
    if (this.termDrawerOpen && this.drawerTool === t) this.termDrawerOpen = false
    else {
      this.drawerTool = t
      this.termDrawerOpen = true
    }
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

  /* ── 发送 / 取消 ── */

  /* 打断式发送：运行中再来指令 = 先终止当前轮（等引擎真正退出，含工具树杀），
     再执行新指令；等待超时则放弃并提示。附件/引用统一为工作目录路径
    （<reference_file> 记录告知模型）；画板草稿（path 空有 file）此时才
     stash 持久化——失败则原样保留输入框内容。 */
  async send(text: string, attachments: Attachment[] = [], fileRefs: FileRef[] = []) {
    if (!this.activeId || (!text.trim() && !attachments.length && !fileRefs.length)) return
    if (attachments.some((a) => !a.path && !a.file)) {
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
    let final: FilePayload[] = attachments.map((a) => ({ name: a.name, path: a.path! }))
    const drafts = attachments.filter((a) => !a.path && a.file)
    if (drafts.length) {
      try {
        const paths = await api.stash(drafts.map((d) => d.file!))
        const by = new Map(drafts.map((d, i) => [d, paths[i]]))
        final = attachments.map((a) => (by.has(a) ? { name: a.name, path: by.get(a)! } : { name: a.name, path: a.path! }))
      } catch (e) {
        this.lastStatus = `画板草稿暂存失败：${(e as Error).message}`
        return
      }
    }
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
    this.settings = s
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
      this.activeId = r.id
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
      this.activeId = r.id
      await this.loadHistory()
      this.resubscribe()
      await this.refreshStatus()
      await this.refreshBranches()
    } catch (e) {
      this.lastStatus = `新建分支失败：${(e as Error).message}`
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

  /* 从任意消息分叉（Copy）：复制源会话 [0, msgIdx]（含选中消息）开新线 */
  async forkFrom(owner: string, msgIdx: number) {
    try {
      const r = await api.forkSession(owner, msgIdx + 1)
      this.activeId = r.id
      await this.loadHistory()
      this.resubscribe()
      await this.refreshStatus()
      await this.refreshBranches()
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
          try {
            const a = it.args ? JSON.parse(it.args) : {}
            question = a.question || ''
          } catch {
            /* 非法 JSON 忽略 */
          }
          out.push({
            id: it.callId,
            rootId: g.rootId,
            kind: it.kind,
            source: it.forkId ? '分身' : name || 'agent',
            forkId: it.forkId || '',
            title: it.tool,
            detail: question,
            time: new Date(it.ts || Date.now()).toTimeString().slice(0, 5),
            status: 'pending',
            resolution: '',
            target: `decision-${it.callId}`,
          })
        }
      }
      const same =
        out.length === this.notices.length &&
        out.every((n, i) => {
          const c = this.notices[i]
          return c.id === n.id && c.rootId === n.rootId && c.detail === n.detail && c.time === n.time
        })
      if (!same) this.notices = out
    } catch {
      /* 后端不可达静默（下一轮重试） */
    }
  }

  /* resolveNoticeGlobal 通知栏内联决策：按通知携带的分支直接回传（不依赖
  当前分支的时间线）。乐观移除在前（点击即消失），失败则刷新恢复真实状态。 */
  async resolveNoticeGlobal(n: NoticeData, action: string, input?: string) {
    this.notices = this.notices.filter((x) => !(x.id === n.id && x.rootId === n.rootId))
    try {
      if (n.kind === 'approve') await api.decideApprove(n.rootId, n.id, action === 'approve', '')
      else await api.decideAnswer(n.rootId, n.id, input ?? '')
    } catch {
      await this.refreshNotices()
    }
  }

  /* jumpToNotice 跳转到通知来源：跨分支先切换（等待历史加载），分身请求
  打开抽屉定位，主时间线经 jumpMain 锚点由 ChatView 滚动。 */
  async jumpToNotice(n: NoticeData) {
    if (!n.target) return
    if (n.rootId && n.rootId !== this.activeId) await this.switchBranch(n.rootId)
    if (n.forkId) {
      this.openFork(n.forkId, n.id)
      return
    }
    this.jumpMain = n.target
  }

  dismissNotice(id: string) {
    this.notices = this.notices.filter((x) => x.id !== id)
  }

  /* ── 事件归约 ── */

  apply(ev: SseEvent) {
    this.tick++
    switch (ev.type) {
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
        /* 回放重建：本轮 user 输入（实时路径 send 已本地 push，同文本去重）。
        主轮带引用时载荷是对象 {text, files, fileRefs}（后端 Publish 附带——
        本轮消息要等轮结束才并入历史，运行中切回分支全靠回放帧重建 chips） */
        const d = ev.data
        const obj = d && typeof d === 'object' ? d : null
        const text = typeof d === 'string' ? d : (obj?.text ?? '')
        // 新一轮开始：清空上一轮的资源变更（右上角只挂本轮）（res.change 按需推送不自动清）
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
            this.blocks.push({ kind: 'user', uid, text })
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
      case 'model_end': {
        this.modelActive = false
        // 关闭本次调用所属块流的流式态（fork 关自己的）：否则下一轮正文
        // 会追加进工具调用前的旧流式块（思考直调工具时正文顺序错乱）
        const bs = ev.forkId ? this.ensureFork(ev.forkId).blocks : this.blocks
        const last = this.lastStreaming(bs)
        if (last) {
          last.streaming = false
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
        const drawer = this.autoOpenDrawers.get(d.name || '')
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
      case 'tool_end': {
        const d = ev.data || {}
        const bs = ev.forkId ? this.ensureFork(ev.forkId).blocks : this.blocks
        const t = bs.find((b) => b.kind === 'tool' && b.id === d.callId)
        if (t && t.kind === 'tool') {
          t.result = d.content || ''
          t.err = d.err || ''
          t.state = 'done'
        }
        // 工具结果携带图片加载标记：实时渲染缩略图行（历史路径走
        // <image_loaded> user 消息，两条路径渲染同一 imgload 块）
        const loadedPaths = [...String(d.content || '').matchAll(/<image_loaded path="([^"]*)"\s*\/>/g)].map((m) => m[1])
        if (loadedPaths.length) {
          bs.push({ kind: 'imgload', uid: this.nuid(), paths: loadedPaths, images: [] })
        }
        if (!ev.forkId) this.lastTool = ''
        break
      }
      case 'task.start': {
        const d = ev.data || {}
        const fid = d.id || ev.forkId || ''
        const f = this.ensureFork(fid)
        f.task = d.task || ''
        f.status = 'running'
        // 入口卡紧跟对应的 task 工具卡（callId 精确匹配，回退最后一张 task 卡）
        let ti = this.blocks.findLastIndex((b) => b.kind === 'tool' && b.id === d.callId)
        if (ti < 0) ti = this.blocks.findLastIndex((b) => b.kind === 'tool' && b.name === 'task')
        const card: Block = { kind: 'fork', uid: this.nuid(), forkId: fid }
        if (ti >= 0) this.blocks.splice(ti + 1, 0, card)
        else this.blocks.push(card)
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
      case 'approve.request':
      case 'askuser.request': {
        const d = ev.data || {}
        const id = d.id || ''
        // term_*/browser_* 进入审批：取消免审弹板计时，等批准后再弹
        if (ev.type === 'approve.request' && this.toolOpenTimers.has(id)) {
          clearTimeout(this.toolOpenTimers.get(id))
          this.toolOpenTimers.delete(id)
          this.toolApprovals.set(id, this.autoOpenDrawers.get(d.name || '') ?? 'term')
        }
        // 分身请求路由进分身聊天框（不进主时间线）；bs=目标块数组
        const bs = ev.forkId ? this.ensureFork(ev.forkId).blocks : this.blocks
        // 去重：SSE 断线重连会重放 pending 帧
        if (!id || bs.some((b) => b.kind === 'decision' && b.id === id)) break
        let args = d.args
        if (typeof args !== 'string') args = JSON.stringify(args ?? {})
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
        try {
          const a = typeof args === 'string' && args ? JSON.parse(args) : {}
          question = a.question || ''
          if (Array.isArray(a.options)) options = a.options.filter((o: unknown) => typeof o === 'string')
        } catch {
          /* 非法 JSON 忽略 */
        }
        const dtype: DecisionData['dtype'] = ev.type === 'approve.request' ? 'approve' : 'ask'
        const card: Block = {
          kind: 'decision',
          uid: this.nuid(),
          id,
          dtype,
          name: d.name || '',
          args: typeof args === 'string' ? args : '',
          question,
          options,
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
        bs.push({ kind: 'assistant', uid: this.nuid(), text: `⚠️ ${msg}`, reasoning: '', streaming: false })
        break
      }
      case 'res.change': {
        // 资源变更（remind 变更段推送，单一来源）：变更卡插到本轮 user 块前 +
        // 右上角 StatusCard 同步行。res.change 不可回放（replayable 排除），
        // 断线重连靠历史 <res_change> 消息重建。
        if (ev.forkId) break
        const items: string[] = Array.isArray(ev.data) ? ev.data : []
        if (!items.length) break
        this.liveChanges = items
        this.insertBeforeLastUser({ kind: 'reschange', uid: this.nuid(), items })
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
          text: `✂️ 上下文已整理：早期对话折叠为摘要（${ev.data?.folded ?? '?'} 条 → 保留最近 ${ev.data?.kept ?? '?'} 条）`,
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
        this.busy = false
        this.modelActive = false
        this.lastTool = ''
        this.pendingUserUid = 0
        this.pendingUserText = ''
        // 兜底收尾：取消路径引擎不发 model_end，流式块的打字光标须在此收掉；
        // 残留 building 工具块（模型输出了调用但引擎未执行）同样标记完成
        for (const bs of [this.blocks, ...Object.values(this.forks).map((f) => f.blocks)]) {
          for (const b of bs) {
            if (b.kind === 'tool' && b.state === 'building') b.state = 'done'
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

  /* insertBeforeLastUser 把块插到最后一个 user 块之前（status/reschange
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
