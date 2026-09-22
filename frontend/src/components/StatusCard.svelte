<script lang="ts">
  import { store } from '../lib/store.svelte'

  function fmtK(n: number): string {
    if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
    if (n >= 1000) return `${(n / 1000).toFixed(1)}k`
    return String(n)
  }

  /* 收起为小方块（显示上下文占用），localStorage 记忆收起态 */
  const collapsedKey = 'ezh.statusCard.collapsed'
  let collapsed = $state((() => {
    try {
      return localStorage.getItem(collapsedKey) === '1'
    } catch {
      return false
    }
  })())
  function fold(v: boolean) {
    collapsed = v
    try {
      localStorage.setItem(collapsedKey, v ? '1' : '0')
    } catch {
      /* 存储不可用时仅本次生效 */
    }
  }

  const s = $derived(store.status)
  const live = $derived(store.live)
  const ctx = $derived(live?.ctxTokens || s?.contextTokens || 0)
  const win = $derived(live?.ctxWindow || s?.contextWindow || 0)
  const hot = $derived(live?.suggestCompact ?? false)
  const pct = $derived(win > 0 ? Math.min(100, (ctx / win) * 100) : 0)
  const cp = $derived(s?.trimPercent ?? 0)
  const remain = $derived(cp > 0 ? cp - pct : 0)
  const barTitle = $derived(
    win > 0 && cp > 0
      ? remain > 0
        ? `自动整理阈值 ${cp}% · 当前 ${pct.toFixed(0)}% · 还差 ${remain.toFixed(0)}%`
        : `自动整理阈值 ${cp}% · 当前 ${pct.toFixed(0)}%，已达阈值，迭代回边自动整理`
      : '自动整理已禁用（0%）',
  )
  const context = $derived(ctx > 0 ? (win > 0 ? `${fmtK(ctx)}/${fmtK(win)}` : fmtK(ctx)) : '-')
  const hit = $derived(s && s.promptTokens > 0 ? `${(s.cacheHitRate * 100).toFixed(0)}%` : '-')
  const tools = $derived(s?.tools ?? [])
  const mcp = $derived(s?.mcpServers ?? [])
  const skills = $derived(s?.skills ?? [])
</script>

{#if collapsed}
  <button class="mini" onclick={() => fold(false)} title="状态 · 点击展开">
    <span class="mini-pct" class:hot>{win > 0 ? `${pct.toFixed(0)}%` : '—'}</span>
  </button>
{:else}
  <aside class="card">
    <div class="head">
      <h2>状态</h2>
      {#if store.liveChanges.length}
        <span class="changes" title={store.liveChanges.join('\n')}>{store.liveChanges.join(' · ')}</span>
      {/if}
      <button class="fold" onclick={() => fold(true)} title="收起">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round">
          <path d="M5 12h14" />
        </svg>
      </button>
    </div>
  <div class="ctxrow">
    <div class="row">
      <span class="label">当前上下文{hot ? '（推荐压缩）' : ''}</span>
      <span class="value" class:hot>{context}</span>
    </div>
    {#if win > 0}
      <div class="bar" title={barTitle}>
        <div class="fill" class:hot style="width:{pct}%"></div>
        {#if cp > 0}
          <div class="mark" style="left:{cp}%"></div>
        {/if}
      </div>
    {/if}
  </div>
  <div class="row">
    <span class="label">缓存命中率</span>
    <span class="value">{hit}</span>
  </div>
  <div class="caps">
    <span class="label">工具</span>
    {#if tools.length}
      <div class="chips">{#each tools as t (t)}<i class="chip">{t}</i>{/each}</div>
    {:else}<span class="none">-</span>{/if}
  </div>
  <div class="caps">
    <span class="label">MCP</span>
    {#if mcp.length}
      <div class="chips">{#each mcp as m (m)}<i class="chip">{m}</i>{/each}</div>
    {:else}<span class="none">-</span>{/if}
  </div>
  <div class="caps">
    <span class="label">Skill</span>
    {#if skills.length}
      <div class="chips">{#each skills as k (k)}<i class="chip">{k}</i>{/each}</div>
    {:else}<span class="none">-</span>{/if}
  </div>
  </aside>
{/if}

<style>
  .card {
    display: flex;
    flex-direction: column;
    gap: 6px;
    width: 100%;
    max-height: 100%;
    overflow-y: auto;
    padding: 12px 14px;
    background: var(--glass);
    backdrop-filter: var(--glass-blur);
    border: 1px solid var(--line);
    border-radius: 12px;
    box-shadow: 0 1px 3px rgb(0 0 0 / 4%);
  }
  /* 收起态小方块：上下文占用一目了然，点击展开；靠右贴边（.side 列的末端对齐） */
  .mini {
    flex: none;
    align-self: flex-end;
    display: grid;
    place-items: center;
    width: 38px;
    height: 38px;
    border: 1px solid var(--line);
    border-radius: 10px;
    background: var(--glass);
    backdrop-filter: var(--glass-blur);
    cursor: pointer;
    padding: 0;
  }
  .mini:hover {
    border-color: var(--line-strong);
  }
  .mini-pct {
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--muted);
  }
  .mini-pct.hot {
    color: #d29922;
    font-weight: 600;
  }
  .head {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 2px;
  }
  h2 {
    font-size: 12px;
    font-weight: 700;
    color: var(--muted);
  }
  .changes {
    flex: 1;
    min-width: 0;
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: #3fb950;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .fold {
    flex: none;
    margin-left: auto;
    display: grid;
    place-items: center;
    width: 20px;
    height: 20px;
    border: none;
    border-radius: 5px;
    background: transparent;
    color: var(--faint);
    cursor: pointer;
    opacity: 0;
    transition: opacity var(--dur-fast) var(--ease-out);
  }
  .card:hover .fold {
    opacity: 1;
  }
  .fold:hover {
    background: var(--bg-soft);
    color: var(--fg);
  }
  .fold svg {
    width: 11px;
    height: 11px;
  }
  .ctxrow {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding-bottom: 4px;
  }
  .row {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    gap: 16px;
  }
  .label {
    font-size: 11px;
    color: var(--faint);
    white-space: nowrap;
  }
  .label:has(+ .value.hot) {
    color: #d29922;
  }
  .value {
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--fg);
    white-space: nowrap;
  }
  .value.hot {
    color: #d29922;
    font-weight: 600;
  }
  .bar {
    position: relative;
    height: 4px;
    border-radius: 2px;
    background: var(--line);
  }
  .fill {
    height: 100%;
    border-radius: 2px;
    background: #2563eb;
    transition: width 0.3s var(--ease-out);
  }
  .fill.hot {
    background: #d29922;
  }
  /* 自动压缩阈值线：上下出头 2px，红线与蓝色水位、琥珀热色区分 */
  .mark {
    position: absolute;
    top: -2px;
    bottom: -2px;
    width: 2px;
    margin-left: -1px;
    border-radius: 1px;
    background: #f85149;
  }
  .caps {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding-top: 2px;
  }
  .none {
    font-size: 11px;
    color: var(--faint);
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
  }
  .chip {
    font-family: var(--font-mono);
    font-style: normal;
    font-size: 10px;
    color: var(--muted);
    border: 1px solid var(--line);
    border-radius: 4px;
    padding: 1px 5px;
    white-space: nowrap;
  }
</style>
