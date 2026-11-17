<script lang="ts">
  /* 时间线的懒加载边界条：继续加载 / 上下文压缩 / 上一会话共用同一套外观，
     区别只在描述与是否有摘要可展开。 */
  let {
    kind,
    title = '',
    brief = '',
    full = '',
    loadable = false,
    loading = false,
    onLoad,
  }: {
    kind: 'more' | 'trim' | 'archive'
    title?: string
    brief?: string
    full?: string
    loadable?: boolean
    loading?: boolean
    onLoad?: () => void
  } = $props()

  let open = $state(false)

  const label = $derived(kind === 'more' ? '' : kind === 'trim' ? '上下文压缩' : '上一会话')
  const arrow = $derived(full ? (open ? '▾' : '▸') : loading ? '⟳' : loadable ? '↑' : '')

  /* 摘要按【小节】切块（旧格式没有小节标题时整段一条） */
  function trimSections(text: string): { title: string; lines: string[] }[] {
    const out: { title: string; lines: string[] }[] = []
    for (const raw of text.split(/\r?\n/)) {
      const line = raw.trim()
      if (!line) continue
      const m = line.match(/^【(.+?)】\s*(.*)$/)
      if (m) {
        out.push({ title: m[1], lines: m[2] ? [m[2].replace(/^[-•]\s*/, '')] : [] })
      } else if (out.length) {
        out[out.length - 1].lines.push(line.replace(/^[-•]\s*/, ''))
      } else {
        out.push({ title: '', lines: [line.replace(/^[-•]\s*/, '')] })
      }
    }
    return out
  }

  function onBar() {
    if (full) open = !open
    else if (loadable) onLoad?.()
  }
</script>

<div class="fold" class:more={kind === 'more'}>
  <button type="button" class="bar" onclick={onBar}
    title={full ? (open ? '收起摘要' : '展开摘要') : loadable ? '加载更早的内容' : undefined}>
    <span class="arrow" class:open>{arrow}</span>
    <svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
      {#if kind === 'trim'}
        <path d="M4 6h16M4 12h16M4 18h10" />
      {:else if kind === 'archive'}
        <path d="M4 7h16v13H4zM3 4h18v3H3zM9 12h6" />
      {:else}
        <path d="M12 19V5M5 12l7-7 7 7" />
      {/if}
    </svg>
    {#if kind === 'more'}
      <span class="only">{loading ? '加载更早的对话…' : '继续加载更早的对话'}</span>
    {:else}
      <span class="label">{label}</span>
      {#if title}<span class="ttl">{title}</span>{/if}
      {#if brief && brief !== title}<span class="sum">{brief}</span>{/if}
      {#if loadable}<span class="hint">{loading ? '加载中…' : '向上滚动加载'}</span>{/if}
    {/if}
  </button>
  {#if open && full}
    <div class="body">
      {#each trimSections(full) as sec}
        {#if sec.title}<p class="sec">{sec.title}</p>{/if}
        {#each sec.lines as line}<p class="line">{line}</p>{/each}
      {/each}
    </div>
  {/if}
</div>

<style>
  /* 边界条统一外观：虚线卡片条；只有描述与摘要内容不同 */
  .fold {
    border: 1px dashed var(--line-strong);
    border-radius: 10px;
    background: var(--bg-soft);
    overflow: hidden;
    animation: float-in var(--dur-fast) var(--ease-out) both;
  }
  .bar {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 6px 12px;
    font: inherit;
    font-size: var(--font-size-ui);
    color: var(--faint);
    text-align: left;
  }
  .fold.more .bar {
    justify-content: center;
  }
  .bar:hover {
    background: color-mix(in srgb, var(--fg) 4%, transparent);
  }
  .bar:focus {
    outline: none;
  }
  .bar:focus-visible {
    outline: 2px solid color-mix(in srgb, var(--accent) 35%, transparent);
    outline-offset: -2px;
  }
  .arrow {
    flex: none;
    min-width: 12px;
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .arrow.open {
    color: var(--muted);
  }
  .icon {
    flex: none;
    width: 14px;
    height: 14px;
    color: var(--muted);
  }
  .only {
    color: var(--muted);
  }
  .label {
    flex: none;
    color: var(--muted);
    font-size: var(--font-size-small);
    white-space: nowrap;
  }
  .ttl {
    flex: 0 1 auto;
    min-width: 0;
    max-width: 40%;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    color: var(--muted);
    font-size: var(--font-size-small);
  }
  .sum {
    flex: 1;
    min-width: 0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .hint {
    flex: none;
    color: var(--faint);
    font-size: var(--font-size-small);
    white-space: nowrap;
  }
  /* 展开区：按【小节】排的摘要正文，超高内滚，不把时间线撑出屏 */
  .body {
    border-top: 1px solid var(--line);
    max-height: 360px;
    overflow-y: auto;
    padding: 8px 12px 10px;
  }
  .sec {
    margin: 10px 0 2px;
    font-size: var(--font-size-small);
    font-weight: 600;
    color: var(--fg);
  }
  .sec:first-child {
    margin-top: 0;
  }
  .line {
    position: relative;
    margin: 0;
    padding-left: 12px;
    font-size: var(--font-size-ui);
    line-height: 1.65;
    color: var(--muted);
    overflow-wrap: anywhere;
  }
  .line::before {
    content: '';
    position: absolute;
    left: 3px;
    top: 0.68em;
    width: 3px;
    height: 3px;
    border-radius: 50%;
    background: var(--line-strong);
  }
</style>
