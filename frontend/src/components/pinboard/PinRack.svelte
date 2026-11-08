<script lang="ts">
  /*
  顶栏便签小点：板上每张待办对应一颗便签色小点，排在便签图标左侧；
  点一颗点 → 从顶部挂下完整便签（正文/图片/记于/截止/关联会话/新会话），
  再点同一颗点收起。点左右拖动即排序（ord 落盘）。点正文就地编辑
  （与板上卡片同款编辑器；拖动划选文字仍归复制，不进编辑）。图片
  双击看大图、悬停可复制/删除（与板上一致，但不可拖拽摆位）。
  */
  import { onDestroy } from 'svelte'
  import { fly } from 'svelte/transition'
  import { api, type PinCard, type PinSessionRef } from '../../lib/api'
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
  import { compressPinImage, copyPinImage } from '../../lib/pinImage'
  import PinRichEditor from './PinRichEditor.svelte'

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
  let layerEl = $state<HTMLElement>()
  let bodyEl = $state<HTMLElement>()

  /* ── 就地编辑正文：点正文进编辑器，Ctrl/⌘+Enter 或点卡片外落定 ── */
  let editing = $state(false)
  let editId = $state('')
  let editText = $state('')
  let editErr = $state('')
  let editFocus = $state<{ x: number; y: number; scrollTop: number } | null>(null)

  function startEdit(x: number, y: number): void {
    if (!card || editing) return
    editId = card.id
    editText = card.text
    editErr = ''
    editFocus = { x, y, scrollTop: bodyEl?.scrollTop ?? 0 }
    editing = true
  }

  /* endEdit 落定：只写回真的改过的正文；清空到「内容与图片都无」则放弃 */
  function endEdit(): void {
    if (!editing) return
    const c = board.cards.find((x) => x.id === editId)
    const t = editText.trim()
    if (c && t !== c.text && (t || c.images?.length)) patchCard(editId, { text: t })
    editing = false
    editId = ''
    editErr = ''
  }

  function cancelEdit(): void {
    editing = false
    editId = ''
    editErr = ''
  }

  /* 编辑器里粘贴图片：上传后直接贴到卡面（与板卡非编辑态拖图同款） */
  async function addImages(files: File[]): Promise<void> {
    if (!card) return
    try {
      const urls: string[] = []
      for (const f of files) urls.push((await api.uploadPinImage(await compressPinImage(f))).url)
      patchCard(card.id, { images: [...(card.images ?? []), ...urls] })
    } catch (e) {
      editErr = `图片上传失败：${(e as Error).message}`
    }
  }

  /* ── 图片：双击看大图、悬停复制/删除（不跳便签板、不可拖拽摆位） ── */
  let viewImg = $state('')
  let lbEl = $state<HTMLElement>()
  let imgMsg = $state('')
  let imgMsgTimer: ReturnType<typeof setTimeout> | undefined

  function flashImgMsg(msg: string): void {
    imgMsg = msg
    clearTimeout(imgMsgTimer)
    imgMsgTimer = setTimeout(() => (imgMsg = ''), 1600)
  }

  function openView(src: string): void {
    viewImg = src
    requestAnimationFrame(() => lbEl?.focus())
  }

  async function copyImage(src: string): Promise<void> {
    const r = await copyPinImage(src)
    flashImgMsg(r.msg)
  }

  /* 删图：连带清掉摆位记录（imgpos 里指向不存在图片的条目下次加载也会被滤掉） */
  function removeImage(src: string): void {
    if (!card) return
    const images = (card.images ?? []).filter((s) => s !== src)
    const pos = card.imgpos
      ? Object.fromEntries(Object.entries(card.imgpos).filter(([k]) => k !== src))
      : {}
    patchCard(card.id, {
      images: images.length ? images : undefined,
      imgpos: Object.keys(pos).length ? pos : undefined,
    })
    flashImgMsg('已移除图片')
    if (viewImg === src) viewImg = ''
  }

  /* 点卡片外先落定编辑（遮罩点击随后自然收起面板） */
  $effect(() => {
    if (!editing) return
    const down = (e: PointerEvent) => {
      if (layerEl?.contains(e.target as Node)) return
      endEdit()
    }
    document.addEventListener('pointerdown', down, true)
    return () => document.removeEventListener('pointerdown', down, true)
  })
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
    endEdit()
    selected = ''
    store.jumpView = 'pinboard'
  }

  function openSession(s: PinSessionRef): void {
    endEdit()
    selected = ''
    void store.openSession(s.id)
  }

  async function newSession(c: PinCard): Promise<void> {
    if (creatingFor) return
    endEdit()
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
    endEdit()
    completeCard(c.id)
    void saveNow()
    selected = ''
    deleteAsk = false
  }

  function removeNow(): void {
    if (!card) return
    endEdit()
    removeCard(card.id)
    void saveNow()
    selected = ''
    deleteAsk = false
  }

  /* 点正文区（文字、空白、图片都算——图片把卡片撑高后，文字旁边的大片
     空白也要能点进编辑）：勾选框翻转；链接/按钮随自身走；其余单击进
     编辑。「点击」按按下→抬起的位移判定（板卡同款 5px 阈值）——手抖
     一两像素照样算点击；真挪动了是划选文字（要复制），双击/三击选词
     选段也不进编辑 */
  let bodyDown = { x: 0, y: 0 }

  function onBodyDown(e: PointerEvent): void {
    if (e.button === 0) bodyDown = { x: e.clientX, y: e.clientY }
  }

  function onBodyClick(e: MouseEvent): void {
    if (!card) return
    const box = (e.target as HTMLElement).closest('input[type="checkbox"]')
    if (box) {
      e.preventDefault()
      const host = e.currentTarget as HTMLElement
      const idx = Array.from(host.querySelectorAll('input[type="checkbox"]')).indexOf(box as HTMLInputElement)
      if (idx < 0) return
      const next = toggleTask(card.text, idx)
      if (next !== null) patchCard(card.id, { text: next })
      return
    }
    if (editing || (e.target as HTMLElement).closest('a, button')) return
    if (e.detail > 1) return
    if (Math.hypot(e.clientX - bodyDown.x, e.clientY - bodyDown.y) >= 5) return
    startEdit(e.clientX, e.clientY)
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

  onDestroy(() => {
    stopDragListeners()
    clearTimeout(imgMsgTimer)
  })
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
  <div class="card-layer" bind:this={layerEl} transition:fly={{ y: -8, duration: 150 }}>
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

      <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
      <div class="cbody" class:editing bind:this={bodyEl} onpointerdown={onBodyDown} onclick={onBodyClick}>
        {#if editing}
          <div class="cedit">
            <PinRichEditor
              value={editText}
              focusAt={editFocus}
              onchange={(md) => (editText = md)}
              onsave={endEdit}
              oncancel={cancelEdit}
              onimage={(files) => void addImages(files)}
            />
          </div>
        {:else}
          <div class="cmd">{@html cardHtml}</div>
        {/if}
        {#if card.images?.length}
          <div class="cimgs">
            {#each card.images as src (src)}
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <div class="cimg" title="双击看大图 · 悬停可复制/移除" ondblclick={() => openView(src)}>
                <img {src} alt="" loading="lazy" draggable="false" />
                <button class="cimgact copy" onclick={() => void copyImage(src)} title="复制图片" aria-label="复制图片">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <rect x="9" y="9" width="10" height="11" rx="2" />
                    <path d="M5 15V6a2 2 0 0 1 2-2h7" />
                  </svg>
                </button>
                <button class="cimgact del" onclick={() => removeImage(src)} title="移除图片" aria-label="移除图片">✕</button>
              </div>
            {/each}
          </div>
        {/if}
      </div>

      {#if editing}
        <div class="cebar">
          {#if editErr}<span class="cerr">{editErr}</span>{/if}
          <button class="cbtn" onclick={cancelEdit} title="放弃修改（Esc）">取消</button>
          <button class="cbtn save" onclick={endEdit} title="保存（Ctrl/⌘+Enter）">保存</button>
        </div>
      {/if}

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

      {#if imgMsg}<div class="cimgmsg">{imgMsg}</div>{/if}
    </div>
  </div>
{/if}

{#if viewImg}
  <!-- 双击图片的全屏大图：点空白 / Esc / 右上角关闭（Esc 只关大图，不落
       到顶栏收起） -->
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
  <div
    class="lightbox"
    bind:this={lbEl}
    tabindex="-1"
    onclick={(e) => { if (e.target === e.currentTarget) viewImg = '' }}
    onkeydown={(e) => { if (e.key === 'Escape') { e.stopPropagation(); viewImg = '' } }}
  >
    <img src={viewImg} alt="" />
    <button class="lbclose" onclick={() => (viewImg = '')} title="关闭（Esc）" aria-label="关闭大图">✕</button>
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
  /* 编辑中：正文列撑满行高，编辑器在列内自滚（长文不把卡片顶破） */
  .cbody.editing {
    align-items: stretch;
    overflow: hidden;
  }
  .cedit {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }
  /* 编辑操作条：取消/保存右对齐，紧贴底栏上方 */
  .cebar {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 4px;
    margin-top: 4px;
  }
  .cerr {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    margin-right: auto;
    font-size: var(--font-size-small);
    color: var(--danger);
  }
  .cbtn {
    padding: 1px 8px;
    border: 1px solid var(--line);
    border-radius: 5px;
    background: var(--bg);
    color: var(--muted);
    font: inherit;
    font-size: var(--font-size-small);
    cursor: pointer;
  }
  .cbtn:hover {
    color: var(--fg);
    border-color: var(--line-strong);
  }
  .cbtn.save {
    border-color: var(--fg);
    background: var(--fg);
    color: var(--bg);
    font-weight: 600;
  }
  .cbtn.save:hover {
    opacity: 0.9;
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
    position: relative;
    width: 56px;
    height: 56px;
    border: 1px solid var(--line);
    border-radius: 5px;
    background: var(--bg-soft);
    overflow: hidden;
    cursor: zoom-in;
  }
  .cimg img {
    display: block;
    width: 100%;
    height: 100%;
    object-fit: cover;
    pointer-events: none;
  }
  .cimg:hover {
    border-color: var(--line-strong);
  }
  /* 悬停浮出的复制/移除小钮（与板卡贴纸同款） */
  .cimgact {
    position: absolute;
    top: 3px;
    display: none;
    place-items: center;
    width: 16px;
    height: 16px;
    padding: 0;
    border: none;
    border-radius: 50%;
    background: color-mix(in srgb, var(--fg) 86%, transparent);
    color: var(--bg);
    font: inherit;
    font-size: 9px;
    line-height: 1;
    cursor: pointer;
  }
  .cimg:hover .cimgact {
    display: grid;
  }
  .cimgact.copy {
    left: 3px;
  }
  .cimgact.del {
    right: 3px;
  }
  .cimgact.copy svg {
    width: 10px;
    height: 10px;
  }
  .cimgact:hover {
    background: var(--fg);
  }
  /* 图片操作结果浮条 */
  .cimgmsg {
    position: absolute;
    left: 50%;
    bottom: 8px;
    z-index: 6;
    padding: 2px 8px;
    border-radius: 6px;
    background: var(--bg-invert);
    color: var(--fg-invert);
    font-size: var(--font-size-small);
    white-space: nowrap;
    transform: translateX(-50%);
    pointer-events: none;
    box-shadow: 0 4px 12px rgb(0 0 0 / 18%);
  }
  /* 双击图片的全屏大图 */
  .lightbox {
    position: fixed;
    inset: 0;
    z-index: 3000;
    display: grid;
    place-items: center;
    box-sizing: border-box;
    padding: 4vh 4vw;
    background: rgb(0 0 0 / 68%);
    backdrop-filter: blur(3px);
    cursor: zoom-out;
    outline: none;
  }
  .lightbox img {
    max-width: 100%;
    max-height: 100%;
    border-radius: 6px;
    background: var(--bg);
    box-shadow: 0 24px 64px rgb(0 0 0 / 45%);
    object-fit: contain;
    cursor: default;
  }
  .lbclose {
    position: absolute;
    top: 14px;
    right: 14px;
    display: grid;
    place-items: center;
    width: 30px;
    height: 30px;
    padding: 0;
    border: 1px solid rgb(255 255 255 / 22%);
    border-radius: 50%;
    background: rgb(0 0 0 / 35%);
    color: #fff;
    font: inherit;
    font-size: 14px;
    cursor: pointer;
  }
  .lbclose:hover {
    background: rgb(0 0 0 / 55%);
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
