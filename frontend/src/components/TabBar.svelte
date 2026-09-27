<script lang="ts">
  /*
  会话标签条：打开的会话各占一标签（点击激活、×/中键关闭、+ 新建），
  关标签只是摘除条目——会话与后台轮不受影响。运行中标签呼吸转点、
  待审批挂角标（branches 3s 轮询驱动）。零标签自隐藏（空状态由 ChatEmpty 呈现）。
  右键菜单：关闭 / 关闭其他 / 关闭右侧（作用对象 = 右键的那个标签，
  与是否活动无关）。
  */
  import { store } from '../lib/store.svelte'

  /* 右键菜单：屏幕坐标定位（fixed），点外部/Escape/执行动作即关 */
  let menu = $state<{ x: number; y: number; rootId: string } | null>(null)
  const menuIdx = $derived(menu ? store.tabs.findIndex((t) => t.rootId === menu.rootId) : -1)
  const hasOthers = $derived(!!menu && store.tabs.length > 1)
  const hasRight = $derived(menuIdx >= 0 && menuIdx < store.tabs.length - 1)

  function openMenu(e: MouseEvent, rootId: string) {
    e.preventDefault()
    menu = {
      x: Math.min(e.clientX, window.innerWidth - 150),
      y: Math.min(e.clientY, window.innerHeight - 130),
      rootId,
    }
  }
  function run(fn: (rootId: string) => void | Promise<void>) {
    const rootId = menu?.rootId
    menu = null
    if (rootId !== undefined) void fn(rootId)
  }
  function onGlobalDown(e: PointerEvent) {
    if (menu && !(e.target as HTMLElement).closest('.tabmenu')) menu = null
  }
</script>

<svelte:window onpointerdown={onGlobalDown} onkeydown={(e) => e.key === 'Escape' && (menu = null)} />

{#if store.tabs.length}
  <div class="tabbar">
    <div class="strip">
      {#each store.tabs as t (t.rootId)}
        {@const info = store.branches.find((b) => b.id === t.rootId)}
        <button
          class="tab"
          class:active={t.rootId === store.activeId}
          onclick={() => store.openTab(t.rootId)}
          onauxclick={(e) => e.button === 1 && store.closeTab(t.rootId)}
          oncontextmenu={(e) => openMenu(e, t.rootId)}
          title={(info?.title || '未命名会话') + (t.rootId === store.activeId ? '（当前会话）' : '')}
        >
          <span class="dot" class:run={info?.running}></span>
          <span class="name">{info?.title || '未命名会话'}</span>
          {#if info?.waiting}<span class="wait" title="等待审批">⚠</span>{/if}
          <span
            class="x"
            role="button"
            tabindex="-1"
            title="关闭标签（会话继续）"
            onclick={(e) => {
              e.stopPropagation()
              store.closeTab(t.rootId)
            }}
          >
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round">
              <path d="M6 6l12 12M18 6L6 18" />
            </svg>
          </span>
        </button>
      {/each}
      <button class="new" onclick={() => store.newChatTab()} title="新建会话标签">+</button>
    </div>
  </div>
{/if}

{#if menu}
  <div class="tabmenu" style="left:{menu.x}px; top:{menu.y}px" role="menu">
    <button role="menuitem" onclick={() => run((id) => store.closeTab(id))}>关闭</button>
    <button role="menuitem" disabled={!hasOthers} onclick={() => run((id) => store.closeOtherTabs(id))}>关闭其他</button>
    <button role="menuitem" disabled={!hasRight} onclick={() => run((id) => store.closeRightTabs(id))}>关闭右侧</button>
  </div>
{/if}

<style>
  .tabbar {
    flex: none;
    border-bottom: 1px solid var(--line);
    background: var(--bg);
    padding: 5px 10px;
  }
  .strip {
    display: flex;
    align-items: center;
    gap: 6px;
    overflow-x: auto;
    scrollbar-width: none;
  }
  .strip::-webkit-scrollbar {
    display: none;
  }
  .tab {
    display: flex;
    align-items: center;
    gap: 6px;
    flex: none;
    border: 1px solid var(--line);
    background: var(--bg);
    border-radius: 8px;
    padding: 3px 5px 3px 9px;
    font-size: 11.5px;
    color: var(--muted);
    max-width: 190px;
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .tab:hover {
    background: var(--bg-soft);
    color: var(--fg);
  }
  .tab.active {
    border-color: var(--line-strong);
    background: var(--bg-soft);
    color: var(--fg);
    font-weight: 600;
  }
  .name {
    max-width: 130px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--line-strong);
    flex: none;
  }
  .dot.run {
    background: var(--accent);
    animation: breathe 1.6s ease-in-out infinite;
  }
  .wait {
    font-size: 10px;
    color: #e67e22;
    flex: none;
  }
  .x {
    display: grid;
    place-items: center;
    width: 16px;
    height: 16px;
    border-radius: 4px;
    color: var(--faint);
    flex: none;
  }
  .x:hover {
    background: var(--line);
    color: var(--fg);
  }
  .x svg {
    width: 9px;
    height: 9px;
  }
  .new {
    flex: none;
    display: grid;
    place-items: center;
    width: 22px;
    height: 22px;
    border: 1px solid var(--line);
    background: var(--bg);
    border-radius: 7px;
    color: var(--muted);
    font-size: 13px;
    line-height: 1;
  }
  .new:hover {
    background: var(--bg-soft);
    color: var(--fg);
  }
  .tabmenu {
    position: fixed;
    z-index: 60;
    display: flex;
    flex-direction: column;
    min-width: 108px;
    padding: 4px;
    border: 1px solid var(--line);
    border-radius: 9px;
    background: var(--bg);
    box-shadow: 0 6px 20px rgb(0 0 0 / 10%);
  }
  .tabmenu button {
    border: none;
    background: transparent;
    text-align: left;
    padding: 6px 10px;
    font-size: 12px;
    color: var(--fg);
    border-radius: 6px;
  }
  .tabmenu button:hover:not(:disabled) {
    background: var(--bg-soft);
  }
  .tabmenu button:disabled {
    color: var(--faint);
    cursor: default;
  }
</style>
