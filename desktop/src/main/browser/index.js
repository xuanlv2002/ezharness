/*
共享浏览器（desktop 资产）：每标签一个 WebContentsView，呈现页有两处
——主窗口工作区抽屉的浏览器页、可弹出的独立浏览器窗口（ez:popout）。
UI（标签条/地址栏）由呈现页的 BrowserPane 渲染，内容区 rect 由其
ResizeObserver/显隐联动上报、主进程 setBounds——视图宿主跟随最近
上报非零 rect 的窗口（同一时间只挂一处）；WebContentsView 不在 DOM
流内，显隐与位置全由这里管理。

AI 链路：桥客户端连 core 的 /api/browser/bridge，browser_* 工具调用
（start/navigate/read/eval/console/network/screenshot/upload/list/close）
在本模块直接操作对应标签的 webContents（executeJavaScript / capturePage /
CDP 截图与上传）后回执。页面里的点击/填值/滚动不设独立动作——模型用 eval
自己写（见 AGENTS 5.6）。console/network 的开发数据由 devtools.js 经 CDP
采集（建标签即 attach，用户开 devtools 即让出槽位）。

共见语义：AI start 或 browser:// chip 触发时由前端展开浏览器抽屉页，
视图贴抽屉内容区；抽屉收起/切走时前端上报零矩形，视图随之下线
（webContents 存活，AI 可继续操作）。应用退出时全部销毁。
*/
const path = require('path')
const { BrowserWindow, WebContentsView, ipcMain, session } = require('electron')
/* 主进程用 ws 客户端：可设请求头，令牌不再拼进 bridge URL */
const WebSocket = require('ws')
const { attachInspectMenu } = require('../inspect')
const devtools = require('./devtools')
const acorn = require('../../vendor/acorn')

let corePort = 5260
let coreToken = ''
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

/* 浏览器标签的原生缩放（不预设缩放，就是真实渲染比例）；Ctrl+0 回到这里 */
const BROWSER_ZOOM = 1

/* Ctrl+滚轮/快捷键的缩放档（factor，非 zoomLevel） */
const ZOOM_MIN = 0.25
const ZOOM_MAX = 3
const ZOOM_STEP = 1.1

/* zoomTab 调整某个标签的缩放（Ctrl+滚轮 / Ctrl+±  / Ctrl+0 复位到默认）。 */
function zoomTab(wc, dir) {
  if (!wc || wc.isDestroyed()) return
  const cur = wc.getZoomFactor()
  const next = dir === 'reset' ? BROWSER_ZOOM : dir === 'in' ? Math.min(ZOOM_MAX, cur * ZOOM_STEP) : Math.max(ZOOM_MIN, cur / ZOOM_STEP)
  wc.setZoomFactor(next)
}

/* tabList 标签清单（带 active 标记）：当前激活标签只有主进程知道（它决定
   显示哪个视图），渲染层据此高亮标签条、定地址栏——不能让各窗口自己猜，
   新建/关闭后两边最容易不一致（页面切了、地址栏没换）。
   ai/busy 供标签条画标识与锁：ai=AI 建的标签，busy=AI 桥调用正在该标签上
   执行——提示用户此刻别抢着操作，避免人机同时动一张页面互相打架。 */
function tabList() {
  return [...tabs.entries()].map(([id, t]) => ({
    id, name: t.name, desc: t.desc || '', origin: t.origin, url: t.url, title: t.title,
    loading: t.loading, active: id === activeTab, ai: t.origin === 'AI', busy: !!t.agentBusy,
  }))
}

/* broadcastTabs 清单变化广播到**所有**呈现浏览器页的窗口（主窗口 + 各独立
   窗口，去重）。不能只发给当前宿主：宿主会随窗口显隐让位，让位后旁路窗口若
   收不到清单就会冻住（表现是标签条点关没反应、也看不到新标签）。
   多标签的加载/标题事件会在短时间内密集触发，这里合并到一个短窗口内只发
   一次，且内容没变就不发——渲染层每次收到都会全量重建标签条。
   immediate=true 跳过合并立即发（AI 忙碌锁这类状态过渡要当场见，合并窗口
   会把已完成调用的 true→false 抵消成"什么都没发生"）。 */
let tabsPayload = ''
let broadcastTimer = null
let tabsDirty = false
function broadcastTabs(immediate) {
  tabsDirty = true
  if (immediate) {
    if (broadcastTimer) {
      clearTimeout(broadcastTimer)
      broadcastTimer = null
    }
    flushTabs()
    return
  }
  if (broadcastTimer) return
  broadcastTimer = setTimeout(() => {
    broadcastTimer = null
    flushTabs()
  }, 60)
}
function flushTabs() {
  if (!tabsDirty) return
  tabsDirty = false
  const payload = JSON.stringify(tabList())
  if (payload === tabsPayload) return
  tabsPayload = payload
  const wins = new Set([getParentWindow(), ...paneWindows])
  for (const w of wins) {
    if (w && !w.isDestroyed()) w.webContents.send('ez-browser:tabs', payload)
  }
}

/* BUSY_LINGER 忙碌锁在最后一次桥调用结束后的保留时长：eval 这类瞬时调用
   若执行完立刻解锁，用户根本看不到锁；连续工具调用间隔内也保持锁亮，
   表达"AI 正在这片页面上干活"。 */
const BUSY_LINGER = 2500

/* markAgentBusy 给标签打/解 AI 忙碌标记。上锁立即广播，解锁延迟
   BUSY_LINGER 并把新调用视为连续活动（清掉待解锁定时器）。 */
function markAgentBusy(tab, on) {
  if (!tab) return
  if (tab.busyTimer) {
    clearTimeout(tab.busyTimer)
    tab.busyTimer = null
  }
  if (on) {
    if (!tab.agentBusy) {
      tab.agentBusy = true
      broadcastTabs(true)
    }
    return
  }
  tab.busyTimer = setTimeout(() => {
    tab.busyTimer = null
    if (!tab.agentBusy) return
    tab.agentBusy = false
    broadcastTabs(true)
  }, BUSY_LINGER)
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
      preload: path.join(__dirname, '../../preload/browser.js'), // 只管 Ctrl+滚轮缩放
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
  attachInspectMenu(wc)
  /* Ctrl（Windows/Linux）/ Cmd（macOS）+＋/－/0 缩放（滚轮在 preload 转发） */
  wc.on('before-input-event', (event, input) => {
    if (input.type !== 'keyDown' || !(input.control || input.meta) || input.alt) return
    if (input.key === '=' || input.key === '+') {
      event.preventDefault()
      zoomTab(wc, 'in')
    } else if (input.key === '-') {
      event.preventDefault()
      zoomTab(wc, 'out')
    } else if (input.key === '0') {
      event.preventDefault()
      zoomTab(wc, 'reset')
    }
  })
  /* 建标签即挂 CDP 的 Page 域（只为接管原生对话框——必须在弹窗之前就挂上，否则
     页面会被对话框挂住） */
  devtools.attachTab(tab)
  /* 用户一开该标签的开发者工具，就摘掉我们的 CDP 采集，把调试器槽位让出去
     （attach 是独占的，抢着人工调试不可接受） */
  wc.on('devtools-opened', () => devtools.detachDevtools(tab))
  /* beforeunload 的"确认离开"不让它拦住导航（AI 要能自由跳转） */
  wc.on('will-prevent-unload', (e) => e.preventDefault())
  /* 主框架导航清空采集缓冲（上一页的日志/请求不该混进新页面） */
  wc.on('did-start-navigation', (_e, _url, _inPlace, isMainFrame) => {
    if (isMainFrame) devtools.clearDevtools(tab)
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
  if (tab.busyTimer) clearTimeout(tab.busyTimer)
  devtools.detachDevtools(tab)
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

/*
	深查询前置（注入到每个页面脚本）：穿透 shadow root 与**同源** iframe——
	`document.querySelector` 两者都进不去，而后台管理系统常常整片在 iframe 里。
	跨域 iframe 的 contentDocument 抛错被跳过（够不着，read mode=elements 另行报告
	帧清单让模型知道）。__ezAbsRect 把元素矩形折算到最顶层视口坐标（iframe 内元素
	的 getBoundingClientRect 是相对它自己那个 frame 的）。
*/
const DEEP_JS = `
function __ezRoots(root) {
  const out = [root]
  const walk = (node) => {
    let all
    try { all = node.querySelectorAll('*') } catch { return }
    for (const el of all) {
      if (el.shadowRoot) { out.push(el.shadowRoot); walk(el.shadowRoot) }
      if (el.tagName === 'IFRAME') {
        let doc = null
        try { doc = el.contentDocument } catch { doc = null }
        if (doc) { out.push(doc); walk(doc) }
      }
    }
  }
  walk(root)
  return out
}
function __ezAll(sel) {
  const res = []
  for (const r of __ezRoots(document)) { try { res.push(...r.querySelectorAll(sel)) } catch {} }
  return res
}
function __ezOne(sel) { return __ezAll(sel)[0] || null }
function __ezAbsRect(el) {
  let r = el.getBoundingClientRect()
  let win = el.ownerDocument && el.ownerDocument.defaultView
  while (win && win.frameElement) {
    const fr = win.frameElement.getBoundingClientRect()
    r = { left: r.left + fr.left, top: r.top + fr.top, width: r.width, height: r.height }
    win = win.frameElement.ownerDocument && win.frameElement.ownerDocument.defaultView
  }
  return r
}
`

/* runDeep 在页面里执行一段代码（带深查询前置）。code 是 IIFE 体，可 return。 */
function runDeep(wc, code) {
  return wc.executeJavaScript(`(() => { ${DEEP_JS} ${code} })()`, true)
}

/* wrapEval 把用户脚本包成 async IIFE 源码:按 async 函数体解析(顶层 await 与
   return 都合法),末尾是表达式语句就提升为 return——浏览器 console 的取值语义;
   解析失败抛「语法错误: …」,报错行号折回用户脚本的行号。 */
function wrapEval(js) {
  const src = String(js ?? '')
  const prefix = '(async function () {\n'
  let body
  try {
    body = acorn.parse(prefix + src + '\n})', { ecmaVersion: 'latest' })
      .body[0].expression.body.body
  } catch (e) {
    const msg = String(e.message || e).replace(/\((\d+):(\d+)\)/,
      (_, l, c) => `(${Math.max(1, l - 1)}:${c})`)
    throw new Error(`语法错误: ${e.name || 'SyntaxError'}: ${msg}`)
  }
  const last = body[body.length - 1]
  if (last && last.type === 'ExpressionStatement') {
    const head = src.slice(0, last.start - prefix.length)
    const expr = src.slice(last.start - prefix.length, last.end - prefix.length)
    const tail = src.slice(last.end - prefix.length)
    return `(async () => {\n${head}return (\n${expr}\n)${tail}})()`
  }
  return `(async () => {\n${src}\n})()`
}

/* withSurface 截图前的上屏保障：视图未挂任何窗口时（浏览器抽屉没开过/
目标是非激活标签——syncHost 只把激活视图挂到上屏宿主），capturePage 报
UnknownVizError、CDP 等首帧直到护栏超时。临时把视图挂到主窗口外沿
（屏幕外 bounds：attach 有合成面但用户不可见），完事摘除并收敛宿主。 */
async function withSurface(tab, fn) {
  const hosts = [getParentWindow(), ...paneWindows].filter((w) => w && !w.isDestroyed())
  const attached = hosts.some((w) => w.contentView.children.includes(tab.view))
  const win = getParentWindow()
  if (attached || !win || win.isDestroyed()) return fn()
  win.contentView.addChildView(tab.view)
  const size = win.getContentSize()
  tab.view.setBounds({ x: -30000, y: 0, width: size[0] || 1280, height: size[1] || 800 })
  try {
    await delay(120) /* 等首帧合成 */
    return await fn()
  } finally {
    try {
      if (!win.isDestroyed() && win.contentView.children.includes(tab.view)) {
        win.contentView.removeChildView(tab.view)
      }
    } catch { /* 窗口已销毁 */ }
    syncHost()
  }
}

/* screenshot 截图。默认**视口**——就是浏览器真实渲染的那一块，尺寸与用户所见一致、
   像素坐标与页面视口坐标系一致（最贴近真实交互）；fullPage=true 才截整页，用于看
   整体结构（按视口高分段、每段原生分辨率，避免一张超高图被视觉模型压糊）。
   不做设备仿真：截的就是浏览器真实大小。一律抓「可见视口的合成画面」，不走
   captureBeyondViewport 那条扩展视口路径——它在 Electron 上会把没合成到的区域回成
   黑色。CDP 在 target 换代（导航提交/渲染进程重建的瞬间）会报 target closed 类
   错误——重挂**当前** target 重试一次再定论；仍不行则退 capturePage 保底。 */
async function screenshot(wc, fullPage, selector) {
  const shoot = () => (selector ? cdpElementShot(wc, selector) : fullPage ? cdpShot(wc) : cdpViewportShot(wc))
  try {
    return await shoot()
  } catch (err) {
    if (/target|session|context/i.test(String(err?.message || err))) {
      await delay(600)
      try {
        return await shoot()
      } catch (e2) {
        err = e2
      }
    }
    if (selector) throw err // 元素级截图（找不到元素等）不做整屏兜底
    /* CDP 不可用（devtools 已占调试器）时退直接截图，别整个失败 */
    const v = await captureViewport(wc)
    if (v) return { images: [v], note: 'CDP 截图不可用（可能开发者工具已打开），已退为直接截图' }
    throw err
  }
}

/* captureViewport 兜底截图：视图实际画面（未上屏时返回 null）。 */
async function captureViewport(wc) {
  try {
    const image = await wc.capturePage()
    if (!image.isEmpty()) return image.toPNG().toString('base64')
  } catch { /* 视图未上屏无合成 surface */ }
  return null
}

const CDP_VERSION = '1.3'
const SHOT_TILES = 5 // 整页分段段数上限（控制一次进上下文的图片数）

/*
	withShotEnv 截图共用环境：藏滚动条（分段会溢出，滚动条落图难看），跑完还原；
	用完即摘调试器（只摘自己挂的——长期挂着会让标签变慢）。fn(cdp) 在内层执行。
*/
async function withShotEnv(wc, fn) {
  const ours = !wc.debugger.isAttached()
  if (ours) wc.debugger.attach(CDP_VERSION)
  const cdp = (method, params) => wc.debugger.sendCommand(method, params)
  let hid = false
  try {
    try {
      await cdp('Emulation.setScrollbarsHidden', { hidden: true })
      hid = true
    } catch { /* 老版本无此命令，忽略 */ }
    return await fn(cdp)
  } finally {
    if (hid) {
      try {
        await cdp('Emulation.setScrollbarsHidden', { hidden: false })
      } catch { /* 忽略 */ }
    }
    if (ours) {
      try {
        if (!wc.isDestroyed() && wc.debugger.isAttached()) wc.debugger.detach()
      } catch { /* 视图已销毁 */ }
    }
  }
}

/* shotCapture 截图单次（首帧迟迟不合成时 sendCommand 可能无限挂——不设护栏会把整个
   桥调用拖到 core 侧 30s 超时，报错还是一句不痛不痒的"操作超时"）。 */
function shotCapture(cdp, params) {
  return Promise.race([
    cdp('Page.captureScreenshot', params).then((r) => r.data),
    delay(20000).then(() => { throw new Error('截图超时:页面长时间未合成首帧') }),
  ])
}

/* scrollPos 当前滚动位置（拿不到就当在顶部）。 */
async function scrollPos(cdp) {
  try {
    const r = await cdp('Runtime.evaluate', {
      expression: '[Math.round(window.scrollX), Math.round(window.scrollY)]',
      returnByValue: true,
    })
    if (r.result && Array.isArray(r.result.value)) {
      return { x: r.result.value[0] || 0, y: r.result.value[1] || 0 }
    }
  } catch { /* 忽略 */ }
  return { x: 0, y: 0 }
}

/* cdpViewportShot 视口截图：直接抓当前可见视口那一屏。不设 clip、不走扩展视口
   ——视口截图本来就不需要"截到视口之外"，而那条路径会把没合成到的区域回成黑色。 */
async function cdpViewportShot(wc) {
  return withShotEnv(wc, async (cdp) => {
    return { images: [await shotCapture(cdp, { format: 'png' })] }
  })
}

/* cdpElementShot 截某个元素（矩形→页面坐标）：验证图表/组件这类局部时只截它。 */
async function cdpElementShot(wc, selector) {
  return withShotEnv(wc, async (cdp) => {
    const r = await runDeep(wc, `
      const el = __ezOne(${JSON.stringify(selector)})
      if (!el) return null
      el.scrollIntoView({ block: 'center', inline: 'center' })
      const b = __ezAbsRect(el)
      return { x: b.left + window.scrollX, y: b.top + window.scrollY, w: b.width, h: b.height }
    `)
    if (!r) throw new Error(`元素 ${selector} 未找到`)
    const width = Math.max(1, Math.ceil(r.w))
    const height = Math.max(1, Math.ceil(r.h))
    const data = await shotCapture(cdp, {
      format: 'png',
      captureBeyondViewport: true,
      clip: { x: Math.round(r.x), y: Math.round(r.y), width, height, scale: 1 },
    })
    return { images: [data], note: `元素 ${selector}，${width}×${height}` }
  })
}

/* cdpShot 整页分段截图：逐段滚动到视口、抓真实画面，完了还原滚动位置。
   不用扩展视口取"宽度拉满"——那条路径在 Electron 上会把没合成到的区域回成黑色。
   代价：横向溢出到视口外的部分不在图里（附注提示用 scroll left/right 看）。 */
async function cdpShot(wc) {
  return withShotEnv(wc, async (cdp) => {
    const m = await cdp('Page.getLayoutMetrics')
    const cs = m.cssContentSize || m.contentSize || {}
    const lv = m.cssLayoutViewport || m.layoutViewport || {}
    const vw = Math.max(1, Math.ceil(lv.clientWidth || 0))
    const segH = Math.max(200, Math.ceil(lv.clientHeight || 0) || 800)
    const total = Math.ceil(cs.height || 0)
    if (total <= 0) return { images: [await shotCapture(cdp, { format: 'png' })] } // 尺寸拿不到：退单张
    const segs = Math.max(1, Math.ceil(total / segH))
    const n = Math.min(segs, SHOT_TILES)
    const pos = await scrollPos(cdp)
    const images = []
    try {
      for (let i = 0; i < n; i++) {
        await cdp('Runtime.evaluate', { expression: `window.scrollTo(${pos.x}, ${i * segH})` })
        await delay(120) // 等这一屏落帧再抓
        images.push(await shotCapture(cdp, { format: 'png' }))
      }
    } finally {
      try {
        await cdp('Runtime.evaluate', { expression: `window.scrollTo(${pos.x}, ${pos.y})` })
      } catch { /* 页面可能已跳走 */ }
    }
    const notes = []
    if (segs > n) notes.push(`页面共 ${segs} 段，已截前 ${n} 段；其余滚动后逐屏看或 viewport 截图`)
    if (Math.ceil(cs.width || 0) > vw + 1) notes.push(`页面有横向溢出（宽 ${Math.ceil(cs.width || 0)}，视口 ${vw}），横向部分用 scroll left/right 看`)
    return { images, note: notes.join('；') }
  })
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

/* frameReport 列出子 frame。深查询穿不过**跨域** iframe，这里把帧清单报出来，让
   模型知道可以改用 navigate 直达该 frame 的 URL。拿不到（未 attach）返回空串。 */
async function frameReport(tab) {
  const d = tab.dt
  if (!d || !d.attached || !d.wc || d.wc.isDestroyed()) return ''
  try {
    const { frameTree } = await d.wc.debugger.sendCommand('Page.getFrameTree')
    const frames = []
    const walk = (n) => {
      for (const c of n.childFrames || []) {
        frames.push(c.frame)
        walk(c)
      }
    }
    walk(frameTree)
    if (!frames.length) return ''
    const lines = frames.map((f) => `  ${f.url || '(空)'}${f.name ? ` [name=${f.name}]` : ''}`)
    return `（页面含 ${frames.length} 个子 frame；同源的已并入上面清单，跨域的读不到——可 browser_action navigate 直达其 URL 或改读主文档）\n` + lines.join('\n')
  } catch {
    return ''
  }
}

/* executors 方法名 → {result, images?}；抛错 = 工具失败。
   result 统一为标签状态 JSON（read 的正文放 output，close 为 {id,closed}）。 */
const executors = {
  async start({ name, desc, url, timeoutMs }) {
    const id = createTab(name, desc, 'AI', '')
    const tab = tabs.get(id)
    const note = '已创建,后续操作用返回的 id;内嵌于工作区抽屉,用户实时共见可随时接管'
    if (!url) return { result: JSON.stringify({ ...tabState(id), note }) }
    markAgentBusy(tab, true)
    try {
      const title = await loadSettled(tab.view.webContents, completeURL(url), timeoutMs || 20000)
      const state = tabState(id)
      if (title) state.title = title
      return { result: JSON.stringify({ ...state, note: `${note};页面已加载` }) }
    } finally {
      markAgentBusy(tab, false)
    }
  },

  async navigate({ tabId, url, timeoutMs }) {
    const tab = tabOf(tabId)
    const title = await loadSettled(tab.view.webContents, completeURL(url), timeoutMs || 20000)
    const state = tabState(tab.id)
    if (title) state.title = title
    return { result: JSON.stringify(state) }
  },

  /* upload 给 <input type=file> 塞文件：只能走 CDP DOM.setFileInputFiles（页面 JS 改不了
     files）。files 是本机绝对路径，逗号分隔；非 multiple 的 input 只设第一个。 */
  async upload({ tabId, selector, files }) {
    const tab = tabOf(tabId)
    if (!selector) throw new Error('upload 需要 selector')
    const list = String(files || '').split(',').map((s) => s.trim()).filter(Boolean)
    if (!list.length) throw new Error('upload 需要 files（本机绝对路径，逗号分隔）')
    const d = devtools.ensureDevtools(tab)
    if (!d.attached) throw new Error(`上传需要 CDP（${d.error || '未挂上'}）`)
    const wc = tab.view.webContents
    const info = await runDeep(wc, `
      const el = __ezOne(${JSON.stringify(selector)})
      return el ? { multiple: !!el.multiple } : null
    `)
    if (!info) throw new Error(`元素 ${selector} 未找到`)
    /* 非 multiple 的 file input 只认第一个文件（浏览器会静默丢掉其余），
       这里就只传第一个并把这件事说出来，别让模型以为全传上去了 */
    const apply = info.multiple ? list : list.slice(0, 1)
    const trimNote = !info.multiple && list.length > 1 ? `（该 input 非 multiple，只设置第一个，其余 ${list.length - 1} 个被忽略）` : ''
    const obj = await d.wc.debugger.sendCommand('Runtime.evaluate', {
      expression: `${DEEP_JS}\n__ezOne(${JSON.stringify(selector)})`,
      includeCommandLineAPI: false,
    })
    if (!obj.result || !obj.result.objectId) throw new Error(`定位 ${selector} 失败`)
    await d.wc.debugger.sendCommand('DOM.setFileInputFiles', { files: apply, objectId: obj.result.objectId })
    await delay(200)
    return { result: JSON.stringify({ ...tabState(tab.id), note: `已向 ${selector} 设置 ${apply.length} 个文件${trimNote}` }) }
  },

  async read({ tabId, mode, selector, chars, timeoutMs }) {
    const tab = tabOf(tabId)
    const limit = chars > 0 ? chars : 4000
    const wc = tab.view.webContents
    await waitIdle(wc, timeoutMs || 8000)
    let body
    if (mode === 'elements') {
      body = await runDeep(wc, `
        const attr = (v) => String(v).replace(/"/g, '&quot;')
        const cssId = (id) => '#' + (window.CSS && CSS.escape ? CSS.escape(id) : id)
        const unique = (sel) => { try { return __ezAll(sel).length === 1 } catch { return false } }
        /* selector 生成优先级:#id > [name] > [aria-label] > [placeholder] > nth-of-type 路径 */
        const path = (el) => {
          const tag = el.tagName.toLowerCase()
          if (el.id && unique(cssId(el.id))) return cssId(el.id)
          if (el.name && unique(tag + '[name="' + attr(el.name) + '"]')) return tag + '[name="' + attr(el.name) + '"]'
          const aria = el.getAttribute && el.getAttribute('aria-label')
          if (aria && unique(tag + '[aria-label="' + attr(aria) + '"]')) return tag + '[aria-label="' + attr(aria) + '"]'
          const ph = el.getAttribute && el.getAttribute('placeholder')
          if (ph && unique(tag + '[placeholder="' + attr(ph) + '"]')) return tag + '[placeholder="' + attr(ph) + '"]'
          const parts = []
          let n = el
          for (let i = 0; i < 5 && n && n !== el.ownerDocument.body; i++) {
            const parent = n.parentElement
            if (!parent) break
            const same = Array.from(parent.children).filter((c) => c.tagName === n.tagName)
            parts.unshift(same.length > 1 ? n.tagName.toLowerCase() + ':nth-of-type(' + (same.indexOf(n) + 1) + ')' : n.tagName.toLowerCase())
            n = parent
          }
          return parts.join('>')
        }
        const els = __ezAll('a,button,input,select,textarea,[role="button"],[role="link"],[role="tab"],[role="menuitem"],[contenteditable="true"]')
        const out = []
        for (const el of els) {
          if (out.length >= 150) break
          const r = el.getBoundingClientRect()
          if (r.width < 2 || r.height < 2) continue
          const st = getComputedStyle(el)
          if (st.display === 'none' || st.visibility === 'hidden') continue
          let sel
          try { sel = path(el) } catch { continue }
          if (!sel) continue
          const inFrame = el.ownerDocument !== document
          const text = (el.innerText || el.value || el.placeholder || (el.getAttribute && el.getAttribute('aria-label')) || '')
            .trim().replace(/\\s+/g, ' ').slice(0, 60)
          let line = (inFrame ? '[frame] ' : '') + el.tagName.toLowerCase() + ' [' + sel + ']'
          if (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA') line += ' type=' + (el.type || 'text')
          if (text) line += ' "' + text + '"'
          if (el.tagName === 'A' && el.href) line += ' -> ' + el.href.slice(0, 100)
          out.push(line)
        }
        return out.join('\\n')
      `)
    } else if (mode === 'html') {
      body = selector
        ? await runDeep(wc, `
            const el = __ezOne(${JSON.stringify(selector)})
            return el ? el.outerHTML : '(未找到元素 ' + ${JSON.stringify(selector)} + ')'
          `)
        : await wc.executeJavaScript(`document.documentElement.outerHTML`, true)
    } else if (mode === 'element') {
      if (!selector) throw new Error('mode=element 需要 selector')
      body = await runDeep(wc, `
        const el = __ezOne(${JSON.stringify(selector)})
        if (!el) return '(未找到元素 ' + ${JSON.stringify(selector)} + ')'
        const st = getComputedStyle(el)
        const r = __ezAbsRect(el)
        const chain = []
        for (let n = el; n && n.nodeType === 1 && chain.length < 6; n = n.parentElement) {
          const cls = (typeof n.className === 'string' && n.className.trim()) ? '.' + n.className.trim().split(/\\s+/).slice(0, 2).join('.') : ''
          chain.unshift(n.tagName.toLowerCase() + (n.id ? '#' + n.id : '') + cls)
        }
        const attrs = [...el.attributes].map((a) => a.name + '=' + JSON.stringify(a.value)).join(' ')
        const props = ['display', 'position', 'width', 'height', 'color', 'background-color', 'font-size', 'font-weight', 'opacity', 'visibility', 'overflow', 'z-index', 'pointer-events']
        const cs = props.map((p) => p + ':' + st.getPropertyValue(p)).join('; ')
        const text = (el.innerText || el.value || '').trim().replace(/\\s+/g, ' ').slice(0, 200)
        const frame = el.ownerDocument !== document ? '(位于 iframe 内) ' : ''
        return '路径 ' + frame + chain.join(' > ') + '\\n' +
          el.tagName.toLowerCase() + (attrs ? ' ' + attrs : '') + '\\n' +
          '盒 ' + Math.round(r.width) + 'x' + Math.round(r.height) + ' @' + Math.round(r.left) + ',' + Math.round(r.top) + '(顶层视口坐标)\\n' +
          cs + (text ? '\\ntext "' + text + '"' : '')
      `)
    } else if (mode === 'links') {
      body = await runDeep(wc, `
        return __ezAll('a').slice(0, 200)
          .map(a => (a.innerText.trim().slice(0,60) || '(无文字)') + ' → ' + a.href).join('\\n')
      `)
    } else if (mode === 'frames') {
      body = (await frameReport(tab)) || '(无子 frame)'
    } else {
      body = await wc.executeJavaScript(`document.body ? document.body.innerText : '(空页面)'`, true)
    }
    if (mode === 'elements') {
      const extra = await frameReport(tab)
      if (extra) body = String(body || '(无可交互元素)') + '\n' + extra
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

  async eval({ tabId, js }) {
    const tab = tabOf(tabId)
    const wc = tab.view.webContents
    await waitIdle(wc, 3000)
    /* 主进程先解析包装(语法错误在这里拦下),页面侧只负责执行与异常透传 */
    const wrapped = wrapEval(js)
    const script = `(async (src) => await (0, eval)('(async()=>{try{return await('+src+')}catch(e){throw new Error((e&&(e.stack||e.message))||String(e))}})()'))(${JSON.stringify(wrapped)})`
    let val
    try {
      val = await wc.executeJavaScript(script, true)
    } catch (err) {
      throw new Error(`eval 执行出错：${String(err?.message || err)}`)
    }
    let text
    if (val === undefined) text = '(已执行，无返回值)'
    else if (typeof val === 'string') text = val
    else {
      try { text = JSON.stringify(val) } catch { text = String(val) }
    }
    if (text && text.length > 8000) text = text.slice(0, 8000) + '\n…(过长已截断)'
    /* 全局视觉变形会持续污染后续所有截图与坐标（曾有人用 zoom 缩页面把二维码挪进
       视口，结果 selector 截图截到空白）——改了就当场提示，并给出复位入口。 */
    const deformed = await runDeep(wc, `
      const out = []
      for (const pair of [['html', document.documentElement], ['body', document.body]]) {
        const el = pair[1]
        if (!el || !el.style) continue
        for (const p of ['zoom', 'transform', 'filter']) {
          const v = el.style.getPropertyValue(p)
          if (v) out.push(pair[0] + '.' + p + '=' + v)
        }
      }
      return out
    `).catch(() => [])
    if (deformed && deformed.length) {
      text += `\n⚠ 你改动了页面全局视觉属性（${deformed.join('、')}）——这会持续偏移所有元素坐标、selector 截图会截到空白。还原用 action=reset`
    }
    return { result: JSON.stringify({ ...tabState(tab.id), output: text }) }
  },

  async console({ tabId, mode, errorsOnly }) {
    const tab = tabOf(tabId)
    const d = devtools.ensureDevtools(tab)
    if (d.error) return { result: JSON.stringify({ ...tabState(tab.id), output: `(控制台采集不可用：${d.error})` }) }
    if (mode === 'clear') {
      devtools.clearDevtools(tab)
      return { result: JSON.stringify({ ...tabState(tab.id), output: '(控制台已清空)' }) }
    }
    return { result: JSON.stringify({ ...tabState(tab.id), output: devtools.formatConsole(tab, !!errorsOnly) }) }
  },

  async network({ tabId, index, filter, errorsOnly }) {
    const tab = tabOf(tabId)
    const d = devtools.ensureDevtools(tab)
    if (d.error) return { result: JSON.stringify({ ...tabState(tab.id), output: `(网络采集不可用：${d.error})` }) }
    const out = index > 0
      ? await devtools.formatNetworkDetail(tab, index)
      : devtools.formatNetworkList(tab, { filter: filter || '', errorsOnly: !!errorsOnly })
    return { result: JSON.stringify({ ...tabState(tab.id), output: out }) }
  },

  async screenshot({ tabId, fullPage, selector, timeoutMs }) {
    const tab = tabOf(tabId)
    const wc = tab.view.webContents
    await waitIdle(wc, timeoutMs || 10000)
    const shot = await withSurface(tab, () => screenshot(wc, !!fullPage, selector || ''))
    return { result: JSON.stringify(tabState(tab.id)), images: shot.images, note: shot.note || '' }
  },

  async activate({ tabId }) {
    const tab = tabOf(tabId)
    selectTab(tab.id)
    return { result: JSON.stringify({ ...tabState(tab.id), note: '已切到前台（用户也会看到这张标签）' }) }
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
  const ws = new WebSocket(`ws://127.0.0.1:${corePort}/api/browser/bridge`, {
    headers: { 'X-EZ-Token': coreToken },
  })
  bridge = ws
  ws.onmessage = async (ev) => {
    let req
    try { req = JSON.parse(String(ev.data)) } catch { return }
    const exec = executors[req.method]
    let reply
    if (!exec) {
      reply = { id: req.id, ok: false, error: `未知浏览器指令 ${req.method}` }
    } else {
      const params = req.params || {}
      /* 执行期间给目标标签打 busy（标签条画锁）；结束后延迟释放，瞬时
         调用也看得见锁 */
      const tab = params.tabId ? tabs.get(params.tabId) : null
      markAgentBusy(tab, true)
      try {
        const out = await exec(params)
        reply = { id: req.id, ok: true, result: out.result, images: out.images, note: out.note }
      } catch (err) {
        reply = { id: req.id, ok: false, error: String(err?.message || err) }
      } finally {
        markAgentBusy(tab, false)
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
    if (!tab || !url) return
    /* AI 操作中（标签条亮锁）：用户改地址一律忽略，别和 AI 抢同一个页面。
       AI 自己的导航走 executors 直接调用，不经过这条 IPC */
    if (tab.agentBusy) return
    tab.view.webContents.loadURL(completeURL(url))
  })
  ipcMain.on('ez-browser:select', (_e, tabId) => selectTab(tabId))
  ipcMain.on('ez-browser:close', (_e, tabId) => closeTab(tabId))
  /* Ctrl+滚轮：preload 在页面里捕获后转发（sender 就是该标签的 webContents） */
  ipcMain.on('ez-browser:wheel-zoom', (e, dir) => zoomTab(e.sender, dir))

  /* browser:// chip 定位标签（前端同时展开抽屉页） */
  ipcMain.on('ez-browser:focus-tab', (_e, tabId) => {
    if (tabs.has(tabId)) selectTab(tabId)
  })
}

/* startBrowserModule 壳装配入口。 */
function startBrowserModule({ corePort: port, token, getParentWindow: parent, findBrowserOwner }) {
  corePort = port
  coreToken = token || ''
  getParentWindow = parent
  findOwner = findBrowserOwner || (() => null)
  registerIpc()
  connectBridge()
}

/* overlayBrowser(hide)：主窗页面弹 modal（关闭确认框等）时临时藏起
激活标签的视图——WebContentsView 是 OS 层子视图，永远浮在页面 DOM
之上，页内 z-index 盖不住它（"弹框不在最上层"的根因）。恢复时无脑
setVisible(true)（视图只可能因这里被藏）。 */
let hiddenForOverlay = false
function overlayBrowser(hide) {
  const t = tabs.get(activeTab)
  if (!t) return
  if (hide) {
    const parent = getParentWindow()
    if (hostWindow && parent && hostWindow === parent && !parent.isDestroyed()) {
      hiddenForOverlay = true
      t.view.setVisible(false)
    }
  } else if (hiddenForOverlay) {
    hiddenForOverlay = false
    if (!t.view.webContents.isDestroyed()) t.view.setVisible(true)
  }
}

module.exports = { startBrowserModule, detachViewsFromWindow, overlayBrowser }
