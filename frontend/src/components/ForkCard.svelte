<script lang="ts">
  /*
  分身入口卡：主时间线里 task 调用的唯一呈现（过程细节一律进抽屉）。
  点击打开 ForkPanel 查看完整执行记录；done 后附答案首行预览。
  */
  import { store } from '../lib/store.svelte'
  import type { ForkState } from '../lib/store.svelte'

  let { fork }: { fork: ForkState | undefined } = $props()

  function clip(s: string): string {
    const t = s.trim().replace(/\s+/g, ' ')
    return t.length > 120 ? t.slice(0, 120) + '…' : t
  }
</script>

{#if fork}
  <button class="entry" class:done={fork.status === 'done'} onclick={() => store.openFork(fork.id)}>
    {#if fork.status === 'running'}
      <span class="breathe"></span>
    {:else}
      <span class="ended"></span>
    {/if}
    <span class="fid">{fork.id}</span>
    <span class="task" title={fork.task}>{fork.task || '子任务'}</span>
    {#if fork.status === 'running'}
      <span class="state">运行中</span>
    {:else}
      <span class="state dim">{fork.stopReason || 'done'}</span>
      {#if fork.answer}
        <span class="ans" title={fork.answer}>{clip(fork.answer)}</span>
      {/if}
    {/if}
    <span class="go">查看 ›</span>
  </button>
{/if}

<style>
  .entry {
    display: flex;
    align-items: center;
    gap: 10px;
    width: calc(100% - 40px); /* 与 ToolBlock 等长：扣掉左缩进，不超出右缘 */
    margin-left: var(--proc-indent, 40px);
    padding: 6px 12px;
    border: 1px solid var(--line-strong);
    border-radius: 10px;
    background: transparent;
    text-align: left;
    cursor: pointer;
    transition: background var(--dur-fast) var(--ease-out);
  }
  .entry:hover {
    background: var(--bg-soft);
  }
  .entry.done {
    border-color: var(--line);
    opacity: 0.9;
  }
  .breathe {
    flex: none;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--accent);
    animation: breathe 2.4s ease-in-out infinite;
  }
  @keyframes breathe {
    0%,
    100% {
      opacity: 0.35;
      transform: scale(0.85);
    }
    50% {
      opacity: 1;
      transform: scale(1.05);
    }
  }
  .ended {
    flex: none;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--line);
  }
  .fid {
    font-family: var(--font-mono);
    font-size: 11px;
    font-weight: 700;
    color: var(--accent);
    flex: none;
  }
  .entry.done .fid {
    color: var(--muted);
  }
  .task {
    flex: 1;
    min-width: 80px;
    font-size: 13px;
    color: var(--fg);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .state {
    flex: none;
    font-size: 11px;
    color: var(--accent);
    font-family: var(--font-mono);
  }
  .state.dim {
    color: var(--faint);
  }
  .ans {
    flex: none;
    max-width: 260px;
    font-size: 11px;
    color: var(--faint);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .go {
    flex: none;
    font-size: 11px;
    color: var(--muted);
  }
  .entry:hover .go {
    color: var(--accent);
  }
</style>
