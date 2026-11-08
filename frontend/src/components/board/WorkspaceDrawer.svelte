<script lang="ts">
  import { store } from '../../lib/store.svelte'
  import { filePane } from '../../lib/filePaneState'
  import { explorerUI, setExplorerOpen } from '../../lib/explorerState.svelte'
  import TerminalTab from './TerminalTab.svelte'
  import ResourcePane from '../viewer/ResourcePane.svelte'
  import BrowserPane from './BrowserPane.svelte'

  /*
  工区抽屉：推挤式右布局列（非悬浮 overlay）——打开时占布局宽度，
  聊天主列被挤窄不遮挡。工具页（终端/资源查看器/共享浏览器）互斥显
  隐，多 pane 常驻挂载保活（终端 WS/xterm、资源 tab 的 buffer/标注/
  画布、浏览器标签条互不丢失）；入口是聊天输入条的 mini 钮
  （store.toggleDrawerTool，开着且同页=收起）。收起：头部 » 钮 /
  双击标题栏 / Esc，收起后整列出净、不占宽度。
  宽度：中间线定主列宽（默认 42vw、保底 480px），抽屉吃掉剩余全部——
  窗口缩放时抽屉跟着伸缩、主列不动；抽屉保底 PANE_MIN 640px（浏览器/
  终端/资源都要放得下，不允许压成细条）。动画 = width 展开（内层
  .inner 恒宽 var(--pane-w)：过渡期间内容不重排、xterm 不反复 fit；
  窗口 resize 时宽度变化经 var 继承同步到 inner，ResizeObserver 自动 fit）。
  */
  let { visible = true }: { visible?: boolean } = $props()

  const open = $derived(store.termDrawerOpen && visible)
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

  /* 双击标题栏收起（点在按钮上不算；抽屉开着才够得到标题栏） */
  function onHeadDbl(e: MouseEvent): void {
    if (!open || (e.target as HTMLElement).closest('button')) return
    store.closeTermDrawer()
  }

  /* 拖拽脱离：按住头部把手拖出抽屉 → 拖拽期间只画一个跟随光标的幽灵卡片，
     松手在抽屉外就在落点**新建一个普通窗口**（抽屉收起），松手回抽屉内取消。
     窗口全程只有默认状态——不再做"预览态（透明/不可聚焦/置顶）→ 落定态"
     的切换：那些临时窗口态在 Windows 上会让窗口合成面进入异常态，表现就是
     内容区白屏（抽屉从不需要它们，所以抽屉从来没这个问题）。
     指针移出窗口时靠 Chromium 在按键期对源窗口的隐式鼠标捕获
     （setPointerCapture 强化）继续收 pointermove/pointerup 与屏幕坐标。 */
  const ENTER = 8 // 越出抽屉边缘多少像素算进入脱离
  const EXIT = 16 // 拖回抽屉内多少像素算撤销（滞回，防边缘抖动）
  const SLOP = 6 // 小于此位移不判定（防误触）

  let drawerEl: HTMLElement | undefined = $state()
  let dragging = $state(false)
  let ghost = $state<{ x: number; y: number } | null>(null) // 脱离中的光标幽灵
  let drag: {
    pointerId: number
    originX: number
    originY: number
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
    drag = {
      pointerId: e.pointerId,
      originX: e.screenX - e.clientX, // 窗口屏幕原点（判定用 screenX/Y）
      originY: e.screenY - e.clientY,
      startX: e.screenX,
      startY: e.screenY,
      rect: drawerEl.getBoundingClientRect(),
      tearing: false,
    }
    dragging = true
    drawerEl.setPointerCapture(e.pointerId)
    e.preventDefault()
  }

  function moveDrag(e: PointerEvent) {
    if (!drag || e.pointerId !== drag.pointerId) return
    if (!drag.tearing) {
      if (Math.hypot(e.screenX - drag.startX, e.screenY - drag.startY) < SLOP) return
      if (insideDrawer(e.screenX, e.screenY, ENTER)) return
      drag.tearing = true
    }
    if (insideDrawer(e.screenX, e.screenY, -EXIT)) {
      drag.tearing = false // 拖回抽屉内：撤销脱离
      ghost = null
      return
    }
    ghost = { x: e.clientX, y: e.clientY }
  }

  /* popOut 弹出为独立窗口（按钮点击与拖拽脱离同一条路）：给落点就按落点摆
     （拖拽），不给就默认居中（按钮）。资源页带 tab 状态快照，浏览器 view
     随上报自动接管，终端靠 WS 重连续屏。 */
  function popOut(x?: number, y?: number) {
    const ez = (window as any).ez
    if (!ez?.popout) return
    ez.popout.open(tool, tool === 'file' ? filePane.serialize() : null, x, y)
    store.toolPoppedOut(tool)
    store.closeTermDrawer()
  }

  /* commit=false 用于 pointercancel / lostpointercapture 兜底 */
  function endDrag(e: PointerEvent, commit: boolean) {
    if (!drag || e.pointerId !== drag.pointerId) return
    const { tearing } = drag
    drag = null
    dragging = false
    ghost = null
    if (!tearing || !commit) return
    popOut(e.screenX, e.screenY)
  }

  /* 左缘拖拽定主列宽：写入 store.chatWidth（0 = 跟随默认公式）。
  主列保底 480、抽屉保底 640（主窗口 minWidth 与 desktop 壳同步） */
  let resizing = $state(false)
  let resize: { pointerId: number; startX: number; startChatW: number } | null = null

  /* 开/关抽屉时：①把默认主列宽落成固定 px（默认公式含 42vw 随窗口缩放，
  不落 px 的话拉窗口主列抽屉会一起变宽，主列应是不动锚点）；②进入 300ms
  动画窗，width 过渡仅在此期间启用，平时宽度即时跟随窗口/侧栏变化 */
  let animating = $state(false)
  $effect(() => {
    open
    if (open && !store.chatWidth) {
      store.chatWidth = Math.round(Math.max(480, window.innerWidth * 0.42))
    }
    animating = true
    const timer = setTimeout(() => (animating = false), 300)
    return () => clearTimeout(timer)
  })

  function sidebarW(): number {
    if (!drawerEl) return 64
    const v = parseFloat(getComputedStyle(drawerEl).getPropertyValue('--sidebar-w'))
    return Number.isFinite(v) && v > 0 ? v : 64
  }

  function beginResize(e: PointerEvent) {
    if (!open || !drawerEl || resize) return
    resize = {
      pointerId: e.pointerId,
      startX: e.clientX,
      startChatW: window.innerWidth - sidebarW() - drawerEl.getBoundingClientRect().width,
    }
    resizing = true
    ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
    e.preventDefault()
  }

  function moveResize(e: PointerEvent) {
    if (!resize || e.pointerId !== resize.pointerId) return
    const max = window.innerWidth - sidebarW() - 640
    store.chatWidth = Math.round(Math.max(480, Math.min(max, resize.startChatW + (e.clientX - resize.startX))))
  }

  function endResize(e: PointerEvent) {
    if (resize && e.pointerId === resize.pointerId) {
      resize = null
      resizing = false
    }
  }
</script>

<svelte:window onkeydown={onKey} />

{#if ghost}
  <!-- 脱离中的幽灵卡片（纯 DOM：拖拽期间不开窗口） -->
  <div class="ghost" style="left:{ghost.x}px;top:{ghost.y}px">{title[0]}</div>
{/if}

<aside
  class="drawer"
  class:open
  class:animating
  class:dragging
  class:resizing
  role="complementary"
  aria-label="工作区"
  bind:this={drawerEl}
  style={store.chatWidth ? `--chat-w-pref:${store.chatWidth}px` : ''}
  onpointerdown={beginDrag}
  onpointermove={moveDrag}
  onpointerup={(e) => endDrag(e, true)}
  onpointercancel={(e) => endDrag(e, false)}
  onlostpointercapture={(e) => endDrag(e, false)}
>
  {#if open}
    <div
      class="resizer"
      class:on={resizing}
      onpointerdown={beginResize}
      onpointermove={moveResize}
      onpointerup={endResize}
      onpointercancel={endResize}
      title="拖动调整宽度"
    ></div>
  {/if}
  <div class="inner">
    <header title="按住拖出抽屉，成为独立窗口；双击收起" ondblclick={onHeadDbl}>
      <span class="grip" aria-hidden="true">
        <svg viewBox="0 0 16 16" fill="currentColor">
          <circle cx="5" cy="3" r="1.25" /><circle cx="11" cy="3" r="1.25" />
          <circle cx="5" cy="8" r="1.25" /><circle cx="11" cy="8" r="1.25" />
          <circle cx="5" cy="13" r="1.25" /><circle cx="11" cy="13" r="1.25" />
        </svg>
      </span>
      <div class="head-text">
        <h3>{title[0]}</h3>
        <p>{title[1]}</p>
      </div>
      <div class="actions">
        {#if tool === 'file'}
          <!-- 目录面板开关进标题栏：树从标题栏正下方展开，预览区里不再
          常驻一枚钮占一行（弹出独立窗没有标题栏，面板内自留） -->
          <button
            class="pop"
            class:active={explorerUI.open}
            onclick={() => setExplorerOpen(!explorerUI.open)}
            title={explorerUI.open ? '收起目录面板' : '打开目录面板'}
          >
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
              <rect x="3" y="4" width="18" height="16" rx="2" />
              <path d="M9 4v16" />
            </svg>
          </button>
        {/if}
        <button class="pop" onclick={() => popOut()} title="弹出为独立窗口（也可按住头部拖出）">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round">
            <path d="M15 4h5v5" />
            <path d="M20 4l-8 8" />
            <path d="M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5" />
          </svg>
        </button>
        <button class="pop" onclick={() => store.closeTermDrawer()} title="收起抽屉（Esc / 双击标题栏）">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M6 6l6 6-6 6" />
            <path d="M13 6l6 6-6 6" />
          </svg>
        </button>
      </div>
    </header>
    <div class="body" class:treedocked={tool === 'file' && explorerUI.open && explorerUI.docked}>
      <div class="pane" class:hidden={tool !== 'term'}>
        <TerminalTab active={open && tool === 'term'} />
      </div>
      <div class="pane" class:hidden={tool !== 'file'}>
        <ResourcePane active={open && tool === 'file'} hosted />
      </div>
      <div class="pane" class:hidden={tool !== 'browser'}>
        <BrowserPane active={open && tool === 'browser'} {animating} />
      </div>
    </div>
  </div>
</aside>

<style>
  /* 中间线定主列宽（--chat-w，默认 42vw 保底 480），抽屉吃掉剩余全部
  （100vw - 侧栏 - 主列）：窗口缩放时抽屉跟着伸缩、主列与中间线不动；
  抽屉保底 640：浏览器/终端/资源页都按这个下限布局，不允许再压 */
  .drawer {
    /* 主列优先让出宽度：窗口变窄时主列收缩，抽屉至少保留 640；
    但「侧栏展开(176)+主列保底(480)+640」超视口时（壳 minWidth 1184 按
    收起侧栏算的，展开即溢出）抽屉必须让位——封顶到主列保底后的剩余，
    底线 280 保住头部弹出/收起钮在窗内，不再顶出窗外 */
    --chat-w: min(var(--chat-w-pref, max(480px, 42vw)), calc(100vw - var(--sidebar-w, 64px) - 640px));
    --pane-w: min(
      max(640px, calc(100vw - var(--sidebar-w, 64px) - var(--chat-w))),
      max(280px, calc(100vw - var(--sidebar-w, 64px) - 480px))
    );
    position: relative;
    flex: none;
    width: 0;
    overflow: hidden;
    background: var(--bg);
    border-left: 1px solid var(--line);
  }
  .drawer.open {
    width: var(--pane-w);
  }
  /* width 过渡只在开关抽屉的动画窗内启用：平时宽度随窗口/侧栏即时
  跟随（有过渡会滞后——main 先吃掉增量，中间线跳过去再弹回来） */
  .drawer.animating:not(.resizing) {
    transition: width 0.28s var(--ease-out);
  }
  /* 左缘调宽把手：细热区 + 悬停显形指示线 */
  .resizer {
    position: absolute;
    top: 0;
    bottom: 0;
    left: 0;
    width: 7px;
    z-index: 5;
    cursor: col-resize;
  }
  .resizer::after {
    content: '';
    position: absolute;
    top: 0;
    bottom: 0;
    left: 3px;
    width: 2px;
    background: transparent;
    transition: background var(--dur-fast) var(--ease-out);
  }
  .resizer:hover::after,
  .resizer.on::after {
    background: var(--accent-soft, var(--accent));
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
    padding: 12px 12px 12px 14px;
    border-bottom: 1px solid var(--line);
    background: var(--bg);
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
  /* 脱离中的光标幽灵：跟着指针的幽灵卡片（拖拽期不开窗口） */
  .ghost {
    position: fixed;
    z-index: 60;
    transform: translate(-50%, -50%);
    pointer-events: none;
    padding: 6px 14px;
    border: 1px solid var(--accent-soft, var(--accent));
    border-radius: 10px;
    background: color-mix(in srgb, var(--bg) 80%, transparent);
    color: var(--accent, #2563eb);
    font-size: var(--font-size-ui);
    font-weight: 600;
    box-shadow: 0 6px 20px rgb(0 0 0 / 18%);
  }
  /* 标题两行：主名 + 一句用途；操作按钮右对齐 */
  .head-text {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  h3 {
    margin: 0;
    font-size: var(--font-size-body);
    font-weight: 700;
    line-height: 1.25;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .head-text p {
    margin: 0;
    font-size: var(--font-size-ui);
    line-height: 1.25;
    color: var(--faint);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .actions {
    flex: none;
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .pop {
    display: grid;
    place-items: center;
    width: 28px;
    height: 28px;
    border: none;
    background: transparent;
    color: var(--muted);
    border-radius: 8px;
    cursor: pointer;
    flex: none;
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .pop:hover {
    background: var(--line);
    color: var(--fg);
  }
  /* 目录面板开着：开关钮亮着（面板从标题栏正下方展开） */
  .pop.active {
    background: var(--accent-soft, color-mix(in srgb, #2563eb 10%, transparent));
    color: var(--accent, #2563eb);
  }
  .pop svg {
    width: 15px;
    height: 15px;
  }
  .body {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    padding: 12px 12px 12px 16px;
    transition: padding 0.26s var(--ease-out); /* 与目录树收放同步 */
  }
  /* 目录钉住：抽屉内容满铺（树/内容直接贴边），不再留一圈白缝 */
  .body.treedocked {
    padding: 0;
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
