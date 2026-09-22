/*
ezharness desktop：Electron 壳主进程。
职责：拉起 core sidecar（ezharness-core.exe）→ 健康就绪后开主窗口加载
core 伺服的页面；托盘常驻；关闭语义（托盘隐藏/确认退出）；外链与快应
用子窗口；共享浏览器（WebContentsView，browser 模块）。
core 是唯一业务后端——本进程只做壳与桌面资产，不做业务。
*/
const { app, BrowserWindow, Tray, Menu, ipcMain, shell, nativeImage } = require('electron')
const path = require('path')
const fs = require('fs')
const http = require('http')
const { spawn } = require('child_process')
const { startBrowserModule, overlayBrowser } = require('./browser')

let mainWindow = null
let tray = null
let coreProc = null
let quitting = false
let corePort = 5260
let findBrowserPopout = () => null // registerIpc 注入：当前浏览器页的独立窗口（无则 null）

/* 页面基地址：开发模式（EZHARNESS_DEV_URL=vite dev server）走热更页面
   （其 /api 代理到 core），生产模式直接用 core 伺服的内嵌页面 */
function pageBase() {
  return process.env.EZHARNESS_DEV_URL || `http://127.0.0.1:${corePort}`
}

/* core exe 所在目录：打包后在 resources/；开发模式在仓库根 bin/ */
function coreDir() {
  return app.isPackaged
    ? process.resourcesPath
    : path.join(path.resolve(__dirname, '../../..'), 'bin')
}

/* 配置目录：portable 绿色版自解压到临时目录运行，配置与数据须锚定到
exe 所在目录（PORTABLE_EXECUTABLE_DIR 由 portable 运行器注入） */
function configDir() {
  return process.env.PORTABLE_EXECUTABLE_DIR || coreDir()
}

/* readCoreConfig 读配置目录 ezharness.json（缺失按默认端口） */
function readCoreConfig() {
  try {
    return JSON.parse(fs.readFileSync(path.join(configDir(), 'ezharness.json'), 'utf8'))
  } catch {
    return {}
  }
}

/* startCore 拉起 core sidecar 并等健康就绪（30s 超时） */
function startCore() {
  const cfg = readCoreConfig()
  corePort = cfg.port || 5260
  const exe = process.env.EZHARNESS_CORE_EXE ||
    path.join(coreDir(), 'ezharness-core.exe')
  if (!fs.existsSync(exe)) {
    console.error(`core 不存在: ${exe}（先构建 core）`)
    app.quit()
    return Promise.reject(new Error('core missing'))
  }
  const args = []
  if (process.env.PORTABLE_EXECUTABLE_DIR) {
    args.push('--root', process.env.PORTABLE_EXECUTABLE_DIR)
  }
  /* windowsHide：让 core 拿到一个隐藏控制台（非 Windows 忽略）——它 spawn 的
     控制台程序（terminal 的 cmd.exe、taskkill、MCP server）继承该控制台，否则
     Windows 会给每个子进程新开一个可见控制台，黑框一闪。stdio 仍走 inherit，
     dev 下 core 日志照常出现在启动它的控制台里 */
  coreProc = spawn(exe, args, { cwd: configDir(), stdio: 'inherit', windowsHide: true })
  coreProc.on('exit', () => {
    if (!quitting) {
      console.error('core 进程退出，桌面壳随之退出')
      quitting = true
      app.quit()
    }
  })
  const started = Date.now()
  return new Promise((resolve, reject) => {
    const tick = () => {
      if (quitting) return reject(new Error('quitting'))
      const req = http.get({ host: '127.0.0.1', port: corePort, path: '/api/app/health', timeout: 1500 }, (res) => {
        res.resume()
        resolve()
      })
      req.on('error', () => {
        if (Date.now() - started > 30000) {
          console.error('core 健康检查超时（30s）')
          quitting = true
          app.quit()
          reject(new Error('health timeout'))
        } else {
          setTimeout(tick, 300)
        }
      })
    }
    tick()
  })
}

/* createMainWindow 主窗口：无边框（前端 TitleBar 自绘 + app-region 拖拽） */
function createMainWindow() {
  const cfg = readCoreConfig()
  mainWindow = new BrowserWindow({
    width: cfg.windowWidth || 1280,
    height: cfg.windowHeight || 800,
    minWidth: 840,
    frame: false,
    show: false,
    autoHideMenuBar: true,
    webPreferences: {
      preload: path.join(__dirname, '../preload/index.js'),
    },
  })
  mainWindow.loadURL(`${pageBase()}/?desktop=1`)
  mainWindow.once('ready-to-show', () => mainWindow.show())
  /* 系统级关闭（Alt+F4/任务栏 X）拦截：与标题栏 X 同一条询问流程 */
  mainWindow.on('close', (e) => {
    if (!quitting) {
      e.preventDefault()
      void handleCloseRequest()
    }
  })
}

/* coreSettings 读/写 core 的用户设置（关闭到托盘等） */
async function coreSettings() {
  try {
    const res = await fetch(`http://127.0.0.1:${corePort}/api/settings`)
    return await res.json()
  } catch {
    return {}
  }
}
async function saveCloseToTray(value) {
  try {
    await fetch(`http://127.0.0.1:${corePort}/api/settings`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ closeToTray: value }),
    })
  } catch {
    /* core 不可达时保持现状 */
  }
}

/* handleCloseRequest 点 X：已配置托盘则直接隐藏；否则让前端弹确认框 */
async function handleCloseRequest() {
  const settings = await coreSettings()
  if (settings.closeToTray === true) {
    mainWindow.hide()
    return { prompt: false }
  }
  /* 确认框是主窗内的页面 modal——主窗若被别的窗口（弹出窗/快应用/
     其他程序）压住，弹框就跟着"不在最上层"。弹之前先把主窗带前台；
     共享浏览器视图是 OS 层子视图（永远在页面 DOM 之上），一并临时
     藏起，否则浏览器内容会盖住确认框 */
  if (mainWindow && !mainWindow.isDestroyed()) {
    mainWindow.show()
    mainWindow.focus()
  }
  overlayBrowser(true)
  mainWindow.webContents.send('ez:close-prompt')
  return { prompt: true }
}

function quitApp() {
  quitting = true
  if (coreProc) coreProc.kill()
  app.quit()
}

/* createTray 托盘：左键切换主窗口显隐，右键菜单显示/退出 */
function createTray() {
  const iconPath = app.isPackaged
    ? path.join(process.resourcesPath, 'icon.png')
    : path.resolve(__dirname, '../../../assets/icon.png')
  tray = new Tray(nativeImage.createFromPath(iconPath))
  tray.setToolTip('ezharness')
  tray.setContextMenu(Menu.buildFromTemplate([
    { label: '显示主窗口', click: () => showMain() },
    { type: 'separator' },
    { label: '退出', click: () => quitApp() },
  ]))
  tray.on('click', () => {
    if (mainWindow.isVisible()) mainWindow.hide()
    else showMain()
  })
}

function showMain() {
  if (!mainWindow) return
  mainWindow.show()
  mainWindow.focus()
}

/* registerIpc 窗口控制与桌面能力 */
function registerIpc() {
  ipcMain.on('ez:minimize', () => mainWindow?.minimize())
  ipcMain.handle('ez:toggle-maximize', () => {
    if (!mainWindow) return false
    if (mainWindow.isMaximized()) mainWindow.unmaximize()
    else mainWindow.maximize()
    return mainWindow.isMaximized()
  })
  ipcMain.handle('ez:is-maximized', () => mainWindow?.isMaximized() ?? false)
  ipcMain.handle('ez:close-request', () => handleCloseRequest())
  ipcMain.on('ez:close-decision', (_e, tray, remember) => {
    overlayBrowser(false) /* 恢复被确认框临时藏起的浏览器视图 */
    if (tray) {
      mainWindow?.hide()
      if (remember) void saveCloseToTray(true)
    } else {
      quitApp()
    }
  })
  /* 取消关闭：同样恢复浏览器视图（关闭确认框把它临时藏起了） */
  ipcMain.on('ez:close-cancel', () => overlayBrowser(false))
  ipcMain.on('ez:open-url', (_e, url) => {
    if (typeof url === 'string' && /^https?:\/\//.test(url)) shell.openExternal(url)
  })
  /* 快应用子窗口：加载 core 同源页面，独立标题 */
  ipcMain.on('ez:open-app', (_e, url, title) => {
    if (typeof url !== 'string' || !url.startsWith('/')) return
    const win = new BrowserWindow({
      width: 1000,
      height: 700,
      title: title || 'ezharness',
      autoHideMenuBar: true,
    })
    win.loadURL(`${pageBase()}${url}`)
  })
  /* 抽屉工具弹出窗口：view → BrowserWindow。popoutState 是各工具最新
     的 pane 状态快照（file 弹出时由弹窗持续上报），关窗回流给主窗口。 */
  const popoutWindows = new Map()
  const popoutState = new Map()
  const POPOUT_VIEWS = ['term', 'file', 'browser']
  const POPOUT_TITLES = { term: 'ezharness · 终端', file: 'ezharness · 资源', browser: 'ezharness · 浏览器' }

  /* createPopoutWindow 建窗并加载单工具页面；x/y 给了就按落点摆。
     一律用普通窗口状态（不碰 opacity/focusable/alwaysOnTop/showInactive）：
     这些状态在 Windows 上会让窗口的合成面进入异常态，表现为内容区白屏——
     抽屉从不需要它们，所以抽屉从来没这个问题。 */
  function createPopoutWindow(view, x, y) {
    const at = Number.isFinite(x) && Number.isFinite(y) ? { x: Math.round(x), y: Math.round(y) } : {}
    const win = new BrowserWindow({
      width: 1100,
      height: 760,
      title: POPOUT_TITLES[view],
      autoHideMenuBar: true,
      webPreferences: {
        preload: path.join(__dirname, '../preload/index.js'),
      },
      ...at,
    })
    win.loadURL(`${pageBase()}/?desktop=1&popout=${view}`)
    return win
  }

  /* commitPopout 落定弹出窗口：入册 + 关窗回流抽屉（主窗口重开抽屉到
     该工具页并带回最新状态） */
  function commitPopout(view, win) {
    popoutWindows.set(view, win)
    win.on('closed', () => {
      popoutWindows.delete(view)
      const state = view === 'file' ? (popoutState.get(view) ?? null) : null
      popoutState.delete(view)
      if (mainWindow && !mainWindow.isDestroyed() && !quitting) {
        mainWindow.webContents.send('ez:popout-closed', view, state)
      }
    })
  }

  /* 拖拽脱离（tear-off）：拖拽期间不开任何窗口（渲染层只画一个跟随光标的
     幽灵卡片），松手在落点时**新建一个普通窗口**——窗口状态全程只有默认值，
     不存在"预览态 → 落定态"的切换。 */
  ipcMain.handle('ez:popout', (_e, view, stateJson, x, y) => {
    if (!POPOUT_VIEWS.includes(view)) return
    /* 已弹出：聚焦既有窗口（mini 图标/AI 拉开抽屉都走这里）。
       顺带请它重报一次 rect：视图归属只认上报，窗口若被重新显示过，
       重报即重新认领并按当前结构重绘（用户手点 mini 图标能恢复的正是这件事）。 */
    const existing = popoutWindows.get(view)
    if (existing && !existing.isDestroyed()) {
      existing.show()
      existing.focus()
      existing.webContents.send('ez-browser:refresh-rect')
      return
    }
    if (view === 'file' && typeof stateJson === 'string') popoutState.set(view, stateJson)
    commitPopout(view, createPopoutWindow(view, x, y))
  })
  ipcMain.handle('ez:popout-tools', () => [...popoutWindows.keys()])
  /* 浏览器页的独立窗口（浏览器视图全程归它，见 browser 模块 findOwner） */
  findBrowserPopout = () => {
    const w = popoutWindows.get('browser')
    return w && !w.isDestroyed() ? w : null
  }
  /* 弹出窗口启动时取初始状态；之后每次变化上报覆盖（关窗取最新回流） */
  ipcMain.handle('ez:popout-take-state', (_e, view) => popoutState.get(view) ?? null)
  ipcMain.on('ez:popout-state', (_e, view, json) => {
    if (typeof json === 'string') popoutState.set(view, json)
  })
  /* 主窗口 → 弹出窗口的外部定位转发（term:// / file:// 点击） */
  ipcMain.on('ez:popout-signal', (_e, view, name, value) => {
    const win = popoutWindows.get(view)
    if (!win || win.isDestroyed()) return
    win.show()
    win.focus()
    win.webContents.send('ez:popout-signal', name, value)
  })
}

app.whenReady().then(async () => {
  await startCore()
  createMainWindow()
  createTray()
  registerIpc()
  startBrowserModule({ corePort, getParentWindow: () => mainWindow, findBrowserOwner: () => findBrowserPopout() })
})

/* 托盘常驻：全部窗口关闭不退出（退出只走托盘菜单/关闭确认） */
app.on('window-all-closed', () => {})

app.on('before-quit', () => {
  quitting = true
  if (coreProc) coreProc.kill()
})
