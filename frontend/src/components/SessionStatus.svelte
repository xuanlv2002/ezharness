<script lang="ts">
  /*
  会话级运行状态入口（输入条内）：上下文占用圆环，点开向上弹出状态卡。
  通知是全局的、留在顶栏；状态只属于当前会话，跟输入框同列。
  */
  import { store } from '../lib/store.svelte'
  import StatusCard from './StatusCard.svelte'

  let open = $state(false)

  /* 打开时拉一次状态：会话定时回调与用量随外部操作变化，弹层要现查 */
  function toggle() {
    open = !open
    if (open) void store.refreshStatus()
  }

  const s = $derived(store.status)
  const live = $derived(store.live)
  const ctx = $derived(live?.ctxTokens || s?.contextTokens || 0)
  const win = $derived(live?.ctxWindow || s?.contextWindow || 0)
  const pct = $derived(win > 0 ? Math.min(100, (ctx / win) * 100) : 0)
  const ring = 2 * Math.PI * 8.5
</script>

<div class="status-pick">
  <button
    class="status-btn"
    class:on={open}
    onclick={toggle}
    title={win > 0 ? `上下文 ${pct.toFixed(0)}% · 点击查看运行状态` : '运行状态'}
  >
    <svg class="ring" viewBox="0 0 24 24" aria-hidden="true">
      <circle class="track" cx="12" cy="12" r="8.5" />
      <circle
        class="fill"
        class:hot={pct >= 80}
        cx="12"
        cy="12"
        r="8.5"
        stroke-dasharray={`${(pct / 100) * ring} ${ring}`}
        transform="rotate(-90 12 12)"
      />
    </svg>
    <span class="pct">{win > 0 ? `${pct.toFixed(0)}%` : '—'}</span>
  </button>

  {#if open}
    <div class="scrim" role="presentation" onclick={() => (open = false)}></div>
    <div class="popover" role="dialog" aria-label="运行状态">
      <StatusCard onClose={() => (open = false)} />
    </div>
  {/if}
</div>

<style>
  .status-pick {
    position: relative;
  }
  .status-btn {
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
  .status-btn:hover,
  .status-btn.on {
    background: var(--bg-soft);
    color: var(--fg);
  }
  .status-btn svg {
    width: 14px;
    height: 14px;
  }
  /* 上下文占用圆环：接近阈值转橙 */
  .ring .track {
    fill: none;
    stroke: var(--line);
    stroke-width: 2.6;
  }
  .ring .fill {
    fill: none;
    stroke: var(--accent);
    stroke-width: 2.6;
    stroke-linecap: round;
    transition: stroke-dasharray var(--dur-in) var(--ease-out);
  }
  .ring .fill.hot {
    stroke: #f0883e;
  }
  .pct {
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    font-variant-numeric: tabular-nums;
  }
  /* 透明遮罩：点浮层外任意处收起 */
  .scrim {
    position: fixed;
    inset: 0;
    z-index: 70;
  }
  .popover {
    position: absolute;
    bottom: calc(100% + 8px);
    right: 0;
    z-index: 71;
    width: 320px;
    max-height: min(60vh, 560px);
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
    animation: pop-up 140ms var(--ease-out) both;
  }
  @keyframes pop-up {
    from {
      opacity: 0;
      transform: translateY(4px);
    }
  }
  /* 面板本身是浮层内容：去掉它自己的卡片底/内边距/滚动 */
  .popover :global(.card) {
    background: transparent;
    border: none;
    border-radius: 0;
    box-shadow: none;
    padding: 0;
    max-height: none;
    overflow: visible;
  }
</style>
