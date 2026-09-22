/*
共享浏览器（desktop 资产）：每标签一个 WebContentsView，呈现页有两处
——主窗口工作区抽屉的浏览器页、可弹出的独立浏览器窗口（ez:popout）。
UI（标签条/地址栏）由呈现页的 BrowserPane 渲染，内容区 rect 由其
ResizeObserver/显隐联动上报、主进程 setBounds——视图宿主跟随最近
上报非零 rect 的窗口（同一时间只挂一处）；WebContentsView 不在 DOM
流内，显隐与位置全由这里管理。

AI 链路：桥客户端连 core 的 /api/browser/bridge，browser_* 工具调用
（start/navigate/click/type/key/scroll/read/screenshot/list/close）在本
模块直接操作对应标签的 webContents（executeJavaScript / sendInputEvent /
capturePage / CDP 整页截图）后回执。

共见语义：AI start 或 browser:// chip 触发时由前端展开浏览器抽屉页，
视图贴抽屉内容区；抽屉收起/切走时前端上报零矩形，视图随之下线
（webContents 存活，AI 可继续操作）。应用退出时全部销毁。
*/
const { BrowserWindow, WebContentsView, ipcMain, session } = require('electron')

let corePort = 5260
let getParentWindow = null
let bridge = null          // WebSocket → core
let bridgeReconnectTimer = null

/** @type {Map<string, object>} tabID → {view, name, origin, url, title, loading} */
const tabs = new Map()
let seq = 0
let activeTab = ''
let contentRect = { x: 0, y: 0, width: 0, height: 0 }
let paneVisible = false // 浏览器页是否在上屏（rect 非零）
let hostWindow = null // 视图宿主：最近上报非零 rect 的窗口（主窗口或弹出窗口）

/* ── 工具函数 ── */

function completeURL(raw) {
  if (!raw || raw.startsWith('about:')) return raw || 'about:blank'
  return raw.includes('://') ? raw : `https://${raw}`
}

/* 用户新建标签的默认起始页（AI 的 browser_tab open 不带 url 仍为空白） */
const HOME_PAGE = 'https://cn.bing.com/'

/* tabList 标签清单（带 active 标记）：当前激活标签只有主进程知道（它决定
   显示哪个视图），渲染层据此高亮标签条、定地址栏——不能让各窗口自己猜，
   新建/关闭后两边最容易不一致（页面切了、地址栏没换）。 */
function tabList() {
  return [...tabs.entries()].map(([id, t]) => ({
    id, name: t.name, desc: t.desc || '', origin: t.origin, url: t.url, title: t.title,
    loading: t.loading, active: id === activeTab,
  }))
}

/* broadcastTabs 清单变化广播到**所有**呈现浏览器页的窗口（主窗口 + 各独立
   窗口，去重）。不能只发给当前宿主：宿主会随窗口显隐让位，让位后旁路窗口若
   收不到清单就会冻住（表现是标签条点关没反应、也看不到新标签）。
   多标签的加载/标题事件会在短时间内密集触发，这里合并到一个短窗口内只发
   一次，且内容没变就不发——渲染层每次收到都会全量重建标签条。 */
let tabsPayload = ''
let broadcastTimer = null
let tabsDirty = false
function broadcastTabs() {
  tabsDirty = true
  if (broadcastTimer) return
  broadcastTimer = setTimeout(() => {
    broadcastTimer = null
    if (!tabsDirty) return
    tabsDirty = false
    const payload = JSON.stringify(tabList())
    if (payload === tabsPayload) return
    tabsPayload = payload
    const wins = new Set([getParentWindow(), ...paneWindows])
    for (const w of wins) {
      if (w && !w.isDestroyed()) w.webContents.send('ez-browser:tabs', payload)
    }
  }, 60)
}

/* tabState 工具回执的标签状态对象（list 与各操作同构；id 即标签 id，
   模型引用它）。空值给括号语言说明（title 未设、desc 未填），
   模型不会把空串误读成"字段丢了"。 */
function tabState(id) {
  const t = tabs.get(id)
  if (!t) return { id, note: '标签已不存在' }
  return {
    id, name: t.name, ...(t.desc ? { desc: t.desc } : {}), origin: t.origin,
    url: t.url, title: t.title || '(无标题)', loading: !!t.loading,
  }
}

/* ── 视图管理 ── */

/* findOwner 由壳注入：浏览器页已弹出为独立窗口时返回那个窗口。
   视图只有一个，抽屉与独立窗口同时上屏就会互相抢（两边都在报 rect，谁最后
   报谁显示、另一个白屏并来回闪）——所以弹出期间视图固定归独立窗口。 */
let findOwner = () => null

/* ownerWindow 当前该由谁显示视图（无独立窗口 = nil，不限制）。 */
function ownerWindow() {
  const w = findOwner()
  return w && !w.isDestroyed() ? w : null
}

/* paneWindows 挂着浏览器页（UI）的窗口：主窗口 + 各独立窗口。清单变化要广播
   给全部这些窗口，跨窗口摘视图时也要在它们之间找。入口 = BrowserPane 挂载时
   的 list 调用 + 每次 rect 上报；窗口关闭即移除。 */
const paneWindows = new Set()
function notePane(win) {
  if (!win || win.isDestroyed() || paneWindows.has(win)) return
  paneWindows.add(win)
  win.on('close', (e) => {
    if (e.defaultPrevented) return
    if (hostWindow === win) detachViewsFromWindow(win)
    paneWindows.delete(win)
  })
}

/* askRepane 请除 except 之外的浏览器页窗口重新上报一次 rect。视图归属只认
   rect 上报，而"当前宿主被隐藏/收起"这类让位不会让渲染层自发重报——不主动
   问一声，视图就丢在半空：内容区白屏，也不再有窗口收得到标签更新。 */
function askRepane(except) {
  const wins = new Set([getParentWindow(), ...paneWindows])
  for (const w of wins) {
    if (!w || w === except || w.isDestroyed()) continue
    w.webContents.send('ez-browser:refresh-rect')
  }
}

/* detachView 把视图从除 keep 之外的所有浏览器页窗口上摘下来。
   视图同一时刻只能挂一处：Electron 的 addChildView 只承诺"同一父容器内
   重复添加会置顶"，跨窗口挂载未定义——主窗口 ↔ 独立窗口切换（含拖拽脱离）
   时必须先摘旧的，否则页面可能双份合成、切标签明显变卡。 */
function detachView(view, keep) {
  for (const w of paneWindows) {
    if (w === keep || w.isDestroyed()) continue
    if (w.contentView.children.includes(view)) w.contentView.removeChildView(view)
  }
}

/* syncHost 收敛「宿主窗口 ↔ 激活视图」这条不变量：只有激活标签的视图挂在
   宿主上（bounds = contentRect），其余视图一律不在任何窗口上。切标签/关标签/
   换宿主都只走这一处，别在各处散着写。
   注意**不要**在这里调 webContents.invalidate() 之类强制重绘：窗口自己的
   整窗重绘会把子视图的合成面丢掉，页面要等自身下一帧才回来——表现就是
   每次切/关标签当前页都像"刷新"了一下甚至变白。视图结构变了 Chromium 自己
   会重排重绘，不需要我们推。 */
function syncHost() {
  const active = tabs.get(activeTab)
  for (const tab of tabs.values()) {
    if (tab !== active) detachView(tab.view, null)
  }
  if (!active) return
  if (!hostWindow || hostWindow.isDestroyed() || !paneVisible) {
    detachView(active.view, null)
    return
  }
  if (!hostWindow.contentView.children.includes(active.view)) {
    detachView(active.view, hostWindow)
    hostWindow.contentView.addChildView(active.view)
  }
  active.view.setBounds({ ...contentRect })
}

/* detachViewsFromWindow 把窗口上的激活视图摘出来（webContents 存活），
并复位贴靠状态。
视图是窗口的子视图，窗口销毁会连视图一起销毁（标签全丢）——窗口关闭
（含拖拽取消的预览窗口）前必须先摘。摘完请主窗口重新上报一次 rect：
视图回挂只认 rect 上报，而抽屉一直开着时渲染层不会自发重报。 */
function detachViewsFromWindow(win) {
  const active = tabs.get(activeTab)
  if (active && win && !win.isDestroyed()) win.contentView.removeChildView(active.view)
  if (hostWindow === win) {
    paneVisible = false
    hostWindow = null
  }
  /* 别的窗口可能还开着浏览器页：请它重新报一次 rect 接手视图 */
  askRepane(win)
}

/* selectTab 激活标签：其余视图卸载（webContents 存活，重新挂载即恢复）。 */
function selectTab(id) {
  if (!tabs.has(id)) return
  activeTab = id
  syncHost()
  broadcastTabs()
}

/* createTab 新建标签（origin:"用户"|"AI"；name 简短标识，desc 用途描述）；
   startURL 空则 about:blank。 */
function createTab(name, desc, origin, startURL) {
  seq += 1
  const id = `b${seq}`
  const url = completeURL(startURL)
  const view = new WebContentsView({
    webPreferences: {
      session: session.fromPartition('persist:ezbrowser'),
    },
  })
  const tab = { id, view, name: name || id, desc: desc || '', origin, url, title: '', loading: !!startURL }
  tabs.set(id, tab)

  const wc = view.webContents
  wc.setWindowOpenHandler(({ url: target }) => {
    /* target=_blank 就地开新标签（同浏览习惯），不弹独立窗口 */
    createTab(hostOf(target) || '新标签', '', '用户', target)
    return { action: 'deny' }
  })
  wc.on('did-start-loading', () => { tab.loading = true; broadcastTabs() })
  wc.on('did-stop-loading', () => { tab.loading = false; broadcastTabs() })
  wc.on('did-navigate', (_e, target) => { tab.url = target; broadcastTabs() })
  wc.on('page-title-updated', (_e, title) => { tab.title = title })
  wc.on('page-title-set', () => { broadcastTabs() })

  selectTab(id)
  if (startURL) wc.loadURL(url)
  else tab.loading = false
  broadcastTabs()
  return id
}

function hostOf(raw) {
  try { return new URL(raw).host } catch { return '' }
}

/* closeTab 关闭标签（视图销毁）；空态由抽屉页 UI 呈现。 */
function closeTab(id) {
  const tab = tabs.get(id)
  if (!tab) return
  detachView(tab.view, null)
  tabs.delete(id)
  if (activeTab === id) {
    activeTab = ''
    const nextID = [...tabs.keys()].pop()
    if (nextID) activeTab = nextID
  }
  syncHost() // 先把接替的视图挂好，再销毁旧视图
  broadcastTabs()
  /* 视图已摘、接替的已挂上，销毁推到下一 tick：别和"挂新视图"挤在同一帧里 */
  setTimeout(() => {
    try {
      tab.view.webContents.close()
    } catch {
      /* 已销毁 */
    }
  }, 0)
}

/* loadSettled 导航并等加载收尾（did-finish/did-fail 或超时），返回尽力而为
的标题。必须走 loadURL 返回的 promise（页面收尾时落定）：旧实现先 fire
loadLoad 再查 isLoading()，而 loadURL 尚未把 loading 置位——竞态下
"!isLoading" 立即成立，返回「已加载」时页面根本没开始加载，紧接着的
截图就撞在首帧未合成上。超时/失败都照常返回（不 throw）：标签状态里
url/title/loading 自明，调用方语义是"尽力等到稳定"。 */
async function loadSettled(wc, url, timeoutMs) {
  try {
    await Promise.race([
      wc.loadURL(url).catch(() => {}), /* 加载失败（DNS/拒绝）不算调用失败 */
      delay(timeoutMs || 20000),
    ])
  } catch { /* 不会到这，兜底 */ }
  return wc.getTitle()
}

/* waitIdle 等加载收尾（每 150ms 轮询 isLoading，封顶 capMs 后放行）——
截图/读取在页面加载中硬上，轻则读到半页，重则首帧未合成把 CDP 拖到
桥超时。放行而非报错：降级语义，晚到好过不到。 */
async function waitIdle(wc, capMs) {
  const cap = capMs > 0 ? capMs : 0
  const start = Date.now()
  while (wc.isLoading() && (!cap || Date.now() - start < cap)) await delay(150)
}

/* ── 执行器（browser_* 工具的直接实现） ── */

/* clickAt 视口坐标点击（move 前置：悬浮态/hover 依赖）。 */
function clickAt(wc, x, y) {
  wc.sendInputEvent({ type: 'mouseMove', x, y, button: 'none' })
  wc.sendInputEvent({ type: 'mouseDown', x, y, button: 'left', clickCount: 1 })
  wc.sendInputEvent({ type: 'mouseUp', x, y, button: 'left', clickCount: 1 })
}

/* elementPoint selector → 视口中心坐标（元素不存在返回 null）。 */
async function elementPoint(wc, selector) {
  const safe = JSON.stringify(selector)
  return wc.executeJavaScript(`
    (() => {
      const el = document.querySelector(${safe})
      if (!el) return null
      el.scrollIntoView({ block: 'center', inline: 'center' })
      const r = el.getBoundingClientRect()
      return [r.left + r.width / 2, r.top + r.height / 2]
    })()
  `, true)
}

/* DOM 键名 → Electron keyCode。 */
const KEY_CODES = {
  Enter: 'Enter', Backspace: 'Backspace', Tab: 'Tab', Escape: 'Escape', Space: 'Space',
  Delete: 'Delete', Insert: 'Insert', Home: 'Home', End: 'End',
  PageUp: 'PageUp', PageDown: 'PageDown',
  ArrowLeft: 'Left', ArrowUp: 'Up', ArrowRight: 'Right', ArrowDown: 'Down',
}

/* sendKey 组合键 down+up（modifiers 数组形）。 */
function sendKey(wc, key, modifiers) {
  const keyCode = KEY_CODES[key] || key
  wc.sendInputEvent({ type: 'keyDown', modifiers, keyCode })
  wc.sendInputEvent({ type: 'keyUp', modifiers, keyCode })
}

/* screenshot 视口截图；fullPage 走 CDP captureBeyondViewport。
抽屉收起/视图卸载时 capturePage 可能空白甚至 reject(UnknownVizError:
视图未上屏无合成 surface)——统一优先 capturePage,空图或抛错回落 CDP。
CDP 在 target 换代（导航提交/渲染进程重建的瞬间）会报 target closed 类
错误，与页面挂了无法区分——按 tabId 重挂**当前** target 重试一次再定论。 */
async function screenshot(wc, fullPage) {
  const params = fullPage ? { captureBeyondViewport: true } : {}
  if (!fullPage) {
    try {
      const image = await wc.capturePage()
      if (!image.isEmpty()) return image.toPNG().toString('base64')
    } catch { /* 视图未上屏:回落 CDP */ }
  }
  try {
    return await cdpScreenshot(wc, params)
  } catch (err) {
    if (!/target|session|context/i.test(String(err?.message || err))) throw err
    await delay(600)
    return cdpScreenshot(wc, params)
  }
}

const CDP_VERSION = '1.3'
async function cdpScreenshot(wc, extraParams) {
  const ours = !wc.debugger.isAttached()
  if (ours) wc.debugger.attach(CDP_VERSION)
  try {
    /* 首帧迟迟不合成时 sendCommand 可能无限挂——不设护栏会把整个桥调用
       拖到 core 侧 30s 超时，报错还是一句不痛不痒的"操作超时" */
    const { data } = await Promise.race([
      wc.debugger.sendCommand('Page.captureScreenshot', { format: 'png', ...extraParams }),
      delay(20000).then(() => { throw new Error('截图超时:页面长时间未合成首帧') }),
    ])
    return data
  } finally {
    /* 用完即摘（只摘自己挂的）：调试器长期挂着会让该标签明显变慢，
       也影响渲染进程的节流 */
    if (ours) {
      try {
        if (!wc.isDestroyed() && wc.debugger.isAttached()) wc.debugger.detach()
      } catch { /* 视图已销毁 */ }
    }
  }
}

/* tabOf 取目标标签（tabId 必填）；取不到报错附当前标签清单（模型自愈）。 */
function tabOf(tabId) {
  const tab = tabs.get(tabId)
  if (!tab) {
    const list = tabList().map((t) => `${t.id}(${t.name})`).join('、') || '无'
    throw new Error(`浏览器标签不存在（当前已有标签：${list}）`)
  }
  return tab
}

/* executors 方法名 → {result, imageB64?}；抛错 = 工具失败。
   result 统一为标签状态 JSON（read 的正文放 output，close 为 {id,closed}）。 */
const executors = {
  async start({ name, desc, url, timeoutMs }) {
    const id = createTab(name, desc, 'AI', '')
    const note = '已创建,后续操作用返回的 id;内嵌于工作区抽屉,用户实时共见可随时接管'
    if (!url) return { result: JSON.stringify({ ...tabState(id), note }) }
    const tab = tabs.get(id)
    const title = await loadSettled(tab.view.webContents, completeURL(url), timeoutMs || 20000)
    const state = tabState(id)
    if (title) state.title = title
    return { result: JSON.stringify({ ...state, note: `${note};页面已加载` }) }
  },

  async navigate({ tabId, url, timeoutMs }) {
    const tab = tabOf(tabId)
    const title = await loadSettled(tab.view.webContents, completeURL(url), timeoutMs || 20000)
    const state = tabState(tab.id)
    if (title) state.title = title
    return { result: JSON.stringify(state) }
  },

  async click({ tabId, selector, x, y }) {
    const tab = tabOf(tabId)
    const wc = tab.view.webContents
    if (selector) {
      const point = await elementPoint(wc, selector)
      if (!point) throw new Error(`元素 ${selector} 未找到`)
      clickAt(wc, point[0], point[1])
    } else {
      clickAt(wc, x || 0, y || 0)
    }
    await delay(300)
    return { result: JSON.stringify(tabState(tab.id)) }
  },

  async type({ tabId, selector, text, submit }) {
    const tab = tabOf(tabId)
    const wc = tab.view.webContents
    if (selector) {
      const point = await elementPoint(wc, selector)
      if (!point) throw new Error(`输入框 ${selector} 未找到`)
      clickAt(wc, point[0], point[1])
      await delay(120)
    }
    wc.focus()
    wc.insertText(text)
    if (submit) {
      await delay(120)
      sendKey(wc, 'Enter', [])
      await delay(500)
    }
    return { result: JSON.stringify(tabState(tab.id)) }
  },

  async key({ tabId, combo }) {
    const tab = tabOf(tabId)
    const parts = String(combo || '').split('+').map((s) => s.trim()).filter(Boolean)
    const key = parts[parts.length - 1]
    const modifiers = parts.slice(0, -1).map((m) => {
      const lower = m.toLowerCase()
      if (lower === 'control' || lower === 'ctrl') return 'control'
      if (lower === 'alt' || lower === 'option') return 'alt'
      if (lower === 'shift') return 'shift'
      if (lower === 'meta' || lower === 'cmd' || lower === 'command' || lower === 'os') return 'meta'
      throw new Error(`未知修饰键 ${m}`)
    })
    sendKey(tab.view.webContents, key, modifiers)
    await delay(300)
    return { result: JSON.stringify(tabState(tab.id)) }
  },

  async scroll({ tabId, direction, amountPx }) {
    const tab = tabOf(tabId)
    const amount = amountPx > 0 ? amountPx : 600
    const wc = tab.view.webContents
    const bounds = wc.getBounds() // 视口尺寸（挂主窗口后的 view bounds）
    wc.sendInputEvent({
      type: 'mouseWheel',
      x: bounds.width / 2, y: bounds.height / 2,
      deltaY: direction === 'up' ? -amount : amount,
    })
    await delay(200)
    return { result: JSON.stringify(tabState(tab.id)) }
  },

  async read({ tabId, mode, chars, timeoutMs }) {
    const tab = tabOf(tabId)
    const limit = chars > 0 ? chars : 4000
    const wc = tab.view.webContents
    await waitIdle(wc, timeoutMs || 8000)
    let body
    if (mode === 'links') {
      body = await wc.executeJavaScript(`
        Array.from(document.querySelectorAll('a')).slice(0, 200)
          .map(a => (a.innerText.trim().slice(0,60) || '(无文字)') + ' → ' + a.href).join('\\n')
      `, true)
    } else {
      body = await wc.executeJavaScript(`document.body ? document.body.innerText : '(空页面)'`, true)
    }
    const text = String(body ?? '')
    const truncated = text.length > limit
    return {
      result: JSON.stringify({
        ...tabState(tab.id),
        output: truncated
          ? `${text.slice(0, limit)}\n…(过长已截断)`
          : text || (mode === 'links' ? '(无链接)' : '(空页面)'),
        note: truncated ? '过长已截断' : undefined,
      }),
    }
  },

  async screenshot({ tabId, fullPage, timeoutMs }) {
    const tab = tabOf(tabId)
    const wc = tab.view.webContents
    await waitIdle(wc, timeoutMs || 10000)
    const imageB64 = await screenshot(wc, !!fullPage)
    return { result: JSON.stringify(tabState(tab.id)), imageB64 }
  },

  async list() {
    return { result: JSON.stringify(tabList()) }
  },

  async close({ tabId }) {
    if (!tabs.has(tabId)) return { result: JSON.stringify({ id: tabId, closed: true, note: '标签已不存在(幂等)' }) }
    closeTab(tabId)
    return { result: JSON.stringify({ id: tabId, closed: true }) }
  },
}

function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

/* ── core 桥客户端 ── */

function connectBridge() {
  if (bridge) return
  const ws = new WebSocket(`ws://127.0.0.1:${corePort}/api/browser/bridge`)
  bridge = ws
  ws.onmessage = async (ev) => {
    let req
    try { req = JSON.parse(String(ev.data)) } catch { return }
    const exec = executors[req.method]
    let reply
    if (!exec) {
      reply = { id: req.id, ok: false, error: `未知浏览器指令 ${req.method}` }
    } else {
      try {
        const out = await exec(req.params || {})
        reply = { id: req.id, ok: true, result: out.result, imageB64: out.imageB64 }
      } catch (err) {
        reply = { id: req.id, ok: false, error: String(err?.message || err) }
      }
    }
    if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify(reply))
  }
  const drop = () => {
    if (bridge === ws) bridge = null
    bridgeReconnectTimer = setTimeout(() => {
      bridgeReconnectTimer = null
      connectBridge()
    }, 3000)
  }
  ws.onclose = drop
  ws.onerror = () => ws.close()
}

/* ── renderer IPC（浏览器页 UI + 联动入口；上报者可来自主窗口或弹出窗口） ── */

function registerIpc() {
  /* 浏览器页内容区 rect 上报（ResizeObserver + 显隐联动）：
     非零 = 该页上屏，上报窗口成为宿主、激活视图贴靠；零 = 该页收起/
     切走，仅当上报者就是宿主时视图下线（另一窗口仍呈现则不动） */
  ipcMain.on('ez-browser:content-rect', (e, rect) => {
    const sender = BrowserWindow.fromWebContents(e.sender)
    if (!sender || sender.isDestroyed()) return
    notePane(sender)
    if (!rect || rect.width < 10 || rect.height < 10) {
      if (sender !== hostWindow) return
      paneVisible = false
      const active = tabs.get(activeTab)
      if (active) detachView(active.view, null)
      /* 让出宿主：还开着浏览器页的窗口重新报一次 rect，谁在上屏谁接手 */
      askRepane(sender)
      return
    }
    /* 已弹出为独立窗口：视图归它，其它窗口的上报一律不认（否则两边互相抢，
       表现就是一个窗口白屏 + 来回闪） */
    const owner = ownerWindow()
    if (owner && owner !== sender) {
      if (hostWindow !== owner) {
        hostWindow = owner
        paneVisible = true
        owner.webContents.send('ez-browser:refresh-rect')
      }
      return
    }
    contentRect = rect
    paneVisible = true
    hostWindow = sender
    syncHost()
  })
  /* BrowserPane 挂载时的清单拉取：同时登记"这个窗口有浏览器页"（广播对象） */
  /* BrowserPane 挂载时的清单拉取：同时登记"这个窗口有浏览器页"（广播对象） */
  ipcMain.handle('ez-browser:list', (e) => {
    notePane(BrowserWindow.fromWebContents(e.sender))
    return JSON.stringify(tabList())
  })
  ipcMain.on('ez-browser:create', (_e, url) => {
    createTab(hostOf(url || '') || '新标签', '', '用户', url || HOME_PAGE)
  })
  ipcMain.on('ez-browser:navigate', (_e, tabId, url) => {
    const tab = tabs.get(tabId)
    if (tab && url) tab.view.webContents.loadURL(completeURL(url))
  })
  ipcMain.on('ez-browser:select', (_e, tabId) => selectTab(tabId))
  ipcMain.on('ez-browser:close', (_e, tabId) => closeTab(tabId))

  /* browser:// chip 定位标签（前端同时展开抽屉页） */
  ipcMain.on('ez-browser:focus-tab', (_e, tabId) => {
    if (tabs.has(tabId)) selectTab(tabId)
  })
}

/* startBrowserModule 壳装配入口。 */
function startBrowserModule({ corePort: port, getParentWindow: parent, findBrowserOwner }) {
  corePort = port
  getParentWindow = parent
  findOwner = findBrowserOwner || (() => null)
  registerIpc()
  connectBridge()
}

module.exports = { startBrowserModule, detachViewsFromWindow }
