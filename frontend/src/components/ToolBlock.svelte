<script lang="ts">
  import type { ToolBlockData } from '../lib/store.svelte'

  let { data }: { data: ToolBlockData } = $props()
  let open = $state(false)

  function prettyArgs(raw: string): string {
    try {
      return JSON.stringify(JSON.parse(raw), null, 2)
    } catch {
      return raw
    }
  }

  /* building 态参数尾部预览（tail 跟随，像日志一样看内容增长） */
  const tail = $derived(data.state === 'building' ? data.args.slice(-600) : '')
  const nchars = $derived(data.args.length)
</script>

<div class="tool enter-rise" class:done={data.state === 'done'}>
  <button class="head" onclick={() => (open = !open)}>
    <span class="arrow" class:open>{open ? '▾' : '▸'}</span>
    {#if data.state === 'running'}
      <span class="spinner"></span>
    {:else if data.state === 'building'}
      <span class="breath"></span>
    {:else}
      <span class="dot"></span>
    {/if}
    <span class="name">{data.name || 'tool'}</span>
    {#if data.state === 'building'}
      <span class="building-tag">构造中 {nchars} 字</span>
    {/if}
    {#if data.state === 'done' && data.err}
      <span class="err-tag">error</span>
    {/if}
  </button>
  {#if data.state === 'building'}
    {#if tail}
      <div class="detail">
        <pre class="stream">{tail}</pre>
      </div>
    {/if}
  {:else if open}
    <div class="detail">
      {#if data.args}
        <div class="section">
          <span class="label">args</span>
          <pre>{prettyArgs(data.args)}</pre>
        </div>
      {/if}
      {#if data.result || data.err}
        <div class="section">
          <span class="label">result</span>
          <pre class:err={!!data.err}>{data.err || data.result}</pre>
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .tool {
    margin-left: 40px;
    border: 1px solid var(--line);
    border-radius: 10px;
    overflow: hidden;
  }
  .tool.done {
    opacity: 0.85;
  }
  .head {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    border: none;
    background: transparent;
    padding: 7px 12px;
    font-family: var(--font-mono);
    font-size: 12.5px;
    text-align: left;
    transition: background var(--dur-fast) var(--ease-out);
  }
  .head:hover {
    background: var(--bg-soft);
  }
  .arrow {
    color: var(--faint);
    font-size: 10px;
    transition: transform var(--dur-fast) var(--ease-out);
  }
  .name {
    color: var(--fg);
  }
  .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--line-strong);
  }
  .spinner {
    width: 10px;
    height: 10px;
    border: 1.5px solid var(--line);
    border-top-color: var(--line-strong);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }
  .breath {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--line-strong);
    animation: breath 1.4s ease-in-out infinite;
  }
  @keyframes breath {
    0%,
    100% {
      opacity: 0.25;
      transform: scale(0.85);
    }
    50% {
      opacity: 1;
      transform: scale(1.05);
    }
  }
  .building-tag {
    margin-left: auto;
    font-size: 10px;
    color: var(--faint);
    font-variant-numeric: tabular-nums;
  }
  pre.stream {
    font-family: var(--font-mono);
    font-size: 11.5px;
    line-height: 1.5;
    white-space: pre-wrap;
    word-break: break-all;
    max-height: 140px;
    overflow: hidden;
    color: var(--muted);
    mask-image: linear-gradient(to bottom, transparent, #000 28px);
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
  .err-tag {
    margin-left: auto;
    font-size: 10px;
    color: var(--bg);
    background: var(--bg-invert);
    padding: 1px 6px;
    border-radius: 4px;
  }
  .detail {
    border-top: 1px solid var(--line);
    padding: 10px 12px;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .section .label {
    display: block;
    font-size: 10px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--faint);
    margin-bottom: 4px;
  }
  pre {
    font-family: var(--font-mono);
    font-size: 12px;
    line-height: 1.55;
    white-space: pre-wrap;
    word-break: break-all;
    max-height: 280px;
    overflow-y: auto;
  }
  pre.err {
    color: var(--fg);
    text-decoration: underline wavy;
    text-decoration-color: var(--faint);
  }
</style>
