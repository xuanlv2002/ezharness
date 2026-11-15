<script lang="ts">
  /*
  HTML 查看器：预览（iframe sandbox = 禁脚本，纯静态渲染，无脚本执行
  面）/ 原文（textarea 编辑，Ctrl+S 保存）切换。预览前把相对资源引用
  （src/href/poster）按文件所在目录改写成工作目录文件服务 URL
  （respaths）——沙箱 iframe 里相对地址本会按 SPA 地址解析而全部 404。

  sandbox 必须放行 allow-same-origin：空 sandbox 的 iframe 是不透明源，
  其中发起的图片/CSS 请求被判定为跨站，SameSite=Strict 的鉴权 cookie
  不会附带，/api/workspace/file 全部 401（表现为图片裂开）。放行同源后
  资源请求正常带 cookie；未加 allow-scripts，预览页仍不能执行脚本。
  */

  import { tick } from 'svelte'
  import { fileResUrl, resolveLocalRef } from '../../../lib/respaths'
  import { clearDomFind, domFind } from '../../../lib/domfind'
  import { matchRanges } from '../../../lib/findtext'
  import { findScope } from '../../../lib/findscope'
  import type { ResTab } from '../registry'
  import FindLayer from './FindLayer.svelte'

  let {
    tab,
    active = false,
    flush = false,
    onSave,
    onfind,
    onfindclose,
  }: {
    tab: ResTab
    active?: boolean
    flush?: boolean
    onSave?: () => void
    onfind?: () => void
    onfindclose?: () => void
  } = $props()

  let preview = $state(true)

  /* ── 文件内查找（ResourcePane 调 Ctrl+F 时驱动本组件）──
     预览态搜 iframe 渲染后的 DOM（sandbox 放行同源，父页可直接读写），
     原文态搜源码 */
  let editorEl = $state<HTMLTextAreaElement>()
  let frameEl = $state<HTMLIFrameElement>()
  let findQuery = $state('')
  let findIndex = $state(0)
  let findActive = $state(false)
  let boundKey = ''

  function frameBody(): HTMLElement | null {
    try {
      return frameEl?.contentDocument?.body ?? null
    } catch {
      return null /* 理论不可达：同源被放行；防御未来 sandbox 策略回退 */
    }
  }

  export function find(q: string, i: number): { count: number; index: number } {
    findQuery = q
    findActive = !!q.trim()
    if (!findActive) return { count: 0, index: 0 }
    if (preview) {
      const r = domFind(frameBody(), q, i)
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
    clearDomFind(frameBody())
  }

  /* 预览/原文切换后按原查询重新定位 */
  function setPreview(v: boolean): void {
    if (v === preview) return
    if (!v) clearDomFind(frameBody())
    preview = v
    if (findActive) void tick().then(() => find(findQuery, findIndex))
  }

  /*
  srcdoc 重建（正文变化/切 tab）后 iframe 是新文档：查找开着就重刷；
  同时把指针/键盘归属接进来——iframe 有独立文档，父页 window 收不到
  它的 pointerdown/keydown，点进预览后 Ctrl+F 会落空。
  */
  function onFrameLoad(): void {
    const doc = frameEl?.contentDocument
    if (doc) {
      doc.addEventListener('pointerdown', onFrameDown, true)
      doc.addEventListener('keydown', onFrameKey, true)
    }
    if (findActive) find(findQuery, findIndex)
  }

  function onFrameDown(): void {
    findScope.file = true
  }

  function onFrameKey(e: KeyboardEvent): void {
    if ((e.ctrlKey || e.metaKey) && !e.altKey && e.key.toLowerCase() === 'f') {
      e.preventDefault()
      findScope.file = true
      onfind?.()
    } else if (findActive && e.key === 'Escape') {
      e.preventDefault()
      clearFind()
      onfindclose?.()
    }
  }

  $effect(() => {
    if (tab.key === boundKey) return
    boundKey = tab.key
    clearFind()
  })

  /* 属性值形态：双引号 / 单引号 / 裸值三种引法都认 */
  const RES_ATTR_RE = /\b(src|href|poster|data)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'`<>]+))/gi

  const framed = $derived.by(() => {
    if (!tab.path) return tab.content
    return tab.content.replace(RES_ATTR_RE, (whole, attr: string, dq?: string, sq?: string, bare?: string) => {
      const raw = dq ?? sq ?? bare ?? ''
      const abs = resolveLocalRef(tab.path!, raw)
      if (!abs) return whole
      return `${attr}="${fileResUrl(abs)}"`
    })
  })

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
    <span class="hint">{preview ? '沙箱静态渲染（禁脚本；本地资源按文件目录解析）' : '编辑后 Ctrl+S 保存'}</span>
  </div>
  {#if tab.err}
    <div class="veil err">{tab.err}</div>
  {:else if tab.loading}
    <div class="veil">加载中…</div>
  {:else if preview}
    <iframe
      class="frame"
      title="HTML 预览"
      sandbox="allow-same-origin"
      srcdoc={framed}
      bind:this={frameEl}
      onload={onFrameLoad}
    ></iframe>
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
  .frame,
  .editor-wrap {
    flex: 1;
    min-height: 0;
    width: 100%;
    border: 1px solid var(--line);
    border-radius: 12px;
    background: #fff;
  }
  /* 满铺模式（目录钉住展开）：内容区是一整面，取消内层卡片边框与圆角 */
  .viewer.flush {
    gap: 0;
  }
  .viewer.flush .bar {
    padding: 4px 8px;
  }
  .viewer.flush .frame,
  .viewer.flush .editor-wrap,
  .viewer.flush .veil {
    border: none;
    border-radius: 0;
  }
  .frame {
    border: none;
  }
  .editor-wrap {
    position: relative;
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
    color: #e74c3c;
    padding: 0 20px;
    text-align: center;
  }
</style>
