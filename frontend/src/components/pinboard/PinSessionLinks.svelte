<script lang="ts">
  /*
  便签卡 / 归档卡右下角的会话链接（纯文本 + 小图标，不是按钮）：
    · showNew 时第一条是「新会话」：点击跳对话页，发出消息后才关联
    · 只直接列最近 MAX_VISIBLE 个关联会话，其余收进「更多会话」浮层——
      卡片高度不随关联数增长（配合 showNew 最多三行）
  点击只做跳转；阻止 pointerdown 冒泡，避免触发卡片的拖动/排序。
  */
  import type { PinSessionRef } from '../../lib/api'
  import { store } from '../../lib/store.svelte'

  const MAX_VISIBLE = 1
  const MENU_W = 220

  let {
    sessions = [],
    showNew = false,
    onnew,
    onopen,
  }: {
    sessions?: PinSessionRef[]
    showNew?: boolean
    onnew?: () => void
    onopen?: (id: string) => void
  } = $props()

  /* 最近的一个直接显示；更早的收进浮层 */
  const visible = $derived(sessions.slice(-MAX_VISIBLE))
  const hiddenCount = $derived(Math.max(0, sessions.length - visible.length))

  let open = $state(false)
  let triggerEl = $state<HTMLElement>()
  let pos = $state({ left: 0, top: 0, maxHeight: 260 })

  /* 标题优先取分支列表的实时标题（首条消息生成），未刷新时回退缓存 */
  function titleOf(s: PinSessionRef): string {
    return store.branches.find((b) => b.id === s.id)?.title || s.title || '会话'
  }

  /* 会话创建时间：优先分支列表的实时值，未刷新时用关联时缓存 */
  function timeOf(s: PinSessionRef): string {
    const ts = store.branches.find((b) => b.id === s.id)?.createdAt || s.createdAt || 0
    return ts > 0
      ? new Date(ts).toLocaleString('zh-CN', { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' })
      : ''
  }

  function portal(node: HTMLElement) {
    document.body.appendChild(node)
    return { destroy: () => node.remove() }
  }

  /* place 浮层贴在「还有 N 个」触发钮旁，右侧对齐；下方放不下就向上弹 */
  function place() {
    if (!triggerEl) return
    const r = triggerEl.getBoundingClientRect()
    const maxHeight = Math.min(280, Math.max(80, sessions.length * 34 + 8))
    const left = Math.max(8, Math.min(r.right - MENU_W, window.innerWidth - MENU_W - 8))
    let top = r.bottom + 4
    if (top + maxHeight > window.innerHeight - 8) top = Math.max(8, r.top - maxHeight - 4)
    pos = { left, top, maxHeight }
  }

  function toggle() {
    open = !open
    if (open) place()
  }

  function pick(id: string) {
    open = false
    onopen?.(id)
  }

  $effect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') open = false
    }
    window.addEventListener('keydown', onKey)
    window.addEventListener('resize', place)
    return () => {
      window.removeEventListener('keydown', onKey)
      window.removeEventListener('resize', place)
    }
  })
</script>

<div class="links nodrag" role="group" aria-label="关联会话" onpointerdown={(e) => e.stopPropagation()}>
  {#if showNew}
    <button
      class="link new"
      onclick={(e) => { e.stopPropagation(); onnew?.() }}
      title="新建会话并跳转；在那边发出第一条消息后才会关联到这张便签"
    >
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
        <path d="M12 5v14M5 12h14" />
      </svg>
      <span>新会话</span>
    </button>
  {/if}
  {#each visible as s (s.id)}
    <button
      class="link ses"
      onclick={(e) => { e.stopPropagation(); onopen?.(s.id) }}
      title={`打开会话：${titleOf(s)}`}
    >
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <path d="M7 17L17 7M9 7h8v8" />
      </svg>
      <span class="title">{titleOf(s)}</span>
      {#if timeOf(s)}<span class="time">{timeOf(s)}</span>{/if}
    </button>
  {/each}
  {#if hiddenCount > 0}
    <button
      class="link more"
      bind:this={triggerEl}
      onclick={(e) => { e.stopPropagation(); toggle() }}
      title="查看全部关联会话"
    >
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
        <circle cx="5" cy="12" r="1" /><circle cx="12" cy="12" r="1" /><circle cx="19" cy="12" r="1" />
      </svg>
      <span>更多会话</span>
    </button>
  {/if}
</div>

{#if open}
  <div class="scrim" use:portal role="presentation" onclick={() => (open = false)}></div>
  <div
    class="menu"
    use:portal
    role="menu"
    aria-label="全部关联会话"
    style:left="{pos.left}px"
    style:top="{pos.top}px"
    style:max-height="{pos.maxHeight}px"
  >
    {#each [...sessions].reverse() as s (s.id)}
      <button class="mitem" role="menuitem" onclick={() => pick(s.id)} title={`打开会话：${titleOf(s)}`}>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M7 17L17 7M9 7h8v8" />
        </svg>
        <span class="mtitle">{titleOf(s)}</span>
        {#if timeOf(s)}<span class="mtime">{timeOf(s)}</span>{/if}
      </button>
    {/each}
  </div>
{/if}

<style>
  .links {
    flex: none;
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 2px;
    min-width: 0;
    max-width: 100%;
    margin-top: auto;
    padding-top: 5px;
  }
  .link {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    max-width: 100%;
    padding: 0;
    border: none;
    background: none;
    color: var(--muted);
    font: inherit;
    font-size: var(--font-size-small);
    line-height: 1.5;
    text-align: right;
    cursor: pointer;
    transition: color var(--dur-fast) var(--ease-out);
  }
  .link:hover {
    color: var(--accent, #2563eb);
  }
  .link svg {
    flex: none;
    width: 10px;
    height: 10px;
    opacity: 0.65;
  }
  .link .title {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .link .time {
    flex: none;
    font-size: 10px;
    color: var(--faint);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .link:hover .title {
    text-decoration: underline;
    text-underline-offset: 2px;
  }
  .link:hover .time {
    text-decoration: none;
  }
  /* 新会话比关联链接更弱：灰字，悬停才浮现 */
  .link.new {
    color: var(--faint);
  }
  .link.new:hover {
    color: var(--fg);
  }
  .link.more {
    color: var(--faint);
  }
  .link.more:hover {
    color: var(--fg);
  }
  /* 全部关联会话浮层（portal 到 body，高于当日展开层 1000） */
  .scrim {
    position: fixed;
    inset: 0;
    z-index: 1100;
  }
  .menu {
    position: fixed;
    z-index: 1101;
    width: 220px;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 1px;
    padding: 4px;
    border: 1px solid var(--line-strong);
    border-radius: 10px;
    background: var(--bg);
    box-shadow: 0 10px 28px rgb(0 0 0 / 18%);
    scrollbar-width: thin;
    scrollbar-color: color-mix(in srgb, var(--fg) 18%, transparent) transparent;
  }
  .mitem {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    padding: 6px 8px;
    border: none;
    border-radius: 7px;
    background: transparent;
    color: var(--muted);
    font: inherit;
    font-size: var(--font-size-ui);
    text-align: left;
    cursor: pointer;
  }
  .mitem:hover {
    color: var(--fg);
    background: var(--bg-soft);
  }
  .mitem svg {
    flex: none;
    width: 11px;
    height: 11px;
    opacity: 0.6;
  }
  .mtitle {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .mtime {
    flex: none;
    font-size: 10px;
    color: var(--faint);
    font-variant-numeric: tabular-nums;
  }
</style>
