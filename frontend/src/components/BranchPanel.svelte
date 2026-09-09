<script lang="ts">
  /*
  分支面板：会话树的"线"列表（叶子到根的路线），按更新时间分组。
  条目 = 分支（根 ID 身份，compact 换代不换条目）；指示器：
  橙点呼吸 = 有轮运行中（含后台分支）；⚠ 待审批。操作：点击切换
  （状态/审批随之切换）、顶部开新线、行尾归档。
  悬浮覆盖抽屉：浮在聊天内容之上（不占布局宽度），点外部收起。
  */
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
    void store.switchBranch(b.id)
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
      const label = groupLabel(b.updatedAt || b.createdAt)
      const last = out[out.length - 1]
      if (last && last.label === label) last.items.push(b)
      else out.push({ label, items: [b] })
    }
    return out
  })
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
      <button class="newbig" onclick={() => void store.newBranch()} title="开一条新分支（新话题）">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <circle cx="12" cy="12" r="9" />
          <path d="M9 12h6M12 9v6" />
        </svg>
        开启新分支
      </button>
      {#each groups as g (g.label)}
        <div class="glabel">{g.label}</div>
        {#each g.items as b (b.id)}
          <div class="branch" class:cur={b.active || b.id === store.activeId} role="button" tabindex="0"
            onclick={() => switchTo(b)}
            onkeydown={(e) => (e.key === 'Enter' || e.key === ' ') && switchTo(b)}>
            <span class="st" class:on={b.running || b.waiting} class:run={b.running}></span>
            <span class="t-text">{b.title || '未命名分支'}</span>
            {#if b.kind === 'fork'}<i class="kbadge" title={b.origin?.title ? `来自 session：${b.origin.title}` : 'fork 产生的分支'}>⑂</i>{/if}
            {#if b.archiving || b.id === store.archivingRootId}<em class="flag arc-ing">归档中</em>{/if}
            {#if b.waiting}<em class="flag">待审批</em>{/if}
            <span class="tm" title={b.updatedAt || b.createdAt ? new Date(b.updatedAt || b.createdAt!).toLocaleString() : ''}>
              {timeLabel(b.updatedAt || b.createdAt)}
            </span>
            <button class="arc" disabled={b.archiving || b.id === store.archivingRootId || b.running}
              onclick={(e) => { e.stopPropagation(); void store.compactTopic(b.id) }}
              title="归档此话题：总结归档并开新会话（同线换代，树上加一代）">⇪</button>
          </div>
        {/each}
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
    background: var(--bg);
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
    max-height: 100%;
    overflow: hidden;
  }
  .head {
    flex: none;
    display: flex;
    align-items: center;
    padding: 8px 10px;
  }
  .head h2 {
    font-size: 11.5px;
    font-weight: 700;
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
    font-size: 12px;
    font-weight: 550;
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
    font-size: 10.5px;
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
    font-size: 12px;
    color: var(--fg);
    font-weight: 550;
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
    font-size: 10px;
    font-style: normal;
    color: var(--accent);
    background: var(--accent-soft);
    border-radius: 5px;
    padding: 0 5px;
  }
  .flag {
    flex: none;
    font-style: normal;
    font-size: 10px;
    color: #f0883e;
  }
  .arc-ing {
    color: #8957e5;
  }
  /* 行尾更新时间：右推（今天=时刻，更早=日期），hover 归档按钮时与之并排 */
  .tm {
    flex: none;
    margin-left: auto;
    font-size: 10px;
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
    font-size: 12px;
    cursor: pointer;
    opacity: 0;
    transition: opacity var(--dur-fast) var(--ease-out), color var(--dur-fast) var(--ease-out),
      background var(--dur-fast) var(--ease-out);
  }
  .branch:hover .arc,
  .arc:focus-visible {
    opacity: 1;
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
    font-size: 11.5px;
    color: var(--faint);
  }
</style>
