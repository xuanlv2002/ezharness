<script lang="ts">
  import { store } from '../../lib/store.svelte'
  import TerminalTab from './TerminalTab.svelte'

  /*
  共享终端抽屉：推挤式右布局列（非悬浮 overlay）——打开时占布局宽度，
  聊天主列被挤窄不遮挡。TerminalTab 常驻挂载（WS/xterm 保活）。
  宽度与视口联动让位：主列保住 ~748px 内容安全宽（80 字符代码行不出
  横向滚动），终端在余量内取宽；余量不足先压终端、保底 280px。
  收起 = 负 margin 卷帘：盒子恒宽（内容不重排、xterm 不反复 fit），
  仅布局占位归零，溢出部分由 .app overflow hidden 裁切。
  */
  const open = $derived(store.termDrawerOpen)
</script>

<aside class="drawer" class:open={open} role="complementary" aria-label="共享终端">
  <div class="inner">
    <header>
      <h3>共享终端</h3>
      <span class="hint">用户与 AI 共写 · 收起不中断</span>
      <button class="close" onclick={() => store.closeTermDrawer()} title="收起">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
          <path d="M6 6l12 12M18 6L6 18" />
        </svg>
      </button>
    </header>
    <div class="body">
      <TerminalTab active={open} />
    </div>
  </div>
</aside>

<style>
  /* 782px = 主列内容安全宽（96 inner padding + 34 双侧滚动槽 + 28
  卡 padding + 624 八十字符代码行）；侧栏宽经 --sidebar-w 联动
  （收 64 / 展 176）。余量不足时终端先让位，保底 280 */
  .drawer {
    --term-w: max(280px, min(620px, 42vw, calc(100vw - var(--sidebar-w, 64px) - 782px)));
    flex: none;
    width: var(--term-w);
    margin-right: calc(-1 * var(--term-w));
    background: var(--bg);
    border-left: 1px solid var(--line);
    transition:
      margin-right 0.28s var(--ease-out),
      width 0.28s var(--ease-out);
  }
  .drawer.open {
    margin-right: 0;
  }
  .inner {
    display: flex;
    flex-direction: column;
    width: 100%;
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
</style>
