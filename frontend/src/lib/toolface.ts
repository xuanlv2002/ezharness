/*
工具参数可读化协议:按工具名把原始 args JSON 解析成单一展示形态。
write_file→代码块(行号),edit_file→行级 diff,terminal/term_action send→命令高亮;
其余工具返回 null,组件退回 JSON 美化。
*/
export type ToolFace =
  | { kind: 'code'; path: string; content: string }
  | { kind: 'diff'; path: string; old: string; new: string }
  | { kind: 'cmd'; command: string }
  | { kind: 'plan'; goal: string; context: string; design: string; progress: { title: string; status: string }[]; verify: string }

export function toolFace(name: string, raw: string): ToolFace | null {
  let a: any
  try {
    a = JSON.parse(raw)
  } catch {
    return null
  }
  switch (name) {
    case 'write_file':
      return typeof a.content === 'string' ? { kind: 'code', path: a.path ?? '', content: a.content } : null
    case 'edit_file':
      return typeof a.old_text === 'string' ? { kind: 'diff', path: a.path ?? '', old: a.old_text, new: a.new_text ?? '' } : null
    case 'terminal':
      return typeof a.command === 'string' ? { kind: 'cmd', command: a.command } : null
    case 'term_action':
      return a.action === 'send' && typeof a.command === 'string' ? { kind: 'cmd', command: a.command } : null
    case 'plan': {
      const goal = typeof a.goal === 'string' ? a.goal : ''
      const context = typeof a.context === 'string' ? a.context : ''
      const design = typeof a.design === 'string' ? a.design : ''
      const verify = typeof a.verify === 'string' ? a.verify : ''
      const progress = Array.isArray(a.progress)
        ? a.progress
            .filter((p: any) => p && typeof p.title === 'string')
            .map((p: any) => ({ title: String(p.title), status: String(p.status || 'pending') }))
        : []
      return a.action === 'new' || goal || context || design || verify || progress.length
        ? { kind: 'plan', goal, context, design, progress, verify }
        : null
    }
  }
  return null
}

/* 记忆变更视图：参数是意图，结果是后端 enrichment（before/after/created/status）。
   结果未到（执行中）时按参数预演；project-read 是只读，不算变更卡。 */
export interface MemoryChangeView {
  action: string
  target: string
  id: string
  name: string
  description: string
  before: string
  after: string
  created: boolean
  pendingId: string
  status: string
  message: string
  text: string
}

const MEMORY_ACTIONS = new Set(['add', 'update', 'remove', 'project-save', 'project-remove'])

export function memoryChange(name: string, rawArgs: string, rawResult: string): MemoryChangeView | null {
  if (name !== 'memory') return null
  let a: any
  try {
    a = JSON.parse(rawArgs)
  } catch {
    return null
  }
  if (!a || !MEMORY_ACTIONS.has(a.action)) return null
  let r: any = null
  if (rawResult) {
    try {
      const parsed = JSON.parse(rawResult)
      if (parsed && typeof parsed === 'object' && MEMORY_ACTIONS.has(parsed.action)) r = parsed
    } catch {
      /* 非 JSON 结果（执行失败等）按参数预演 */
    }
  }
  const pick = (k: string) => (typeof r?.[k] === 'string' ? r[k] : typeof a[k] === 'string' ? a[k] : '')
  const text = pick('after') || pick('text') || pick('content')
  return {
    action: String(r?.action || a.action),
    target: pick('target'),
    id: pick('id'),
    name: pick('name'),
    description: pick('description'),
    before: pick('before'),
    after: pick('after'),
    created: !!r?.created,
    pendingId: pick('pendingId'),
    status: pick('status'),
    message: pick('message'),
    text,
  }
}

export type DiffLine = { t: '-' | '+' | ' '; s: string }

/* 行级 diff(LCS 公共子序列);规模超限退化为整块 -/+ 并列 */
export function diffLines(oldS: string, newS: string): DiffLine[] {
  const a = oldS.split('\n')
  const b = newS.split('\n')
  if (a.length * b.length > 160000) {
    return [...a.map((s): DiffLine => ({ t: '-', s })), ...b.map((s): DiffLine => ({ t: '+', s }))]
  }
  const n = a.length
  const m = b.length
  const dp: number[][] = Array.from({ length: n + 1 }, () => new Array<number>(m + 1).fill(0))
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      dp[i][j] = a[i] === b[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1])
    }
  }
  const out: DiffLine[] = []
  let i = 0
  let j = 0
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      out.push({ t: ' ', s: a[i] })
      i++
      j++
    } else if (dp[i + 1][j] >= dp[i][j + 1]) {
      out.push({ t: '-', s: a[i] })
      i++
    } else {
      out.push({ t: '+', s: b[j] })
      j++
    }
  }
  while (i < n) out.push({ t: '-', s: a[i++] })
  while (j < m) out.push({ t: '+', s: b[j++] })
  return out
}

/* shell 命令高亮:命令词/选项/字符串/变量/连接符分色,输出 HTML(文本已转义) */
export function highlightCommand(cmd: string): string {
  const esc = (s: string) => s.replace(/[&<>]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' })[c] as string)
  const tokens = cmd.match(/("(?:[^"\\]|\\.)*"|'[^']*'|&&|\|\||[|;]|--?[A-Za-z][\w-]*|\$\{[^}]*\}|\$\w+|[^\s|;&]+|\s+)/g) ?? []
  let inCmdPos = true
  let out = ''
  for (const m of tokens) {
    if (/^\s+$/.test(m)) {
      out += esc(m)
      continue
    }
    if (m === '&&' || m === '||' || m === '|' || m === ';') {
      out += `<span class="tf-op">${esc(m)}</span>`
      inCmdPos = true
    } else if (m.startsWith('"') || m.startsWith("'")) {
      out += `<span class="tf-str">${esc(m)}</span>`
      inCmdPos = false
    } else if (m.startsWith('$')) {
      out += `<span class="tf-var">${esc(m)}</span>`
      inCmdPos = false
    } else if (m.startsWith('-')) {
      out += `<span class="tf-flag">${esc(m)}</span>`
      inCmdPos = false
    } else if (inCmdPos) {
      out += `<span class="tf-word">${esc(m)}</span>`
      inCmdPos = false
    } else {
      out += esc(m)
    }
  }
  return out
}
