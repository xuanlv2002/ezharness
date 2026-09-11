<script lang="ts">
  /*
  Markdown 查看器：预览（marked + DOMPurify，与聊天渲染同栈）/ 原文
  （textarea 编辑，Ctrl+S 保存）切换。tab 状态数据化（content 存
  ResTab，单实例重绑）。
  */

  import { marked } from 'marked'
  import DOMPurify from 'dompurify'
  import type { ResTab } from '../registry'

  let { tab, active = false, onSave }: { tab: ResTab; active?: boolean; onSave?: () => void } = $props()

  marked.setOptions({ breaks: true, gfm: true })

  let preview = $state(true)
  const html = $derived(tab.content ? DOMPurify.sanitize(marked.parse(tab.content) as string) : '')

  function onKey(e: KeyboardEvent) {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's' && !e.isComposing) {
      e.preventDefault()
      onSave?.()
    }
  }
</script>

<div class="viewer">
  <div class="bar">
    <button class:active={preview} onclick={() => (preview = true)}>预览</button>
    <button class:active={!preview} onclick={() => (preview = false)}>原文</button>
    {#if !preview}<span class="hint">编辑后 Ctrl+S 保存</span>{/if}
  </div>
  {#if tab.err}
    <div class="veil err">{tab.err}</div>
  {:else if tab.loading}
    <div class="veil">加载中…</div>
  {:else if preview}
    <div class="md-body">
      {@html html}
    </div>
  {:else}
    <div class="editor-wrap">
      <textarea
        bind:value={tab.content}
        onkeydown={onKey}
        spellcheck="false"
        placeholder="（空文件）"
      ></textarea>
    </div>
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
    gap: 6px;
  }
  .bar button {
    border: 1px solid var(--line);
    background: var(--bg);
    color: var(--muted);
    border-radius: 8px;
    padding: 3px 12px;
    font-size: 11.5px;
    cursor: pointer;
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out),
      border-color var(--dur-fast) var(--ease-out);
  }
  .bar button.active {
    border-color: var(--accent-soft, var(--accent));
    color: var(--accent, #2563eb);
    background: var(--bg-soft);
    font-weight: 600;
  }
  .hint {
    font-size: 11px;
    color: var(--faint);
  }
  .md-body,
  .editor-wrap {
    flex: 1;
    min-height: 0;
    border: 1px solid var(--line);
    border-radius: 12px;
    background: #fff;
    overflow: auto;
    padding: 12px 16px;
    font-size: 13.5px;
    line-height: 1.7;
  }
  .md-body :global(pre) {
    background: var(--bg-soft);
    border-radius: 8px;
    padding: 10px 12px;
    overflow: auto;
    font-family: var(--font-mono);
    font-size: 12.5px;
  }
  .md-body :global(code) {
    font-family: var(--font-mono);
    font-size: 12.5px;
    background: var(--bg-soft);
    border-radius: 4px;
    padding: 0 4px;
  }
  .md-body :global(pre code) {
    background: transparent;
    padding: 0;
  }
  .md-body :global(table) {
    border-collapse: collapse;
  }
  .md-body :global(th),
  .md-body :global(td) {
    border: 1px solid var(--line);
    padding: 4px 10px;
  }
  .md-body :global(img) {
    max-width: 100%;
  }
  .md-body :global(a) {
    color: var(--accent, #2563eb);
  }
  .md-body :global(blockquote) {
    border-left: 3px solid var(--line-strong);
    margin: 0;
    padding-left: 12px;
    color: var(--muted);
  }
  .editor-wrap {
    padding: 0;
    overflow: hidden;
  }
  .editor-wrap textarea {
    display: block;
    width: 100%;
    height: 100%;
    padding: 10px 12px;
    font-family: var(--font-mono);
    font-size: 12.5px;
    line-height: 1.6;
    tab-size: 4;
    white-space: pre-wrap;
    overflow-wrap: break-word;
    scrollbar-gutter: stable;
    box-sizing: border-box;
    border: none;
    outline: none;
    resize: none;
    background: transparent;
    color: var(--fg);
  }
  .veil {
    flex: 1;
    display: grid;
    place-items: center;
    border: 1px solid var(--line);
    border-radius: 12px;
    background: var(--bg);
    color: var(--faint);
    font-size: 12.5px;
  }
  .veil.err {
    color: #e74c3c;
    padding: 0 20px;
    text-align: center;
  }
</style>
