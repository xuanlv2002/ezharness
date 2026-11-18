/*
无头后端注入页面的 JS（与 desktop 桥执行器同源的移植——模型看到的
工具行为跨端一致）。
*/
package service

import "strings"

/*
	deepQueryJS 深查询前置：穿透 shadow root 与同源 iframe（跨域帧够不着，

read mode=frames 另行报告）。__ezAbsRect 把元素矩形折算到顶层视口坐标。
*/
const deepQueryJS = `
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

/* elementsJS 列可交互元素（#id > [name] > [aria-label] > [placeholder] > nth-of-type 路径）。 */
const elementsJS = `(() => {
  const attr = (v) => String(v).replace(/"/g, '&quot;')
  const cssId = (id) => '#' + (window.CSS && CSS.escape ? CSS.escape(id) : id)
  const unique = (sel) => { try { return __ezAll(sel).length === 1 } catch { return false } }
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
      .trim().replace(/\s+/g, ' ').slice(0, 60)
    let line = (inFrame ? '[frame] ' : '') + el.tagName.toLowerCase() + ' [' + sel + ']'
    if (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA') line += ' type=' + (el.type || 'text')
    if (text) line += ' "' + text + '"'
    if (el.tagName === 'A' && el.href) line += ' -> ' + el.href.slice(0, 100)
    out.push(line)
  }
  return out.join('\n')
})()`

/* elementJS 单元素详情（路径/属性/盒/关键样式）。 */
func elementJS(selector string) string {
	return `(() => {
  const el = __ezOne(` + jsonString(selector) + `)
  if (!el) return '(未找到元素 ` + strings.ReplaceAll(selector, "`", "'") + `)'
  const st = getComputedStyle(el)
  const r = __ezAbsRect(el)
  const chain = []
  for (let n = el; n && n.nodeType === 1 && chain.length < 6; n = n.parentElement) {
    const cls = (typeof n.className === 'string' && n.className.trim()) ? '.' + n.className.trim().split(/\s+/).slice(0, 2).join('.') : ''
    chain.unshift(n.tagName.toLowerCase() + (n.id ? '#' + n.id : '') + cls)
  }
  const attrs = [...el.attributes].map((a) => a.name + '=' + JSON.stringify(a.value)).join(' ')
  const props = ['display', 'position', 'width', 'height', 'color', 'background-color', 'font-size', 'font-weight', 'opacity', 'visibility', 'overflow', 'z-index', 'pointer-events']
  const cs = props.map((p) => p + ':' + st.getPropertyValue(p)).join('; ')
  const text = (el.innerText || el.value || '').trim().replace(/\s+/g, ' ').slice(0, 200)
  const frame = el.ownerDocument !== document ? '(位于 iframe 内) ' : ''
  return '路径 ' + frame + chain.join(' > ') + '\n' +
    el.tagName.toLowerCase() + (attrs ? ' ' + attrs : '') + '\n' +
    '盒 ' + Math.round(r.width) + 'x' + Math.round(r.height) + ' @' + Math.round(r.left) + ',' + Math.round(r.top) + '(顶层视口坐标)\n' +
    cs + (text ? '\ntext "' + text + '"' : '')
})()`
}

/* linksJS 链接清单。 */
const linksJS = `(() => {
  return __ezAll('a').slice(0, 200)
    .map(a => (a.innerText.trim().slice(0,60) || '(无文字)') + ' → ' + a.href).join('\n')
})()`

/* deformJS 全局视觉变形检测（zoom/transform/filter 改动会污染坐标与截图）。 */
const deformJS = `(() => {
  const out = []
  for (const pair of [['html', document.documentElement], ['body', document.body]]) {
    const el = pair[1]
    if (!el || !el.style) continue
    for (const p of ['zoom', 'transform', 'filter']) {
      const v = el.style.getPropertyValue(p)
      if (v) out.push(pair[0] + '.' + p + '=' + v)
    }
  }
  return out.join('、')
})()`
