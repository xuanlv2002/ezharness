<script lang="ts">
  import { store } from '../lib/store.svelte'

  function fmtK(n: number): string {
    if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
    if (n >= 1000) return `${(n / 1000).toFixed(1)}k`
    return String(n)
  }

  const s = $derived(store.status)
  const context = $derived((s && s.contextTokens > 0 ? fmtK(s.contextTokens) : '—') as string)
</script>

<aside class="card">
  <div class="head">
    <h2>状态</h2>
  </div>
  <div class="row">
    <span class="label">当前上下文</span>
    <span class="value">{context}</span>
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
  .value {
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--fg);
    white-space: nowrap;
  }
</style>
