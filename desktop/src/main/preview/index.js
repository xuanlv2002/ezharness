/* 资源页 HTML 内嵌预览：每窗口一个 WebContentsView，固定归当前窗口，隐藏时保留页面状态 */
const path = require('path')
const { BrowserWindow, WebContentsView, ipcMain, session, shell } = require('electron')
const { attachInspectMenu } = require('../inspect')

/** @type {Map<import('electron').BrowserWindow, {view: WebContentsView, url: string, hidden: boolean}>} */
const previews = new Map()

function sameUrl(a, b) {
  try {
    const x = new URL(a)
    const y = new URL(b)
    x.hash = ''
    y.hash = ''
    return x.href === y.href
  } catch {
    return a === b
  }
}

function getPreview(win) {
  const old = previews.get(win)
  if (old) return old
  const view = new WebContentsView({
    webPreferences: {
      session: session.fromPartition('persist:ezbrowser'),
      preload: path.join(__dirname, '../../preload/browser.js'),
    },
  })
  const wc = view.webContents
  const entry = { view, url: '', hidden: false }
  wc.setWindowOpenHandler(({ url }) => {
    if (/^https?:\/\//i.test(url)) shell.openExternal(url)
    else if (/^file:/i.test(url)) setImmediate(() => wc.loadURL(url).catch(() => {}))
    return { action: 'deny' }
  })
  wc.on('will-prevent-unload', (e) => e.preventDefault())
  /* 预览页里 Ctrl+S 交给页面编辑器保存，保存后主进程会 reload 本视图 */
  wc.on('before-input-event', (event, input) => {
    if (input.type !== 'keyDown' || input.alt) return
    if (!(input.control || input.meta) || input.key.toLowerCase() !== 's') return
    event.preventDefault()
    if (!win.isDestroyed()) win.webContents.send('ez-preview:save')
  })
  wc.on('did-navigate', (_e, url) => (entry.url = url))
  wc.on('did-navigate-in-page', (_e, url) => (entry.url = url))
  attachInspectMenu(wc)
  previews.set(win, entry)
  win.on('closed', () => {
    previews.delete(win)
    if (!wc.isDestroyed()) wc.close()
  })
  return entry
}

function hidePreview(win) {
  const p = previews.get(win)
  if (!p || !win || win.isDestroyed()) return
  if (win.contentView.children.includes(p.view)) win.contentView.removeChildView(p.view)
}

function showPreview(win, rect) {
  if (!win || win.isDestroyed() || !rect || rect.width < 10 || rect.height < 10) {
    if (win) hidePreview(win)
    return
  }
  const p = getPreview(win)
  if (!win.contentView.children.includes(p.view)) win.contentView.addChildView(p.view)
  p.view.setVisible(!p.hidden)
  p.view.setBounds({
    x: Math.round(rect.x),
    y: Math.round(rect.y),
    width: Math.round(rect.width),
    height: Math.round(rect.height),
  })
}

/* 关闭确认框是页面 DOM 弹层，原生视图永远盖在 DOM 上——弹框期间先藏预览 */
function overlayPreview(hide) {
  for (const p of previews.values()) {
    if (p.view.webContents.isDestroyed()) continue
    p.hidden = hide
    p.view.setVisible(!hide)
  }
}

function startPreviewModule() {
  ipcMain.on('ez-preview:load', (e, url) => {
    const win = BrowserWindow.fromWebContents(e.sender)
    if (!win || typeof url !== 'string' || !/^(file:|https?:)/i.test(url)) return
    getPreview(win).view.webContents.loadURL(url).catch(() => {})
  })

  ipcMain.on('ez-preview:show', (e, rect) => {
    showPreview(BrowserWindow.fromWebContents(e.sender), rect)
  })

  ipcMain.on('ez-preview:hide', (e) => {
    hidePreview(BrowserWindow.fromWebContents(e.sender))
  })

  ipcMain.on('ez-preview:reload', (e, url) => {
    const win = BrowserWindow.fromWebContents(e.sender)
    const p = win ? previews.get(win) : null
    if (!p || p.view.webContents.isDestroyed()) return
    if (!url || sameUrl(p.url, url)) p.view.webContents.reload()
  })
}

module.exports = { startPreviewModule, overlayPreview }
