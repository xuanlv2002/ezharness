<script lang="ts">
  /*
  文本查看器（txt/代码）：纯 textarea 编辑（Ctrl+S 保存）。tab 状态
  数据化（ResourcePane 持有，本组件单实例重绑）。支持的代码语言
  （见 codehl.langOf，目前 Go）在 textarea 底下垫一层高亮 pre——文字
  本身透明、光标/选择/编辑/滚动全走原生 textarea，输入法组字期间退回
  普通文本，组字结束再叠回高亮。
  */

  import type { ResTab } from '../registry'
  import { highlightCode, langOf } from '../../../lib/codehl'

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
  let hlEl = $state<HTMLPreElement | undefined>()
  let hlHtml = $state('')
  let composing = $state(false)

  /* 高亮语言按文件路径判定（不支持的语言为 null → 保持纯文本） */
  const lang = $derived(langOf(tab.path ?? tab.name))
  const highlighted = $derived(!!lang && !!hlHtml && !composing)

  /* 高亮层跟随内容：同步重扫（体积上限在 codehl 里，超限回退纯文本）。
     不做防抖——防抖窗口里垫层要么缺失要么过期，打字时会闪/吞字符 */
  $effect(() => {
    const text = tab.content
    const l = lang
    hlHtml = l ? (highlightCode(text, l) ?? '') : ''
  })

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

  /* 高亮层没有自己的滚动条，跟随 textarea 的滚动位置 */
  function syncScroll() {
    if (!editorEl || !hlEl) return
    hlEl.scrollTop = editorEl.scrollTop
    hlEl.scrollLeft = editorEl.scrollLeft
  }

  /* 垫层重建（首次高亮/组字结束）后重同步滚动——新节点 scrollTop 从 0 起 */
  $effect(() => {
    hlHtml
    composing
    syncScroll()
  })

  /* 滚动位置导出（磁盘重载后恢复视口） */
  export function scrollTop(): number {
    return editorEl?.scrollTop ?? 0
  }
  export function restoreScroll(st: number) {
    if (editorEl) editorEl.scrollTop = st
  }
</script>

<div class="editor-wrap" class:hl={highlighted}>
  {#if highlighted}
    <!-- 高亮垫层：与 textarea 同字体/同内边距/同换行规则，滚动手动同步 -->
    <pre class="hlayer" bind:this={hlEl} aria-hidden="true"><code>{@html hlHtml}</code></pre>
  {/if}
  <textarea
    bind:this={editorEl}
    bind:value={tab.content}
    onkeydown={onKey}
    onscroll={syncScroll}
    oncompositionstart={() => (composing = true)}
    oncompositionend={() => (composing = false)}
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
    background: var(--card);
    overflow: hidden;
  }
  textarea {
    position: relative;
    z-index: 1;
    display: block;
    width: 100%;
    height: 100%;
    padding: 10px 12px;
    font-family: var(--font-mono);
    font-size: var(--font-size-section);
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
  /* 高亮态：文字由垫层显示，textarea 只保留光标与选择 */
  .editor-wrap.hl textarea {
    color: transparent;
    caret-color: var(--fg);
  }
  .editor-wrap.hl textarea::placeholder {
    color: var(--faint);
  }
  /* 垫层与 textarea 的排版参数必须逐项一致，否则换行位置会错开 */
  .hlayer {
    position: absolute;
    inset: 0;
    z-index: 0;
    margin: 0;
    padding: 10px 12px;
    font-family: var(--font-mono);
    font-size: var(--font-size-section);
    line-height: 1.6;
    tab-size: 4;
    white-space: pre-wrap;
    overflow-wrap: break-word;
    scrollbar-gutter: stable;
    box-sizing: border-box;
    overflow: hidden;
    pointer-events: none;
    color: var(--hl-text);
  }
  .hlayer code {
    font: inherit;
  }
  /* 代码配色随主题翻转（app.css 的 --hl-*：浅色 GitHub 浅色系，暗色暗色系） */
  .hlayer :global(.hljs-comment),
  .hlayer :global(.hljs-quote) {
    color: var(--hl-comment);
  }
  .hlayer :global(.hljs-keyword),
  .hlayer :global(.hljs-selector-tag),
  .hlayer :global(.hljs-doctag),
  .hlayer :global(.hljs-section),
  .hlayer :global(.hljs-name),
  .hlayer :global(.hljs-strong) {
    color: var(--hl-keyword);
  }
  .hlayer :global(.hljs-string),
  .hlayer :global(.hljs-regexp),
  .hlayer :global(.hljs-addition),
  .hlayer :global(.hljs-attribute),
  .hlayer :global(.hljs-meta .hljs-string) {
    color: var(--hl-string);
  }
  .hlayer :global(.hljs-number),
  .hlayer :global(.hljs-symbol),
  .hlayer :global(.hljs-bullet),
  .hlayer :global(.hljs-variable),
  .hlayer :global(.hljs-template-variable),
  .hlayer :global(.hljs-link),
  .hlayer :global(.hljs-selector-attr),
  .hlayer :global(.hljs-selector-pseudo),
  .hlayer :global(.hljs-literal) {
    color: var(--hl-constant);
  }
  .hlayer :global(.hljs-title),
  .hlayer :global(.hljs-title.class_),
  .hlayer :global(.hljs-title.function_),
  .hlayer :global(.hljs-meta) {
    color: var(--hl-title);
  }
  .hlayer :global(.hljs-built_in),
  .hlayer :global(.hljs-type) {
    color: var(--hl-builtin);
  }
  .hlayer :global(.hljs-emphasis) {
    font-style: italic;
  }
  .veil {
    position: absolute;
    inset: 0;
    display: grid;
    place-items: center;
    background: var(--bg);
    color: var(--faint);
    font-size: var(--font-size-section);
    z-index: 3;
  }
  .veil.err {
    white-space: pre-wrap;
    padding: 0 20px;
    text-align: center;
    color: var(--danger);
  }
</style>
