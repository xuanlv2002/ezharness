<script lang="ts">
  /*
  文本查看器（txt/代码）：纯 textarea 编辑（Ctrl+S 保存）。tab 状态
  数据化（ResourcePane 持有，本组件单实例重绑）。
  */

  import type { ResTab } from '../registry'

  let {
    tab,
    active = false,
    onSave,
  }: {
    tab: ResTab
    active?: boolean
    onSave?: () => void
  } = $props()

  let editorEl = $state<HTMLTextAreaElement | undefined>()

  /* 激活时聚焦编辑器 */
  $effect(() => {
    if (active) requestAnimationFrame(() => editorEl?.focus())
  })

  /* Ctrl+S（preventDefault 挡 WebView2 的"保存网页"默认行为） */
  function onKey(e: KeyboardEvent) {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's' && !e.isComposing) {
      e.preventDefault()
      onSave?.()
    }
  }

  /* 滚动位置导出（磁盘重载后恢复视口） */
  export function scrollTop(): number {
    return editorEl?.scrollTop ?? 0
  }
  export function restoreScroll(st: number) {
    if (editorEl) editorEl.scrollTop = st
  }
</script>

<div class="editor-wrap">
  <textarea
    bind:this={editorEl}
    bind:value={tab.content}
    onkeydown={onKey}
    spellcheck="false"
    placeholder="（空文件）"
  ></textarea>
  {#if tab.loading}<div class="veil">加载中…</div>{/if}
  {#if tab.err}<div class="veil err">{tab.err}</div>{/if}
</div>

<style>
  .editor-wrap {
    position: relative;
    flex: 1;
    min-height: 0;
    border: 1px solid var(--line);
    border-radius: 12px;
    background: #fff;
    overflow: hidden;
  }
  textarea {
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
    position: absolute;
    inset: 0;
    display: grid;
    place-items: center;
    background: var(--bg);
    color: var(--faint);
    font-size: 12.5px;
    z-index: 3;
  }
  .veil.err {
    white-space: pre-wrap;
    padding: 0 20px;
    text-align: center;
    color: #e74c3c;
  }
</style>
