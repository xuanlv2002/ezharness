/* 便签正文的行块渲染与待办翻转：渲染态与编辑器共用同一套行解析，
   一行源码 = 一个块（空行也占一行），点正文进编辑器时光标才落得准。
   行内 markdown 原样显示——编辑器也只解析行首块语法，两边看到同一份
   文字，行高与换行天然一致。 */

export type PinLineKind = 'p' | 'h1' | 'h2' | 'h3' | 'todo' | 'bullet'

/* parsePinLine 一行源码 → 块类型/正文/勾选态（编辑器与渲染态共用）。 */
export function parsePinLine(line: string): { kind: PinLineKind; text: string; checked: boolean } {
  let m = /^(#{1,3})\s+(.*)$/.exec(line)
  if (m) return { kind: `h${m[1].length}` as PinLineKind, text: m[2], checked: false }
  m = /^\s*(?:[-*+]\s+)?\[([ xX]?)\](?:\s(.*))?$/.exec(line)
  if (m) return { kind: 'todo', text: m[2] ?? '', checked: m[1].toLowerCase() === 'x' }
  m = /^\s*[-*+]\s+(.*)$/.exec(line)
  if (m) return { kind: 'bullet', text: m[1], checked: false }
  return { kind: 'p', text: line, checked: false }
}

/* plainPinText 列表预览用的纯文本：去掉标题/列表/待办前缀与强调符号，
   不渲染 markdown（便签小窗与顶栏预览共用）。 */
export function plainPinText(md: string): string {
  return md
    .replace(/^#{1,6}\s+/gm, '')
    .replace(/^\s*[-*+]\s+(?:\[[ xX]?\]\s+)?/gm, '')
    .replace(/[*_`>]/g, '')
    .trim()
}

function escText(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

export function renderPinMarkdown(src: string): string {
  return src
    .split('\n')
    .map((line) => {
      const b = parsePinLine(line)
      const body = escText(b.text) || '<br>'
      if (b.kind === 'todo') {
        return `<div class="pin-blk" data-kind="todo"><input type="checkbox"${b.checked ? ' checked' : ''}><div class="pin-btext">${body}</div></div>`
      }
      if (b.kind === 'bullet') {
        return `<div class="pin-blk" data-kind="bullet"><span class="pin-box" aria-hidden="true"></span><div class="pin-btext">${body}</div></div>`
      }
      return `<div class="pin-blk" data-kind="${b.kind}"><div class="pin-btext">${body}</div></div>`
    })
    .join('')
}

/* toggleTask 按渲染顺序第 index 个待办块，翻转原文里对应的任务行；
   行判定与 parsePinLine 同源，勾选框和行一一对得上。 */
export function toggleTask(text: string, index: number): string | null {
  const lines = text.split('\n')
  let n = 0
  for (let i = 0; i < lines.length; i++) {
    const b = parsePinLine(lines[i])
    if (b.kind !== 'todo') continue
    if (n++ !== index) continue
    lines[i] = lines[i].replace(/\[([ xX]?)\]/, b.checked ? '[ ]' : '[x]')
    return lines.join('\n')
  }
  return null
}
