/* 全局状态机：bootstrap 模式（会话隐藏）+ SSE 事件归约，Svelte 5 runes。 */

import {
  api,
  subscribe,
  type HistoryMessage,
  type Settings,
  type SseEvent,
  type Status,
} from './api'

export interface ToolBlockData {
  id: string
  name: string
  args: string
  result: string
  err: string
  state: 'running' | 'done'
}

export interface ForkState {
  id: string
  task: string
  status: 'running' | 'done'
  text: string
  reasoning: string
  tools: ToolBlockData[]
  answer: string
  stopReason: string
  collapsed: boolean
}

export interface DecisionData {
  id: string
  dtype: 'approve' | 'ask' | 'plan'
  name: string
  args: string
  question: string
  plan: string
  forkId: string
  resolved: boolean
  resolution: string
}

export type Block =
  | { kind: 'user'; text: string }
  | { kind: 'assistant'; text: string; reasoning: string; streaming: boolean }
  | { kind: 'tool' } & ToolBlockData
  | { kind: 'fork'; forkId: string }
  | { kind: 'decision' } & DecisionData
  | { kind: 'note'; text: string }

export interface TotalUsage {
  prompt: number
  completion: number
  cached: number
}

class AppStore {
  activeId = $state('')
  blocks = $state<Block[]>([])
  forks = $state<Record<string, ForkState>>({})
  busy = $state(false)
  lastStatus = $state('')
  tick = $state(0)

  status = $state<Status | null>(null)
  settings = $state<Settings | null>(null)
  total = $state<TotalUsage>({ prompt: 0, completion: 0, cached: 0 })
  panel = $state<'' | 'settings' | 'memory' | 'topics'>('')

  private unsub: (() => void) | null = null

  /* ── 启动 ── */

  async bootstrap() {
    const b = await api.bootstrap()
    this.activeId = b.sessionId
    this.settings = b.settings
    this.status = b.status
    await this.loadHistory()
    this.unsub?.()
    this.unsub = subscribe(this.activeId, (ev) => this.apply(ev))
  }

  async refreshStatus() {
    try {
      this.status = await api.status()
    } catch {
      /* 静默 */
    }
  }

  private async loadHistory() {
    this.blocks = []
    this.forks = {}
    this.busy = false
    this.lastStatus = ''
    try {
      const s = await api.getHistory(this.activeId)
      this.rebuild(s.messages)
      this.busy = s.busy
    } catch {
      /* 网络异常时保底空时间线 */
    }
  }

  /* 历史重建：user/assistant/tool 消息序列，tool_calls 展开为工具块 */
  private rebuild(messages: HistoryMessage[]) {
    const out: Block[] = []
    for (const m of messages) {
      if (m.role === 'user') {
        out.push({ kind: 'user', text: m.content })
      } else if (m.role === 'assistant') {
        if (m.content || m.reasoning) {
          out.push({ kind: 'assistant', text: m.content, reasoning: m.reasoning || '', streaming: false })
        }
        // 展开工具调用：名称与参数来自 tool_calls（此前丢失的渲染）
        for (const tc of m.tool_calls || []) {
          out.push({
            kind: 'tool',
            id: tc.ID || '',
            name: tc.Name || '',
            args: tc.Args || '',
            result: '',
            err: '',
            state: 'done',
          })
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
    this.blocks = out
  }

  /* ── 发送 / 取消 ── */

  async send(text: string) {
    if (!this.activeId || !text.trim()) return
    this.blocks.push({ kind: 'user', text })
    this.busy = true
    this.lastStatus = ''
    try {
      await api.send(this.activeId, text)
    } catch (e) {
      this.busy = false
      this.lastStatus = `发送失败：${(e as Error).message}`
    }
  }

  async cancel() {
    if (!this.activeId) return
    await api.cancel(this.activeId).catch(() => {})
  }

  async summarize() {
    if (!this.activeId) return
    this.lastStatus = '摘要中…'
    try {
      const { text } = await api.summarize(this.activeId)
      this.blocks.push({ kind: 'assistant', text: `📝 ${text}`, reasoning: '', streaming: false })
    } catch (e) {
      this.lastStatus = `摘要失败：${(e as Error).message}`
    }
  }

  /* ── 设置 / 记忆 / 话题 ── */

  async saveSettings(s: Settings) {
    await api.saveSettings(s)
    this.settings = s
    this.lastStatus = '设置已保存（模型与提示即时生效）'
    await this.refreshStatus()
  }

  async resumeTopic(id: string) {
    try {
      const r = await api.resumeTopic(id)
      this.activeId = r.id
      await this.loadHistory()
      this.unsub?.()
      this.unsub = subscribe(this.activeId, (ev) => this.apply(ev))
      this.blocks.push({ kind: 'note', text: '⟲ 已回到该话题继续' })
      await this.refreshStatus()
    } catch (e) {
      this.lastStatus = `回到话题失败：${(e as Error).message}`
    }
  }

  /* ── 决策回传 ── */

  async decideApprove(block: DecisionData, approve: boolean, reason: string) {
    if (!this.activeId) return
    block.resolved = true
    block.resolution = approve ? '已批准' : reason ? `已拒绝：${reason}` : '已拒绝'
    await api.decideApprove(this.activeId, block.id, approve, reason).catch(() => {})
  }

  async decideAnswer(block: DecisionData, input: string) {
    if (!this.activeId) return
    block.resolved = true
    block.resolution = input || '(未回答)'
    await api.decideAnswer(this.activeId, block.id, input).catch(() => {})
  }

  async decidePlan(block: DecisionData, kind: 'execute' | 'reject' | 'revise', input: string) {
    if (!this.activeId) return
    block.resolved = true
    block.resolution =
      kind === 'execute' ? '已执行' : kind === 'reject' ? '已否决' : `修改意见：${input}`
    await api.decidePlan(this.activeId, block.id, kind, input).catch(() => {})
  }

  /* ── 事件归约 ── */

  apply(ev: SseEvent) {
    this.tick++
    switch (ev.type) {
      case 'model_chunk':
      case 'reasoning_chunk': {
        const delta: string = typeof ev.data === 'string' ? ev.data : ''
        if (!delta) break
        if (ev.forkId) {
          const f = this.forks[ev.forkId]
          if (f) {
            if (ev.type === 'model_chunk') f.text += delta
            else f.reasoning += delta
          }
        } else {
          this.appendMain(delta, ev.type === 'model_chunk')
        }
        break
      }
      case 'model_end': {
        const last = this.lastStreamingAssistant()
        if (last) last.streaming = false
        const u = ev.data?.usage
        if (u && this.status) {
          this.status.contextTokens = u.PromptTokens || 0
        }
        break
      }
      case 'tool_start': {
        const d = ev.data || {}
        const tool: ToolBlockData = {
          id: d.id || '',
          name: d.name || '',
          args: typeof d.args === 'string' ? d.args : JSON.stringify(d.args ?? ''),
          result: '',
          err: '',
          state: 'running',
        }
        if (ev.forkId) {
          this.forks[ev.forkId]?.tools.push(tool)
        } else {
          this.blocks.push({ kind: 'tool', ...tool })
        }
        break
      }
      case 'tool_end': {
        const d = ev.data || {}
        const target = ev.forkId
          ? this.forks[ev.forkId]?.tools.find((t) => t.id === d.callId)
          : this.blocks.find((b) => b.kind === 'tool' && b.id === d.callId)
        if (target && target.kind === 'tool') {
          target.result = d.content || ''
          target.err = d.err || ''
          target.state = 'done'
        }
        break
      }
      case 'task.start': {
        const d = ev.data || {}
        const fid = d.id || ev.forkId || ''
        this.forks[fid] = {
          id: fid,
          task: d.task || '',
          status: 'running',
          text: '',
          reasoning: '',
          tools: [],
          answer: '',
          stopReason: '',
          collapsed: false,
        }
        this.blocks.push({ kind: 'fork', forkId: fid })
        break
      }
      case 'task.end': {
        const fid = ev.forkId || ev.data?.id || ''
        const f = this.forks[fid]
        if (f) {
          f.status = 'done'
          f.answer = ev.data?.answer || ''
          f.stopReason = ev.data?.stopReason || ''
          f.collapsed = true
        }
        break
      }
      case 'rotate.topic': {
        const d = ev.data || {}
        this.blocks.push({
          kind: 'note',
          text: d.auto
            ? `⟲ 上下文水位达到阈值，话题《${d.title || ''}》已归档，新话题开始`
            : `⟲ 话题《${d.title || ''}》已归档，新话题开始`,
        })
        // 会话已轮换：切到新 ID 重新订阅（决策回传等 URL 用新标识）
        if (d.newId && d.newId !== this.activeId) {
          this.activeId = d.newId
          this.unsub?.()
          this.unsub = subscribe(this.activeId, (ev2) => this.apply(ev2))
        }
        void this.refreshStatus()
        break
      }
      case 'approve.request':
      case 'askuser.request':
      case 'taskplan.request': {
        const d = ev.data || {}
        let args = d.args
        if (typeof args !== 'string') args = JSON.stringify(args ?? {})
        let question = ''
        let plan = ''
        try {
          const a = typeof args === 'string' && args ? JSON.parse(args) : {}
          question = a.question || ''
          plan = a.plan || ''
        } catch {
          /* 非法 JSON 忽略 */
        }
        this.blocks.push({
          kind: 'decision',
          id: d.id || '',
          dtype:
            ev.type === 'approve.request' ? 'approve' : ev.type === 'askuser.request' ? 'ask' : 'plan',
          name: d.name || '',
          args: typeof args === 'string' ? args : '',
          question,
          plan,
          forkId: ev.forkId || '',
          resolved: false,
          resolution: '',
        })
        break
      }
      case 'error': {
        const msg = typeof ev.data === 'string' ? ev.data : JSON.stringify(ev.data ?? '')
        this.blocks.push({ kind: 'assistant', text: `⚠️ ${msg}`, reasoning: '', streaming: false })
        break
      }
      case 'turn_end': {
        this.busy = false
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
        void this.refreshStatus()
        break
      }
    }
  }

  private appendMain(delta: string, isContent: boolean) {
    let last = this.lastStreamingAssistant()
    if (!last) {
      this.blocks.push({ kind: 'assistant', text: '', reasoning: '', streaming: true })
      last = this.blocks[this.blocks.length - 1] as Extract<Block, { kind: 'assistant' }>
    }
    if (isContent) last.text += delta
    else last.reasoning += delta
  }

  private lastStreamingAssistant(): Extract<Block, { kind: 'assistant' }> | null {
    for (let i = this.blocks.length - 1; i >= 0; i--) {
      const b = this.blocks[i]
      if (b.kind === 'assistant') {
        return b.streaming ? b : null
      }
      if (b.kind === 'user') return null
    }
    return null
  }
}

export const store = new AppStore()
