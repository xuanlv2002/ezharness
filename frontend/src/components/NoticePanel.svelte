<script lang="ts">
  /*
  通知栏：agent 的人机交互请求（审批/询问/规划）在此集中呈现。
  可内联完成，也可「查看」跳转到时间线对应卡片（如 fork 内的审批块）。
  原型阶段纯渲染层，数据由后端下发（SSE 决策请求帧），回调待业务接入。
  */
  interface Notice {
    id: string
    kind: 'approve' | 'ask' | 'plan' | 'info'
    source: string // 'agent' 或 fork 名
    title: string
    detail?: string
    time: string
    status: 'pending' | 'done'
    resolution?: string
    target?: string // 时间线跳转目标（卡片/块 id）
  }

  let {
    notices = [],
    onResolve,
    onJump,
  }: {
    notices?: Notice[]
    onResolve?: (id: string, action: string, input?: string) => void
    onJump?: (n: Notice) => void
  } = $props()

  const kindMeta: Record<Notice['kind'], { label: string; icon: string }> = {
    approve: { label: '审批', icon: '<path d="M12 3l7 3v5c0 4.5-3 8-7 10-4-2-7-5.5-7-10V6l7-3z"/><path d="M9 12l2 2 4-4"/>' },
    ask: { label: '询问', icon: '<circle cx="12" cy="12" r="9"/><path d="M9.5 9.5a2.5 2.5 0 1 1 3.4 2.3c-.6.3-.9.8-.9 1.4v.3"/><path d="M12 17h.01"/>' },
    plan: { label: '规划', icon: '<path d="M4 6h16M4 12h16M4 18h10"/>' },
    info: { label: '通知', icon: '<circle cx="12" cy="12" r="9"/><path d="M12 8h.01M12 11v5"/>' },
  }

  let answers = $state<Record<string, string>>({})
</script>

<div class="panel">
  <div class="head">
    <h2>通知</h2>
    {#if notices.some((n) => n.status === 'pending')}
      <span class="badge">{notices.filter((n) => n.status === 'pending').length} 待处理</span>
    {/if}
  </div>

  {#if notices.length === 0}
    <p class="empty">暂无通知——审批与提问会出现在这里</p>
  {:else}
    <div class="list">
      {#each notices as n (n.id)}
        <div class="notice" class:pending={n.status === 'pending'} class:done={n.status === 'done'}>
          <div class="row" role={n.target ? 'button' : undefined} tabindex="0" onclick={() => n.target && onJump?.(n)}>
            <span class="kind-icon">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                {@html kindMeta[n.kind].icon}
              </svg>
            </span>
            <div class="info">
              <span class="title">
                {n.title}
                {#if n.source !== 'agent'}
                  <i class="src">{n.source}</i>
                {/if}
              </span>
              {#if n.detail}
                <span class="detail">{n.detail}</span>
              {/if}
            </div>
            <span class="time">{n.time}</span>
          </div>

          {#if n.status === 'pending'}
            <div class="actions">
              {#if n.kind === 'approve'}
                <button class="btn ok" onclick={() => onResolve?.(n.id, 'approve')}>批准</button>
                <button class="btn no" onclick={() => onResolve?.(n.id, 'reject')}>拒绝</button>
              {:else if n.kind === 'ask'}
                <input
                  type="text"
                  placeholder="输入回答…"
                  bind:value={answers[n.id]}
                  onkeydown={(e) => {
                    if (e.key === 'Enter' && answers[n.id]?.trim()) onResolve?.(n.id, 'answer', answers[n.id].trim())
                  }}
                />
                <button class="btn ok" onclick={() => answers[n.id]?.trim() && onResolve?.(n.id, 'answer', answers[n.id].trim())}>回答</button>
              {:else if n.kind === 'plan'}
                <button class="btn ok" onclick={() => onResolve?.(n.id, 'execute')}>执行</button>
                <button class="btn no" onclick={() => onResolve?.(n.id, 'reject')}>否决</button>
              {/if}
              {#if n.target}
                <button class="jump" onclick={() => onJump?.(n)} title="跳转到对应位置">查看 →</button>
              {/if}
            </div>
          {:else}
            <div class="resolved">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <path d="M5 13l4 4L19 7" />
              </svg>
              {n.resolution || '已处理'}
            </div>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .panel {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
    width: 100%;
    max-height: 100%;
    padding: 12px 14px;
    background: var(--bg);
    border: 1px solid var(--line);
    border-radius: 12px;
    box-shadow: 0 1px 3px rgb(0 0 0 / 4%);
  }
  .head {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  h2 {
    font-size: 12px;
    font-weight: 700;
    color: var(--muted);
  }
  .badge {
    font-size: 10px;
    color: var(--accent);
    background: var(--accent-soft);
    border-radius: 5px;
    padding: 1px 7px;
  }
  .empty {
    font-size: 11px;
    color: var(--faint);
    padding: 4px 0 6px;
  }
  .list {
    display: flex;
    flex-direction: column;
    gap: 8px;
    overflow-y: auto;
    min-height: 0;
  }
  .notice {
    display: flex;
    flex-direction: column;
    gap: 7px;
    padding: 8px 6px;
    border-radius: 8px;
  }
  .notice + .notice {
    border-top: 1px solid var(--line);
    border-radius: 0;
  }
  .notice.pending {
    background: color-mix(in srgb, var(--accent) 5%, var(--bg));
    box-shadow: inset 3px 0 0 var(--accent);
    margin: 0 -6px;
    padding: 8px 8px 8px 11px;
    border-radius: 0 8px 8px 0;
  }
  .notice.pending + .notice {
    border-top: none;
    padding-top: 8px;
  }
  .notice.done {
    opacity: 0.6;
  }
  .row {
    display: flex;
    align-items: flex-start;
    gap: 8px;
  }
  .row[role='button'] {
    cursor: pointer;
  }
  .kind-icon {
    flex: none;
    display: grid;
    place-items: center;
    width: 22px;
    height: 22px;
    border-radius: 6px;
    background: var(--bg-soft);
    color: var(--muted);
  }
  .notice.pending .kind-icon {
    background: var(--bg-invert);
    color: var(--fg-invert);
  }
  .kind-icon svg {
    width: 12px;
    height: 12px;
  }
  .info {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .title {
    font-size: 11.5px;
    font-weight: 550;
    color: var(--fg);
    display: flex;
    align-items: center;
    gap: 5px;
    overflow-wrap: anywhere;
  }
  .src {
    flex: none;
    font-style: normal;
    font-size: 9px;
    color: var(--muted);
    border: 1px solid var(--line);
    border-radius: 4px;
    padding: 0 4px;
  }
  .detail {
    font-size: 10.5px;
    color: var(--faint);
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .time {
    flex: none;
    font-family: var(--font-mono);
    font-size: 9.5px;
    color: var(--faint);
  }
  .actions {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
  }
  .actions input {
    flex: 1;
    min-width: 80px;
    border: 1px solid var(--line);
    border-radius: 7px;
    padding: 4px 8px;
    font-size: 11px;
    outline: none;
    background: var(--bg);
    color: var(--fg);
  }
  .actions input:focus {
    border-color: var(--line-strong);
  }
  .btn {
    border: 1px solid var(--line-strong);
    background: transparent;
    color: var(--fg);
    border-radius: 7px;
    padding: 4px 12px;
    font-size: 11px;
    font-weight: 550;
  }
  .btn.ok {
    background: var(--bg-invert);
    color: var(--fg-invert);
  }
  .btn:hover {
    opacity: 0.85;
  }
  .jump {
    margin-left: auto;
    border: none;
    background: transparent;
    color: var(--accent);
    font-size: 10.5px;
    padding: 4px 2px;
  }
  .resolved {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 10.5px;
    color: var(--faint);
  }
  .resolved svg {
    width: 11px;
    height: 11px;
    color: var(--accent);
  }
</style>
