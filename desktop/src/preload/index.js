/*
preload：向渲染层暴露 desktop 能力（contextBridge，最小面）。
浏览器（web 端）直接访问 core 时没有本脚本——渲染层据此降级。
*/
const { contextBridge, ipcRenderer } = require('electron')

contextBridge.exposeInMainWorld('ez', {
  desktop: true,
  /* 渲染层按平台切换窗口按钮形态（macOS 用原生红绿灯，不再自绘，见 TitleBar） */
  platform: process.platform,
  window: {
    minimize: () => ipcRenderer.send('ez:minimize'),
    toggleMaximize: () => ipcRenderer.invoke('ez:toggle-maximize'),
    isMaximized: () => ipcRenderer.invoke('ez:is-maximized'),
    /* 点 X：返回 {prompt:true} 表示需用户确认（前端弹关闭框），否则已直接隐藏到托盘 */
    closeRequest: () => ipcRenderer.invoke('ez:close-request'),
    /* 关闭框决策：tray=true 隐藏到托盘（remember 记住选择），false 整体退出 */
    closeDecision: (tray, remember) => ipcRenderer.send('ez:close-decision', tray, remember),
    /* 取消关闭确认：恢复被确认框临时藏起的共享浏览器视图 */
    closeCanceled: () => ipcRenderer.send('ez:close-cancel'),
    openUrl: (url) => ipcRenderer.send('ez:open-url', url),
    openApp: (url, title) => ipcRenderer.send('ez:open-app', url, title),
    /* 在系统文件管理器中定位本机文件（资源查看器的当前 tab） */
    reveal: (p) => ipcRenderer.send('ez:reveal', p),
    /* 原生目录选择对话框（资源页目录树），取消返回 null */
    pickDir: () => ipcRenderer.invoke('ez:pick-dir'),
    /* 路径是文件夹时在资源管理器打开它,返回是否已处理 */
    openDir: (p) => ipcRenderer.invoke('ez:open-dir', p),
    /* 系统级关闭（Alt+F4/任务栏）由主进程转到前端弹框 */
    onClosePrompt: (cb) => ipcRenderer.on('ez:close-prompt', () => cb()),
  },
  /* 抽屉工具弹出窗口（view: term/file/browser） */
  popout: {
    /* 弹出（已弹出则聚焦）；state 是资源页初始状态快照；
       x/y 是屏幕坐标落点（拖拽脱离用，不给就默认居中） */
    open: (view, state, x, y) => ipcRenderer.invoke('ez:popout', view, state, x, y),
    /* 当前已弹出的工具列表（主窗口启动时同步） */
    tools: () => ipcRenderer.invoke('ez:popout-tools'),
    /* 弹出窗口取初始状态 / 持续上报最新状态（关窗回流快照） */
    take: (view) => ipcRenderer.invoke('ez:popout-take-state', view),
    push: (view, json) => ipcRenderer.send('ez:popout-state', view, json),
    /* 主窗口 → 弹出窗口的外部定位转发 */
    signal: (view, name, value) => ipcRenderer.send('ez:popout-signal', view, name, value),
    /* 关窗回流（主窗口收）：view + 最新状态快照 */
    onClosed: (cb) => ipcRenderer.on('ez:popout-closed', (_e, view, state) => cb(view, state)),
    onSignal: (cb) => ipcRenderer.on('ez:popout-signal', (_e, name, value) => cb(name, value)),
  },
  /* 桌面宠物：打开主窗口；主进程 60ms 推光标窗口内坐标（穿透时渲染层
     收不到 DOM 鼠标事件），渲染层据此判定悬浮并回推是否穿透 */
  pet: {
    openMain: () => ipcRenderer.send('ez:pet-open-main'),
    onMove: (cb) => ipcRenderer.on('ez:pet-move', (_e, x, y) => cb(x, y)),
    passthrough: (on) => ipcRenderer.send('ez:pet-passthrough', !!on),
    drag: (on) => ipcRenderer.send('ez:pet-drag', !!on),
    dragMove: (x, y) => ipcRenderer.send('ez:pet-dragmove', x, y),
    /* 设置页保存成功后通知主进程切显隐（持久化由设置页直连 core 完成） */
    setEnabled: (on) => ipcRenderer.send('ez:pet-set-enabled', !!on),
  },
  /* 通知：铃铛球开独立通知窗（open）；通知窗点条目 → 主进程亮主窗并把
     跳转载荷（{rootId,forkId,id,target}）转给主窗渲染层（jump / onJump） */
  notice: {
    open: () => ipcRenderer.send('ez:notice-open'),
    jump: (payload) => ipcRenderer.send('ez:notice-jump', payload),
    onJump: (cb) => ipcRenderer.on('ez:notice-jump', (_e, payload) => cb(payload)),
  },
  browser: {
    /* 标签清单（JSON 字符串）订阅与拉取 */
    onTabs: (cb) => ipcRenderer.on('ez-browser:tabs', (_e, payload) => cb(payload)),
    list: () => ipcRenderer.invoke('ez-browser:list'),
    create: (url) => ipcRenderer.send('ez-browser:create', url),
    navigate: (tabId, url) => ipcRenderer.send('ez-browser:navigate', tabId, url),
    select: (tabId) => ipcRenderer.send('ez-browser:select', tabId),
    close: (tabId) => ipcRenderer.send('ez-browser:close', tabId),
    /* 主窗口抽屉页（BrowserPane）：内容区 rect 上报（WebContentsView 贴靠） */
    reportRect: (rect) => ipcRenderer.send('ez-browser:content-rect', rect),
    /* 视图被摘出窗口（弹窗关闭/拖拽取消）后请抽屉页重新上报 rect 回挂 */
    onRefreshRect: (cb) => ipcRenderer.on('ez-browser:refresh-rect', () => cb()),
    /* browser:// chip 定位标签（前端同时展开抽屉页） */
    focusTab: (tabId) => ipcRenderer.send('ez-browser:focus-tab', tabId),
  },
})
