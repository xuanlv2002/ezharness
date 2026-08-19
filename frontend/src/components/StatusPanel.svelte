<script lang="ts">
  import { store } from '../lib/store.svelte'

  let s = $derived(store.status)
  let level = $derived(
    s && s.rotateThreshold > 0 ? Math.min(100, (s.contextTokens / s.rotateThreshold) * 100) : 0,
  )
  let hot = $derived(level >= 80)
  let sid = $derived(s ? s.sessionId.slice(-8) : '')
</script>

<aside class="status">
  <div class="title">生命体征</div>

  <section>
    <div class="label">上下文水位</div>
    <div class="bar">
      <div class="fill" class:hot style="width: {level}%"></div>
    </div>
    <div class="kv">
      <span>{s?.contextTokens ?? 0} tokens</span>
      <span class="dim">/ {s?.rotateThreshold || '∞'}</span>
    </div>
  </section>

  <section>
    <div class="label">当前会话</div>
    <div class="kv mono">
      <span class="dot" class:busy={s?.busy}></span>
      <span>{sid}</span>
    </div>
    <div class="kv dim">{s?.sessionMsgs ?? 0} 条消息 · {s?.busy ? '运行中' : '空闲'}</div>
  </section>

  <section>
    <div class="label">累计用量</div>
    <div class="kv mono">{store.total.prompt} → {store.total.completion}</div>
    <div class="kv dim">缓存命中 {store.total.cached}</div>
  </section>

  <section>
    <div class="label">模型</div>
    <div class="kv mono">{s?.model || '—'}</div>
  </section>

  <section>
    <div class="label">工具 {s?.tools.length ?? 0}</div>
    <div class="chips">
      {#each s?.tools || [] as t (t)}
        <span class="chip">{t}</span>
      {/each}
    </div>
  </section>

  <section>
    <div class="label">MCP</div>
    {#if (s?.mcpServers?.length ?? 0) === 0}
      <div class="kv dim">未配置（mcp.json）</div>
    {:else}
      <div class="chips">
        {#each s?.mcpServers || [] as m (m)}
          <span class="chip accent">{m}</span>
        {/each}
      </div>
    {/if}
  </section>

  <section>
    <div class="label">已归档话题</div>
    <div class="kv mono">{s?.topicsCount ?? 0} 个</div>
  </section>
</aside>

<style>
  .status {
    padding: 18px 16px;
    border-left: 1px solid var(--line);
    background: var(--bg-soft);
    overflow-y: auto;
    font-size: 12px;
    display: flex;
    flex-direction: column;
    gap: 18px;
  }
  .title {
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--faint);
    padding-bottom: 4px;
  }
  section {
    display: flex;
    flex-direction: column;
    gap: 5px;
  }
  .label {
    font-size: 10px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--faint);
  }
  .kv {
    display: flex;
    align-items: center;
    gap: 6px;
    color: var(--fg);
  }
  .kv.dim,
  .dim {
    color: var(--muted);
  }
  .mono {
    font-family: var(--font-mono);
  }
  .bar {
    height: 4px;
    background: var(--line);
    border-radius: 2px;
    overflow: hidden;
  }
  .fill {
    height: 100%;
    background: var(--fg);
    border-radius: 2px;
    transition: width 400ms var(--ease-out);
  }
  .fill.hot {
    background: var(--accent);
  }
  .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--faint);
    flex: none;
  }
  .dot.busy {
    background: var(--accent);
    animation: breathe 2.4s ease-in-out infinite;
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
  }
  .chip {
    font-family: var(--font-mono);
    font-size: 10.5px;
    padding: 2px 7px;
    border: 1px solid var(--line);
    border-radius: 5px;
    color: var(--muted);
  }
  .chip.accent {
    border-color: var(--accent);
    color: var(--accent);
  }
</style>
