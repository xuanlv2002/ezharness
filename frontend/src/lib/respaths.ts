/*
相对资源路径解析：markdown/html 查看器预览里引用的本地资源（图片、
音视频等）按「文件所在目录」归一成绝对路径，再交给工作目录文件服务
（/api/workspace/file）直出。协议外链（http/https/data/mailto…）、
协议相对（//cdn…）与纯锚点（#x）不属本地资源，返回 null 不改写。
md/html 原文编辑不受影响——只发生在预览渲染层。
*/

/* 解析出的绝对路径 → 工作目录文件服务 URL（同源 cookie 鉴权） */
export function fileResUrl(absPath: string): string {
  return '/api/workspace/file?path=' + encodeURIComponent(absPath)
}

/*
resolveLocalRef 把 ref（相对/以 / 开头的路径）解析到 baseFile 所在
目录下的绝对路径；非本地资源返回 null。`..` 不越出根（POSIX 根 /
Windows 盘符），锚点与查询串不参与路径。baseFile 的反斜杠统一按
分隔符处理（supper_url file:// 与 normFileKey 同一约定）。
*/
export function resolveLocalRef(baseFile: string, ref: string): string | null {
  const r = (ref || '').trim()
  if (!r || /^[a-z][a-z0-9+.-]*:/i.test(r) || r.startsWith('//') || r.startsWith('#')) return null
  const clean = r.replace(/[?#].*$/, '').trim()
  if (!clean) return null
  const base = baseFile.replace(/\\/g, '/')
  const dir = base.slice(0, base.lastIndexOf('/'))
  const drive = dir.match(/^([a-zA-Z]:)(\/|$)/)?.[1] ?? '' // Windows 盘符（C:）
  const stack: string[] = []
  if (!clean.startsWith('/')) {
    for (const seg of dir.split('/')) if (seg && seg !== '.') stack.push(seg)
  }
  for (const seg of clean.split('/')) {
    if (!seg || seg === '.') continue
    if (seg === '..') {
      /* 盘符段保留（.. 最多退到 C:/），POSIX 退到根为止 */
      if (stack.length > (drive ? 1 : 0)) stack.pop()
      continue
    }
    stack.push(seg)
  }
  if (!stack.length) return null
  /* 以 / 开头的引用按文件系统绝对路径对待（站点根语义无从判定） */
  const prefix = clean.startsWith('/') ? '/' : drive ? '' : '/'
  return prefix + stack.join('/')
}
