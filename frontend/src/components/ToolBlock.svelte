<script lang="ts">
  import type { ToolBlockData } from '../lib/store.svelte'

  let { data }: { data: ToolBlockData } = $props()
  /* null＝用户未操作：未完成（building/running）默认展开（过程可见），
     done 默认折叠；用户点击后固定展开态 */
  let open = $state<boolean | null>(null)

  /* 当前可见性：未操作时执行中展开、完成折叠 */
  const shown = $derived(open ?? data.state !== 'done')

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

  /* done 态折叠时结果首行摘要：不用展开即可大致知道干了什么 */
  const preview = $derived.by(() => {
    if (data.state !== 'done') return ''
    const src = (data.err || data.result || '').trim()
    if (!src) return ''
    const line = src.split(/\r?\n/).find((l) => l.trim()) || ''
    const one = line.trim().replace(/\s+/g, ' ')
    return one.length > 90 ? one.slice(0, 90) + '…' : one
  })
</script>

<div class="tool enter-rise" class:done={data.state === 'done'}>
  <button class="head" onclick={() => (open = !shown)}>
    <span class="arrow" class:open={shown}>{shown ? '▾' : '▸'}</span>
    {#if data.state === 'running'}
      <span class="spinner"></span>
    {:else if data.state === 'building'}
      <span class="breath"></span>
    {:else}
      <span class="dot"></span>
    {/if}
    <span class="name">{data.name || 'tool'}</span>
    {#if data.decision}
      <span class="dec-tag" class:rej={data.decision.startsWith('已拒绝')}
        >{data.decision.startsWith('已批准') ? '✓ 审批' : data.decision}</span
      >
    {/if}
    {#if data.state === 'building'}
      <span class="building-tag">构造中 {nchars} 字</span>
    {/if}
    {#if preview && !shown}
      <span class="preview" title={preview}>{preview}</span>
    {/if}
    {#if data.state === 'done' && data.err}
      <span class="err-tag">error</span>
    {/if}
  </button>
  {#if data.state === 'building'}
    {#if shown && tail}
      <div class="detail">
        <pre class="stream">{tail}</pre>
      </div>
    {/if}
  {:else if shown}
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
  .dec-tag {
    font-size: 10px;
    color: var(--muted);
    border: 1px solid var(--line);
    border-radius: 999px;
    padding: 1px 7px;
    max-width: 220px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .dec-tag.rej {
    color: #f85149;
    border-color: rgb(248 81 73 / 30%);
  }
  .building-tag {
    margin-left: auto;
    font-size: 10px;
    color: var(--faint);
    font-variant-numeric: tabular-nums;
  }
  /* 折叠态结果首行摘要：右侧弹性截断，与 error 徽标同侧 */
  .preview {
    margin-left: auto;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 11px;
    color: var(--faint);
  }
  .preview + .err-tag {
    margin-left: 8px;
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
