/*
附件卡的轻量元数据：是否目录、文件大小与（pptx）幻灯片页数。后端只 stat
+ 读 zip 中央目录，不解压内容；前端按路径缓存一次，同一文件在对话里出现
多次也只请求一次。
*/

export interface FileMeta {
  size?: number
  pages?: number
  dir?: boolean
}

const cache = new Map<string, Promise<FileMeta>>()

export function fileMeta(path: string): Promise<FileMeta> {
  let pr = cache.get(path)
  if (!pr) {
    pr = fetch(`/api/workspace/meta?path=${encodeURIComponent(path)}`)
      .then((r) => (r.ok ? (r.json() as Promise<FileMeta>) : Promise.resolve({} as FileMeta)))
      .then((j) => ({ size: j.size, pages: j.pages, dir: j.dir }))
      .catch(() => ({}))
    cache.set(path, pr)
  }
  return pr
}

export function fmtBytes(n?: number): string {
  if (!n || n <= 0) return ''
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(n < 10 * 1024 ? 1 : 0)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

export function fmtDuration(sec: number): string {
  const s = Math.round(sec)
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`
}
