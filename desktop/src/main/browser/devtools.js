/*
浏览器开发者数据采集（CDP）：控制台/异常/浏览器日志 + 网络请求。
走 Chrome DevTools Protocol —— 和 F12 同一套信息源。

建标签即 attach 并 enable 全部采集域（Page + Runtime/Log/Network）：首次加载的
请求与控制台就在缓冲里，wait networkIdle 也从一开始判得准（曾懒加载到首次读，
结果模型永远看不到首次加载的网络）。代价是每条标签常驻一份网络缓冲（CDP 侧
上限 16MB）。不碰 Debugger 域——拖慢 V8 的是它。用户一旦打开该标签的开发者工具
（index.js 监听 devtools-opened）立即 detach，把槽位让给人工调试，绝不阻塞；
标签关闭同样 detach。缓冲按标签常驻（主框架导航时清空），不做空闲释放。
*/
const CDP_VERSION = '1.3'
const CONSOLE_MAX = 300
const NET_MAX = 300
const BODY_MAX = 128 * 1024

/* dt 取或建某标签的采集状态。 */
function dt(tab) {
  if (!tab.dt) {
    tab.dt = {
      attached: false, error: '', console: [], net: [], pending: new Map(), seq: 0, wc: null,
      pageOn: false, domainsOn: false,
    }
  }
  return tab.dt
}

/* ensureAttached 只做 attach + 事件接线（幂等）。 */
function ensureAttached(tab) {
  const d = dt(tab)
  if (d.attached) return d
  const wc = tab.view.webContents
  if (wc.isDestroyed()) {
    d.error = '标签已销毁'
    return d
  }
  try {
    wc.debugger.attach(CDP_VERSION)
  } catch (err) {
    d.error = String(err?.message || err)
    return d
  }
  d.attached = true
  d.error = ''
  d.wc = wc
  d.pageOn = false
  d.domainsOn = false
  wc.debugger.removeAllListeners('message')
  wc.debugger.removeAllListeners('detach')
  wc.debugger.on('message', (_e, method, params) => onMessage(d, method, params))
  wc.debugger.on('detach', () => {
    d.attached = false
    d.pageOn = false
    d.domainsOn = false
  })
  return d
}

/*
	attachTab 建标签时调用：attach + enable 采集域（幂等）。

对话框必须在它弹出**之前**就挂上监听，否则页面会被 Chromium 挂住（CDP 场景下
javascriptDialogOpening 不回应 = 渲染暂停），所以这一档不能懒加载；采集域一并
在这里开，页面首次加载的网络/控制台才不会漏掉。
*/
function attachTab(tab) {
  const d = ensureAttached(tab)
  if (!d.attached) return d
  if (!d.pageOn) {
    d.pageOn = true
    d.wc.debugger.sendCommand('Page.enable').catch(() => {})
  }
  enableDomains(d)
  return d
}

/* enableDomains 开 Runtime/Log/Network（幂等）。三者开销都不大，重的是 Debugger 域
   ——本模块从不碰它。网络要给缓冲设上限，否则长会话撑内存。 */
function enableDomains(d) {
  if (d.domainsOn) return
  d.domainsOn = true
  for (const m of ['Runtime.enable', 'Log.enable']) {
    d.wc.debugger.sendCommand(m).catch(() => {})
  }
  d.wc.debugger.sendCommand('Network.enable', {
    maxTotalBufferSize: 16 << 20,
    maxResourceBufferSize: 2 << 20,
    maxPostDataSize: 64 << 10,
  }).catch(() => {})
}

/* onDialog 原生对话框接管：一律**自动取消**（dismiss），页面永远不会被弹窗挂住，
   并把这件事记进控制台缓冲让模型知道。只覆盖 alert/confirm——prompt() 在 Electron
   下直接抛 "not supported"。 */
function onDialog(d, p) {
  pushConsole(d, {
    level: 'warn',
    text: `[原生对话框 ${p.type || 'alert'}] ${p.message || ''}${p.defaultPrompt ? `（默认值 ${p.defaultPrompt}）` : ''} → 已自动取消`,
    stack: [],
    source: 'dialog',
  })
  d.wc.debugger.sendCommand('Page.handleJavaScriptDialog', { accept: false }).catch(() => {})
}

/* detachDevtools 摘掉本标签的采集（用户开 devtools / 关标签时调用）。 */
function detachDevtools(tab) {
  const d = tab.dt
  if (!d) return
  try {
    if (d.wc && !d.wc.isDestroyed() && d.wc.debugger.isAttached()) d.wc.debugger.detach()
  } catch {
    /* 已摘或无连接 */
  }
  d.attached = false
}

/* clearDevtools 清空缓冲（主框架导航时调用；attach 保持）。 */
function clearDevtools(tab) {
  const d = tab.dt
  if (!d) return
  d.console = []
  d.net = []
  d.pending = new Map()
}

/* ── 事件分发 ── */

function onMessage(d, method, params) {
  switch (method) {
    case 'Runtime.consoleAPICalled':
      return pushConsole(d, consoleEntry(params))
    case 'Runtime.exceptionThrown':
      return pushConsole(d, exceptionEntry(params.exceptionDetails))
    case 'Log.entryAdded':
      return pushConsole(d, logEntry(params.entry))
    case 'Network.requestWillBeSent':
      return onRequest(d, params)
    case 'Network.responseReceived':
      return onResponse(d, params)
    case 'Network.loadingFinished':
      return onFinished(d, params)
    case 'Network.loadingFailed':
      return onFailed(d, params)
    case 'Page.javascriptDialogOpening':
      return onDialog(d, params)
  }
}

function pushConsole(d, entry) {
  if (!entry) return
  d.console.push(entry)
  if (d.console.length > CONSOLE_MAX) d.console.splice(0, d.console.length - CONSOLE_MAX)
}

/* ── 控制台/异常/日志 ── */

function consoleEntry(p) {
  const stack = (p.stackTrace?.callFrames || [])
    .slice(0, 3)
    .map(frameLine)
    .filter(Boolean)
  return {
    level: levelOf(p.type),
    text: (p.args || []).map(remoteText).join(' '),
    stack,
    source: '',
  }
}

function exceptionEntry(x) {
  if (!x) return null
  const frames = (x.stackTrace?.callFrames || []).slice(0, 4).map(frameLine).filter(Boolean)
  const desc = x.exception ? remoteText(x.exception) : ''
  const text = [x.text, desc !== x.text ? desc : ''].filter(Boolean).join(' ')
  return { level: 'error', text: text || '未捕获异常', stack: frames, source: x.url || '' }
}

function logEntry(e) {
  if (!e) return null
  const src = [e.source, e.url ? shortURL(e.url) : ''].filter(Boolean).join(' ')
  return { level: levelOf(e.level), text: e.text || '', stack: [], source: src }
}

function frameLine(f) {
  if (!f) return ''
  const loc = f.url ? `${shortURL(f.url)}:${(f.lineNumber ?? 0) + 1}` : ''
  return `${f.functionName || '(匿名)'} @ ${loc}`.trim()
}

function levelOf(t) {
  switch (t) {
    case 'warning':
    case 'warn':
      return 'warn'
    case 'error':
      return 'error'
    case 'debug':
      return 'debug'
    case 'verbose':
      return 'verbose'
    case 'info':
      return 'info'
    default:
      return 'log'
  }
}

/* remoteText 把 CDP RemoteObject 压成一行可读文本（对象给浅层预览）。 */
function remoteText(a) {
  if (!a) return 'undefined'
  switch (a.type) {
    case 'string':
      return a.value ?? ''
    case 'number':
    case 'boolean':
      return String(a.value)
    case 'undefined':
      return 'undefined'
    case 'function':
      return a.description || 'function'
    case 'object':
      if (a.subtype === 'null') return 'null'
      if (a.preview) return previewText(a.preview)
      return a.description || a.className || 'Object'
    default:
      if ('value' in a) {
        try {
          return JSON.stringify(a.value)
        } catch {
          return String(a.value)
        }
      }
      return a.description || a.type
  }
}

function previewText(p) {
  const open = p.subtype === 'array' ? '[' : '{'
  const close = p.subtype === 'array' ? ']' : '}'
  const props = (p.properties || []).map((x) => `${x.name}: ${x.value === undefined ? x.type : x.value}`)
  return open + props.join(', ') + (p.overflow ? ', …' : '') + close
}

/* ── 网络 ── */

function onRequest(d, p) {
  const req = p.request || {}
  const entry = {
    id: p.requestId,
    n: ++d.seq,
    method: req.method || 'GET',
    url: req.url || '',
    type: p.type || '',
    status: 0,
    statusText: '',
    mime: '',
    reqHeaders: req.headers || {},
    resHeaders: {},
    postData: req.postData || '',
    size: 0,
    ms: 0,
    failed: false,
    error: '',
    startedAt: Date.now(),
    done: false,
  }
  d.pending.set(p.requestId, entry)
  d.net.push(entry)
  if (d.net.length > NET_MAX) d.net.splice(0, d.net.length - NET_MAX)
}

function onResponse(d, p) {
  const e = d.pending.get(p.requestId)
  if (!e) return
  const r = p.response || {}
  e.status = r.status || 0
  e.statusText = r.statusText || ''
  e.mime = r.mimeType || ''
  e.resHeaders = r.headers || {}
  if (p.type) e.type = p.type
}

function onFinished(d, p) {
  const e = d.pending.get(p.requestId)
  if (!e) return
  e.size = p.encodedDataLength || 0
  e.ms = Date.now() - e.startedAt
  e.done = true
  d.pending.delete(p.requestId)
}

function onFailed(d, p) {
  const e = d.pending.get(p.requestId)
  if (!e) return
  e.failed = true
  e.error = [p.errorText, p.blockedReason].filter(Boolean).join(' / ')
  e.ms = Date.now() - e.startedAt
  e.done = true
  d.pending.delete(p.requestId)
}

/* ── 呈现 ── */

/* formatConsole 控制台/异常/日志成多行文本（errorsOnly 只留 error）。 */
function formatConsole(tab, errorsOnly) {
  const d = dt(tab)
  const rows = errorsOnly ? d.console.filter((e) => e.level === 'error') : d.console
  if (!rows.length) return errorsOnly ? '(无 error)' : '(控制台无输出)'
  const lines = rows.map((e) => {
    let line = `[${e.level}] ${e.text}`
    if (e.source) line += `  (${e.source})`
    if (e.stack.length) line += '\n    at ' + e.stack.join('\n    at ')
    return line
  })
  return lines.join('\n')
}

/* formatNetworkList 网络清单（按序号，最近的在后）。filter=URL 子串，errorsOnly=只看失败。 */
function formatNetworkList(tab, opts) {
  const d = dt(tab)
  const filter = (opts && opts.filter) || ''
  const errorsOnly = !!(opts && opts.errorsOnly)
  let rows = d.net
  if (errorsOnly) rows = rows.filter((e) => e.failed || (e.status && e.status >= 400))
  if (filter) rows = rows.filter((e) => e.url.includes(filter))
  if (!rows.length) return d.net.length ? '(没有匹配的请求；去掉 filter/errorsOnly 看全部)' : '(暂无网络请求)'
  const total = rows.length
  const shown = rows.slice(-60)
  const head = shown.length < total ? `（共 ${total} 条，显示最近 ${shown.length} 条）\n` : ''
  return head + shown
    .map((e) => {
      const status = e.failed ? `失败${e.error ? ' ' + e.error : ''}` : e.status || '…'
      const t = e.done ? `${e.ms}ms` : '进行中'
      const size = e.size ? fmtSize(e.size) : ''
      return `#${e.n} ${e.method} ${status} [${e.type || '?'}] ${t} ${size} ${e.url}`.replace(/\s+/g, ' ')
    })
    .join('\n')
}

/* formatNetworkDetail 单条请求详情（含请求/响应头与 body）。 */
async function formatNetworkDetail(tab, n) {
  const d = dt(tab)
  const e = d.net.find((x) => x.n === n)
  if (!e) return `(没有序号 #${n} 的请求；先用 network 不带 index 看清单)`
  const head = `#${e.n} ${e.method} ${e.failed ? '失败' : e.status + ' ' + e.statusText} [${e.type || '?'}] ${e.done ? e.ms + 'ms' : '进行中'} ${e.url}`
  const parts = [head]
  if (e.error) parts.push('失败原因: ' + e.error)
  parts.push('请求头:\n' + headerLines(e.reqHeaders))
  if (e.postData) parts.push('请求体:\n' + e.postData)
  parts.push('响应头:\n' + headerLines(e.resHeaders))
  const body = await responseBody(d, e)
  if (body) parts.push('响应体:\n' + body)
  return parts.join('\n')
}

function headerLines(h) {
  const keys = Object.keys(h || {})
  if (!keys.length) return '  (无)'
  return keys.map((k) => `  ${k}: ${h[k]}`).join('\n')
}

/* responseBody 取响应体（懒取，failure/过大/非文本一律降级为提示）。 */
async function responseBody(d, e) {
  if (e.failed || !e.done) return ''
  if (!d.wc || d.wc.isDestroyed() || !d.wc.debugger.isAttached()) return ''
  try {
    const { body, base64Encoded } = await d.wc.debugger.sendCommand('Network.getResponseBody', { requestId: e.id })
    if (base64Encoded) return '(二进制响应体，已省略)'
    if (!body) return '(空响应体)'
    return body.length > BODY_MAX ? body.slice(0, BODY_MAX) + '\n…(响应体过长已截断)' : body
  } catch (err) {
    return `(响应体不可用：${String(err?.message || err)})`
  }
}

function fmtSize(n) {
  if (n < 1024) return n + 'B'
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + 'KB'
  return (n / 1024 / 1024).toFixed(2) + 'MB'
}

function shortURL(u) {
  try {
    const x = new URL(u)
    return x.pathname
  } catch {
    return u
  }
}

module.exports = {
  attachTab, detachDevtools, clearDevtools,
  formatConsole, formatNetworkList, formatNetworkDetail,
}
