<script lang="ts">
  import { store } from '../lib/store.svelte'
  import type { DecisionData } from '../lib/store.svelte'

  let { data }: { data: DecisionData } = $props()

  let input = $state('')
  /* 决策完成后自动折叠成一行；点头部可展开回看详情（open 用户操作优先） */
  let open = $state<boolean | null>(null)
  const collapsed = $derived(data.resolved && open !== true)

  function pretty(raw: string): string {
    try {
      return JSON.stringify(JSON.parse(raw), null, 2)
    } catch {
      return raw
    }
  }

  function approve(yes: boolean) {
    void store.decideApprove(data, yes, yes ? '' : input)
    input = ''
  }
  function answer() {
    void store.decideAnswer(data, input)
    input = ''
  }
  /* 点选即提交：选项文本直接作为回答回传 */
  function decideOption(label: string) {
    void store.decideAnswer(data, label)
  }
</script>

<div class="card enter-float" class:resolved={data.resolved}>
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
  <div
    class="head"
    class:foldable={data.resolved}
    onclick={() => {
      if (data.resolved) open = collapsed
    }}
  >
    {#if data.dtype === 'approve'}
      <span class="icon approve">?</span>
      <span class="title">审批请求</span>
    {:else}
      <span class="icon ask">?</span>
      <span class="title">ez 想问你</span>
    {/if}
    {#if data.forkId}
      <span class="from">⟨{data.forkId}⟩</span>
    {/if}
    {#if data.resolved}
      <span class="resolution">{data.resolution}</span>
      <span class="fold-arrow" class:open={!collapsed}>{collapsed ? '▸' : '▾'}</span>
    {/if}
  </div>

  {#if !collapsed}
  <div class="content">
    {#if data.dtype === 'ask' && data.question}
      <p class="question">{data.question}</p>
      {#if !data.resolved && data.options.length}
        <div class="options">
          {#each data.options as o (o)}
            <button class="opt" onclick={() => decideOption(o)}>{o}</button>
          {/each}
        </div>
      {/if}
    {:else}
      <div class="call">
        <span class="tool-name">{data.name}</span>
        <pre>{pretty(data.args)}</pre>
      </div>
    {/if}
  </div>

  {#if !data.resolved}
    {#if data.dtype === 'approve'}
      <div class="actions">
        <input
          type="text"
          placeholder="拒绝理由（可选）"
          bind:value={input}
          onkeydown={(e) => e.stopPropagation()}
        />
        <button class="btn ghost" onclick={() => approve(false)}>拒绝</button>
        <button class="btn solid" onclick={() => approve(true)}>批准</button>
      </div>
    {:else}
      <div class="actions">
        <input
          type="text"
          placeholder="输入回答，Enter 发送"
          bind:value={input}
          onkeydown={(e) => {
            e.stopPropagation()
            if (e.key === 'Enter' && input.trim()) answer()
          }}
        />
        <button class="btn solid" onclick={answer}>回答</button>
      </div>
    {/if}
  {/if}
  {/if}
</div>

<style>
  .card {
    margin-left: var(--proc-indent, 40px);
    border: 1px solid var(--line-strong);
    border-radius: 12px;
    overflow: hidden;
    background: var(--bg);
  }
  .card.resolved {
    border-color: var(--line);
    opacity: 0.75;
  }
  .head {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 6px 12px;
    border-bottom: 1px solid var(--line);
    background: var(--bg-soft);
  }
  .head.foldable {
    cursor: pointer;
  }
  .head.foldable:hover {
    background: color-mix(in srgb, var(--bg-soft) 60%, var(--bg));
  }
  .fold-arrow {
    margin-left: auto;
    color: var(--faint);
    font-size: 10px;
  }
  .icon {
    display: grid;
    place-items: center;
    width: 18px;
    height: 18px;
    font-family: var(--font-mono);
    font-size: 11px;
    font-weight: 700;
    border: 1px solid var(--line-strong);
    border-radius: 5px;
  }
  .title {
    font-size: 13px;
    font-weight: 650;
  }
  .from {
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--accent);
  }
  .resolution {
    margin-left: auto;
    font-size: 12px;
    color: var(--muted);
    font-family: var(--font-mono);
    max-width: 320px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .content {
    padding: 12px 14px;
  }
  .question {
    font-size: 14.5px;
    white-space: pre-wrap;
  }
  /* 选项按钮：点选即提交，与输入框自由输入二选一 */
  .options {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    margin-top: 10px;
  }
  .opt {
    border: 1px solid var(--line-strong);
    border-radius: 999px;
    background: transparent;
    padding: 6px 16px;
    font-size: 13px;
    font-weight: 550;
    cursor: pointer;
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .opt:hover {
    background: var(--bg-invert);
    color: var(--fg-invert);
  }
  .call {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .tool-name {
    font-family: var(--font-mono);
    font-size: 12.5px;
    font-weight: 700;
  }
  .content pre {
    font-family: var(--font-mono);
    font-size: 12px;
    line-height: 1.55;
    white-space: pre-wrap;
    word-break: break-all;
    max-height: 200px;
    overflow-y: auto;
  }
  .actions {
    display: flex;
    gap: 8px;
    padding: 10px 14px;
    border-top: 1px solid var(--line);
  }
  .actions input {
    flex: 1;
    min-width: 0;
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 7px 10px;
    font-family: inherit;
    font-size: 13px;
    outline: none;
    transition: border-color var(--dur-fast) var(--ease-out);
  }
  .actions input:focus {
    border-color: var(--line-strong);
  }
  .btn {
    border: 1px solid var(--line-strong);
    background: transparent;
    border-radius: 8px;
    padding: 7px 14px;
    font-size: 13px;
    font-weight: 550;
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .btn.solid {
    background: var(--bg-invert);
    color: var(--fg-invert);
  }
  .btn.ghost:hover {
    background: var(--bg-invert);
    color: var(--fg-invert);
  }
  .btn.solid:hover {
    opacity: 0.85;
  }
</style>
