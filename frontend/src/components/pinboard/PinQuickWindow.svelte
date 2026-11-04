<script lang="ts">
  /*
  便签小窗（?pins=1 窗口，宠物便签球打开）：不拉主客户端的 bootstrap/SSE，
  只轮询 /api/pinboard 展示板上待办；没手工排过时按截止从紧到松，拖过之后
  按 ord 排（手动顺序持久化进 pinboard.json）。点条目 → 主进程亮主窗并定
  位到那张卡，点右侧「完成」直接归档到时间线，点右上「打开便签板」只切到
  便签页。没有待办/加载失败都有占位。拖动排序由 PinSortableList 承载。
  */
  import { onMount } from 'svelte'
  import type { PinCard } from '../../lib/api'
  import {
    board,
    completeCard,
    ensurePinboard,
    orderedCards,
    pinboardBusy,
    reloadPinboard,
    saveNow,
  } from '../../lib/pinboard.svelte'
  import { plainPinText } from '../../lib/pinMarkdown'
  import { dueColor, dueHint, dueLabel, dueLevel } from '../../lib/pinDue'
  import PinSortableList from './PinSortableList.svelte'

  const ez = (window as any).ez

  let now = $state(Date.now())
  let sortDragging = $state(false)

  onMount(() => {
    void ensurePinboard()
    const t = setInterval(() => {
      /* 拖动中/本地有未落盘改动时不重拉：避免列表在手下换序 */
      if (!sortDragging && !pinboardBusy()) void reloadPinboard()
      now = Date.now()
    }, 3000)
    return () => clearInterval(t)
  })

  const items = $derived(orderedCards(board.cards))
  const overCount = $derived(board.cards.filter((c) => dueLevel(c.due, now) === 'over').length)

  function open(c: PinCard): void {
    ez?.pin?.jump?.(c.id)
  }

  function complete(c: PinCard, e: MouseEvent): void {
    e.stopPropagation()
    completeCard(c.id)
    void saveNow() // 小窗随时可能被关，完成立即落盘
  }
</script>

{#snippet rowBody(c: PinCard)}
  <i class="tint" style:background={c.tint}></i>
  <span class="body">
    <span class="text">{plainPinText(c.text) || (c.images?.length ? '（图片）' : '（空便签）')}</span>
    <span class="meta">
      <span class="due" style:color={dueColor(c.due, now)}>{dueLabel(c.due)}</span>
      {#if c.images?.length}<span class="nimg">{c.images.length} 张图</span>{/if}
    </span>
  </span>
  {#if c.images?.length}
    <img class="thumb" src={c.images[0]} alt="" loading="lazy" draggable="false" />
  {/if}
{/snippet}

{#snippet row(c: PinCard)}
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

  {#if !board.loaded}
    <p class="empty">{board.loading ? '便签加载中…' : '便签加载失败'}</p>
  {:else if !items.length}
    <p class="empty">板上暂无待办</p>
  {:else}
    <PinSortableList
      {items}
      bind:dragging={sortDragging}
      onopen={open}
      titleOf={(c) => `${c.text || '（空便签）'}\n${dueHint(c.due, now)}\n拖动排序 · 点击打开便签板并定位`}
      row={row}
    />
  {/if}
</div>

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
  .empty {
    margin: 24px 0;
    text-align: center;
    font-size: var(--font-size-ui);
    color: var(--faint);
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
