<script lang="ts">
  /*
  便签小窗（?pins=1 窗口，宠物便签球打开）：不拉主客户端的 bootstrap/SSE，
  只轮询 /api/pinboard 展示板上待办；没手工排过时按截止从紧到松，拖过之后
  按 ord 排（手动顺序持久化进 pinboard.json）。点条目 → 主进程亮主窗并定
  位到那张卡，点右侧「完成」直接归档到时间线，点右上「打开便签板」只切到
  便签页。没有待办/加载失败都有占位。

  拖动排序时卡片本体浮在指针下，原位置留出等高的空档（不画插入线）；空档
  位置按拖动开始时的条目中线判定，列表重排不会带着落点抖。
  */
  import { onMount } from 'svelte'
  import type { PinCard } from '../../lib/api'
  import {
    board,
    completeCard,
    ensurePinboard,
    pinboardBusy,
    reloadPinboard,
    reorderCards,
    saveNow,
  } from '../../lib/pinboard.svelte'
  import { DUE_COLOR, dueLevel } from '../../lib/pinDue'

  const ez = (window as any).ez

  let now = $state(Date.now())
  let listEl = $state<HTMLElement>()
  let drag = $state<{
    id: string
    px: number
    py: number
    dx: number
    dy: number
    ox: number
    oy: number
    w: number
    h: number
    active: boolean
    /* 各条中线的内容坐标（拖动开始时记一次）：空档插入后条目位置会变，
       但落点判定只认这份基准，不会来回跳 */
    mids: Map<string, number> | null
  } | null>(null)
  let insertAt = $state(-1)
  let suppressClick = false

  onMount(() => {
    void ensurePinboard()
    const t = setInterval(() => {
      /* 拖动中/本地有未落盘改动时不重拉：避免列表在手下换序 */
      if (!drag && !pinboardBusy()) void reloadPinboard()
      now = Date.now()
    }, 3000)
    return () => {
      clearInterval(t)
      stopDragListeners()
    }
  })

  /*
  手工顺序（ord）优先；没排过的卡（新记的）排在已排的后面，彼此按截止
  从紧到松、同档新记的在前。整窗都没排过时就是原来的纯截止排序。
  */
  const items = $derived.by(() =>
    [...board.cards].sort((a, b) => {
      if (a.ord !== undefined || b.ord !== undefined) {
        if (a.ord === undefined) return 1
        if (b.ord === undefined) return -1
        if (a.ord !== b.ord) return a.ord - b.ord
      }
      const ad = a.due ?? Number.MAX_SAFE_INTEGER
      const bd = b.due ?? Number.MAX_SAFE_INTEGER
      if (ad !== bd) return ad - bd
      return b.created - a.created
    }),
  )
  const overCount = $derived(board.cards.filter((c) => dueLevel(c.due, now) === 'over').length)

  /* 拖动中：列表里去掉被拖的那条，其余条目之间插一个等高的空档 */
  const others = $derived.by(() => {
    const d = drag
    return d?.active ? items.filter((c) => c.id !== d.id) : items
  })
  const dragging = $derived.by(() => {
    const d = drag
    if (!d?.active) return null
    const c = items.find((x) => x.id === d.id)
    return c ? { c, d } : null
  })

  function fmtShort(ts: number): string {
    return new Date(ts).toLocaleString('zh-CN', { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  }

  function dur(ms: number): string {
    const m = Math.max(0, Math.floor(ms / 60000))
    if (m < 60) return `${m} 分钟`
    const h = Math.floor(m / 60)
    if (h < 24) return `${h} 小时`
    return `${Math.floor(h / 24)} 天`
  }

  function dueColor(c: PinCard): string {
    const lv = dueLevel(c.due, now)
    return lv ? DUE_COLOR[lv] : 'var(--faint)'
  }

  function dueText(c: PinCard): string {
    return c.due ? `截止 ${fmtShort(c.due)}` : '未设置截止'
  }

  function dueTitle(c: PinCard): string {
    if (!c.due) return '未设置截止时间'
    return c.due <= now ? `已超期 ${dur(now - c.due)}` : `还剩 ${dur(c.due - now)}`
  }

  /* 小列表不渲染 markdown，只去掉标题/列表/待办前缀 */
  function plain(md: string): string {
    return md
      .replace(/^#{1,6}\s+/gm, '')
      .replace(/^\s*[-*+]\s+(?:\[[ xX]?\]\s+)?/gm, '')
      .replace(/[*_`>]/g, '')
      .trim()
  }

  function open(c: PinCard): void {
    if (suppressClick) return
    ez?.pin?.jump?.(c.id)
  }

  function complete(c: PinCard, e: MouseEvent): void {
    e.stopPropagation()
    suppressClick = true
    completeCard(c.id)
    void saveNow() // 小窗随时可能被关，完成立即落盘
    window.setTimeout(() => (suppressClick = false), 80)
  }

  /* ── 上下拖动排序：卡片浮在指针下，原位置留空档 ── */

  function startDrag(e: PointerEvent, c: PinCard): void {
    if (e.button !== 0) return
    if ((e.target as HTMLElement).closest('button')) return
    stopDragListeners()
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect()
    drag = {
      id: c.id,
      px: e.clientX,
      py: e.clientY,
      dx: e.clientX,
      dy: e.clientY,
      ox: e.clientX - r.left,
      oy: e.clientY - r.top,
      w: r.width,
      h: r.height,
      active: false,
      mids: null,
    }
    /* window 监听不依赖指针捕获：拖出窗口/失焦也能收尾 */
    window.addEventListener('pointermove', onDragMove)
    window.addEventListener('pointerup', onDragUp)
    window.addEventListener('pointercancel', onDragCancel)
    window.addEventListener('blur', onDragCancel)
  }

  function stopDragListeners(): void {
    window.removeEventListener('pointermove', onDragMove)
    window.removeEventListener('pointerup', onDragUp)
    window.removeEventListener('pointercancel', onDragCancel)
    window.removeEventListener('blur', onDragCancel)
  }

  /* 激活时记下各条中线的内容坐标（含当时滚动量）；之后列表插空档挪动
     条目，落点仍按这份基准算，不会抖 */
  function captureMids(): Map<string, number> {
    const m = new Map<string, number>()
    if (!listEl) return m
    const lr = listEl.getBoundingClientRect()
    listEl.querySelectorAll<HTMLElement>('.item').forEach((el, i) => {
      if (!items[i]) return
      const r = el.getBoundingClientRect()
      m.set(items[i].id, r.top + r.height / 2 - lr.top + listEl!.scrollTop)
    })
    return m
  }

  function insertIndex(clientY: number): number {
    const d = drag
    if (!d?.mids || !listEl) return insertAt
    const top = listEl.getBoundingClientRect().top - listEl.scrollTop
    let idx = 0
    for (const c of items) {
      if (c.id === d.id) continue
      const mid = d.mids.get(c.id)
      if (mid !== undefined && clientY > top + mid) idx++
    }
    return idx
  }

  function onDragMove(e: PointerEvent): void {
    if (!drag || !listEl) return
    if (!drag.active) {
      if (Math.hypot(e.clientX - drag.px, e.clientY - drag.py) < 5) return
      drag.mids = captureMids()
      drag.active = true
      suppressClick = true
    }
    drag.dx = e.clientX
    drag.dy = e.clientY
    /* 靠近列表上下缘时自动滚动，长列表也能拖到视野外 */
    const lr = listEl.getBoundingClientRect()
    if (e.clientY < lr.top + 24) listEl.scrollTop -= 8
    else if (e.clientY > lr.bottom - 24) listEl.scrollTop += 8
    insertAt = insertIndex(e.clientY)
  }

  function onDragUp(e: PointerEvent): void {
    finishDrag(e)
  }

  function onDragCancel(): void {
    finishDrag(null)
  }

  /* finishDrag 统一收尾：e=null（失焦/销毁）只取消，不改顺序 */
  function finishDrag(e: PointerEvent | null): void {
    if (!drag) return
    const d = drag
    const at = d.active && e ? insertIndex(e.clientY) : -1
    stopDragListeners()
    drag = null
    insertAt = -1
    if (!d.active) return
    window.setTimeout(() => (suppressClick = false), 80)
    if (at < 0) return
    const ids = items.map((c) => c.id).filter((id) => id !== d.id)
    ids.splice(at, 0, d.id)
    reorderCards(ids)
  }
</script>

{#snippet rowBody(c: PinCard)}
  <i class="tint" style:background={c.tint}></i>
  <span class="body">
    <span class="text">{plain(c.text) || (c.images?.length ? '（图片）' : '（空便签）')}</span>
    <span class="meta">
      <span class="due" style:color={dueColor(c)}>{dueText(c)}</span>
      {#if c.images?.length}<span class="nimg">{c.images.length} 张图</span>{/if}
    </span>
  </span>
  {#if c.images?.length}
    <img class="thumb" src={c.images[0]} alt="" loading="lazy" draggable="false" />
  {/if}
{/snippet}

<div class="wrap">
  <header>
    <h1>便签</h1>
    <span class="count">
      {board.cards.length} 件待办{overCount ? ` · ${overCount} 件超期` : ''}
    </span>
    <button class="all" onclick={() => ez?.pin?.jump?.('')} title="打开主客户端的便签页">
      打开便签板
    </button>
  </header>

  {#if board.err}<p class="err" title={board.err}>{board.err}</p>{/if}

  <div class="list" bind:this={listEl}>
    {#if !board.loaded}
      <p class="empty">{board.loading ? '便签加载中…' : '便签加载失败'}</p>
    {:else if !items.length}
      <p class="empty">板上暂无待办</p>
    {:else}
      {#each others as c, i (c.id)}
        {#if drag?.active && insertAt === i}
          <div class="gap" style:height="{drag.h}px"></div>
        {/if}
        <div
          class="item"
          role="button"
          tabindex="0"
          onclick={() => open(c)}
          onkeydown={(e) => {
            if (e.target !== e.currentTarget) return
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault()
              open(c)
            }
          }}
          onpointerdown={(e) => startDrag(e, c)}
          title={`${c.text || '（空便签）'}\n${dueTitle(c)}\n拖动排序 · 点击打开便签板并定位`}
        >
          {@render rowBody(c)}
          <button
            class="done"
            onclick={(e) => complete(c, e)}
            title="完成（归档到时间线）"
            aria-label="完成这张便签"
          >
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round">
              <path d="M5 12.5l4.5 4.5L19 7" />
            </svg>
          </button>
        </div>
      {/each}
      {#if drag?.active && insertAt >= others.length}
        <div class="gap" style:height="{drag.h}px"></div>
      {/if}
    {/if}
  </div>
</div>

{#if dragging}
  <div
    class="dragrow"
    style:left="{dragging.d.dx - dragging.d.ox}px"
    style:top="{dragging.d.dy - dragging.d.oy}px"
    style:width="{dragging.d.w}px"
  >
    <div class="item clone">
      {@render rowBody(dragging.c)}
      <span class="done-space" aria-hidden="true"></span>
    </div>
  </div>
{/if}

<style>
  .wrap {
    display: flex;
    flex-direction: column;
    gap: 8px;
    height: 100vh;
    box-sizing: border-box;
    padding: 12px;
    background: var(--bg-soft);
  }
  header {
    flex: none;
    display: flex;
    align-items: baseline;
    gap: 8px;
    min-width: 0;
  }
  h1 {
    font-size: var(--font-size-section);
    font-weight: 700;
    white-space: nowrap;
  }
  .count {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--font-size-ui);
    color: var(--muted);
    font-variant-numeric: tabular-nums;
  }
  .all {
    flex: none;
    margin-left: auto;
    padding: 2px 8px;
    border: 1px solid var(--line);
    border-radius: 7px;
    background: var(--bg);
    color: var(--muted);
    font: inherit;
    font-size: var(--font-size-small);
    cursor: pointer;
  }
  .all:hover {
    color: var(--fg);
    border-color: var(--line-strong);
  }
  .err {
    flex: none;
    margin: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--font-size-small);
    color: var(--danger);
  }
  .list {
    position: relative;
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
    overflow-y: auto;
    scrollbar-width: thin;
    scrollbar-color: color-mix(in srgb, var(--fg) 18%, transparent) transparent;
  }
  .empty {
    margin: 24px 0;
    text-align: center;
    font-size: var(--font-size-ui);
    color: var(--faint);
  }
  .item {
    position: relative;
    flex: none;
    display: flex;
    align-items: flex-start;
    gap: 8px;
    width: 100%;
    box-sizing: border-box;
    padding: 9px 8px 8px 13px;
    border: 1px solid var(--line);
    border-radius: 8px;
    background: var(--bg);
    color: var(--fg);
    font: inherit;
    text-align: left;
    cursor: pointer;
    user-select: none;
    touch-action: none;
    transition:
      border-color var(--dur-fast) var(--ease-out),
      box-shadow var(--dur-fast) var(--ease-out);
  }
  .item:hover {
    border-color: color-mix(in srgb, var(--accent) 45%, var(--line));
    box-shadow: 0 4px 12px -8px rgb(0 0 0 / 30%);
  }
  /* 拖动时原位置的空档：列表里其它条目自然让开 */
  .gap {
    flex: none;
  }
  /* 浮在指针下的卡片本体 */
  .dragrow {
    position: fixed;
    z-index: 60;
    pointer-events: none;
  }
  .item.clone {
    cursor: grabbing;
    border-color: color-mix(in srgb, var(--accent) 35%, var(--line));
    box-shadow: 0 14px 30px -10px rgb(0 0 0 / 35%);
  }
  .done-space {
    flex: none;
    align-self: center;
    width: 24px;
    height: 24px;
  }
  .tint {
    position: absolute;
    left: 0;
    top: 8px;
    bottom: 8px;
    width: 3px;
    border-radius: 2px;
  }
  .body {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  .text {
    display: -webkit-box;
    overflow: hidden;
    -webkit-box-orient: vertical;
    line-clamp: 2;
    -webkit-line-clamp: 2;
    font-size: var(--font-size-section);
    line-height: 1.45;
    font-weight: 500;
    word-break: break-word;
  }
  .meta {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: var(--font-size-small);
    line-height: 1.4;
    color: var(--faint);
    font-variant-numeric: tabular-nums;
  }
  .due {
    white-space: nowrap;
  }
  .nimg {
    color: var(--faint);
  }
  .thumb {
    flex: none;
    width: 38px;
    height: 38px;
    border: 1px solid var(--line);
    border-radius: 5px;
    background: var(--bg-soft);
    object-fit: cover;
  }
  /* 完成按钮：平时淡，悬停变 accent；点它不打开主窗 */
  .done {
    flex: none;
    align-self: center;
    display: grid;
    place-items: center;
    width: 24px;
    height: 24px;
    padding: 0;
    border: 1px solid var(--line);
    border-radius: 50%;
    background: var(--bg);
    color: var(--faint);
    cursor: pointer;
    transition:
      color var(--dur-fast) var(--ease-out),
      border-color var(--dur-fast) var(--ease-out),
      background var(--dur-fast) var(--ease-out);
  }
  .done:hover {
    border-color: color-mix(in srgb, var(--accent) 55%, var(--line));
    background: var(--accent-soft);
    color: var(--accent);
  }
  .done svg {
    width: 13px;
    height: 13px;
  }
</style>
