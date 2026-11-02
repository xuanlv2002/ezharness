/*
代码语法高亮（资源文本查看器用）：highlight.js core 按需注册语言，
只把用到的语法打进包。新增语言 = 这里 registerLanguage + LANG_BY_EXT
加一项，TextViewer 不用改。
*/
import hljs from 'highlight.js/lib/core'
import go from 'highlight.js/lib/languages/go'

hljs.registerLanguage('go', go)

const LANG_BY_EXT: Record<string, string> = { go: 'go' }

/* 超过该字符数不做高亮：hljs 逐字符扫描，超大文件每键重扫会拖住输入
   （编辑本身仍正常，只是回退纯文本显示） */
export const HL_MAX_CHARS = 64 * 1024

/* langOf 按文件名/路径判定高亮语言；不支持的返回 null。 */
export function langOf(pathOrName: string): string | null {
  const base = pathOrName.slice(Math.max(pathOrName.lastIndexOf('/'), pathOrName.lastIndexOf('\\')) + 1).toLowerCase()
  const i = base.lastIndexOf('.')
  if (i <= 0) return null
  return LANG_BY_EXT[base.slice(i + 1)] ?? null
}

/* highlightCode 返回已转义的 HTML（hljs 自带转义）；不支持/超限/出错
   返回 null，调用方回退纯文本。 */
export function highlightCode(text: string, lang: string): string | null {
  if (text.length > HL_MAX_CHARS) return null
  try {
    return hljs.highlight(text, { language: lang, ignoreIllegals: true }).value
  } catch {
    return null
  }
}
