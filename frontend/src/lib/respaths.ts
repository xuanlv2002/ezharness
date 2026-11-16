/* 本地路径工具：Markdown 预览的相对引用解析 + 集成浏览器的 file:// 打开 */

/* 本地路径 → file:// URL；isDir 时补尾斜杠（集成浏览器打开文件夹） */
export function toFileUrl(path: string, isDir = false): string {
  let p = path.replace(/\\/g, '/')
  if (isDir && !p.endsWith('/')) p += '/'
  const body = p
    .split('/')
    .map((seg, i) => (i <= 1 && /^[A-Za-z]:$/.test(seg) ? seg : encodeURIComponent(seg)))
    .join('/')
  if (body.startsWith('//')) return 'file:' + body
  return 'file://' + (body.startsWith('/') ? '' : '/') + body
}

/* 解析出的绝对路径 → 工作目录文件服务 URL（同源 cookie 鉴权） */
export function fileResUrl(absPath: string): string {
  return '/api/workspace/file?path=' + encodeURIComponent(absPath)
}

/* 把本地引用解析到 baseFile 所在目录的绝对路径；外链/锚点/查询串返回 null */
export function resolveLocalRef(baseFile: string, ref: string): string | null {
  const r = (ref || '').trim()
  if (!r || r.startsWith('#') || r.startsWith('?') || /^[a-z][a-z0-9+.-]*:/i.test(r)) return null
  try {
    const url = new URL(r, toFileUrl(baseFile))
    if (url.protocol !== 'file:' || url.host) return null
    let path = url.pathname
    try {
      path = decodeURIComponent(path)
    } catch {
      /* 非法 % 序列按原样处理 */
    }
    if (path.length > 1 && path.endsWith('/')) path = path.slice(0, -1)
    return /^[A-Za-z]:[\\/]/.test(baseFile) && /^\/[A-Za-z]:\//.test(path) ? path.slice(1) : path
  } catch {
    return null
  }
}
