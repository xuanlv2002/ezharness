<script lang="ts">
  /* HTML 查看器：原文编辑 / 内嵌浏览器预览两态（预览是真浏览器，不做路径改写） */
  import type { ResTab } from '../registry'
  import TextViewer from './TextViewer.svelte'
  import HtmlPreview from './HtmlPreview.svelte'

  let {
    tab,
    active = false,
    flush = false,
    animating = false,
    blocked = false,
    onSave,
  }: {
    tab: ResTab
    active?: boolean
    flush?: boolean
    animating?: boolean
    blocked?: boolean
    onSave?: () => void
  } = $props()

  let textViewer = $state<TextViewer>()
  const preview = $derived(tab.htmlPreview === true)

  function setPreview(v: boolean): void {
    tab.htmlPreview = v
  }

  export function find(q: string, i: number): { count: number; index: number } {
    return textViewer?.find(q, i) ?? { count: 0, index: 0 }
  }

  export function clearFind(): void {
    textViewer?.clearFind()
  }

  export function scrollTop(): number {
    return textViewer?.scrollTop() ?? 0
  }

  export function restoreScroll(st: number): void {
    textViewer?.restoreScroll(st)
  }
</script>

<div class="viewer" class:flush>
  <div class="bar">
    <div class="seg">
      <button class:active={!preview} onclick={() => setPreview(false)}>原文</button>
      <button class:active={preview} onclick={() => setPreview(true)}>预览</button>
    </div>
    {#if !preview}<span class="hint">编辑后 Ctrl+S 保存</span>{/if}
  </div>
  {#if tab.err}
    <div class="veil err">{tab.err}</div>
  {:else if tab.loading}
    <div class="veil">加载中…</div>
  {:else if preview}
    <HtmlPreview {tab} {active} {animating} {blocked} {flush} {onSave} />
  {:else}
    <TextViewer bind:this={textViewer} {tab} {active} {onSave} />
  {/if}
</div>

<style>
  .viewer {
    display: flex;
    flex-direction: column;
    gap: 8px;
    height: 100%;
    min-height: 0;
  }
  .bar {
    flex: none;
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .seg {
    display: flex;
    align-items: center;
    gap: 2px;
    width: fit-content;
    padding: 2px;
    border: 1px solid var(--line);
    border-radius: 8px;
    background: var(--bg-soft);
  }
  .seg button {
    border: none;
    background: transparent;
    color: var(--muted);
    border-radius: 6px;
    padding: 2px 10px;
    font-size: var(--font-size-ui);
    white-space: nowrap;
    cursor: pointer;
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .seg button.active {
    background: var(--bg);
    color: var(--fg);
    font-weight: 600;
    box-shadow: 0 1px 2px rgb(0 0 0 / 8%);
  }
  .hint {
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .viewer.flush {
    gap: 0;
  }
  .viewer.flush .bar {
    padding: 4px 8px;
  }
  .viewer.flush :global(.editor-wrap) {
    border: none;
    border-radius: 0;
  }
  .veil {
    flex: 1;
    display: grid;
    place-items: center;
    border: 1px solid var(--line);
    border-radius: 12px;
    background: var(--bg);
    color: var(--faint);
    font-size: var(--font-size-section);
  }
  .veil.err {
    color: var(--danger);
    padding: 0 20px;
    text-align: center;
  }
  .viewer.flush .veil {
    border: none;
    border-radius: 0;
  }
</style>
