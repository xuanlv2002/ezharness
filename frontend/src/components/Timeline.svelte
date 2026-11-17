<script lang="ts">
  import { onMount, tick } from 'svelte'
  import { store } from '../lib/store.svelte'
  import { buildRows, type Row } from '../lib/rows'
  import Logo from './Logo.svelte'
  import EzAvatar from './EzAvatar.svelte'
  import ThreadRows from './ThreadRows.svelte'
  import QuickStart from './QuickStart.svelte'
  import FoldMark from './FoldMark.svelte'

  let el: HTMLDivElement
  let innerEl: HTMLDivElement
  let stick = true

  /* 行窗口按用户轮次切：打开会话先载最近 10 轮，向上每次再载 5 轮。
     初始 10 轮与后续 5 轮走同一个 windowStart：数满之前先遇到
     trim / archive 边界就停在边界上，边界条本身即折叠标记。 */
  const INITIAL_TURNS = 10
  const LOAD_TURNS = 5
  /* 向上滚动加载：箭头出现后持续滚动 0.5s 才载入；中途停下就收起箭头 */
  const PULL_HOLD = 500
  const PULL_IDLE = 80
  const rows = $derived(buildRows(store.blocks))
  let rowStart = $state(0)
  let rowSession = $state('')
  let loadingEarlier = $state(false)
  let pullArrow = $state(false)
  let pullLastAt = 0
  let pullTimer: ReturnType<typeof setTimeout> | undefined

  function isTurn(r: Row): boolean {
    return r.kind === 'user' && (!!r.b.text.trim() || !!r.b.images?.length)
  }

  /* 从 from 向上数 count 个用户轮次；先碰到上下文边界就停在边界上 */
  function windowStart(from: number, count: number): number {
    let n = 0
    for (let i = from; i > 0; i--) {
      const r = rows[i - 1]
      if (r.kind === 'marker') return i - 1
      if (isTurn(r)) {
        n++
        if (n >= count) return i - 1
      }
    }
    return 0
  }

  const autoStart = $derived(windowStart(rows.length, INITIAL_TURNS))
  const start = $derived(
    rowSession === store.activeId ? Math.min(rowStart, Math.max(0, rows.length - 1)) : autoStart,
  )
  const visibleRows = $derived(rows.slice(start))

  const topRow = $derived(start > 0 ? rows[start] : null)
  const boundaryKey = $derived(topRow?.kind === 'marker' ? topRow.key : '')
  const showMore = $derived(start > 0 && !boundaryKey)
  const showPrev = $derived(start === 0 && store.hasPrev)
  const canLoadMore = $derived(start > 0 || store.hasPrev)

  function clearPull() {
    if (!pullArrow && !pullTimer) return
    if (pullTimer) clearTimeout(pullTimer)
    pullTimer = undefined
    pullArrow = false
  }

  /* 只在持续向上滚动时加载：200ms 内没有新的向上滚动事件就取消 */
  function armPull() {
    pullLastAt = Date.now()
    if (pullArrow) return
    pullArrow = true
    pullTimer = setTimeout(() => {
      pullTimer = undefined
      if (!pullArrow) return
      if (Date.now() - pullLastAt > PULL_IDLE) {
        pullArrow = false
        return
      }
      pullArrow = false
      void loadEarlier()
    }, PULL_HOLD)
  }

  async function loadEarlier() {
    if (loadingEarlier || !canLoadMore) return
    clearPull()
    stick = false
    loadingEarlier = true
    let prevHeight = el?.scrollHeight ?? 0
    let prevTop = el?.scrollTop ?? 0
    if (start > 0) {
      rowSession = store.activeId
      rowStart = windowStart(start, LOAD_TURNS)
    } else if (store.hasPrev) {
      const sid = store.activeId
      const markerUid = await store.loadPrev()
      if (store.activeId !== sid) {
        loadingEarlier = false
        return
      }
      /* 等接口期间用户可能继续滚动：补偿基准取当前视口 */
      prevHeight = el?.scrollHeight ?? 0
      prevTop = el?.scrollTop ?? 0
      if (markerUid) {
        const anchor = rows.findIndex((r) => r.kind === 'marker' && r.b.uid === markerUid)
        rowSession = store.activeId
        rowStart = anchor > 0 ? windowStart(anchor, LOAD_TURNS) : 0
      }
    }
    await tick()
    loadingEarlier = false
    if (el) {
      selfScroll = true
      el.scrollTop = prevTop + (el.scrollHeight - prevHeight)
      lastTop = el.scrollTop
      requestAnimationFrame(() => (selfScroll = false))
    }
  }

  /* 右侧轮次导航：每轮用户输入一行，点击回溯、滚动跟随高亮（纯图片轮次也算）。
     收起时只显示最近 5 个刻度；悬浮展开全部轮次，列高封顶后可滚动选择。 */
  const turns = $derived(
    rows
      .filter((r): r is Extract<Row, { kind: 'user' }> => isTurn(r))
      .map((r) => r.b),
  )
  let navHover = $state(false)
  const navTurns = $derived(navHover ? turns : turns.slice(-5))
  let activeUid = $state(0)

  /* 窗口从某轮中间开始时，该轮的 user 行在窗口外：高亮回落到最后一条
     已渲染前的 user 轮次，别让导航整列无当前项 */
  const startUserUid = $derived.by(() => {
    for (let i = start - 1; i >= 0; i--) {
      const r = rows[i]
      if (r.kind === 'user') return r.b.uid
    }
    return 0
  })

  function updateActive() {
    if (!el) return
    const base = el.getBoundingClientRect().top
    const line = base + Math.min(el.clientHeight * 0.35, 280)
    let cur = startUserUid
    for (const n of el.querySelectorAll<HTMLElement>('[data-uid]')) {
      if (n.getBoundingClientRect().top <= line) cur = Number(n.dataset.uid) || 0
      else break
    }
    activeUid = cur
  }

  /* 逐帧合并滚动时的量距：滚动事件一帧可触发多次，每次都全量量矩形
     会强制布局（放大到数千行时就是滚动卡顿来源） */
  let activeRaf = 0
  function scheduleActive() {
    if (activeRaf) return
    activeRaf = requestAnimationFrame(() => {
      activeRaf = 0
      updateActive()
    })
  }

  /* 悬浮展开全部刻度后：把当前轮次滚进导航列视窗（居中）。手动算
     scrollTop——scrollIntoView 会连带滚外层聊天容器 */
  let navEl = $state<HTMLDivElement>()
  $effect(() => {
    const uid = activeUid
    const hovering = navHover
    const box = navEl
    if (!box || !hovering || !uid) return
    const cur = box.querySelector<HTMLButtonElement>('button.cur')
    if (!cur || box.scrollHeight <= box.clientHeight) return
    box.scrollTop = Math.max(0, cur.offsetTop - box.clientHeight / 2 + cur.offsetHeight / 2)
  })

  /* 吸底：程序滚动不算用户滚动；向上滚动立即脱离跟随，向下滚到近底恢复。
     正文懒渲染/图片加载会让内容继续变高，ResizeObserver 兜住这些增长。 */
  let selfScroll = false
  let lastTop = 0
  let jumpDir = $state<'' | 'top' | 'bottom'>('')

  function follow() {
    if (!el) return
    selfScroll = true
    el.scrollTop = el.scrollHeight
    lastTop = el.scrollTop
    requestAnimationFrame(() => (selfScroll = false))
  }

  let followRaf = 0
  function scheduleFollow() {
    if (followRaf) return
    followRaf = requestAnimationFrame(() => {
      followRaf = 0
      if (stick) follow()
    })
  }

  function toBottom() {
    stick = true
    follow()
    scheduleFollow()
  }

  function restoreScroll(pos: number) {
    if (!el) return
    selfScroll = true
    el.scrollTop = pos
    lastTop = el.scrollTop
    requestAnimationFrame(() => (selfScroll = false))
  }

  /* 每个已打开标签的窗口与滚动位置：切回已打开标签时恢复原处，
     只有新打开的标签（无记录）才回底。 */
  type SavedView = { rowStart: number; scrollTop: number; atBottom: boolean }
  const savedViews = new Map<string, SavedView>()
  let pendingView: SavedView | null = null

  let lastSessionId = $state('')
  $effect(() => {
    const id = store.activeId
    if (lastSessionId === id) return
    const prev = lastSessionId
    if (prev && store.tabs.some((t) => t.rootId === prev)) {
      savedViews.set(prev, { rowStart, scrollTop: lastTop, atBottom: stick })
    }
    lastSessionId = id
    pendingView = savedViews.get(id) ?? null
    stick = true
    lastTop = 0
    jumpDir = ''
    activeUid = 0
    clearPull()
  })
  $effect(() => {
    const ids = new Set(store.tabs.map((t) => t.rootId))
    for (const id of savedViews.keys()) if (!ids.has(id)) savedViews.delete(id)
  })
  $effect(() => {
    const id = store.activeId
    if (!rows.length) {
      /* 历史重载会先清空再填入：清空时解除窗口归属，新内容到达后重算 */
      if (rowSession) rowSession = ''
      stick = true
      return
    }
    if (rowSession === id) return
    rowSession = id
    const saved = pendingView
    pendingView = null
    if (saved) {
      rowStart = Math.min(saved.rowStart, Math.max(0, rows.length - 1))
      if (saved.atBottom) {
        stick = true
        void tick().then(toBottom)
      } else {
        stick = false
        void tick().then(() => restoreScroll(saved.scrollTop))
      }
    } else {
      rowStart = windowStart(rows.length, INITIAL_TURNS)
      stick = true
      void tick().then(toBottom)
    }
  })

  /* 实时整理完成：窗口收拢到新的 trim 边界（正在上翻看历史时不收，
     避免把用户正在读的内容抽走） */
  $effect(() => {
    const uid = store.foldAt
    if (!uid) return
    const idx = rows.findIndex((r) => r.kind === 'marker' && r.b.uid === uid)
    if (idx < 0) return
    store.foldAt = 0
    if (!stick) return
    rowSession = store.activeId
    rowStart = idx
    stick = true
    void tick().then(follow)
  })

  function jump(direction: '' | 'top' | 'bottom') {
    if (!el || !direction) return
    if (direction === 'bottom') {
      toBottom()
      return
    }
    stick = false
    clearPull() // 只回当前窗口顶部，不触发向上懒加载
    el.scrollTo({ top: 0, behavior: 'smooth' })
  }

  /* 输入框开始输入：等本轮 DOM 落定后回到底部 */
  $effect(() => {
    if (!store.bottomSeq) return
    void tick().then(toBottom)
  })

  /* 对话页切走时只是 display:none（常驻挂载），隐藏期量不到高度；
     重新显示或内容变高时补一次贴底 */
  onMount(() => {
    const ro = new ResizeObserver(() => {
      if (stick) scheduleFollow()
    })
    ro.observe(el)
    if (innerEl) ro.observe(innerEl)
    return () => {
      ro.disconnect()
      if (followRaf) cancelAnimationFrame(followRaf)
      clearPull()
    }
  })

  async function jumpTo(uid: number) {
    if (!el) return
    const idx = rows.findIndex((r) => r.kind === 'user' && r.b.uid === uid)
    if (idx < 0) return
    stick = false
    /* 目标在渲染窗口外：先把窗口扩到包含它，再定位 */
    if (idx < start) {
      rowSession = store.activeId
      rowStart = idx
      await tick()
    }
    const node = el.querySelector<HTMLElement>(`[data-uid="${uid}"]`)
    if (!node) return
    const top = el.scrollTop + node.getBoundingClientRect().top - el.getBoundingClientRect().top - 24
    el.scrollTo({ top, behavior: 'smooth' })
  }

  /* 通知跳转锚点（decision-<id>）落在渲染窗口外时先扩窗；可见时交给
     ChatView 原有的滚动逻辑。 */
  $effect(() => {
    const target = store.jumpMain
    if (!target) return
    const idx = rows.findIndex(
      (r) =>
        r.kind !== 'user' &&
        r.kind !== 'marker' &&
        r.segs.some((seg) => seg.type === 'one' && seg.b.kind === 'decision' && `decision-${seg.b.id}` === target),
    )
    if (idx < 0 || idx >= start) return
    stick = false
    rowSession = store.activeId
    rowStart = idx
    void (async () => {
      await tick()
      const node = document.getElementById(target)
      if (!node) return
      node.scrollIntoView({ behavior: 'smooth', block: 'center' })
      store.jumpMain = ''
    })()
  })

  /* 空状态判定：note/status/resourcechange/endtick/imgload/trim/archive
     是系统自动记录，不算对话内容——只剩系统记录时仍展示欢迎页 */
  const empty = $derived(
    !store.blocks.some(
      (b) =>
        b.kind !== 'note' &&
        b.kind !== 'status' &&
        b.kind !== 'resourcechange' &&
        b.kind !== 'endtick' &&
        b.kind !== 'imgload' &&
        b.kind !== 'trim' &&
        b.kind !== 'archive',
    ),
  )

  let openGroups = $state<Set<number>>(new Set())

  function toggleGroup(key: number) {
    const s = new Set(openGroups)
    if (s.has(key)) s.delete(key)
    else s.add(key)
    openGroups = s
  }

  function atTop(): boolean {
    return !!el && el.scrollTop <= 0
  }

  function onWheel(e: WheelEvent) {
    if (e.deltaY >= 0) {
      clearPull()
      return
    }
    /* 内容不足一屏时滚不动：别把贴底状态误判成用户上翻 */
    if (!el || el.scrollHeight <= el.clientHeight) {
      clearPull()
      return
    }
    stick = false
    if (!atTop() || !canLoadMore || loadingEarlier) {
      clearPull()
      return
    }
    armPull()
  }

  let touchY = 0
  function onTouchStart(e: TouchEvent) {
    touchY = e.touches[0].clientY
  }
  function onTouchMove(e: TouchEvent) {
    const y = e.touches[0].clientY
    const down = y - touchY
    touchY = y
    if (!atTop() || !canLoadMore || loadingEarlier) {
      clearPull()
      return
    }
    if (down > 0) {
      stick = false
      armPull()
    } else if (down < 0) clearPull()
  }

  function onScroll() {
    if (!el) return
    const top = el.scrollTop
    const dist = el.scrollHeight - top - el.clientHeight
    const goingUp = top < lastTop
    lastTop = top
    if (top > 0) clearPull()
    if (selfScroll) selfScroll = false
    else if (goingUp) stick = false
    else if (dist < 40) stick = true
    /* 跳转钮方向按实际位置更新，程序贴底后也要从「回底」切成「回顶」 */
    if (el.scrollHeight <= el.clientHeight + 24) jumpDir = ''
    else if (top <= 24) jumpDir = 'bottom'
    else if (dist <= 24) jumpDir = 'top'
    else jumpDir = goingUp ? 'top' : 'bottom'
    scheduleActive()
  }

  /* 模型活跃且正文尚未流出时的活动指示：思考（首 token 前 / 只有思考在
  流——思考默认折叠，长时间无正文像卡死）与输出（工具参数流式构造期）两
  态；正文一旦流出即收指示（内容本身是进度）。building 工具的参数就是模
  型流式输出，计"输出"避免 tool_chunk→tool_start 间指示条显隐跳变 */
  /* 实时迭代数（长任务进度；轮末/切分支清零） */
  const iterLabel = $derived(store.currentIter > 0 ? ` · 第 ${store.currentIter} 轮` : '')

  const modelPhase = $derived.by(() => {
    if (!store.modelActive) return ''
    for (let i = store.blocks.length - 1; i >= 0; i--) {
      const b = store.blocks[i]
      if (b.kind === 'assistant') return b.streaming && b.text ? '' : 'think'
      if (b.kind === 'user') return 'think'
      if (b.kind === 'tool' && b.state === 'building') return 'gen'
    }
    return 'think'
  })

  $effect(() => {
    void store.tick
    if (stick) scheduleFollow()
  })
</script>

<div class="tlwrap">
  <div class="timeline" bind:this={el} onscroll={onScroll} onwheel={onWheel}
    ontouchstart={onTouchStart} ontouchmove={onTouchMove}>
  <div class="inner" bind:this={innerEl}>
    {#if showMore}
      <FoldMark kind="more" loadable loading={loadingEarlier} onLoad={() => void loadEarlier()} />
    {:else if showPrev}
      <FoldMark kind="archive" title={store.prevTitle} loadable loading={loadingEarlier}
        onLoad={() => void loadEarlier()} />
    {/if}
    {#if !empty}
      <ThreadRows
        rows={visibleRows}
        liveBusy={store.busy}
        avatarOutside
        eager
        {openGroups}
        onToggleGroup={toggleGroup}
        boundaryKey={boundaryKey}
        boundaryLoading={loadingEarlier}
        onLoadEarlier={() => void loadEarlier()}
      />
    {/if}
    {#if store.retryInfo}
      <div class="thinking retry" title={store.retryInfo.err}>
        <span class="tdot"></span>模型请求失败（{store.retryInfo.err.slice(0, 80) || '未知原因'}），第 {store.retryInfo.attempt}/{store.retryInfo.maxAttempts} 次重试{(store.retryInfo.delayMs / 1000).toFixed(1)}s 后…{iterLabel}
      </div>
    {:else if store.wrapPhase}
      <div class="thinking wrap">
        <span class="tdot"></span>已达迭代上限，正在总结收尾（不再调用工具）…{iterLabel}
      </div>
    {:else if modelPhase || store.lastTool}
      <div class="thinking"><span class="tdot"></span>{store.lastTool
          ? `⚙ ${store.lastTool} 执行中…`
          : modelPhase === 'gen'
            ? '模型输出中…'
            : '模型思考中…'}{iterLabel}</div>
    {/if}
    {#if empty}
      <div class="empty">
        <div class="mark"><EzAvatar size={56} random><Logo size={56} /></EzAvatar></div>
        <p class="lead">想让它做什么？</p>
        <p class="sub">办公，创作，学习 -- 一站式Agent应用</p>
        <QuickStart />
      </div>
    {/if}
  </div>
  </div>
  {#if pullArrow || loadingEarlier}
    <div class="pullhint" class:loading={loadingEarlier}>
      <span class="parrow" class:spin={loadingEarlier}>{loadingEarlier ? '⟳' : '↑'}</span>
      <span>{loadingEarlier ? '加载更早的消息…' : '继续向上滚动'}</span>
    </div>
  {/if}
  {#if turns.length >= 2}
    <div
      class="turnnav"
      class:open={navHover}
      bind:this={navEl}
      onmouseenter={() => (navHover = true)}
      onmouseleave={() => (navHover = false)}
    >
      {#each navTurns as t (t.uid)}
        <button class:cur={t.uid === activeUid} onclick={() => jumpTo(t.uid)}
          title={t.text.length > 40 ? t.text : undefined}>
          <span class="tt">{t.text || '[附件]'}</span>
          <span class="tick"></span>
        </button>
      {/each}
    </div>
  {/if}
  {#if jumpDir}
    <button class="jump" onclick={() => jump(jumpDir)}
      title={jumpDir === 'top' ? '回到顶部' : '回到底部'}
      aria-label={jumpDir === 'top' ? '回到顶部' : '回到底部'}>
      {#if jumpDir === 'top'}
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <path d="M12 19V5M5 12l7-7 7 7" />
        </svg>
      {:else}
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <path d="M12 5v14M19 12l-7 7-7-7" />
        </svg>
      {/if}
    </button>
  {/if}
</div>

  <style>
  /* 时间线容器：滚动区 + 右侧轮次导航 overlay 的定位父级 */
  .tlwrap {
    position: relative;
    flex: 1;
    /* 同 .chat：宽度跟随主列被抽屉挤窄，不被消息内容的 min-content 撑开 */
    min-width: 0;
    min-height: 0;
    display: flex;
  }
  /* 上滚加载指示：悬浮在滚动区顶部，不占布局；持续滚动 0.2s 后触发加载 */
  .pullhint {
    position: absolute;
    top: 10px;
    left: 0;
    right: 0;
    z-index: 5;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    pointer-events: none;
    font-size: var(--font-size-ui);
    color: var(--faint);
    font-family: var(--font-mono);
    animation: pull-in 0.12s var(--ease-out) both;
  }
  .pullhint.loading {
    color: var(--accent);
  }
  .parrow {
    display: inline-block;
  }
  .parrow.spin {
    animation: spin 0.9s linear infinite;
  }
  @keyframes pull-in {
    from {
      opacity: 0;
      transform: translateY(-6px);
    }
    to {
      opacity: 0.9;
      transform: none;
    }
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
  /* 快速跳转：向上滚显示回顶、向下滚显示回底，已在端上则指向另一端。
    右侧与轮次导航同列，贴内容列右缘（窄窗口退回窗口边） */
  .jump {
    position: absolute;
    right: max(12px, calc(50% - 380px));
    bottom: 12px;
    z-index: 6;
    width: 32px;
    height: 32px;
    display: grid;
    place-items: center;
    border: 1px solid var(--line);
    border-radius: 50%;
    background: var(--glass);
    backdrop-filter: var(--glass-blur);
    color: var(--muted);
    box-shadow: 0 4px 16px rgb(0 0 0 / 8%);
    transition:
      color var(--dur-fast) var(--ease-out),
      border-color var(--dur-fast) var(--ease-out),
      background var(--dur-fast) var(--ease-out);
    animation: jump-in 140ms var(--ease-out) both;
  }
  .jump:hover {
    color: var(--fg);
    border-color: var(--line-strong);
    background: var(--bg);
  }
  .jump svg {
    width: 15px;
    height: 15px;
  }
  @keyframes jump-in {
    from {
      opacity: 0;
      transform: translateY(4px);
    }
    to {
      opacity: 1;
      transform: none;
    }
  }
  .timeline {
    flex: 1;
    overflow-y: auto;
    overflow-x: hidden; /* 会话区不要横向滚动条（长串断行后此为兜底） */
    /* 滚动容器也是 flex 项：显式归零最小宽，长串只换行不把列撑宽 */
    min-width: 0;
    min-height: 0;
    overscroll-behavior: contain;
  }
  /* 轮次导航：收起时只露最近 5 个刻度（一列点线垂直居中贴右缘）；悬浮
     展开全部轮次与文字卡片，高度封顶后可滚动选择——滚动条仅悬浮时显形 */
  .turnnav {
    position: absolute;
    /* 贴内容列右缘（主列已满宽，直接 right:10 会跑到窗口最右压住悬浮列） */
    right: max(10px, calc(50% - 380px));
    top: 50%;
    transform: translateY(-50%);
    z-index: 4;
    display: flex;
    flex-direction: column;
    gap: 5px;
    max-height: 160px;
    overflow-y: auto;
    padding: 4px;
    border: 1px solid transparent;
    border-radius: 10px;
    scrollbar-width: none;
    transition: background var(--dur-fast) var(--ease-out), border-color var(--dur-fast) var(--ease-out),
      box-shadow var(--dur-fast) var(--ease-out);
  }
  /* 悬浮：展开更高（约 20 个刻度），全量轮次可滚动选择 */
  .turnnav.open {
    max-height: 320px;
  }
  .turnnav::-webkit-scrollbar {
    display: none;
    width: 4px;
  }
  .turnnav:hover {
    background: color-mix(in srgb, var(--bg) 88%, transparent);
    backdrop-filter: blur(8px);
    border-color: var(--line);
    box-shadow: 0 4px 16px rgb(0 0 0 / 6%);
    scrollbar-width: thin;
    scrollbar-color: var(--line-strong) transparent;
  }
  .turnnav:hover::-webkit-scrollbar {
    display: block;
  }
  .turnnav::-webkit-scrollbar-thumb {
    background: var(--line-strong);
    border-radius: 2px;
  }
  .turnnav button {
    display: flex;
    align-items: center;
    gap: 0;
    border: none;
    background: transparent;
    padding: 2px;
    border-radius: 5px;
    cursor: pointer;
    transition: gap var(--dur-fast) var(--ease-out), padding var(--dur-fast) var(--ease-out),
      background var(--dur-fast) var(--ease-out);
  }
  .turnnav button:hover {
    background: var(--bg-soft);
  }
  .turnnav:hover button {
    gap: 8px;
    padding: 3px 8px;
  }
  .turnnav .tt {
    display: none;
    flex: 0 1 auto;
    min-width: 0;
    max-width: 200px;
    font-size: var(--font-size-small);
    color: var(--faint);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .turnnav:hover .tt {
    display: inline;
  }
  .turnnav button:hover .tt {
    color: var(--muted);
  }
  .turnnav button.cur .tt {
    color: var(--fg);
    font-weight: 500;
  }
  .turnnav .tick {
    flex: none;
    margin-left: auto;
    width: 12px;
    height: 2px;
    border-radius: 1px;
    background: var(--line-strong);
    transition: width var(--dur-fast) var(--ease-out), background var(--dur-fast) var(--ease-out);
  }
  .turnnav button.cur .tick {
    width: 18px;
    background: var(--fg);
  }
  .inner {
    max-width: 780px;
    margin: 0 auto;
    /* 左右 58：48 给头像、10 是头像与正文的间隙 */
    padding: 40px 58px 24px;
    display: flex;
    flex-direction: column;
    gap: 20px;
    min-height: 100%; /* 空状态时也撑满可视区，使欢迎语垂直居中 */
    --av-size: 48px; /* 头像列宽：组头像与用户行占位共用 */
  }
  .thinking {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: var(--font-size-ui);
    color: var(--faint);
    padding: 2px 0;
  }
  .tdot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--line-strong);
    animation: breath 1.4s ease-in-out infinite;
  }
  /* 重试提示：琥珀色（区别于正常思考的灰） */
  .thinking.retry {
    color: #d29922;
  }
  .thinking.retry .tdot {
    background: #d29922;
  }
  /* 软收尾提示：蓝色（区别于常规思考） */
  .thinking.wrap {
    color: var(--accent);
  }
  .thinking.wrap .tdot {
    background: var(--accent);
  }
  @keyframes breath {
    0%,
    100% {
      opacity: 0.25;
      transform: scale(0.85);
    }
    50% {
      opacity: 1;
      transform: scale(1.05);
    }
  }
  .empty {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 6px;
    padding-bottom: 24px; /* 视觉重心略上提；留白过大时矮窗初始化就会溢出出滚动条 */
    color: var(--muted);
  }
  .mark {
    display: inline-grid;
    place-items: center;
    width: 56px;
    height: 56px;
    margin-bottom: 10px;
  }
  .lead {
    font-size: var(--font-size-content);
    font-weight: 600;
    color: var(--fg);
  }
  .sub {
    font-size: var(--font-size-ui);
    color: var(--muted);
  }
</style>
