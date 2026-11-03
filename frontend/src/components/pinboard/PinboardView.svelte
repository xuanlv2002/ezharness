<script lang="ts">
  /*
  便签页：自由摆放的待办板 + 按天归档的完成时间线，两者共用一套卡片
  数据，靠「划线完成」把卡片从板上转到时间线。

  · 板上：双击空白/「记一笔」新建（Enter 提交、Shift+Enter 换行、Esc
    放弃、空内容丢弃），卡片可拖动/缩放/双击编辑，✓ 划线入库。
  · 时间线：左新右旧按天铺开，悬停抽出，点某天就地展开当天详情（在板
    下半部展开，不覆盖时间线），拖卡片跨天改归属、拖回板上变回待办。
  */
  import { onDestroy, onMount, tick } from 'svelte'
  import type { PinRecord } from '../../lib/api'
  import { store } from '../../lib/store.svelte'
  import {
    CARD_DEF_H,
    CARD_DEF_W,
    PAD,
    addCard,
    addPendingSession,
    board,
    byDayOrder,
    completeCard,
    dayOf,
    ensurePinboard,
    reorderDay,
    restoreRecord,
    saveNow,
    tidyCards,
  } from '../../lib/pinboard.svelte'
  import PinCard from './PinCard.svelte'
  import PinEditor from './PinEditor.svelte'
  import PinSessionLinks from './PinSessionLinks.svelte'
  import PinTimeline from './PinTimeline.svelte'

  const clamp = (v: number, lo: number, hi: number): number => Math.max(lo, Math.min(hi, v))

  let boardEl = $state<HTMLElement>()
  let stripEl = $state<HTMLElement>()
  let dcardsEl = $state<HTMLElement>()
  let draftEl = $state<HTMLTextAreaElement>()
  let bw = $state(0)
  let bh = $state(0)
  let nowTick = $state(Date.now()) // 卡片「已持续/超期」每 30s 重算

  let draft = $state<{ x: number; y: number; text: string } | null>(null)
  let editId = $state('')
  let leaving = $state<Set<string>>(new Set())
  let dayOpen = $state('')
  let highlight = $state('')

  const editing = $derived(editId ? (board.cards.find((c) => c.id === editId) ?? null) : null)
  const dayRecs = $derived.by(() => (dayOpen ? board.done.filter((r) => dayOf(r) === dayOpen).sort(byDayOrder) : []))

  /* 渲染层级按 z 排序归一（10 起、封顶 60）：便签板长期使用 bump 累加的
     z 会无限增长，盖住页面浮层（编辑面板 250） */
  const stackOf = $derived.by(() => {
    const sorted = [...board.cards].sort((a, b) => a.z - b.z)
    const map = new Map<string, number>()
    sorted.forEach((c, i) => map.set(c.id, Math.min(10 + i, 60)))
    return map
  })

  onMount(() => {
    void ensurePinboard()
    const clock = setInterval(() => (nowTick = Date.now()), 30000)
    const ro = boardEl ? new ResizeObserver(([e]) => {
      bw = e.contentRect.width
      bh = e.contentRect.height
    }) : null
    if (ro && boardEl) ro.observe(boardEl)
    return () => {
      clearInterval(clock)
      ro?.disconnect()
      void saveNow()
    }
  })

  onDestroy(removeRdWindow)

  /* ── 新建 ── */

  function createAt(x: number, y: number) {
    dayOpen = ''
    draft = {
      x: clamp(x, PAD, Math.max(PAD, bw - CARD_DEF_W - PAD)),
      y: clamp(y, PAD, Math.max(PAD, bh - CARD_DEF_H - PAD)),
      text: '',
    }
    void tick().then(() => draftEl?.focus())
  }

  /* 双击板空白（卡片/展开层上的双击不触发） */
  function onBoardDbl(e: MouseEvent) {
    if (e.target !== boardEl || !boardEl) return
    const r = boardEl.getBoundingClientRect()
    createAt(e.clientX - r.left - CARD_DEF_W / 2, e.clientY - r.top - CARD_DEF_H / 2)
  }

  function commitDraft() {
    const d = draft
    if (!d) return
    draft = null
    const text = d.text.trim()
    if (!text) return // 空内容自动丢弃
    addCard(text, d.x, d.y)
  }

  function onDraftKey(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      commitDraft()
    } else if (e.key === 'Escape') {
      e.preventDefault()
      draft = null
    }
  }

  /* ── 完成 / 删除 / 整理 ── */

  function complete(id: string) {
    leaving = new Set(leaving).add(id)
    setTimeout(() => {
      completeCard(id)
      const next = new Set(leaving)
      next.delete(id)
      leaving = next
    }, 200)
  }

  function tidy() {
    dayOpen = ''
    tidyCards(bw - PAD * 2, bh - PAD * 2)
  }

  /* 「新会话」：开新会话并跳对话页；先记成待定，真实发出消息后才在卡片
     上显示关联（多次点击各自独立，没聊过的永不显示） */
  let creatingFor = $state('') // 防双击一次开两条（创建完成即释放）
  async function newSessionFor(cardId: string) {
    if (creatingFor) return
    creatingFor = cardId
    try {
      const before = store.activeId
      await store.newChatTab()
      const id = store.activeId
      if (!id || id === before) return
      addPendingSession(cardId, id)
      store.jumpView = 'chat'
    } finally {
      creatingFor = ''
    }
  }

  /* ── 时间线拖回板上 ── */

  function overBoard(x: number, y: number): boolean {
    const r = boardEl?.getBoundingClientRect()
    return !!r && x >= r.left && x <= r.right && y >= r.top && y <= r.bottom
  }

  function restoreToBoard(rec: PinRecord, x: number, y: number) {
    const r = boardEl?.getBoundingClientRect()
    if (!r) return
    dayOpen = '' // 展开的当天详情会挡住落点，收起再放回
    const card = restoreRecord(rec.id, clamp(x - r.left - CARD_DEF_W / 2, PAD, Math.max(PAD, bw - CARD_DEF_W - PAD)), clamp(y - r.top - CARD_DEF_H / 2, PAD, Math.max(PAD, bh - CARD_DEF_H - PAD)))
    if (card) {
      highlight = card.id
      setTimeout(() => {
        if (highlight === card.id) highlight = ''
      }, 1800)
    }
  }

  /* ── 当天详情：横向拖动排序 ── */

  let rdrag = $state<{ id: string; px: number; dx: number; active: boolean } | null>(null)

  function rdStart(e: PointerEvent, id: string) {
    if (e.button !== 0) return
    removeRdWindow()
    rdrag = { id, px: e.clientX, dx: 0, active: false }
    try {
      ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
    } catch {
      /* window 监听兜底 */
    }
    window.addEventListener('pointermove', rdMove)
    window.addEventListener('pointerup', rdUp)
    window.addEventListener('pointercancel', rdCancel)
    window.addEventListener('blur', rdCancel)
  }

  function removeRdWindow() {
    window.removeEventListener('pointermove', rdMove)
    window.removeEventListener('pointerup', rdUp)
    window.removeEventListener('pointercancel', rdCancel)
    window.removeEventListener('blur', rdCancel)
  }

  function rdMove(e: PointerEvent) {
    if (!rdrag) return
    const dx = e.clientX - rdrag.px
    if (!rdrag.active) {
      if (Math.abs(dx) < 6) return
      rdrag.active = true
    }
    rdrag.dx = dx
  }

  function rdCancel() {
    removeRdWindow()
    rdrag = null
  }

  /* 当天展开区：滚轮横滚卡片带（纵向滚轮也转成横向，和普通横向列表一致）。
     内容超出卡片高度时，卡片自己纵向滚动优先，不劫持 */
  function onStripWheel(e: WheelEvent) {
    if (!dcardsEl) return
    const card = (e.target as HTMLElement).closest('.dcard') as HTMLElement | null
    if (card && card.scrollHeight > card.clientHeight + 1) return
    const d = e.deltaY + e.deltaX
    if (!d) return
    e.preventDefault()
    dcardsEl.scrollLeft += d
  }

  function rdUp(e: PointerEvent) {
    removeRdWindow()
    if (!rdrag) return
    const d = rdrag
    rdrag = null
    if (!d.active || !dayOpen) return
    /* 目标索引在「去掉被拖卡后的顺序」上算，避免索引含自己时错一位 */
    const cards = Array.from(stripEl?.querySelectorAll<HTMLElement>('[data-rec]') ?? []).filter(
      (el) => el.dataset.rec !== d.id,
    )
    const target = cards.findIndex((el) => {
      const r = el.getBoundingClientRect()
      return e.clientX < r.left + r.width / 2
    })
    const ids = dayRecs.map((r) => r.id).filter((id) => id !== d.id)
    ids.splice(target < 0 ? ids.length : target, 0, d.id)
    reorderDay(dayOpen, ids)
  }

  /* ── 日期显示 / 键盘 ── */

  function dayLabel(key: string): string {
    const [y, m, d] = key.split('-').map(Number)
    const day = new Date(y, m - 1, d)
    const today = new Date()
    today.setHours(0, 0, 0, 0)
    const diff = Math.round((today.getTime() - day.getTime()) / 86400000)
    if (diff === 0) return '今天'
    if (diff === 1) return '昨天'
    if (diff === 2) return '前天'
    return `${m}月${d}日`
  }

  function fmt(ts: number): string {
    return new Date(ts).toLocaleString('zh-CN', { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  }

  function dur(ms: number): string {
    const m = Math.max(0, Math.floor(ms / 60000))
    if (m < 60) return `${m} 分钟`
    const h = Math.floor(m / 60)
    if (h < 24) return `${h} 小时${m % 60 ? ` ${m % 60} 分` : ''}`
    return `${Math.floor(h / 24)} 天${h % 24 ? ` ${h % 24} 小时` : ''}`
  }

  function onKey(e: KeyboardEvent) {
    const el = document.activeElement as HTMLElement | null
    const typing = !!el && (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.isContentEditable)
    if (e.key === 'Escape') {
      if (dayOpen) dayOpen = ''
      else if (draft) draft = null
      return
    }
    if (typing || editId) return
    if ((e.key === 'n' || e.key === 'N') && !e.ctrlKey && !e.metaKey && !e.altKey) {
      e.preventDefault()
      createAt(bw / 2 - CARD_DEF_W / 2, bh * 0.26)
    }
  }
</script>

<svelte:window onkeydown={onKey} />

<div class="pinpage">
  <header class="head">
    <h1>便签</h1>
    <span class="count">{board.cards.length} 件待办 · {board.done.length} 条已归档</span>
    {#if board.err}<span class="err" title={board.err}>{board.err}</span>{/if}
    <span class="spacer"></span>
    <button class="ghost" onclick={tidy} title="按挂得久→新排列，自动缩放保证不出板">整理</button>
    <button class="primary" onclick={() => createAt(bw / 2 - CARD_DEF_W / 2, bh * 0.26)} title="新建便签（也可双击板空白 / 按 N）">+ 记一笔</button>
  </header>

  <div class="work">
    <div
      class="board"
      bind:this={boardEl}
      ondblclick={onBoardDbl}
      onpointerdown={(e) => { if (e.target === boardEl) dayOpen = '' }}
      role="application"
      aria-label="便签板"
    >
      {#each board.cards as c (c.id)}
        <PinCard
          card={c}
          width={bw}
          height={bh}
          stack={stackOf.get(c.id) ?? 10}
          now={nowTick}
          highlighted={highlight === c.id}
          leaving={leaving.has(c.id)}
          onedit={() => {
            dayOpen = '' // 日期详情在编辑遮罩之上（1000），先收起
            editId = c.id
          }}
          oncomplete={() => complete(c.id)}
          onnewsession={() => void newSessionFor(c.id)}
          onopensession={(id) => void store.openSession(id)}
        />
      {/each}

      {#if draft}
        <textarea
          bind:this={draftEl}
          class="draft"
          bind:value={draft.text}
          style:left="{draft.x}px"
          style:top="{draft.y}px"
          style:width="{CARD_DEF_W}px"
          style:min-height="{CARD_DEF_H}px"
          placeholder="记一笔…（Enter 提交，Shift+Enter 换行，Esc 放弃）"
          spellcheck="false"
          onkeydown={onDraftKey}
          onblur={commitDraft}
        ></textarea>
      {/if}

      {#if !board.loaded}
        <div class="veil">{board.loading ? '便签加载中…' : '便签加载失败'}</div>
      {:else if !board.cards.length && !draft}
        <div class="veil hint">双击空白处记一笔，或点右上「记一笔」</div>
      {/if}

      {#if dayOpen}
        <!-- 当天详情：就地展开在板下半部（不覆盖时间线，保留「落在轴上哪
             一天」的空间参照）；Esc / × / 点板空白收起 -->
        <section
          class="daypanel"
          bind:this={stripEl}
          aria-label="当天完成详情"
          onwheel={onStripWheel}
          onpointerdown={(e) => { if (!(e.target as HTMLElement).closest('.dcard')) dayOpen = '' }}
        >
          <div class="dhead">
            <b>{dayLabel(dayOpen)}</b>
            <span>{dayRecs.length} 条完成</span>
            <span class="dhint">拖动卡片可排序</span>
            <span class="spacer"></span>
            <button class="xbtn" onclick={() => (dayOpen = '')} title="收起（Esc）">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                <path d="M6 6l12 12M18 6L6 18" />
              </svg>
            </button>
          </div>
          <div class="dcards" bind:this={dcardsEl}>
            {#each dayRecs as r (r.id)}
              <article
                class="dcard"
                class:dragging={rdrag?.id === r.id && rdrag.active}
                data-rec={r.id}
                style:--tint={r.tint}
                style:transform={rdrag?.id === r.id && rdrag.active ? `translateX(${rdrag.dx}px)` : undefined}
                onpointerdown={(e) => rdStart(e, r.id)}
              >
                <p class="dtext">{r.text || '（空）'}</p>
                <div class="dline">记于 {fmt(r.created)} · 完成 {fmt(r.doneAt)}</div>
                <div class="dline">用时 {dur(r.doneAt - r.created)}</div>
                {#if r.day}<div class="dnote">手动归到此日（完成于 {fmt(r.doneAt)}）</div>{/if}
                {#if r.sessions?.length}
                  <PinSessionLinks sessions={r.sessions} onopen={(id) => void store.openSession(id)} />
                {/if}
              </article>
            {:else}
              <p class="dempty">这一天没有完成的便签</p>
            {/each}
          </div>
        </section>
      {/if}
    </div>

    <PinTimeline
      done={board.done}
      {dayOpen}
      {overBoard}
      onrestore={restoreToBoard}
      onexpand={(k) => (dayOpen = dayOpen === k ? '' : k)}
      ondaychange={() => (dayOpen = '')}
      onblank={() => (dayOpen = '')}
    />
  </div>
</div>

{#if editing}
  <PinEditor
    card={editing}
    onclose={() => (editId = '')}
  />
{/if}

<style>
  /* 使用页（与对话同级），不是管理页：不套全局 .page 的 624px 居中限宽 */
  .pinpage {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 18px 24px 16px;
  }
  .head {
    flex: none;
    display: flex;
    align-items: baseline;
    gap: 12px;
  }
  h1 {
    margin: 0;
    font-size: var(--font-size-page);
    font-weight: 700;
  }
  .count {
    font-size: var(--font-size-ui);
    color: var(--muted);
    font-variant-numeric: tabular-nums;
  }
  .err {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--font-size-small);
    color: var(--danger);
  }
  .spacer {
    flex: 1;
  }
  .ghost,
  .primary {
    border-radius: 9px;
    font: inherit;
    font-size: var(--font-size-body);
    padding: 6px 16px;
    cursor: pointer;
  }
  .ghost {
    border: 1px solid var(--line);
    background: var(--bg);
    color: var(--muted);
  }
  .ghost:hover {
    color: var(--fg);
    border-color: var(--line-strong);
    background: var(--bg-soft);
  }
  .primary {
    border: none;
    background: var(--fg);
    color: var(--bg);
    font-weight: 600;
  }
  .primary:hover {
    opacity: 0.9;
  }
  .work {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    border: 1px solid var(--line);
    border-radius: 12px;
    overflow: hidden;
    background: var(--bg);
  }
  /* 板：点阵背景（位置即记忆，纸与墨的质感） */
  .board {
    position: relative;
    flex: 1;
    min-height: 0;
    overflow: hidden;
    background-color: var(--bg);
    background-image: radial-gradient(circle, color-mix(in srgb, var(--fg) 9%, transparent) 1px, transparent 1.1px);
    background-size: 26px 26px;
  }
  .veil {
    position: absolute;
    inset: 0;
    display: grid;
    place-items: center;
    color: var(--faint);
    font-size: var(--font-size-section);
    pointer-events: none;
  }
  .veil.hint {
    opacity: 0.85;
  }
  /* 新建即编辑：卡片同款版式，Enter 提交 */
  .draft {
    position: absolute;
    z-index: 2000;
    box-sizing: border-box;
    padding: 8px 10px 6px 13px;
    border: 1px solid var(--accent);
    border-radius: 7px;
    background: var(--bg);
    color: var(--fg);
    font: inherit;
    font-size: var(--font-size-ui);
    line-height: 1.55;
    resize: none;
    outline: none;
    box-shadow: 0 10px 22px -16px rgb(0 0 0 / 55%);
  }
  /* 当天详情：板下半部就地展开 */
  .daypanel {
    position: absolute;
    z-index: 1000;
    left: 0;
    right: 0;
    bottom: 0;
    max-height: 52%;
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 10px 14px 12px;
    border-top: 1px solid var(--line);
    background: var(--bg);
    box-shadow: 0 -12px 28px -20px rgb(0 0 0 / 45%);
    animation: dayup 0.2s var(--ease-out) both;
  }
  @keyframes dayup {
    from {
      opacity: 0;
      transform: translateY(8px);
    }
  }
  .dhead {
    flex: none;
    display: flex;
    align-items: center;
    gap: 10px;
    font-size: var(--font-size-section);
    color: var(--muted);
  }
  .dhead b {
    color: var(--fg);
  }
  .dhint {
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .xbtn {
    display: grid;
    place-items: center;
    width: 20px;
    height: 20px;
    border: none;
    border-radius: 5px;
    background: transparent;
    color: var(--faint);
    cursor: pointer;
  }
  .xbtn:hover {
    background: var(--line);
    color: var(--fg);
  }
  .xbtn svg {
    width: 11px;
    height: 11px;
  }
  .dcards {
    flex: 1;
    min-height: 0;
    display: flex;
    gap: 10px;
    overflow-x: auto;
    overflow-y: hidden;
    padding-bottom: 4px;
    /* 滚轮横向滚动已覆盖操作，隐藏滚动条 */
    scrollbar-width: none;
  }
  .dcards::-webkit-scrollbar {
    display: none;
  }
  .dcard {
    position: relative;
    flex: 0 0 330px;
    max-height: 100%;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 6px;
    box-sizing: border-box;
    padding: 12px 14px 11px;
    border: 1px solid var(--line);
    border-top: 3px solid var(--tint, var(--accent));
    border-radius: 8px;
    background: var(--bg);
    cursor: grab;
    touch-action: none;
  }
  .dcard.dragging {
    cursor: grabbing;
    z-index: 5;
    box-shadow: 0 10px 24px -12px rgb(0 0 0 / 30%);
  }
  .dtext {
    margin: 0;
    font-size: var(--font-size-section);
    line-height: 1.65;
    font-weight: 500;
    white-space: pre-wrap;
    word-break: break-word;
  }
  .dline {
    font-size: var(--font-size-small);
    color: color-mix(in srgb, var(--fg) 52%, transparent);
    font-variant-numeric: tabular-nums;
  }
  .dnote {
    font-size: var(--font-size-small);
    color: #b45309;
  }
  .dempty {
    margin: 0;
    padding: 8px 2px;
    color: var(--faint);
    font-size: var(--font-size-ui);
  }
</style>
