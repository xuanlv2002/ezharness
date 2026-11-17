<script lang="ts">
  /* 分支面板：会话树的"线"列表，按更新时间分组；点击切换、行尾归档/收藏。
     悬浮覆盖抽屉——浮在聊天内容之上不占布局宽度，点外部收起。 */
  import { fly } from 'svelte/transition'
  import { type BranchView } from '../lib/api'
  import { store } from '../lib/store.svelte'

  /* 收起为小方块：localStorage 记忆（有分支在跑/等审批时红点提示）。
  悬浮抽屉语义：点面板外部自动收起（面板内点击——切分支/归档——不收） */
  const collapsedKey = 'ezh.branchPanel.collapsed'
  let collapsed = $state((() => {
    try {
      return localStorage.getItem(collapsedKey) === '1'
    } catch {
      return false
    }
  })())
  function fold(v: boolean) {
    collapsed = v
    try {
      localStorage.setItem(collapsedKey, v ? '1' : '0')
    } catch {
      /* 存储不可用时仅本次生效 */
    }
  }

  let panelEl = $state<HTMLElement | null>(null)
  function onGlobalPointerDown(e: PointerEvent) {
    if (collapsed || !panelEl) return
    if (panelEl.contains(e.target as Node)) return
    fold(true)
  }

  const alertCount = $derived(store.branches.filter((b) => b.running || b.waiting).length)

  function switchTo(b: BranchView) {
    if (b.active || b.id === store.activeId) return
    void store.openTab(b.id)
  }

  /* 时间分组：branches 已按 updatedAt 降序，顺序遍历切组即可 */
  function groupLabel(ts?: number): string {
    if (!ts) return '更早'
    const now = new Date()
    const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime()
    const t = ts
    if (t >= startOfToday) return '今天'
    if (t >= startOfToday - 86400000) return '昨天'
    if (t >= startOfToday - 7 * 86400000) return '7 天内'
    return '更早'
  }

  /* 行内时间：今天显示时刻，更早显示日期（跨年带年份），悬停有完整时间 */
  function timeLabel(ts?: number): string {
    if (!ts) return ''
    const d = new Date(ts)
    const now = new Date()
    const pad = (n: number) => String(n).padStart(2, '0')
    if (groupLabel(ts) === '今天') return `${pad(d.getHours())}:${pad(d.getMinutes())}`
    if (d.getFullYear() === now.getFullYear()) return `${d.getMonth() + 1}-${d.getDate()}`
    return `${String(d.getFullYear()).slice(2)}-${d.getMonth() + 1}-${d.getDate()}`
  }
  const groups = $derived.by(() => {
    const out: { label: string; items: BranchView[] }[] = []
    for (const b of store.branches) {
      if (stars.has(b.id)) continue // 收藏的进置顶组，不重复进时间组
      const label = groupLabel(b.updatedAt || b.createdAt)
      const last = out[out.length - 1]
      if (last && last.label === label) last.items.push(b)
      else out.push({ label, items: [b] })
    }
    return out
  })

  /* ── 收藏分支：★ 名单 localStorage 持久（rootId 身份，compact 换代不变）；
     收藏组固定置顶，重要分支不会被时间与新增话题沉底 ── */
  const starsKey = 'ezh.branch.stars'
  let stars = $state<Set<string>>(
    (() => {
      try {
        return new Set(JSON.parse(localStorage.getItem(starsKey) || '[]') as string[])
      } catch {
        return new Set<string>()
      }
    })(),
  )
  const favs = $derived(store.branches.filter((b) => stars.has(b.id)))

  function toggleStar(b: BranchView) {
    const s = new Set(stars)
    if (s.has(b.id)) s.delete(b.id)
    else s.add(b.id)
    stars = s
    try {
      localStorage.setItem(starsKey, JSON.stringify([...s]))
    } catch {
      /* 存储不可用时仅本次生效 */
    }
  }
</script>

<svelte:window onpointerdown={onGlobalPointerDown} />

{#if collapsed}
  <button class="mini" onclick={() => fold(false)} title={`分支 · 点击展开${alertCount > 0 ? `（${alertCount} 条在跑/待审批）` : ''}`}>
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
      <circle cx="6" cy="6" r="3" />
      <circle cx="6" cy="18" r="3" />
      <path d="M6 9v6" />
      <path d="M18 9a3 3 0 1 0-6 0v6a3 3 0 1 0 6 0" />
    </svg>
    {#if alertCount > 0}
      <i class="dot"></i>
    {/if}
  </button>
{:else}
  <div class="panel" bind:this={panelEl} transition:fly={{ x: -16, duration: 180 }}>
    <div class="head">
      <h2>分支</h2>
      <button class="fold" onclick={() => fold(true)} title="收起">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <path d="M15 6l-6 6 6 6" />
        </svg>
      </button>
    </div>
    <div class="list">
      <button class="newbig" onclick={() => void store.newChatTab()} title="开一条新分支（新话题）">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <circle cx="12" cy="12" r="9" />
          <path d="M9 12h6M12 9v6" />
        </svg>
        开启新分支
      </button>
      {#snippet row(b: BranchView)}
        <div class="branch" class:cur={b.active || b.id === store.activeId} role="button" tabindex="0"
          onclick={() => switchTo(b)}
          onkeydown={(e) => (e.key === 'Enter' || e.key === ' ') && switchTo(b)}>
          <span class="st" class:on={b.running || b.waiting} class:run={b.running}></span>
          <span class="t-text">{b.title || '未命名分支'}</span>
          {#if b.kind === 'fork'}
            <i class="kbadge" title={b.origin?.title ? `来自 session：${b.origin.title}` : 'fork 产生的分支'}>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <circle cx="6" cy="5" r="2" /><circle cx="6" cy="19" r="2" /><circle cx="18" cy="7" r="2" />
                <path d="M6 7v10M18 9v2a4 4 0 0 1-4 4H8" />
              </svg>
            </i>
          {/if}
          {#if b.archiving || b.id === store.archivingRootId}<em class="flag arc-ing">归档中</em>{/if}
          {#if b.waiting}<em class="flag">待审批</em>{/if}
          <span class="tm" title={b.updatedAt || b.createdAt ? new Date(b.updatedAt || b.createdAt!).toLocaleString() : ''}>
            {timeLabel(b.updatedAt || b.createdAt)}
          </span>
          <button class="star" class:on={stars.has(b.id)}
            onclick={(e) => { e.stopPropagation(); toggleStar(b) }}
            title={stars.has(b.id) ? '取消收藏（回到时间分组）' : '收藏此分支（置顶显示，不随时间沉底）'}>
            <svg viewBox="0 0 24 24" fill={stars.has(b.id) ? 'currentColor' : 'none'} stroke="currentColor" stroke-width="1.7" stroke-linejoin="round">
              <path d="M12 3.6l2.6 5.4 5.9.8-4.3 4.1 1.1 5.9-5.3-2.9-5.3 2.9 1.1-5.9L3.5 9.8l5.9-.8z" />
            </svg>
          </button>
          <button class="arc" disabled={b.archiving || b.id === store.archivingRootId || b.running}
            onclick={(e) => { e.stopPropagation(); void store.compactTopic(b.id) }}
            title="归档此话题：总结归档并开新会话（同线换代，树上加一代）">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
              <path d="M4 4h16v4H4z" />
              <path d="M6 8v12h12V8" />
              <path d="M12 17v-5M9.5 14.5L12 12l2.5 2.5" />
            </svg>
          </button>
        </div>
      {/snippet}
      {#if favs.length}
        <div class="glabel fav">
          <svg viewBox="0 0 24 24" fill="currentColor" stroke="currentColor" stroke-width="1.7" stroke-linejoin="round">
            <path d="M12 3.6l2.6 5.4 5.9.8-4.3 4.1 1.1 5.9-5.3-2.9-5.3 2.9 1.1-5.9L3.5 9.8l5.9-.8z" />
          </svg>
          收藏
        </div>
        {#each favs as b (b.id)}{@render row(b)}{/each}
      {/if}
      {#each groups as g (g.label)}
        <div class="glabel">{g.label}</div>
        {#each g.items as b (b.id)}{@render row(b)}{/each}
      {/each}
      {#if store.branches.length === 0}
        <div class="empty">暂无分支——点「开启新分支」开一条。</div>
      {/if}
    </div>
  </div>
{/if}

<style>
  .mini {
    flex: none;
    position: relative; /* 提醒红点的定位锚点，缺失会飞到悬浮列外 */
    display: grid;
    place-items: center;
    width: 36px;
    height: 36px;
    border: 1px solid var(--line);
    border-radius: 10px;
    background: var(--glass);
    backdrop-filter: var(--glass-blur);
    color: var(--muted);
    cursor: pointer;
    transition: color var(--dur-fast) var(--ease-out), border-color var(--dur-fast) var(--ease-out);
  }
  .mini:hover {
    color: var(--fg);
    border-color: var(--line-strong);
  }
  .mini svg {
    width: 17px;
    height: 17px;
  }
  .mini .dot {
    position: absolute;
    top: -3px;
    right: -3px;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: #f0883e;
  }
  .panel {
    /* 脱离容器 flex 流：fly 离场期间不占位（否则 mini 进场被挤到
    面板下方、离场完再弹回）。相对 .side-left（absolute）定位 */
    position: absolute;
    top: 0;
    left: 0;
    display: flex;
    flex-direction: column;
    border: 1px solid var(--line);
    border-radius: 12px;
    background: var(--bg);
    /* 分支多时别一路撑到窗口底：与状态卡同档，超出列表内部滚动 */
    max-height: min(100%, 60vh);
    overflow: hidden;
  }
  .head {
    flex: none;
    display: flex;
    align-items: center;
    padding: 8px 10px;
  }
  .head h2 {
    font-size: var(--font-size-ui);
    font-weight: 600;
    color: var(--muted);
    flex: 1;
  }
  .fold {
    flex: none;
    display: grid;
    place-items: center;
    width: 22px;
    height: 22px;
    border: none;
    background: transparent;
    color: var(--faint);
    cursor: pointer;
    border-radius: 6px;
  }
  .fold:hover {
    color: var(--fg);
    background: var(--bg-soft);
  }
  .fold svg {
    width: 14px;
    height: 14px;
  }
  .list {
    min-height: 0;
    overflow-y: auto;
    padding: 0 6px 6px;
    display: flex;
    flex-direction: column;
  }
  /* 顶部全宽新建按钮 */
  .newbig {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 7px;
    margin: 4px 2px 8px;
    padding: 8px 0;
    border: 1px solid var(--line);
    border-radius: 10px;
    background: var(--bg);
    color: var(--fg);
    font-size: var(--font-size-ui);
    font-weight: 500;
    cursor: pointer;
    transition: border-color var(--dur-fast) var(--ease-out), color var(--dur-fast) var(--ease-out),
      background var(--dur-fast) var(--ease-out);
  }
  .newbig:hover {
    border-color: var(--accent);
    color: var(--accent);
    background: var(--accent-soft);
  }
  .newbig svg {
    width: 14px;
    height: 14px;
  }
  .glabel {
    flex: none;
    padding: 8px 8px 3px;
    font-size: var(--font-size-small);
    color: var(--faint);
    user-select: none;
  }
  .branch {
    display: flex;
    align-items: center;
    gap: 7px;
    padding: 7px 8px;
    border-radius: 8px;
    cursor: pointer;
    transition: background var(--dur-fast) var(--ease-out);
  }
  .branch:hover {
    background: var(--bg-soft);
  }
  .branch.cur {
    background: color-mix(in srgb, var(--accent) 7%, var(--bg));
  }
  .branch .t-text {
    flex: 0 1 auto;
    min-width: 0;
    font-size: var(--font-size-ui);
    color: var(--fg);
    font-weight: 500;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  /* 状态点：空闲透明占位（保持标题对齐），橙闪=运行中 橙亮=待审批 */
  .st {
    flex: none;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: transparent;
  }
  .st.on {
    background: #f0883e;
  }
  .st.run {
    animation: breath 1.4s ease-in-out infinite;
  }
  @keyframes breath {
    0%,
    100% {
      opacity: 0.35;
      transform: scale(0.85);
    }
    50% {
      opacity: 1;
      transform: scale(1.05);
    }
  }
  .kbadge {
    flex: none;
    display: grid;
    place-items: center;
    width: 17px;
    height: 17px;
    font-style: normal;
    color: var(--accent);
    background: var(--accent-soft);
    border-radius: 5px;
  }
  .kbadge svg {
    width: 11px;
    height: 11px;
  }
  .flag {
    flex: none;
    font-style: normal;
    font-size: var(--font-size-small);
    color: #f0883e;
  }
  .arc-ing {
    color: #8957e5;
  }
  /* 行尾更新时间：右推（今天=时刻，更早=日期），hover 归档按钮时与之并排 */
  .tm {
    flex: none;
    margin-left: auto;
    font-size: var(--font-size-small);
    color: var(--faint);
    font-variant-numeric: tabular-nums;
    user-select: none;
  }
  /* 行尾归档按钮：hover 行时浮现 */
  .arc {
    flex: none;
    display: grid;
    place-items: center;
    width: 22px;
    height: 22px;
    border: none;
    border-radius: 6px;
    background: transparent;
    color: var(--faint);
    font-size: var(--font-size-ui);
    cursor: pointer;
    opacity: 0;
    transition: opacity var(--dur-fast) var(--ease-out), color var(--dur-fast) var(--ease-out),
      background var(--dur-fast) var(--ease-out);
  }
  .branch:hover .arc,
  .arc:focus-visible {
    opacity: 1;
  }
  /* 收藏星标：hover 行时浮现；已收藏常亮金色（收藏组里的行一眼可辨） */
  .star {
    flex: none;
    display: grid;
    place-items: center;
    width: 22px;
    height: 22px;
    border: none;
    border-radius: 6px;
    background: transparent;
    color: var(--faint);
    line-height: 1;
    cursor: pointer;
    opacity: 0;
    transition: opacity var(--dur-fast) var(--ease-out), color var(--dur-fast) var(--ease-out),
      background var(--dur-fast) var(--ease-out);
  }
  .star svg {
    width: 14px;
    height: 14px;
  }
  .branch:hover .star,
  .star:focus-visible {
    opacity: 1;
  }
  .star.on {
    opacity: 1;
    color: #e3b341;
  }
  .star:hover {
    color: #e3b341;
    background: var(--bg);
  }
  .star:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }
  /* 收藏组标题：金色星标点题（SVG 图标，不用 emoji/文字符号） */
  .glabel.fav {
    display: flex;
    align-items: center;
    gap: 5px;
    color: #b0862a;
  }
  .glabel.fav svg {
    width: 12px;
    height: 12px;
  }
  .arc svg {
    width: 13px;
    height: 13px;
  }
  .arc:hover {
    color: var(--fg);
    background: var(--bg);
  }
  .arc:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }
  .empty {
    padding: 18px 12px;
    text-align: center;
    font-size: var(--font-size-ui);
    color: var(--faint);
  }
</style>
