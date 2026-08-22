<script lang="ts">
  import type { StatusPayload } from '../lib/api'

  let { data, raw }: { data: StatusPayload | null; raw: string } = $props()

  const pct = $derived(
    data && data.ctxWindow > 0 ? Math.min(100, (data.ctxTokens / data.ctxWindow) * 100) : 0,
  )
  const fmt = (n: number) => (n >= 10000 ? Math.round(n / 1000) + 'k' : String(n))
</script>

<div class="scard">
  {#if data}
    <div class="row">
      <span class="t">{data.now}</span>
      {#if data.sinceLastOutputMin > 0}
        <span>距上次输出 {data.sinceLastOutputMin} 分钟</span>
      {/if}
      {#if data.suggestCompact}
        <span class="warn">推荐压缩</span>
      {:else if data.ctxWindow > 0}
        <span class="ok">暂不需压缩</span>
      {/if}
    </div>
    {#if data.ctxWindow > 0}
      <div class="ctx">
        <div class="bar">
          <div class="fill" class:hot={data.suggestCompact} style="width:{pct}%"></div>
        </div>
        <span class="mono">上下文 {fmt(data.ctxTokens)}/{fmt(data.ctxWindow)}</span>
      </div>
    {/if}
    {#if data.mcp?.length}
      <div class="chips">
        {#each data.mcp as m (m.name)}
          <span class="chip" title={m.desc}>{m.name}</span>
        {/each}
      </div>
    {/if}
    {#if data.changes?.length}
      <div class="changes">
        {#each data.changes as c (c)}
          <span class={c.startsWith('+') ? 'add' : 'del'}>{c}</span>
        {/each}
      </div>
    {/if}
  {:else}
    <pre class="raw">{raw}</pre>
  {/if}
</div>

<style>
  .scard {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 10px 14px;
    border: 1px solid var(--line);
    border-radius: 10px;
    background: rgba(127, 127, 127, 0.06);
    font-size: 12px;
    color: var(--muted);
  }
  .row {
    display: flex;
    align-items: center;
    gap: 14px;
    flex-wrap: wrap;
  }
  .t {
    font-family: var(--font-mono);
    color: var(--faint);
  }
  .warn {
    color: #d29922;
    font-weight: 600;
  }
  .ok {
    color: var(--faint);
  }
  .ctx {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .bar {
    flex: 1;
    height: 4px;
    border-radius: 2px;
    background: var(--line);
    overflow: hidden;
  }
  .fill {
    height: 100%;
    border-radius: 2px;
    background: #58a6ff;
    transition: width 0.3s var(--ease-out);
  }
  .fill.hot {
    background: #f0883e;
  }
  .mono {
    font-family: var(--font-mono);
    white-space: nowrap;
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .chip {
    padding: 1px 8px;
    border: 1px solid var(--line);
    border-radius: 999px;
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--muted);
  }
  .changes {
    display: flex;
    flex-direction: column;
    gap: 2px;
    font-family: var(--font-mono);
    font-size: 11.5px;
  }
  .add {
    color: #3fb950;
  }
  .del {
    color: #f85149;
  }
  .raw {
    margin: 0;
    font-family: var(--font-mono);
    font-size: 11.5px;
    white-space: pre-wrap;
    word-break: break-all;
    max-height: 160px;
    overflow-y: auto;
  }
</style>
