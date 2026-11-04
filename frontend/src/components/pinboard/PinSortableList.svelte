<script lang="ts">
  /*
  便签小窗的拖动排序：卡片本体浮在指针下，原位置留出等高的空档（不画
  插入线）；空档位置按拖动开始时的条目中线判定，列表重排不会带着落点抖。
  松手把目标顺序写回 ord。

  items 已是展示顺序；row 片段只画条目内容，外层的点击/拖动/焦点由这里
  统一处理。
  */
  import { onDestroy } from 'svelte'
  import type { Snippet } from 'svelte'
  import type { PinCard } from '../../lib/api'
  import { reorderCards } from '../../lib/pinboard.svelte'

  let {
    items = [],
    titleOf,
    onopen,
    row,
    dragging = $bindable(false),
  }: {
    items?: PinCard[]
    titleOf?: (c: PinCard) => string
    onopen?: (c: PinCard) => void
    row: Snippet<[PinCard]>
    /* 拖动中（小窗轮询/主窗刷新据此避让） */
    dragging?: boolean
  } = $props()

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

  /* 拖动中：列表里去掉被拖的那条，其余条目之间插一个等高的空档 */
  const others = $derived.by(() => {
    const d = drag
    return d?.active ? items.filter((c) => c.id !== d.id) : items
  })
  const dragItem = $derived.by(() => {
    const d = drag
    if (!d?.active) return null
    const c = items.find((x) => x.id === d.id)
    return c ? { c, d } : null
  })

  /* ── 拖动排序：卡片浮在指针下，原位置留空档 ── */

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
      dragging = true
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
    dragging = false
    window.setTimeout(() => (suppressClick = false), 80)
    if (at < 0) return
    const ids = items.map((c) => c.id).filter((id) => id !== d.id)
    ids.splice(at, 0, d.id)
    reorderCards(ids)
  }

  function activate(c: PinCard): void {
    if (suppressClick) return
    onopen?.(c)
  }

  function onRowKey(e: KeyboardEvent, c: PinCard): void {
    if (e.target !== e.currentTarget) return
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      activate(c)
    }
  }

  onDestroy(stopDragListeners)
</script>

<div class="list" bind:this={listEl}>
  {#each others as c, i (c.id)}
    {#if drag?.active && insertAt === i}
      <div class="gap" style:height="{drag.h}px"></div>
    {/if}
    <div
      class="item"
      role="button"
      tabindex="0"
      onclick={() => activate(c)}
      onkeydown={(e) => onRowKey(e, c)}
      onpointerdown={(e) => startDrag(e, c)}
      title={titleOf?.(c)}
    >
      {@render row(c)}
    </div>
  {/each}
  {#if drag?.active && insertAt >= others.length}
    <div class="gap" style:height="{drag.h}px"></div>
  {/if}
</div>

{#if dragItem}
  <div
    class="dragrow"
    style:left="{dragItem.d.dx - dragItem.d.ox}px"
    style:top="{dragItem.d.dy - dragItem.d.oy}px"
    style:width="{dragItem.d.w}px"
  >
    <div class="item clone">
      {@render row(dragItem.c)}
    </div>
  </div>
{/if}

<style>
  .list {
    position: relative;
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-height: 0;
    overflow-y: auto;
    scrollbar-width: thin;
    scrollbar-color: color-mix(in srgb, var(--fg) 18%, transparent) transparent;
  }
  .item {
    position: relative;
    flex: none;
    display: flex;
    align-items: flex-start;
    gap: 8px;
    width: 100%;
    min-width: 0;
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
  .item:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }
  /* 拖动时原位置的空档：列表里其它条目自然让开 */
  .gap {
    flex: none;
  }
  /* 浮在指针下的卡片本体 */
  .dragrow {
    position: fixed;
    z-index: 100;
    pointer-events: none;
  }
  .item.clone {
    cursor: grabbing;
    border-color: color-mix(in srgb, var(--accent) 35%, var(--line));
    box-shadow: 0 14px 30px -10px rgb(0 0 0 / 35%);
  }
</style>
