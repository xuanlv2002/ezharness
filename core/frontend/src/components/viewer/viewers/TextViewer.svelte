<script lang="ts">
  /*
  文本查看器（txt/代码）：textarea 编辑 + 透明高亮垫层（组字期间退回纯文本）；
  左侧行号槽，文本层不折行以对齐行号。tab 状态数据化（ResourcePane 持有）。
  */

  import type { ResTab } from '../registry'
  import { highlightCode, langOf } from '../../../lib/codehl'
  import { matchRanges } from '../../../lib/findtext'
  import FindLayer from './FindLayer.svelte'

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
  let gutterEl = $state<HTMLDivElement | undefined>()
  let composing = $state(false)

  /* 行号槽：一行一号。行号按固定行高对齐，因此文本层不折行——软折行
     会让同一个逻辑行占多行，行号与正文错位。 */
  const totalLines = $derived.by(() => {
    const text = tab.content // 每次按键都会重算：只数换行，不 split 出整行数组
    let n = 1
    for (let i = 0; i < text.length; i++) if (text.charCodeAt(i) === 10) n++
    return n
  })
  const gutterText = $derived.by(() => {
    const out = new Array<string>(totalLines)
    for (let i = 0; i < totalLines; i++) out[i] = String(i + 1)
    return out.join('\n')
  })
  const gutterPx = $derived(Math.max(2, String(totalLines).length) * 8 + 10)

  /* 高亮语言按文件路径判定（不支持的语言为 null → 纯文本） */
  const lang = $derived(langOf(tab.path ?? tab.name))

  /* 高亮结果放 derived：切 tab / 输入时与正文本体在同一次渲染里更新。
     若放 effect 里延后一拍，切文件瞬间会先看到上一个文件的高亮层
     （textarea 文字透明），表现为选中/切换时闪一下旧内容。
     体积上限在 codehl 里，超限返回 null 回退纯文本 */
  const hlHtml = $derived(lang ? (highlightCode(tab.content, lang) ?? '') : '')
  const highlighted = $derived(!!hlHtml && !composing)

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

  /* 高亮层/行号槽没有自己的滚动条：跟随 textarea 的滚动位置 */
  function syncScroll() {
    if (!editorEl) return
    if (hlEl) {
      hlEl.scrollTop = editorEl.scrollTop
      hlEl.scrollLeft = editorEl.scrollLeft
    }
    if (gutterEl) gutterEl.scrollTop = editorEl.scrollTop
  }

  /* 垫层重建（首次高亮/组字结束）后重同步滚动——新节点 scrollTop 从 0 起 */
  $effect(() => {
    hlHtml
    composing
    syncScroll()
  })

  /* ── 文件内查找（ResourcePane 调 Ctrl+F 时驱动本组件）──
     检索永远针对整份内容（不是可见区），命中画在查找垫层里 */
  let findQuery = $state('')
  let findIndex = $state(0)
  let findActive = $state(false)
  let boundKey = ''

  /* find 返回总命中数与实际定位序号（index 越界自动绕回） */
  export function find(q: string, i: number): { count: number; index: number } {
    findQuery = q
    findActive = !!q.trim()
    const count = matchRanges(tab.content, q).length
    const index = count ? ((i % count) + count) % count : 0
    findIndex = index
    return { count, index }
  }

  export function clearFind(): void {
    findActive = false
    findQuery = ''
    findIndex = 0
  }

  /* 单实例重绑到新 tab：旧文件的查找状态/命中不能跟过来 */
  $effect(() => {
    if (tab.key === boundKey) return
    boundKey = tab.key
    clearFind()
  })

  /* 滚动位置导出（磁盘重载后恢复视口） */
  export function scrollTop(): number {
    return editorEl?.scrollTop ?? 0
  }
  export function restoreScroll(st: number) {
    if (!editorEl) return
    editorEl.scrollTop = st
    syncScroll()
  }

  /* gotoLine 定位到指定行区间（ResourcePane 打开 #L 锚点时调用）：
     选中目标行并显式滚动——不依赖 focus/选区让浏览器自己滚，抽屉刚
     展开时它不一定滚；行高固定，按行号直接算位置最稳。 */
  export function gotoLine(from: number, to = from) {
    if (!editorEl) return
    /* textarea 的 value 会把 CRLF 归一成 LF，选区偏移必须按归一后的文本算 */
    const lines = tab.content.replace(/\r\n?/g, '\n').split('\n')
    const startLine = Math.min(Math.max(from, 1), lines.length)
    const endLine = Math.min(Math.max(to, startLine), lines.length)
    let start = 0
    for (let i = 0; i < startLine - 1; i++) start += lines[i].length + 1
    let end = start
    for (let i = startLine - 1; i < endLine; i++) end += lines[i].length + 1
    const style = getComputedStyle(editorEl)
    const lh = parseFloat(style.lineHeight) || 24
    const padTop = parseFloat(style.paddingTop) || 0
    editorEl.focus()
    editorEl.setSelectionRange(start, Math.max(start, end - 1))
    editorEl.scrollTop = Math.max(0, padTop + (startLine - 1) * lh - editorEl.clientHeight / 3)
    editorEl.scrollLeft = 0
    syncScroll()
  }
</script>

<div class="editor-wrap" class:hl={highlighted} style="--ed-gutter: {gutterPx}px">
  {#if highlighted}
    <!-- 高亮垫层：与 textarea 同字体/同内边距/同行高，滚动手动同步 -->
    <pre class="hlayer" bind:this={hlEl} aria-hidden="true"><code>{@html hlHtml}</code></pre>
  {/if}
  <!-- 行号槽：固定宽度、跟随纵向滚动；pointer-events 关闭后不影响编辑 -->
  <div class="gutter" bind:this={gutterEl} aria-hidden="true"><pre>{gutterText}</pre></div>
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
  {#if findActive}
    <FindLayer text={tab.content} query={findQuery} current={findIndex} scrollEl={editorEl} wrap={false} />
  {/if}
  {#if tab.loading}<div class="veil">加载中…</div>{/if}
  {#if tab.err}<div class="veil err">{tab.err}</div>{/if}
</div>

<style>
  .editor-wrap {
    --ed-lh: 24px;
    --ed-pady: 14px;
    --ed-padx: 16px;
    /* 正文起点：左边距 + 行号槽 + 槽间距；textarea 与各垫层共用 */
    --ed-pl: calc(var(--ed-padx) + var(--ed-gutter, 0px) + 8px);
    position: relative;
    flex: 1;
    min-height: 0;
    border: 1px solid var(--line);
    border-radius: 12px;
    background: var(--card);
    overflow: hidden;
  }
  /* 聚焦时边框提亮：编辑态有明确的主场感 */
  .editor-wrap:focus-within {
    border-color: var(--accent-soft, var(--accent));
  }
  textarea {
    position: absolute;
    inset: 0;
    z-index: 1;
    display: block;
    width: 100%;
    height: 100%;
    padding: var(--ed-pady) var(--ed-padx) var(--ed-pady) var(--ed-pl);
    box-sizing: border-box;
    border: none;
    outline: none;
    resize: none;
    background: transparent;
    color: var(--fg);
    caret-color: var(--fg);
    font-family: var(--font-mono);
    font-size: 14px;
    line-height: var(--ed-lh);
    tab-size: 4;
    /* 行号按固定行高逐行对齐；软折行会让一个逻辑行占多行，故不折行 */
    white-space: pre;
    overflow-wrap: normal;
    overflow-x: auto;
    overflow-y: auto;
    scrollbar-gutter: stable;
  }
  /* 高亮态：文字由垫层显示，textarea 只保留光标与选择 */
  .editor-wrap.hl textarea {
    color: transparent;
  }
  .editor-wrap.hl textarea::placeholder {
    color: var(--faint);
  }
  textarea::selection {
    background: color-mix(in srgb, var(--accent) 24%, transparent);
  }
  /* 行号槽：与 textarea 同 padding-top/行高，纵向跟随滚动 */
  .gutter {
    position: absolute;
    left: var(--ed-padx);
    top: 0;
    bottom: 0;
    width: var(--ed-gutter, 0px);
    padding: var(--ed-pady) 6px var(--ed-pady) 0;
    box-sizing: border-box;
    overflow: hidden;
    z-index: 2;
    background: var(--card); /* 横向滚动时盖住滑到行号下面的正文 */
    pointer-events: none;
  }
  .gutter pre {
    margin: 0;
    font-family: var(--font-mono);
    font-size: 12px;
    line-height: var(--ed-lh);
    text-align: right;
    color: var(--faint);
  }
  /* 垫层与 textarea 的排版参数必须逐项一致，否则行位会错开 */
  .hlayer {
    position: absolute;
    inset: 0;
    z-index: 0;
    margin: 0;
    padding: var(--ed-pady) var(--ed-padx) var(--ed-pady) var(--ed-pl);
    box-sizing: border-box;
    font-family: var(--font-mono);
    font-size: 14px;
    line-height: var(--ed-lh);
    tab-size: 4;
    white-space: pre;
    overflow-wrap: normal;
    overflow: hidden;
    scrollbar-gutter: stable;
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
