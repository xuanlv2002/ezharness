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
const { startBrowserModule } = require('./browser')

let mainWindow = null
let tray = null
let coreProc = null
let quitting = false
let corePort = 5260

/* 页面基地址：开发模式（EZHARNESS_DEV_URL=vite dev server）走热更页面
   （其 /api 代理到 core），生产模式直接用 core 伺服的内嵌页面 */
function pageBase() {
  return process.env.EZHARNESS_DEV_URL || `http://127.0.0.1:${corePort}`
}

/* core 根目录：打包后 core exe 与配置在 resources/；开发模式为仓库根 */
function coreDir() {
  return app.isPackaged ? process.resourcesPath : path.resolve(__dirname, '../../..')
}

/* readCoreConfig 读 core 同目录 ezharness.json（缺失按默认端口） */
function readCoreConfig() {
  try {
    return JSON.parse(fs.readFileSync(path.join(coreDir(), 'ezharness.json'), 'utf8'))
  } catch {
    return {}
  }
}

/* startCore 拉起 core sidecar 并等健康就绪（30s 超时） */
function startCore() {
  const cfg = readCoreConfig()
  corePort = cfg.port || 5260
  const exe = process.env.EZHARNESS_CORE_EXE ||
    path.join(coreDir(), 'ezharness-core.exe') // dev 也构建到仓库根：exe 目录=配置与数据目录
  if (!fs.existsSync(exe)) {
    console.error(`core 不存在: ${exe}（先构建 core）`)
    app.quit()
    return Promise.reject(new Error('core missing'))
  }
  coreProc = spawn(exe, [], { cwd: coreDir(), stdio: 'inherit' })
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
  if (coreProc) coreProc.kill()
})
