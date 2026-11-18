<script lang="ts">
  /* 会话标签条：打开的会话各占一标签，关标签会停掉该线正在跑的一轮
     （会话本身留在列表）。零标签自隐藏。 */
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

  /* 高度上报：折行后 tabbar 变高，对话页两侧悬浮列的 top 跟随避让 */
  let barH = $state(0)
  $effect(() => {
    store.tabbarH = barH
  })</script>

<svelte:window onpointerdown={onGlobalDown} onkeydown={(e) => e.key === 'Escape' && (menu = null)} />

<div class="tabbar" class:empty={!store.tabs.length} bind:clientHeight={barH}>
  {#if store.tabs.length}
    <div class="strip">
      {#each store.tabs as t (t.rootId)}
        {@const info = store.branches.find((b) => b.id === t.rootId)}
        <button
          class="tab"
          class:active={t.rootId === store.activeId}
          onclick={() => store.openTab(t.rootId)}
          onauxclick={(e) => e.button === 1 && store.closeTab(t.rootId)}
          oncontextmenu={(e) => openMenu(e, t.rootId)}
          title={(info?.title || '新对话') + (t.rootId === store.activeId ? '（当前会话）' : '')}
        >
          <span class="dot" class:run={info?.running}></span>
          <span class="name">{info?.title || '新对话'}</span>
          {#if info?.waiting}<span class="wait" title="等待审批">⚠</span>{/if}
          <span
            class="x"
            role="button"
            tabindex="-1"
            title="关闭标签（停止该线当前轮）"
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
  {/if}
</div>

{#if menu}
  <div class="tabmenu" style="left:{menu.x}px; top:{menu.y}px" role="menu">
    <button role="menuitem" onclick={() => run((id) => store.closeTab(id))}>关闭</button>
    <button role="menuitem" disabled={!hasOthers} onclick={() => run((id) => store.closeOtherTabs(id))}>关闭其他</button>
    <button role="menuitem" disabled={!hasRight} onclick={() => run((id) => store.closeRightTabs(id))}>关闭右侧</button>
  </div>
{/if}

<style>
  /* 无独立底色与分割线：标签条与内容同底，只有标签自身是胶囊 */
  .tabbar {
    flex: none;
    /* 主列已占满内容区：标签条自身限宽居中，与消息流/输入框同列 */
    width: 100%;
    max-width: 780px;
    margin: 0 auto;
    padding: 5px 10px;
  }
  /* 无标签不渲染（也不显示底线），但根元素常在以回报高度 0 */
  .tabbar.empty {
    display: none;
  }
  .strip {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    flex-wrap: wrap; /* 放不下的 tab 折行：不裁不滚，全部可点 */
  }
  .tab {
    display: flex;
    align-items: center;
    gap: 6px;
    flex: 0 1 auto;
    min-width: 72px; /* 名字至少留约 3 字，收不到只剩点+叉的残骸 */
    border: none;
    background: transparent;
    border-radius: 8px;
    padding: 4px 5px 4px 9px;
    font-size: var(--font-size-ui);
    color: var(--muted);
    max-width: 190px;
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .tab:hover {
    background: color-mix(in srgb, var(--bg-soft) 60%, transparent);
    color: var(--fg);
  }
  .tab.active {
    background: var(--bg-soft);
    color: var(--fg);
    font-weight: 600;
  }
  .name {
    min-width: 0;
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
    font-size: var(--font-size-small);
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
    border: none;
    background: transparent;
    border-radius: 7px;
    color: var(--muted);
    font-size: var(--font-size-section);
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
    font-size: var(--font-size-ui);
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
