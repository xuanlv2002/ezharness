/*
行级对比的纯函数（VSCode 式左右双栏）：旧/新文本 → 对齐行对序列。
先裁掉公共前后缀，中段走 LCS（规模受限，超限退化为按序配对）；
删除/新增按行序逐行配对成同高行，缺侧补空——左右行号一一对应，
两栏滚动才同步。
*/

export type RowType = 'same' | 'del' | 'add' | 'blank'

/* DiffSide 是一行对中的单侧：n=原文行号（0 = 空侧），t='' 为填充空行。 */
export interface DiffSide {
  n: number
  t: RowType
  text: string
}
export interface DiffRow {
  l: DiffSide
  r: DiffSide
}

/* LCS 中段的单侧规模上限（乘积再受限），超过退化按序配对。 */
const LCS_SIDE_CAP = 2000

function splitLines(s: string): string[] {
  const lines = s.replace(/\r\n/g, '\n').split('\n')
  if (lines.length > 1 && lines[lines.length - 1] === '') lines.pop()
  return lines
}

/* commonHead 公共前缀行数（在后缀之前计算，避免前后缀重叠吞掉中段）。 */
function commonHead(a: string[], b: string[]): number {
  let i = 0
  while (i < a.length && i < b.length && a[i] === b[i]) i++
  return i
}
function commonTail(a: string[], b: string[], head: number): number {
  let i = 0
  while (
    i < a.length - head &&
    i < b.length - head &&
    a[a.length - 1 - i] === b[b.length - 1 - i]
  )
    i++
  return i
}

/* diffRows 旧/新全文 → 行对序列（truncated 时只对比前 LIMIT 行）。 */
export function diffRows(oldText: string, newText: string): { rows: DiffRow[]; truncated: boolean } {
  const LIMIT = 6000
  let a = splitLines(oldText)
  let b = splitLines(newText)
  const truncated = a.length > LIMIT || b.length > LIMIT
  if (truncated) {
    a = a.slice(0, LIMIT)
    b = b.slice(0, LIMIT)
  }
  const head = commonHead(a, b)
  const tail = commonTail(a, b, head)
  const rows: DiffRow[] = []
  let an = 0 // 已消费的旧侧行号
  let bn = 0
  const same = (text: string) =>
    rows.push({ l: { n: ++an, t: 'same', text }, r: { n: ++bn, t: 'same', text } })
  for (let i = 0; i < head; i++) same(a[i])

  const am = a.slice(head, a.length - tail)
  const bm = b.slice(head, b.length - tail)
  for (const seg of middleSegments(am, bm)) {
    if (seg.t === 'same') {
      for (const text of seg.lines) same(text)
      continue
    }
    /* 删除/新增段逐行配对：短侧耗尽后长侧剩余行单侧成行（另一侧补空） */
    const dels = seg.t === 'del' ? seg.lines : []
    const adds = seg.t === 'add' ? seg.lines : []
    const k = Math.max(dels.length, adds.length)
    for (let i = 0; i < k; i++) {
      rows.push({
        l: i < dels.length ? { n: ++an, t: 'del', text: dels[i] } : { n: 0, t: 'blank', text: '' },
        r: i < adds.length ? { n: ++bn, t: 'add', text: adds[i] } : { n: 0, t: 'blank', text: '' },
      })
    }
  }
  for (let i = a.length - tail; i < a.length; i++) same(a[i])
  return { rows, truncated }
}

/* diffStats 增/删行数（状态条 +N −M 摘要用）。 */
export function diffStats(rows: DiffRow[]): { add: number; del: number } {
  let add = 0
  let del = 0
  for (const row of rows) {
    if (row.l.t === 'del') del++
    if (row.r.t === 'add') add++
  }
  return { add, del }
}

/* ── 融合（unified）视图：单列流水，先删后增按块输出 ── */

export interface UnifiedLine {
  n: number // 行号（del=旧侧，add/same=新侧）
  t: 'same' | 'del' | 'add'
  text: string
}

/* unifyRows 行对序列 → 融合行流水：连续的删/增行归为一个变更块，
   块内先删后增（git diff 的阅读习惯），上下文行原位穿插。 */
export function unifyRows(rows: DiffRow[]): UnifiedLine[] {
  const out: UnifiedLine[] = []
  let dels: DiffSide[] = []
  let adds: DiffSide[] = []
  const flush = () => {
    for (const d of dels) out.push({ n: d.n, t: 'del', text: d.text })
    for (const a of adds) out.push({ n: a.n, t: 'add', text: a.text })
    dels = []
    adds = []
  }
  for (const row of rows) {
    if (row.l.t === 'same' && row.r.t === 'same') {
      flush()
      out.push({ n: row.l.n, t: 'same', text: row.l.text })
      continue
    }
    if (row.l.t === 'del') dels.push(row.l)
    if (row.r.t === 'add') adds.push(row.r)
  }
  flush()
  return out
}

/* Seg 是中段的操作块（LCS 归并出 same/del/add 交替序列）。 */
interface Seg {
  t: 'same' | 'del' | 'add'
  lines: string[]
}

/*
middleSegments 计算中段：规模允许走 LCS 保持相同行的对齐，超限退化
为整段 del+add（仍逐行配对可看，只是相同行不再对齐）。
*/
function middleSegments(a: string[], b: string[]): Seg[] {
  if (a.length === 0 && b.length === 0) return []
  if (a.length === 0) return [{ t: 'add', lines: b }]
  if (b.length === 0) return [{ t: 'del', lines: a }]
  if (a.length > LCS_SIDE_CAP || b.length > LCS_SIDE_CAP || a.length * b.length > 4_000_000) {
    return [
      { t: 'del', lines: a },
      { t: 'add', lines: b },
    ]
  }
  /* LCS 动态规划表：f[i][j] = a[i..] 与 b[j..] 的公共子序列长度 */
  const m = a.length
  const n = b.length
  const f: Uint32Array[] = []
  for (let i = 0; i <= m; i++) f.push(new Uint32Array(n + 1))
  for (let i = m - 1; i >= 0; i--) {
    for (let j = n - 1; j >= 0; j--) {
      f[i][j] = a[i] === b[j] ? f[i + 1][j + 1] + 1 : Math.max(f[i + 1][j], f[i][j + 1])
    }
  }
  const segs: Seg[] = []
  const push = (t: Seg['t'], text: string) => {
    const last = segs[segs.length - 1]
    if (last && last.t === t) last.lines.push(text)
    else segs.push({ t, lines: [text] })
  }
  let i = 0
  let j = 0
  while (i < m && j < n) {
    if (a[i] === b[j]) {
      push('same', a[i])
      i++
      j++
    } else if (f[i + 1][j] >= f[i][j + 1]) {
      push('del', a[i++])
    } else {
      push('add', b[j++])
    }
  }
  while (i < m) push('del', a[i++])
  while (j < n) push('add', b[j++])
  return segs
}
