<script lang="ts">
  /*
  顶栏右侧：便签小点（板上每张待办一颗，点开挂下完整便签）、主题切换
  与全局通知铃铛。会话级的状态不在这里（见输入条内的 SessionStatus）。
  工作区工具三键在搜索框左侧（TitleBar 的 TopTools）。
  */
  import { onMount } from 'svelte'
  import { store } from '../lib/store.svelte'
  import { theme, toggleTheme } from '../lib/theme.svelte'
  import { board, ensurePinboard } from '../lib/pinboard.svelte'
  import NoticePanel from './NoticePanel.svelte'
  import PinRack from './pinboard/PinRack.svelte'

  let noticeOpen = $state(false)
  let pinsSelected = $state('')
  let now = $state(Date.now())

  onMount(() => {
    /* 顶栏便签小点常驻：先拉一次板，小点才有数据 */
    void ensurePinboard()
    const t = setInterval(() => (now = Date.now()), 30000)
    return () => clearInterval(t)
  })

  const pending = $derived(store.notices.filter((n) => n.status === 'pending').length)
  const info = $derived(store.notices.filter((n) => n.kind === 'info').length)

  $effect(() => {
    if (pinsSelected) noticeOpen = false
  })

  function toggleNotice(): void {
    noticeOpen = !noticeOpen
    if (noticeOpen) pinsSelected = ''
  }
</script>

<svelte:window
  onkeydown={(e) => {
    if (e.key !== 'Escape') return
    if (pinsSelected) pinsSelected = ''
    else if (noticeOpen) noticeOpen = false
  }}
/>

<div class="rail">
  {#if pinsSelected}
    <div class="scrim" role="presentation" onclick={() => (pinsSelected = '')}></div>
  {/if}
  {#if board.cards.length}
    <div class="pins-entry" class:lifted={!!pinsSelected}>
      <PinRack {now} bind:selected={pinsSelected} />
    </div>
  {/if}
  <button
    class="rail-btn theme"
    onclick={toggleTheme}
    title={theme.current === 'dark' ? '切换到浅色' : '切换到暗色'}
  >
    {#if theme.current === 'dark'}
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
        <circle cx="12" cy="12" r="4" />
        <path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
      </svg>
    {:else}
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
        <path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z" />
      </svg>
    {/if}
  </button>
  <button
    class="rail-btn bell"
    class:on={noticeOpen}
    onclick={toggleNotice}
    title={pending > 0 ? `通知（${pending} 条待处理）` : '通知'}
    aria-haspopup="dialog"
    aria-expanded={noticeOpen}
  >
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
      <path d="M18 8a6 6 0 0 0-12 0c0 7-3 9-3 9h18s-3-2-3-9" />
      <path d="M13.7 21a2 2 0 0 1-3.4 0" />
    </svg>
    {#if pending > 0}
      <i class="badge">{pending > 9 ? '9+' : pending}</i>
    {:else if info > 0}
      <i class="dot"></i>
    {/if}
  </button>

  {#if noticeOpen}
    <div class="scrim" role="presentation" onclick={() => (noticeOpen = false)}></div>
    <div class="popover" role="dialog" aria-label="通知">
      <NoticePanel
        notices={store.notices}
        onClose={() => (noticeOpen = false)}
        onResolve={(id, action, input) => {
          const n = store.notices.find((x) => x.id === id)
          if (n) void store.resolveNoticeGlobal(n, action, input)
        }}
        onJump={(n) => {
          noticeOpen = false
          void store.jumpToNotice(n)
        }}
        onDismiss={(id) => store.dismissNotice(id)}
      />
    </div>
  {/if}
</div>

<style>
  .rail {
    position: relative;
    display: flex;
    align-items: center;
    gap: 2px;
    height: 100%;
    app-region: no-drag;
  }
  /* 便签小点入口：完整便签层从点区右缘向下挂 */
  .pins-entry {
    position: relative;
    display: flex;
    align-items: center;
    height: 100%;
  }
  /* 展开时抬到遮罩之上，点小点仍可直接操作 */
  .pins-entry.lifted {
    z-index: 73;
  }
  .rail-btn {
    position: relative;
    display: flex;
    align-items: center;
    gap: 5px;
    height: 26px;
    padding: 0 7px;
    border-radius: 7px;
    color: var(--muted);
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .rail-btn:hover,
  .rail-btn.on {
    background: var(--bg);
    color: var(--fg);
  }
  .rail-btn svg {
    width: 14px;
    height: 14px;
  }
  .bell,
  .theme {
    padding: 0 8px;
  }
  .badge {
    position: absolute;
    top: 0;
    right: 0;
    min-width: 14px;
    height: 14px;
    padding: 0 3px;
    display: grid;
    place-items: center;
    border-radius: 7px;
    background: var(--accent);
    color: #fff;
    font-size: 9px;
    font-weight: 700;
    font-style: normal;
    line-height: 1;
  }
  .dot {
    position: absolute;
    top: 4px;
    right: 5px;
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--accent);
  }
  /* 透明遮罩：点浮层外任意处收起 */
  .scrim {
    position: fixed;
    inset: 0;
    z-index: 70;
  }
  .popover {
    position: absolute;
    top: calc(100% + 8px);
    right: 0;
    z-index: 71;
    width: 320px;
    max-height: min(70vh, 620px);
    padding: 14px;
    background: var(--bg);
    border: 1px solid var(--line);
    border-radius: 14px;
    box-shadow:
      0 16px 40px rgb(0 0 0 / 14%),
      0 2px 8px rgb(0 0 0 / 6%);
    overflow-y: auto;
    scrollbar-width: thin;
    scrollbar-color: var(--line-strong) transparent;
    animation: pop-in 140ms var(--ease-out) both;
  }
  @keyframes pop-in {
    from {
      opacity: 0;
      transform: translateY(-4px);
    }
  }
  /* 面板本身是浮层内容：去掉它自己的卡片底/内边距/滚动，
     统一由浮层容器承载 */
  .popover :global(.panel) {
    background: transparent;
    border: none;
    border-radius: 0;
    box-shadow: none;
    padding: 0;
    max-height: none;
    overflow: visible;
  }
  .popover :global(.list) {
    max-height: none;
    overflow: visible;
  }
</style>
