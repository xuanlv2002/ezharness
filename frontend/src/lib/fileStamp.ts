/* 文件磁盘指纹：优先 ETag（后端按纳秒 mtime + size 生成），回退 Last-Modified */

export function getStamp(r: Response): string {
  return r.headers.get('etag') || r.headers.get('last-modified') || ''
}

/* HEAD 取当前磁盘指纹；失败返回 null（调用方跳过本轮） */
export async function getFileStamp(path: string): Promise<string | null> {
  try {
    const r = await fetch('/api/workspace/file?path=' + encodeURIComponent(path), {
      method: 'HEAD',
      cache: 'no-store',
    })
    return r.ok ? getStamp(r) : null
  } catch {
    return null
  }
}
