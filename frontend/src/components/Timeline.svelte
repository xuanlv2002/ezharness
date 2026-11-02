<script lang="ts">
  import { store, type Block } from '../lib/store.svelte'
  import { buildRows } from '../lib/rows'
  import Logo from './Logo.svelte'
  import EzAvatar from './EzAvatar.svelte'
  import ThreadRows from './ThreadRows.svelte'
  import QuickStart from './QuickStart.svelte'

  let el: HTMLDivElement
  let stick = true

  /* 右侧轮次导航：每轮用户输入一行，点击回溯、滚动跟随高亮（纯图片轮次也算） */
  const turns = $derived(
    store.blocks.filter(
      (b): b is Extract<Block, { kind: 'user' }> =>
        b.kind === 'user' && (!!b.text.trim() || !!b.images?.length),
    ),
  )
  /* 轮次导航最多 5 个刻度（取最近 5 轮）：长会话下整列刻度太占视觉，
     更早的轮次靠滚动/下拉历史查看 */
  const navTurns = $derived(turns.length > 5 ? turns.slice(-5) : turns)
  let activeUid = $state(0)

  function updateActive() {
    if (!el) return
    const base = el.getBoundingClientRect().top
    const line = base + Math.min(el.clientHeight * 0.35, 280)
    let cur = 0
    for (const n of el.querySelectorAll<HTMLElement>('[data-uid]')) {
      if (n.getBoundingClientRect().top <= line) cur = Number(n.dataset.uid) || 0
      else break
    }
    activeUid = cur
  }

  /* ── 吸底（stick）机制 ──
  selfScroll：程序设 scrollTop 引发的 scroll 事件不算用户滚动（否则
  吸底回弹会被误判成"用户滚回底部"又把 stick 置真）。
  lastUserScrollAt：用户滚动（滚动条拖动/触摸/wheel）后 300ms 内暂停
  跟随——流式 tick 不再把刚滚上去的视口拽回底部，脱离吸附不再费劲。
  滞回：<30px 恢复跟随，>120px 脱离，中间保持，杜绝边界抖动。 */
  let selfScroll = false
  let lastUserScrollAt = 0
  /* 快速跳转：向上滚给"回顶"，向下滚给"回底"；已在某端则改为去另一端 */
  let lastTop = 0
  let jumpDir = $state<'' | 'top' | 'bottom'>('')

  function follow() {
    if (!el) return
    selfScroll = true
    el.scrollTop = el.scrollHeight
    lastTop = el.scrollTop
  }

  function jump(direction: 'top' | 'bottom') {
    if (!el) return
    if (direction === 'bottom') stick = true
    el.scrollTo({ top: direction === 'top' ? 0 : el.scrollHeight, behavior: 'smooth' })
  }

  function jumpTo(uid: number) {
    if (!el) return
    const node = el.querySelector<HTMLElement>(`[data-uid="${uid}"]`)
    if (!node) return
    const top = el.scrollTop + node.getBoundingClientRect().top - el.getBoundingClientRect().top - 24
    el.scrollTo({ top, behavior: 'smooth' })
  }

  /* 行模型见 lib/rows（主时间线与分身抽屉共用同一组装） */
  const rows = $derived(buildRows(store.blocks))

  /* 空状态判定：note/status/resourcechange/endtick/imgload 是系统自动记录
     （压缩翻页后的新会话仅含一条 <end_reason> 收尾），不算对话内容——
     只剩系统记录时仍展示欢迎页 */
  const empty = $derived(
    !store.blocks.some(
      (b) =>
        b.kind !== 'note' &&
        b.kind !== 'status' &&
        b.kind !== 'resourcechange' &&
        b.kind !== 'endtick' &&
        b.kind !== 'imgload',
    ),
  )

  let openGroups = $state<Set<number>>(new Set())

  function toggleGroup(key: number) {
    const s = new Set(openGroups)
    if (s.has(key)) s.delete(key)
    else s.add(key)
    openGroups = s
  }

  /* ── 下拉加载（pull-to-load）：置顶后 wheel 向上/触屏下拉累积拉动量，
     达阈值拉出上一会话；指示器随拉动量渐显，内容展开动画 ── */
  let pull = $state(0)
  let loading = $state(false)
  let settle = $state(false) // 松手回弹：换用更慢的缓动，跟着手指的拉动用短过渡
  let settleTimer: ReturnType<typeof setTimeout> | undefined
  const THRESHOLD = 64
  const MAX_PULL = 96

  const armed = $derived(pull >= THRESHOLD)
  /* 没有更早内容只在拉动过程中可见，松手即随 pull 归零一起消失 */
  const showNoMore = $derived(!store.hasPrev && pull > 0)
  const showPull = $derived(pull > 0 || loading)

  function atTop(): boolean {
    return !!el && el.scrollTop <= 0
  }

  /* 松手回弹：切到更慢的回弹缓动后归零；再次拉动时恢复跟手过渡 */
  function cancelPull() {
    if (pull <= 0) return
    clearTimeout(settleTimer)
    settle = true
    pull = 0
    settleTimer = setTimeout(() => (settle = false), 200)
  }

  function startPull() {
    clearTimeout(settleTimer)
    settle = false
  }

  let wheelTimer: ReturnType<typeof setTimeout> | undefined

  /* wheel 无松手事件：按滚动量连续累积拉动（不再一格跳 14px），
     停顿即"松手"——未达阈值立刻回弹；达阈值停顿后加载。没有上一级
     时同样跟手拉出提示，停下即回弹，不再弹出一块固定高度的提示条 */
  function onWheel(e: WheelEvent) {
    if (!el) return
    if (e.deltaY < 0) stick = false /* 向上滚 = 明确脱离跟随：别等距离阈值，下一个流式 tick 就不该再被拉回底部 */
    if (e.deltaY < 0 && atTop()) {
      if (loading) return
      clearTimeout(wheelTimer)
      startPull()
      // deltaMode 归一成像素：行/页模式的小数值不能直接当像素累积
      const dy = e.deltaY * (e.deltaMode === 1 ? 16 : e.deltaMode === 2 ? el.clientHeight || 1 : 1)
      pull = Math.min(MAX_PULL, pull + Math.max(2, Math.min(28, -dy * 0.35)))
      /* 松手等待与回弹同一节奏：到阈值直接触发加载（trigger 内部先把
         位移归零），未到阈值回弹；两者都不在顶部滞留 */
      wheelTimer = store.hasPrev && pull >= THRESHOLD
        ? setTimeout(() => void trigger(), 40)
        : setTimeout(cancelPull, 40)
    } else if (e.deltaY > 0) {
      clearTimeout(wheelTimer)
      cancelPull()
    }
  }

  let touchY = 0
  let touching = false

  function onTouchStart(e: TouchEvent) {
    if (!atTop()) return
    touching = true
    touchY = e.touches[0].clientY
    startPull()
  }

  function onTouchMove(e: TouchEvent) {
    if (!touching || loading) return
    const d = e.touches[0].clientY - touchY
    if (d <= 0) {
      cancelPull()
      return
    }
    pull = Math.min(MAX_PULL, d * 0.4) // 阻尼；没有上一级也跟手拉出提示
  }

  function onTouchEnd() {
    touching = false
    if (store.hasPrev && armed) void trigger()
    else cancelPull()
  }

  async function trigger() {
    if (loading) return
    if (!store.hasPrev) {
      cancelPull()
      return
    }
    clearTimeout(wheelTimer)
    loading = true
    /* 加载态由悬浮指示器自己显示，拉起的正文立即回弹，不再保持位移 */
    settle = true
    clearTimeout(settleTimer)
    settleTimer = setTimeout(() => (settle = false), 200)
    pull = 0
    const prevHeight = el?.scrollHeight ?? 0
    await store.loadPrev()
    loading = false
    // prepend 高度补偿：视口停在原有内容处，不跳顶
    if (el) el.scrollTop = el.scrollHeight - prevHeight
  }

  function onScroll() {
    if (!el) return
    if (selfScroll) {
      /* 程序滚动（吸底回弹）：不动 stick，也别记成用户滚动 */
      selfScroll = false
      lastTop = el.scrollTop
      updateActive()
      return
    }
    lastUserScrollAt = Date.now()
    const top = el.scrollTop
    const dist = el.scrollHeight - top - el.clientHeight
    const goingUp = top < lastTop
    lastTop = top
    if (dist < 30) stick = true
    else if (dist > 120) stick = false
    /* 不足一屏时没有可跳的距离 */
    if (el.scrollHeight <= el.clientHeight + 24) jumpDir = ''
    else if (top <= 24) jumpDir = 'bottom'
    else if (dist <= 24) jumpDir = 'top'
    else jumpDir = goingUp ? 'top' : 'bottom'
    updateActive()
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
    /* 用户刚滚动过就别抢滚动位置（300ms 冷却）；updateActive 只在
    scroll 事件里跑——每 tick 遍历全部块节点量矩形会强制布局，
    流式高频 tick 下整页卡死的主凶之一 */
    if (el && stick && !pull && Date.now() - lastUserScrollAt > 300) follow()
  })
</script>

<div class="tlwrap">
  <div class="timeline" bind:this={el} onscroll={onScroll} onwheel={onWheel}
    ontouchstart={onTouchStart} ontouchmove={onTouchMove} ontouchend={onTouchEnd}>
  <div
    class="inner"
    class:settle
    style="transform:translateY({pull}px)">
    {#if !empty}
      <ThreadRows
        {rows}
        liveBusy={store.busy}
        reveal={store.batchIds}
        avatarOutside
        {openGroups}
        onToggleGroup={toggleGroup}
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
        <p class="sub">它可全权操作本机——文件、终端、浏览器。</p>
        <QuickStart />
        {#if store.hasPrev}
          <p class="hint">↑ 下拉可查看上一话题</p>
        {/if}
      </div>
    {/if}
  </div>
  </div>
  <!-- 上拉指示器：脱离滚动内容悬浮在顶部，不参与布局；内容本身用
       transform 跟手，回弹只动合成层，不触发整条时间线重排 -->
  <div
    class="pullind"
    class:armed={armed && store.hasPrev}
    class:show={showPull}
    aria-hidden={!showPull}>
    <span class="arrow" class:spin={loading}>{loading ? '⟳' : showNoMore ? '·' : '↓'}</span>
    <span class="ptext">
      {#if loading}加载上一话题…{:else if showNoMore}没有更早的会话了{:else if armed}松开加载上一话题{:else}下拉加载上一话题{/if}
    </span>
  </div>
  {#if turns.length >= 2}
    <div class="turnnav">
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
    min-height: 0;
    display: flex;
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
    min-height: 0;
    overscroll-behavior: contain;
  }
  /* 轮次导航：默认仅一列刻度线垂直居中贴右缘，悬浮展开文字卡片。
     高度封顶（约 20 条刻度），超出滚动——滚动条仅悬浮时显形 */
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
    /* 跟手阶段不做过渡：transform 直接跟 wheel 事件走，避免任何迟滞 */
  }
  /* 松手回弹：只动 transform（合成层），不触发时间线重排 */
  .inner.settle {
    transition: transform 0.05s cubic-bezier(0.25, 0.8, 0.3, 1);
  }
  /* 提示悬浮在滚动区顶部，不占布局；内容由 .inner 的 transform 下拉 */
  .pullind {
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
    opacity: 0;
    transform: translateY(-4px);
    transition: opacity 0.06s var(--ease-out), transform 0.06s var(--ease-out);
  }
  .pullind.show {
    opacity: 0.85;
    transform: translateY(0);
  }
  .pullind.armed {
    color: var(--accent, #2563eb);
    opacity: 1;
  }
  .arrow {
    display: inline-block;
    transition: transform 0.18s var(--ease-out);
  }
  .pullind.armed .arrow {
    transform: rotate(180deg);
  }
  .arrow.spin {
    animation: spin 0.9s linear infinite;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
  .ptext {
    white-space: nowrap;
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
  .hint {
    margin-top: 8px;
    font-size: var(--font-size-ui);
    color: var(--faint);
  }
</style>
