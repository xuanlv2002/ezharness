<script lang="ts">
  import { diffLines, type MemoryChangeView } from '../lib/toolface'
  import { store } from '../lib/store.svelte'

  let {
    change,
    state,
    err = '',
  }: { change: MemoryChangeView; state: 'building' | 'running' | 'done'; err?: string } = $props()

  const targetLabel = $derived(change.target === 'soul' ? '灵魂' : '用户')
  const actionLabel = $derived(
    change.action === 'add'
      ? '新增记忆'
      : change.action === 'update'
        ? '更新记忆'
        : change.action === 'remove'
          ? '删除记忆'
          : change.action === 'project-save'
            ? change.created
              ? '新建项目记忆'
              : '更新项目记忆'
            : '删除项目记忆',
  )
  const meta = $derived(
    change.action.startsWith('project')
      ? [change.name, change.id].filter(Boolean).join(' · ')
      : [targetLabel, change.id].filter(Boolean).join(' · '),
  )
  /* 与通知栏同一份数据：后端待确认区里还有这条 op → 可确认；已消失 → 已提交/已知结果 */
  const pendingNotice = $derived(
    change.pendingId
      ? (store.notices.find((n) => n.kind === 'memory' && n.id === change.pendingId && n.status === 'pending') ?? null)
      : null,
  )
  const decision = $derived(change.pendingId ? (store.memoryDecisions[change.pendingId] ?? '') : '')
  const statusLabel = $derived(
    err
      ? '失败'
      : state !== 'done'
        ? '提交中'
        : pendingNotice
          ? '待确认'
          : change.status === 'pending'
            ? decision === 'accept'
              ? '已确认'
              : decision === 'cancel'
                ? '已取消'
                : '已提交'
            : '',
  )

  /* add/update/project-save 以新内容为准；remove/project-remove 以旧内容为准 */
  const body = $derived.by(() => {
    switch (change.action) {
      case 'add':
        return { old: '', next: change.after || change.text }
      case 'update':
        return { old: change.before, next: change.after || change.text }
      case 'remove':
        return { old: change.before || change.text, next: '' }
      case 'project-save':
        return { old: change.before, next: change.after || change.text }
      case 'project-remove':
        return { old: [change.name, change.description].filter(Boolean).join('\n'), next: '' }
    }
    return { old: '', next: '' }
  })
  const lines = $derived(diffLines(body.old, body.next))
</script>

<div class="memory">
  <div class="head">
    <span class="action">{actionLabel}</span>
    {#if meta}<span class="meta">{meta}</span>{/if}
    {#if statusLabel}
      <span class="tag" class:pending={statusLabel === '待确认'} class:failed={!!err}>{statusLabel}</span>
    {/if}
  </div>
  {#if change.action === 'project-save' && change.description}
    <p class="desc">{change.description}</p>
  {/if}
  {#if err}
    <pre class="err">{err}</pre>
  {:else}
    <pre class="diff">{#each lines as l, i}{#if l.t === '-'}<span class="del">{l.s || ' '}</span>{:else if l.t === '+'}<span class="add">{l.s || ' '}</span>{:else}<span class="ctx">{l.s || ' '}</span>{/if}{i < lines.length - 1 ? '\n' : ''}{/each}</pre>
  {/if}
  {#if !err && pendingNotice}
    {@const notice = pendingNotice}
    <div class="actions">
      <button class="btn ok" onclick={() => void store.resolveNoticeGlobal(notice, 'accept')}>确认</button>
      <button class="btn" onclick={() => void store.resolveNoticeGlobal(notice, 'cancel')}>取消</button>
      <span class="hint">与通知栏同一份待确认</span>
    </div>
  {:else if !err && change.status === 'pending' && !change.pendingId}
    <p class="hint">已提交，请在通知栏确认</p>
  {/if}
</div>

<style>
  .memory {
    border-top: 1px solid var(--line);
    padding: 8px 12px 9px;
    background: color-mix(in srgb, var(--accent) 4%, transparent);
  }
  .head {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }
  .action {
    flex: none;
    font-size: var(--font-size-ui);
    font-weight: 600;
    color: var(--fg);
  }
  .meta {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .tag {
    flex: none;
    margin-left: auto;
    font-size: var(--font-size-small);
    color: var(--muted);
    border: 1px solid var(--line);
    border-radius: 999px;
    padding: 1px 7px;
  }
  .tag.pending {
    color: #d29922;
    border-color: color-mix(in srgb, #d29922 35%, transparent);
  }
  .tag.failed {
    color: #f85149;
    border-color: color-mix(in srgb, #f85149 35%, transparent);
  }
  .desc {
    margin: 5px 0 0;
    font-size: var(--font-size-ui);
    color: var(--muted);
    word-break: break-word;
  }
  pre {
    margin: 6px 0 0;
    padding: 6px 8px;
    border-radius: 6px;
    background: var(--bg);
    font-family: var(--font-mono);
    font-size: var(--font-size-ui);
    line-height: 1.55;
    white-space: pre-wrap;
    word-break: break-all;
    max-height: 260px;
    overflow-y: auto;
  }
  .del {
    display: block;
    background: color-mix(in srgb, #f85149 12%, transparent);
    color: #f85149;
  }
  .add {
    display: block;
    background: color-mix(in srgb, #3fb950 12%, transparent);
    color: #3fb950;
  }
  .ctx {
    display: block;
    color: var(--muted);
  }
  .err {
    color: #f85149;
  }
  .actions {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-top: 7px;
  }
  .btn {
    border: 1px solid var(--line-strong);
    background: transparent;
    color: var(--fg);
    border-radius: 7px;
    padding: 3px 12px;
    font-size: var(--font-size-small);
    font-weight: 500;
  }
  .btn.ok {
    background: var(--bg-invert);
    color: var(--fg-invert);
  }
  .btn:hover {
    opacity: 0.85;
  }
  .hint {
    margin: 4px 0 0;
    font-size: var(--font-size-small);
    color: var(--faint);
    text-align: right;
  }
  .actions .hint {
    margin: 0 0 0 auto;
  }
</style>
