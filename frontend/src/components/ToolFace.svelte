<script lang="ts">
  import { toolFace, diffLines, highlightCommand } from '../lib/toolface'

  let { name, raw }: { name: string; raw: string } = $props()

  const face = $derived(toolFace(name, raw))
  const dlines = $derived(face?.kind === 'diff' ? diffLines(face.old, face.new) : [])
  const cmdHtml = $derived(face?.kind === 'cmd' ? highlightCommand(face.command) : '')
  const codeLines = $derived(face?.kind === 'code' ? face.content.split('\n') : null)
  const numbered = $derived(!!codeLines && codeLines.length <= 800)

  function pretty(raw: string): string {
    try {
      return JSON.stringify(JSON.parse(raw), null, 2)
    } catch {
      return raw
    }
  }
</script>

{#if face?.kind === 'code'}
  <div class="tf">
    {#if face.path}<div class="tf-path">{face.path}</div>{/if}
    {#if numbered}
      <pre class="tf-code ln">{#each codeLines as line}<span>{line || ' '}</span>{/each}</pre>
    {:else}
      <pre class="tf-code">{face.content}</pre>
    {/if}
  </div>
{:else if face?.kind === 'diff'}
  <div class="tf">
    {#if face.path}<div class="tf-path">{face.path}</div>{/if}
    <pre class="tf-diff">{#each dlines as l, i}{#if l.t === '-'}<span class="del">{l.s || ' '}</span>{:else if l.t === '+'}<span class="add">{l.s || ' '}</span>{:else}<span class="ctx">{l.s || ' '}</span>{/if}{i < dlines.length - 1 ? '\n' : ''}{/each}</pre>
  </div>
{:else if face?.kind === 'cmd'}
  <div class="tf">
    <pre class="tf-cmd">{@html cmdHtml}</pre>
  </div>
{:else if face?.kind === 'plan'}
  <div class="tf">
    {#if face.goal}<p class="tf-goal">{face.goal}</p>{/if}
    {#if face.design}<pre class="tf-plan">{face.design}</pre>{/if}
    {#if face.progress.length}
      <ul class="tf-steps">
        {#each face.progress as step, i (i)}
          <li class={step.status}>
            <span class="ico">{step.status === 'completed' ? '✓' : step.status === 'running' ? '●' : '○'}</span>
            <span>{step.title}</span>
          </li>
        {/each}
      </ul>
    {:else if !face.goal && !face.design}
      <span class="tf-empty">清除当前计划</span>
    {/if}
  </div>
{:else}
  <pre class="tf-json">{pretty(raw)}</pre>
{/if}

<style>
  .tf {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
  }
  .tf-path {
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    color: var(--faint);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  pre {
    font-family: var(--font-mono);
    font-size: var(--font-size-ui);
    line-height: 1.55;
    white-space: pre-wrap;
    word-break: break-all;
    max-height: 200px;
    overflow-y: auto;
    margin: 0;
  }
  .tf-json {
    color: var(--fg);
  }
  /* 行号:CSS 计数器画在每行左侧,行内容随滚动 */
  .tf-code.ln {
    counter-reset: ln;
    white-space: pre;
    overflow-x: auto;
  }
  .tf-code.ln span {
    display: block;
    counter-increment: ln;
    padding-left: 3.2em;
    text-indent: -3.2em;
  }
  .tf-code.ln span::before {
    content: counter(ln);
    display: inline-block;
    width: 2.6em;
    margin-right: 0.6em;
    text-align: right;
    color: var(--faint);
    user-select: none;
  }
  .tf-diff .del {
    display: block;
    background: color-mix(in srgb, #f85149 12%, transparent);
    color: #f85149;
    white-space: pre-wrap;
  }
  .tf-diff .add {
    display: block;
    background: color-mix(in srgb, #3fb950 12%, transparent);
    color: #3fb950;
    white-space: pre-wrap;
  }
  .tf-diff .ctx {
    display: block;
    color: var(--muted);
    white-space: pre-wrap;
  }
  .tf-cmd {
    color: var(--fg);
  }
  .tf-goal {
    font-weight: 600;
    color: var(--fg);
  }
  .tf-plan {
    white-space: pre-wrap;
    color: var(--muted);
  }
  .tf-steps {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .tf-steps li {
    display: flex;
    align-items: flex-start;
    gap: 6px;
    font-size: var(--font-size-ui);
    line-height: 1.45;
    color: var(--muted);
  }
  .tf-steps li.running {
    color: var(--fg);
  }
  .tf-steps li.completed {
    color: var(--faint);
  }
  .tf-steps .ico {
    flex: none;
    width: 12px;
    text-align: center;
    font-size: var(--font-size-small);
  }
  .tf-steps li.running .ico {
    color: var(--accent);
  }
  .tf-steps li.completed .ico {
    color: #3fb950;
  }
  .tf-empty {
    font-size: var(--font-size-ui);
    color: var(--muted);
  }
  .tf-cmd :global(.tf-word) {
    font-weight: 700;
    color: var(--accent);
  }
  .tf-cmd :global(.tf-flag) {
    color: #58a6ff;
  }
  .tf-cmd :global(.tf-str) {
    color: #3fb950;
  }
  .tf-cmd :global(.tf-var) {
    color: #d29a63;
  }
  .tf-cmd :global(.tf-op) {
    color: var(--faint);
    font-weight: 700;
  }
</style>
