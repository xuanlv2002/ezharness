<script lang="ts">
  import { store, type Block } from '../lib/store.svelte'
  import Logo from './Logo.svelte'
  import MessageItem from './MessageItem.svelte'
  import ToolBlock from './ToolBlock.svelte'
  import ToolGroup from './ToolGroup.svelte'
  import ForkCard from './ForkCard.svelte'
  import DecisionCard from './DecisionCard.svelte'
  import StatusTagCard from './StatusTagCard.svelte'
  import ResChangeCard from './ResChangeCard.svelte'

  let el: HTMLDivElement
  let stick = true

  /* 右侧轮次导航：每轮用户输入一行，点击回溯、滚动跟随高亮（纯图片轮次也算） */
  const turns = $derived(
    store.blocks.filter((b) => b.kind === 'user' && (b.text.trim() || b.images?.length)),
  )
  let activeUid = $state('')

  function updateActive() {
    if (!el) return
    const base = el.getBoundingClientRect().top
    const line = base + Math.min(el.clientHeight * 0.35, 280)
    let cur = ''
    for (const n of el.querySelectorAll<HTMLElement>('[data-uid]')) {
      if (n.getBoundingClientRect().top <= line) cur = n.dataset.uid ?? ''
      else break
    }
    activeUid = cur
  }

  function jumpTo(uid: string) {
    if (!el) return
    const node = el.querySelector<HTMLElement>(`[data-uid="${uid}"]`)
    if (!node) return
    const top = el.scrollTop + node.getBoundingClientRect().top - el.getBoundingClientRect().top - 24
    el.scrollTo({ top, behavior: 'smooth' })
  }

  /* 连续工具段分组：≥ TOOL_GROUP_MIN 折成一条摘要（审批卡等非 tool 块打断分组） */
  const TOOL_GROUP_MIN = 5
  type Seg = { type: 'one'; b: Block } | { type: 'tools'; blocks: Block[] }
  const segs = $derived.by(() => {
    const out: Seg[] = []
    let cur: Block[] = []
    const flush = () => {
      if (cur.length) out.push({ type: 'tools', blocks: cur })
      cur = []
    }
    for (const b of store.blocks) {
      if (b.kind === 'tool') cur.push(b)
      else {
        flush()
        out.push({ type: 'one', b })
      }
    }
    flush()
    return out
  })

  /* 空状态判定：note/status/reschange/endtick/imgload 是系统自动记录
     （压缩翻页后的新会话仅含一条 <end_reason> 收尾），不算对话内容——
     只剩系统记录时仍展示欢迎页 */
  const empty = $derived(
    !store.blocks.some(
      (b) =>
        b.kind !== 'note' &&
        b.kind !== 'status' &&
        b.kind !== 'reschange' &&
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
    const prevHeight = el?.scrollHeight ?? 0
    await store.loadPrev()
    loading = false
    pull = 0
    if (!store.hasPrev) hintNoMore()
    // prepend 高度补偿：视口停在原有内容处，不跳顶
    if (el) el.scrollTop = el.scrollHeight - prevHeight
  }

  function onScroll() {
    if (!el) return
    stick = el.scrollHeight - el.scrollTop - el.clientHeight < 120
    updateActive()
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
    updateActive()
  })
</script>

<div class="tlwrap">
  <div class="timeline" bind:this={el} onscroll={onScroll} onwheel={onWheel}
    ontouchstart={onTouchStart} ontouchmove={onTouchMove} ontouchend={onTouchEnd}>
  <div class="inner">
    <!-- 下拉指示器：随拉动量展开，文案三态（noMore 固定高度让"没有更早"可见） -->
    {#if pull > 0 || loading || noMore}
      <div class="pullind" style="height:{noMore ? 28 : Math.max(pull, loading ? 40 : 0)}px" class:armed>
        <span class="arrow" class:spin={loading}>{loading ? '⟳' : noMore ? '·' : '↓'}</span>
        <span class="ptext">
          {#if loading}加载上一话题…{:else if noMore}没有更早的会话了{:else if armed}松开加载上一话题{:else}下拉加载上一话题{/if}
        </span>
      </div>
    {/if}
    {#if !empty}
      {#each segs as seg}
      {#if seg.type === 'tools'}
        {@const key = seg.blocks[0].uid}
        {#if seg.blocks.length >= TOOL_GROUP_MIN}
          <ToolGroup blocks={seg.blocks} open={openGroups.has(key)} onToggle={() => toggleGroup(key)} />
        {:else}
          {#each seg.blocks as b (b.uid)}
            <div class:reveal={store.batchIds.has(b.uid)}>
              <ToolBlock data={b} />
            </div>
          {/each}
        {/if}
      {:else if seg.b.kind === 'user'}
        {@const ub = seg.b}
        <div class:reveal={store.batchIds.has(ub.uid)} data-uid={ub.uid}>
          <MessageItem
            text={ub.text}
            images={ub.images}
            files={ub.files}
            role="user"
            onFork={ub.owner && ub.msgIdx !== undefined ? () => void store.forkFrom(ub.owner!, ub.msgIdx!) : undefined}
          />
        </div>
      {:else if seg.b.kind === 'assistant'}
        {@const ab = seg.b}
        <div class:reveal={store.batchIds.has(ab.uid)}>
          <MessageItem
            text={ab.text}
            reasoning={ab.reasoning}
            streaming={ab.streaming}
            role="assistant"
            onFork={ab.owner && ab.msgIdx !== undefined && !ab.streaming ? () => void store.forkFrom(ab.owner!, ab.msgIdx!) : undefined}
          />
        </div>
      {:else if seg.b.kind === 'fork'}
        <div class:reveal={store.batchIds.has(seg.b.uid)}>
          <ForkCard fork={store.forks[seg.b.forkId]} />
        </div>
      {:else if seg.b.kind === 'decision'}
        <div id={`decision-${seg.b.id}`} class:reveal={store.batchIds.has(seg.b.uid)}>
          <DecisionCard data={seg.b} />
        </div>
      {:else if seg.b.kind === 'status'}
        <div class:reveal={store.batchIds.has(seg.b.uid)}>
          <StatusTagCard data={seg.b.data} raw={seg.b.text} />
        </div>
      {:else if seg.b.kind === 'reschange'}
        <div class:reveal={store.batchIds.has(seg.b.uid)}>
          <ResChangeCard items={seg.b.items} />
        </div>
      {:else if seg.b.kind === 'note'}
        <div class="note" class:reveal={store.batchIds.has(seg.b.uid)}>
          <span class="line"></span>
          {seg.b.text}
          <span class="line"></span>
        </div>
      {:else if seg.b.kind === 'imgload'}
        <div class="imgload" class:reveal={store.batchIds.has(seg.b.uid)} title={seg.b.paths.join('\n')}>
          {#each seg.b.paths as p, i (p)}
            {#if seg.b.images[i]}
              <img src={`data:${seg.b.images[i].mimeType};base64,${seg.b.images[i].data}`} alt={p} loading="lazy"
                onclick={() => void store.editImage(`data:${seg.b.images[i].mimeType};base64,${seg.b.images[i].data}`, p.split(/[/\\]/).pop() || p)}
                title="点击进画板编辑" />
            {:else}
              <!-- 实时路径：工具结果只有路径，缩略图走工作目录文件服务 -->
              <img src={`/api/workspace/file?path=${encodeURIComponent(p)}`} alt={p} loading="lazy"
                onclick={() => void store.editImage(`/api/workspace/file?path=${encodeURIComponent(p)}`, p.split(/[/\\]/).pop() || p)}
                onerror={(e) => ((e.currentTarget as HTMLImageElement).style.display = 'none')}
                title="点击进画板编辑" />
            {/if}
          {/each}
          <span class="label">已加载上下文</span>
        </div>
      {:else if seg.b.kind === 'endtick'}
        <div class="endtick" class:reveal={store.batchIds.has(seg.b.uid)} title={seg.b.title}>
          <span class="dot" class:warn={seg.b.icon === '⚠'}>{seg.b.icon}</span>
          <span class="rtext">{seg.b.title}</span>
        </div>
      {/if}
      {/each}
    {/if}
    {#if thinking || store.lastTool}
      <div class="thinking"><span class="tdot"></span>{store.lastTool ? `⚙ ${store.lastTool} 执行中…` : '模型输出中…'}</div>
    {/if}
    {#if empty}
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
  {#if turns.length >= 2}
    <div class="turnnav">
      {#each turns as t (t.uid)}
        <button class:cur={t.uid === activeUid} onclick={() => jumpTo(t.uid)}
          title={t.text.length > 40 ? t.text : undefined}>
          <span class="tt">{t.text || '[附件]'}</span>
          <span class="tick"></span>
        </button>
      {/each}
    </div>
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
  .timeline {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
    /* 滚动条槽位常驻且双侧对称（both-edges）：显隐不再挤压文本宽度，
    且消息流中心与下方输入框（居中于全宽）对齐——单侧槽会让内容
    整体左偏半个槽宽，输入框看起来左宽右窄 */
    scrollbar-gutter: stable both-edges;
    overscroll-behavior: contain;
  }
  /* 轮次导航：默认仅一列刻度线垂直居中贴右缘，悬浮展开文字卡片。
     高度封顶（约 20 条刻度），超出滚动——滚动条仅悬浮时显形 */
  .turnnav {
    position: absolute;
    right: 10px;
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
    font-size: 11px;
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
    font-weight: 550;
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
  /* 轮次收尾行：靠左紧凑「图标 + 结束原因」，宽约 30% 截断，悬浮看全文 */
  .endtick {
    display: flex;
    align-items: center;
    gap: 7px;
    max-width: 30%;
    padding-left: 48px; /* 与消息文本起点对齐（头像 26 + gap 14 + 内边距 8） */
    font-size: 11px;
    color: var(--faint);
    animation: float-in var(--dur-fast) var(--ease-out) both;
  }
  /* <image_loaded> 图片消息：缩略图小行（read_file 读图已进上下文） */
  .imgload {
    display: flex;
    align-items: center;
    gap: 6px;
    padding-left: 48px;
    font-size: 11px;
    color: var(--faint);
    animation: float-in var(--dur-fast) var(--ease-out) both;
  }
  .imgload img {
    width: 22px;
    height: 22px;
    object-fit: cover;
    border-radius: 5px;
    border: 1px solid var(--line);
    cursor: zoom-in;
  }
  .imgload .label {
    white-space: nowrap;
  }
  .endtick .dot {
    flex: none;
    font-size: 10px;
    line-height: 1;
    color: var(--line-strong);
    user-select: none;
  }
  .endtick .dot.warn {
    color: #f0883e;
  }
  .endtick .rtext {
    flex: 0 1 auto;
    min-width: 0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
</style>
