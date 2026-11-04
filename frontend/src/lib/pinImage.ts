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
