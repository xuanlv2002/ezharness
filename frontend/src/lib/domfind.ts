/*
Markdown 预览（.md-body）的文件内查找：在指定根节点的文本节点里自绘
命中高亮，不用 Electron findInPage（理由见 FindBar）。

每次查找先清掉上一次的 mark，再按全文顺序重新包，当前项 scrollIntoView。
*/

const STYLE_ID = 'ez-find-style'
const STYLE =
  'mark.ezfind{background:rgba(245,166,35,.35);color:inherit;border-radius:2px;padding:0}' +
  'mark.ezfind.cur{background:#f5a623;color:#111}'

/* 这些子树里的文字不进查找（不可见/控件/已有我们自己的命中） */
const SKIP = 'script, style, noscript, textarea, input, select, [contenteditable], mark.ezfind'

function docOf(root: ParentNode): Document | null {
  return root.nodeType === Node.DOCUMENT_NODE ? (root as Document) : (root as Node).ownerDocument
}

function addStyle(doc: Document): void {
  if (doc.getElementById(STYLE_ID)) return
  const el = doc.createElement('style')
  el.id = STYLE_ID
  el.textContent = STYLE
  ;(doc.head || doc.documentElement)?.appendChild(el)
}

/* 还原上一次的命中：mark 换回纯文本并合并相邻文本节点 */
export function clearDomFind(root: ParentNode | null | undefined): void {
  if (!root) return
  const doc = docOf(root)
  if (!doc) return
  root.querySelectorAll('mark.ezfind').forEach((m) => {
    const parent = m.parentNode
    if (!parent) return
    parent.replaceChild(doc.createTextNode(m.textContent || ''), m)
    parent.normalize()
  })
}

function textNodes(root: ParentNode, doc: Document): Text[] {
  const out: Text[] = []
  const walker = doc.createTreeWalker(root as Node, NodeFilter.SHOW_TEXT, {
    acceptNode(n) {
      const p = (n as Text).parentElement
      if (!p || p.closest(SKIP)) return NodeFilter.FILTER_REJECT
      return NodeFilter.FILTER_ACCEPT
    },
  })
  for (let n = walker.nextNode(); n; n = walker.nextNode()) out.push(n as Text)
  return out
}

export interface DomFindResult {
  count: number
  index: number
}

/*
domFind 在 root 的可见文本里查找 query，index 为期望命中的序号
（越界自动取模），返回总命中数与实际定位序号；命中项居中滚动。
root 为 null（预览还没挂载）时返回 0。
*/
export function domFind(root: ParentNode | null | undefined, query: string, index: number): DomFindResult {
  if (!root) return { count: 0, index: 0 }
  const doc = docOf(root)
  if (!doc) return { count: 0, index: 0 }
  addStyle(doc)
  clearDomFind(root)
  const q = query.trim().toLowerCase()
  if (!q) return { count: 0, index: 0 }
  const marks: HTMLElement[] = []
  for (const node of textNodes(root, doc)) {
    const text = node.data
    const low = text.toLowerCase()
    if (!low.includes(q)) continue
    const frag = doc.createDocumentFragment()
    let pos = 0
    let hit = low.indexOf(q)
    while (hit >= 0) {
      if (hit > pos) frag.appendChild(doc.createTextNode(text.slice(pos, hit)))
      const m = doc.createElement('mark')
      m.className = 'ezfind'
      m.textContent = text.slice(hit, hit + q.length)
      frag.appendChild(m)
      marks.push(m)
      pos = hit + q.length
      hit = low.indexOf(q, pos)
    }
    if (pos < text.length) frag.appendChild(doc.createTextNode(text.slice(pos)))
    node.parentNode?.replaceChild(frag, node)
  }
  if (!marks.length) return { count: 0, index: 0 }
  const i = ((index % marks.length) + marks.length) % marks.length
  marks[i].classList.add('cur')
  marks[i].scrollIntoView({ block: 'center', behavior: 'auto' })
  return { count: marks.length, index: i }
}
