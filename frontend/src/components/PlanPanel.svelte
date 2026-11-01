<script lang="ts">
  import { store } from '../lib/store.svelte'

  /* 输入框上方的计划面板（只读）：折叠显示目标与进度，展开看目标/方案/步骤。
     计划是结构化三段（goal/approach/progress），不再解析正文行。 */
  const plan = $derived(store.plan)
  const running = $derived(plan?.progress.find((p) => p.status === 'running') ?? null)
  const done = $derived(plan?.progress.filter((p) => p.status === 'completed').length ?? 0)
  const total = $derived(plan?.progress.length ?? 0)
  const allDone = $derived(total > 0 && done === total)
  let open = $state(false)
  const peek = $derived(plan?.goal || '')
</script>

{#if plan && (plan.goal || plan.approach || total)}
  <div class="plan" class:finished={allDone}>
    <button class="head" onclick={() => (open = !open)} title={open ? '收起计划' : '展开完整计划'}>
      <span class="label">任务计划</span>
      {#if running}
        <span class="now"><span class="spin"></span>正在 {running.title}</span>
      {:else if allDone}
        <span class="now done">✓ 全部完成</span>
      {:else}
        <span class="now idle">等待推进</span>
      {/if}
      <span class="count">{done}/{total}</span>
      <span class="arrow">{open ? '▾' : '▸'}</span>
    </button>
    {#if open}
      <div class="detail">
        {#if plan.goal}
          <section class="goal">
            <p class="label">目标</p>
            <p class="goal-text">{plan.goal}</p>
          </section>
        {/if}
        {#if plan.approach}
          <section>
            <p class="label">方案</p>
            <p class="approach">{plan.approach}</p>
          </section>
        {/if}
        {#if total}
          <section>
            <p class="label">步骤</p>
            <ol class="steps">
              {#each plan.progress as p, i (i)}
                <li class:running={p.status === 'running'} class:done={p.status === 'completed'}>
                  <span class="idx">
                    {#if p.status === 'running'}<i class="dot"></i>{:else if p.status === 'completed'}✓{:else}{i + 1}{/if}
                  </span>
                  <span class="tt">{p.title}</span>
                </li>
              {/each}
            </ol>
          </section>
        {/if}
      </div>
    {:else if peek}
      <p class="peek">{peek}</p>
    {/if}
  </div>
{/if}

<style>
  .plan {
    margin: 0 4px 10px;
    border: 1px solid var(--line);
    border-radius: 10px;
    background: var(--bg-soft);
    overflow: hidden;
  }
  .plan.finished {
    opacity: 0.75;
  }
  .head {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 7px 10px;
    border: none;
    background: transparent;
    text-align: left;
    color: var(--fg);
  }
  .head:hover {
    background: color-mix(in srgb, var(--bg-invert) 4%, transparent);
  }
  .label {
    flex: none;
    font-size: var(--font-size-small);
    letter-spacing: 0.08em;
    color: var(--faint);
  }
  .now {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--font-size-ui);
    color: var(--fg);
  }
  .now.idle {
    color: var(--muted);
  }
  .now.done {
    color: #3fb950;
  }
  .spin {
    flex: none;
    width: 9px;
    height: 9px;
    border: 1.5px solid var(--line);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
  .count {
    flex: none;
    margin-left: auto;
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    color: var(--faint);
    font-variant-numeric: tabular-nums;
  }
  .arrow {
    flex: none;
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .peek {
    margin: 0;
    padding: 0 10px 7px 10px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--font-size-ui);
    color: var(--muted);
  }
  .detail {
    display: flex;
    flex-direction: column;
    gap: 10px;
    max-height: 260px;
    overflow-y: auto;
    padding: 0 10px 10px;
  }
  .detail .label {
    margin-bottom: 4px;
  }
  .goal {
    padding: 8px 10px;
    border-radius: 8px;
    background: color-mix(in srgb, var(--accent) 6%, var(--bg));
    border: 1px solid color-mix(in srgb, var(--accent) 16%, var(--line));
  }
  .goal-text {
    margin: 0;
    font-size: var(--font-size-ui);
    font-weight: 600;
    line-height: 1.5;
    color: var(--fg);
    white-space: pre-wrap;
    word-break: break-word;
  }
  .approach {
    margin: 0;
    font-size: var(--font-size-ui);
    line-height: 1.55;
    color: var(--muted);
    white-space: pre-wrap;
    word-break: break-word;
  }
  .steps {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .steps li {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    font-size: var(--font-size-ui);
    line-height: 1.45;
    color: var(--muted);
  }
  .steps li.running {
    color: var(--fg);
  }
  .steps li.done {
    color: var(--faint);
  }
  .idx {
    flex: none;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 16px;
    height: 16px;
    margin-top: 1px;
    border: 1px solid var(--line-strong);
    border-radius: 50%;
    font-family: var(--font-ui);
    font-size: 9px;
    font-weight: 600;
    line-height: 1;
    font-variant-numeric: tabular-nums;
    color: var(--faint);
  }
  /* 进行中用实心圆点而不是 ● 字形：字形基线在不同字体里偏移，圆点永远居中 */
  .idx .dot {
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: currentColor;
  }
  li.running .idx {
    border-color: var(--accent);
    color: var(--accent);
  }
  li.done .idx {
    border-color: color-mix(in srgb, #3fb950 45%, var(--line));
    color: #3fb950;
  }
  li.done .tt {
    text-decoration: line-through;
    text-decoration-color: var(--line-strong);
  }
  .tt {
    min-width: 0;
    word-break: break-word;
  }
</style>
