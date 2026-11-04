<script lang="ts">
  /*
  顶栏便签小点：板上每张待办对应一颗便签色小点，排在便签图标左侧；
  点一颗点 → 从顶部挂下完整便签（正文/图片/记于/截止/关联会话/新会话），
  再点同一颗点收起。点左右拖动即排序（ord 落盘）。
  */
  import { onDestroy } from 'svelte'
  import { fly } from 'svelte/transition'
  import type { PinCard, PinSessionRef } from '../../lib/api'
  import { store } from '../../lib/store.svelte'
  import {
    addPendingSession,
    board,
    completeCard,
    orderedCards,
    patchCard,
    pinboardBusy,
    reloadPinboard,
    removeCard,
    reorderCards,
    saveNow,
  } from '../../lib/pinboard.svelte'
  import { plainPinText, renderPinMarkdown, toggleTask } from '../../lib/pinMarkdown'
  import { dueColor, dueHint, fmtDuration, fmtShortTime } from '../../lib/pinDue'

  let {
    now,
    selected = $bindable(''),
  }: {
    now: number
    /* 展开的便签 id（空 = 全部收起成小点） */
    selected?: string
  } = $props()

  const items = $derived(orderedCards(board.cards))
  const card = $derived(items.find((c) => c.id === selected) ?? null)
  const cardHtml = $derived.by(() =>
    card ? renderPinMarkdown(card.text || (card.images?.length ? '' : '（空便签）')) : '',
  )
  const latest = $derived(card ? latestSession(card) : undefined)
  const latestTime = $derived(latest ? sessionTime(latest) : '')
  const extra = $derived(card ? extraSessions(card) : 0)

  let creatingFor = $state('')
  let deleteAsk = $state(false)
  let dotsEl = $state<HTMLElement>()
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
    mids: Map<string, number> | null
  } | null>(null)
  let insertAt = $state(-1)
  let suppressClick = false
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

  function latestSession(c: PinCard): PinSessionRef | undefined {
    return c.sessions?.length ? c.sessions[c.sessions.length - 1] : undefined
  }

  function extraSessions(c: PinCard): number {
    return Math.max(0, (c.sessions?.length ?? 0) - 1)
  }

  function sessionTitle(s: PinSessionRef): string {
    return store.branches.find((b) => b.id === s.id)?.title || s.title || '会话'
  }

  function sessionTime(s: PinSessionRef): string {
    const ts = store.branches.find((b) => b.id === s.id)?.createdAt || s.createdAt || 0
    return ts > 0 ? fmtShortTime(ts) : ''
  }

  /* ── 展开 / 收起 / 跳转 ── */

  function pick(c: PinCard): void {
    if (suppressClick) return
    deleteAsk = false
    if (selected === c.id) {
      selected = ''
      return
    }
    selected = c.id
    /* 点开时顺手拉一次最新板：小窗/别处可能刚改过 */
    if (!pinboardBusy()) void reloadPinboard()
  }

  function openBoard(): void {
    selected = ''
    store.jumpView = 'pinboard'
  }

  function openCard(c: PinCard): void {
    selected = ''
    store.pinFocus = c.id
    store.jumpView = 'pinboard'
  }

  function openSession(s: PinSessionRef): void {
    selected = ''
    void store.openSession(s.id)
  }

  async function newSession(c: PinCard): Promise<void> {
    if (creatingFor) return
    creatingFor = c.id
    try {
      const before = store.activeId
      await store.newChatTab()
      const id = store.activeId
      if (!id || id === before) return
      addPendingSession(c.id, id)
      selected = ''
      store.jumpView = 'chat'
    } finally {
      creatingFor = ''
    }
  }

  function complete(c: PinCard, e: MouseEvent): void {
    e.stopPropagation()
    completeCard(c.id)
    void saveNow()
    selected = ''
    deleteAsk = false
  }

  function removeNow(): void {
    if (!card) return
    removeCard(card.id)
    void saveNow()
    selected = ''
    deleteAsk = false
  }

  /* 正文里的待办勾选框：翻转原文对应任务行 */
  function onMdClick(e: MouseEvent): void {
    const box = (e.target as HTMLElement).closest('input[type="checkbox"]')
    if (!box || !card) return
    e.preventDefault()
    const host = e.currentTarget as HTMLElement
    const idx = Array.from(host.querySelectorAll('input[type="checkbox"]')).indexOf(box as HTMLInputElement)
    if (idx < 0) return
    const next = toggleTask(card.text, idx)
    if (next !== null) patchCard(card.id, { text: next })
  }

  /* ── 沿顶栏左右拖动排序 ── */

  function startDrag(e: PointerEvent, c: PinCard): void {
    if (e.button !== 0) return
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

  function captureMids(): Map<string, number> {
    const m = new Map<string, number>()
    if (!dotsEl) return m
    dotsEl.querySelectorAll<HTMLElement>('.slot').forEach((el, i) => {
      if (!items[i]) return
      const r = el.getBoundingClientRect()
      m.set(items[i].id, r.left + r.width / 2)
    })
    return m
  }

  function insertIndex(clientX: number): number {
    const d = drag
    if (!d?.mids) return insertAt
    let idx = 0
    for (const c of items) {
      if (c.id === d.id) continue
      const mid = d.mids.get(c.id)
      if (mid !== undefined && clientX > mid) idx++
    }
    return idx
  }

  function onDragMove(e: PointerEvent): void {
    if (!drag) return
    if (!drag.active) {
      if (Math.hypot(e.clientX - drag.px, e.clientY - drag.py) < 5) return
      drag.mids = captureMids()
      drag.active = true
      selected = ''
      deleteAsk = false
      suppressClick = true
    }
    drag.dx = e.clientX
    drag.dy = e.clientY
    insertAt = insertIndex(e.clientX)
  }

  function onDragUp(e: PointerEvent): void {
    finishDrag(e)
  }

  function onDragCancel(): void {
    finishDrag(null)
  }

  function finishDrag(e: PointerEvent | null): void {
    if (!drag) return
    const d = drag
    const at = d.active && e ? insertIndex(e.clientX) : -1
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

  function onSlotKey(e: KeyboardEvent, c: PinCard): void {
    if (e.target !== e.currentTarget) return
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      pick(c)
    }
  }

  /* 选中的卡被完成/删除/外部改掉后收起 */
  $effect(() => {
    if (selected && !items.some((c) => c.id === selected)) selected = ''
  })

  onDestroy(stopDragListeners)
</script>

{#if items.length}
  <div class="dots" bind:this={dotsEl}>
    {#each others as c, i (c.id)}
      {#if drag?.active && insertAt === i}
        <div class="gap" style:width="{drag.w}px"></div>
      {/if}
      <div
        class="slot"
        class:sel={selected === c.id}
        style:--tint={c.tint}
        role="button"
        tabindex="0"
        aria-expanded={selected === c.id}
        onclick={() => pick(c)}
        onkeydown={(e) => onSlotKey(e, c)}
        onpointerdown={(e) => startDrag(e, c)}
        title={`${plainPinText(c.text) || (c.images?.length ? '（图片）' : '（空便签）')}\n${dueHint(c.due, now)}\n${selected === c.id ? '点击收起' : '点击展开'} · 拖动排序`}
      >
        <span class="dot"></span>
      </div>
    {/each}
    {#if drag?.active && insertAt >= others.length}
      <div class="gap" style:width="{drag.w}px"></div>
    {/if}
  </div>
{/if}

{#if card}
  <div class="card-layer" transition:fly={{ y: -8, duration: 150 }}>
    <div class="note-card" style:--tint={card.tint}>
      <i class="ctint"></i>
      <div class="cbar">
        <button class="cboard" onclick={openBoard} title="打开便签板">便签板 ↗</button>
        <button
          class="cicon"
          onclick={(e) => complete(card, e)}
          title="划线完成，归档到时间线"
          aria-label="完成这张便签"
        >
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round">
            <path d="M5 12.5l4.5 4.5L19 7" />
          </svg>
        </button>
        <button class="cicon" onclick={() => (deleteAsk = true)} title="删除这张便签" aria-label="删除这张便签">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round">
            <path d="M6 6l12 12M18 6L6 18" />
          </svg>
        </button>
      </div>

      <div class="cbody">
        <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
        <div class="cmd" onclick={onMdClick}>{@html cardHtml}</div>
        {#if card.images?.length}
          <div class="cimgs">
            {#each card.images.slice(0, 3) as src (src)}
              <button class="cimg" onclick={() => openCard(card)} title="在便签板查看这张图">
                <img {src} alt="" loading="lazy" draggable="false" />
              </button>
            {/each}
          </div>
        {/if}
      </div>

      <div class="cfoot">
        <div class="ctimes">
          <span>记于 {fmtShortTime(card.created)} · 已持续 {fmtDuration(Math.max(0, now - card.created))}</span>
          <span class="cdue" style:color={dueColor(card.due, now)} title={dueHint(card.due, now)}>
            截止 {card.due ? fmtShortTime(card.due) : '未设置'}
          </span>
        </div>
        <div class="clinks">
          <button
            class="cnew"
            disabled={!!creatingFor}
            onclick={() => void newSession(card)}
            title="新建会话并跳转；在那边发出第一条消息后才会关联到这张便签"
          >
            {creatingFor ? '创建中…' : '＋ 新会话'}
          </button>
          {#if latest}
            <button
              class="cses"
              onclick={() => openSession(latest)}
              title={`打开关联会话：${sessionTitle(latest)}${extra ? `（另有 ${extra} 个，见便签板）` : ''}`}
            >
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <path d="M7 17L17 7M9 7h8v8" />
              </svg>
              <span class="cstitle">{sessionTitle(latest)}</span>
              {#if latestTime}<span class="ctime">{latestTime}</span>{/if}
              {#if extra}<span class="cmore">+{extra}</span>{/if}
            </button>
          {/if}
        </div>
      </div>

      {#if deleteAsk}
        <div class="cdel">
          <p>删掉这张？</p>
          <div class="cdelbtns">
            <button onclick={() => (deleteAsk = false)}>取消</button>
            <button class="danger" onclick={removeNow}>删除</button>
          </div>
        </div>
      {/if}
    </div>
  </div>
{/if}

{#if dragItem}
  <div
    class="dragdot"
    style:left="{dragItem.d.dx - dragItem.d.ox}px"
    style:top="{dragItem.d.dy - dragItem.d.oy}px"
    style:width="{dragItem.d.w}px"
    style:height="{dragItem.d.h}px"
    style:--tint={dragItem.c.tint}
  >
    <span class="dot"></span>
  </div>
{/if}

<style>
  .dots {
    display: flex;
    align-items: center;
    gap: 2px;
    height: 100%;
  }
  .slot {
    position: relative;
    flex: none;
    display: grid;
    place-items: center;
    width: 16px;
    height: 100%;
    cursor: grab;
    touch-action: none;
    user-select: none;
  }
  .slot:active {
    cursor: grabbing;
  }
  .slot:focus-visible {
    outline: none;
  }
  .gap {
    flex: none;
    height: 100%;
  }
  .dot {
    display: block;
    width: 7px;
    height: 7px;
    box-sizing: border-box;
    border-radius: 50%;
    border: 1px solid color-mix(in srgb, var(--tint) 62%, var(--line));
    background: var(--tint);
    box-shadow: 0 0 0 1px color-mix(in srgb, var(--bg-soft) 60%, transparent);
    transition:
      transform 140ms var(--ease-out),
      box-shadow 140ms var(--ease-out);
  }
  .slot:hover .dot {
    transform: scale(1.55);
  }
  .slot.sel .dot {
    transform: scale(1.5);
    box-shadow:
      0 0 0 2px var(--accent-soft),
      0 0 0 1px var(--accent);
  }
  /* 选中的点垂下一根线，接到下面的完整便签 */
  .slot.sel::after {
    content: '';
    position: absolute;
    top: calc(50% + 5px);
    left: 50%;
    width: 1px;
    height: 24px;
    background: color-mix(in srgb, var(--fg) 24%, transparent);
    pointer-events: none;
  }
  /* 完整便签从顶部挂下来，右缘与点区对齐 */
  .card-layer {
    position: absolute;
    top: 100%;
    right: 0;
    z-index: 2;
    padding-top: 10px;
  }
  .note-card {
    position: relative;
    display: flex;
    flex-direction: column;
    width: 320px;
    max-width: calc(100vw - 24px);
    max-height: min(62vh, 540px);
    padding: 8px 10px 10px 15px;
    box-sizing: border-box;
    border: 1px solid var(--line);
    border-radius: 11px;
    background: var(--bg);
    box-shadow:
      0 16px 40px rgb(0 0 0 / 14%),
      0 2px 8px rgb(0 0 0 / 6%);
    overflow: hidden;
  }
  .ctint {
    position: absolute;
    left: 0;
    top: 9px;
    bottom: 9px;
    width: 3px;
    border-radius: 2px;
    background: var(--tint);
  }
  .cbar {
    flex: none;
    display: flex;
    align-items: center;
    gap: 4px;
    min-height: 20px;
  }
  .cboard {
    margin-right: auto;
    padding: 0;
    color: var(--faint);
    font-size: var(--font-size-small);
    line-height: 1.5;
    transition: color var(--dur-fast) var(--ease-out);
  }
  .cboard:hover {
    color: var(--accent);
  }
  .cicon {
    flex: none;
    display: grid;
    place-items: center;
    width: 20px;
    height: 20px;
    border-radius: 5px;
    background: var(--bg-soft);
    color: var(--muted);
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .cicon:hover {
    background: var(--line);
    color: var(--fg);
  }
  .cicon svg {
    width: 11px;
    height: 11px;
  }
  .cbody {
    flex: 1;
    min-height: 0;
    display: flex;
    align-items: flex-start;
    gap: 10px;
    margin-top: 4px;
    overflow-y: auto;
    overscroll-behavior: contain;
    scrollbar-width: thin;
    scrollbar-color: color-mix(in srgb, var(--fg) 18%, transparent) transparent;
  }
  .cmd {
    flex: 1;
    min-width: 0;
    font-size: var(--font-size-section);
    line-height: 1.6;
    font-weight: 500;
    color: var(--fg);
    word-break: break-word;
    user-select: text;
    cursor: default;
  }
  .cmd :global(p) {
    margin: 0 0 3px;
  }
  .cmd :global(p:last-child) {
    margin-bottom: 0;
  }
  .cmd :global(h1),
  .cmd :global(h2),
  .cmd :global(h3),
  .cmd :global(h4) {
    margin: 6px 0 2px;
    font-weight: 700;
    line-height: 1.35;
  }
  .cmd :global(h1) {
    font-size: 1.35em;
  }
  .cmd :global(h2) {
    font-size: 1.2em;
  }
  .cmd :global(h3),
  .cmd :global(h4) {
    font-size: 1.08em;
    font-weight: 600;
  }
  .cmd :global(ul),
  .cmd :global(ol) {
    margin: 1px 0 4px;
    padding-left: 1.25em;
  }
  .cmd :global(li) {
    margin: 0;
  }
  .cmd :global(input[type='checkbox']) {
    width: 12px;
    height: 12px;
    margin: 0 5px 0 0;
    accent-color: var(--accent);
    vertical-align: -2px;
    cursor: pointer;
  }
  .cmd :global(li:has(> input[type='checkbox'])) {
    list-style: none;
    margin-left: -1.25em;
  }
  .cmd :global(code) {
    padding: 0 4px;
    border-radius: 4px;
    background: var(--bg-soft);
    font-family: var(--font-mono);
    font-size: 0.9em;
  }
  .cmd :global(pre) {
    margin: 4px 0;
    padding: 6px 8px;
    border-radius: 6px;
    background: var(--bg-soft);
    overflow-x: auto;
  }
  .cmd :global(pre code) {
    padding: 0;
    background: none;
  }
  .cmd :global(blockquote) {
    margin: 4px 0;
    padding-left: 8px;
    border-left: 2px solid var(--line-strong);
    color: var(--muted);
  }
  .cmd :global(a) {
    color: var(--accent);
    text-decoration: none;
  }
  .cmd :global(a:hover) {
    text-decoration: underline;
  }
  .cmd :global(hr) {
    margin: 8px 0;
    border: none;
    border-top: 1px solid var(--line);
  }
  .cimgs {
    flex: none;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .cimg {
    display: block;
    width: 56px;
    height: 56px;
    padding: 0;
    border: 1px solid var(--line);
    border-radius: 5px;
    background: var(--bg-soft);
    overflow: hidden;
  }
  .cimg img {
    display: block;
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  .cimg:hover {
    border-color: var(--line-strong);
  }
  .cfoot {
    flex: none;
    display: flex;
    align-items: flex-end;
    justify-content: space-between;
    gap: 10px;
    margin-top: 8px;
    min-width: 0;
  }
  .ctimes {
    display: flex;
    flex-direction: column;
    gap: 1px;
    min-width: 0;
    font-size: var(--font-size-small);
    color: var(--faint);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .cdue {
    white-space: nowrap;
  }
  .clinks {
    flex: none;
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 1px;
    min-width: 0;
    max-width: 58%;
  }
  .cnew {
    padding: 0;
    color: var(--faint);
    font-size: var(--font-size-small);
    line-height: 1.5;
    white-space: nowrap;
    transition: color var(--dur-fast) var(--ease-out);
  }
  .cnew:hover {
    color: var(--fg);
  }
  .cnew:disabled {
    cursor: default;
    color: var(--faint);
  }
  .cses {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    min-width: 0;
    max-width: 100%;
    padding: 0;
    color: var(--muted);
    font-size: var(--font-size-small);
    line-height: 1.5;
    transition: color var(--dur-fast) var(--ease-out);
  }
  .cses:hover {
    color: var(--accent);
  }
  .cses svg {
    flex: none;
    width: 10px;
    height: 10px;
    opacity: 0.65;
  }
  .cstitle {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .cses:hover .cstitle {
    text-decoration: underline;
    text-underline-offset: 2px;
  }
  .ctime {
    flex: none;
    font-size: 10px;
    color: var(--faint);
    font-variant-numeric: tabular-nums;
  }
  .cmore {
    flex: none;
    font-size: 10px;
    color: var(--faint);
  }
  /* 删除就地确认 */
  .cdel {
    position: absolute;
    inset: 0;
    z-index: 5;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 8px;
    border-radius: inherit;
    background: color-mix(in srgb, var(--bg) 92%, transparent);
    backdrop-filter: blur(2px);
    font-size: var(--font-size-ui);
  }
  .cdel p {
    margin: 0;
    color: var(--muted);
  }
  .cdelbtns {
    display: flex;
    gap: 6px;
  }
  .cdelbtns button {
    padding: 3px 10px;
    border: 1px solid var(--line);
    border-radius: 6px;
    background: var(--bg);
    color: var(--fg);
    font-size: var(--font-size-small);
  }
  .cdelbtns button:hover {
    border-color: var(--line-strong);
  }
  .cdelbtns .danger {
    background: var(--danger);
    border-color: var(--danger);
    color: #fff;
  }
  /* 拖动跟手的点 */
  .dragdot {
    position: fixed;
    z-index: 120;
    display: grid;
    place-items: center;
    pointer-events: none;
  }
  .dragdot .dot {
    transform: scale(1.5);
    box-shadow: 0 6px 14px -4px rgb(0 0 0 / 45%);
  }
</style>
