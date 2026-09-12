/*
共享浏览器连接管理:单条 WebSocket 多路复用全部标签(帧带 tabId 路由)。
hello/tabs 帧同步标签清单(含 AI 新建的),frame 帧是页面镜像 JPEG(data
URL 直喂 <img>),status 帧是浏览器启动/下载状态,window 帧是真窗口
唤起/收起状态;断线指数退避重连,重连后 hello 各标签最新帧恢复画面。
镜像仅供监视,页面操控在唤起的真窗口内原生完成;控制帧(navigate/
viewport/window)经同一条 WS 回传。模块单例,看板 keep-alive 常驻。
*/

export type BrowserTabInfo = {
  id: string
  name: string
  origin: string
  url: string
  title: string
  loading: boolean
}

export type BrowserStatus = { phase: string; message: string }

export type FrameImage = { dataUrl: string; width: number; height: number }

type HelloTab = BrowserTabInfo & { frame?: string; width?: number; height?: number }

class BrowserManager {
  private ws: WebSocket | null = null
  private retry = 0
  private timer: number | undefined
  private tabCbs = new Set<(tabs: BrowserTabInfo[]) => void>()
  private frameCbs = new Set<(id: string, img: FrameImage) => void>()
  private snapshotCbs = new Set<(id: string, img: FrameImage) => void>()
  private statusCbs = new Set<(status: BrowserStatus) => void>()
  private windowCbs = new Set<(shown: boolean) => void>()
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
    const ws = new WebSocket(`${proto}//${location.host}/api/browser/ws`)
    this.ws = ws
    ws.onopen = () => {
      this.retry = 0
      this.connected = true
      this.stateCbs.forEach((cb) => cb(true))
    }
    ws.onmessage = (ev) => this.handleFrame(String(ev.data))
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

  private handleFrame(raw: string) {
    let f: any
    try {
      f = JSON.parse(raw)
    } catch {
      return
    }
    switch (f.type) {
      case 'hello': {
        const tabs: BrowserTabInfo[] = []
        for (const t of (f.tabs ?? []) as HelloTab[]) {
          if (t.frame) {
            this.snapshotCbs.forEach((cb) =>
              cb(t.id, { dataUrl: `data:image/jpeg;base64,${t.frame}`, width: t.width || 0, height: t.height || 0 }),
            )
          }
          const { frame: _frame, width: _width, height: _height, ...info } = t
          tabs.push(info)
        }
        this.tabCbs.forEach((cb) => cb(tabs))
        if (f.status) this.statusCbs.forEach((cb) => cb(f.status))
        if (f.window) this.windowCbs.forEach((cb) => cb(f.window.shown))
        break
      }
      case 'tabs':
        this.tabCbs.forEach((cb) => cb(f.tabs ?? []))
        break
      case 'frame':
        if (f.data) {
          const img = { dataUrl: `data:image/jpeg;base64,${f.data}`, width: f.width || 0, height: f.height || 0 }
          this.frameCbs.forEach((cb) => cb(f.tabId, img))
        }
        break
      case 'status':
        if (f.status) this.statusCbs.forEach((cb) => cb(f.status))
        break
      case 'window':
        if (f.window) this.windowCbs.forEach((cb) => cb(f.window.shown))
        break
    }
  }

  private send(obj: unknown) {
    if (this.ws?.readyState === WebSocket.OPEN) this.ws.send(JSON.stringify(obj))
  }

  /* window 唤起/收起真浏览器窗口(用户原生接管入口);show 时 tabId 为要激活的标签。 */
  window(show: boolean, tabId?: string) {
    this.send({ type: 'window', show, tabId })
  }

  navigate(tabId: string, url: string) {
    this.send({ type: 'navigate', tabId, url })
  }

  /* viewport 镜像尺寸上报:width/height 为 CSS 像素,scaleFactor 为屏幕
  devicePixelRatio——后端按 DSF 渲染,screencast 帧达到物理分辨率(高 DPI 不模糊)。 */
  viewport(tabId: string, width: number, height: number, scaleFactor: number) {
    this.send({ type: 'viewport', tabId, width, height, scaleFactor })
  }

  async create(name?: string, url?: string): Promise<BrowserTabInfo> {
    const r = await fetch('/api/browser/create', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, url }),
    })
    if (!r.ok) throw new Error((await r.json().catch(() => ({}))).error || `新建标签失败: ${r.status}`)
    return r.json()
  }

  async close(id: string): Promise<void> {
    await fetch('/api/browser/close', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id }),
    })
  }

  onTabs(cb: (tabs: BrowserTabInfo[]) => void): () => void {
    this.tabCbs.add(cb)
    return () => this.tabCbs.delete(cb)
  }

  onFrame(cb: (id: string, img: FrameImage) => void): () => void {
    this.frameCbs.add(cb)
    return () => this.frameCbs.delete(cb)
  }

  onSnapshot(cb: (id: string, img: FrameImage) => void): () => void {
    this.snapshotCbs.add(cb)
    return () => this.snapshotCbs.delete(cb)
  }

  onStatus(cb: (status: BrowserStatus) => void): () => void {
    this.statusCbs.add(cb)
    return () => this.statusCbs.delete(cb)
  }

  onWindow(cb: (shown: boolean) => void): () => void {
    this.windowCbs.add(cb)
    return () => this.windowCbs.delete(cb)
  }

  onState(cb: (connected: boolean) => void): () => void {
    this.stateCbs.add(cb)
    cb(this.connected)
    return () => this.stateCbs.delete(cb)
  }
}

export const browserManager = new BrowserManager()
