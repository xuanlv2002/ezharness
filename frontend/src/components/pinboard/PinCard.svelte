<script lang="ts">
  /*
  板上的一张便签卡：拖动（3px 阈值）、右下角缩放、悬停工具条（✓ 完成 /
  ✕ 删除就地确认）、双击打开编辑面板。内容下方固定显示记于时间、已持续
  时长与截止时间。

  拖动/缩放期间只改本地 transform 与宽高（不写共享状态），松手才提交
  x/y/w/h——连续 pointermove 不触发整板重排。now 由父级定时推进（30s），
  持续时间与超期状态跟着刷新。
  */
  import { onDestroy } from 'svelte'
  import type { PinCard as PinCardT } from '../../lib/api'
  import { MIN_H, MIN_W, PAD, bumpCard, patchCard, removeCard } from '../../lib/pinboard.svelte'

  let {
    card,
    width = 800,
    height = 600,
    stack = 10,
    now = Date.now(),
    highlighted = false,
    leaving = false,
    onedit,
    oncomplete,
  }: {
    card: PinCardT
    width?: number
    height?: number
    stack?: number
    now?: number
    highlighted?: boolean
    leaving?: boolean
    onedit?: () => void
    oncomplete?: () => void
  } = $props()

  function fmtShort(ts: number): string {
    return new Date(ts).toLocaleString('zh-CN', { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  }

  function dur(ms: number): string {
    const m = Math.max(0, Math.floor(ms / 60000))
    if (m < 60) return `${m} 分钟`
    const h = Math.floor(m / 60)
    if (h < 24) return `${h} 小时${m % 60 ? ` ${m % 60} 分` : ''}`
    return `${Math.floor(h / 24)} 天${h % 24 ? ` ${h % 24} 小时` : ''}`
  }

  const clamp = (v: number, lo: number, hi: number): number => Math.max(lo, Math.min(hi, v))

  let el = $state<HTMLElement>()
  let drag = $state<{ px: number; py: number; x: number; y: number } | null>(null)
  let moved = $state(false)
  let dx = $state(0)
  let dy = $state(0)

  let resizing = $state(false)
  let liveW = $state(0)
  let liveH = $state(0)
  let rs = $state<{ px: number; py: number; w: number; h: number } | null>(null)

  let confirmDel = $state(false)

  const showW = $derived(resizing ? liveW : card.w)
  const showH = $derived(resizing ? liveH : card.h)

  /* 拖拽/缩放的收尾监听挂在 window 上：松手、取消、窗口失焦都能收尾，
     不会出现「松手没检测到、卡一直挂在拖动状态」 */
  function onDown(e: PointerEvent) {
    if (e.button !== 0) return
    if ((e.target as HTMLElement).closest('.nodrag')) return
    drag = { px: e.clientX, py: e.clientY, x: card.x, y: card.y }
    moved = false
    try {
      el?.setPointerCapture(e.pointerId)
    } catch {
      /* 指针捕获失败时 window 监听仍可用 */
    }
    window.addEventListener('pointermove', onMove)
    window.addEventListener('pointerup', onUp)
    window.addEventListener('pointercancel', cancelDrag)
    window.addEventListener('blur', cancelDrag)
  }

  function removeDragWindow() {
    window.removeEventListener('pointermove', onMove)
    window.removeEventListener('pointerup', onUp)
    window.removeEventListener('pointercancel', cancelDrag)
    window.removeEventListener('blur', cancelDrag)
  }

  function onMove(e: PointerEvent) {
    if (!drag) return
    const ddx = e.clientX - drag.px
    const ddy = e.clientY - drag.py
    if (!moved && Math.hypot(ddx, ddy) < 3) return
    if (!moved) {
      moved = true
      bumpCard(card.id) // 拖起提层，拖动中的卡在最上层
    }
    dx = clamp(drag.x + ddx, PAD, Math.max(PAD, width - card.w - PAD)) - card.x
    dy = clamp(drag.y + ddy, PAD, Math.max(PAD, height - card.h - PAD)) - card.y
  }

  function onUp() {
    removeDragWindow()
    if (!drag) return
    if (moved) patchCard(card.id, { x: card.x + dx, y: card.y + dy })
    drag = null
    moved = false
    dx = 0
    dy = 0
  }

  function cancelDrag() {
    removeDragWindow()
    drag = null
    moved = false
    dx = 0
    dy = 0
  }

  function dbl(e: MouseEvent) {
    if ((e.target as HTMLElement).closest('.nodrag')) return
    if (moved) return
    onedit?.()
  }

  /* 缩放：右下角 grip，最小尺寸 MIN_W/MIN_H，不越出板右/下缘 */
  function rDown(e: PointerEvent) {
    e.stopPropagation()
    rs = { px: e.clientX, py: e.clientY, w: card.w, h: card.h }
    liveW = card.w
    liveH = card.h
    resizing = true
    try {
      ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
    } catch {
      /* window 监听兜底 */
    }
    window.addEventListener('pointermove', rMove)
    window.addEventListener('pointerup', rUp)
    window.addEventListener('pointercancel', cancelResize)
    window.addEventListener('blur', cancelResize)
  }

  function removeResizeWindow() {
    window.removeEventListener('pointermove', rMove)
    window.removeEventListener('pointerup', rUp)
    window.removeEventListener('pointercancel', cancelResize)
    window.removeEventListener('blur', cancelResize)
  }

  function rMove(e: PointerEvent) {
    if (!rs) return
    liveW = clamp(rs.w + e.clientX - rs.px, MIN_W, Math.max(MIN_W, width - card.x - PAD))
    liveH = clamp(rs.h + e.clientY - rs.py, MIN_H, Math.max(MIN_H, height - card.y - PAD))
  }

  function rUp() {
    removeResizeWindow()
    if (!rs) return
    patchCard(card.id, { w: liveW, h: liveH })
    rs = null
    resizing = false
  }

  function cancelResize() {
    removeResizeWindow()
    rs = null
    resizing = false
    liveW = 0
    liveH = 0
  }

  onDestroy(() => {
    removeDragWindow()
    removeResizeWindow()
  })

  function removeNow() {
    confirmDel = false
    removeCard(card.id)
  }
</script>

<article
  bind:this={el}
  class="pcard"
  class:dragging={moved}
  class:resizing
  class:leaving
  class:hl={highlighted}
  class:short={card.h < 56}
  style:left="{card.x}px"
  style:top="{card.y}px"
  style:width="{showW}px"
  style:height="{showH}px"
  style:z-index={moved ? 9999 : stack}
  style:--tint={card.tint}
  style:transform={moved ? `translate(${dx}px, ${dy}px)` : undefined}
  onpointerdown={onDown}
  ondblclick={dbl}
  role="group"
  aria-label="便签"
>
  <div class="bar">
    <button class="tool nodrag" onclick={(e) => { e.stopPropagation(); oncomplete?.() }} title="划线完成，归档到时间线">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round">
        <path d="M5 12.5l4.5 4.5L19 7" />
      </svg>
    </button>
    <button class="tool nodrag" onclick={(e) => { e.stopPropagation(); confirmDel = true }} title="删除这张便签">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round">
        <path d="M6 6l12 12M18 6L6 18" />
      </svg>
    </button>
  </div>

  <div class="body">
    <p class="text">{card.text || '（空便签）'}</p>
    <!-- 固定信息行：记于 + 已持续 / 截止（未设置也显式占位） -->
    <div class="times">
      <span class="tline">记于 {fmtShort(card.created)} · 已持续 {dur(now - card.created)}</span>
      <span class="tline" class:over={!!card.due && card.due < now}>截止 {card.due ? fmtShort(card.due) : '未设置'}</span>
    </div>
  </div>

  <span class="grip nodrag" onpointerdown={rDown} role="presentation"></span>

  {#if confirmDel}
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="confirm nodrag" onpointerdown={(e) => e.stopPropagation()}>
      <p>删掉这张？</p>
      <div class="cbtns">
        <button onclick={(e) => { e.stopPropagation(); confirmDel = false }}>取消</button>
        <button class="danger" onclick={(e) => { e.stopPropagation(); removeNow() }}>删除</button>
      </div>
    </div>
  {/if}
</article>

<style>
  .pcard {
    position: absolute;
    display: flex;
    flex-direction: column;
    box-sizing: border-box;
    /* 白纸便签：卡面保持纸色，便签色只做左侧 2px 竖条（第一版观感） */
    border: 1px solid var(--line);
    border-radius: 8px;
    background: var(--bg);
    color: var(--fg);
    box-shadow:
      0 1px 2px rgb(24 20 16 / 4%),
      0 10px 22px -16px rgb(24 20 16 / 55%);
    cursor: grab;
    touch-action: none;
    /* 只过渡阴影：transform 参与过渡会在松手（left/top 提交 + 位移归零）
       时播放一段位移，看起来抖一下 */
    transition: box-shadow var(--dur-fast) var(--ease-out);
  }
  /* 便签色：左侧 2px 竖条常驻 */
  .pcard::before {
    content: '';
    position: absolute;
    left: 0;
    top: 6px;
    bottom: 6px;
    width: 2px;
    border-radius: 2px;
    background: var(--tint, #b291a6);
  }
  .pcard:hover {
    box-shadow:
      0 1px 2px rgb(24 20 16 / 5%),
      0 14px 28px -16px rgb(24 20 16 / 60%);
  }
  .pcard.dragging,
  .pcard.resizing {
    cursor: grabbing;
    transition: none;
    z-index: 9999;
  }
  /* 高亮（时间线卡拖回板上）：描边闪一下，卡不挪位 */
  .pcard.hl {
    box-shadow:
      0 0 0 2px var(--accent),
      0 14px 28px -16px rgb(24 20 16 / 60%);
  }
  /* 完成离场：淡出上移（240ms 后再真正移出数据） */
  .pcard.leaving {
    opacity: 0;
    transform: translateY(10px) scale(0.95);
    pointer-events: none;
    transition:
      opacity 0.2s var(--ease-out),
      transform 0.2s var(--ease-out);
  }
  .bar {
    position: absolute;
    top: 4px;
    right: 4px;
    display: flex;
    gap: 2px;
    opacity: 0;
    pointer-events: none;
    transition: opacity var(--dur-fast) var(--ease-out);
  }
  .pcard:hover .bar,
  .pcard:focus-within .bar {
    opacity: 1;
    pointer-events: auto;
  }
  .tool {
    display: grid;
    place-items: center;
    width: 20px;
    height: 20px;
    border: none;
    border-radius: 5px;
    background: var(--bg-soft);
    color: var(--muted);
    cursor: pointer;
  }
  .tool:hover {
    background: var(--line);
    color: var(--fg);
  }
  .tool svg {
    width: 11px;
    height: 11px;
  }
  .body {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    padding: 11px 12px 8px 15px;
    overflow: hidden;
  }
  .text {
    flex: 1;
    min-height: 0;
    margin: 0;
    font-size: var(--font-size-section);
    line-height: 1.6;
    font-weight: 500;
    white-space: pre-wrap;
    overflow: hidden;
    word-break: break-word;
  }
  /* 固定时间信息：记于+已持续 / 截止（两行，等宽数字） */
  .times {
    flex: none;
    display: flex;
    flex-direction: column;
    padding-top: 6px;
    font-size: var(--font-size-small);
    line-height: 1.5;
    color: color-mix(in srgb, var(--fg) 52%, transparent);
    font-variant-numeric: tabular-nums;
  }
  .tline {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .tline.over {
    color: var(--danger);
  }
  /* 矮卡紧凑排版：缩小字号与内边距，底部时间信息永远保留 */
  .pcard.short .body {
    padding: 7px 9px 5px 12px;
  }
  .pcard.short .text {
    font-size: var(--font-size-ui);
    line-height: 1.45;
  }
  .pcard.short .times {
    padding-top: 2px;
    font-size: 9px;
  }
  /* 右下角缩放手柄 */
  .grip {
    position: absolute;
    right: 0;
    bottom: 0;
    width: 12px;
    height: 12px;
    cursor: nwse-resize;
    background: linear-gradient(135deg, transparent 50%, color-mix(in srgb, var(--fg) 16%, transparent) 50%);
    border-bottom-right-radius: 8px;
  }
  /* 删除就地确认 */
  .confirm {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 8px;
    border-radius: 8px;
    background: color-mix(in srgb, var(--bg) 92%, transparent);
    backdrop-filter: blur(2px);
    font-size: var(--font-size-ui);
    cursor: default;
  }
  .confirm p {
    margin: 0;
    color: var(--muted);
  }
  .cbtns {
    display: flex;
    gap: 6px;
  }
  .cbtns button {
    border: 1px solid var(--line);
    border-radius: 6px;
    background: var(--bg);
    color: var(--fg);
    font: inherit;
    font-size: var(--font-size-small);
    padding: 2px 8px;
    cursor: pointer;
  }
  .cbtns button:hover {
    background: var(--bg-soft);
  }
  .cbtns .danger {
    color: var(--danger);
    border-color: color-mix(in srgb, var(--danger) 40%, var(--line));
  }
</style>
