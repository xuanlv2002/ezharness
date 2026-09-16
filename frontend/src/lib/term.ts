/*
共享终端连接管理:单条 WebSocket 多路复用全部终端(帧带 id 路由)。
hello/terminals 帧同步会话清单(含 AI 新建的),data 帧分发输出字节
(xterm.write 直收 Uint8Array,多字节序列由其内部缓冲解析),断线指数
退避重连,重连后 hello 快照恢复屏幕。模块单例,看板 keep-alive 常驻。
*/

export type TermInfo = {
  id: string
  name: string
  desc?: string
  origin: string
  exited: boolean
  lastCmd: string
}

type HelloSession = TermInfo & { snapshot: string }

function toB64(bytes: Uint8Array): string {
  let s = ''
  for (let i = 0; i < bytes.length; i++) s += String.fromCharCode(bytes[i])
  return btoa(s)
}

function fromB64(s: string): Uint8Array {
  const bin = atob(s)
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out
}

class TerminalManager {
  private ws: WebSocket | null = null
  private retry = 0
  private timer: number | undefined
  private sessionCbs = new Set<(ss: TermInfo[]) => void>()
  private dataCbs = new Set<(id: string, bytes: Uint8Array) => void>()
  private snapshotCbs = new Set<(id: string, bytes: Uint8Array) => void>()
  private stateCbs = new Set<(connected: boolean) => void>()
  connected = false

  /* connect 幂等建连(已连/连过不断重)。 */
  connect() {
    if (this.ws) return
    if (this.timer) {
      window.clearTimeout(this.timer)
      this.timer = undefined
    }
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
    const ws = new WebSocket(`${proto}//${location.host}/api/terminal/ws`)
    this.ws = ws
    ws.onopen = () => {
      this.retry = 0
      this.connected = true
      this.stateCbs.forEach((cb) => cb(true))
    }
    ws.onmessage = (ev) => this.onFrame(String(ev.data))
    ws.onclose = () => {
      this.ws = null
      if (this.connected) {
        this.connected = false
        this.stateCbs.forEach((cb) => cb(false))
      }
      this.scheduleReconnect()
    }
    ws.onerror = () => ws.close()
  }

  private scheduleReconnect() {
    if (this.timer !== undefined) return
    const delay = Math.min(1000 * 2 ** this.retry++, 10000)
    this.timer = window.setTimeout(() => {
      this.timer = undefined
      this.connect()
    }, delay)
  }

  private onFrame(raw: string) {
    let f: any
    try {
      f = JSON.parse(raw)
    } catch {
      return
    }
    switch (f.type) {
      case 'hello': {
        const ss: TermInfo[] = (f.sessions ?? []).map(({ snapshot, ...info }: HelloSession) => {
          if (snapshot) this.snapshotCbs.forEach((cb) => cb(info.id, fromB64(snapshot)))
          return info
        })
        this.sessionCbs.forEach((cb) => cb(ss))
        break
      }
      case 'terminals':
        this.sessionCbs.forEach((cb) => cb(f.sessions ?? []))
        break
      case 'data':
        this.dataCbs.forEach((cb) => cb(f.id, fromB64(f.data ?? '')))
        break
    }
  }

  private send(obj: unknown) {
    if (this.ws?.readyState === WebSocket.OPEN) this.ws.send(JSON.stringify(obj))
  }

  /* input 写入用户键入(后端走 UserInput,记录人为操作供 agent_status)。 */
  input(id: string, data: Uint8Array) {
    this.send({ type: 'input', id, data: toB64(data) })
  }

  resize(id: string, cols: number, rows: number) {
    this.send({ type: 'resize', id, cols, rows })
  }

  async create(name?: string, desc?: string): Promise<TermInfo> {
    const r = await fetch('/api/terminal/create', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, desc }),
    })
    if (!r.ok) throw new Error(`新建终端失败: ${r.status}`)
    return r.json()
  }

  async close(id: string): Promise<void> {
    await fetch('/api/terminal/close', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id }),
    })
  }

  onSessions(cb: (ss: TermInfo[]) => void): () => void {
    this.sessionCbs.add(cb)
    return () => this.sessionCbs.delete(cb)
  }

  onData(cb: (id: string, bytes: Uint8Array) => void): () => void {
    this.dataCbs.add(cb)
    return () => this.dataCbs.delete(cb)
  }

  onSnapshot(cb: (id: string, bytes: Uint8Array) => void): () => void {
    this.snapshotCbs.add(cb)
    return () => this.snapshotCbs.delete(cb)
  }

  onState(cb: (connected: boolean) => void): () => void {
    this.stateCbs.add(cb)
    cb(this.connected)
    return () => this.stateCbs.delete(cb)
  }
}

export const termManager = new TerminalManager()
