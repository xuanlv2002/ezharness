/*
preload：向渲染层暴露 desktop 能力（contextBridge，最小面）。
浏览器（web 端）直接访问 core 时没有本脚本——渲染层据此降级。
*/
const { contextBridge, ipcRenderer } = require('electron')

contextBridge.exposeInMainWorld('ez', {
  desktop: true,
  window: {
    minimize: () => ipcRenderer.send('ez:minimize'),
    toggleMaximize: () => ipcRenderer.invoke('ez:toggle-maximize'),
    isMaximized: () => ipcRenderer.invoke('ez:is-maximized'),
    /* 点 X：返回 {prompt:true} 表示需用户确认（前端弹关闭框），否则已直接隐藏到托盘 */
    closeRequest: () => ipcRenderer.invoke('ez:close-request'),
    /* 关闭框决策：tray=true 隐藏到托盘（remember 记住选择），false 整体退出 */
    closeDecision: (tray, remember) => ipcRenderer.send('ez:close-decision', tray, remember),
    openUrl: (url) => ipcRenderer.send('ez:open-url', url),
    openApp: (url, title) => ipcRenderer.send('ez:open-app', url, title),
    /* 系统级关闭（Alt+F4/任务栏）由主进程转到前端弹框 */
    onClosePrompt: (cb) => ipcRenderer.on('ez:close-prompt', () => cb()),
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
    /* browser:// chip 定位标签（前端同时展开抽屉页） */
    focusTab: (tabId) => ipcRenderer.send('ez-browser:focus-tab', tabId),
  },
})
