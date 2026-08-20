<script lang="ts">
  import { onMount } from 'svelte'

  /*
  魔法画板（对象模型）：元素存对象数组，静态层缓存到离屏 buf，
  拖动/笔画期间只叠加活动元素（rAF 节流）。
  工具：选择（拖动 / Del 删除 / 双击改字）、画笔、白笔、矩形、
  椭圆、箭头、文本。贴入图片作底图垫底；导出走离屏合成。
  */
  let {
    source = null,
    onDone,
    onClose,
  }: {
    source?: File | null
    onDone: (f: File) => void
    onClose: () => void
  } = $props()

  /* ── 常量与类型 ── */

  /* 画布逻辑尺寸：挂载时按容器 × dpr 自适应（cap 2048 保清晰与性能） */
  let W = 960
  let H = 600
  const colors = ['#0a0a0a', '#ffffff', '#2563eb', '#c0392b']
  const sizes = [2, 4, 9]
  const SEL_COLOR = '#2563eb'

  type Pt = { x: number; y: number }
  type Tool = 'select' | 'pen' | 'eraser' | 'rect' | 'ellipse' | 'arrow' | 'text'
  type ShapeKind = 'rect' | 'ellipse' | 'arrow'
  type El =
    | { id: number; kind: 'pen' | 'eraser'; color: string; size: number; pts: Pt[] }
    | { id: number; kind: ShapeKind; color: string; size: number; a: Pt; b: Pt }
    | { id: number; kind: 'text'; color: string; size: number; x: number; y: number; text: string }

  const tools: { key: Tool; label: string; icon: string }[] = [
    { key: 'select', label: '选择（拖动 / Del 删除 / 双击改字）', icon: '<path d="M4 3l7 17 2.5-7 7-2.5z"/>' },
    { key: 'pen', label: '画笔', icon: '<path d="M17 3a2.85 2.85 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5L17 3z"/>' },
    { key: 'eraser', label: '白笔（覆盖擦除）', icon: '<path d="M20 20H8L3.5 15.5a2 2 0 0 1 0-2.8l9.2-9.2a2 2 0 0 1 2.8 0l4.8 4.8a2 2 0 0 1 0 2.8L13 18.4"/><path d="M8.5 9.5l6 6"/>' },
    { key: 'rect', label: '矩形', icon: '<rect x="4" y="6" width="16" height="12" rx="1"/>' },
    { key: 'ellipse', label: '椭圆', icon: '<ellipse cx="12" cy="12" rx="8.5" ry="6.5"/>' },
    { key: 'arrow', label: '箭头', icon: '<path d="M5 19L19 5"/><path d="M11 5h8v8"/>' },
    { key: 'text', label: '文本', icon: '<path d="M5 6V4h14v2"/><path d="M12 4v16"/><path d="M9 20h6"/>' },
  ]

  /* ── 状态 ── */

  let canvas: HTMLCanvasElement | undefined = $state()
  let color = $state('#0a0a0a')
  let size = $state(4)
  let tool = $state<Tool>('pen')
  let elements = $state<El[]>([])
  let selected = $state<number | null>(null)
  let canUndo = $state(false)

  let textEdit = $state<{ x: number; y: number; id: number | null } | null>(null)
  let textValue = $state('')

  /* 非响应式：渲染层与交互中间态 */
  let baseImg: HTMLImageElement | null = null
  let nextId = 1
  let snapshots: El[][] = []
  let buf = document.createElement('canvas')
  let raf = 0
  let drawing = false
  let grab: { el: El; off: Pt } | null = null

  /* 类型谓词：显式收窄到形状元素（联合判别收窄在部分场景失效） */
  type ShapeEl = Extract<El, { kind: ShapeKind }>
  const isShape = (el: El): el is ShapeEl =>
    el.kind === 'rect' || el.kind === 'ellipse' || el.kind === 'arrow'

  /* 视图变换：canvasPx = (world - view) * zoom，滚轮缩放 / 空格或中键平移 */
  let zoom = 1
  let viewX = 0
  let viewY = 0
  let spaceHeld = false
  let panning: { x: number; y: number } | null = null

  /* ── 工具切换副作用 ── */

  function pickTool(t: Tool) {
    tool = t
    if (canvas) canvas.style.cursor = '' // 清除 select 工具的 inline cursor
  }

  /* ── 渲染管线 ── */

  function ctx2d() {
    return canvas!.getContext('2d')!
  }

  function bctx() {
    return buf.getContext('2d')!
  }

  /* paintTo 导出合成：白底 + 底图 + 元素（出界部分随画布矩形裁掉）。 */
  function paintTo(ctx: CanvasRenderingContext2D, els: El[]) {
    ctx.fillStyle = '#ffffff'
    ctx.fillRect(0, 0, W, H)
    if (baseImg) ctx.drawImage(baseImg, 0, 0, W, H)
    for (const el of els) drawEl(ctx, el)
  }

  /* rebuildBuf 重建静态层。buf 尺寸即画布尺寸，出界元素自然被裁——
  所见即所得：画布上看到的 = 导出的 = 发给 AI 的。 */
  function rebuildBuf(exclude: number | null = null) {
    const c = bctx()
    c.setTransform(1, 0, 0, 1, 0, 0)
    c.fillStyle = '#ffffff'
    c.fillRect(0, 0, W, H)
    if (baseImg) c.drawImage(baseImg, 0, 0, W, H)
    for (const el of elements) {
      if (el.id !== exclude) drawEl(c, el)
    }
  }

  /* doPaint 显示一帧：视图变换下画静态层 + 活动元素 + 选择框，
  全部 clip 在画布矩形内（绘制中出界即不可见）。 */
  function doPaint(active: El | null = null) {
    if (!canvas) return
    const ctx = ctx2d()
    ctx.setTransform(1, 0, 0, 1, 0, 0)
    ctx.clearRect(0, 0, W, H)
    ctx.setTransform(zoom, 0, 0, zoom, -viewX * zoom, -viewY * zoom)
    ctx.save()
    ctx.beginPath()
    ctx.rect(0, 0, W, H)
    ctx.clip()
    ctx.drawImage(buf, 0, 0)
    if (active) drawEl(ctx, active)
    const focus = active ?? (selected !== null ? (elements.find((e) => e.id === selected) ?? null) : null)
    if (focus) drawSelBox(ctx, focus)
    ctx.restore()
    /* 画布边界线（clip 外，屏幕 1px） */
    const px = W / canvas.getBoundingClientRect().width / zoom
    ctx.strokeStyle = '#c8c8c8'
    ctx.lineWidth = px
    ctx.strokeRect(0, 0, W, H)
    ctx.setTransform(1, 0, 0, 1, 0, 0)
  }

  /* schedulePaint rAF 节流：高频 move 每帧最多画一次。 */
  function schedulePaint(active: El | null = null) {
    if (raf) return
    raf = requestAnimationFrame(() => {
      raf = 0
      doPaint(active)
    })
  }

  /* refresh 全量刷新（选择/撤销/清空/文本等状态变化）。 */
  function refresh() {
    if (!canvas) return
    rebuildBuf()
    doPaint()
  }

  function textFont(s: number) {
    return `600 ${12 + s * 4}px system-ui, 'Segoe UI', 'Microsoft YaHei', sans-serif`
  }

  function drawEl(ctx: CanvasRenderingContext2D, el: El) {
    ctx.save()
    if (el.kind === 'pen' || el.kind === 'eraser') {
      ctx.strokeStyle = el.kind === 'eraser' ? '#ffffff' : el.color
      ctx.lineWidth = el.size
      ctx.lineCap = 'round'
      ctx.lineJoin = 'round'
      ctx.beginPath()
      el.pts.forEach((p, i) => (i ? ctx.lineTo(p.x, p.y) : ctx.moveTo(p.x, p.y)))
      if (el.pts.length === 1) ctx.lineTo(el.pts[0].x + 0.1, el.pts[0].y)
      ctx.stroke()
    } else if (el.kind === 'text') {
      ctx.fillStyle = el.color
      ctx.font = textFont(el.size)
      ctx.textBaseline = 'top'
      ctx.fillText(el.text, el.x, el.y)
    } else if (isShape(el)) {
      ctx.strokeStyle = el.color
      ctx.lineWidth = el.size
      ctx.lineCap = 'round'
      ctx.lineJoin = 'round'
      const x = Math.min(el.a.x, el.b.x)
      const y = Math.min(el.a.y, el.b.y)
      const w = Math.abs(el.b.x - el.a.x)
      const h = Math.abs(el.b.y - el.a.y)
      if (el.kind === 'rect') {
        ctx.strokeRect(x, y, w, h)
      } else if (el.kind === 'ellipse') {
        ctx.beginPath()
        ctx.ellipse(x + w / 2, y + h / 2, w / 2, h / 2, 0, 0, Math.PI * 2)
        ctx.stroke()
      } else {
        ctx.beginPath()
        ctx.moveTo(el.a.x, el.a.y)
        ctx.lineTo(el.b.x, el.b.y)
        ctx.stroke()
        const ang = Math.atan2(el.b.y - el.a.y, el.b.x - el.a.x)
        const head = 8 + el.size * 2
        ctx.beginPath()
        ctx.moveTo(el.b.x, el.b.y)
        ctx.lineTo(el.b.x - head * Math.cos(ang - Math.PI / 7), el.b.y - head * Math.sin(ang - Math.PI / 7))
        ctx.moveTo(el.b.x, el.b.y)
        ctx.lineTo(el.b.x - head * Math.cos(ang + Math.PI / 7), el.b.y - head * Math.sin(ang + Math.PI / 7))
        ctx.stroke()
      }
    }
    ctx.restore()
  }

  function drawSelBox(ctx: CanvasRenderingContext2D, el: El) {
    const b = boundsOf(ctx, el)
    if (!b) return
    /* 视图变换下保持屏幕 1px 线宽 */
    const px = canvas ? W / canvas.getBoundingClientRect().width / zoom : 1
    ctx.save()
    ctx.strokeStyle = SEL_COLOR
    ctx.setLineDash([5 * px, 4 * px])
    ctx.lineWidth = px
    ctx.strokeRect(b.x - 5 * px, b.y - 5 * px, b.w + 10 * px, b.h + 10 * px)
    ctx.restore()
  }

  /* ── 几何：包围盒与命中 ── */

  function boundsOf(ctx: CanvasRenderingContext2D, el: El): { x: number; y: number; w: number; h: number } | null {
    if (el.kind === 'pen' || el.kind === 'eraser') {
      const xs = el.pts.map((p) => p.x)
      const ys = el.pts.map((p) => p.y)
      const pad = el.size / 2 + 1
      return { x: Math.min(...xs) - pad, y: Math.min(...ys) - pad, w: Math.max(...xs) - Math.min(...xs) + pad * 2, h: Math.max(...ys) - Math.min(...ys) + pad * 2 }
    }
    if (el.kind === 'text') {
      ctx.font = textFont(el.size)
      const m = ctx.measureText(el.text)
      const f = 12 + el.size * 4
      return { x: el.x, y: el.y, w: m.width, h: f * 1.2 }
    }
    if (isShape(el)) {
      return {
        x: Math.min(el.a.x, el.b.x) - el.size / 2,
        y: Math.min(el.a.y, el.b.y) - el.size / 2,
        w: Math.abs(el.b.x - el.a.x) + el.size,
        h: Math.abs(el.b.y - el.a.y) + el.size,
      }
    }
    return null
  }

  function hit(p: Pt, tol = 4): El | null {
    if (!canvas) return null
    const ctx = ctx2d()
    for (let i = elements.length - 1; i >= 0; i--) {
      const b = boundsOf(ctx, elements[i])
      if (b && p.x >= b.x - tol && p.x <= b.x + b.w + tol && p.y >= b.y - tol && p.y <= b.y + b.h + tol) {
        return elements[i]
      }
    }
    return null
  }

  /* 滚轮缩放（锚定光标）。 */
  function onWheel(e: WheelEvent) {
    if (!canvas) return
    e.preventDefault()
    const r = canvas.getBoundingClientRect()
    const cx = ((e.clientX - r.left) * W) / r.width
    const cy = ((e.clientY - r.top) * H) / r.height
    const world = { x: cx / zoom + viewX, y: cy / zoom + viewY }
    zoom = Math.min(Math.max(zoom * (e.deltaY < 0 ? 1.15 : 1 / 1.15), 0.1), 8)
    viewX = world.x - cx / zoom
    viewY = world.y - cy / zoom
    schedulePaint()
  }

  /* fitView 适应窗口（重置视图）。 */
  function fitView() {
    zoom = 1
    viewX = 0
    viewY = 0
    schedulePaint()
  }

  /* ── 交互 ── */

  /* 事件坐标 → 世界坐标（逆视图变换）。 */
  function pos(e: PointerEvent | MouseEvent): Pt {
    const r = canvas!.getBoundingClientRect()
    const cx = ((e.clientX - r.left) * W) / r.width
    const cy = ((e.clientY - r.top) * H) / r.height
    return { x: cx / zoom + viewX, y: cy / zoom + viewY }
  }

  function down(e: PointerEvent) {
    if (!canvas) return
    if (e.button === 1 || spaceHeld) {
      e.preventDefault()
      canvas.setPointerCapture(e.pointerId)
      panning = { x: e.clientX, y: e.clientY }
      return
    }
    if (e.button !== 0) return
    const p = pos(e)
    if (tool === 'text') {
      commitText()
      textEdit = { x: p.x, y: p.y, id: null }
      textValue = ''
      return
    }
    canvas.setPointerCapture(e.pointerId)
    if (tool === 'select') {
      selectDown(p)
      return
    }
    drawDown(p)
  }

  function selectDown(p: Pt) {
    const el = hit(p)
    selected = el ? el.id : null
    if (!el) {
      refresh()
      return
    }
    pushSnapshot()
    const b = boundsOf(ctx2d(), el)!
    grab = { el, off: { x: p.x - b.x, y: p.y - b.y } }
    rebuildBuf(el.id) // 被拖元素移出静态层，move 时只叠加它
    doPaint(grab.el)
  }

  function drawDown(p: Pt) {
    pushSnapshot()
    drawing = true
    const el: El =
      tool === 'pen' || tool === 'eraser'
        ? { id: nextId++, kind: tool, color, size, pts: [p] }
        : { id: nextId++, kind: tool as ShapeKind, color, size, a: p, b: p }
    elements = [...elements, el]
    selected = null
    schedulePaint(el)
  }

  function move(e: PointerEvent) {
    if (!canvas) return
    if (panning) {
      const r = canvas.getBoundingClientRect()
      const dx = ((e.clientX - panning.x) * W) / r.width / zoom
      const dy = ((e.clientY - panning.y) * H) / r.height / zoom
      viewX -= dx
      viewY -= dy
      panning = { x: e.clientX, y: e.clientY }
      schedulePaint()
      return
    }
    const p = pos(e)
    if (drawing) {
      const el = elements[elements.length - 1]
      if (el && (el.kind === 'pen' || el.kind === 'eraser')) el.pts.push(p)
      else if (el && isShape(el)) el.b = p
      schedulePaint(el)
      return
    }
    if (grab) {
      const b = boundsOf(ctx2d(), grab.el)
      if (b) translate(grab.el, p.x - b.x - grab.off.x, p.y - b.y - grab.off.y)
      schedulePaint(grab.el)
      return
    }
    if (tool === 'select') canvas.style.cursor = hit(p) ? 'move' : ''
  }

  function up() {
    if (panning) {
      panning = null
      return
    }
    if (drawing) {
      const el = elements[elements.length - 1]
      if (el) drawEl(bctx(), el) // 笔画定稿进静态层
      doPaint()
    } else if (grab) {
      rebuildBuf() // 拖动定稿：静态层还原全量
      doPaint()
    }
    drawing = false
    grab = null
  }

  function translate(el: El, dx: number, dy: number) {
    if (el.kind === 'pen' || el.kind === 'eraser') {
      el.pts = el.pts.map((p) => ({ x: p.x + dx, y: p.y + dy }))
    } else if (el.kind === 'text') {
      el.x += dx
      el.y += dy
    } else if (isShape(el)) {
      el.a = { x: el.a.x + dx, y: el.a.y + dy }
      el.b = { x: el.b.x + dx, y: el.b.y + dy }
    }
  }

  function dblClick(e: MouseEvent) {
    if (tool !== 'select' || !canvas) return
    const el = hit(pos(e))
    if (el && el.kind === 'text') {
      selected = el.id
      textEdit = { x: el.x, y: el.y, id: el.id }
      textValue = el.text
    }
  }

  /* ── 文本编辑 ── */

  function commitText() {
    const t = textEdit
    if (!t) return
    const v = textValue
    textEdit = null
    if (!v.trim()) return
    pushSnapshot()
    if (t.id !== null) {
      const el = elements.find((e) => e.id === t.id)
      if (el && el.kind === 'text') el.text = v
    } else {
      elements = [...elements, { id: nextId++, kind: 'text', color, size, x: t.x, y: t.y, text: v }]
    }
    refresh()
  }

  function textInputStyle() {
    if (!canvas || !textEdit) return ''
    const scale = (canvas.getBoundingClientRect().width / W) * zoom
    return `left:${(textEdit.x - viewX) * scale}px;top:${(textEdit.y - viewY) * scale}px;font-size:${(12 + size * 4) * scale}px;color:${color}`
  }

  /* ── 历史 ── */

  function pushSnapshot() {
    /* $state 深代理无法 structuredClone，先 snapshot 摘代理 */
    snapshots.push($state.snapshot(elements))
    if (snapshots.length > 40) snapshots.shift()
    canUndo = true
  }

  function undo() {
    const snap = snapshots.pop()
    canUndo = snapshots.length > 0
    if (!snap) return
    elements = structuredClone(snap) // snapshot 不可变，克隆出可写副本
    selected = null
    refresh()
  }

  function clearAll() {
    if (!elements.length) return
    pushSnapshot()
    elements = []
    selected = null
    refresh()
  }

  function delSelected() {
    if (selected === null) return
    pushSnapshot()
    elements = elements.filter((e) => e.id !== selected)
    selected = null
    refresh()
  }

  /* ── 导出与生命周期 ── */

  function finish() {
    const off = document.createElement('canvas')
    off.width = W
    off.height = H
    paintTo(off.getContext('2d')!, elements)
    off.toBlob((blob) => {
      if (blob) onDone(new File([blob], `画板-${Date.now()}.png`, { type: 'image/png' }))
    }, 'image/png')
  }

  async function init() {
    if (source) {
      const url = URL.createObjectURL(source)
      try {
        const img = new Image()
        img.src = url
        await new Promise((r) => (img.onload = r))
        baseImg = img
        /* 画布取图片原始尺寸（等比 cap 3072），导出保分辨率 */
        const s = Math.min(3072 / img.width, 3072 / img.height, 1)
        W = Math.max(1, Math.round(img.width * s))
        H = Math.max(1, Math.round(img.height * s))
      } finally {
        URL.revokeObjectURL(url)
      }
    }
    if (canvas) {
      canvas.width = W
      canvas.height = H
    }
    buf.width = W
    buf.height = H
    elements = []
    selected = null
    snapshots = []
    canUndo = false
    fitView()
    refresh()
  }

  /* 挂载即初始化一次。不可用 $effect：refresh 读 elements/selected，
  会把交互写入反哺成重新 init，画布被反复清空。 */
  onMount(() => {
    const wrap = canvas?.parentElement
    if (wrap && canvas) {
      const dpr = Math.min(window.devicePixelRatio || 1, 2)
      W = Math.min(Math.round(wrap.clientWidth * dpr), 2048) || W
      H = Math.min(Math.round(wrap.clientHeight * dpr), 2048) || H
      canvas.width = W
      canvas.height = H
    }
    buf.width = W
    buf.height = H
    void init()
  })

  function onKey(e: KeyboardEvent) {
    if (e.code === 'Space' && !textEdit) {
      e.preventDefault()
      spaceHeld = true
      return
    }
    if (textEdit) {
      if (e.key === 'Escape') textEdit = null
      return
    }
    if (e.key === 'Escape') {
      onClose()
      return
    }
    if ((e.key === 'Delete' || e.key === 'Backspace') && selected !== null) {
      e.preventDefault()
      delSelected()
    }
    if (e.key === 'z' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault()
      undo()
    }
  }

  function onKeyUp(e: KeyboardEvent) {
    if (e.code === 'Space') spaceHeld = false
  }
</script>

<svelte:window onkeydown={onKey} onkeyup={onKeyUp} />

<div class="overlay" role="dialog" aria-label="魔法画板">
  <div class="board">
    <header>
      <h2>魔法画板</h2>
      <span class="sub">{source ? '标注后添加到聊天' : '画点什么，直接发出去'}</span>
      <button class="fit" onclick={fitView} title="适应窗口">适应窗口</button>
      <button class="close" onclick={onClose} title="关闭">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
          <path d="M6 6l12 12M18 6L6 18" />
        </svg>
      </button>
    </header>

    <div class="canvas-wrap">
      <canvas
        bind:this={canvas}
        class:panning={spaceHeld || !!panning}
        onpointerdown={down}
        onpointermove={move}
        onpointerup={up}
        onpointercancel={up}
        ondblclick={dblClick}
        onwheel={onWheel}
      ></canvas>
      {#if textEdit}
        <input
          class="text-input"
          style={textInputStyle()}
          placeholder="输入文字…"
          bind:value={textValue}
          onkeydown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              commitText()
            }
          }}
          onblur={commitText}
        />
      {/if}
    </div>

    <footer>
      <div class="tools">
        <div class="group">
          {#each colors as c (c)}
            <button
              class="swatch"
              class:active={color === c && tool !== 'eraser'}
              style="background:{c}"
              onclick={() => {
                color = c
                if (tool === 'select' || tool === 'eraser') pickTool('pen')
              }}
              title={c}
            ></button>
          {/each}
        </div>
        <div class="group">
          {#each sizes as s (s)}
            <button class="size" class:active={size === s} onclick={() => (size = s)} title="粗细 {s}">
              <i style="width:{4 + s * 2}px;height:{4 + s * 2}px"></i>
            </button>
          {/each}
        </div>
        <div class="group">
          {#each tools as t (t.key)}
            <button class="tool" class:active={tool === t.key} onclick={() => pickTool(t.key)} title={t.label}>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                {@html t.icon}
              </svg>
            </button>
          {/each}
        </div>
        <div class="group">
          <button class="tool" disabled={!canUndo} onclick={undo} title="撤销（Ctrl+Z）">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
              <path d="M9 14L4 9l5-5" />
              <path d="M4 9h10a6 6 0 0 1 0 12h-3" />
            </svg>
          </button>
          <button class="tool" onclick={clearAll} title="清空">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
              <path d="M3 6h18M8 6V4a1 1 0 0 1 1-1h6a1 1 0 0 1 1 1v2M6 6l1 14a1 1 0 0 0 1 1h8a1 1 0 0 0 1-1l1-14" />
            </svg>
          </button>
        </div>
      </div>
      <button class="done" onclick={finish}>添加到聊天</button>
    </footer>
  </div>
</div>

<style>
  .overlay {
    position: fixed;
    inset: 0;
    z-index: 100;
    display: grid;
    place-items: center;
    background: rgb(0 0 0 / 42%);
    backdrop-filter: blur(3px);
    animation: fade var(--dur-fast) var(--ease-out) both;
  }
  @keyframes fade {
    from {
      opacity: 0;
    }
    to {
      opacity: 1;
    }
  }
  /* 近全屏：屏幕多大画板多大 */
  .board {
    display: flex;
    flex-direction: column;
    gap: 12px;
    width: calc(100vw - 48px);
    height: calc(100vh - 48px);
    max-width: 1920px;
    background: var(--bg);
    border-radius: 18px;
    padding: 18px 18px 16px;
    box-shadow: 0 24px 64px rgb(0 0 0 / 24%);
    animation: pop var(--dur-in) var(--ease-out) both;
  }
  @keyframes pop {
    from {
      opacity: 0;
      transform: translateY(10px) scale(0.98);
    }
    to {
      opacity: 1;
      transform: translateY(0) scale(1);
    }
  }
  header {
    display: flex;
    align-items: baseline;
    gap: 10px;
  }
  h2 {
    font-size: 15px;
    font-weight: 700;
  }
  .sub {
    font-size: 11.5px;
    color: var(--faint);
    flex: 1;
  }
  .close {
    display: grid;
    place-items: center;
    width: 28px;
    height: 28px;
    border: none;
    background: transparent;
    color: var(--muted);
    border-radius: 8px;
  }
  .close:hover {
    background: var(--line);
    color: var(--fg);
  }
  .close svg {
    width: 14px;
    height: 14px;
  }
  .fit {
    border: 1px solid var(--line);
    background: transparent;
    color: var(--muted);
    border-radius: 8px;
    padding: 4px 12px;
    font-size: 11.5px;
  }
  .fit:hover {
    border-color: var(--line-strong);
    color: var(--fg);
  }
  canvas.panning {
    cursor: grab !important;
  }
  .canvas-wrap {
    position: relative;
    flex: 1;
    min-height: 0;
    display: grid;
    place-items: center;
    border: 1px solid var(--line);
    border-radius: 12px;
    overflow: hidden;
    background: var(--bg-soft);
  }
  /* 等比 contain：内在尺寸比例与逻辑分辨率一致，不会变形 */
  canvas {
    display: block;
    max-width: 100%;
    max-height: 100%;
    touch-action: none;
    background: #fff;
    cursor: crosshair;
  }
  .text-input {
    position: absolute;
    min-width: 60px;
    max-width: 80%;
    border: 1px dashed var(--accent);
    background: transparent;
    outline: none;
    padding: 1px 4px;
    font-family: inherit;
    font-weight: 600;
    line-height: 1.2;
  }
  footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }
  .tools {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }
  .group {
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 4px;
    border: 1px solid var(--line);
    border-radius: 10px;
  }
  .swatch {
    width: 22px;
    height: 22px;
    border-radius: 6px;
    border: 1px solid var(--line);
    padding: 0;
  }
  .swatch.active {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
  .size {
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    border: none;
    background: transparent;
    border-radius: 7px;
    padding: 0;
  }
  .size i {
    display: block;
    border-radius: 50%;
    background: var(--fg);
  }
  .size.active {
    background: var(--bg-invert);
  }
  .size.active i {
    background: var(--fg-invert);
  }
  .tool {
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    border: none;
    background: transparent;
    border-radius: 7px;
    color: var(--muted);
  }
  .tool svg {
    width: 14px;
 height: 14px;
  }
  .tool:hover {
    background: var(--line);
    color: var(--fg);
  }
  .tool.active {
    background: var(--bg-invert);
    color: var(--fg-invert);
  }
  .tool:disabled {
    opacity: 0.3;
    cursor: default;
  }
  .done {
    border: 1px solid var(--line-strong);
    background: var(--bg-invert);
    color: var(--fg-invert);
    border-radius: 10px;
    padding: 9px 20px;
    font-size: 13px;
    font-weight: 600;
  }
</style>
