<script lang="ts">
  import { store } from '../lib/store.svelte'
  import type { DecisionData } from '../lib/store.svelte'
  import ToolFace from './ToolFace.svelte'
  import PlanReviewCard from './PlanReviewCard.svelte'

  let { data }: { data: DecisionData } = $props()

  let input = $state('')
  /* 决策完成后自动折叠成一行；点头部可展开回看详情（open 用户操作优先） */
  let open = $state<boolean | null>(null)
  const collapsed = $derived(data.resolved && open !== true)

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
    {#if data.dtype === 'plan'}
      <span class="icon plan">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M4 6h9M4 12h9M4 18h9" />
          <path d="M17 6l1.6 1.6L21.5 4.6" />
        </svg>
      </span>
      <span class="title">任务计划待确认</span>
    {:else if data.dtype === 'approve'}
      <span class="icon approve">?</span>
      <span class="title">审批请求</span>
    {:else if data.dtype === 'mcp-auth'}
      <span class="icon auth">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round">
          <circle cx="8" cy="15" r="4" />
          <path d="M11 12l7-7M16 5l2 2M14 7l2 2" />
        </svg>
      </span>
      <span class="title">MCP 需要授权</span>
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
  {#if data.dtype === 'plan'}
    <PlanReviewCard {data} />
  {:else}
  <div class="content">
    {#if data.dtype === 'mcp-auth'}
      <p class="question">
        服务 <b>{data.name}</b> 需要 OAuth 授权才能调用。点「去授权」在浏览器中完成，完成后本次调用会自动继续。
      </p>
      {#if data.err}
        <p class="err">{data.err}</p>
      {/if}
    {:else if data.dtype === 'ask' && data.question}
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
        <ToolFace name={data.name} raw={data.args} />
      </div>
    {/if}
  </div>

  {#if !data.resolved}
    {#if data.dtype === 'approve'}
      <!-- 键位提示常驻：回车在填了理由时 = 带理由拒绝，不静默改键 -->
      <p class="hint">填写理由后按 Enter 拒绝；点「批准」放行</p>
      <div class="actions">
        <input
          type="text"
          placeholder="拒绝理由（可选）"
          bind:value={input}
          onkeydown={(e) => {
            e.stopPropagation()
            if (e.key === 'Enter' && !e.isComposing && input.trim()) {
              e.preventDefault()
              approve(false)
            }
          }}
        />
        <button class="btn ghost" onclick={() => approve(false)}>拒绝</button>
        <button class="btn solid" onclick={() => approve(true)}>批准</button>
      </div>
    {:else if data.dtype === 'mcp-auth'}
      <div class="actions right">
        <button class="btn ghost" onclick={() => store.decideMcpAuth(data, false, '已取消')}>取消</button>
        <button class="btn solid" disabled={data.busy} onclick={() => store.decideMcpAuth(data, true, '')}>
          {data.busy ? '等待授权…' : '去授权'}
        </button>
      </div>
    {:else}
      <div class="actions">
        <input
          type="text"
          placeholder="输入回答，Enter 发送"
          bind:value={input}
          onkeydown={(e) => {
            e.stopPropagation()
            if (e.key === 'Enter' && !e.isComposing && input.trim()) answer()
          }}
        />
        <button class="btn solid" onclick={answer}>回答</button>
      </div>
    {/if}
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
    font-size: var(--font-size-small);
  }
  .icon {
    display: grid;
    place-items: center;
    width: 18px;
    height: 18px;
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    font-weight: 700;
    border: 1px solid var(--line-strong);
    border-radius: 5px;
  }
  .icon svg {
    width: 11px;
    height: 11px;
  }
  .icon.auth {
    color: var(--accent);
    border-color: color-mix(in srgb, var(--accent) 40%, var(--line-strong));
  }
  .icon.plan {
    color: var(--accent);
    border-color: color-mix(in srgb, var(--accent) 40%, var(--line-strong));
  }
  .title {
    font-size: var(--font-size-section);
    font-weight: 600;
  }
  .from {
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    color: var(--accent);
  }
  .resolution {
    margin-left: auto;
    font-size: var(--font-size-ui);
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
    font-size: var(--font-size-body);
    white-space: pre-wrap;
  }
  .question b {
    font-weight: 700;
    font-family: var(--font-mono);
  }
  .err {
    margin-top: 8px;
    font-size: var(--font-size-ui);
    color: #dc2626;
    word-break: break-word;
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
    font-size: var(--font-size-section);
    font-weight: 500;
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
    font-size: var(--font-size-section);
    font-weight: 700;
  }
  /* 键位提示：常驻一行小字，让「回车=拒绝」可见 */
  .hint {
    padding: 0 14px 8px;
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .actions {
    display: flex;
    gap: 8px;
    padding: 10px 14px;
    border-top: 1px solid var(--line);
  }
  /* 无输入框的动作行（mcp-auth）：按钮靠右 */
  .actions.right {
    justify-content: flex-end;
  }
  .actions.right .btn[disabled] {
    opacity: 0.6;
  }
  .actions input {
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
  .actions input:focus {
    border-color: var(--line-strong);
  }
  .btn {
    border: 1px solid var(--line-strong);
    background: transparent;
    border-radius: 8px;
    padding: 7px 14px;
    font-size: var(--font-size-section);
    font-weight: 500;
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
