/* 便签图片：上传前缩到最长边 1600px、压成 WebP，控制 pinboard-images
   的体积与便签加载开销；透明通道由 WebP 保留。 */
const MAX_EDGE = 1600

export async function compressPinImage(file: File): Promise<Blob> {
  let bitmap: ImageBitmap
  try {
    bitmap = await createImageBitmap(file)
  } catch {
    throw new Error('无法读取图片')
  }
  const scale = Math.min(1, MAX_EDGE / Math.max(bitmap.width, bitmap.height))
  const w = Math.max(1, Math.round(bitmap.width * scale))
  const h = Math.max(1, Math.round(bitmap.height * scale))
  const canvas = document.createElement('canvas')
  canvas.width = w
  canvas.height = h
  const ctx = canvas.getContext('2d')
  if (!ctx) {
    bitmap.close()
    throw new Error('画布不可用')
  }
  ctx.drawImage(bitmap, 0, 0, w, h)
  bitmap.close()
  const out = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, 'image/webp', 0.86))
  if (!out) throw new Error('图片压缩失败')
  return out
}

/* hasImageFile 只看条目类型（dragover 阶段拿不到文件本体）。 */
export function hasImageFile(items: DataTransferItemList | DataTransferItem[] | null | undefined): boolean {
  if (!items) return false
  return Array.from(items).some((it) => it.kind === 'file' && it.type.startsWith('image/'))
}

/* imageFiles 从剪贴板/拖拽项里挑出图片文件。 */
export function imageFiles(items: DataTransferItemList | DataTransferItem[] | null | undefined): File[] {
  if (!items) return []
  return Array.from(items)
    .filter((it) => it.kind === 'file' && it.type.startsWith('image/'))
    .map((it) => it.getAsFile())
    .filter((f): f is File => !!f)
}

/*
copyPinImage 把卡面图片写进系统剪贴板（非 PNG 先转 PNG）；ok=是否成功，
msg=结果消息（默认成功文案，调用方可换成自己的）——板卡与顶栏便签预览共用。
*/
export async function copyPinImage(src: string): Promise<{ ok: boolean; msg: string }> {
  try {
    let blob = await (await fetch(src)).blob()
    if (blob.type !== 'image/png') {
      const bmp = await createImageBitmap(blob)
      const canvas = document.createElement('canvas')
      canvas.width = bmp.width
      canvas.height = bmp.height
      canvas.getContext('2d')?.drawImage(bmp, 0, 0)
      bmp.close()
      blob = await new Promise<Blob>((resolve, reject) =>
        canvas.toBlob((b) => (b ? resolve(b) : reject(new Error('转成 PNG 失败'))), 'image/png'),
      )
    }
    await navigator.clipboard.write([new ClipboardItem({ 'image/png': blob })])
    return { ok: true, msg: '已复制图片' }
  } catch (e) {
    return { ok: false, msg: `复制失败：${(e as Error).message}` }
  }
}
