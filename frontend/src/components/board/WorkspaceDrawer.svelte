<script lang="ts">
  import { store } from '../../lib/store.svelte'
  import TerminalTab from './TerminalTab.svelte'
  import FilePane from './FilePane.svelte'

  /*
  工作区抽屉：推挤式右布局列（非悬浮 overlay）——打开时占布局宽度，
  聊天主列被挤窄不遮挡。工具页（终端/文件）互斥显隐，双 pane 常驻
  挂载保活（终端 WS/xterm、文件 buffer/备注/选区互不丢失）；入口是
  右侧悬浮列的两个独立 mini 钮（store.toggleDrawerTool）。
  宽度与视口联动让位：主列保住 ~782px 内容安全宽（80 字符代码行不出
  横向滚动），抽屉在余量内取宽；余量不足先压抽屉、保底 280px。
  动画 = width 展开（内层 .inner 恒宽 var(--term-w)：过渡期间内容不
  重排、xterm 不反复 fit；窗口 resize 时 --term-w 变化经 var 继承
  同步到 inner，ResizeObserver 自动 fit）。
  */
  const open = $derived(store.termDrawerOpen)
  const tool = $derived(store.drawerTool)
</script>

<aside class="drawer" class:open={open} role="complementary" aria-label="工作区">
  <div class="inner">
    <header>
      <h3>{tool === 'term' ? '共享终端' : '文件'}</h3>
      <span class="hint">{tool === 'term' ? '用户与 AI 共写 · 收起不中断' : '查看 · 编辑 · 发给 AI'}</span>
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
        <FilePane active={open && tool === 'file'} />
      </div>
    </div>
  </div>
</aside>

<style>
  /* 782px = 主列内容安全宽（96 inner padding + 34 双侧滚动槽 + 28
  卡 padding + 624 八十字符代码行）；侧栏宽经 --sidebar-w 联动
  （收 64 / 展 176）。余量不足时抽屉先让位，保底 280 */
  .drawer {
    --term-w: max(280px, min(620px, 42vw, calc(100vw - var(--sidebar-w, 64px) - 782px)));
    flex: none;
    width: 0;
    overflow: hidden;
    background: var(--bg);
    border-left: 1px solid var(--line);
    transition: width 0.28s var(--ease-out);
  }
  .drawer.open {
    width: var(--term-w);
  }
  /* 内层恒宽（继承 --term-w）：外层宽度过渡时内容不重排；窗口
  resize 时 --term-w 变化随之更新，RO fit 跟随实际宽 */
  .inner {
    display: flex;
    flex-direction: column;
    width: var(--term-w);
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
