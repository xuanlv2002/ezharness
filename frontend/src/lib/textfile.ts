/*
文本文件判定与路径工具：supper_url file:// chip 点击门禁与文件面板
tab 去重共用（白名单单一来源）。后端 /api/workspace/* 不校验后缀，
文本门禁只在此处。
*/

const TEXT_EXTS = new Set([
  'txt', 'log', 'md', 'markdown', 'json', 'jsonc', 'json5',
  'py', 'pyw', 'go', 'js', 'mjs', 'cjs', 'ts', 'jsx', 'tsx',
  'svelte', 'vue', 'css', 'scss', 'less', 'html', 'htm', 'xml', 'svg',
  'yml', 'yaml', 'toml', 'ini', 'cfg', 'conf', 'env',
  'sh', 'bash', 'zsh', 'fish', 'ps1', 'bat', 'cmd',
  'sql', 'c', 'h', 'cpp', 'hpp', 'cc', 'hh', 'cs', 'rs',
  'java', 'kt', 'kts', 'swift', 'rb', 'php', 'lua', 'pl', 'r', 'm', 'dart', 'scala',
  'csv', 'tsv', 'gradle', 'properties', 'proto', 'tf',
])

/* 无扩展名的常见文本文件名 */
const TEXT_NAMES = new Set(['makefile', 'dockerfile', 'license', 'readme'])

export function fileBaseName(p: string): string {
  const i = Math.max(p.lastIndexOf('/'), p.lastIndexOf('\\'))
  return i >= 0 ? p.slice(i + 1) : p
}

export function isTextFilePath(p: string): boolean {
  const base = fileBaseName(p).toLowerCase()
  const i = base.lastIndexOf('.')
  return TEXT_NAMES.has(base) || (i > 0 && TEXT_EXTS.has(base.slice(i + 1)))
}

/* 去重键：分隔符与尾斜杠归一（C:\a.py 与 C:/a.py 同一 tab） */
export function normFileKey(p: string): string {
  return p.trim().replace(/\\/g, '/').replace(/\/+$/, '')
}
