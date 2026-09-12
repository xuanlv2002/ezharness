/*
共享浏览器（desktop 资产）：独立浏览器窗口 + 每标签一个 WebContentsView。
浏览器窗口加载 core 伺服的 /browser.html（标签条/地址栏 UI 框架），内容
区 rect 由 renderer ResizeObserver 上报、主进程 setBounds——WebContentsView
不在 DOM 流内，显隐与位置全由这里管理。

AI 链路：桥客户端连 core 的 /api/browser/bridge，browser_* 工具调用
（start/navigate/click/type/key/scroll/read/screenshot/list/close）在本
模块直接操作对应标签的 webContents（executeJavaScript / sendInputEvent /
capturePage / CDP 整页截图）后回执。

窗口语义：AI start 或用户入口触发时创建并置前（共见）；用户点 X 关闭
= 隐藏保留（标签后台存活，AI 可能正在用）；应用退出时全部销毁。
*/
const { BrowserWindow, WebContentsView, ipcMain, session } = require('electron')
const path = require('path')

let corePort = 5260
let getParentWindow = null
let bridge = null          // WebSocket → core
let bridgeReconnectTimer = null

let browserWindow = null
/** @type {Map<string, object>} tabID → {view, name, origin, url, title, loading} */
const tabs = new Map()
let seq = 0
let activeTab = ''
let contentRect = { x: 0, y: 0, width: 0, height: 0 }

/* ── 工具函数 ── */

function completeURL(raw) {
  if (!raw || raw.startsWith('about:')) return raw || 'about:blank'
  return raw.includes('://') ? raw : `https://${raw}`
}

function tabList() {
  return [...tabs.entries()].map(([id, t]) => ({
    id, name: t.name, origin: t.origin, url: t.url, title: t.title, loading: t.loading,
  }))
}

/* broadcastTabs 清单变化广播到浏览器窗口（标签条）与主窗口（联动入口）。 */
function broadcastTabs() {
  const payload = JSON.stringify(tabList())
  browserWindow?.webContents.send('ez-browser:tabs', payload)
  getParentWindow()?.webContents.send('ez-browser:tabs', payload)
}

/* stateLine 工具回执的状态头行。 */
function stateLine(id) {
  const t = tabs.get(id)
  if (!t) return `[浏览器 #${id} 已不存在]`
  return `[浏览器 #${id} "${t.name}"] 页面: ${t.title}${t.loading ? '(加载中)' : ''}`
}

/* ── 浏览器窗口与视图管理 ── */

/* ensureBrowserWindow 创建（或复用）浏览器窗口并置前。 */
function ensureBrowserWindow() {
  if (browserWindow && !browserWindow.isDestroyed()) {
    browserWindow.show()
    browserWindow.focus()
    return
  }
  browserWindow = new BrowserWindow({
    width: 1200,
    height: 820,
    title: 'ezharness · 浏览器',
    autoHideMenuBar: true,
    show: false,
    webPreferences: {
      preload: path.join(__dirname, '../../preload/index.js'),
    },
  })
  const base = process.env.EZHARNESS_DEV_URL || `http://127.0.0.1:${corePort}`
  browserWindow.loadURL(`${base}/browser.html`)
  browserWindow.once('ready-to-show', () => browserWindow.show())
  /* 关窗 = 隐藏保留：标签后台存活（AI 可能正在用），入口可再次唤起 */
  browserWindow.on('close', (e) => {
    if (!browserWindow) return
    e.preventDefault()
    browserWindow.hide()
  })
  browserWindow.on('resize', () => applyBounds())
}

/* applyBounds 把激活标签的 view 贴到 renderer 上报的内容区。 */
function applyBounds() {
  const active = tabs.get(activeTab)
  if (active && browserWindow && !browserWindow.isDestroyed()) {
    active.view.setBounds({ ...contentRect })
  }
}

/* attachView 挂载并贴 bounds（激活路径专用）。 */
function attachView(tab) {
  browserWindow.contentView.addChildView(tab.view)
  tab.view.setBounds({ ...contentRect })
}

/* selectTab 激活标签：其余视图卸载（webContents 存活，重新挂载即恢复）。 */
function selectTab(id) {
  const next = tabs.get(id)
  if (!next) return
  if (activeTab && activeTab !== id) {
    const prev = tabs.get(activeTab)
    if (prev) browserWindow?.contentView.removeChildView(prev.view)
  }
  activeTab = id
  if (browserWindow && !browserWindow.isDestroyed()) {
    if (!browserWindow.contentView.children.includes(next.view)) attachView(next)
    else applyBounds()
  }
  broadcastTabs()
}

/* createTab 新建标签（origin:"用户"|"AI"）；startURL 空则 about:blank。 */
function createTab(name, origin, startURL) {
  ensureBrowserWindow()
  seq += 1
  const id = `b${seq}`
  const url = completeURL(startURL)
  const view = new WebContentsView({
    webPreferences: {
      session: session.fromPartition('persist:ezbrowser'),
    },
  })
  const tab = { view, name: name || id, origin, url, title: '', loading: !!startURL }
  tabs.set(id, tab)

  const wc = view.webContents
  wc.setWindowOpenHandler(({ url: target }) => {
    /* target=_blank 就地开新标签（同浏览习惯），不弹独立窗口 */
    createTab(hostOf(target) || '新标签', '用户', target)
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

/* closeTab 关闭标签（视图销毁）；最后一个标签关掉仅隐藏窗口。 */
function closeTab(id) {
  const tab = tabs.get(id)
  if (!tab) return
  browserWindow?.contentView.removeChildView(tab.view)
  tab.view.webContents.close()
  tabs.delete(id)
  if (activeTab === id) {
    activeTab = ''
    const nextID = [...tabs.keys()].pop()
    if (nextID) selectTab(nextID)
    else if (browserWindow) browserWindow.hide()
  }
  broadcastTabs()
}

/* waitLoad 等主帧加载完成（或超时），返回尽力而为的标题。 */
function waitLoad(wc, timeoutMs) {
  return new Promise((resolve) => {
    const timer = setTimeout(() => cleanup(), timeoutMs)
    const done = () => { cleanup(); resolve(wc.getTitle()) }
    const cleanup = () => {
      clearTimeout(timer)
      wc.removeListener('did-finish-load', done)
      wc.removeListener('did-fail-load', done)
    }
    wc.on('did-finish-load', done)
    wc.on('did-fail-load', done)
    if (!wc.isLoading()) done()
  })
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
窗口隐藏时 capturePage 可能空白——统一优先 CDP，失败回落 capturePage。 */
async function screenshot(wc, fullPage) {
  if (fullPage) return cdpScreenshot(wc, { captureBeyondViewport: true })
  const image = await wc.capturePage()
  if (!image.isEmpty()) return image.toPNG().toString('base64')
  return cdpScreenshot(wc, {})
}

const CDP_VERSION = '1.3'
async function cdpScreenshot(wc, extraParams) {
  if (!wc.debugger.isAttached()) wc.debugger.attach(CDP_VERSION)
  const { data } = await wc.debugger.sendCommand('Page.captureScreenshot', {
    format: 'png', ...extraParams,
  })
  return data
}

/* executors 方法名 → {result, imageB64?}；抛错 = 工具失败。 */
const executors = {
  async start({ desc, url, timeoutMs }) {
    const id = createTab(desc, 'AI', '')
    const head = `[浏览器 #${id} "${desc}" 已创建,内嵌于浏览器窗口,用户实时共见可随时接管]`
    if (!url) return { result: head }
    const tab = tabs.get(id)
    tab.view.webContents.loadURL(completeURL(url))
    const title = await waitLoad(tab.view.webContents, timeoutMs || 20000)
    return { result: `${head}\n页面: ${title || '(无标题)'}` }
  },

  async navigate({ tabId, url, timeoutMs }) {
    const tab = tabs.get(tabId)
    if (!tab) throw new Error(`浏览器标签 ${tabId} 不存在(browser_list 可查)`)
    tab.view.webContents.loadURL(completeURL(url))
    const title = await waitLoad(tab.view.webContents, timeoutMs || 20000)
    return { result: `${stateLine(tabId)}\n页面: ${title || '(无标题)'}` }
  },

  async click({ tabId, selector, x, y }) {
    const tab = tabs.get(tabId)
    if (!tab) throw new Error(`浏览器标签 ${tabId} 不存在`)
    const wc = tab.view.webContents
    if (selector) {
      const point = await elementPoint(wc, selector)
      if (!point) throw new Error(`元素 ${selector} 未找到`)
      clickAt(wc, point[0], point[1])
    } else {
      clickAt(wc, x || 0, y || 0)
    }
    await delay(300)
    return { result: stateLine(tabId) }
  },

  async type({ tabId, selector, text, submit }) {
    const tab = tabs.get(tabId)
    if (!tab) throw new Error(`浏览器标签 ${tabId} 不存在`)
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
    return { result: stateLine(tabId) }
  },

  async key({ tabId, combo }) {
    const tab = tabs.get(tabId)
    if (!tab) throw new Error(`浏览器标签 ${tabId} 不存在`)
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
    return { result: stateLine(tabId) }
  },

  async scroll({ tabId, direction, amountPx }) {
    const tab = tabs.get(tabId)
    if (!tab) throw new Error(`浏览器标签 ${tabId} 不存在`)
    const amount = amountPx > 0 ? amountPx : 600
    const wc = tab.view.webContents
    const size = wc.getOwnerBrowserWindow()?.getContentSize() || [800, 600]
    wc.sendInputEvent({
      type: 'mouseWheel',
      x: size[0] / 2, y: size[1] / 2,
      deltaY: direction === 'up' ? -amount : amount,
    })
    await delay(200)
    return { result: stateLine(tabId) }
  },

  async read({ tabId, mode, chars }) {
    const tab = tabs.get(tabId)
    if (!tab) throw new Error(`浏览器标签 ${tabId} 不存在`)
    const limit = chars > 0 ? chars : 4000
    const wc = tab.view.webContents
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
    const truncated = text.length > limit ? `${text.slice(0, limit)}\n…(过长已截断)` : text
    return { result: `${stateLine(tabId)}\n${truncated}` }
  },

  async screenshot({ tabId, fullPage }) {
    const tab = tabs.get(tabId)
    if (!tab) throw new Error(`浏览器标签 ${tabId} 不存在`)
    const imageB64 = await screenshot(tab.view.webContents, !!fullPage)
    return { result: stateLine(tabId), imageB64 }
  },

  async list() {
    return { result: JSON.stringify(tabList()) }
  },

  async close({ tabId }) {
    if (!tabs.has(tabId)) return { result: `[浏览器 #${tabId} 已不存在]` }
    closeTab(tabId)
    return { result: `[浏览器 #${tabId} 已关闭]` }
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

/* ── renderer IPC（浏览器窗口 UI + 主应用入口） ── */

function registerIpc() {
  /* 浏览器窗口页面：内容区 rect 上报（ResizeObserver） */
  ipcMain.on('ez-browser:content-rect', (_e, rect) => {
    contentRect = rect
    applyBounds()
  })
  ipcMain.handle('ez-browser:list', () => JSON.stringify(tabList()))
  ipcMain.on('ez-browser:create', (_e, url) => {
    createTab(hostOf(url || '') || '新标签', '用户', url || '')
  })
  ipcMain.on('ez-browser:navigate', (_e, tabId, url) => {
    const tab = tabs.get(tabId)
    if (tab && url) tab.view.webContents.loadURL(completeURL(url))
  })
  ipcMain.on('ez-browser:select', (_e, tabId) => selectTab(tabId))
  ipcMain.on('ez-browser:close', (_e, tabId) => closeTab(tabId))

  /* 主应用入口：地球钮切换浏览器窗口显隐；browser:// chip 定位标签 */
  ipcMain.on('ez-browser:toggle-window', () => {
    ensureBrowserWindow()
    if (browserWindow.isVisible()) browserWindow.hide()
    else { browserWindow.show(); browserWindow.focus() }
  })
  ipcMain.on('ez-browser:focus-tab', (_e, tabId) => {
    if (!tabs.has(tabId)) return
    ensureBrowserWindow()
    browserWindow.show()
    browserWindow.focus()
    selectTab(tabId)
  })
}

/* startBrowserModule 壳装配入口。 */
function startBrowserModule({ corePort: port, getParentWindow: parent }) {
  corePort = port
  getParentWindow = parent
  registerIpc()
  connectBridge()
}

module.exports = { startBrowserModule }
