<script lang="ts">
  /*
  Markdown 查看器：预览（marked + DOMPurify，与聊天渲染同栈）/ 原文编辑（Ctrl+S 保存）切换。
  预览里的相对资源按文件所在目录改写到工作目录文件服务直出，本地文件链接在资源页打开。
  */

  import { tick } from 'svelte'
  import { marked } from 'marked'
  import DOMPurify from 'dompurify'
  import { store } from '../../../lib/store.svelte'
  import { fileResUrl, resolveLocalRef } from '../../../lib/respaths'
  import { clearDomFind, domFind } from '../../../lib/domfind'
  import { matchRanges } from '../../../lib/findtext'
  import type { ResTab } from '../registry'
  import FindLayer from './FindLayer.svelte'

  let {
    tab,
    active = false,
    flush = false,
    onSave,
  }: { tab: ResTab; active?: boolean; flush?: boolean; onSave?: () => void } = $props()

  marked.setOptions({ breaks: true, gfm: true })

  let preview = $state(true)

  /* ── 文件内查找（ResourcePane 调 Ctrl+F 时驱动本组件）──
     预览态搜渲染后的 DOM（整篇正文），原文态搜源码；切换视图时
     用同一查询重新定位 */
  let editorEl = $state<HTMLTextAreaElement>()
  let mdBodyEl = $state<HTMLDivElement>()
  let findQuery = $state('')
  let findIndex = $state(0)
  let findActive = $state(false)
  let boundKey = ''

  export function find(q: string, i: number): { count: number; index: number } {
    findQuery = q
    findActive = !!q.trim()
    if (!findActive) return { count: 0, index: 0 }
    if (preview) {
      const r = domFind(mdBodyEl, q, i)
      findIndex = r.index
      return r
    }
    const count = matchRanges(tab.content, q).length
    const index = count ? ((i % count) + count) % count : 0
    findIndex = index
    return { count, index }
  }

  export function clearFind(): void {
    findActive = false
    findQuery = ''
    findIndex = 0
    clearDomFind(mdBodyEl)
  }

  /* 预览/原文切换后按原查询重新定位（原文态的命中随 DOM 一起销毁） */
  function setPreview(v: boolean): void {
    if (v === preview) return
    if (!v) clearDomFind(mdBodyEl)
    preview = v
    if (findActive) void tick().then(() => find(findQuery, findIndex))
  }

  /* 切 tab：旧文件的查询与高亮不跟到新文件 */
  $effect(() => {
    if (tab.key === boundKey) return
    boundKey = tab.key
    clearFind()
  })

  /* 媒体 src 改写白名单（href 只认链接类标签） */
  const SRC_TAGS = new Set(['IMG', 'VIDEO', 'AUDIO', 'SOURCE', 'EMBED', 'TRACK', 'IMAGE'])

  /* 消毒期改写本地资源引用：src → 文件服务 URL；href → 占位 # +
     data-file-link（点击委托进资源页）。无 tab.path（草稿等）不改写 */
  function attrHook(base: string, node: Element, data: { attrName: string; attrValue: string }) {
    const tag = node.tagName
    const isMediaSrc = data.attrName === 'src' || data.attrName === 'xlink:href'
    const isLinkHref = data.attrName === 'href' && (tag === 'A' || tag === 'AREA')
    if (!isMediaSrc && !isLinkHref) return
    if (isMediaSrc && !SRC_TAGS.has(tag)) return
    const abs = resolveLocalRef(base, data.attrValue)
    if (!abs) return
    if (isLinkHref) {
      node.setAttribute('data-file-link', abs)
      data.attrValue = '#'
    } else {
      data.attrValue = fileResUrl(abs)
    }
  }

  const html = $derived.by(() => {
    if (!tab.content) return ''
    const parsed = marked.parse(tab.content) as string
    if (!tab.path) return DOMPurify.sanitize(parsed)
    const hook = (node: Element, data: { attrName: string; attrValue: string }) =>
      attrHook(tab.path!, node, data)
    DOMPurify.addHook('uponSanitizeAttribute', hook)
    const out = DOMPurify.sanitize(parsed)
    DOMPurify.removeHook('uponSanitizeAttribute')
    return out
  })

  /* 本地文件链接点击：在资源页打开（不在 SPA 里裸跳相对地址） */
  function onPreviewClick(e: MouseEvent) {
    const a = (e.target as HTMLElement).closest<HTMLAnchorElement>('a[data-file-link]')
    if (!a) return
    e.preventDefault()
    const p = a.dataset.fileLink || ''
    if (p) store.openFileAt(p)
  }

  function onKey(e: KeyboardEvent) {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's' && !e.isComposing) {
      e.preventDefault()
      onSave?.()
    }
  }
</script>

<div class="viewer" class:flush>
  <div class="bar">
    <div class="seg">
      <button class:active={preview} onclick={() => setPreview(true)}>预览</button>
      <button class:active={!preview} onclick={() => setPreview(false)}>原文</button>
    </div>
    {#if !preview}<span class="hint">编辑后 Ctrl+S 保存</span>{/if}
  </div>
  {#if tab.err}
    <div class="veil err">{tab.err}</div>
  {:else if tab.loading}
    <div class="veil">加载中…</div>
  {:else if preview}
    <div class="md-body" bind:this={mdBodyEl} onclick={onPreviewClick}>
      {@html html}
    </div>
  {:else}
    <div class="editor-wrap">
      <textarea
        bind:this={editorEl}
        bind:value={tab.content}
        onkeydown={onKey}
        spellcheck="false"
        placeholder="（空文件）"
      ></textarea>
      {#if findActive}
        <FindLayer text={tab.content} query={findQuery} current={findIndex} scrollEl={editorEl} />
      {/if}
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
    gap: 8px;
  }
  /* 预览/原文 = 分段控件（一个容器两条边，替代两颗各自带框的胶囊） */
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
  .md-body,
  .editor-wrap {
    flex: 1;
    min-height: 0;
    border: 1px solid var(--line);
    border-radius: 12px;
    background: var(--card);
    color: var(--fg); /* 预览正文显式落主题前景：不依赖继承，暗色下不发虚 */
    overflow: auto;
    padding: 12px 16px;
    font-size: var(--font-size-section);
    line-height: 1.7;
  }
  /* 预览按正文阅读排：字号/行距比旧版松一档，长文不挤 */
  .md-body {
    padding: 16px 20px 24px;
    font-size: var(--font-size-body);
    line-height: 1.75;
    overflow-x: hidden;
  }
  /* 满铺模式（目录钉住展开）：内容区是一整面，取消内层卡片边框与圆角 */
  .viewer.flush {
    gap: 0;
  }
  .viewer.flush .bar {
    padding: 4px 8px;
  }
  .viewer.flush .md-body,
  .viewer.flush .editor-wrap {
    border: none;
    border-radius: 0;
  }
  /* 标题层级收敛：浏览器默认 h1 2em / h2 1.5em 又粗又大，长文像海报；
     统一 600 字重、行高 1.35，只有 h2 留一条细分隔线 */
  .md-body :global(h1),
  .md-body :global(h2),
  .md-body :global(h3),
  .md-body :global(h4),
  .md-body :global(h5),
  .md-body :global(h6) {
    margin: 1.5em 0 0.55em;
    font-weight: 600;
    line-height: 1.35;
    color: var(--fg);
  }
  .md-body :global(h1:first-child),
  .md-body :global(h2:first-child),
  .md-body :global(h3:first-child),
  .md-body :global(h4:first-child) {
    margin-top: 0;
  }
  .md-body :global(h1) {
    font-size: 21px;
    letter-spacing: -0.01em;
  }
  .md-body :global(h2) {
    font-size: 17px;
    padding-bottom: 6px;
    border-bottom: 1px solid var(--line);
  }
  .md-body :global(h3) {
    font-size: 15px;
  }
  .md-body :global(h4) {
    font-size: 14px;
  }
  .md-body :global(h5),
  .md-body :global(h6) {
    font-size: 13px;
    color: var(--muted);
  }
  .md-body :global(p) {
    margin: 0.6em 0;
  }
  .md-body :global(ul),
  .md-body :global(ol) {
    margin: 0.6em 0;
    padding-left: 1.5em;
  }
  .md-body :global(li) {
    margin: 0.25em 0;
  }
  .md-body :global(li > p) {
    margin: 0.2em 0;
  }
  .md-body :global(strong) {
    font-weight: 600;
  }
  .md-body :global(hr) {
    border: none;
    border-top: 1px solid var(--line);
    margin: 1.6em 0;
  }
  /* 代码块：一块安静的浅底，不做外框；行内 code 用同一底色但更小 */
  .md-body :global(pre) {
    margin: 0.9em 0;
    padding: 12px 14px;
    border-radius: 8px;
    background: var(--bg-soft);
    /* 代码块同样折行：文本类预览不出现横向滚动条 */
    white-space: pre-wrap;
    overflow-wrap: break-word;
    overflow-x: hidden;
    font-family: var(--font-mono);
    font-size: 12.5px;
    line-height: 1.65;
  }
  .md-body :global(code) {
    font-family: var(--font-mono);
    font-size: 0.92em;
    background: var(--bg-soft);
    border-radius: 4px;
    padding: 1px 5px;
  }
  .md-body :global(pre code) {
    background: transparent;
    padding: 0;
    font-size: inherit;
    border-radius: 0;
  }
  /* 表格：只留横向行线，去掉整格网格（满屏框线很吵） */
  .md-body :global(table) {
    width: 100%;
    margin: 0.9em 0;
    border-collapse: collapse;
    font-size: 13px;
  }
  .md-body :global(th),
  .md-body :global(td) {
    padding: 6px 10px;
    text-align: left;
    border-bottom: 1px solid var(--line);
    vertical-align: top;
    overflow-wrap: break-word;
  }
  .md-body :global(th) {
    font-weight: 600;
    color: var(--muted);
    background: var(--bg-soft);
  }
  .md-body :global(tr:last-child td) {
    border-bottom: none;
  }
  .md-body :global(img) {
    max-width: 100%;
    border-radius: 6px;
  }
  .md-body :global(a) {
    color: var(--accent, #2563eb);
  }
  .md-body :global(blockquote) {
    margin: 0.9em 0;
    padding: 2px 0 2px 14px;
    border-left: 3px solid var(--line-strong);
    color: var(--muted);
  }
  .md-body :global(blockquote > :first-child) {
    margin-top: 0;
  }
  .md-body :global(blockquote > :last-child) {
    margin-bottom: 0;
  }
  .editor-wrap {
    position: relative;
    padding: 0;
    overflow: hidden;
  }
  .editor-wrap textarea {
    display: block;
    width: 100%;
    height: 100%;
    padding: 14px 16px;
    font-family: var(--font-mono);
    font-size: 14px;
    line-height: 24px;
    tab-size: 4;
    white-space: pre-wrap;
    overflow-wrap: break-word;
    overflow-x: hidden;
    overflow-y: auto;
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
    font-size: var(--font-size-section);
  }
  .veil.err {
    color: var(--danger);
    padding: 0 20px;
    text-align: center;
  }
</style>
