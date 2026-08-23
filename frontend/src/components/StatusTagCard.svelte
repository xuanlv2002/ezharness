<script lang="ts">
  import type { StatusPayload } from '../lib/api'

  let { data, raw }: { data: StatusPayload | null; raw: string } = $props()

  const hm = $derived(data ? data.now.slice(11) : '')
  const pct = $derived(
    data && data.ctxWindow > 0 ? Math.min(100, (data.ctxTokens / data.ctxWindow) * 100) : 0,
  )
  const fmt = (n: number) => (n >= 10000 ? Math.round(n / 1000) + 'k' : String(n))
</script>

<!-- 仅异常时渲染（推荐压缩/资源变更），一条细警示行 -->
<div class="alert">
  {#if data}
    <span class="t">{hm}</span>
    {#if data.suggestCompact}
      <span class="mono">{fmt(data.ctxTokens)}/{fmt(data.ctxWindow)}</span>
      <span class="warn">推荐压缩</span>
    {/if}
    {#each data.changes || [] as c (c)}
      <span class={c.startsWith('+') ? 'add' : 'del'}>{c}</span>
    {/each}
  {:else}
    <pre class="raw">{raw}</pre>
  {/if}
</div>

<style>
  .alert {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
    padding: 3px 12px;
    font-size: 11.5px;
    color: var(--muted);
    border-left: 2px solid #d29922;
  }
  .t {
    font-family: var(--font-mono);
    color: var(--faint);
  }
  .mono {
    font-family: var(--font-mono);
  }
  .warn {
    color: #d29922;
    font-weight: 600;
  }
  .add {
    color: #3fb950;
    font-family: var(--font-mono);
  }
  .del {
    color: #f85149;
    font-family: var(--font-mono);
  }
  .raw {
    margin: 0;
    font-family: var(--font-mono);
    font-size: 11.5px;
    white-space: pre-wrap;
    word-break: break-all;
    max-height: 120px;
    overflow-y: auto;
  }
</style>
