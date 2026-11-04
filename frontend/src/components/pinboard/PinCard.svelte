<script lang="ts">
  /*
  板上的一张便签卡：拖动（3px 阈值）、右下角缩放、悬停工具条（✓ 完成 /
  ✕ 删除就地确认）、双击就地编辑。正文按 markdown 渲染，待办勾选框可直接
  点，超出卡片高度时在正文区域内滚动。图片是贴在卡面上的贴纸：可直接拖入
  图片文件或在编辑器里粘贴，编辑时拖动图片移动、右下角手柄缩放，位置与
  大小随卡片一起存盘；选中后可复制到系统剪贴板，点图片看大图。

  底栏固定显示记于、已持续与截止（按剩余时长分档着色：3/6/12/24 小时内
  与 24 小时以上各一色，超期为危险色）。宽卡时会话列挪到右侧、与「记于」
  首行对齐：新会话 / 最近 1 个会话 / 更多会话，最多 3 行；窄卡仍上下排。
  底栏常显：卡片最小高度 = 底栏 + 一行正文（见 minCardHeight），正文超出
  时在内部滚动。

  编辑不弹面板：正文换成卡内块编辑器（`# ` 标题、`[] ` 待办、`- ` 列表
  自动转格式），底部一行改截止时间 + 取消/保存。点卡片外、保存按钮或
  Ctrl/⌘+Enter 提交，Esc 取消；空内容不提交（有图片时可只存图片）。

  拖动/缩放期间只改本地 transform 与宽高（不写共享状态），松手才提交
  x/y/w/h——连续 pointermove 不触发整板重排。now 由父级定时推进（30s），
  持续时间与超期状态跟着刷新。
  */
  import { onDestroy } from 'svelte'
  import { api, type PinCard as PinCardT } from '../../lib/api'
  import {
    MIN_W,
    PAD,
    WIDE_W,
    bumpCard,
    minCardHeight,
    patchCard,
    removeCard,
    sessionRows,
  } from '../../lib/pinboard.svelte'
  import { DUE_COLOR, dueLevel } from '../../lib/pinDue'
  import { renderPinMarkdown, toggleTask } from '../../lib/pinMarkdown'
  import { compressPinImage, hasImageFile, imageFiles } from '../../lib/pinImage'
  import PinRichEditor from './PinRichEditor.svelte'
  import PinSessionLinks from './PinSessionLinks.svelte'

  let {
    card,
    width = 800,
    height = 600,
    stack = 10,
    now = Date.now(),
    highlighted = false,
    leaving = false,
    oncomplete,
    onnewsession,
    onopensession,
    onview,
  }: {
    card: PinCardT
    width?: number
    height?: number
    stack?: number
    now?: number
    highlighted?: boolean
    leaving?: boolean
    oncomplete?: () => void
    onnewsession?: () => void
    onopensession?: (id: string) => void
    /* 点图片看大图（父级弹全屏预览） */
    onview?: (src: string) => void
  } = $props()

  function fmtShort(ts: number): string {
    return new Date(ts).toLocaleString('zh-CN', { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  }

  function dur(ms: number): string {
    const m = Math.max(0, Math.floor(ms / 60000))
    if (m < 60) return `${m} 分钟`
    const h = Math.floor(m / 60)
    if (h < 24) return `${h} 小时${m % 60 ? ` ${m % 60} 分` : ''}`
    return `${Math.floor(h / 24)} 天${h % 24 ? ` ${h % 24} 小时` : ''}`
  }

  /* 截止档位与说明：颜色按档位取，悬停说明还剩/超期多久 */
  const dueLv = $derived(dueLevel(card.due, now))
  const dueTip = $derived(
    !card.due
      ? '未设置截止时间'
      : card.due <= now
        ? `已超期 ${dur(now - card.due)}`
        : `还剩 ${dur(card.due - now)}`,
  )

  const clamp = (v: number, lo: number, hi: number): number => Math.max(lo, Math.min(hi, v))

  const pad2 = (n: number): string => String(n).padStart(2, '0')

  /* datetime-local 的值与卡片 due 时间戳互转（都按本地时区，精确到分钟） */
  function dueInput(ts: number): string {
    const d = new Date(ts)
    return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}T${pad2(d.getHours())}:${pad2(d.getMinutes())}`
  }
  function dueValue(s: string): number | undefined {
    const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})/.exec(s)
    if (!m) return undefined
    const ts = new Date(+m[1], +m[2] - 1, +m[3], +m[4], +m[5]).getTime()
    return Number.isFinite(ts) ? ts : undefined
  }

  /* 双击就地编辑：打开时快照正文、截止时间与图片，提交才写回卡片。 */
  function startEdit() {
    if (editing) return
    editText = card.text
    editDue = card.due ? dueInput(card.due) : ''
    editImages = [...(card.images ?? [])]
    editBoxes = {}
    imgUndo = []
    selImg = editImages[0] ?? '' // 选中第一张，手柄一眼可见
    editErr = ''
    editing = true
    bumpCard(card.id) // 编辑中的卡提到最上层，避免被相邻卡盖住
  }

  function saveEdit() {
    if (uploading) {
      editErr = '图片上传中…'
      return
    }
    const t = editText.trim()
    if (!t && !editImages.length) {
      editErr = '内容或图片至少留一样'
      return
    }
    /* 只落用户实际摆过的图：没动过的图继续走默认位 */
    const imgpos: NonNullable<PinCardT['imgpos']> = {}
    for (const src of editImages) {
      const b = editBoxes[src] ?? card.imgpos?.[src]
      if (b) imgpos[src] = b
    }
    patchCard(card.id, {
      text: t,
      due: dueValue(editDue),
      images: editImages.length ? editImages : undefined,
      imgpos: Object.keys(imgpos).length ? imgpos : undefined,
    })
    editing = false
    selImg = ''
    editBoxes = {}
    imgUndo = []
    editErr = ''
  }

  function cancelEdit() {
    editing = false
    selImg = ''
    editBoxes = {}
    imgUndo = []
    editErr = ''
  }

  function onEditKey(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      cancelEdit()
    } else if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
      e.preventDefault()
      saveEdit()
    }
  }

  /* 渲染态点击待办勾选框：按 DOM 顺序找到对应的原文任务行并翻转。
     marked 的 checkbox 默认 disabled，mdHtml 里已去掉该属性 */
  function onMdClick(e: MouseEvent) {
    const box = (e.target as HTMLElement).closest('input[type="checkbox"]') as HTMLInputElement | null
    if (!box || !mdEl) return
    e.preventDefault()
    const idx = Array.from(mdEl.querySelectorAll('input[type="checkbox"]')).indexOf(box)
    if (idx < 0) return
    const next = toggleTask(card.text, idx)
    if (next !== null) patchCard(card.id, { text: next })
  }

  /* ── 图片：编辑器内粘贴或把图片文件拖到卡面；卡面图片是贴纸，可拖动/缩放 ── */
  type ImgBox = { x: number; y: number; w: number; ar: number }

  let editImages = $state<string[]>([])
  /* 编辑中的图片摆放草稿（没动过的图不落草稿，沿用卡上已存位置/默认位） */
  let editBoxes = $state<Record<string, ImgBox>>({})
  let selImg = $state('')
  let imgMsg = $state('')
  let imgMsgTimer: ReturnType<typeof setTimeout> | undefined
  let uploading = $state(0)
  let dropping = $state(false)
  let dropErr = $state('')
  let dropErrTimer: ReturnType<typeof setTimeout> | undefined

  /* 图片宽高比：每张图只量一次，量到后高度才能按比例算 */
  let ars = $state<Record<string, number>>({})
  const arPending = new Set<string>()

  /* 新贴上去的图默认落在卡片左下、底栏上方，多张图往上错开一点 */
  function defaultImgBox(src: string, i: number): ImgBox {
    const cw = Math.max(1, showW - 2)
    const ch = Math.max(1, showH - 2)
    const ar = ars[src] ?? 4 / 3
    const w = 0.4
    const hFrac = (w * cw) / ar / ch
    const y = Math.max(0.04, 1 - 44 / ch - hFrac - (i % 3) * 0.06)
    return { x: 0.05 + (i % 4) * 0.05, y, w, ar }
  }

  /* imgBox 取当前该用的摆位：编辑草稿 → 卡上已存 → 默认位 */
  function imgBox(src: string, i: number): ImgBox {
    return (editing ? editBoxes[src] : undefined) ?? card.imgpos?.[src] ?? defaultImgBox(src, i)
  }

  /* imgStyle 把比例摆位换算成像素；宽定高、按宽高比推高，夹在卡内 */
  function imgStyle(src: string, i: number): string {
    const cw = Math.max(1, showW - 2)
    const ch = Math.max(1, showH - 2)
    const b = imgBox(src, i)
    const ar = b.ar > 0 ? b.ar : 4 / 3
    let w = Math.max(24, Math.min(cw, b.w * cw))
    let h = Math.max(12, w / ar)
    if (h > ch) {
      h = ch
      w = Math.max(12, h * ar)
    }
    const x = clamp(b.x * cw, 0, Math.max(0, cw - w))
    const y = clamp(b.y * ch, 0, Math.max(0, ch - h))
    return `left: ${x}px; top: ${y}px; width: ${w}px; height: ${h}px`
  }

  /* ── 贴纸拖动/缩放（只改编辑草稿，保存时才写回卡片） ── */
  let imgDrag = $state<{
    src: string
    mode: 'move' | 'resize'
    sx: number
    sy: number
    box: ImgBox
    lw: number
    lh: number
    active: boolean
  } | null>(null)

  function startImgDrag(e: PointerEvent, src: string, mode: 'move' | 'resize'): void {
    if (!editing || e.button !== 0) return
    e.preventDefault()
    e.stopPropagation()
    selImg = src
    const sticker = (e.currentTarget as HTMLElement).closest('.sticker') as HTMLElement | null
    const layer = el?.querySelector('.imglayer') as HTMLElement | null
    const lr = layer?.getBoundingClientRect()
    if (!sticker || !lr || !lr.width || !lr.height) return
    const r = sticker.getBoundingClientRect()
    const box: ImgBox = {
      x: (r.left - lr.left) / lr.width,
      y: (r.top - lr.top) / lr.height,
      w: r.width / lr.width,
      ar: r.width / Math.max(1, r.height),
    }
    imgDrag = { src, mode, sx: e.clientX, sy: e.clientY, box, lw: lr.width, lh: lr.height, active: false }
    window.addEventListener('pointermove', onImgMove)
    window.addEventListener('pointerup', onImgUp)
    window.addEventListener('pointercancel', onImgCancel)
    window.addEventListener('blur', onImgCancel)
  }

  function stopImgListeners(): void {
    window.removeEventListener('pointermove', onImgMove)
    window.removeEventListener('pointerup', onImgUp)
    window.removeEventListener('pointercancel', onImgCancel)
    window.removeEventListener('blur', onImgCancel)
  }

  function onImgMove(e: PointerEvent): void {
    if (!imgDrag) return
    const dx = e.clientX - imgDrag.sx
    const dy = e.clientY - imgDrag.sy
    if (!imgDrag.active) {
      if (Math.hypot(dx, dy) < 3) return
      imgDrag.active = true
      pushImgUndo() // 拍下移动/缩放前的摆位
    }
    const { lw, lh } = imgDrag
    const b = { ...imgDrag.box }
    if (imgDrag.mode === 'move') {
      const w = b.w * lw
      const h = w / b.ar
      b.x = clamp(b.x + dx / lw, 0, Math.max(0, 1 - w / lw))
      b.y = clamp(b.y + dy / lh, 0, Math.max(0, 1 - h / lh))
    } else {
      /* 右下角手柄：左上角不动，宽改变、高按宽高比跟着走，不越出卡面 */
      const maxW = Math.max(36, Math.min(lw - b.x * lw - 2, (lh - b.y * lh - 2) * b.ar))
      b.w = clamp(b.w * lw + dx, 36, maxW) / lw
    }
    editBoxes = { ...editBoxes, [imgDrag.src]: b }
  }

  function onImgUp(): void {
    endImgDrag(false)
  }

  function onImgCancel(): void {
    endImgDrag(true)
  }

  function endImgDrag(revert: boolean): void {
    if (!imgDrag) return
    const d = imgDrag
    if (revert && d.active) editBoxes = { ...editBoxes, [d.src]: d.box }
    stopImgListeners()
    imgDrag = null
  }

  function removeEditImage(src: string): void {
    pushImgUndo()
    editImages = editImages.filter((s) => s !== src)
    /* 保留 selImg：刚删掉的图还能立刻 Ctrl+Z 撤回 */
  }

  /* ── 复制图片：写系统剪贴板（PNG），复制后可直接粘到别的应用或再粘回卡面 ── */
  function flashImgMsg(msg: string): void {
    imgMsg = msg
    clearTimeout(imgMsgTimer)
    imgMsgTimer = setTimeout(() => (imgMsg = ''), 1600)
  }

  async function pngBlob(blob: Blob): Promise<Blob> {
    if (blob.type === 'image/png') return blob
    const bmp = await createImageBitmap(blob)
    const canvas = document.createElement('canvas')
    canvas.width = bmp.width
    canvas.height = bmp.height
    canvas.getContext('2d')?.drawImage(bmp, 0, 0)
    bmp.close()
    return await new Promise((resolve, reject) =>
      canvas.toBlob((b) => (b ? resolve(b) : reject(new Error('转成 PNG 失败'))), 'image/png'),
    )
  }

  async function copyImage(src: string, msg = '已复制图片'): Promise<boolean> {
    try {
      const png = await pngBlob(await (await fetch(src)).blob())
      await navigator.clipboard.write([new ClipboardItem({ 'image/png': png })])
      flashImgMsg(msg)
      return true
    } catch (e) {
      flashImgMsg(`复制失败：${(e as Error).message}`)
      return false
    }
  }

  /* ── 图片撤销：编辑会话内每次移动/缩放/增删前拍一张快照 ── */
  type ImgSnap = { images: string[]; boxes: Record<string, ImgBox> }
  let imgUndo: ImgSnap[] = []

  function pushImgUndo(): void {
    imgUndo.push({ images: [...editImages], boxes: { ...editBoxes } })
    if (imgUndo.length > 30) imgUndo.shift()
  }

  function undoImg(): void {
    const s = imgUndo.pop()
    if (!s) return
    const had = new Set(editImages)
    editImages = s.images
    editBoxes = s.boxes
    const back = s.images.find((src) => !had.has(src))
    if (back) selImg = back
    else if (!editImages.includes(selImg)) selImg = editImages[0] ?? ''
    flashImgMsg('已撤销')
  }

  async function cutImage(src: string): Promise<void> {
    if (!editImages.includes(src)) return
    if (!(await copyImage(src, '已剪切图片'))) return
    pushImgUndo()
    editImages = editImages.filter((s) => s !== src)
    /* 保留 selImg：刚剪掉的图还能立刻 Ctrl+Z 撤回 */
  }

  /* 选中图片时 Ctrl/⌘+C 复制、Ctrl/⌘+X 剪切、Ctrl/⌘+Z 撤销图片操作；
     有文字选区或在输入框里时让浏览器照常处理文字 */
  $effect(() => {
    if (!editing) return
    const onKey = (e: KeyboardEvent) => {
      if (!(e.ctrlKey || e.metaKey)) return
      const key = e.key.toLowerCase()
      if (key !== 'c' && key !== 'x' && key !== 'z') return
      if ((e.target as HTMLElement | null)?.closest('input, textarea')) return
      const s = window.getSelection()
      if (s && !s.isCollapsed && s.anchorNode && el?.contains(s.anchorNode)) return
      if (key === 'z') {
        if (!selImg || !imgUndo.length) return
        e.preventDefault()
        undoImg()
        return
      }
      if (!selImg) return
      e.preventDefault()
      if (key === 'c') void copyImage(selImg)
      else void cutImage(selImg)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })

  async function uploadImages(files: File[]): Promise<string[]> {
    uploading += files.length
    try {
      return await Promise.all(
        files.map(async (f) => (await api.uploadPinImage(await compressPinImage(f))).url),
      )
    } finally {
      uploading -= files.length
    }
  }

  async function addEditImages(files: File[]) {
    editErr = ''
    try {
      const urls = await uploadImages(files)
      if (urls.length) {
        pushImgUndo()
        editImages = [...editImages, ...urls]
        selImg = urls[urls.length - 1] // 新贴的图直接选中，好马上调位置
      }
    } catch (e) {
      editErr = `图片上传失败：${(e as Error).message}`
    }
  }

  /* 非编辑态拖入失败的提示：卡面浮一条，4s 后自动消失 */
  function showDropErr(msg: string) {
    dropErr = msg
    clearTimeout(dropErrTimer)
    dropErrTimer = setTimeout(() => (dropErr = ''), 4000)
  }

  function onDragOver(e: DragEvent) {
    if (!hasImageFile(e.dataTransfer?.items)) return
    e.preventDefault()
    dropping = true
  }

  function onDragLeave(e: DragEvent) {
    const to = e.relatedTarget
    if (!(to instanceof Node) || !el?.contains(to)) dropping = false
  }

  async function onDrop(e: DragEvent) {
    const files = imageFiles(e.dataTransfer?.items)
    dropping = false
    if (!files.length) return
    e.preventDefault()
    e.stopPropagation()
    if (editing) {
      await addEditImages(files)
      return
    }
    try {
      const urls = await uploadImages(files)
      patchCard(card.id, { images: [...(card.images ?? []), ...urls] })
    } catch (err) {
      showDropErr(`图片上传失败：${(err as Error).message}`)
    }
  }

  let el = $state<HTMLElement>()
  let drag = $state<{ px: number; py: number; x: number; y: number } | null>(null)
  let moved = $state(false)
  let dx = $state(0)
  let dy = $state(0)

  let resizing = $state(false)
  let liveW = $state(0)
  let liveH = $state(0)
  let rs = $state<{ px: number; py: number; w: number; h: number } | null>(null)

  let confirmDel = $state(false)

  /* 就地编辑草稿：打开时快照，提交才写回卡片 */
  let editing = $state(false)
  let editText = $state('')
  let editDue = $state('')
  let editErr = $state('')
  let mdEl = $state<HTMLElement>()

  const showW = $derived(resizing ? liveW : card.w)
  const showH = $derived(resizing ? liveH : card.h)

  /* 显示中的图片：编辑态看草稿，平时看卡上已存的图 */
  const shownImages = $derived(editing ? editImages : (card.images ?? []))
  /* 量每张图的原生宽高比；量到后按比例排高度 */
  $effect(() => {
    for (const src of shownImages) {
      if (ars[src] !== undefined || arPending.has(src)) continue
      arPending.add(src)
      const im = new Image()
      im.onload = () => {
        arPending.delete(src)
        if (!im.naturalWidth || !im.naturalHeight) return
        ars = { ...ars, [src]: im.naturalWidth / im.naturalHeight }
      }
      im.onerror = () => arPending.delete(src)
      im.src = src
    }
  })

  /* 底栏（时间 + 会话）固定常显：卡片高度不低于底栏 + 一行正文 */
  const minH = $derived(minCardHeight(card.w, sessionRows(card.sessions?.length ?? 0)))
  /* 旧数据或关联会话变多后自动补足，避免底栏被裁 */
  $effect(() => {
    if (card.h < minH) patchCard(card.id, { h: minH })
  })

  /* 卡片正文按 markdown 渲染；待办勾选框去掉 disabled，点击直接改原文。
     只有图片没有正文时不显示「空便签」占位 */
  const mdHtml = $derived(renderPinMarkdown(card.text || (card.images?.length ? '' : '（空便签）')))

  /* 编辑中点击卡片外即提交；正文与图片都空则放弃 */
  $effect(() => {
    if (!editing) return
    const down = (e: PointerEvent) => {
      if (el?.contains(e.target as Node)) return
      if (uploading) return // 上传中不落定，等图片 URL 回来再提交
      if (editText.trim() || editImages.length) saveEdit()
      else cancelEdit()
    }
    document.addEventListener('pointerdown', down, true)
    return () => document.removeEventListener('pointerdown', down, true)
  })

  /* 指针落在内容区的滚动条上：不启动卡片拖动 / 双击编辑，让滚动条自己
     处理拖拽（内容溢出时正文右侧出现 6px 滚动条） */
  function onScrollbar(e: MouseEvent): boolean {
    const t = e.target as HTMLElement
    if (t.scrollHeight > t.clientHeight && e.offsetX >= t.clientWidth) return true
    return t.scrollWidth > t.clientWidth && e.offsetY >= t.clientHeight
  }

  /* 拖拽/缩放的收尾监听挂在 window 上：松手、取消、窗口失焦都能收尾，
     不会出现「松手没检测到、卡一直挂在拖动状态」 */
  function onDown(e: PointerEvent) {
    if (e.button !== 0) return
    selImg = '' // 点卡面空白处取消图片选中（点贴纸本身由贴纸自己拦住）
    if (editing) return
    if ((e.target as HTMLElement).closest('.nodrag, .sticker, a, input, button')) return
    if (onScrollbar(e)) return
    drag = { px: e.clientX, py: e.clientY, x: card.x, y: card.y }
    moved = false
    try {
      el?.setPointerCapture(e.pointerId)
    } catch {
      /* 指针捕获失败时 window 监听仍可用 */
    }
    window.addEventListener('pointermove', onMove)
    window.addEventListener('pointerup', onUp)
    window.addEventListener('pointercancel', cancelDrag)
    window.addEventListener('blur', cancelDrag)
  }

  function removeDragWindow() {
    window.removeEventListener('pointermove', onMove)
    window.removeEventListener('pointerup', onUp)
    window.removeEventListener('pointercancel', cancelDrag)
    window.removeEventListener('blur', cancelDrag)
  }

  function onMove(e: PointerEvent) {
    if (!drag) return
    const ddx = e.clientX - drag.px
    const ddy = e.clientY - drag.py
    if (!moved && Math.hypot(ddx, ddy) < 3) return
    if (!moved) {
      moved = true
      bumpCard(card.id) // 拖起提层，拖动中的卡在最上层
    }
    dx = clamp(drag.x + ddx, PAD, Math.max(PAD, width - card.w - PAD)) - card.x
    dy = clamp(drag.y + ddy, PAD, Math.max(PAD, height - card.h - PAD)) - card.y
  }

  function onUp() {
    removeDragWindow()
    if (!drag) return
    if (moved) patchCard(card.id, { x: card.x + dx, y: card.y + dy })
    drag = null
    moved = false
    dx = 0
    dy = 0
  }

  function cancelDrag() {
    removeDragWindow()
    drag = null
    moved = false
    dx = 0
    dy = 0
  }

  function dbl(e: MouseEvent) {
    if (editing) return
    if ((e.target as HTMLElement).closest('.nodrag, a, input, button')) return
    if (moved || onScrollbar(e)) return
    startEdit()
  }

  /* 缩放：右下角 grip，最小尺寸 MIN_W/MIN_H，不越出板右/下缘 */
  function rDown(e: PointerEvent) {
    e.stopPropagation()
    rs = { px: e.clientX, py: e.clientY, w: card.w, h: card.h }
    liveW = card.w
    liveH = card.h
    resizing = true
    try {
      ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
    } catch {
      /* window 监听兜底 */
    }
    window.addEventListener('pointermove', rMove)
    window.addEventListener('pointerup', rUp)
    window.addEventListener('pointercancel', cancelResize)
    window.addEventListener('blur', cancelResize)
  }

  function removeResizeWindow() {
    window.removeEventListener('pointermove', rMove)
    window.removeEventListener('pointerup', rUp)
    window.removeEventListener('pointercancel', cancelResize)
    window.removeEventListener('blur', cancelResize)
  }

  function rMove(e: PointerEvent) {
    if (!rs) return
    liveW = clamp(rs.w + e.clientX - rs.px, MIN_W, Math.max(MIN_W, width - card.x - PAD))
    liveH = clamp(rs.h + e.clientY - rs.py, minH, Math.max(minH, height - card.y - PAD))
  }

  function rUp() {
    removeResizeWindow()
    if (!rs) return
    patchCard(card.id, { w: liveW, h: liveH })
    rs = null
    resizing = false
  }

  function cancelResize() {
    removeResizeWindow()
    rs = null
    resizing = false
    liveW = 0
    liveH = 0
  }

  onDestroy(() => {
    removeDragWindow()
    removeResizeWindow()
    stopImgListeners()
    clearTimeout(dropErrTimer)
    clearTimeout(imgMsgTimer)
  })

  function removeNow() {
    confirmDel = false
    removeCard(card.id)
  }
</script>

<article
  bind:this={el}
  class="pcard"
  class:dragging={moved}
  class:resizing
  class:editing
  class:leaving
  class:hl={highlighted}
  class:wide={card.w >= WIDE_W}
  class:dropping
  style:left="{card.x}px"
  style:top="{card.y}px"
  style:width="{showW}px"
  style:height="{showH}px"
  style:z-index={moved ? 9999 : stack}
  style:--tint={card.tint}
  style:transform={moved ? `translate(${dx}px, ${dy}px)` : undefined}
  onpointerdown={onDown}
  ondblclick={dbl}
  ondragover={onDragOver}
  ondragleave={onDragLeave}
  ondrop={onDrop}
  role="group"
  aria-label="便签"
>
  {#if !editing}
    <div class="bar">
      <button class="tool nodrag" onclick={(e) => { e.stopPropagation(); oncomplete?.() }} title="划线完成，归档到时间线">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round">
          <path d="M5 12.5l4.5 4.5L19 7" />
        </svg>
      </button>
      <button class="tool nodrag" onclick={(e) => { e.stopPropagation(); confirmDel = true }} title="删除这张便签">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round">
          <path d="M6 6l12 12M18 6L6 18" />
        </svg>
      </button>
    </div>
  {/if}

  <div class="body">
    {#if editing}
      <PinRichEditor
        value={editText}
        onchange={(md) => (editText = md)}
        onsave={saveEdit}
        oncancel={cancelEdit}
        onimage={(files) => void addEditImages(files)}
      />
      {#if editErr}<p class="edit-err">{editErr}</p>{/if}
      <div class="ebar nodrag">
        {#if uploading}<span class="upstatus">图片上传中…</span>{/if}
        <label class="duefield">
          <span>截止</span>
          <input type="datetime-local" bind:value={editDue} onkeydown={onEditKey} title="截止时间（可留空）" />
        </label>
        <button class="xdue" disabled={!editDue} onclick={() => (editDue = '')} title="清除截止时间">✕</button>
        <button class="ebtn cancel" onclick={cancelEdit} title="放弃修改（Esc）">取消</button>
        <button class="ebtn save" disabled={uploading > 0} onclick={saveEdit} title="保存（Ctrl/⌘+Enter）">保存</button>
      </div>
    {:else}
      <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
      <div class="mdbody" bind:this={mdEl} onclick={onMdClick}>
        {@html mdHtml}
      </div>
      <!-- 底栏：左列记于/已持续/截止，右列会话（新会话、最近 1 个、更多） -->
      <div class="foot">
        <div class="times">
          <span class="tline">记于 {fmtShort(card.created)} · 已持续 {dur(now - card.created)}</span>
          <span class="tline" class:over={dueLv === 'over'} style:color={dueLv ? DUE_COLOR[dueLv] : undefined} title={dueTip}>
            截止 {card.due ? fmtShort(card.due) : '未设置'}
          </span>
        </div>
        <!-- 新会话 → 已真实发起的关联会话（待定会话不显示） -->
        <PinSessionLinks sessions={card.sessions ?? []} showNew onnew={onnewsession} onopen={onopensession} />
      </div>
    {/if}
  </div>

  <!-- 卡面图片贴纸：编辑中拖动/缩放，点选后出现删除与缩放手柄 -->
  <div class="imglayer">
    {#each shownImages as src, i (src)}
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="sticker"
        class:sel={editing && selImg === src}
        style={imgStyle(src, i)}
        title={editing ? '拖动移动 · 双击看大图 · Ctrl+C 复制 · Ctrl+X 剪切 · Ctrl+Z 撤销' : undefined}
        onpointerdown={editing ? (e) => startImgDrag(e, src, 'move') : undefined}
        ondblclick={editing
          ? (e) => {
              e.stopPropagation()
              onview?.(src)
            }
          : undefined}
      >
        <img {src} alt="" draggable="false" />
        {#if editing && selImg === src}
          <button
            class="imgcopy"
            onpointerdown={(e) => e.stopPropagation()}
            onclick={(e) => {
              e.stopPropagation()
              void copyImage(src)
            }}
            title="复制图片"
            aria-label="复制图片"
          >
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <rect x="9" y="9" width="10" height="11" rx="2" />
              <path d="M5 15V6a2 2 0 0 1 2-2h7" />
            </svg>
          </button>
          <button
            class="imgx"
            onpointerdown={(e) => e.stopPropagation()}
            onclick={(e) => {
              e.stopPropagation()
              removeEditImage(src)
            }}
            title="移除图片"
            aria-label="移除图片"
          >✕</button>
          <button
            class="imggrip"
            onpointerdown={(e) => startImgDrag(e, src, 'resize')}
            title="拖动缩放"
            aria-label="拖动缩放图片"
          ></button>
        {/if}
      </div>
    {/each}
  </div>

  <span class="grip nodrag" onpointerdown={rDown} role="presentation"></span>

  {#if dropping}
    <div class="drop-hint">松手贴图</div>
  {/if}
  {#if dropErr}
    <div class="drop-err">{dropErr}</div>
  {/if}

  {#if confirmDel}
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="confirm nodrag" onpointerdown={(e) => e.stopPropagation()}>
      <p>删掉这张？</p>
      <div class="cbtns">
        <button onclick={(e) => { e.stopPropagation(); confirmDel = false }}>取消</button>
        <button class="danger" onclick={(e) => { e.stopPropagation(); removeNow() }}>删除</button>
      </div>
    </div>
  {/if}

  {#if imgMsg}<div class="imgmsg">{imgMsg}</div>{/if}
</article>

<style>
  .pcard {
    position: absolute;
    display: flex;
    flex-direction: column;
    box-sizing: border-box;
    /* 白纸便签：卡面保持纸色，便签色只做左侧 2px 竖条（第一版观感） */
    border: 1px solid var(--line);
    border-radius: 8px;
    background: var(--bg);
    color: var(--fg);
    box-shadow:
      0 1px 2px rgb(24 20 16 / 4%),
      0 10px 22px -16px rgb(24 20 16 / 55%);
    cursor: grab;
    touch-action: none;
    /* 只过渡阴影与描边：transform 参与过渡会在松手（left/top 提交 + 位移
       归零）时播放一段位移，看起来抖一下 */
    transition:
      box-shadow var(--dur-fast) var(--ease-out),
      border-color var(--dur-fast) var(--ease-out);
  }
  /* 便签色：左侧 2px 竖条常驻 */
  .pcard::before {
    content: '';
    position: absolute;
    left: 0;
    top: 6px;
    bottom: 6px;
    width: 2px;
    border-radius: 2px;
    background: var(--tint, #b291a6);
  }
  .pcard:hover {
    box-shadow:
      0 1px 2px rgb(24 20 16 / 5%),
      0 14px 28px -16px rgb(24 20 16 / 60%);
  }
  /* 编辑中：描边与柔光标记当前卡，提示不是在拖动 */
  .pcard.editing {
    border-color: color-mix(in srgb, var(--accent) 45%, var(--line));
    box-shadow:
      0 0 0 3px color-mix(in srgb, var(--accent) 10%, transparent),
      0 14px 28px -16px rgb(24 20 16 / 60%);
    cursor: default;
  }
  .pcard.dragging,
  .pcard.resizing {
    cursor: grabbing;
    transition: none;
    z-index: 9999;
  }
  /* 高亮（时间线卡拖回板上）：描边闪一下，卡不挪位 */
  .pcard.hl {
    box-shadow:
      0 0 0 2px var(--accent),
      0 14px 28px -16px rgb(24 20 16 / 60%);
  }
  /* 图片文件拖到卡面：整卡描边提示松手即贴 */
  .pcard.dropping {
    border-color: var(--accent);
    box-shadow:
      0 0 0 3px color-mix(in srgb, var(--accent) 12%, transparent),
      0 14px 28px -16px rgb(24 20 16 / 60%);
  }
  /* 完成离场：淡出上移（240ms 后再真正移出数据） */
  .pcard.leaving {
    opacity: 0;
    transform: translateY(10px) scale(0.95);
    pointer-events: none;
    transition:
      opacity 0.2s var(--ease-out),
      transform 0.2s var(--ease-out);
  }
  .bar {
    position: absolute;
    top: 4px;
    right: 4px;
    z-index: 4;
    display: flex;
    gap: 2px;
    opacity: 0;
    pointer-events: none;
    transition: opacity var(--dur-fast) var(--ease-out);
  }
  .pcard:hover .bar,
  .pcard:focus-within .bar {
    opacity: 1;
    pointer-events: auto;
  }
  .tool {
    display: grid;
    place-items: center;
    width: 20px;
    height: 20px;
    border: none;
    border-radius: 5px;
    background: var(--bg-soft);
    color: var(--muted);
    cursor: pointer;
  }
  .tool:hover {
    background: var(--line);
    color: var(--fg);
  }
  .tool svg {
    width: 11px;
    height: 11px;
  }
  .body {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    padding: 11px 12px 8px 15px;
    overflow: hidden;
  }
  /* 渲染态正文：markdown 排版，可滚动；任务勾选框直接可点 */
  .mdbody {
    flex: 1;
    /* 底栏固定常显，正文至少留一行、再多在内部滚动 */
    min-height: 1.6em;
    font-size: var(--font-size-section);
    line-height: 1.6;
    font-weight: 500;
    overflow-x: hidden;
    overflow-y: auto;
    overscroll-behavior: contain;
    scrollbar-width: thin;
    scrollbar-color: color-mix(in srgb, var(--fg) 18%, transparent) transparent;
    word-break: break-word;
    cursor: default;
  }
  /* 底栏：窄卡时间在上、会话在下；宽卡改成左右两列，首行对齐。
     底栏固定占位（flex:none），正文在剩余空间里滚动 */
  .foot {
    position: relative;
    z-index: 2;
    flex: none;
    display: flex;
    flex-direction: column;
    align-items: stretch;
  }
  .pcard.wide .foot {
    flex-direction: row;
    align-items: flex-start;
    justify-content: space-between;
    gap: 8px;
  }
  .foot :global(.links) {
    flex: none;
    align-self: flex-end;
    max-width: 100%;
    margin-top: 0;
    padding-top: 5px;
  }
  .pcard.wide .foot :global(.links) {
    max-width: 56%;
    padding-top: 6px;
  }
  /*
  卡面图片贴纸层：铺满卡片，平时不吃指针（不影响拖卡/点正文）；编辑中每张
  图可拖动、右下角可缩放。底栏/工具条抬 z 压在贴纸上面。
  */
  .imglayer {
    position: absolute;
    inset: 0;
    z-index: 1;
    overflow: hidden;
    border-radius: inherit;
    pointer-events: none;
  }
  .sticker {
    position: absolute;
    box-sizing: border-box;
    border: 1px solid var(--line);
    border-radius: 6px;
    background: var(--bg-soft);
    overflow: hidden;
    user-select: none;
  }
  .sticker img {
    display: block;
    width: 100%;
    height: 100%;
    object-fit: contain;
    pointer-events: none;
  }
  .pcard.editing .sticker {
    pointer-events: auto;
    cursor: grab;
    touch-action: none;
  }
  .pcard.editing .sticker.sel {
    z-index: 2;
    outline: 1.5px solid var(--accent);
    outline-offset: 1px;
    box-shadow: 0 8px 20px -10px rgb(0 0 0 / 35%);
    cursor: grab;
  }
  /* 选中贴纸的复制 / 移除按钮 / 缩放手柄 */
  .imgx,
  .imgcopy {
    position: absolute;
    top: 3px;
    display: grid;
    place-items: center;
    width: 16px;
    height: 16px;
    padding: 0;
    border: none;
    border-radius: 50%;
    background: color-mix(in srgb, var(--fg) 86%, transparent);
    color: var(--bg);
    font: inherit;
    font-size: 9px;
    line-height: 1;
    cursor: pointer;
  }
  .imgx {
    right: 3px;
  }
  .imgcopy {
    left: 3px;
  }
  .imgcopy svg {
    width: 10px;
    height: 10px;
  }
  .imggrip {
    position: absolute;
    right: 2px;
    bottom: 2px;
    width: 12px;
    height: 12px;
    padding: 0;
    border: 2px solid var(--bg);
    border-radius: 3px;
    background: var(--accent);
    cursor: nwse-resize;
    touch-action: none;
  }
  .imgmsg {
    position: absolute;
    left: 50%;
    bottom: 8px;
    z-index: 6;
    padding: 2px 8px;
    border-radius: 6px;
    background: var(--bg-invert);
    color: var(--fg-invert);
    font-size: var(--font-size-small);
    white-space: nowrap;
    transform: translateX(-50%);
    pointer-events: none;
    box-shadow: 0 4px 12px rgb(0 0 0 / 18%);
  }
  .mdbody :global(p) {
    margin: 0 0 3px;
  }
  .mdbody :global(p:last-child) {
    margin-bottom: 0;
  }
  .mdbody :global(h1),
  .mdbody :global(h2),
  .mdbody :global(h3),
  .mdbody :global(h4) {
    margin: 6px 0 2px;
    font-weight: 700;
    line-height: 1.35;
  }
  .mdbody :global(h1) {
    font-size: 1.35em;
  }
  .mdbody :global(h2) {
    font-size: 1.2em;
  }
  .mdbody :global(h3),
  .mdbody :global(h4) {
    font-size: 1.08em;
    font-weight: 600;
  }
  .mdbody :global(ul),
  .mdbody :global(ol) {
    margin: 1px 0 4px;
    padding-left: 1.25em;
  }
  .mdbody :global(li) {
    margin: 0;
  }
  .mdbody :global(input[type='checkbox']) {
    width: 12px;
    height: 12px;
    margin: 0 5px 0 0;
    accent-color: var(--accent);
    vertical-align: -2px;
    cursor: pointer;
  }
  /* 任务项去掉圆点，勾选框顶格 */
  .mdbody :global(li:has(> input[type='checkbox'])) {
    list-style: none;
    margin-left: -1.25em;
  }
  .mdbody :global(code) {
    padding: 0 4px;
    border-radius: 4px;
    background: var(--bg-soft);
    font-family: var(--font-mono);
    font-size: 0.9em;
  }
  .mdbody :global(pre) {
    margin: 4px 0;
    padding: 6px 8px;
    border-radius: 6px;
    background: var(--bg-soft);
    overflow-x: auto;
  }
  .mdbody :global(pre code) {
    padding: 0;
    background: none;
  }
  .mdbody :global(blockquote) {
    margin: 4px 0;
    padding-left: 8px;
    border-left: 2px solid var(--line-strong);
    color: var(--muted);
  }
  .mdbody :global(a) {
    color: var(--accent);
    text-decoration: none;
  }
  .mdbody :global(a:hover) {
    text-decoration: underline;
  }
  .mdbody :global(hr) {
    margin: 8px 0;
    border: none;
    border-top: 1px solid var(--line);
  }
  .edit-err {
    flex: none;
    margin: 2px 0 0;
    font-size: var(--font-size-small);
    color: var(--danger);
  }
  .ebar {
    position: relative;
    z-index: 2;
    flex: none;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px;
    padding-top: 5px;
    animation: ereveal var(--dur-fast) var(--ease-out) both;
  }
  .upstatus {
    font-size: var(--font-size-small);
    color: var(--muted);
  }
  @keyframes ereveal {
    from {
      opacity: 0;
      transform: translateY(2px);
    }
  }
  .duefield {
    flex: 1 1 110px;
    display: flex;
    align-items: center;
    gap: 4px;
    min-width: 0;
    font-size: var(--font-size-small);
    color: var(--muted);
  }
  .duefield span {
    flex: none;
  }
  .duefield input {
    min-width: 0;
    width: 100%;
    padding: 1px 4px;
    border: 1px solid var(--line);
    border-radius: 5px;
    background: var(--bg);
    color: var(--fg);
    font-family: inherit;
    font-size: 10px;
    color-scheme: inherit;
    outline: none;
    transition: border-color var(--dur-fast) var(--ease-out);
  }
  .duefield input:focus {
    border-color: var(--accent);
  }
  .xdue {
    flex: none;
    width: 16px;
    height: 16px;
    padding: 0;
    border: none;
    border-radius: 4px;
    background: transparent;
    color: var(--faint);
    font: inherit;
    font-size: 10px;
    line-height: 1;
    cursor: pointer;
  }
  .xdue:hover:not(:disabled) {
    background: var(--bg-soft);
    color: var(--fg);
  }
  .xdue:disabled {
    opacity: 0;
    pointer-events: none;
  }
  .ebtn {
    flex: none;
    padding: 1px 7px;
    border: 1px solid var(--line);
    border-radius: 5px;
    background: var(--bg);
    color: var(--muted);
    font: inherit;
    font-size: 10px;
    cursor: pointer;
  }
  .ebtn:hover {
    color: var(--fg);
    border-color: var(--line-strong);
  }
  .ebtn.cancel {
    margin-left: auto;
  }
  .ebtn.save {
    border-color: var(--fg);
    background: var(--fg);
    color: var(--bg);
    font-weight: 600;
  }
  .ebtn.save:hover {
    opacity: 0.9;
  }
  .ebtn.save:disabled {
    opacity: 0.5;
    cursor: default;
  }
  /* 固定时间信息：记于+已持续 / 截止（两行，等宽数字） */
  .times {
    flex: none;
    display: flex;
    flex-direction: column;
    padding-top: 6px;
    font-size: var(--font-size-small);
    line-height: 1.5;
    color: color-mix(in srgb, var(--fg) 52%, transparent);
    font-variant-numeric: tabular-nums;
  }
  .pcard.wide .times {
    flex: 1;
    min-width: 0;
  }
  .tline {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .tline.over {
    font-weight: 600;
  }
  /* 拖入图片的落点提示 / 上传失败提示（都不参与布局） */
  .drop-hint {
    position: absolute;
    inset: 4px;
    z-index: 4;
    display: grid;
    place-items: center;
    border: 2px dashed var(--accent);
    border-radius: 6px;
    background: color-mix(in srgb, var(--accent) 10%, transparent);
    color: var(--accent);
    font-size: var(--font-size-section);
    font-weight: 600;
    pointer-events: none;
  }
  .drop-err {
    position: absolute;
    left: 8px;
    right: 8px;
    bottom: 8px;
    z-index: 5;
    padding: 3px 8px;
    border-radius: 6px;
    background: color-mix(in srgb, var(--danger) 12%, var(--bg));
    color: var(--danger);
    font-size: var(--font-size-small);
    line-height: 1.4;
    pointer-events: none;
  }
  /* 右下角缩放手柄 */
  .grip {
    position: absolute;
    right: 0;
    bottom: 0;
    z-index: 3;
    width: 12px;
    height: 12px;
    cursor: nwse-resize;
    background: linear-gradient(135deg, transparent 50%, color-mix(in srgb, var(--fg) 16%, transparent) 50%);
    border-bottom-right-radius: 8px;
  }
  /* 删除就地确认 */
  .confirm {
    position: absolute;
    inset: 0;
    z-index: 10;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 8px;
    border-radius: 8px;
    background: color-mix(in srgb, var(--bg) 92%, transparent);
    backdrop-filter: blur(2px);
    font-size: var(--font-size-ui);
    cursor: default;
  }
  .confirm p {
    margin: 0;
    color: var(--muted);
  }
  .cbtns {
    display: flex;
    gap: 6px;
  }
  .cbtns button {
    border: 1px solid var(--line);
    border-radius: 6px;
    background: var(--bg);
    color: var(--fg);
    font: inherit;
    font-size: var(--font-size-small);
    padding: 2px 8px;
    cursor: pointer;
  }
  .cbtns button:hover {
    background: var(--bg-soft);
  }
  .cbtns .danger {
    color: var(--danger);
    border-color: color-mix(in srgb, var(--danger) 40%, var(--line));
  }
</style>
