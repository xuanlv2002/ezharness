<script lang="ts">
  import { store } from '../lib/store.svelte'

  function fmtK(n: number): string {
    if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
    if (n >= 1000) return `${(n / 1000).toFixed(1)}k`
    return String(n)
  }

  const s = $derived(store.status)
  const live = $derived(store.live)
  const ctx = $derived(live?.ctxTokens || s?.contextTokens || 0)
  const win = $derived(live?.ctxWindow || 0)
  const hot = $derived(live?.suggestCompact ?? false)
  const pct = $derived(win > 0 ? Math.min(100, (ctx / win) * 100) : 0)
  const context = $derived(ctx > 0 ? (win > 0 ? `${fmtK(ctx)}/${fmtK(win)}` : fmtK(ctx)) : '—')
</script>

<aside class="card">
  <div class="head">
    <h2>状态</h2>
    {#if live?.changes?.length}
      <span class="changes" title={live.changes.join('\n')}>{live.changes.join(' · ')}</span>
    {/if}
  </div>
  <div class="ctxrow">
    <div class="row">
      <span class="label">当前上下文{hot ? '（推荐压缩）' : ''}</span>
      <span class="value" class:hot>{context}</span>
    </div>
    {#if win > 0}
      <div class="bar">
        <div class="fill" class:hot style="width:{pct}%"></div>
      </div>
    {/if}
  </div>
  <div class="row">
    <span class="label">已服务天数</span>
    <span class="value">{s ? `${s.daysServed} 天` : '—'}</span>
  </div>
  <div class="row">
    <span class="label">缓存命中率</span>
    <span class="value">{s && s.totalTokens > 0 ? `${(s.cacheHitRate * 100).toFixed(0)}%` : '—'}</span>
  </div>
  <div class="row">
    <span class="label">累计用量</span>
    <span class="value">{s && s.totalTokens > 0 ? fmtK(s.totalTokens) : '—'}</span>
  </div>
  <div class="row">
    <span class="label">当前工具</span>
    <span class="value">{store.lastTool || '—'}</span>
  </div>
</aside>

<style>
  .card {
    display: flex;
    flex-direction: column;
    gap: 6px;
    width: 100%;
    padding: 12px 14px;
    background: var(--bg);
    border: 1px solid var(--line);
    border-radius: 12px;
    box-shadow: 0 1px 3px rgb(0 0 0 / 4%);
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
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: #3fb950;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
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
    height: 4px;
    border-radius: 2px;
    background: var(--line);
    overflow: hidden;
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
</style>
