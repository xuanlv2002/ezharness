<script lang="ts">
  import type { ForkState } from '../lib/store.svelte'

  let { fork }: { fork: ForkState | undefined } = $props()

  let body: HTMLDivElement | undefined = $state()

  function clip(s: string): string {
    const t = s.trim()
    return t.length > 160 ? t.slice(0, 160) + '…' : t
  }

  // 分身内容流式增长时，卡内独立滚动跟随底部
  $effect(() => {
    if (!fork || !body) return
    void fork.text.length
    void fork.reasoning.length
    if (fork.status === 'running') body.scrollTop = body.scrollHeight
  })
</script>

{#if fork}
  <div class="fork enter-rise" class:done={fork.status === 'done'}>
    <button class="head" onclick={() => (fork.collapsed = !fork.collapsed)}>
      {#if fork.status === 'running'}
        <span class="breathe"></span>
      {:else}
        <span class="ended"></span>
      {/if}
      <span class="fid">{fork.id}</span>
      <span class="task" class:hidden={fork.collapsed && fork.answer}>{fork.task || '子任务'}</span>
      <span class="spacer"></span>
      {#if fork.status === 'running'}
        <span class="state">运行中</span>
      {:else}
        <span class="state dim">{fork.stopReason || 'done'}</span>
      {/if}
      <span class="arrow">{fork.collapsed ? '▸' : '▾'}</span>
    </button>

    {#if !fork.collapsed}
      <div class="body" bind:this={body}>
        {#if fork.reasoning}
          <details class="reasoning">
            <summary>思考过程</summary>
            <div class="rtext">{fork.reasoning}</div>
          </details>
        {/if}
        {#if fork.text}
          <div class="text">
            {fork.text}
            {#if fork.status === 'running'}<span class="caret"></span>{/if}
          </div>
        {/if}
        {#if fork.tools.length > 0}
          <div class="tools">
            {#each fork.tools as t (t.id)}
              <div class="trow" class:tdone={t.state === 'done'} class:terr={!!t.err}>
                <span class="tdot"></span>
                <span class="tname">{t.name}</span>
                {#if t.state === 'running'}<span class="tstate">…</span>{/if}
              </div>
            {/each}
          </div>
        {/if}
        {#if fork.status === 'running' && !fork.text && fork.tools.length === 0 && !fork.reasoning}
          <div class="text"><span class="caret"></span></div>
        {/if}
      </div>
    {:else if fork.answer}
      <div class="answer">{clip(fork.answer)}</div>
    {/if}
  </div>
{/if}

<style>
  .fork {
    margin-left: 40px;
    border: 1px solid var(--line-strong);
    border-radius: 12px;
    overflow: hidden;
    background: var(--bg);
  }
  .fork.done {
    border-color: var(--line);
  }
  .head {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    border: none;
    background: transparent;
    padding: 8px 14px;
    text-align: left;
    transition: background var(--dur-fast) var(--ease-out);
  }
  .head:hover {
    background: var(--bg-soft);
  }
  .breathe {
    flex: none;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--accent);
    animation: breathe 2.4s ease-in-out infinite;
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
    letter-spacing: 0.02em;
  }
  .fork.done .fid {
    color: var(--muted);
  }
  .task {
    font-size: 13px;
    color: var(--fg);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 420px;
  }
  .task.hidden {
    max-width: 340px;
  }
  .spacer {
    flex: 1;
  }
  .state {
    font-size: 11px;
    color: var(--accent);
    font-family: var(--font-mono);
  }
  .state.dim {
    color: var(--faint);
  }
  .arrow {
    color: var(--faint);
    font-size: 10px;
  }
  .body {
    border-top: 1px solid var(--line);
    max-height: 300px;
    overflow-y: auto;
    padding: 12px 16px;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .text {
    font-size: 13.5px;
    white-space: pre-wrap;
    word-break: break-word;
    line-height: 1.65;
  }
  .caret {
    display: inline-block;
    width: 7px;
    height: 15px;
    margin-left: 2px;
    vertical-align: -2px;
    background: var(--fg);
    animation: caret 1s steps(1) infinite;
  }
  .reasoning {
    border-left: 2px solid var(--line);
    padding-left: 10px;
    color: var(--muted);
    font-size: 12px;
  }
  .reasoning summary {
    cursor: pointer;
    font-size: 11px;
    user-select: none;
  }
  .rtext {
    white-space: pre-wrap;
    margin-top: 4px;
  }
  .tools {
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  .trow {
    display: flex;
    align-items: center;
    gap: 7px;
    font-family: var(--font-mono);
    font-size: 11.5px;
    color: var(--muted);
  }
  .tdot {
    width: 4px;
    height: 4px;
    border-radius: 50%;
    background: var(--accent);
  }
  .trow.tdone .tdot {
    background: var(--faint);
  }
  .trow.terr .tname {
    text-decoration: underline wavy;
    text-decoration-color: var(--faint);
  }
  .tstate {
    color: var(--accent);
  }
  .answer {
    padding: 8px 14px;
    font-size: 12.5px;
    color: var(--muted);
    border-top: 1px solid var(--line);
    white-space: pre-wrap;
    word-break: break-word;
    max-height: 84px;
    overflow: hidden;
  }
</style>
