<script lang="ts">
  /*
  完成时间线（日期轴）：从今天往过去铺开，左新右旧，无记录的日子也占
  位（空天只有一根刻度）。完成的卡片挂在日期轴下方（顶部色条 + 小夹子，
  像晾在绳上），悬停向上提起一整行（高度不变、压在左邻卡下面，长文点开
  当天看）；命中用几何热区（原位置 ∪ 上移位置），滚轮横向滚动、滚动条隐藏。

  拖拽改归属或当天顺序：卡片本体跟着指针走，目标日期挂在卡片下方；落到
  任何一天（含原天）都置为该天第一个，落到另一天时写 day（拖回真实完成
  日则删除）；拖回便签板 → 交父级还原成待办卡。落点判定带 7px 迟滞、状态
  只在下发目标变化时才切（提示不逐帧重播）。
  */
  import { onDestroy, onMount } from 'svelte'
  import type { PinRecord } from '../../lib/api'
  import { byDayOrder, dayKeyOf, dayOf, moveRecordDay } from '../../lib/pinboard.svelte'

  let {
    done,
    dayOpen = '',
    overBoard,
    onrestore,
    onexpand,
    ondaychange,
    onblank,
  }: {
    done: PinRecord[]
    dayOpen?: string
    overBoard?: (x: number, y: number) => boolean
    onrestore?: (rec: PinRecord, x: number, y: number) => void
    onexpand?: (day: string) => void
    ondaychange?: () => void
    onblank?: () => void // 点时间线空白（非卡片/日期标签）收起当天展开
  } = $props()

  const DAY_MS = 86400000
  const TC_W = 132
  const PITCH = 28
  /* 提起上移量（高度不变）：26 露出一整行正文；再高小夹子会顶出时间线裁切区 */
  const LIFT = 26
  const EMPTY_W = 26
  const GAP = 10
  const EDGE = 18

  let tlEl = $state<HTMLElement>()
  let peekId = $state('')
  let peekLock = 0

  function dayStartMs(key: string): number {
    const [y, m, d] = key.split('-').map(Number)
    return new Date(y, m - 1, d).getTime()
  }

  function labelOf(ts: number): string {
    const d = new Date(ts)
    const today = new Date()
    today.setHours(0, 0, 0, 0)
    const diff = Math.round((today.getTime() - d.getTime()) / DAY_MS)
    if (diff === 0) return '今天'
    if (diff === 1) return '昨天'
    if (diff === 2) return '前天'
    return `${d.getMonth() + 1}月${d.getDate()}日`
  }

  /* 天列表：今天 → 过去，跨度至少 24 天，覆盖最早归档日（上限 200 天） */
  const days = $derived.by(() => {
    const byDay = new Map<string, PinRecord[]>()
    for (const r of done) {
      const k = dayOf(r)
      const arr = byDay.get(k)
      if (arr) arr.push(r)
      else byDay.set(k, [r])
    }
    for (const arr of byDay.values()) arr.sort(byDayOrder)

    const today = new Date()
    today.setHours(0, 0, 0, 0)
    let earliest = today.getTime()
    for (const k of byDay.keys()) earliest = Math.min(earliest, dayStartMs(k))
    const span = Math.min(200, Math.max(24, Math.round((today.getTime() - earliest) / DAY_MS) + 1))

    const out: { key: string; label: string; ts: number; recs: PinRecord[]; left: number; width: number }[] = []
    let left = EDGE
    for (let i = 0; i < span; i++) {
      const d = new Date(today)
      d.setDate(d.getDate() - i)
      const key = dayKeyOf(d.getTime())
      const recs = byDay.get(key) ?? []
      const width = recs.length ? (recs.length - 1) * PITCH + TC_W : EMPTY_W
      out.push({ key, label: labelOf(d.getTime()), ts: d.getTime(), recs, left, width })
      left += width + GAP
    }
    return { list: out, total: left + EDGE }
  })

  function clearPeek() {
    peekId = ''
  }

  /*
  悬停抽出（扑克式）：卡片只上移、不变高，压在左侧（z 更高）的卡下面，
  露出一截即可；完整内容点开当天查看。
  命中用几何热区（原位置 ∪ 上移位置，offset 计算不受 transform 影响），
  按 DOM 顺序取最靠左（视觉层级最高）的一张——重叠处与看到的层级一致。
  */
  function hotZone(el: HTMLElement): { left: number; top: number; right: number; bottom: number } {
    const g = el.parentElement?.getBoundingClientRect()
    const left = (g?.left ?? 0) + el.offsetLeft
    const top = (g?.top ?? 0) + el.offsetTop - LIFT
    return { left, top, right: left + el.offsetWidth, bottom: top + LIFT + el.offsetHeight }
  }

  function onPeekMove(e: PointerEvent) {
    if (drag || Date.now() < peekLock) return
    const cards = tlEl ? Array.from(tlEl.querySelectorAll<HTMLElement>('.mini')) : []
    let found = ''
    for (const el of cards) {
      const z = hotZone(el)
      if (e.clientX >= z.left && e.clientX <= z.right && e.clientY >= z.top && e.clientY <= z.bottom) {
        found = el.dataset.rid ?? ''
        break
      }
    }
    peekId = found
  }

  /* ── 跨天拖拽：卡片本体跟着指针走，日期显示在卡片下方 ── */
  let drag = $state<{
    rec: PinRecord
    px: number
    py: number
    ox: number
    oy: number
    active: boolean
    /* 各日期组的横向区间（激活时量一次）：拖动期间组不动，落点判定
       不再逐帧 getBoundingClientRect 强制排版 */
    rects: { key: string; left: number; right: number }[]
  } | null>(null)
  let dragX = $state(0)
  let dragY = $state(0)
  let overDay = $state('')
  let suppressClick = false

  const dragRec = $derived(drag?.rec ?? null)
  const overLabel = $derived(days.list.find((d) => d.key === overDay)?.label ?? '')
  const onBoard = $derived(overBoard?.(dragX, dragY) ?? false)

  /* 拖拽监听挂在 window 上（不依赖指针捕获）：松手/取消/窗口失焦都一定
     能收尾，不会留下「幽灵卡一直跟着指针」的残留 */
  function startDrag(e: PointerEvent, rec: PinRecord) {
    if (e.button !== 0) return
    stopDragListeners()
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect()
    drag = {
      rec,
      px: e.clientX,
      py: e.clientY,
      ox: e.clientX - r.left,
      oy: e.clientY - r.top,
      active: false,
      rects: [],
    }
    dragX = e.clientX
    dragY = e.clientY
    /* 捕获 + window 监听双保险：指针出窗也尽量收到 pointerup，收不到时
       由 blur 兜底收尾 */
    try {
      ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
    } catch {
      /* 老浏览器/异常指针 id：忽略，window 监听仍可用 */
    }
    window.addEventListener('pointermove', onDragMove)
    window.addEventListener('pointerup', onDragUp)
    window.addEventListener('pointercancel', onDragCancel)
    window.addEventListener('blur', onDragCancel)
  }

  function stopDragListeners() {
    window.removeEventListener('pointermove', onDragMove)
    window.removeEventListener('pointerup', onDragUp)
    window.removeEventListener('pointercancel', onDragCancel)
    window.removeEventListener('blur', onDragCancel)
  }

  function onDragCancel() {
    finishDrag(null)
  }

  /* groupAtX 落点判定（7px 迟滞）：指针还在上一个落点附近就不切换。
     区间用激活时量好的 drag.rects，纯算术不做布局查询 */
  function groupAtX(x: number): string {
    const rects = drag?.rects ?? []
    if (overDay) {
      const prev = rects.find((g) => g.key === overDay)
      if (prev && x >= prev.left - 7 && x <= prev.right + 7) return overDay
    }
    let best = ''
    let bestD = 26
    for (const g of rects) {
      if (x >= g.left && x <= g.right) return g.key
      const d = Math.min(Math.abs(x - g.left), Math.abs(x - g.right))
      if (d < bestD) {
        bestD = d
        best = g.key
      }
    }
    return best
  }

  function onDragMove(e: PointerEvent) {
    if (!drag) return
    if (!drag.active) {
      if (Math.hypot(e.clientX - drag.px, e.clientY - drag.py) < 5) return
      drag.active = true
      drag.rects = tlEl
        ? Array.from(tlEl.querySelectorAll<HTMLElement>('.group')).map((el) => {
            const r = el.getBoundingClientRect()
            return { key: el.dataset.day ?? '', left: r.left, right: r.right }
          })
        : []
      suppressClick = true
      peekId = ''
    }
    dragX = e.clientX
    dragY = e.clientY
    const target = groupAtX(e.clientX)
    if (target !== overDay) overDay = target // 只在落点真变时切（提示不闪）
  }

  function onDragUp(e: PointerEvent) {
    finishDrag(e)
  }

  /* finishDrag 统一收尾：e=null（窗口失焦/组件销毁）只取消，不改归属 */
  function finishDrag(e: PointerEvent | null) {
    if (!drag) return
    stopDragListeners()
    const rec = drag.rec
    const wasActive = drag.active
    drag = null
    if (!wasActive) {
      suppressClick = false
      overDay = ''
      return
    }
    peekLock = Date.now() + 380 // 松手后短暂锁抽出，避免「默认抽出一张」一弹
    setTimeout(() => (suppressClick = false), 80)
    if (!e) {
      overDay = ''
      return
    }
    if (overBoard?.(e.clientX, e.clientY)) {
      overDay = ''
      onrestore?.(rec, e.clientX, e.clientY)
      return
    }
    const target = overDay
    overDay = ''
    if (target) {
      /* 落到任何一天（含原天）都置为该天第一个 */
      moveRecordDay(rec.id, target)
      ondaychange?.()
    }
  }

  function clickMini(rec: PinRecord) {
    if (suppressClick) return
    onexpand?.(dayOf(rec))
  }

  /* 悬停时间线滚轮 = 横向滚动轴（纵向滚轮也转成横向） */
  function onWheel(e: WheelEvent) {
    if (!tlEl) return
    const d = e.deltaY + e.deltaX
    if (!d) return
    e.preventDefault()
    tlEl.scrollLeft += d
  }

  /* 点空白（不是卡片、不是日期标签）收起当天展开 */
  function onDown(e: PointerEvent) {
    if (!(e.target as HTMLElement).closest('.mini, .daylabel')) onblank?.()
  }

  onMount(() => {
    /* 初次进入：把今天滚到左缘（时间线左新右旧） */
    if (tlEl) tlEl.scrollLeft = 0
  })

  onDestroy(stopDragListeners)
</script>

<div
  class="tl"
  bind:this={tlEl}
  style:--lift="{LIFT}px"
  role="group"
  aria-label="完成时间线"
  onpointermove={onPeekMove}
  onpointerleave={clearPeek}
  onpointerdown={onDown}
  onwheel={onWheel}
>
  <div class="track" style:width="{days.total}px">
    <div class="axis"></div>
    {#each days.list as d (d.key)}
      <div
        class="group"
        class:empty={!d.recs.length}
        class:open={dayOpen === d.key}
        class:over={overDay === d.key}
        data-day={d.key}
        style:left="{d.left}px"
        style:width="{d.width}px"
      >
        {#each d.recs as r, i (r.id)}
          <!-- 夹子/吊线单独一层：平时压在卡身下面，悬停提起时抬到最上面，
               这样提起的卡能盖住别家的夹子，但盖不住别家的卡身 -->
          <span
            class="hanger"
            class:lift={peekId === r.id}
            class:dragging={drag?.rec.id === r.id && drag.active}
            style:left="{i * PITCH + TC_W / 2}px"
            style:--tint={r.tint}
            aria-hidden="true"
          ></span>
          <div
            class="mini"
            class:peek={peekId === r.id}
            class:dragging={drag?.rec.id === r.id && drag.active}
            data-rid={r.id}
            style:left="{i * PITCH}px"
            style:z-index={d.recs.length - i}
            style:--tint={r.tint}
            onpointerdown={(e) => startDrag(e, r)}
            onclick={() => clickMini(r)}
            onkeydown={(e) => { if (e.key === 'Enter') clickMini(r) }}
            role="button"
            tabindex="0"
            title={`${r.text}\n点击展开当天 · 拖到其他天改归属，拖回板上变回待办`}
          >
            <p>{r.text || '（空）'}</p>
          </div>
        {/each}
        {#if d.recs.length}
          <button class="daylabel" onclick={() => onexpand?.(d.key)} title={`展开 ${d.label} 的完成记录`}>
            {d.label}
          </button>
        {:else}
          <span class="tick" title={d.label}></span>
        {/if}
      </div>
    {/each}
  </div>

  {#if drag?.active && dragRec}
    <div class="dragwrap" style:transform="translate({dragX - drag.ox}px, {dragY - drag.oy}px)">
      <div class="dragcard" style:--tint={dragRec.tint}>
        <p>{dragRec.text || '（空）'}</p>
      </div>
      <span class="dragday" class:on={!onBoard && !!overDay}>
        {onBoard ? '松手退回便签板' : overLabel || '拖到某一天'}
      </span>
    </div>
  {/if}
</div>

<style>
  .tl {
    flex: none;
    /* 轴下挂卡，轴上方留出提起一整行正文的高度 */
    height: 166px;
    overflow-x: auto;
    overflow-y: hidden;
    border-top: 1px solid var(--line);
    background: var(--bg);
    /* 滚轮横向滚动已覆盖操作，隐藏滚动条（浏览器原生滚动条在这里太扎眼） */
    scrollbar-width: none;
    user-select: none;
  }
  .tl::-webkit-scrollbar {
    display: none;
  }
  .track {
    position: relative;
    height: 100%;
  }
  /* 日期轴：一条贯穿全宽的细线，卡片挂在线的下方 */
  .axis {
    position: absolute;
    left: 0;
    right: 0;
    top: 32px;
    height: 1px;
    background: var(--line);
  }
  .group {
    position: absolute;
    top: 0;
    height: 100%;
  }
  .mini {
    position: absolute;
    top: 46px;
    width: 132px;
    height: 72px;
    box-sizing: border-box;
    display: flex;
    flex-direction: column;
    padding: 6px 9px;
    border: 1px solid var(--line);
    border-top: 3px solid var(--tint, #b291a6);
    border-radius: 4px 4px 7px 7px;
    background: var(--bg);
    color: var(--fg);
    box-shadow: 0 1px 2px rgb(0 0 0 / 5%);
    cursor: grab;
    touch-action: none;
    transition:
      transform 0.18s var(--ease-out),
      box-shadow 0.18s var(--ease-out);
  }
  /*
  夹子 + 吊线：单独一层，平时 z=0 压在卡身下面（提起的卡才能盖住别家的
  夹子）；悬停提起时跟着卡上移并抬到最上面，自家夹子仍露在卡顶。
  */
  .hanger {
    position: absolute;
    top: 32px;
    width: 7px;
    height: 14px;
    margin-left: -3.5px;
    z-index: 0;
    pointer-events: none;
    transition: transform 0.18s var(--ease-out);
  }
  /* 吊线：从日期轴垂到卡顶 */
  .hanger::before {
    content: '';
    position: absolute;
    left: 50%;
    top: 0;
    width: 1px;
    height: 100%;
    background: var(--line-strong);
    opacity: 0.5;
  }
  /* 小夹子：横跨日期轴、夹住吊线，颜色随卡片色条 */
  .hanger::after {
    content: '';
    position: absolute;
    left: 0;
    top: -4px;
    width: 7px;
    height: 9px;
    border-radius: 2px;
    background: color-mix(in srgb, var(--tint, #b291a6) 72%, var(--bg));
    box-shadow: inset 0 0 0 1px rgb(0 0 0 / 10%);
    transform: rotate(-10deg);
  }
  .hanger.lift {
    transform: translateY(calc(-1 * var(--lift)));
    z-index: 700;
  }
  .hanger.dragging {
    opacity: 0.3;
  }
  /* 提起：只上移、不变高；卡身不抬 z——左邻卡（层级更高）仍压在它上面，
     露出提起的那一截，像把衣服从绳上提起一角。夹子/吊线另见 .hanger */
  .mini.peek {
    transform: translateY(calc(-1 * var(--lift)));
    box-shadow: 0 12px 26px -12px rgb(0 0 0 / 38%);
  }
  /* 拖动中：原槽位淡出，卡片本体由 dragcard 跟着指针走 */
  .mini.dragging {
    opacity: 0.3;
    box-shadow: none;
    transition: none;
    cursor: grabbing;
  }
  .mini p {
    flex: 1;
    min-height: 0;
    margin: 0;
    font-size: var(--font-size-ui);
    line-height: 1.5;
    font-weight: 500;
    white-space: pre-wrap;
    word-break: break-word;
    overflow: hidden;
  }
  /* 空天：日期轴下方一根细刻度 */
  .tick {
    position: absolute;
    left: 50%;
    top: 32px;
    width: 1px;
    height: 14px;
    background: var(--line);
  }
  .daylabel {
    position: absolute;
    top: 122px;
    left: 0;
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 1px 3px;
    border: none;
    border-radius: 5px;
    background: transparent;
    color: var(--faint);
    font: inherit;
    font-size: var(--font-size-ui);
    white-space: nowrap;
    cursor: pointer;
  }
  .daylabel:hover,
  .group.open .daylabel {
    color: var(--fg);
    background: var(--bg-soft);
  }
  /* 拖拽落点那天的日期名点亮 */
  .group.over .daylabel {
    color: var(--accent);
    background: var(--accent-soft);
  }
  /* 拖拽中的卡片本体：按抓取点跟着指针（transform 走合成，不动排版），
     目标日期挂在卡片下方 */
  .dragwrap {
    position: fixed;
    left: 0;
    top: 0;
    z-index: 90;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 6px;
    pointer-events: none;
    will-change: transform;
  }
  .dragcard {
    width: 132px;
    height: 72px;
    box-sizing: border-box;
    display: flex;
    flex-direction: column;
    padding: 6px 9px;
    border: 1px solid var(--line);
    border-top: 3px solid var(--tint, #b291a6);
    border-radius: 4px 4px 7px 7px;
    background: var(--bg);
    color: var(--fg);
    box-shadow: 0 16px 32px -12px rgb(0 0 0 / 38%);
    overflow: hidden;
  }
  .dragcard p {
    flex: 1;
    min-height: 0;
    margin: 0;
    font-size: var(--font-size-ui);
    line-height: 1.5;
    font-weight: 500;
    white-space: pre-wrap;
    word-break: break-word;
    overflow: hidden;
  }
  /* 日期标签与轴下的日期名同一套叫法：今天 / 昨天 / 10月26日 */
  .dragday {
    padding: 1px 5px;
    border-radius: 5px;
    background: color-mix(in srgb, var(--bg) 82%, transparent);
    color: var(--muted);
    font-size: var(--font-size-ui);
    white-space: nowrap;
    box-shadow: 0 1px 4px rgb(0 0 0 / 10%);
  }
  .dragday.on {
    background: var(--accent-soft);
    color: var(--accent);
  }
</style>
