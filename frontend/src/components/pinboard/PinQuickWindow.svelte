<script lang="ts">
  /*
  便签小窗（?pins=1 窗口，宠物便签球打开）：不拉主客户端的 bootstrap/SSE，
  只轮询 /api/pinboard 展示板上待办，按截止时间从紧到松排序、沿用卡片的
  截止分档色；点条目 → 主进程亮主窗并定位到那张卡，点右上「打开便签板」
  只切到便签页。没有待办/加载失败都有占位。
  */
  import { onMount } from 'svelte'
  import type { PinCard } from '../../lib/api'
  import { board, ensurePinboard, reloadPinboard } from '../../lib/pinboard.svelte'
  import { DUE_COLOR, dueLevel } from '../../lib/pinDue'

  const ez = (window as any).ez

  let now = $state(Date.now())

  onMount(() => {
    void ensurePinboard()
    const t = setInterval(() => {
      void reloadPinboard()
      now = Date.now()
    }, 3000)
    return () => clearInterval(t)
  })

  /* 按截止从紧到松；无截止排最后，同档新记的在前 */
  const items = $derived.by(() =>
    [...board.cards].sort((a, b) => {
      const ad = a.due ?? Number.MAX_SAFE_INTEGER
      const bd = b.due ?? Number.MAX_SAFE_INTEGER
      if (ad !== bd) return ad - bd
      return b.created - a.created
    }),
  )
  const overCount = $derived(board.cards.filter((c) => dueLevel(c.due, now) === 'over').length)

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
</script>

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

  <div class="list">
    {#if !board.loaded}
      <p class="empty">{board.loading ? '便签加载中…' : '便签加载失败'}</p>
    {:else if !items.length}
      <p class="empty">板上暂无待办</p>
    {:else}
      {#each items as c (c.id)}
        <button
          class="item"
          onclick={() => ez?.pin?.jump?.(c.id)}
          title={`${c.text || '（空便签）'}\n${dueTitle(c)}\n点击打开便签板并定位`}
        >
          <i class="tint" style:background={c.tint}></i>
          <span class="body">
            <span class="text">{plain(c.text) || (c.images?.length ? '（图片）' : '（空便签）')}</span>
            <span class="meta">
              <span class="due" style:color={dueColor(c)}>{dueText(c)}</span>
              {#if c.images?.length}<span class="nimg">{c.images.length} 张图</span>{/if}
            </span>
          </span>
          {#if c.images?.length}
            <img class="thumb" src={c.images[0]} alt="" loading="lazy" />
          {/if}
        </button>
      {/each}
    {/if}
  </div>
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
  .list {
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
    padding: 9px 10px 8px 13px;
    border: 1px solid var(--line);
    border-radius: 8px;
    background: var(--bg);
    color: var(--fg);
    font: inherit;
    text-align: left;
    cursor: pointer;
    transition:
      border-color var(--dur-fast) var(--ease-out),
      box-shadow var(--dur-fast) var(--ease-out);
  }
  .item:hover {
    border-color: color-mix(in srgb, var(--accent) 45%, var(--line));
    box-shadow: 0 4px 12px -8px rgb(0 0 0 / 30%);
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
</style>
