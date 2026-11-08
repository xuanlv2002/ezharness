<script lang="ts">
  /*
  顶栏工具三键（终端/浏览器/资源）：紧贴搜索框右侧——位置固定，不随
  右侧便签小点的增减而挪动。角标 = 本窗口该工具打开的 tab 数（终端按
  会话数、浏览器按标签数、资源按文件 tab 数）。点击语义：已弹出为独立
  窗口 = 聚焦窗口；不在对话页（抽屉看不见）= 回对话页并保证拉开；在
  对话页 = 开合切换（开着且是该页=收起，否则拉开/切换）。
  */
  import { store, type DrawerTool } from '../lib/store.svelte'
  import { paneTabs } from '../lib/paneTabs.svelte'

  function toggleTool(t: DrawerTool): void {
    if (store.popoutTools.includes(t)) {
      store.toggleDrawerTool(t)
      return
    }
    if (store.view !== 'chat') {
      store.jumpView = 'chat'
      store.drawerTool = t
      store.termDrawerOpen = true
      return
    }
    store.toggleDrawerTool(t)
  }

  function toolOn(t: DrawerTool): boolean {
    return (store.termDrawerOpen && store.drawerTool === t) || store.popoutTools.includes(t)
  }
</script>

<div class="tools" role="group" aria-label="工作区工具">
  <button
    class="tbtn"
    class:on={toolOn('term')}
    onclick={() => toggleTool('term')}
    title="共享终端（用户与 AI 共写）——点开/收起抽屉"
    aria-label="共享终端"
  >
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
      <path d="M5 8l4 4-4 4" />
      <path d="M12 16.5h7" />
    </svg>
    {#if paneTabs.term}<i class="badge">{paneTabs.term}</i>{/if}
  </button>
  {#if (window as any).ez?.browser}
    <button
      class="tbtn"
      class:on={toolOn('browser')}
      onclick={() => toggleTool('browser')}
      title="共享浏览器（AI 操控 · 实时共见）——点开/收起抽屉"
      aria-label="共享浏览器"
    >
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
        <circle cx="12" cy="12" r="9" />
        <path d="M3 12h18M12 3a15 15 0 0 1 0 18M12 3a15 15 0 0 0 0 18" />
      </svg>
      {#if paneTabs.browser}<i class="badge">{paneTabs.browser}</i>{/if}
    </button>
  {/if}
  <button
    class="tbtn"
    class:on={toolOn('file')}
    onclick={() => toggleTool('file')}
    title="资源（查看 · 编辑 · 发给 AI）——点开/收起抽屉"
    aria-label="资源"
  >
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
      <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" />
      <path d="M14 3v5h5" />
      <path d="M9 13h6M9 17h6" />
    </svg>
    {#if paneTabs.file}<i class="badge">{paneTabs.file}</i>{/if}
  </button>
</div>

<style>
  .tools {
    display: flex;
    align-items: center;
    gap: 2px;
    height: 100%;
    app-region: no-drag;
  }
  .tbtn {
    position: relative;
    display: grid;
    place-items: center;
    width: 28px;
    height: 26px;
    border-radius: 7px;
    color: var(--muted);
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .tbtn svg {
    width: 14px;
    height: 14px;
  }
  .tbtn:hover,
  .tbtn.on {
    background: var(--bg);
    color: var(--fg);
  }
  /* tab 数角标：反色小胶囊（浅色深底白字 / 深色浅底黑字，两模式高对比） */
  .badge {
    position: absolute;
    top: 1px;
    right: 1px;
    min-width: 12px;
    height: 12px;
    padding: 0 2px;
    display: grid;
    place-items: center;
    border-radius: 6px;
    background: var(--bg-invert);
    color: var(--fg-invert);
    font-size: 9px;
    font-weight: 700;
    font-style: normal;
    line-height: 1;
  }
</style>
