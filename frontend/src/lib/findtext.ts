/* 文件内文本查找纯函数：命中拼成「原文 + <mark>」HTML 叠层（textarea 不能给局部文字上色） */

export interface MatchRange {
  start: number
  end: number
}

/* 命中区间（按出现顺序、互不重叠）；空查询返回空表 */
export function matchRanges(text: string, query: string): MatchRange[] {
  const q = query.trim().toLowerCase()
  if (!q || !text) return []
  const low = text.toLowerCase()
  const out: MatchRange[] = []
  for (let i = low.indexOf(q); i >= 0; i = low.indexOf(q, i + q.length)) {
    out.push({ start: i, end: i + q.length })
  }
  return out
}

const ESC: Record<string, string> = {
  '&': '&amp;',
  '<': '&lt;',
  '>': '&gt;',
  '"': '&quot;',
  "'": '&#39;',
}

function escapeHtml(s: string): string {
  return s.replace(/[&<>"']/g, (c) => ESC[c])
}

/* 全量文本 + 命中 <mark>：第 current 个加 cur 类（当前项深一档） */
export function markHtml(text: string, ranges: MatchRange[], current: number): string {
  let out = ''
  let pos = 0
  ranges.forEach((r, k) => {
    if (r.start < pos) return
    out += escapeHtml(text.slice(pos, r.start))
    const cls = k === current ? ' class="cur"' : ''
    out += `<mark${cls}>${escapeHtml(text.slice(r.start, r.end))}</mark>`
    pos = r.end
  })
  return out + escapeHtml(text.slice(pos))
}
