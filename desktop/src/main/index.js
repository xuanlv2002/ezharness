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
const { startBrowserModule, detachViewsFromWindow } = require('./browser')

let mainWindow = null
let tray = null
let coreProc = null
let quitting = false
let corePort = 5260
let tearWindow = null // 拖拽脱离中的预览窗口（未落定，不在 popoutWindows）

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
  coreProc = spawn(exe, args, { cwd: configDir(), stdio: 'inherit' })
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
    if (tray) {
      mainWindow?.hide()
      if (remember) void saveCloseToTray(true)
    } else {
      quitApp()
    }
  })
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

  /* createPopoutWindow 建窗并加载单工具页面（extra 给拖拽预览加临时选项） */
  function createPopoutWindow(view, extra = {}) {
    const win = new BrowserWindow({
      width: 1100,
      height: 760,
      title: POPOUT_TITLES[view],
      autoHideMenuBar: true,
      webPreferences: {
        preload: path.join(__dirname, '../preload/index.js'),
      },
      ...extra,
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

  /* 拖拽脱离（tear-off）：按住抽屉面板头部拖出 → 预览窗口跟着光标走。
     松手窗外落位（入册，关窗回流抽屉）；松手抽屉内销毁预览（不入册、
     不发回流），销毁前先摘浏览器视图（视图是窗口子视图，随窗口销毁会
     连标签一起丢）。预览期窗口不聚焦、不抢鼠标捕获。 */
  let tearView = ''
  let tearOffset = { x: 0, y: 0 }
  let tearLast = { x: 0, y: 0 }

  function moveTear(x, y) {
    tearLast = { x, y }
    if (tearWindow && !tearWindow.isDestroyed()) {
      tearWindow.setPosition(Math.round(x - tearOffset.x), Math.round(y - tearOffset.y))
    }
  }

  function finishTear(commit) {
    const win = tearWindow
    const view = tearView
    tearWindow = null
    tearView = ''
    if (!win || win.isDestroyed()) {
      if (!view) return
      if (!commit) {
        popoutState.delete(view)
        return
      }
      /* 极快拖拽：松手时预览窗口还没建起来，按最后位置补一个 */
      const late = createPopoutWindow(view)
      late.setPosition(Math.round(tearLast.x - tearOffset.x), Math.round(tearLast.y - tearOffset.y))
      commitPopout(view, late)
      return
    }
    if (!commit) {
      detachViewsFromWindow(win)
      popoutState.delete(view)
      win.destroy()
      return
    }
    win.setOpacity(1)
    win.setFocusable(true)
    win.setAlwaysOnTop(false)
    win.setSkipTaskbar(false)
    win.focus()
    commitPopout(view, win)
  }

  ipcMain.on('ez:popout-tear-begin', (_e, view, stateJson, x, y, offsetX, offsetY) => {
    if (!POPOUT_VIEWS.includes(view) || popoutWindows.has(view) || tearWindow) return
    /* 资源页状态快照要在建窗前写入（弹窗启动时 take） */
    if (view === 'file' && typeof stateJson === 'string') popoutState.set(view, stateJson)
    tearView = view
    tearOffset = { x: offsetX, y: offsetY }
    const win = createPopoutWindow(view, {
      show: false,
      opacity: 0.9,
      focusable: false,
      alwaysOnTop: true,
      skipTaskbar: true,
    })
    tearWindow = win
    moveTear(x, y)
    win.showInactive()
    /* 预览窗口意外销毁时清引用（落定/取消路径已先行清空，不会误伤） */
    win.on('closed', () => {
      if (tearWindow === win) {
        tearWindow = null
        tearView = ''
      }
    })
  })
  ipcMain.on('ez:popout-tear-move', (_e, x, y) => moveTear(x, y))
  ipcMain.on('ez:popout-tear-end', (_e, commit) => finishTear(!!commit))

  ipcMain.handle('ez:popout', (_e, view, stateJson) => {
    if (!POPOUT_VIEWS.includes(view)) return
    /* 该工具正被拖拽脱离：直接落定预览窗口，不再另开一个 */
    if (tearWindow && tearView === view) {
      finishTear(true)
      return
    }
    /* 已弹出：聚焦既有窗口（mini 图标/AI 拉开抽屉都走这里） */
    const existing = popoutWindows.get(view)
    if (existing && !existing.isDestroyed()) {
      existing.show()
      existing.focus()
      return
    }
    if (view === 'file' && typeof stateJson === 'string') popoutState.set(view, stateJson)
    commitPopout(view, createPopoutWindow(view))
  })
  ipcMain.handle('ez:popout-tools', () => [...popoutWindows.keys()])
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
  startBrowserModule({ corePort, getParentWindow: () => mainWindow })
})

/* 托盘常驻：全部窗口关闭不退出（退出只走托盘菜单/关闭确认） */
app.on('window-all-closed', () => {})

app.on('before-quit', () => {
  quitting = true
  if (tearWindow && !tearWindow.isDestroyed()) {
    detachViewsFromWindow(tearWindow)
    tearWindow.destroy()
    tearWindow = null
  }
  if (coreProc) coreProc.kill()
})
