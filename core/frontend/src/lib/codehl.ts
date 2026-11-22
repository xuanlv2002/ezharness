/*
代码语法高亮（资源文本查看器用）：highlight.js core 按需注册语言，
只把用到的语法打进包。新增语言 = 这里 registerLanguage + LANG_BY_EXT
加一项，TextViewer 不用改。
*/
import hljs from 'highlight.js/lib/core'
import bash from 'highlight.js/lib/languages/bash'
import c from 'highlight.js/lib/languages/c'
import cpp from 'highlight.js/lib/languages/cpp'
import csharp from 'highlight.js/lib/languages/csharp'
import css from 'highlight.js/lib/languages/css'
import dockerfile from 'highlight.js/lib/languages/dockerfile'
import go from 'highlight.js/lib/languages/go'
import ini from 'highlight.js/lib/languages/ini'
import java from 'highlight.js/lib/languages/java'
import javascript from 'highlight.js/lib/languages/javascript'
import json from 'highlight.js/lib/languages/json'
import kotlin from 'highlight.js/lib/languages/kotlin'
import makefile from 'highlight.js/lib/languages/makefile'
import markdown from 'highlight.js/lib/languages/markdown'
import powershell from 'highlight.js/lib/languages/powershell'
import python from 'highlight.js/lib/languages/python'
import rust from 'highlight.js/lib/languages/rust'
import scss from 'highlight.js/lib/languages/scss'
import sql from 'highlight.js/lib/languages/sql'
import swift from 'highlight.js/lib/languages/swift'
import typescript from 'highlight.js/lib/languages/typescript'
import xml from 'highlight.js/lib/languages/xml'
import yaml from 'highlight.js/lib/languages/yaml'

const LANGS = {
  bash,
  c,
  cpp,
  csharp,
  css,
  dockerfile,
  go,
  ini,
  java,
  javascript,
  json,
  kotlin,
  makefile,
  markdown,
  powershell,
  python,
  rust,
  scss,
  sql,
  swift,
  typescript,
  xml,
  yaml,
}
for (const [name, lang] of Object.entries(LANGS)) hljs.registerLanguage(name, lang)

/* 扩展名 → hljs 语言。svelte/vue 没有专属语法，按 xml 标记高亮
   （标签/属性正确，<script> 内退化为纯文本，仍比全灰好读） */
const LANG_BY_EXT: Record<string, string> = {
  go: 'go',
  js: 'javascript', mjs: 'javascript', cjs: 'javascript', jsx: 'javascript',
  ts: 'typescript', tsx: 'typescript',
  svelte: 'xml', vue: 'xml', html: 'xml', htm: 'xml', xml: 'xml', svg: 'xml',
  json: 'json', jsonc: 'json', json5: 'json',
  css: 'css', scss: 'scss', less: 'css',
  md: 'markdown', markdown: 'markdown',
  yml: 'yaml', yaml: 'yaml',
  sh: 'bash', bash: 'bash', zsh: 'bash', fish: 'bash',
  ps1: 'powershell',
  py: 'python', pyw: 'python',
  rs: 'rust',
  java: 'java', kt: 'kotlin', kts: 'kotlin',
  swift: 'swift',
  c: 'c', h: 'c', cpp: 'cpp', hpp: 'cpp', cc: 'cpp', hh: 'cpp', cs: 'csharp',
  sql: 'sql',
  ini: 'ini', cfg: 'ini', conf: 'ini', toml: 'ini', properties: 'ini', env: 'ini',
  dockerfile: 'dockerfile', makefile: 'makefile',
}

/* 超过该字符数不做高亮：hljs 逐字符扫描，超大文件每键重扫会拖住输入
   （编辑本身仍正常，只是回退纯文本显示） */
export const HL_MAX_CHARS = 64 * 1024

/* langOf 按文件名/路径判定高亮语言；不支持的返回 null。 */
export function langOf(pathOrName: string): string | null {
  const base = pathOrName.slice(Math.max(pathOrName.lastIndexOf('/'), pathOrName.lastIndexOf('\\')) + 1).toLowerCase()
  /* 无扩展名的常见文件名（Dockerfile / Makefile） */
  if (base === 'dockerfile' || base === 'makefile') return base
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
