<script lang="ts">
  import { onMount } from 'svelte'
  import {
    api,
    type HistoryMessage,
    type LongtermMemory,
    type MemoryConfig,
    type MemoryItem,
    type MemoryProjectDetail,
    type MemorySkillContent,
    type MemorySkillEntry,
    type SessionNode,
    type TraceSpan,
  } from '../lib/api'
  import { store } from '../lib/store.svelte'

  /*
  记忆 = 长期记忆/能力记忆两个文件夹（GET /api/memory/config）+ 会话树
  （GET /api/memory/tree）：话题记忆展示完整会话树——每条分支（线）从根
  到当前叶，compact 旧世代标"已归档"，fork 分叉可见。树按层懒加载：
  根层分页，子代展开时再拉，列表查询只读元数据。操作：回顾（只读展开）、
  切到分支（跳对话页）、手动归档、删整条线。
  */
  let { onNavigate }: { onNavigate?: (v: string) => void } = $props()

  let cfg = $state<MemoryConfig | null>(null)
  let message = $state('')

  /* 会话树按层懒加载：根层每页 TREE_PAGE 条（“加载更多”翻页），
     子代点开哪层拉哪层；已拉过的层缓存在 tree 里，收起再展开不重拉。 */
  const TREE_PAGE = 30
  let tree = $state<SessionNode[] | null>(null) // 已加载节点（按层/页累积）
  let rootsLoaded = $state(false)
  let rootNext = $state('')
  let rootTotal = $state(0)
  let loadedChildren = $state<Set<string>>(new Set())
  let loadingParent = $state<string | null>(null) // 正在拉哪一层（'' = 根层）
  let treeErr = $state('')

  async function loadRoots(more = false) {
    if (loadingParent !== null || (more && !rootNext)) return
    loadingParent = ''
    try {
      const page = await api.getMemoryTree('', more ? rootNext : '', TREE_PAGE)
      const have = new Set((tree ?? []).map((n) => n.id))
      const incoming = page.nodes.filter((n) => !have.has(n.id))
      tree = more ? [...(tree ?? []), ...incoming] : incoming
      rootNext = page.next ?? ''
      rootTotal = page.total ?? (tree?.length ?? 0)
      rootsLoaded = true
      treeErr = ''
    } catch {
      treeErr = '会话树加载失败（后端不可达）'
    } finally {
      loadingParent = null
    }
  }

  async function loadChildren(parent: string): Promise<boolean> {
    if (loadingParent !== null || loadedChildren.has(parent)) return false
    loadingParent = parent
    try {
      const page = await api.getMemoryTree(parent)
      const have = new Set((tree ?? []).map((n) => n.id))
      // 子代数以本层实际返回为准（期间删除节点时箭头随之收敛）
      tree = [
        ...(tree ?? []).map((n) => (n.id === parent ? { ...n, childCount: page.nodes.length } : n)),
        ...page.nodes.filter((n) => !have.has(n.id)),
      ]
      loadedChildren = new Set(loadedChildren).add(parent)
      return true
    } catch {
      treeErr = '会话树加载失败（后端不可达）'
      return false
    } finally {
      loadingParent = null
    }
  }

  /* 回顾侧边抽屉：目标节点 + 双页签——「消息」= 只读全文 + 压缩摘要，
     「调用链」= 该会话 trace.jsonl 的 otel span（瀑布图 + 汇总） */
  let reviewNode = $state<SessionNode | null>(null)
  let reviewTab = $state<'msgs' | 'trace'>('msgs')
  let reviewMsgs = $state<HistoryMessage[]>([])
  let reviewSummary = $state('')
  let reviewSystem = $state('')
  let reviewSpans = $state<TraceSpan[]>([])
  let reviewLoading = $state(false)
  let sysOpen = $state(false) // 系统提示词折叠态（换节点/关抽屉时复位）
  let pickedSpan = $state<TraceSpan | null>(null)
  /* 瀑布图上的分身调用链默认折起：fork 内部的模型/工具调用属于细节，
     默认只留一行标识，需要时再展开 */
  let openForks = $state<Set<string>>(new Set())

  /* 顶栏搜索跳转：滚动到能力记忆分区（消费后清空锚点） */
  $effect(() => {
    const anchor = store.jumpAnchor
    if (!anchor.startsWith('memory:')) return
    store.jumpAnchor = ''
    const el = document.getElementById(`mem-${anchor.slice('memory:'.length)}`)
    requestAnimationFrame(() => el?.scrollIntoView({ block: 'start', behavior: 'smooth' }))
  })

  /* 三段数据互不依赖：并发拉取；会话树先拉根层第一页，子代按需展开 */
  onMount(() => {
    void loadConfig()
    void loadRoots()
    void loadLongterm()
  })

  async function loadConfig() {
    try {
      cfg = await api.getMemoryConfig()
    } catch {
      message = '记忆数据加载失败（后端不可达）'
    }
  }

  /* ── 长期记忆（条目式，与 memory 工具同一存储）：user/soul 条目面板
     + 项目卡片，直接编辑保存（整组回写）── */

  let longterm = $state<LongtermMemory | null>(null)
  const panels: Array<{ target: 'user' | 'soul'; label: string; desc: string; empty: string }> = [
    { target: 'user', label: '用户信息', desc: '关于用户：偏好、背景、工作环境与长期约定。', empty: '暂无条目——对话中 agent 会把值得保留的用户信息沉淀进来。' },
    {
      target: 'soul',
      label: 'agent soul',
      desc: '关于 agent 自己：性格、表达方式、工作方式与协作原则——用户对你的期望，不是项目知识。',
      empty: '暂无条目——对话中 agent 会把用户对自己的要求沉淀进来。',
    },
  ]
  let editing = $state<{ target: 'user' | 'soul'; index: number; text: string } | null>(null)
  let adding = $state<{ target: 'user' | 'soul'; text: string } | null>(null)
  let memBusy = $state(false)

  async function loadLongterm() {
    try {
      longterm = await api.getLongterm()
    } catch {
      message = '长期记忆加载失败（后端不可达）'
    }
  }

  function itemsOf(target: 'user' | 'soul'): MemoryItem[] {
    return (longterm?.[target] ?? []) as MemoryItem[]
  }

  /* 条目更新距今（列表展示；空/非法时间返回空串） */
  function relTime(s: string): string {
    const t = s ? new Date(s).getTime() : 0
    if (!t || Number.isNaN(t)) return ''
    const d = Date.now() - t
    const min = 60_000
    const hour = 60 * min
    const day = 24 * hour
    if (d < min) return '刚刚'
    if (d < hour) return `${Math.floor(d / min)} 分钟前`
    if (d < day) return `${Math.floor(d / hour)} 小时前`
    if (d < 30 * day) return `${Math.floor(d / day)} 天前`
    if (d < 365 * day) return `${Math.floor(d / (30 * day))} 个月前`
    return `${Math.floor(d / (365 * day))} 年前`
  }

  async function saveItems(target: 'user' | 'soul', items: MemoryItem[]) {
    if (memBusy) return
    memBusy = true
    try {
      const r = await api.saveLongtermItems(target, items)
      if (longterm) longterm[target] = r.items || items
    } catch (e) {
      message = `记忆保存失败：${(e as Error).message}`
    } finally {
      memBusy = false
    }
  }

  function commitEdit() {
    if (!editing || !longterm) return
    const { target, index } = editing
    const items = [...itemsOf(target)]
    if (!items[index]) return
    const text = editing.text.trim()
    if (!text) {
      message = '条目内容不能为空（删除请用条目上的 ✕）'
      return
    }
    items[index] = { ...items[index], text }
    editing = null
    void saveItems(target, items)
  }

  function removeItem(target: 'user' | 'soul', index: number) {
    const items = itemsOf(target).filter((_, i) => i !== index)
    void saveItems(target, items)
  }

  function commitAdd() {
    if (!adding || !longterm) return
    const text = adding.text.trim()
    if (!text) {
      adding = null
      return
    }
    const target = adding.target
    adding = null
    void saveItems(target, [...itemsOf(target), { id: '', text, updatedAt: '' }])
  }

  /* 条目正文默认收三行：超长记忆不再撑高面板，也不在行内出现滚动条。
     展开态按条目 key 记忆；overflow 只在折叠态测量，展开时沿用上次结果 */
  let expandedMem = $state<Set<string>>(new Set())
  let overflowMem = $state<Record<string, boolean>>({})

  function memKey(target: 'user' | 'soul', it: MemoryItem, i: number): string {
    return `${target}:${it.id || `#${i}`}`
  }

  function toggleMem(key: string) {
    const next = new Set(expandedMem)
    if (next.has(key)) next.delete(key)
    else next.add(key)
    expandedMem = next
  }

  function trackOverflow(node: HTMLElement, p: { key: string; expanded: boolean }) {
    let cur = p
    const run = () => {
      if (cur.expanded) return
      const over = node.scrollHeight > node.clientHeight + 1
      if (!!overflowMem[cur.key] !== over) overflowMem = { ...overflowMem, [cur.key]: over }
    }
    run()
    const ro = new ResizeObserver(run)
    ro.observe(node)
    return {
      update(next: typeof p) {
        cur = next
        run()
      },
      destroy() {
        ro.disconnect()
      },
    }
  }

  /* ── 项目记忆：卡片列表 + 展开编辑全文 ── */

  let projectDraft = $state<MemoryProjectDetail | null>(null)
  let projectNew = $state(false)

  function closeProject() {
    projectDraft = null
    projectNew = false
  }

  async function openProject(id: string) {
    if (projectDraft?.id === id) {
      projectDraft = null
      return
    }
    try {
      projectDraft = await api.getMemoryProject(id)
      projectNew = false
    } catch (e) {
      message = `项目记忆读取失败：${(e as Error).message}`
    }
  }

  async function commitProject() {
    if (!projectDraft || memBusy) return
    memBusy = true
    try {
      await api.saveMemoryProject({
        id: projectNew ? '' : projectDraft.id,
        name: projectDraft.name,
        description: projectDraft.description,
        content: projectDraft.content,
      })
      projectDraft = null
      projectNew = false
      await loadLongterm()
    } catch (e) {
      message = `项目记忆保存失败：${(e as Error).message}`
    } finally {
      memBusy = false
    }
  }

  async function removeProject(id: string) {
    if (memBusy) return
    memBusy = true
    try {
      await api.deleteMemoryProject(id)
      if (projectDraft?.id === id) {
        projectDraft = null
        projectNew = false
      }
      await loadLongterm()
    } catch (e) {
      message = `项目删除失败：${(e as Error).message}`
    } finally {
      memBusy = false
    }
  }

  /* 回顾：侧边抽屉只读全文；压缩新叶几乎无消息，摘要段给上下文。
     调用链不在打开时预取，切到“调用链”页签再读（大 trace 不拖慢消息回顾） */
  let traceLoaded = $state(false)
  let traceLoading = $state(false)
  /* 请求序号：抽屉快速开关/切换节点时，旧响应不得覆盖新节点 */
  let reviewSeq = 0

  async function reviewTopic(n: SessionNode) {
    const seq = ++reviewSeq
    reviewNode = n
    reviewTab = 'msgs'
    sysOpen = false
    pickedSpan = null
    openForks = new Set()
    reviewSpans = []
    traceLoaded = false
    traceLoading = false
    reviewLoading = true
    try {
      const d = await api.getTopic(n.id)
      if (seq !== reviewSeq) return
      reviewMsgs = d.messages || []
      reviewSummary = (d.summary || '').replace(/<\/?compact-summary>/g, '').trim()
      reviewSystem = d.systemPrompt || ''
    } catch (e) {
      if (seq === reviewSeq) message = `回顾失败：${(e as Error).message}`
    } finally {
      if (seq === reviewSeq) reviewLoading = false
    }
  }

  async function openTraceTab() {
    reviewTab = 'trace'
    if (!reviewNode || traceLoaded || traceLoading) return
    const seq = reviewSeq
    const id = reviewNode.id
    traceLoading = true
    try {
      const spans = await api.getTopicTrace(id)
      if (seq !== reviewSeq) return
      reviewSpans = spans
    } catch {
      if (seq === reviewSeq) reviewSpans = []
    } finally {
      if (seq === reviewSeq) {
        traceLoading = false
        traceLoaded = true
      }
    }
  }

  function closeReview() {
    reviewSeq++
    reviewNode = null
    reviewMsgs = []
    reviewSummary = ''
    reviewSystem = ''
    sysOpen = false
    reviewSpans = []
    pickedSpan = null
    openForks = new Set()
    traceLoaded = false
    traceLoading = false
  }

  /* ── 调用链视图：按轮分块的瀑布图与汇总 ── */

  /* 一轮 = 一个根 span + 其子 span；子 span 相对本块起点定位 */
  type TraceKid = { sp: TraceSpan; left: number; width: number }
  type TraceBlock = {
    root: TraceSpan
    start: number
    dur: number
    kids: TraceKid[]
    /* 挂在本块某一子 span 行下的 fork 块（分身自带局部时间轴，不并进父条） */
    nested: TraceBlock[]
    anchor: string // 挂在哪个子 span 行下
  }

  /* 空档压缩后最多占块宽的比例（与条的最小宽度同量级，够看出是空转即可） */
  const GAP_WIDTH = 0.005

  /*
  块内时间轴：没有被任何子 span 覆盖的空档（审批等待之后的空转、轮内的
  长停顿）压成细缝，其余段保持线性——直接用墙钟比例时，一段长空转会
  把真正干活的 span 全挤成左边的点。映射单调，故跨空档的条仍与相邻条
  对齐。
  */
  function traceAxis(start: number, end: number, kids: TraceSpan[]): (t: number) => number {
    const cap = Math.max((end - start) * GAP_WIDTH, 1)
    const cuts = new Set<number>([start, end])
    for (const k of kids) {
      for (const t of [k.start, k.end ?? k.start]) {
        if (t > start && t < end) cuts.add(t)
      }
    }
    const at = [...cuts].sort((a, b) => a - b)
    const segs: { from: number; to: number; weight: number }[] = []
    let total = 0
    for (let i = 0; i + 1 < at.length; i++) {
      const from = at[i]
      const to = at[i + 1]
      const gap = !kids.some((k) => k.start < to && (k.end ?? k.start) > from)
      const weight = gap ? Math.min(to - from, cap) : to - from
      segs.push({ from, to, weight })
      total += weight
    }
    if (total <= 0) return () => 0
    return (t: number) => {
      let acc = 0
      for (const s of segs) {
        if (t >= s.to) {
          acc += s.weight
          continue
        }
        return t <= s.from ? acc : acc + (s.weight * (t - s.from)) / (s.to - s.from)
      }
      return total
    }
  }

  /*
  轮次分块：根 span（无 parentId）为一块，子 span 相对本块起点定位——轮与
  轮之间有大段空闲，用整条 trace 的墙钟做轴会把每轮压成一条线。fork 根
  也算一块：分身有自己的局部时间轴，缩进挂在发起它的 task 行下。未闭合
  且无子节点的根没有任何内容可看，跳过。
  */
  const traceBlocks = $derived.by(() => {
    const spans = reviewSpans
    if (!spans.length) return { tops: [] as TraceBlock[], all: [] as TraceBlock[] }
    const kids = new Map<string, TraceSpan[]>()
    for (const s of spans) {
      if (s.parentId) kids.set(s.parentId, [...(kids.get(s.parentId) || []), s])
    }
    const all: TraceBlock[] = []
    for (const root of spans.filter((s) => !s.parentId || s.kind === 'fork').sort((a, b) => a.start - b.start)) {
      const children = (kids.get(root.spanId) || []).sort((a, b) => a.start - b.start)
      if (!root.end && children.length === 0) continue
      const end = root.end ?? Math.max(...children.map((c) => c.end ?? c.start))
      const axis = traceAxis(root.start, end, children)
      const axisTotal = axis(end) || 1
      all.push({
        root,
        start: root.start,
        dur: Math.max(end - root.start, 1),
        kids: children.map((sp) => {
          const left = (axis(sp.start) / axisTotal) * 100
          return {
            sp,
            left,
            width: Math.max((axis(sp.end ?? sp.start) / axisTotal) * 100 - left, 0.6),
          }
        }),
        nested: [],
        anchor: '',
      })
    }
    // fork 认领：parentId 指向发起它的 task 工具 span（后端按序号配对写入）
    const tops: TraceBlock[] = []
    for (const b of all) {
      const host = b.root.parentId
        ? all.find((x) => x.kids.some((k) => k.sp.spanId === b.root.parentId))
        : undefined
      if (!host) {
        tops.push(b)
        continue
      }
      host.nested.push(b)
      b.anchor = b.root.parentId!
    }
    return { tops, all }
  })

  /*
  汇总：耗时取各块时长之和（不含块间空闲），token 取 model span 的 attrs。
  轮次只数主循环 turn，子代理（fork 根）单独计——两者都算进耗时与调用数
  （fork 块缩进挂在 task 行下，统计仍遍历全部块）。
  */
  const traceSummary = $derived.by(() => {
    let work = 0
    let turns = 0
    let agents = 0
    let models = 0
    let prompt = 0
    let completion = 0
    let cached = 0
    const tools: TraceSpan[] = []
    for (const t of traceBlocks.all) {
      work += t.dur
      if (t.root.kind === 'fork') agents++
      else turns++
      for (const k of t.kids) {
        if (k.sp.kind === 'model') {
          models++
          prompt += numAttr(k.sp.attrs?.promptTokens)
          completion += numAttr(k.sp.attrs?.completionTokens)
          cached += numAttr(k.sp.attrs?.cachedTokens)
        } else if (k.sp.kind === 'tool') {
          tools.push(k.sp)
        }
      }
    }
    const slow = [...tools].sort((a, b) => (b.durMs || 0) - (a.durMs || 0)).slice(0, 5)
    return { turns, agents, work, models, tools: tools.length, prompt, completion, cached, slow }
  })

  function numAttr(v: unknown): number {
    return typeof v === 'number' ? v : 0
  }

  /* tool 行按 callID 回查发起它的工具名（tool_calls 在前面的 assistant 消息里）：
     结果的 content 看不出是哪个工具的，必须靠调用 ID 认领 */
  const toolNames = $derived.by(() => {
    const m = new Map<string, string>()
    for (const msg of reviewMsgs) {
      for (const c of msg.tool_calls || []) {
        if (c.ID) m.set(c.ID, c.Name)
      }
    }
    return m
  })

  function toggleFork(id: string) {
    const next = new Set(openForks)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    openForks = next
  }

  /* span 显示名：tool.browser_tab → browser_tab，model 带迭代号便于对轮 */
  function spanLabel(s: TraceSpan): string {
    if (s.kind === 'tool') return s.name.replace(/^tool\./, '')
    if (s.kind === 'model') return s.iteration ? `model #${s.iteration}` : 'model'
    return s.name
  }

  /* 轮标题用触发它的输入（子代理用 task），比裸 "turn" 好认；⑂ 标出子代理 */
  function turnLabel(s: TraceSpan): string {
    const raw = s.attrs?.input ?? s.attrs?.task
    const text = typeof raw === 'string' ? raw.replace(/\s+/g, ' ').trim() : ''
    const label = !text ? s.name : text.length > 30 ? text.slice(0, 30) + '…' : text
    return s.kind === 'fork' ? `⑂ ${label}` : label
  }

  function fmtDur(ms: number | undefined): string {
    if (!ms) return '—'
    return ms < 1000 ? `${ms}ms` : `${(ms / 1000).toFixed(2)}s`
  }

  function fmtClock(ms: number): string {
    const d = new Date(ms)
    return [d.getHours(), d.getMinutes(), d.getSeconds()].map((n) => String(n).padStart(2, '0')).join(':')
  }

  /* attrs 值渲染：结构化值走 JSON，字符串原样（Go 侧已截断） */
  function attrVal(v: unknown): string {
    return typeof v === 'string' ? v : JSON.stringify(v, null, 2)
  }

  /* 切到分支：该节点所属线的当前叶恢复为活动会话并跳对话页 */
  async function switchLine(n: SessionNode) {
    if (!n.lineRoot) return
    try {
      await store.openTab(n.lineRoot)
      onNavigate?.('chat')
    } catch (e) {
      message = `切换分支失败：${(e as Error).message}`
    }
  }

  function fmtSize(n: number): string {
    if (n < 1024) return `${n} B`
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
    return `${(n / 1024 / 1024).toFixed(1)} MB`
  }

  /* 技能详情抽屉：点击卡片拉取 SKILL.md 正文（内建技能同样可看） */
  let skillDetail = $state<MemorySkillContent | null>(null)
  let skillLoading = $state(false)
  let skillErr = $state('')

  async function openSkillDetail(s: MemorySkillEntry) {
    skillDetail = { ...s, content: '', path: '' }
    skillLoading = true
    skillErr = ''
    try {
      skillDetail = await api.getSkill(s.id)
    } catch (e) {
      skillErr = `技能内容读取失败：${errText(e)}`
    } finally {
      skillLoading = false
    }
  }

  function closeSkillDetail() {
    skillDetail = null
    skillErr = ''
  }

  /* 技能管理：启停（乐观更新，失败回滚）与删除（confirm 后物理删目录） */
  async function toggleSkill(s: MemorySkillEntry) {
    const next = !s.enabled
    s.enabled = next
    try {
      await api.toggleSkill(s.id, next)
    } catch (e) {
      s.enabled = !next
      message = `技能启停失败：${errText(e)}`
    }
  }

  /* 删除确认弹窗：原生 confirm 在 WebView 里贴顶，自制居中确认框 */
  let delTarget = $state<MemorySkillEntry | null>(null)

  async function doDeleteSkill() {
    const s = delTarget
    delTarget = null
    if (!s) return
    try {
      await api.deleteSkill(s.id)
      if (cfg) cfg.skills.items = cfg.skills.items.filter((x) => x.id !== s.id)
    } catch (e) {
      message = `删除技能失败：${errText(e)}`
    }
  }

  /* 新建技能弹窗：选 zip 直接上传（技能名由后端从压缩包推导） */
  let skillModal = $state(false)
  let skZip = $state<{ name: string; size: number; data: string } | null>(null)
  let skBusy = $state(false)
  let skErr = $state('')
  let zipInput = $state<HTMLInputElement | null>(null)

  function openSkillModal() {
    skillModal = true
    skZip = null
    skErr = ''
  }

  function pickZip(e: Event) {
    const f = (e.target as HTMLInputElement).files?.[0]
    if (!f) return
    const r = new FileReader()
    r.onload = () => {
      const url = String(r.result || '')
      skZip = { name: f.name, size: f.size, data: url.slice(url.indexOf(',') + 1) }
    }
    r.readAsDataURL(f)
  }

  async function createSkill() {
    if (!skZip) return
    skBusy = true
    skErr = ''
    try {
      await api.createSkill(skZip.data)
      skillModal = false
      cfg = await api.getMemoryConfig()
    } catch (e) {
      skErr = errText(e)
    } finally {
      skBusy = false
    }
  }

  /* 从 "400: {"error":"xx"}" 形态的报错里提取后端信息 */
  function errText(e: unknown): string {
    const m = /\{"error":"([^"]*)"/.exec((e as Error).message)
    return m ? m[1] : (e as Error).message
  }

  /* 会话树（目录式，节点树模型）：
     虚拟 null 根 → 顶层 = 各条线（parentId 空）；归档世代沿 compress
     边向下成链；fork 挂在分叉源的父下（兄弟位，⑂ 徽标标出处）。
     parentId 由后端按同一规则算好，前端只按它分组；根层分页，子层
     展开时再拉。guides 是各级祖先的竖向导轨列。 */
  type Row = {
    n: SessionNode | null // null = 虚拟根 / 加载行 / 加载更多
    depth: number
    kids: number
    open: boolean
    guides: number
    parentTitle?: string
    loading?: boolean
    more?: boolean
  }
  /* 根层默认展开（进页面就显示最近一页分支）；子代默认收起 */
  let expanded = $state<Set<string>>(new Set(['null']))

  /* 根部展开：第一次展开拉根层，之后用缓存；先展开再拉，让“加载中…”立即可见 */
  async function toggleRoot() {
    if (expanded.has('null')) {
      expanded = new Set()
      return
    }
    expanded = new Set([...expanded, 'null'])
    if (!rootsLoaded) await loadRoots()
  }

  /* 节点展开：第一次展开拉该节点直接子代；收起保留缓存，再展开不重拉 */
  async function toggleNode(n: SessionNode) {
    if (expanded.has(n.id)) {
      const next = new Set(expanded)
      next.delete(n.id)
      expanded = next
      return
    }
    expanded = new Set([...expanded, n.id])
    if (!loadedChildren.has(n.id) && n.childCount > 0 && !(await loadChildren(n.id))) {
      const next = new Set(expanded)
      next.delete(n.id)
      expanded = next
    }
  }

  const treeRows = $derived.by(() => {
    const nodes = tree ?? []
    const kids = new Map<string, SessionNode[]>() // 展示父 id → 子节点
    const roots: SessionNode[] = []
    for (const n of nodes) {
      if (n.parentId) kids.set(n.parentId, [...(kids.get(n.parentId) || []), n])
      else roots.push(n)
    }
    const out: Row[] = []
    const nullOpen = expanded.has('null')
    out.push({ n: null, depth: 0, kids: roots.length, open: nullOpen, guides: 0 })
    if (!nullOpen) return out
    const loadRow = (depth: number): Row => ({
      n: null, depth, kids: 0, open: false, guides: depth - 1, loading: true,
    })
    if (loadingParent === '' && !rootsLoaded) {
      out.push(loadRow(1))
      return out
    }
    const walk = (n: SessionNode, depth: number, parentTitle: string) => {
      const ch = kids.get(n.id) || []
      const open = expanded.has(n.id)
      out.push({ n, depth, kids: ch.length, open, guides: depth - 1, parentTitle })
      if (!open) return
      if (ch.length === 0 && n.childCount > 0 && loadingParent === n.id) {
        out.push(loadRow(depth + 1))
        return
      }
      for (const c of ch) walk(c, depth + 1, n.title)
    }
    for (const r of roots) walk(r, 1, '')
    if (loadingParent === '') out.push(loadRow(1))
    else if (rootNext) out.push({ n: null, depth: 1, kids: 0, open: false, guides: 0, more: true })
    return out
  })

  /* fork 徽章里的源名截断展示（全文走 title 悬浮） */
  function shortTitle(s: string | undefined, k = 16): string {
    if (!s) return '源会话'
    return s.length > k ? s.slice(0, k) + '…' : s
  }

  function fmtDate(ts: number): string {
    if (!ts) return ''
    const d = new Date(ts)
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
  }
</script>

<div class="page">
  <h1>记忆</h1>
  <p class="lead">agent 的记忆由三个文件夹构成，路径可在设置中配置。</p>
  {#if message}
    <p class="lead err">{message}</p>
  {/if}

  <!-- ── 长期记忆（条目式，agent 的 memory 工具与本页同一存储）── -->
  <section>
    <header>
      <div>
        <h2 title="条目式数据库：agent 用 memory 工具读写（变更进通知栏），你也可以在这里直接增删改。"
          >长期记忆</h2
        >
      </div>
    </header>
    <div class="memstack">
      {#each panels as panel (panel.target)}
        <div class="mempnel" class:soul={panel.target === 'soul'}>
          <div class="memhead">
            <div class="memtitle">
              <h3 title={panel.desc}><span class="memsig"></span>{panel.label}<span class="memcnt">{itemsOf(panel.target).length}</span></h3>
            </div>
            <button class="memadd" onclick={() => (adding = { target: panel.target, text: '' })}>＋ 添加</button>
          </div>
          <div class="memlist">
            {#each itemsOf(panel.target) as it, i}
              <div class="memitem">
                <span class="membadge">{it.id || '新'}</span>
                {#if editing?.target === panel.target && editing.index === i}
                  <div class="memeditwrap">
                    <textarea class="memedit" rows="4" value={editing.text} oninput={(e) => (editing!.text = e.currentTarget.value)}></textarea>
                    <div class="memops">
                      <button class="mop primary" onclick={commitEdit} disabled={memBusy}>保存</button>
                      <button class="mop" onclick={() => (editing = null)}>取消</button>
                    </div>
                  </div>
                {:else}
                  {@const mk = memKey(panel.target, it, i)}
                  <div class="membody">
                    <div
                      class="memtext"
                      class:expanded={expandedMem.has(mk)}
                      class:more={overflowMem[mk] && !expandedMem.has(mk)}
                      use:trackOverflow={{ key: mk, expanded: expandedMem.has(mk) }}>{it.text}</div
                    >
                    <div class="memmeta">
                      <span class="memtime">{relTime(it.updatedAt)}</span>
                      {#if overflowMem[mk]}
                        <button class="memmore" onclick={() => toggleMem(mk)}>{expandedMem.has(mk) ? '收起' : '展开'}</button>
                      {/if}
                      <span class="memacts">
                        <button class="micon" title="编辑" onclick={() => (editing = { target: panel.target, index: i, text: it.text })}>✎</button>
                        <button class="micon danger" title="删除" onclick={() => removeItem(panel.target, i)}>✕</button>
                      </span>
                    </div>
                  </div>
                {/if}
              </div>
            {/each}
            {#if !itemsOf(panel.target).length}
              <div class="empty">{panel.empty}</div>
            {/if}
          </div>
        </div>
      {/each}
    </div>

    <!-- 项目记忆 -->
    <div class="projwrap">
      <div class="projhead">
        <h3>项目记忆</h3>
        <button
          class="memadd"
          onclick={() => {
            projectDraft = { id: '', name: '', description: '', content: '' }
            projectNew = true
          }}>＋ 新建项目</button
        >
      </div>
      <div class="list">
        {#each longterm?.projects ?? [] as p (p.id)}
          <div class="file proj" class:open={projectDraft?.id === p.id}>
            <button class="projrow" onclick={() => void openProject(p.id)} title="展开编辑项目记忆">
              <span class="pname">{p.name}</span>
              <span class="stat">{p.description}</span>
              <span class="pid mono">{p.id}</span>
            </button>
            <span class="memacts">
              <button class="micon danger" title="删除项目" onclick={() => void removeProject(p.id)}>✕</button>
            </span>
          </div>
        {/each}
        {#if !(longterm?.projects ?? []).length}
          <div class="empty">暂无项目——围绕明确项目的会话，agent 会建立项目记忆。</div>
        {/if}
      </div>
    </div>
  </section>

  <!-- ── 能力记忆 ── -->
  <section id="mem-skills">
    <header>
      <div>
        <h2 title="沉淀的 skill——做过一次的复杂操作固化为可复用的能力。">能力记忆</h2>
      </div>
    </header>
    <p class="dir"><svg class="dirico" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" /></svg>{cfg ? cfg.skills.dir : '—'}</p>
    <div class="grid">
      {#if cfg}
        {#each cfg.skills.items as s (s.id)}
          <div
            class="card"
            class:off={!s.enabled}
            role="button"
            tabindex="0"
            onclick={() => openSkillDetail(s)}
            onkeydown={(e) => (e.key === 'Enter' || e.key === ' ') && openSkillDetail(s)}
          >
            <div class="card-top">
              <div class="glyph">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M12 3l1.9 5.6L20 10l-5 3.6L16.5 20 12 16.6 7.5 20 9 13.6 4 10l6.1-1.4L12 3z" />
                </svg>
              </div>
              <div class="card-ops">
                {#if !s.builtin}
                  <button
                    class="del"
                    title="删除技能"
                    onclick={(e) => {
                      e.stopPropagation()
                      delTarget = s
                    }}
                  >
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                      <path d="M3 6h18M8 6V4h8v2M6 6l1 14h10l1-14M10 11v6M14 11v6" />
                    </svg>
                  </button>
                {/if}
                <span
                  class="toggle"
                  class:on={s.enabled}
                  role="switch"
                  aria-checked={s.enabled}
                  tabindex="0"
                  onclick={(e) => {
                    e.stopPropagation()
                    toggleSkill(s)
                  }}
                  onkeydown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.stopPropagation()
                      toggleSkill(s)
                    }
                  }}
                  title={s.enabled ? '禁用技能' : '启用技能'}>
                  <i></i>
                </span>
              </div>
            </div>
            <h3>{s.name}{#if s.builtin}<span class="bi" title="随应用分发，可禁用不可删">内建</span>{/if}</h3>
            {#if s.desc}<p class="card-desc" title={s.desc}>{s.desc}</p>{/if}
          </div>
        {/each}
        <button class="card ghost newskill" onclick={openSkillModal}>
          <div class="card-top">
            <div class="glyph ghost-glyph">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                <path d="M12 5v14M5 12h14" />
              </svg>
            </div>
          </div>
          <h3 title="上传 zip 压缩包，把重复性工作流沉淀为可复用的能力。">新建技能</h3>
        </button>
      {:else}
        <button class="card ghost newskill" onclick={openSkillModal}>
          <div class="card-top">
            <div class="glyph ghost-glyph">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                <path d="M12 5v14M5 12h14" />
              </svg>
            </div>
          </div>
          <h3 title="上传 zip 新建技能，重复性工作流可沉淀为 skill。">暂无技能</h3>
        </button>
      {/if}
    </div>
  </section>

  <!-- ── 话题记忆：完整会话树（目录式）── -->
  <section>
    <header>
      <div>
        <h2 title="完整会话树：一级 = 分支（⑂ 为分叉新线），线内向下是压缩世代；已归档不再参与恢复。"
          >话题记忆</h2
        >
      </div>
    </header>
    <p class="dir"><svg class="dirico" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" /></svg>{cfg ? cfg.topics.dir : '—'}</p>
    <div class="treelist">
      {#each treeRows as row, i (`${row.n?.id ?? 'null'}-${i}`)}
        {#if row.loading}
          <div class="trow tload" style="padding-left:{8 + row.guides * 18}px">
            <span class="desc">加载中…</span>
          </div>
        {:else if row.more}
          <div class="trow" style="padding-left:26px">
            <button class="loadmore" onclick={() => void loadRoots(true)}>加载更多</button>
          </div>
        {:else if row.n === null}
          <div class="trow root">
            <button class="tw" onclick={toggleRoot} title={row.open ? '收起分支列表' : '展开分支列表'}>
              <span class="farrow" class:open={row.open}>{row.open ? '▾' : '▸'}</span>
            </button>
            <span class="name mono">sessions</span>
            <span class="desc">{rootsLoaded ? `${row.kids}${rootTotal > row.kids ? ` / ${rootTotal}` : ''} 条分支` : ''}</span>
          </div>
        {:else}
          {@const n = row.n}
          <div class="trow" class:cur={n.isActiveLine && n.isLeaf}>
            {#each Array(row.guides) as _, g (g)}
              <span class="guide"></span>
            {/each}
            {#if row.kids > 0 || n.childCount > 0}
              <button class="tw" onclick={() => void toggleNode(n)} title={row.open ? '收起世代' : '展开世代'}>
                <span class="farrow" class:open={row.open}>{row.open ? '▾' : '▸'}</span>
              </button>
            {:else}
              <span class="tw dot">·</span>
            {/if}
            <div class="info">
              <span class="name">
                <span class="n-text">{n.title || '未命名会话'}</span>
                {#if n.archived}<span class="kbadge arc">已归档</span>{/if}
                {#if n.seedKind === 'fork'}<span class="kbadge" title={n.forkedFrom?.title ? `来自 session：${n.forkedFrom.title}` : 'fork 产生的 session'}>⑂ {shortTitle(n.forkedFrom?.title)}</span>{/if}
                {#if n.seedKind === 'compress'}<span class="kbadge" title={row.parentTitle ? `来自「${row.parentTitle}」压缩生成` : '压缩生成'}>⇪ 压缩生成</span>{/if}
                {#if n.msgs === 0}<span class="kbadge mut">空</span>{/if}
              </span>
              <span class="desc">{fmtDate(n.createdAt)} · {n.msgs} 条消息</span>
            </div>
            <div class="ops">
              <button class="op" onclick={() => reviewTopic(n)}>回顾</button>
              {#if n.isLeaf && n.lineRoot && !(n.isActiveLine)}
                <button class="op" onclick={() => switchLine(n)} title="切到该分支的当前叶继续">切到分支</button>
              {/if}
              <button class="op" disabled title="手动归档：后续版本开放">归档</button>
            </div>
          </div>
        {/if}
      {/each}
      {#if treeErr}
        <div class="empty">{treeErr}</div>
      {:else if rootsLoaded && rootTotal === 0 && expanded.has('null')}
        <div class="empty">暂无会话——在对话页开聊后会沉淀到这里。</div>
      {/if}
    </div>
  </section>
</div>

<!-- 回顾侧边抽屉：消息全文 / 调用链双页签 -->
{#if reviewNode}
  <div class="drawer-mask" onclick={closeReview}></div>
  <aside class="drawer" class:wide={reviewTab === 'trace'}>
    <header>
      <div class="dtitle">
        <span class="name">{reviewNode.title || '未命名会话'}</span>
        {#if reviewNode.archived}<span class="kbadge arc">已归档</span>{/if}
        {#if reviewNode.seedKind === 'fork'}<span class="kbadge">⑂</span>{/if}
        {#if reviewNode.seedKind === 'compress'}<span class="kbadge">⇪ 压缩生成</span>{/if}
      </div>
      <button class="dclose" onclick={closeReview} title="关闭">✕</button>
    </header>
    <div class="dtabs">
      <button class="dtab" class:on={reviewTab === 'msgs'} onclick={() => (reviewTab = 'msgs')}>消息</button>
      <button class="dtab" class:on={reviewTab === 'trace'} onclick={() => void openTraceTab()}>调用链</button>
      {#if reviewTab === 'trace' && reviewSpans.length > 0}
        <span class="dtab-hint">{reviewSpans.length} spans</span>
      {/if}
    </div>

    {#if reviewTab === 'msgs'}
      <div class="dmsgs">
        {#if reviewSystem}
          <details class="dsys" bind:open={sysOpen}>
            <summary>
              <span class="dsys-caret" class:open={sysOpen}>▸</span>
              <span class="dsys-title">系统提示词</span>
              <span class="dsys-hint">{reviewSystem.length.toLocaleString()} 字符 · {sysOpen ? '收起' : '展开'}</span>
            </summary>
            <pre class="dsys-text">{reviewSystem}</pre>
          </details>
        {/if}
        {#if reviewSummary}
          <div class="dsum">
            <div class="dsum-tag">⇪ 压缩摘要（本会话开始前的上下文）</div>
            <div class="dsum-text">{reviewSummary}</div>
          </div>
        {/if}
        {#if reviewLoading}
          <div class="rv">加载中…</div>
        {:else}
          {#each reviewMsgs as m, j (j)}
            <div class="rv" class:me={m.role === 'user'}>
              <span class="rrole">{m.role === 'assistant' ? 'agent' : m.role === 'tool' ? 'tool' : m.role}</span>
              <span class="rtext">{#if m.role === 'tool' && m.tool_call_id}<span class="rcall" title={m.tool_call_id}>{toolNames.get(m.tool_call_id) || '未知工具'} <em>{m.tool_call_id}</em></span>{/if}{m.content || (m.tool_calls ? JSON.stringify(m.tool_calls) : '')}</span>
            </div>
          {/each}
          {#if reviewMsgs.length === 0 && !reviewSummary}
            <div class="rv">（无消息）</div>
          {/if}
        {/if}
      </div>
    {:else}
      <div class="dmsgs">
        {#if traceLoading}
          <div class="rv">加载中…</div>
        {:else if traceBlocks.all.length === 0}
          <div class="rv">（该会话无调用链记录）</div>
        {:else}
          <!-- 汇总：轮次/轮内耗时合计/调用数与 token 累计，慢工具 TopN -->
          <div class="tsum">
            <div class="tscell"><span class="tsk">轮次</span><span class="tsv">{traceSummary.turns}</span></div>
            {#if traceSummary.agents > 0}
              <div class="tscell"><span class="tsk">子代理</span><span class="tsv">{traceSummary.agents}</span></div>
            {/if}
            <div class="tscell"><span class="tsk">耗时合计</span><span class="tsv">{fmtDur(traceSummary.work)}</span></div>
            <div class="tscell"><span class="tsk">模型</span><span class="tsv">{traceSummary.models}</span></div>
            <div class="tscell"><span class="tsk">工具</span><span class="tsv">{traceSummary.tools}</span></div>
            <div class="tscell wide">
              <span class="tsk">token</span>
              <span class="tsv">
                {traceSummary.prompt.toLocaleString()} → {traceSummary.completion.toLocaleString()}
                {#if traceSummary.cached > 0}<span class="tsm">（缓存 {traceSummary.cached.toLocaleString()}）</span>{/if}
              </span>
            </div>
          </div>
          {#if traceSummary.slow.length > 0}
            <div class="tslow">
              <div class="dsum-tag">慢工具</div>
              {#each traceSummary.slow as s (s.spanId)}
                <button
                  type="button"
                  class="tslrow"
                  class:picked={pickedSpan?.spanId === s.spanId}
                  onclick={() => (pickedSpan = s)}>
                  <span class="tslnm">{spanLabel(s)}</span>
                  <span class="tslms">{fmtDur(s.durMs)}</span>
                </button>
              {/each}
            </div>
          {/if}

          <!-- 瀑布图：一轮一块，块内时间轴以该轮为原点（空档压成细缝）；
               fork 块缩进挂在发起它的 task 行下，条宽用分身自己的时间轴 -->
          <div class="twrap">
            {#each traceBlocks.tops as t (t.root.spanId)}
              <div class="tgroup">
                <button
                  type="button"
                  class="tspan root"
                  class:picked={pickedSpan?.spanId === t.root.spanId}
                  onclick={() => (pickedSpan = t.root)}>
                  <span class="tname" title={String(t.root.attrs?.input ?? t.root.attrs?.task ?? '')}>
                    <span class="tclock">{fmtClock(t.start)}</span>{turnLabel(t.root)}
                  </span>
                  <span class="ttrack"><i class={`k-${t.root.kind}`} style="left:0;width:100%"></i></span>
                  <span class="tms">{fmtDur(t.dur)}</span>
                </button>
                {#each t.kids as k (k.sp.spanId)}
                  <button
                    type="button"
                    class="tspan d1"
                    class:picked={pickedSpan?.spanId === k.sp.spanId}
                    onclick={() => (pickedSpan = k.sp)}>
                    <span class="tname">{spanLabel(k.sp)}</span>
                    <span class="ttrack">
                      <i class={`k-${k.sp.kind}`} style={`left:${k.left}%;width:${k.width}%`}></i>
                    </span>
                    <span class="tms">{fmtDur(k.sp.durMs)}</span>
                  </button>
                  {#each t.nested.filter((n) => n.anchor === k.sp.spanId) as f (f.root.spanId)}
                    <div class="fgroup">
                      <div class="tspan d2" class:picked={pickedSpan?.spanId === f.root.spanId}>
                        <span class="tname">
                          <button
                            type="button"
                            class="tw"
                            title={openForks.has(f.root.spanId) ? '折起分身调用链' : '展开分身调用链'}
                            onclick={() => toggleFork(f.root.spanId)}>
                            <span class="farrow" class:open={openForks.has(f.root.spanId)}>
                              {openForks.has(f.root.spanId) ? '▾' : '▸'}
                            </span>
                          </button>
                          <button
                            type="button"
                            class="flabel"
                            title={String(f.root.attrs?.task ?? '')}
                            onclick={() => (pickedSpan = f.root)}>
                            {turnLabel(f.root)}</button>
                        </span>
                        <span class="thint">子代理 · {f.kids.length} 次调用 · 局部时间轴</span>
                        <span class="tms">{fmtDur(f.dur)}</span>
                      </div>
                      {#if openForks.has(f.root.spanId)}
                        {#each f.kids as fk (fk.sp.spanId)}
                          <button
                            type="button"
                            class="tspan d3"
                            class:picked={pickedSpan?.spanId === fk.sp.spanId}
                            onclick={() => (pickedSpan = fk.sp)}>
                            <span class="tname">{spanLabel(fk.sp)}</span>
                            <span class="ttrack">
                              <i class={`k-${fk.sp.kind}`} style={`left:${fk.left}%;width:${fk.width}%`}></i>
                            </span>
                            <span class="tms">{fmtDur(fk.sp.durMs)}</span>
                          </button>
                        {/each}
                      {/if}
                    </div>
                  {/each}
                {/each}
              </div>
            {/each}
          </div>
        {/if}
      </div>
      <!-- span 详情停靠抽屉底部：从瀑布图或慢工具点选后始终可见 -->
      {#if pickedSpan && reviewTab === 'trace'}
        <div class="tdet">
          <div class="tdet-head">
            <span class="tdet-name">{pickedSpan.name}</span>
            <span class="tdet-kind">{pickedSpan.kind}</span>
            {#if pickedSpan.iteration}<span class="tdet-kind">iter {pickedSpan.iteration}</span>{/if}
            <span class="tdet-dur">{fmtDur(pickedSpan.durMs)}</span>
            <button class="tdet-x" onclick={() => (pickedSpan = null)} title="关闭详情">✕</button>
          </div>
          {#each Object.entries(pickedSpan.attrs || {}) as [k, v] (k)}
            <div class="tattr">
              <span class="tak">{k}</span>
              <span class="tav">{attrVal(v)}</span>
            </div>
          {/each}
          {#if !pickedSpan.attrs || Object.keys(pickedSpan.attrs).length === 0}
            <div class="tattr"><span class="tav">（无 attrs）</span></div>
          {/if}
        </div>
      {/if}
    {/if}
  </aside>
{/if}

<!-- 长期记忆添加：居中弹框，不再在条目列表内展开 -->
{#if adding}
  {@const targetLabel = panels.find((p) => p.target === adding!.target)?.label ?? ''}
  <div class="drawer-mask" onclick={() => (adding = null)}></div>
  <div class="modal" role="dialog" aria-modal="true" aria-label="添加记忆条目">
    <header>
      <h3>添加到{targetLabel}</h3>
      <button class="dclose" onclick={() => (adding = null)} title="关闭">✕</button>
    </header>
    <div class="fld">
      <span>条目内容</span>
      <textarea class="memedit" rows="4" placeholder="一条新记忆（自包含的一句话）" value={adding.text} oninput={(e) => (adding!.text = e.currentTarget.value)}></textarea>
    </div>
    <footer>
      <button class="btn" disabled={memBusy} onclick={() => (adding = null)}>取消</button>
      <button class="btn primary" disabled={memBusy} onclick={commitAdd}>保存</button>
    </footer>
  </div>
{/if}

<!-- 项目记忆：新建与编辑共用居中弹框 -->
{#if projectDraft}
  <div class="drawer-mask" onclick={closeProject}></div>
  <div class="modal projmodal" role="dialog" aria-modal="true" aria-label={projectNew ? '新建项目' : '编辑项目'}>
    <header>
      <h3>{projectNew ? '新建项目' : '编辑项目'}</h3>
      <button class="dclose" onclick={closeProject} title="关闭">✕</button>
    </header>
    <div class="prow">
      <label>名称<input class="pin" bind:value={projectDraft.name} placeholder="项目名" /></label>
      <label>描述<input class="pin" bind:value={projectDraft.description} placeholder="一句话描述" /></label>
    </div>
    <textarea class="pcontent" rows="10" bind:value={projectDraft.content} placeholder="项目记忆全文（背景/约定/关键路径…，markdown）" spellcheck="false"></textarea>
    <footer>
      <button class="btn" disabled={memBusy} onclick={closeProject}>取消</button>
      <button class="btn primary" disabled={memBusy} onclick={() => void commitProject()}>保存</button>
    </footer>
  </div>
{/if}

<!-- 新建技能弹窗：选 zip 直接上传（技能名由后端从压缩包推导） -->
<!-- 技能详情抽屉：SKILL.md 正文（含内建技能） -->
{#if skillDetail}
  <div class="drawer-mask" onclick={closeSkillDetail}></div>
  <aside class="drawer skill">
    <header>
      <div class="dtitle">
        <span class="name">{skillDetail.name}</span>
        {#if skillDetail.builtin}<span class="kbadge">内建</span>{/if}
      </div>
      <button class="dclose" onclick={closeSkillDetail} title="关闭">✕</button>
    </header>
    <div class="skmeta">
      {#if skillDetail.desc}<p>{skillDetail.desc}</p>{/if}
      {#if !skillDetail.builtin && skillDetail.path}<span class="skpath">{skillDetail.path}</span>{/if}
    </div>
    {#if skillLoading}
      <div class="skempty">加载中…</div>
    {:else if skillErr}
      <div class="skempty">{skillErr}</div>
    {:else}
      <pre class="skillmd">{skillDetail.content || '（SKILL.md 无正文）'}</pre>
    {/if}
  </aside>
{/if}

{#if skillModal}
  <div class="drawer-mask" onclick={() => (skillModal = false)}></div>
  <div class="modal">
    <header>
      <h3>新建技能</h3>
      <button class="dclose" onclick={() => (skillModal = false)} title="关闭">✕</button>
    </header>
    <div class="fld">
      <span>压缩包（.zip）</span>
      <button class="zipbtn" onclick={() => zipInput?.click()}>
        {skZip ? `${skZip.name} · ${fmtSize(skZip.size)}` : '点击选择 zip 文件'}
      </button>
      <input type="file" accept=".zip,application/zip" hidden bind:this={zipInput} onchange={pickZip} />
    </div>
    <p class="modal-hint">
      zip 需含根级 SKILL.md，scripts 等子资源一并解压。技能名自动确定：单文件夹压缩取文件夹名，平铺取 SKILL.md 里 frontmatter 的 name。
    </p>
    {#if skErr}
      <p class="modal-hint err">{skErr}</p>
    {/if}
    <footer>
      <button class="btn" disabled={skBusy} onclick={() => (skillModal = false)}>取消</button>
      <button class="btn primary" disabled={!skZip || skBusy} onclick={createSkill}>
        {skBusy ? '创建中…' : '创建'}
      </button>
    </footer>
  </div>
{/if}

<!-- 删除技能确认弹窗（居中） -->
{#if delTarget}
  <div class="drawer-mask" onclick={() => (delTarget = null)}></div>
  <div class="modal confirm">
    <h3>删除技能</h3>
    <p class="modal-hint">确定删除「{delTarget.name}」？技能目录与脚本将一并删除，不可恢复。</p>
    <footer>
      <button class="btn" onclick={() => (delTarget = null)}>取消</button>
      <button class="btn danger" onclick={doDeleteSkill}>删除</button>
    </footer>
  </div>
{/if}

<style>
  .page {
    flex: 1;
    min-height: 0;
    /* 全宽滚动容器（滚动条贴窗口右缘）；内容由直接子项各自限宽居中 */
    overflow-y: auto;
    padding: 40px 48px 60px;
    display: flex;
    flex-direction: column;
    gap: 34px;
  }
  h1 {
    font-size: var(--font-size-page);
    font-weight: 700;
  }
  .lead {
    font-size: var(--font-size-section);
    color: var(--muted);
    margin-top: -24px;
  }
  .lead.err {
    color: var(--danger);
  }
  section {
    display: flex;
    flex-direction: column;
    gap: 10px;
    scroll-margin-top: 28px;
  }
  header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 16px;
  }
  h2 {
    font-size: var(--font-size-section);
    font-weight: 600;
    color: var(--muted);
  }
  .hint {
    font-size: var(--font-size-small);
    color: var(--faint);
    margin-top: 2px;
  }
  .dir {
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    color: var(--faint);
    display: flex;
    align-items: center;
    gap: 6px;
    overflow-wrap: anywhere;
  }
  .dir span {
    font-size: var(--font-size-small);
  }
  .list {
    display: flex;
    flex-direction: column;
    border: 1px solid var(--line);
    border-radius: 12px;
    overflow: hidden;
  }
  .empty {
    padding: 22px 14px;
    text-align: center;
    font-size: var(--font-size-ui);
    color: var(--faint);
  }

  /* 长期记忆：文件行 */
  .file {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 14px;
    transition: background var(--dur-fast) var(--ease-out);
  }
  .file + .file {
    border-top: 1px solid var(--line);
  }
  .file:hover {
    background: var(--bg-soft);
  }
  .file .name {
    font-family: var(--font-mono);
    font-size: var(--font-size-ui);
    color: var(--fg);
    overflow-wrap: anywhere;
  }
  .file .badge {
    flex: none;
    font-size: var(--font-size-small);
    color: var(--accent);
    background: var(--accent-soft);
    border-radius: 5px;
    padding: 1px 7px;
  }
  .file .stat {
    margin-left: auto;
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    color: var(--faint);
    white-space: nowrap;
  }
  .file.index {
    background: color-mix(in srgb, var(--accent) 4%, var(--bg));
  }

  /* ── 长期记忆：条目面板与项目编辑 ── */
  /* 两块纵向全宽：各自按内容高度、列表限高内滚——条数再多也不撑长页面，
     长文本也有整行宽度可读 */
  .memstack {
    display: flex;
    flex-direction: column;
    gap: 12px;
    margin-bottom: 12px;
  }
  .mempnel {
    display: flex;
    flex-direction: column;
    border: 1px solid var(--line);
    border-radius: 12px;
    overflow: hidden;
    background: var(--bg);
  }
  .memhead {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
    padding: 9px 14px 10px;
    background: var(--bg-soft);
    border-bottom: 1px solid var(--line);
  }
  .memtitle {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .memhead h3 {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: var(--font-size-ui);
    font-weight: 600;
    color: var(--fg);
  }
  .memsig {
    flex: none;
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--accent);
  }
  .mempnel.soul .memsig {
    background: #7c3aed;
  }
  .memcnt {
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    font-weight: 500;
    color: var(--faint);
    background: var(--bg);
    border: 1px solid var(--line);
    border-radius: 10px;
    padding: 0 6px;
    line-height: 1.6;
  }
  .memadd {
    flex: none;
    border: none;
    background: transparent;
    color: var(--accent);
    font: inherit;
    font-size: var(--font-size-ui);
    cursor: pointer;
    border-radius: 6px;
    padding: 3px 8px;
    transition: background var(--dur-fast) var(--ease-out);
  }
  .memadd:hover {
    background: var(--accent-soft);
  }
  .memlist {
    display: flex;
    flex-direction: column;
    gap: 1px;
    padding: 6px;
    min-height: 52px;
    max-height: 280px;
    overflow-y: auto;
  }
  .memitem {
    position: relative;
    display: flex;
    align-items: flex-start;
    gap: 8px;
    padding: 8px;
    border-radius: 8px;
    transition: background var(--dur-fast) var(--ease-out);
  }
  .memitem + .memitem::before {
    content: '';
    position: absolute;
    top: 0;
    left: 8px;
    right: 8px;
    height: 1px;
    background: var(--line);
  }
  .memitem:hover {
    background: var(--bg-soft);
  }
  .membadge {
    flex: none;
    min-width: 24px;
    text-align: center;
    font-size: var(--font-size-small);
    color: var(--faint);
    background: var(--bg-soft);
    border: 1px solid var(--line);
    border-radius: 5px;
    padding: 1px 5px;
    margin-top: 2px;
  }
  .membody {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  /* 正文默认三行截断（超长记忆不撑高面板），展开后放开 */
  .memtext {
    font-size: var(--font-size-section);
    line-height: 1.6;
    color: var(--fg);
    white-space: pre-wrap;
    word-break: break-word;
    display: -webkit-box;
    -webkit-line-clamp: 3;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .memtext.expanded {
    display: block;
    overflow: visible;
  }
  .memtext.more {
    mask-image: linear-gradient(to bottom, #000 58%, transparent);
  }
  /* 元信息行：时间 + 展开钮在左，编辑/删除悬浮在右，不挤占正文宽度 */
  .memmeta {
    display: flex;
    align-items: center;
    gap: 10px;
    min-height: 20px;
  }
  .memtime {
    flex: none;
    font-size: var(--font-size-small);
    color: var(--faint);
    white-space: nowrap;
  }
  .memmore {
    flex: none;
    padding: 0 2px;
    border: none;
    background: transparent;
    font: inherit;
    font-size: var(--font-size-small);
    color: var(--accent);
    cursor: pointer;
    border-radius: 4px;
  }
  .memmore:hover {
    text-decoration: underline;
  }
  .memmeta .memacts {
    margin-left: auto;
  }
  .memacts {
    flex: none;
    display: inline-flex;
    gap: 2px;
    visibility: hidden;
  }
  .memitem:hover .memacts,
  .file.proj:hover .memacts {
    visibility: visible;
  }
  .micon {
    display: grid;
    place-items: center;
    width: 22px;
    height: 22px;
    border: none;
    background: transparent;
    color: var(--faint);
    font-size: var(--font-size-small);
    line-height: 1;
    cursor: pointer;
    border-radius: 6px;
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .micon:hover {
    background: var(--bg);
    color: var(--fg);
  }
  .micon.danger:hover {
    background: color-mix(in srgb, #c0392b 10%, var(--bg));
    color: var(--danger);
  }
  .memeditwrap {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .memedit {
    width: 100%;
    resize: vertical;
    font-family: inherit;
    font-size: var(--font-size-section);
    line-height: 1.6;
    color: var(--fg);
    background: var(--bg);
    border: 1px solid var(--line-strong);
    border-radius: 8px;
    padding: 8px 10px;
    box-sizing: border-box;
  }
  .memedit:focus {
    outline: none;
    border-color: var(--accent);
  }
  .memops {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }
  .mop {
    font: inherit;
    font-size: var(--font-size-ui);
    color: var(--fg);
    background: var(--bg);
    border: 1px solid var(--line-strong);
    border-radius: 8px;
    padding: 4px 14px;
    cursor: pointer;
    transition:
      border-color var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .mop:hover:not(:disabled) {
    border-color: var(--accent);
    color: var(--accent);
  }
  .mop:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .mop.primary {
    background: var(--accent);
    border-color: var(--accent);
    color: #fff;
  }
  .mop.primary:hover:not(:disabled) {
    color: #fff;
    filter: brightness(1.08);
  }

  /* 项目记忆：清单行 + 展开编辑面板 */
  .projwrap {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .projhead {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }
  .projhead h3 {
    font-size: var(--font-size-ui);
    font-weight: 600;
    color: var(--muted);
  }
  .projwrap .list {
    background: var(--bg);
  }
  .file.proj {
    gap: 6px;
    padding: 9px 12px;
  }
  .file.proj.open {
    background: var(--accent-soft);
  }
  .projrow {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: baseline;
    gap: 10px;
    background: transparent;
    border: none;
    cursor: pointer;
    text-align: left;
    padding: 0;
    font: inherit;
    color: var(--fg);
  }
  .projrow .pname {
    flex: none;
    font-size: var(--font-size-section);
    font-weight: 600;
  }
  .projrow .stat {
    margin-left: 0;
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .projrow .pid {
    flex: none;
    font-size: var(--font-size-small);
    color: var(--faint);
    opacity: 0.75;
  }
  .prow {
    display: flex;
    gap: 12px;
    flex-wrap: wrap;
  }
  .prow label {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: var(--font-size-ui);
    color: var(--muted);
  }
  .pin {
    width: 220px;
    font: inherit;
    font-size: var(--font-size-section);
    color: var(--fg);
    background: var(--bg-soft);
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 6px 10px;
  }
  .pin:focus {
    outline: none;
    border-color: var(--accent);
  }
  .pcontent {
    width: 100%;
    resize: vertical;
    font-family: var(--font-mono);
    font-size: var(--font-size-section);
    line-height: 1.6;
    color: var(--fg);
    background: var(--bg-soft);
    border: 1px solid var(--line);
    border-radius: 10px;
    padding: 10px 12px;
    box-sizing: border-box;
  }
  .pcontent:focus {
    outline: none;
    border-color: var(--accent);
  }


  /* 能力记忆：技能卡片 */
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(196px, 1fr));
    gap: 14px;
  }
  .card {
    display: flex;
    flex-direction: column;
    gap: 8px;
    border: 1px solid var(--line);
    cursor: pointer;
    transition:
      border-color var(--dur-fast) var(--ease-out),
      box-shadow var(--dur-fast) var(--ease-out);
    border-radius: 14px;
    background: var(--bg);
    padding: 16px;
    transition:
      border-color var(--dur-fast) var(--ease-out),
      box-shadow var(--dur-fast) var(--ease-out),
      transform var(--dur-fast) var(--ease-out);
  }
  .card:not(.ghost):hover {
    border-color: var(--line-strong);
    box-shadow: 0 4px 16px rgb(0 0 0 / 7%);
    transform: translateY(-2px);
  }
  .card.off {
    opacity: 0.55;
  }
  .card-top {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
  }
  .glyph {
    display: grid;
    place-items: center;
    width: 34px;
    height: 34px;
    border-radius: 10px;
    background: var(--bg-soft);
    border: 1px solid var(--line);
    color: var(--muted);
  }
  .glyph svg {
    width: 16px;
    height: 16px;
  }
  .card h3 {
    font-size: var(--font-size-section);
    font-weight: 600;
    color: var(--fg);
    margin-top: 2px;
  }
  .card:hover {
    border-color: var(--line-strong);
  }
  /* 技能描述：展示但压缩两行，全文在详情抽屉与 title 里 */
  .card-desc {
    margin: 0;
    font-size: var(--font-size-small);
    line-height: 1.5;
    color: var(--faint);
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  /* 技能详情抽屉（复用回顾抽屉骨架） */
  .drawer.skill {
    width: min(560px, 94vw);
  }
  .skmeta {
    flex: none;
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 12px 16px;
    border-bottom: 1px solid var(--line);
  }
  .skmeta p {
    margin: 0;
    font-size: var(--font-size-ui);
    line-height: 1.6;
    color: var(--muted);
  }
  .skpath {
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    color: var(--faint);
    overflow-wrap: anywhere;
  }
  .skillmd {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    margin: 0;
    padding: 14px 16px 28px;
    font-family: var(--font-mono);
    font-size: var(--font-size-ui);
    line-height: 1.65;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    color: var(--fg);
  }
  .skempty {
    padding: 24px 16px;
    font-size: var(--font-size-ui);
    color: var(--faint);
  }
  .card h3 .bi {
    display: inline-block;
    margin-left: 6px;
    padding: 1px 7px;
    border-radius: 6px;
    font-size: var(--font-size-small);
    font-weight: 500;
    vertical-align: 1px;
    color: var(--accent);
    background: var(--accent-soft);
  }
  .card.ghost {
    border-style: dashed;
  }
  .ghost-glyph {
    border-style: dashed;
    background: transparent;
  }
  .card.ghost h3 {
    color: var(--muted);
  }
  .card.newskill {
    cursor: pointer;
    text-align: left;
    font: inherit;
  }
  .card.newskill:hover {
    border-color: var(--line-strong);
    box-shadow: 0 4px 16px rgb(0 0 0 / 7%);
    transform: translateY(-2px);
  }
  .card-ops {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .del {
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    border: none;
    background: transparent;
    border-radius: 7px;
    color: var(--faint);
    cursor: pointer;
    opacity: 0;
    transition:
      opacity var(--dur-fast) var(--ease-out),
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .del svg {
    width: 14px;
    height: 14px;
  }
  .card:hover .del {
    opacity: 1;
  }
  .del:hover {
    background: color-mix(in srgb, #c0392b 10%, var(--bg));
    color: var(--danger);
  }
  .toggle {
    position: relative;
    width: 30px;
    height: 17px;
    border-radius: 9px;
    background: var(--line);
    transition: background var(--dur-fast) var(--ease-out);
    cursor: pointer;
    flex: none;
  }
  .toggle i {
    position: absolute;
    top: 2px;
    left: 2px;
    width: 13px;
    height: 13px;
    border-radius: 50%;
    background: var(--bg);
    transition: transform var(--dur-fast) var(--ease-out);
    box-shadow: 0 1px 2px rgb(0 0 0 / 20%);
  }
  .toggle.on {
    background: var(--accent);
  }
  .toggle.on i {
    transform: translateX(13px);
  }

  /* ── 新建技能弹窗 ── */
  .modal {
    position: fixed;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    z-index: 59;
    width: min(400px, 92vw);
    display: flex;
    flex-direction: column;
    gap: 14px;
    background: var(--bg);
    border: 1px solid var(--line-strong);
    border-radius: 14px;
    box-shadow: 0 12px 40px rgb(0 0 0 / 18%);
    padding: 18px 20px;
  }
  .modal header {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .modal h3 {
    font-size: var(--font-size-body);
    font-weight: 700;
    color: var(--fg);
  }
  .fld {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .fld > span {
    font-size: var(--font-size-ui);
    color: var(--muted);
  }
  .zipbtn {
    border: 1px dashed var(--line);
    border-radius: 8px;
    background: var(--bg-soft);
    color: var(--muted);
    font: inherit;
    font-size: var(--font-size-ui);
    padding: 9px 10px;
    text-align: left;
    cursor: pointer;
    overflow-wrap: anywhere;
    transition:
      border-color var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .zipbtn:hover {
    border-color: var(--line-strong);
    color: var(--fg);
  }
  .modal-hint {
    font-size: var(--font-size-small);
    line-height: 1.6;
    color: var(--faint);
  }
  .modal-hint.err {
    color: var(--danger);
  }
  .modal footer {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }
  .btn {
    border: 1px solid var(--line);
    border-radius: 8px;
    background: transparent;
    color: var(--muted);
    font: inherit;
    font-size: var(--font-size-ui);
    padding: 7px 16px;
    cursor: pointer;
    transition:
      border-color var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .btn:not(:disabled):hover {
    border-color: var(--line-strong);
    color: var(--fg);
  }
  .btn.primary {
    background: var(--accent);
    border-color: var(--accent);
    color: #fff;
  }
  .btn.danger {
    background: #c0392b;
    border-color: #c0392b;
    color: #fff;
  }
  .btn:disabled {
    opacity: 0.45;
    cursor: default;
  }
  .modal.confirm {
    width: min(340px, 92vw);
    gap: 12px;
  }
  /* 项目记忆弹框：两列名称/描述 + 全文，比确认框宽 */
  .modal.projmodal {
    width: min(560px, 92vw);
    max-height: 86vh;
    overflow-y: auto;
  }

  /* 话题记忆：会话行 */
  .info {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .skill .name {
    font-size: var(--font-size-section);
    color: var(--fg);
    font-weight: 500;
  }
  .desc {
    font-size: var(--font-size-small);
    color: var(--faint);
    overflow-wrap: anywhere;
  }

  /* 话题记忆：目录式会话树。导轨列(guide)按祖先层级等宽排列，
     竖线贯穿同级节点——折叠的子树不渲染行，导轨天然连续不错位 */
  /* 会话树限高内部滚动：条目再多也不把整页顶长 */
  .treelist {
    display: flex;
    flex-direction: column;
    border: 1px solid var(--line);
    border-radius: 12px;
    padding: 0 0 4px;
    max-height: 50vh;
    overflow-x: hidden;
    overflow-y: auto;
  }
  /* 某一层正在拉取时的占位行 */
  .tload {
    color: var(--faint);
    font-size: var(--font-size-small);
  }
  .loadmore {
    border: none;
    background: transparent;
    color: var(--accent);
    font: inherit;
    font-size: var(--font-size-ui);
    padding: 4px 8px;
    border-radius: 6px;
    cursor: pointer;
  }
  .loadmore:hover {
    background: var(--accent-soft);
  }
  .trow {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px 12px 6px 8px;
    transition: background var(--dur-fast) var(--ease-out);
  }
  .trow:hover {
    background: var(--bg-soft);
  }
  .trow.cur {
    background: color-mix(in srgb, var(--accent) 7%, var(--bg));
  }
  /* 根部行吸附在滚动区顶部，展开长树时始终可见 */
  .trow.root {
    position: sticky;
    top: 0;
    z-index: 1;
    background: var(--bg);
    border-bottom: 1px solid var(--line);
    border-radius: 0;
  }
  .guide {
    flex: none;
    width: 18px;
    align-self: stretch;
    border-left: 1px solid var(--line);
  }
  .tw {
    flex: none;
    display: grid;
    place-items: center;
    width: 18px;
    height: 18px;
    border: none;
    background: transparent;
    color: var(--faint);
    font-size: var(--font-size-small);
    cursor: pointer;
    border-radius: 4px;
    padding: 0;
  }
  .tw:hover {
    color: var(--fg);
    background: var(--bg-soft);
  }
  .tw.dot {
    cursor: default;
  }
  .farrow {
    display: inline-block;
    transition: transform var(--dur-fast) var(--ease-out);
  }
  .mono {
    font-family: var(--font-mono);
  }
  .trow.root .name {
    color: var(--muted);
    font-family: var(--font-mono);
    font-size: var(--font-size-ui);
  }
  /* 名称行 = flex：文本自然宽度、过长收缩出省略号，徽章紧跟其后不被挤走 */
  .trow .name {
    display: flex;
    align-items: center;
    min-width: 0;
    font-size: var(--font-size-section);
    color: var(--fg);
    font-weight: 500;
  }
  .trow .name .n-text {
    flex: 0 1 auto;
    min-width: 0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .kbadge {
    flex: none;
    margin-left: 8px;
    font-size: var(--font-size-small);
    font-weight: 500;
    color: var(--accent);
    background: var(--accent-soft);
    border-radius: 5px;
    padding: 1px 7px;
    vertical-align: 1px;
    max-width: 240px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .kbadge.arc {
    color: var(--muted);
    background: var(--bg-soft);
    border: 1px solid var(--line);
  }
  .kbadge.mut {
    color: var(--faint);
    background: transparent;
    border: 1px dashed var(--line);
  }
  .ops {
    display: flex;
    gap: 4px;
    flex: none;
  }
  .op {
    flex: none;
    border: none;
    background: transparent;
    color: var(--muted);
    font-size: var(--font-size-ui);
    padding: 4px 8px;
    border-radius: 6px;
    opacity: 0;
    cursor: pointer;
    transition: background var(--dur-fast) var(--ease-out), color var(--dur-fast) var(--ease-out);
  }
  .trow:hover .op {
    opacity: 1;
  }
  .op:hover:not(:disabled) {
    background: var(--bg-soft);
    color: var(--fg);
  }
  .op:disabled {
    opacity: 0.35;
    cursor: default;
  }

  /* ── 回顾侧边抽屉 ── */
  .drawer-mask {
    position: fixed;
    inset: 0;
    z-index: 58;
    background: rgb(0 0 0 / 18%);
  }
  .drawer {
    position: fixed;
    top: 0;
    right: 0;
    bottom: 0;
    z-index: 59;
    width: min(460px, 92vw);
    display: flex;
    flex-direction: column;
    overflow: hidden;
    background: var(--bg);
    border-left: 1px solid var(--line-strong);
    border-radius: 16px 0 0 16px;
    box-shadow: -8px 0 24px rgb(0 0 0 / 12%);
    animation: drawer-in 0.22s var(--ease-out) both;
  }
  @keyframes drawer-in {
    from {
      transform: translateX(24px);
      opacity: 0;
    }
    to {
      transform: translateX(0);
      opacity: 1;
    }
  }
  .drawer header {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 14px 16px;
    border-bottom: 1px solid var(--line);
  }
  .dtitle {
    display: flex;
    align-items: center;
    gap: 4px;
    min-width: 0;
  }
  .dtitle .name {
    font-size: var(--font-size-section);
    font-weight: 600;
    color: var(--fg);
    overflow-wrap: anywhere;
  }
  /* 抽屉头部徽标用中性色：回顾页正文已去掉高饱和大段蓝字 */
  .dtitle .kbadge {
    color: var(--muted);
    background: var(--bg-soft);
    border: 1px solid var(--line);
  }
  .dclose {
    flex: none;
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    border: 1px solid var(--line);
    border-radius: 8px;
    background: transparent;
    color: var(--muted);
    cursor: pointer;
  }
  .dclose:hover {
    color: var(--fg);
    border-color: var(--line-strong);
  }
  /* 调用链页签需要横向铺开瀑布图的时间轴 */
  .drawer.wide {
    width: min(880px, 94vw);
  }
  .dtabs {
    flex: none;
    display: flex;
    align-items: center;
    gap: 2px;
    padding: 0 16px;
    border-bottom: 1px solid var(--line);
  }
  .dtab {
    border: none;
    background: transparent;
    font: inherit;
    font-size: var(--font-size-ui);
    color: var(--faint);
    padding: 8px 10px;
    margin-bottom: -1px;
    cursor: pointer;
    border-bottom: 2px solid transparent;
    transition: color var(--dur-fast) var(--ease-out);
  }
  .dtab:hover {
    color: var(--fg);
  }
  .dtab.on {
    color: var(--fg);
    border-bottom-color: var(--accent);
  }
  .dtab-hint {
    margin-left: auto;
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  /* 系统提示词：默认折叠（通常很长）。展开后随消息区一起滚动，
     不再单独限高，避免抽屉里套第二根滚动条 */
  .dsys {
    flex: none;
    margin: 0;
    border: 1px solid var(--line);
    border-radius: 10px;
    background: var(--bg-soft);
    overflow: hidden;
  }
  .dsys summary {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 8px 12px;
    font-size: var(--font-size-small);
    font-weight: 600;
    color: var(--fg);
    cursor: pointer;
    user-select: none;
    list-style: none;
  }
  .dsys summary::-webkit-details-marker {
    display: none;
  }
  /* 鼠标点击不留焦点环；键盘聚焦用主题色描边（系统强调色外框与界面不搭） */
  .dsys:focus,
  .dsys summary:focus {
    outline: none;
  }
  .dsys:focus-visible,
  .dsys summary:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }
  .dsys-caret {
    flex: none;
    font-size: var(--font-size-small);
    color: var(--faint);
    transition: transform var(--dur-fast) var(--ease-out);
  }
  .dsys-caret.open {
    transform: rotate(90deg);
  }
  .dsys-hint {
    font-size: var(--font-size-small);
    font-weight: 400;
    color: var(--faint);
  }
  .dsys-text {
    margin: 0;
    padding: 0 12px 10px;
    max-height: 320px;
    overflow-y: auto;
    overscroll-behavior: contain;
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    line-height: 1.65;
    color: var(--muted);
    white-space: pre-wrap;
    word-break: break-word;
  }
  .dsum {
    flex: none;
    margin: 0;
    padding: 10px 12px;
    border: 1px solid var(--line);
    border-radius: 8px;
    background: var(--bg-soft);
  }
  .dsum-tag {
    font-size: var(--font-size-small);
    color: var(--muted);
    font-weight: 600;
    margin-bottom: 6px;
  }
  .dsum-text {
    font-size: var(--font-size-ui);
    line-height: 1.6;
    color: var(--muted);
    white-space: pre-wrap;
    word-break: break-word;
  }
  /* 消息区是抽屉唯一的滚动容器：系统提示词、摘要、消息一起滚 */
  .dmsgs {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    overscroll-behavior: contain;
    scrollbar-gutter: stable;
    padding: 12px 16px 20px;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  /* ── 调用链视图：汇总卡片 ── */
  .tsum {
    display: flex;
    flex-wrap: wrap;
    gap: 6px 14px;
    padding: 10px 12px;
    border: 1px solid var(--line);
    border-radius: 10px;
    background: var(--bg-soft);
  }
  .tscell {
    display: flex;
    flex-direction: column;
    gap: 1px;
    flex: 1 1 62px;
    min-width: 62px;
  }
  .tscell.wide {
    flex: 1 1 100%;
  }
  .tsk {
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .tsv {
    font-family: var(--font-mono);
    font-size: var(--font-size-ui);
    font-weight: 600;
    color: var(--fg);
  }
  .tsm {
    font-size: var(--font-size-small);
    font-weight: 400;
    color: var(--faint);
  }
  .tslow {
    display: flex;
    flex-direction: column;
    gap: 3px;
    padding: 8px 12px;
    border: 1px solid var(--line);
    border-radius: 10px;
  }
  .tslrow {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 1px 4px;
    border: none;
    border-radius: 5px;
    background: transparent;
    color: inherit;
    font: inherit;
    font-size: var(--font-size-ui);
    text-align: left;
    cursor: pointer;
  }
  .tslrow:hover {
    background: var(--bg-soft);
  }
  .tslrow.picked {
    background: var(--accent-soft);
  }
  .tslnm {
    font-family: var(--font-mono);
    color: var(--muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .tslms {
    margin-left: auto;
    font-family: var(--font-mono);
    color: var(--faint);
  }

  /* ── 调用链视图：瀑布图（名称轴 / 时间轴 / 耗时轴 三列）── */
  .twrap {
    display: flex;
    flex-direction: column;
    padding: 8px 10px;
    border: 1px solid var(--line);
    border-radius: 10px;
  }
  /* 一轮一块：块间留白分隔，块内子 span 才可读 */
  .tgroup + .tgroup {
    margin-top: 8px;
    padding-top: 8px;
    border-top: 1px dashed var(--line);
  }
  .tspan {
    display: grid;
    grid-template-columns: 150px 1fr 54px;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 2px;
    border: none;
    border-radius: 6px;
    background: transparent;
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: pointer;
  }
  .tspan:hover {
    background: var(--bg-soft);
  }
  .tspan.picked {
    background: var(--accent-soft);
  }
  .tname {
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    color: var(--muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  /* 缩进层级：子 span 一层，fork 块两层，分身的子 span 三层（根不缩进） */
  .tspan.d1 .tname {
    padding-left: 14px;
  }
  /* 分身块：整组带底色（与主循环的行区分），默认折起只留一行；块头是分组
     标题，条留给分身的子 span 各自画（局部时间轴） */
  .fgroup {
    margin: 2px 0 2px 8px;
    border-radius: 8px;
    background: rgba(124, 58, 237, 0.07);
  }
  /* 组内不透明底色会盖掉组底色，悬停改用同色系叠加 */
  .fgroup .tspan:hover {
    background: rgba(124, 58, 237, 0.1);
  }
  .fgroup .tw {
    width: 14px;
    height: 14px;
  }
  .tspan.d2 .tname {
    display: flex;
    align-items: center;
    padding-left: 20px;
    color: var(--fg);
    font-weight: 600;
  }
  .tspan.d3 .tname {
    padding-left: 34px;
  }
  .flabel {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    padding: 0;
    border: none;
    background: transparent;
    color: inherit;
    font: inherit;
    text-align: left;
    text-overflow: ellipsis;
    white-space: nowrap;
    cursor: pointer;
  }
  .thint {
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .tspan.root .tname {
    color: var(--fg);
    font-weight: 600;
  }
  .tclock {
    margin-right: 6px;
    font-weight: 400;
    color: var(--faint);
  }
  .ttrack {
    position: relative;
    height: 12px;
    border-radius: 3px;
    background: var(--bg-soft);
  }
  .tspan:hover .ttrack,
  .tspan.picked .ttrack {
    background: var(--line);
  }
  .ttrack i {
    position: absolute;
    top: 0;
    bottom: 0;
    min-width: 2px;
    border-radius: 3px;
  }
  .k-turn {
    background: #2563eb;
  }
  .k-fork {
    background: #7c3aed;
  }
  .k-model {
    background: #0891b2;
  }
  .k-tool {
    background: #16a34a;
  }
  .k-compact {
    background: #a3a3a3;
  }
  .tms {
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    color: var(--faint);
    text-align: right;
  }

  /* ── 调用链视图：span 详情（停靠抽屉底部，attrs 原文）── */
  .tdet {
    flex: none;
    max-height: 42%;
    overflow-y: auto;
    border-top: 1px solid var(--line-strong);
  }
  .tdet-head {
    position: sticky;
    top: 0;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 16px;
    background: var(--bg-soft);
    border-bottom: 1px solid var(--line);
  }
  .tdet-name {
    font-family: var(--font-mono);
    font-size: var(--font-size-ui);
    font-weight: 600;
    color: var(--fg);
  }
  .tdet-kind {
    font-size: var(--font-size-small);
    color: var(--muted);
    background: var(--line);
    border-radius: 5px;
    padding: 1px 6px;
  }
  .tdet-dur {
    margin-left: auto;
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .tdet-x {
    border: none;
    background: transparent;
    color: var(--faint);
    font-size: var(--font-size-small);
    padding: 2px 5px;
    border-radius: 5px;
    cursor: pointer;
  }
  .tdet-x:hover {
    color: var(--fg);
    background: var(--line);
  }
  .tattr {
    display: flex;
    gap: 10px;
    padding: 6px 16px;
    border-bottom: 1px solid var(--line);
  }
  .tattr:last-child {
    border-bottom: none;
  }
  .tak {
    flex: none;
    width: 96px;
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    color: var(--muted);
    padding-top: 1px;
  }
  .tav {
    flex: 1;
    min-width: 0;
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    line-height: 1.5;
    color: var(--muted);
    white-space: pre-wrap;
    word-break: break-word;
  }
  /* 消息行：角色标签在上、正文占满整行。窄抽屉里不做固定角色列，
     否则正文整体右移、可用宽度被吃掉 */
  .rv {
    display: flex;
    flex-direction: column;
    gap: 2px;
    font-size: var(--font-size-ui);
    line-height: 1.5;
  }
  .rv.me .rtext {
    color: var(--muted);
  }
  .rrole {
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    line-height: 1.3;
    color: var(--faint);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .rtext {
    min-width: 0;
    white-space: pre-wrap;
    word-break: break-word;
  }
  /* tool 结果行首的认领标签：工具名 + 调用 ID（ID 长，缩小靠后置） */
  .rcall {
    display: inline-block;
    margin-right: 6px;
    padding: 0 6px;
    border-radius: 6px;
    background: var(--bg-soft);
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    color: var(--fg);
  }
  .rcall em {
    font-style: normal;
    font-size: var(--font-size-small);
    color: var(--faint);
  }
</style>
