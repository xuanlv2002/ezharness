<script lang="ts">
  import { store } from '../lib/store.svelte'

  function fmtK(n: number): string {
    if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
    if (n >= 1000) return `${(n / 1000).toFixed(1)}k`
    return String(n)
  }

  /* onClose：浮层里用——头部显示关闭按钮 */
  let { onClose }: { onClose?: () => void } = $props()

  const s = $derived(store.status)
  const live = $derived(store.live)
  const ctx = $derived(live?.ctxTokens || s?.contextTokens || 0)
  const win = $derived(live?.ctxWindow || s?.contextWindow || 0)
  const hot = $derived(live?.suggestCompact ?? false)
  const pct = $derived(win > 0 ? Math.min(100, (ctx / win) * 100) : 0)
  const cp = $derived(s?.trimPercent ?? 0)
  const remain = $derived(cp > 0 ? cp - pct : 0)
  const barTitle = $derived(
    win > 0 && cp > 0
      ? remain > 0
        ? `自动整理阈值 ${cp}% · 当前 ${pct.toFixed(0)}% · 还差 ${remain.toFixed(0)}%`
        : `自动整理阈值 ${cp}% · 当前 ${pct.toFixed(0)}%，已达阈值，迭代回边自动整理`
      : '自动整理已禁用（0%）',
  )
  const context = $derived(ctx > 0 ? (win > 0 ? `${fmtK(ctx)}/${fmtK(win)}` : fmtK(ctx)) : '-')
  const hit = $derived(s && s.promptTokens > 0 ? `${(s.cacheHitRate * 100).toFixed(0)}%` : '-')
  const tools = $derived(s?.tools ?? [])
  const mcp = $derived(s?.mcpServers ?? [])
  const skills = $derived(s?.skills ?? [])
  const callbacks = $derived(s?.callbacks ?? [])

  /* 下次运行时间：今天只显示时刻，跨天带月-日 */
  function fmtTime(ts?: number): string {
    if (!ts) return '-'
    const d = new Date(ts)
    const now = new Date()
    const pad = (n: number) => String(n).padStart(2, '0')
    const hm = `${pad(d.getHours())}:${pad(d.getMinutes())}`
    return d.toDateString() === now.toDateString() ? hm : `${d.getMonth() + 1}-${d.getDate()} ${hm}`
  }
</script>

  <aside class="card">
    <div class="head">
      <h2>状态</h2>
      {#if store.liveChanges.length}
        <span class="changes" title={store.liveChanges.join('\n')}>{store.liveChanges.join(' · ')}</span>
      {/if}
      {#if onClose}
        <button class="close" onclick={onClose} title="关闭">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
            <path d="M6 6l12 12M18 6L6 18" />
          </svg>
        </button>
      {/if}
    </div>
  <div class="ctxrow">
    <div class="row">
      <span class="label">当前上下文{hot ? '（推荐压缩）' : ''}</span>
      <span class="value" class:hot>{context}</span>
    </div>
    {#if win > 0}
      <div class="bar" title={barTitle}>
        <div class="fill" class:hot style="width:{pct}%"></div>
        {#if cp > 0}
          <div class="mark" style="left:{cp}%"></div>
        {/if}
      </div>
    {/if}
  </div>
  <div class="row">
    <span class="label">本轮迭代</span>
    <span class="value">{store.currentIter > 0 ? `第 ${store.currentIter} 轮` : '-'}</span>
  </div>
  <div class="row">
    <span class="label">缓存命中率</span>
    <span class="value">{hit}</span>
  </div>
  <div class="caps">
    <span class="label">工具</span>
    {#if tools.length}
      <div class="chips">{#each tools as t (t)}<i class="chip">{t}</i>{/each}</div>
    {:else}<span class="none">-</span>{/if}
  </div>
  <div class="caps">
    <span class="label">MCP</span>
    {#if mcp.length}
      <div class="chips">{#each mcp as m (m)}<i class="chip">{m}</i>{/each}</div>
    {:else}<span class="none">-</span>{/if}
  </div>
  <div class="caps">
    <span class="label">Skill</span>
    {#if skills.length}
      <div class="chips">{#each skills as k (k)}<i class="chip">{k}</i>{/each}</div>
    {:else}<span class="none">-</span>{/if}
  </div>
  <div class="caps">
    <span class="label">定时回调</span>
    {#if callbacks.length}
      <div class="chips">
        {#each callbacks as c (c.id)}
          <i class="chip" class:off={!c.enabled}
            title={`${c.schedule}${c.enabled && c.nextRunAt ? ` · 下次 ${fmtTime(c.nextRunAt)}` : ''}${c.enabled ? '' : ' · 已停用'}`}>
            {c.title}{#if c.enabled && c.nextRunAt} · {fmtTime(c.nextRunAt)}{/if}
          </i>
        {/each}
      </div>
    {:else}<span class="none">-</span>{/if}
  </div>
  </aside>

<style>
  .card {
    display: flex;
    flex-direction: column;
    gap: 8px;
    width: 100%;
    max-height: 100%;
    overflow-y: auto;
    padding: 12px 14px;
    background: var(--glass);
    backdrop-filter: var(--glass-blur);
    border: 1px solid var(--line);
    border-radius: 12px;
    box-shadow: 0 1px 3px rgb(0 0 0 / 4%);
  }
  .head {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 2px;
  }
  h2 {
    font-size: var(--font-size-section);
    font-weight: 600;
    color: var(--fg);
  }
  .changes {
    flex: 1;
    min-width: 0;
    font-size: var(--font-size-small);
    color: #3fb950;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .close {
    flex: none;
    margin-left: auto;
    display: grid;
    place-items: center;
    width: 22px;
    height: 22px;
    border-radius: 6px;
    color: var(--faint);
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .close:hover {
    background: var(--bg-soft);
    color: var(--fg);
  }
  .close svg {
    width: 12px;
    height: 12px;
  }
  .ctxrow {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding-bottom: 4px;
  }
  .row {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    gap: 16px;
  }
  .label {
    font-size: var(--font-size-small);
    color: var(--faint);
    white-space: nowrap;
  }
  .label:has(+ .value.hot) {
    color: #d29922;
  }
  .value {
    font-family: var(--font-mono);
    font-size: var(--font-size-ui);
    color: var(--fg);
    white-space: nowrap;
  }
  .value.hot {
    color: #d29922;
    font-weight: 600;
  }
  .bar {
    position: relative;
    height: 4px;
    border-radius: 2px;
    background: var(--line);
  }
  .fill {
    height: 100%;
    border-radius: 2px;
    background: #2563eb;
    transition: width 0.3s var(--ease-out);
  }
  .fill.hot {
    background: #d29922;
  }
  /* 自动压缩阈值线：上下出头 2px，红线与蓝色水位、琥珀热色区分 */
  .mark {
    position: absolute;
    top: -2px;
    bottom: -2px;
    width: 2px;
    margin-left: -1px;
    border-radius: 1px;
    background: #f85149;
  }
  .caps {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding-top: 2px;
  }
  .none {
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
  }
  .chip {
    font-family: var(--font-mono);
    font-style: normal;
    font-size: var(--font-size-small);
    color: var(--muted);
    background: var(--bg-soft);
    border-radius: 5px;
    padding: 1.5px 6px;
    white-space: nowrap;
  }
  .chip.off {
    opacity: 0.55;
  }
</style>
