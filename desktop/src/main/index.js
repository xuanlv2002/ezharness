/*
ezharness desktop：Electron 壳主进程。
职责：拉起 core sidecar（ezharness-core.exe）→ 健康就绪后开主窗口加载
core 伺服的页面；托盘常驻；关闭语义（托盘隐藏/确认退出）；外链与快应
用子窗口；共享浏览器（WebContentsView，browser 模块）。
core 是唯一业务后端——本进程只做壳与桌面资产，不做业务。
*/
const { app, BrowserWindow, Tray, Menu, ipcMain, shell, nativeImage, screen, dialog } = require('electron')
const path = require('path')
const fs = require('fs')
const http = require('http')
const crypto = require('crypto')
const { spawn } = require('child_process')
const { startBrowserModule, overlayBrowser } = require('./browser')
const { attachInspectMenu } = require('./inspect')

/* 应用名先行确定：mac/Linux 的用户数据目录由 app.getPath('userData') 推导，
   不设置时开发态会用 package.json 的 name（ezharness-desktop）。 */
app.setName('ezharness')

let mainWindow = null
let tray = null
let coreProc = null
let quitting = false
let coreReady = false
let corePort = 5260
/* 桌面宠物：透明置顶小窗，固定 288×288 一次到位（不动态扩缩）。默认
   整窗鼠标穿透（透明区点击归下层应用）。悬浮/穿透判定：主进程 60ms 轮询
   把光标换算成窗口内坐标推给渲染层（ez:pet-move），渲染层用
   elementFromPoint 判断光标是否压在实体小部件（头像/球/应用栏）上——
   压上才关穿透、并据此放展开动画；透明处一律保持穿透。不用"离中心多少
   距离"的圆判定：那样圈内整片透明白区都会被吃掉，垃圾桶这类就点不到。 */
let petWindow = null
let noticeWindow = null
const PET_SIZE = 288
/* 工作区 pane（浏览器 / 终端 / 资源）共用的最小内容尺寸。抽屉底宽、
   主窗口与弹出窗的最小尺寸都以它为基准——pane 被压成细条后页面会错位 */
const PANE_MIN_WIDTH = 640
const PANE_MIN_HEIGHT = 480
let petHoverTimer = null
let petDrag = null // { dx, dy }：拖拽中光标与窗口左上角的偏移（JS 拖拽替代 app-region）
/* API 令牌：每次启动随机生成，env 注入 core、URL query 带入页面。 */
const coreToken = crypto.randomBytes(18).toString('hex')
let findBrowserPopout = () => null // registerIpc 注入：当前浏览器页的独立窗口（无则 null）

/* 页面基地址：开发模式（EZHARNESS_DEV_URL=vite dev server）走热更页面
   （其 /api 代理到 core），生产模式直接用 core 伺服的内嵌页面 */
function pageBase() {
  return process.env.EZHARNESS_DEV_URL || `http://127.0.0.1:${corePort}`
}

/* 带令牌的页面地址。 */
function pageUrl(suffix) {
  return `${pageBase()}${suffix}${suffix.includes('?') ? '&' : '?'}token=${coreToken}`
}

/* failStart 打包环境没有终端可看：启动失败弹系统对话框，细节留在日志里。 */
function failStart(msg) {
  if (app.isPackaged) dialog.showErrorBox('ezharness 启动失败', msg)
}

/*
installAppMenu 固定应用菜单：macOS 需要标准编辑快捷键（Cmd+C/V/A），但
默认 View 菜单的 Cmd+＋/－ 会抢掉共享浏览器的缩放；Windows/Linux 无自定义
菜单需求，直接去掉（标题栏自绘）。只保留 app/编辑/窗口三组。
*/
function installAppMenu() {
  if (process.platform !== 'darwin') {
    Menu.setApplicationMenu(null)
    return
  }
  Menu.setApplicationMenu(
    Menu.buildFromTemplate([{ role: 'appMenu' }, { role: 'editMenu' }, { role: 'windowMenu' }]),
  )
}

/* coreFetch 主进程访问 core API，统一带头。 */
function coreFetch(pathname, opts = {}) {
  return fetch(`http://127.0.0.1:${corePort}${pathname}`, {
    ...opts,
    headers: { 'Content-Type': 'application/json', 'X-EZ-Token': coreToken, ...(opts.headers || {}) },
  })
}

/* killCore 结束 core 及其全部子进程（Windows 用 taskkill /T 树杀）；
   Unix 先 SIGTERM 让 core 走正常收尾（关终端 shell / MCP 子进程），
   2 秒未退再 SIGKILL。 */
function killCore() {
  if (!coreProc || coreProc.killed) return
  if (process.platform === 'win32') {
    spawn('taskkill', ['/PID', String(coreProc.pid), '/T', '/F'], { windowsHide: true })
    return
  }
  const pid = coreProc.pid
  coreProc.kill('SIGTERM')
  setTimeout(() => {
    try {
      process.kill(pid, 'SIGKILL')
    } catch {
      /* 已退出 */
    }
  }, 2000).unref()
}

/* guardWebContents 导航守卫：只许停留在 core origin，外部目标转系统浏览器。 */
function guardWebContents(wc) {
  const coreOrigin = () => new URL(pageBase()).origin
  wc.on('will-navigate', (e, url) => {
    let same = false
    try {
      same = new URL(url).origin === coreOrigin()
    } catch {
      /* 非法 URL 一律拦 */
    }
    if (same) return
    e.preventDefault()
    if (/^https?:\/\//.test(url)) shell.openExternal(url)
  })
  wc.setWindowOpenHandler(({ url }) => {
    let same = false
    try {
      same = new URL(url).origin === coreOrigin()
    } catch {
      /* 非法 URL 一律拒 */
    }
    if (same) return { action: 'allow' }
    if (/^https?:\/\//.test(url)) shell.openExternal(url)
    return { action: 'deny' }
  })
}

/* core exe 所在目录：打包后在 resources/；开发模式在 release/dev/ */
function coreDir() {
  return app.isPackaged
    ? process.resourcesPath
    : path.join(path.resolve(__dirname, '../../..'), 'release', 'dev')
}

/*
rootDir 是配置与数据根（ezharness.json + data/）：
- 开发态：release/dev（与 core 产物同目录，dev.bat 的既有习惯）
- Windows 打包：便携版锚定 exe 解压目录，安装版锚定安装目录的 resources/
- macOS / Linux 打包：.app 可能被 Gatekeeper 移位、AppImage 是只读挂载，
  写进应用目录不可靠，统一用系统用户数据目录（~/Library/Application Support/
  ezharness、~/.config/ezharness）。EZHARNESS_ROOT 可显式覆盖。
*/
function rootDir() {
  if (process.env.EZHARNESS_ROOT) return path.resolve(process.env.EZHARNESS_ROOT)
  if (!app.isPackaged) return coreDir()
  if (process.platform === 'win32') {
    const base = process.env.PORTABLE_EXECUTABLE_DIR || coreDir()
    // 装到 Program Files 等只读位置时回落到用户目录
    return writableDir(base) ? base : app.getPath('userData')
  }
  return app.getPath('userData')
}

/* writableDir 目录是否可写（只读安装位置/AppImage 挂载点会 false）。 */
function writableDir(dir) {
  try {
    fs.accessSync(dir, fs.constants.W_OK)
    return true
  } catch {
    return false
  }
}

/*
loginPath 解析登录 shell 的 PATH：macOS 从 Finder/Dock 启动时只有 launchd
的最小 PATH，go/node/python 等工具找不到。开发态继承终端环境，不用探测。
*/
function loginPath() {
  if (process.platform !== 'darwin' || !app.isPackaged) return process.env.PATH
  try {
    const { execFileSync } = require('child_process')
    const sh = process.env.SHELL || '/bin/zsh'
    /* fish 的 $PATH 是列表，需显式用冒号连接 */
    const expr = path.basename(sh) === 'fish' ? 'string join : $PATH' : 'printf %s "$PATH"'
    const out = execFileSync(sh, ['-ilc', expr], {
      encoding: 'utf8',
      timeout: 5000,
      stdio: ['ignore', 'pipe', 'ignore'],
    })
    const lines = out.trim().split(/\r?\n/).filter(Boolean)
    return lines.length ? lines[lines.length - 1] : process.env.PATH
  } catch {
    return process.env.PATH
  }
}

/* readCoreConfig 读配置目录 ezharness.json（缺失按默认端口） */
function readCoreConfig() {
  try {
    return JSON.parse(fs.readFileSync(path.join(rootDir(), 'ezharness.json'), 'utf8'))
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
  const root = rootDir()
  try {
    fs.mkdirSync(root, { recursive: true })
  } catch (err) {
    console.error(`数据目录不可用: ${root}（${err.message}）`)
    app.quit()
    return Promise.reject(new Error('root dir unavailable'))
  }
  /* 打包产物经压缩/解压后可能丢可执行位（mac/Linux） */
  if (process.platform !== 'win32') {
    try {
      fs.chmodSync(exe, 0o755)
    } catch {
      /* 只读挂载或权限不足：交给 spawn 报错 */
    }
  }
  /* windowsHide：让 core 拿到一个隐藏控制台（非 Windows 忽略）——它 spawn 的
     控制台程序（terminal 的 cmd.exe、taskkill、MCP server）继承该控制台，否则
     Windows 会给每个子进程新开一个可见控制台，黑框一闪。stdio 仍走 inherit，
     dev 下 core 日志照常出现在启动它的控制台里 */
  coreProc = spawn(exe, ['--root', root], {
    cwd: root, stdio: 'inherit', windowsHide: true,
    env: { ...process.env, PATH: loginPath(), EZHARNESS_TOKEN: coreToken },
  })
  coreProc.on('error', (err) => {
    console.error('core 启动失败:', err.message)
    failStart(`无法启动 core：${err.message}`)
    quitting = true
    app.quit()
  })
  coreProc.on('exit', (code) => {
    if (!quitting) {
      console.error('core 进程退出，桌面壳随之退出')
      if (!coreReady) {
        failStart(`core 启动失败（退出码 ${code}）。\n请确认数据目录可写：${root}\n日志：${path.join(root, 'data', 'logs')}`)
      }
      quitting = true
      app.quit()
    }
  })
  const started = Date.now()
  return new Promise((resolve, reject) => {
    const tick = () => {
      if (quitting) return reject(new Error('quitting'))
      const req = http.get({
        host: '127.0.0.1', port: corePort, path: '/api/app/health', timeout: 1500,
        headers: { 'X-EZ-Token': coreToken },
      }, (res) => {
        res.resume()
        coreReady = true
        resolve()
      })
      req.on('error', () => {
        if (Date.now() - started > 30000) {
          console.error('core 健康检查超时（30s）')
          failStart(`core 30 秒内未就绪。\n请确认端口 ${corePort} 未被占用、数据目录可写：${root}\n日志：${path.join(root, 'data', 'logs')}`)
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
    /* 侧栏 64 + 聊天主列 480 + 抽屉 pane 底宽；高度 = 标题栏 + pane 头部 + 内容 */
    minWidth: 64 + 480 + PANE_MIN_WIDTH,
    minHeight: PANE_MIN_HEIGHT + 120,
    frame: false,
    /* 不可全屏：共享浏览器标签里的页面 requestFullscreen（双击视频/图片常见）
       会把承载它的窗口整个拉成全屏——主窗本就无边框、独立窗全屏后框也没了，
       用户拿不到关闭入口。禁掉窗口全屏后页面改在视图框内铺满，不接管整屏。 */
    fullscreenable: false,
    show: false,
    autoHideMenuBar: true,
    webPreferences: {
      preload: path.join(__dirname, '../preload/index.js'),
    },
  })
  mainWindow.loadURL(pageUrl('/?desktop=1'))
  guardWebContents(mainWindow.webContents)
  attachInspectMenu(mainWindow.webContents)
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
    const res = await coreFetch('/api/settings')
    return await res.json()
  } catch {
    return {}
  }
}
async function saveCloseToTray(value) {
  try {
    await coreFetch('/api/settings', { method: 'POST', body: JSON.stringify({ closeToTray: value }) })
  } catch {
    /* core 不可达时保持现状 */
  }
}
async function savePetEnabled(value) {
  try {
    await coreFetch('/api/settings', { method: 'POST', body: JSON.stringify({ petEnabled: value }) })
  } catch {
    /* core 不可达时保持现状 */
  }
}

/* 关闭悬浮球：hide 不销毁（保留用户拖放的位置，重开即原位）；持久化 false，
   重启后不再建窗 */
function hidePetBall() {
  if (petWindow && !petWindow.isDestroyed()) petWindow.hide()
  void savePetEnabled(false)
}

/* createPetWindow 桌面宠物：透明置顶 288 固定小窗，头像中心落在鼠标
   所在屏右下（距边 72）。默认整窗穿透，悬浮判定与穿透切换见 ez:pet-hover。 */
function createPetWindow() {
  const area = screen.getDisplayNearestPoint(screen.getCursorScreenPoint()).workArea
  const win = new BrowserWindow({
    width: PET_SIZE,
    height: PET_SIZE,
    x: area.x + area.width - PET_SIZE / 2 - 72,
    y: area.y + area.height - PET_SIZE / 2 - 72,
    frame: false,
    transparent: true,
    resizable: false,
    skipTaskbar: true,
    alwaysOnTop: true,
    hasShadow: false,
    show: false,
    autoHideMenuBar: true,
    webPreferences: {
      preload: path.join(__dirname, '../preload/index.js'),
    },
  })
  win.loadURL(pageUrl('/?desktop=1&pet=1'))
  guardWebContents(win.webContents)
  /* 右键只弹「关闭悬浮球」（头像层放行 contextmenu，见 AssistantPet） */
  win.webContents.on('context-menu', () => {
    Menu.buildFromTemplate([{ label: '关闭悬浮球', click: () => hidePetBall() }]).popup()
  })
  win.once('ready-to-show', () => win.show())
  win.on('closed', () => {
    petWindow = null
  })
  return win
}

/* createNoticeWindow 独立通知窗（铃铛球打开）：复用客户端通知面板，普通
   有边框窗口——不用 alwaysOnTop/opacity/focusable（那些在 Windows 上会让
   合成面白屏）。单例，已开则聚焦；位置摆在宠物窗左侧并夹进工作区。 */
function createNoticeWindow() {
  if (noticeWindow && !noticeWindow.isDestroyed()) {
    noticeWindow.show()
    noticeWindow.focus()
    return
  }
  const W = 360
  const H = 540
  let at = {}
  if (petWindow && !petWindow.isDestroyed()) {
    const p = petWindow.getBounds()
    const area = screen.getDisplayNearestPoint({ x: p.x, y: p.y }).workArea
    const x = Math.min(Math.max(p.x - W + 48, area.x), area.x + area.width - W)
    const y = Math.min(Math.max(p.y - H + 120, area.y), area.y + area.height - H)
    at = { x: Math.round(x), y: Math.round(y) }
  }
  const win = new BrowserWindow({
    width: W,
    height: H,
    title: 'ezharness · 通知',
    autoHideMenuBar: true,
    webPreferences: {
      preload: path.join(__dirname, '../preload/index.js'),
    },
    ...at,
  })
  win.loadURL(pageUrl('/?desktop=1&notice=1'))
  guardWebContents(win.webContents)
  win.on('closed', () => {
    noticeWindow = null
  })
  noticeWindow = win
}

/* handleCloseRequest 点 X：已配置托盘则直接隐藏；否则让前端弹确认框 */
async function handleCloseRequest() {
  const settings = await coreSettings()
  if (settings.closeToTray === true) {
    hideMainToTray()
    return { prompt: false }
  }
  /* 确认框是主窗内的页面 modal——主窗若被别的窗口（弹出窗/快应用/
     其他程序）压住，弹框就跟着"不在最上层"。弹之前先把主窗带前台；
     共享浏览器视图是 OS 层子视图（永远在页面 DOM 之上），一并临时
     藏起，否则浏览器内容会盖住确认框 */
  if (mainWindow && !mainWindow.isDestroyed()) showMain()
  overlayBrowser(true)
  mainWindow.webContents.send('ez:close-prompt')
  return { prompt: true }
}

function quitApp() {
  quitting = true
  killCore()
  app.quit()
}

/* createTray 托盘：左键切换主窗口显隐，右键菜单显示/退出 */
function createTray() {
  const iconPath = app.isPackaged
    ? path.join(process.resourcesPath, 'icon.png')
    : path.resolve(__dirname, '../../../assets/icon.png')
  const icon = nativeImage.createFromPath(iconPath)
  /* macOS 菜单栏图标按 18pt 绘制，用原图（512）会撑满整条菜单栏 */
  tray = new Tray(process.platform === 'darwin' ? icon.resize({ width: 18, height: 18 }) : icon)
  tray.setToolTip('ezharness')
  tray.setContextMenu(Menu.buildFromTemplate([
    { label: '显示主窗口', click: () => showMain() },
    { type: 'separator' },
    { label: '退出', click: () => quitApp() },
  ]))
  tray.on('click', () => {
    if (mainWindow.isVisible()) hideMainToTray()
    else showMain()
  })
}

/* hideMainToTray 最小化到托盘：隐藏主窗；macOS 同时收起 Dock 图标
   （只留菜单栏托盘）。窗口仅最小化（黄色按钮）时 Dock 图标照常保留 */
function hideMainToTray() {
  if (!mainWindow || mainWindow.isDestroyed()) return
  mainWindow.hide()
  if (process.platform === 'darwin') app.dock?.hide()
}

function showMain() {
  if (!mainWindow) return
  if (process.platform === 'darwin') void app.dock?.show()
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
      hideMainToTray()
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
  /* 在系统文件管理器中定位本机文件（资源查看器的当前 tab） */
  ipcMain.on('ez:reveal', (_e, p) => {
    if (typeof p === 'string' && p) shell.showItemInFolder(p)
  })
  /* 打开文件夹：是目录就在资源管理器直接打开,返回是否已处理 */
  ipcMain.handle('ez:open-dir', async (_e, p) => {
    if (typeof p !== 'string' || !p) return false
    try {
      const st = await fs.promises.stat(p)
      if (!st.isDirectory()) return false
      await shell.openPath(p)
      return true
    } catch {
      return false
    }
  })
  /* 快应用子窗口：加载 core 同源页面，独立标题；右键可检查元素。
     URL 自带受限令牌（主渲染层拼好），不能再补主令牌。 */
  ipcMain.on('ez:open-app', (_e, url, title) => {
    if (typeof url !== 'string' || !url.startsWith('/')) return
    const win = new BrowserWindow({
      width: 1000,
      height: 700,
      title: title || 'ezharness',
      autoHideMenuBar: true,
    })
    attachInspectMenu(win.webContents)
    win.loadURL(pageBase() + url)
    guardWebContents(win.webContents)
  })
  /* 桌面宠物：点小按钮 → 显示主窗口（拖动走渲染层 app-region，无 IPC） */
  ipcMain.on('ez:pet-open-main', () => showMain())
  /* 设置页保存成功后通知：只切显隐，不落盘（设置页已直连 core 持久化，
     双写会有竞态）。开=show（窗不在则建），关=hide（保位，不销毁） */
  ipcMain.on('ez:pet-set-enabled', (_e, on) => {
    if (on) {
      if (!petWindow || petWindow.isDestroyed()) petWindow = createPetWindow()
      else petWindow.show()
    } else if (petWindow && !petWindow.isDestroyed()) {
      petWindow.hide()
    }
  })
  /* 通知：铃铛球开独立通知窗；通知窗点条目 → 亮主窗 + 把跳转载荷转给主窗渲染层 */
  ipcMain.on('ez:notice-open', () => createNoticeWindow())
  ipcMain.on('ez:notice-jump', (_e, payload) => {
    showMain()
    mainWindow?.webContents.send('ez:notice-jump', payload)
  })
  /* 宠物光标轮询：只把光标换算成窗口内坐标推给渲染层（穿透时渲染层收不到
     DOM 鼠标事件，只能主进程喂坐标）。悬浮/穿透由渲染层 elementFromPoint
     判定后回推 ez:pet-passthrough。 */
  if (!petHoverTimer) {
    petHoverTimer = setInterval(() => {
      const win = petWindow
      if (!win || win.isDestroyed()) return
      /* 隐藏期（悬浮球被关闭）停推坐标，show 后自动恢复 */
      if (!win.isVisible()) return
      const pt = screen.getCursorScreenPoint()
      // 拖拽中：移窗改由 ez:pet-dragmove 事件驱动（指针频率，平滑）；
      // 也不再推 pet-move（穿透已关，渲染层收得到真实鼠标事件，省 IPC）
      if (petDrag) return
      const b = win.getBounds()
      win.webContents.send('ez:pet-move', pt.x - b.x, pt.y - b.y)
    }, 60)
  }
  /* 宠物拖拽移动：渲染层 pointermove 上报屏幕坐标（指针采样频率），
     主进程即时 setPosition——比轮询移窗平滑得多 */
  ipcMain.on('ez:pet-dragmove', (_e, x, y) => {
    const win = petWindow
    if (!win || win.isDestroyed() || !petDrag) return
    win.setPosition(Math.round(x - petDrag.dx), Math.round(y - petDrag.dy))
  })
  /* 宠物 JS 拖拽：按下记光标与窗口左上角偏移
     （不用 app-region:drag——那会把右键变成系统窗口菜单） */
  ipcMain.on('ez:pet-drag', (_e, on) => {
    const win = petWindow
    if (!win || win.isDestroyed()) return
    if (!on) {
      petDrag = null
      return
    }
    const b = win.getBounds()
    const pt = screen.getCursorScreenPoint()
    petDrag = { dx: pt.x - b.x, dy: pt.y - b.y }
  })
  /* 宠物鼠标穿透：默认整窗穿透（透明区点击归下层应用，forward 保留
     mousemove 转发），光标悬浮进圈后渲染层关穿透换实体可交互 */
  ipcMain.on('ez:pet-passthrough', (_e, on) => {
    const win = petWindow
    if (win && !win.isDestroyed()) win.setIgnoreMouseEvents(!!on, { forward: true })
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
      /* 最小窗口 = pane 最小内容 + 系统边框/标题栏余量 */
      minWidth: PANE_MIN_WIDTH + 40,
      minHeight: PANE_MIN_HEIGHT + 80,
      title: POPOUT_TITLES[view],
      autoHideMenuBar: true,
      fullscreenable: false, // 弹出窗里的浏览器页全屏只铺满视图框，不拉整窗（同主窗）
      webPreferences: {
        preload: path.join(__dirname, '../preload/index.js'),
      },
      ...at,
    })
    win.loadURL(pageUrl(`/?desktop=1&popout=${view}`))
    guardWebContents(win.webContents)
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
  installAppMenu()
  await startCore()
  createMainWindow()
  createTray()
  registerIpc()
  startBrowserModule({ corePort, token: coreToken, getParentWindow: () => mainWindow, findBrowserOwner: () => findBrowserPopout() })
  /* 悬浮球默认开：读 petEnabled !== false（coreSettings 失败返回 {} 时也开） */
  const settings = await coreSettings()
  if (settings.petEnabled !== false) petWindow = createPetWindow()
}).catch((err) => {
  console.error('启动失败:', err)
  failStart(String(err?.message || err))
})

/* 托盘常驻：全部窗口关闭不退出（退出只走托盘菜单/关闭确认） */
app.on('window-all-closed', () => {})

/* macOS 点 Dock 图标恢复主窗（窗口是 hide 不是销毁，showMain 即可） */
app.on('activate', () => {
  if (mainWindow && !mainWindow.isDestroyed()) showMain()
})

app.on('before-quit', () => {
  quitting = true
  killCore()
})
