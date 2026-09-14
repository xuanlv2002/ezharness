<script lang="ts">
  import { store } from '../../lib/store.svelte'
  import { filePane } from '../../lib/filePaneState'
  import TerminalTab from './TerminalTab.svelte'
  import ResourcePane from '../viewer/ResourcePane.svelte'
  import BrowserPane from './BrowserPane.svelte'

  /*
  工作区抽屉：推挤式右布局列（非悬浮 overlay）——打开时占布局宽度，
  聊天主列被挤窄不遮挡。工具页（终端/资源查看器/共享浏览器）互斥显
  隐，多 pane 常驻挂载保活（终端 WS/xterm、资源 tab 的 buffer/标注/
  画布、浏览器标签条互不丢失）；入口是右侧悬浮列的 mini 钮
  （store.toggleDrawerTool）。宽度统一（画板比例）：与视口联动让位，
  主列压到手机宽保底 480px；余量不足先压抽屉、保底 280px。
  动画 = width 展开（内层 .inner 恒宽 var(--pane-w)：过渡期间内容不
  重排、xterm 不反复 fit；窗口 resize 时宽度变化经 var 继承同步到
  inner，ResizeObserver 自动 fit）。
  */
  const open = $derived(store.termDrawerOpen)
  const tool = $derived(store.drawerTool)
  const TITLES: Record<string, [string, string]> = {
    term: ['共享终端', '用户与 AI 共写 · 收起不中断'],
    file: ['资源', '查看 · 编辑 · 发给 AI'],
    browser: ['共享浏览器', 'AI 操控 · 实时共见'],
  }
  const title = $derived(TITLES[tool] ?? TITLES.term)

  /* Escape 收抽屉（焦点在输入框/标注浮条时忽略——它们的 Esc 归自己） */
  function onKey(e: KeyboardEvent) {
    if (e.key !== 'Escape' || !open) return
    const el = document.activeElement
    if (el && (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA')) return
    store.closeTermDrawer()
  }

  /* 拖拽脱离：按住头部把手拖出抽屉 → 主进程建预览窗口跟着光标走；
     松手在抽屉外落定为独立窗口（抽屉收起），松手回抽屉内销毁预览。
     指针移出窗口、乃至预览窗口出现在光标下时，靠 Chromium 在按键期对
     源窗口的隐式鼠标捕获（setPointerCapture 强化）继续收 pointermove/
     pointerup 与屏幕坐标。内容迁移沿用既有机制：资源页带 tab 状态快照，
     浏览器 view 随弹出窗口上报 rect 自动接管，终端靠 WS 重连续屏。 */
  const ENTER = 8 // 越出抽屉边缘多少像素算进入脱离
  const EXIT = 16 // 拖回抽屉内多少像素算撤销（滞回，防边缘抖动）
  const SLOP = 6 // 小于此位移不判定（防误触）

  let drawerEl: HTMLElement | undefined = $state()
  let dragging = $state(false)
  let drag: {
    pointerId: number
    originX: number
    originY: number
    grabX: number
    grabY: number
    startX: number
    startY: number
    rect: DOMRect
    tearing: boolean
  } | null = null

  /* 指针是否还在抽屉矩形内（margin 负 = 收缩，正 = 外扩） */
  function insideDrawer(sx: number, sy: number, margin: number): boolean {
    if (!drag) return true
    const { originX, originY, rect } = drag
    return (
      sx >= originX + rect.left - margin &&
      sx <= originX + rect.right + margin &&
      sy >= originY + rect.top - margin &&
      sy <= originY + rect.bottom + margin
    )
  }

  function beginDrag(e: PointerEvent) {
    if (!open || !drawerEl || drag) return
    const el = e.target as HTMLElement
    if (!el.closest('header') || el.closest('button, input, textarea, [contenteditable]')) return
    const rect = drawerEl.getBoundingClientRect()
    drag = {
      pointerId: e.pointerId,
      originX: e.screenX - e.clientX, // 窗口屏幕原点（拖拽期间只用 screenX/Y）
      originY: e.screenY - e.clientY,
      grabX: e.clientX - rect.left, // 抓取点在面板内的偏移（预览窗口对位用）
      grabY: e.clientY - rect.top,
      startX: e.screenX,
      startY: e.screenY,
      rect,
      tearing: false,
    }
    dragging = true
    drawerEl.setPointerCapture(e.pointerId)
    e.preventDefault()
  }

  function moveDrag(e: PointerEvent) {
    if (!drag || e.pointerId !== drag.pointerId) return
    const ez = (window as any).ez
    if (!ez?.popout?.tearBegin) return
    if (!drag.tearing) {
      if (Math.hypot(e.screenX - drag.startX, e.screenY - drag.startY) < SLOP) return
      if (insideDrawer(e.screenX, e.screenY, ENTER)) return
      drag.tearing = true
      ez.popout.tearBegin(tool, tool === 'file' ? filePane.serialize() : null, e.screenX, e.screenY, drag.grabX, drag.grabY)
    }
    if (insideDrawer(e.screenX, e.screenY, -EXIT)) {
      // 拖回抽屉内：撤销预览，留在抽屉
      drag.tearing = false
      ez.popout.tearEnd(false)
      return
    }
    ez.popout.tearMove(e.screenX, e.screenY)
  }

  /* commit=false 用于 pointercancel / lostpointercapture 兜底 */
  function endDrag(e: PointerEvent, commit: boolean) {
    if (!drag || e.pointerId !== drag.pointerId) return
    const tearing = drag.tearing
    drag = null
    dragging = false
    if (!tearing) return
    const ez = (window as any).ez
    ez?.popout?.tearEnd(commit)
    if (commit) {
      store.toolPoppedOut(tool)
      store.closeTermDrawer()
    }
  }
</script>

<svelte:window onkeydown={onKey} />

<aside
  class="drawer"
  class:open
  class:dragging
  role="complementary"
  aria-label="工作区"
  bind:this={drawerEl}
  onpointerdown={beginDrag}
  onpointermove={moveDrag}
  onpointerup={(e) => endDrag(e, true)}
  onpointercancel={(e) => endDrag(e, false)}
  onlostpointercapture={(e) => endDrag(e, false)}
>
  <div class="inner">
    <header title="按住拖出抽屉，成为独立窗口（关闭窗口即回到抽屉）">
      <span class="grip" aria-hidden="true">
        <svg viewBox="0 0 16 16" fill="currentColor">
          <circle cx="5" cy="3" r="1.25" /><circle cx="11" cy="3" r="1.25" />
          <circle cx="5" cy="8" r="1.25" /><circle cx="11" cy="8" r="1.25" />
          <circle cx="5" cy="13" r="1.25" /><circle cx="11" cy="13" r="1.25" />
        </svg>
      </span>
      <h3>{title[0]}</h3>
      <span class="hint">{title[1]}</span>
      <button class="close" onclick={() => store.closeTermDrawer()} title="收起">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
          <path d="M6 6l12 12M18 6L6 18" />
        </svg>
      </button>
    </header>
    <div class="body">
      <div class="pane" class:hidden={tool !== 'term'}>
        <TerminalTab active={open && tool === 'term'} />
      </div>
      <div class="pane" class:hidden={tool !== 'file'}>
        <ResourcePane active={open && tool === 'file'} />
      </div>
      <div class="pane" class:hidden={tool !== 'browser'}>
        <BrowserPane active={open && tool === 'browser'} />
      </div>
    </div>
  </div>
</aside>

<style>
  /* 各页统一宽（画板比例）：抽屉展开时主列压到手机宽保底 480px；
  侧栏宽经 --sidebar-w 联动（收 64 / 展 176）。余量不足时抽屉先让位，
  保底 280 */
  .drawer {
    --pane-w: max(280px, min(960px, 58vw, calc(100vw - var(--sidebar-w, 64px) - 480px)));
    flex: none;
    width: 0;
    overflow: hidden;
    background: var(--bg);
    border-left: 1px solid var(--line);
    transition: width 0.28s var(--ease-out);
  }
  .drawer.open {
    width: var(--pane-w);
  }
  /* 内层恒宽（继承 --pane-w）：外层宽度过渡时内容不重排；窗口
  resize 时宽度变化随之更新，RO fit 跟随实际宽 */
  .inner {
    display: flex;
    flex-direction: column;
    width: var(--pane-w);
    height: 100%;
  }
  /* 头部 = 拖拽把手（按住拖出抽屉成独立窗口） */
  header {
    display: flex;
    align-items: center;
    gap: 10px;
    flex: none;
    padding: 14px 12px 8px 16px;
    cursor: grab;
    user-select: none;
  }
  .drawer.dragging header {
    cursor: grabbing;
  }
  .grip {
    flex: none;
    display: grid;
    place-items: center;
    color: var(--faint);
  }
  .grip svg {
    width: 11px;
    height: 11px;
  }
  h3 {
    font-size: 13px;
    font-weight: 700;
  }
  .hint {
    flex: 1;
    font-size: 11px;
    color: var(--faint);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .close {
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    border: none;
    background: transparent;
    color: var(--muted);
    border-radius: 8px;
    cursor: pointer;
  }
  .close:hover {
    background: var(--line);
    color: var(--fg);
  }
  .close svg {
    width: 13px;
    height: 13px;
  }
  .body {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    padding: 0 12px 12px 16px;
  }
  /* 工具页互斥显隐（pane 常驻保活——不进 {#if}） */
  .pane {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .pane.hidden {
    display: none;
  }
</style>
