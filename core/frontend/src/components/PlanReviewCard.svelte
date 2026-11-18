<script lang="ts">
  import { store } from '../lib/store.svelte'
  import type { DecisionData } from '../lib/store.svelte'

  /*
  计划确认卡正文与动作：三段（目标/方案/步骤）只读展示，确认即按模型提交的
  内容落盘；有异议就写反馈交回模型重做。卡片外壳（头部/折叠/已解决态）由
  DecisionCard 统一承载。
  */
  let { data }: { data: DecisionData } = $props()

  let feedback = $state('')
  let err = $state('')

  const total = $derived(data.progress.length)
  const done = $derived(data.progress.filter((p) => p.status === 'completed').length)

  function confirm() {
    err = ''
    void store.decidePlan(data, 'confirm', '')
  }

  function sendFeedback() {
    const fb = feedback.trim()
    if (!fb) {
      err = '先写下要调整什么；直接执行请点「确认计划」'
      return
    }
    err = ''
    void store.decidePlan(data, 'feedback', fb)
  }

  function onFeedbackKey(e: KeyboardEvent) {
    e.stopPropagation()
    if (e.key === 'Enter' && !e.isComposing && feedback.trim()) {
      e.preventDefault()
      sendFeedback()
    }
  }
</script>

<div class="body">
  {#if data.goal}
    <section class="goal">
      <p class="label">目标</p>
      <p class="goal-text">{data.goal}</p>
    </section>
  {/if}
  {#if data.context}
    <section>
      <p class="label">背景</p>
      <p class="doc">{data.context}</p>
    </section>
  {/if}
  {#if data.design}
    <section>
      <p class="label">方案</p>
      <p class="doc">{data.design}</p>
    </section>
  {/if}
  {#if total}
    <section>
      <p class="label">步骤<span class="count">{done > 0 ? `${done}/${total}` : `${total} 步`}</span></p>
      <ol class="steps">
        {#each data.progress as p, i (i)}
          <li class={p.status}>
            <span class="idx">
              {#if p.status === 'running'}<i class="dot"></i>{:else if p.status === 'completed'}✓{:else}{i + 1}{/if}
            </span>
            <span class="tt">{p.title}</span>
          </li>
        {/each}
      </ol>
    </section>
  {/if}
  {#if data.verify}
    <section>
      <p class="label">验证</p>
      <p class="doc">{data.verify}</p>
    </section>
  {/if}
  {#if !data.goal && !data.context && !data.design && !total && !data.verify}
    <p class="err">计划内容解析失败：请提交反馈，让模型重新提交。</p>
  {/if}
  {#if err}
    <p class="err">{err}</p>
  {/if}
</div>

{#if !data.resolved}
  <div class="actions">
    <input
      type="text"
      class="fb"
      placeholder="哪里要调整？（填写后按 Enter 提交反馈）"
      bind:value={feedback}
      onkeydown={onFeedbackKey}
    />
    <button class="btn ghost" disabled={!feedback.trim()} onclick={sendFeedback}>提交反馈</button>
    <button class="btn solid" onclick={confirm}>确认计划</button>
  </div>
{/if}

<style>
  .body {
    display: flex;
    flex-direction: column;
    gap: 14px;
    padding: 14px 16px;
  }
  .label {
    display: flex;
    align-items: baseline;
    gap: 8px;
    margin-bottom: 5px;
    font-size: var(--font-size-small);
    letter-spacing: 0.08em;
    color: var(--faint);
  }
  .count {
    margin-left: auto;
    font-family: var(--font-mono);
    letter-spacing: 0;
    font-variant-numeric: tabular-nums;
  }
  /* 目标是整张卡的重心：淡强调底 + 描边，一眼先读它 */
  .goal {
    padding: 10px 12px;
    border-radius: 9px;
    background: color-mix(in srgb, var(--accent) 6%, var(--bg-soft));
    border: 1px solid color-mix(in srgb, var(--accent) 16%, var(--line));
  }
  .goal .label {
    margin-bottom: 4px;
  }
  .goal-text {
    font-size: var(--font-size-body);
    font-weight: 600;
    line-height: 1.5;
    color: var(--fg);
    white-space: pre-wrap;
    word-break: break-word;
  }
  /* 背景/方案/验证共用正文样式：长方案限高滚动，确认卡不把时间线撑穿 */
  .doc {
    max-height: 240px;
    overflow-y: auto;
    font-size: var(--font-size-ui);
    line-height: 1.6;
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
    gap: 5px;
  }
  .steps li {
    display: flex;
    align-items: flex-start;
    gap: 9px;
    font-size: var(--font-size-ui);
    line-height: 1.45;
    color: var(--muted);
  }
  .steps li.running {
    color: var(--fg);
  }
  .steps li.completed {
    color: var(--faint);
  }
  .idx {
    flex: none;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 17px;
    height: 17px;
    margin-top: 1px;
    border: 1px solid var(--line-strong);
    border-radius: 50%;
    font-family: var(--font-ui);
    font-size: 10px;
    font-weight: 600;
    line-height: 1;
    font-variant-numeric: tabular-nums;
    color: var(--faint);
  }
  /* 进行中用实心圆点而不是 ● 字形：字形在不同字体里基线偏移，圆点永远居中 */
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
  li.completed .idx {
    border-color: color-mix(in srgb, #3fb950 45%, var(--line));
    color: #3fb950;
  }
  li.completed .tt {
    text-decoration: line-through;
    text-decoration-color: var(--line-strong);
  }
  .tt {
    min-width: 0;
    word-break: break-word;
  }
  .err {
    font-size: var(--font-size-ui);
    color: var(--danger);
  }
  .actions {
    display: flex;
    gap: 8px;
    padding: 10px 14px;
    border-top: 1px solid var(--line);
  }
  .fb {
    flex: 1;
    min-width: 0;
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 7px 10px;
    font-family: inherit;
    font-size: var(--font-size-section);
    outline: none;
    transition: border-color var(--dur-fast) var(--ease-out);
  }
  .fb:focus {
    border-color: var(--line-strong);
  }
  .btn {
    border: 1px solid var(--line-strong);
    background: transparent;
    border-radius: 8px;
    padding: 7px 14px;
    font-size: var(--font-size-section);
    font-weight: 500;
    white-space: nowrap;
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out),
      opacity var(--dur-fast) var(--ease-out);
  }
  .btn.ghost:hover:not(:disabled) {
    background: var(--bg-invert);
    color: var(--fg-invert);
  }
  .btn.solid {
    background: var(--bg-invert);
    color: var(--fg-invert);
  }
  .btn.solid:hover {
    opacity: 0.85;
  }
  .btn:disabled {
    opacity: 0.45;
    cursor: default;
  }
</style>
