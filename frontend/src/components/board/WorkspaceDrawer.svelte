<script lang="ts">
  import { store } from '../../lib/store.svelte'
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
</script>

<svelte:window onkeydown={onKey} />

<aside class="drawer" class:open={open} role="complementary" aria-label="工作区">
  <div class="inner">
    <header>
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
  header {
    display: flex;
    align-items: center;
    gap: 10px;
    flex: none;
    padding: 14px 12px 8px 16px;
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
