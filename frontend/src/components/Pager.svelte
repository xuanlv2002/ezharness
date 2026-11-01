<script lang="ts">
  /* 列表分页条：范围提示 + 上一页/页码/下一页。页码越界由父组件钳制。 */
  let {
    page = 1,
    total = 0,
    pageSize = 8,
    onchange,
  }: {
    page?: number
    total?: number
    pageSize?: number
    onchange?: (page: number) => void
  } = $props()

  const pages = $derived(Math.max(1, Math.ceil(total / pageSize)))
  const from = $derived(total ? (page - 1) * pageSize + 1 : 0)
  const to = $derived(Math.min(page * pageSize, total))

  function go(p: number) {
    if (p < 1 || p > pages || p === page) return
    onchange?.(p)
  }
</script>

<div class="pager">
  <span class="meta">{total ? `${from}–${to} / ${total}` : '0 条'}</span>
  <div class="btns">
    <button type="button" disabled={page <= 1} onclick={() => go(page - 1)} aria-label="上一页" title="上一页">‹</button>
    <span class="num">{page} / {pages}</span>
    <button type="button" disabled={page >= pages} onclick={() => go(page + 1)} aria-label="下一页" title="下一页">›</button>
  </div>
</div>

<style>
  .pager {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 10px;
    padding-top: 2px;
  }
  .meta {
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .btns {
    display: inline-flex;
    align-items: center;
    gap: 2px;
  }
  .btns button {
    display: grid;
    place-items: center;
    width: 24px;
    height: 24px;
    border: 1px solid var(--line);
    border-radius: 7px;
    background: transparent;
    color: var(--muted);
    font: inherit;
    font-size: 13px;
    line-height: 1;
  }
  .btns button:hover:not(:disabled) {
    border-color: var(--line-strong);
    color: var(--fg);
  }
  .btns button:disabled {
    opacity: 0.35;
    cursor: default;
  }
  .num {
    min-width: 46px;
    text-align: center;
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    color: var(--muted);
  }
</style>
