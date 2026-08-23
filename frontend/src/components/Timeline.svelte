<script lang="ts">
  import { store } from '../lib/store.svelte'
  import Logo from './Logo.svelte'
  import MessageItem from './MessageItem.svelte'
  import ToolBlock from './ToolBlock.svelte'
  import ForkCard from './ForkCard.svelte'
  import DecisionCard from './DecisionCard.svelte'
  import StatusTagCard from './StatusTagCard.svelte'

  let el: HTMLDivElement
  let stick = true

  /* ── 下拉加载（pull-to-load）：置顶后 wheel 向上/触屏下拉累积拉动量，
     达阈值拉出上一会话；指示器随拉动量渐显，内容展开动画 ── */
  let pull = $state(0)
  let loading = $state(false)
  let noMore = $state(false) // 已到链尾的短暂提示
  let noMoreTimer: ReturnType<typeof setTimeout> | undefined
  const THRESHOLD = 64
  const MAX_PULL = 96

  const armed = $derived(pull >= THRESHOLD)

  function atTop(): boolean {
    return !!el && el.scrollTop <= 0
  }

  function cancelPull() {
    pull = 0
  }

  let wheelTimer: ReturnType<typeof setTimeout> | undefined

  /* wheel 无松手事件：每格小幅累积出拉动感，停顿即"松手"——
     未达阈值停顿弹回；达阈值进入 armed，停止滚动 450ms 加载 */
  function onWheel(e: WheelEvent) {
    if (!el) return
    stick = el.scrollHeight - el.scrollTop - el.clientHeight < 120
    if (e.deltaY < 0 && atTop()) {
      if (!store.hasPrev || loading) {
        if (!store.hasPrev) hintNoMore()
        return
      }
      clearTimeout(wheelTimer)
      if (pull >= THRESHOLD) {
        // 已 armed：继续滚只是保持拉动，重置停顿计时
        wheelTimer = setTimeout(() => void trigger(), 200)
        return
      }
      pull = Math.min(MAX_PULL, pull + 14)
      wheelTimer = pull >= THRESHOLD
        ? setTimeout(() => void trigger(), 200)
        : setTimeout(() => cancelPull(), 250)
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
  }

  function onTouchMove(e: TouchEvent) {
    if (!touching) return
    const d = e.touches[0].clientY - touchY
    if (d <= 0) {
      cancelPull()
      return
    }
    if (!store.hasPrev || loading) {
      if (!store.hasPrev) hintNoMore()
      return
    }
    pull = Math.min(MAX_PULL, d * 0.4) // 阻尼
  }

  function onTouchEnd() {
    touching = false
    if (armed) void trigger()
    else cancelPull()
  }

  function hintNoMore() {
    noMore = true
    clearTimeout(noMoreTimer)
    noMoreTimer = setTimeout(() => (noMore = false), 1200)
  }

  async function trigger() {
    if (loading) return
    loading = true
    pull = THRESHOLD // 加载期间保持指示器可见
    await store.loadPrev()
    loading = false
    pull = 0
    if (!store.hasPrev) hintNoMore()
    if (el) el.scrollTop = 0 // 视口停在拉出内容的顶部
  }

  function onScroll() {
    if (!el) return
    stick = el.scrollHeight - el.scrollTop - el.clientHeight < 120
  }

  /* 模型调用中但正文尚未流出（首 token 前 / 纯工具调用构造期）→ 思考指示 */
  const thinking = $derived.by(() => {
    if (!store.modelActive) return false
    for (let i = store.blocks.length - 1; i >= 0; i--) {
      const b = store.blocks[i]
      if (b.kind === 'assistant') {
        return !(b.streaming && (b.text || b.reasoning))
      }
      if (b.kind === 'user') return true
      if (b.kind === 'tool' && b.state === 'building') return false
    }
    return true
  })

  $effect(() => {
    void store.tick
    if (el && stick && !pull) el.scrollTop = el.scrollHeight
  })
</script>

<div class="timeline" bind:this={el} onscroll={onScroll} onwheel={onWheel}
  ontouchstart={onTouchStart} ontouchmove={onTouchMove} ontouchend={onTouchEnd}>
  <div class="inner">
    <!-- 下拉指示器：随拉动量展开，文案三态 -->
    {#if pull > 0 || loading || noMore}
      <div class="pullind" style="height:{Math.max(pull, loading ? 40 : 0)}px" class:armed>
        <span class="arrow" class:spin={loading}>{loading ? '⟳' : noMore ? '·' : '↓'}</span>
        <span class="ptext">
          {#if loading}加载上一话题…{:else if noMore}没有更早的会话了{:else if armed}松开加载上一话题{:else}下拉加载上一话题{/if}
        </span>
      </div>
    {/if}
    {#each store.blocks as b (b.uid)}
      {#if b.kind === 'user'}
        <div class:reveal={store.batchIds.has(b.uid)}>
          <MessageItem text={b.text} role="user" />
        </div>
      {:else if b.kind === 'assistant'}
        <div class:reveal={store.batchIds.has(b.uid)}>
          <MessageItem text={b.text} reasoning={b.reasoning} streaming={b.streaming} role="assistant" />
        </div>
      {:else if b.kind === 'tool'}
        <div class:reveal={store.batchIds.has(b.uid)}>
          <ToolBlock data={b} />
        </div>
      {:else if b.kind === 'fork'}
        <div class:reveal={store.batchIds.has(b.uid)}>
          <ForkCard fork={store.forks[b.forkId]} />
        </div>
      {:else if b.kind === 'decision'}
        <div id={`decision-${b.id}`} class:reveal={store.batchIds.has(b.uid)}>
          <DecisionCard data={b} />
        </div>
      {:else if b.kind === 'status'}
        <div class:reveal={store.batchIds.has(b.uid)}>
          <StatusTagCard data={b.data} raw={b.text} />
        </div>
      {:else if b.kind === 'note'}
        <div class="note" class:reveal={store.batchIds.has(b.uid)}>
          <span class="line"></span>
          {b.text}
          <span class="line"></span>
        </div>
      {/if}
    {/each}
    {#if thinking}
      <div class="thinking"><span class="tdot"></span>模型输出中…</div>
    {/if}
    {#if store.blocks.length === 0}
      <div class="empty">
        <div class="mark"><Logo size={56} /></div>
        <p>向 ezharness 发出第一条指令——它可全权操作本机。</p>
        {#if store.hasPrev}
          <p class="hint">↑ 下拉可查看上一话题</p>
        {/if}
      </div>
    {/if}
  </div>
</div>

<style>
  .timeline {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
  }
  .inner {
    max-width: 780px;
    margin: 0 auto;
    padding: 40px 48px 24px;
    display: flex;
    flex-direction: column;
    gap: 20px;
    min-height: 100%; /* 空状态时也撑满可视区，使欢迎语垂直居中 */
  }
  .pullind {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    overflow: hidden;
    font-size: 11.5px;
    color: var(--faint);
    font-family: var(--font-mono);
    transition: height 0.12s var(--ease-out), opacity 0.12s var(--ease-out);
    opacity: 0.85;
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
  /* 拉出的上一会话内容：自上而下展开 */
  .reveal {
    animation: reveal-down 0.36s var(--ease-out) both;
  }
  @keyframes reveal-down {
    from {
      opacity: 0;
      transform: translateY(-10px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }
  .thinking {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12px;
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
    padding-bottom: 80px; /* 补偿下方输入框高度，视觉重心略上提 */
    color: var(--muted);
  }
  .mark {
    display: inline-grid;
    place-items: center;
    width: 56px;
    height: 56px;
    margin-bottom: 16px;
  }
  .hint {
    margin-top: 8px;
    font-size: 11.5px;
    font-family: var(--font-mono);
    color: var(--faint);
  }
  .note {
    display: flex;
    align-items: center;
    gap: 14px;
    font-size: 11.5px;
    color: var(--faint);
    font-family: var(--font-mono);
    padding: 4px 0;
    animation: float-in var(--dur-fast) var(--ease-out) both;
  }
  .note .line {
    flex: 1;
    height: 1px;
    background: var(--line);
  }
</style>
