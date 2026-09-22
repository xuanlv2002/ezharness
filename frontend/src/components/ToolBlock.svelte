<script lang="ts">
  import type { ToolBlockData } from '../lib/store.svelte'

  let { data }: { data: ToolBlockData } = $props()
  /* 详情一律默认折叠（头部右侧的一行摘要已够辨识）；只有用户手动点开
     才展开。别做"执行中默认展开、完成自动折叠"——状态切换时高度
     反复变化，配吸底就是整条时间线的滚动条抖动 */
  let open = $state(false)

  const shown = $derived(open)

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
    <div class="dwrap" class:closed={!shown || !tail}>
      <div class="detail">
        <pre class="stream">{tail}</pre>
      </div>
    </div>
  {:else}
    <div class="dwrap" class:closed={!shown}>
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
    max-height: 180px;
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
  /* 展开/折叠：max-height 过渡 + overflow hidden 硬裁剪（折叠态 0 高度，
     绝无内容泄漏——grid-rows 0fr 技巧在本 Chromium 上裁不住，出过
     "折叠了参数还露出来"的事故）。高度骤变曾是吸底抖动主源，过渡
     让吸底跟随平滑 */
  .dwrap {
    overflow: hidden;
    max-height: 520px;
    transition: max-height 0.2s var(--ease-out);
  }
  .dwrap.closed {
    max-height: 0;
  }
  .detail {
    border-top: 1px solid var(--line);
    padding: 10px 12px;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .dwrap.closed .detail {
    border-top-color: transparent;
  }
  .section .label {
    display: block;
    font-size: 10px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--faint);
    margin-bottom: 4px;
  }
  /* building 尾预览与 running 全文的可见高度上限统一：阶段切换
     （tail→pretty）不再跳高，配 max-height 过渡进一步抹平 */
  pre {
    font-family: var(--font-mono);
    font-size: 12px;
    line-height: 1.55;
    white-space: pre-wrap;
    word-break: break-all;
    max-height: 180px;
    overflow-y: auto;
    transition: max-height 0.18s var(--ease-out);
  }
  pre.err {
    color: var(--fg);
    text-decoration: underline wavy;
    text-decoration-color: var(--faint);
  }
</style>
