/* 便签正文的 markdown 渲染与待办翻转：板卡与当天详情的归档卡共用。
   裸写的 `[ ] 待办` 补成 GFM 任务行（代码块内原样），输出经 DOMPurify
   消毒并去掉勾选框的 disabled（渲染态可直接点）。 */
import { marked } from 'marked'
import DOMPurify from 'dompurify'

function withBareTasks(src: string): string {
  let fence = false
  return src.split('\n').map((line) => {
    if (/^\s*(```|~~~)/.test(line)) {
      fence = !fence
      return line
    }
    if (fence) return line
    const m = /^(\s*)\[([ xX]?)\]\s+(.+)$/.exec(line)
    return m ? `${m[1]}- [${m[2].toLowerCase() === 'x' ? 'x' : ' '}] ${m[3]}` : line
  }).join('\n')
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

export function renderPinMarkdown(src: string): string {
  const html = marked.parse(withBareTasks(src), { gfm: true, breaks: true }) as string
  return DOMPurify.sanitize(html).replace(/<input\b[^>]*>/g, (tag) => tag.replace(/\sdisabled(?:="[^"]*")?/g, ''))
}

/* toggleTask 按 DOM 顺序第 index 个勾选框，翻转原文里对应的任务行。 */
export function toggleTask(text: string, index: number): string | null {
  const lines = text.split('\n')
  const task = /^((?:\s*(?:>\s*)*(?:[-*+]|\d+[.)])\s+|\s*)\[)( |x|X)(\]\s+\S)/
  let n = 0
  for (let i = 0; i < lines.length; i++) {
    const m = task.exec(lines[i])
    if (!m) continue
    if (n++ !== index) continue
    lines[i] = lines[i].slice(0, m[1].length) + (m[2].toLowerCase() === 'x' ? ' ' : 'x') + lines[i].slice(m[1].length + 1)
    return lines.join('\n')
  }
  return null
}
