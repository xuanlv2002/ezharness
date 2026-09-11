<script lang="ts">
  import { marked } from 'marked'
  import DOMPurify from 'dompurify'
  import { api, type ImagePayload } from '../lib/api'
  import { fileBaseName, isImagePath } from '../lib/textfile'
  import { store } from '../lib/store.svelte'

  let {
    text,
    images,
    files,
    fileRefs,
    reasoning = '',
    streaming = false,
    role,
    onFork,
  }: {
    text: string
    images?: ImagePayload[]
    files?: { name: string; path?: string }[]
    fileRefs?: { path: string; count: number }[]
    reasoning?: string
    streaming?: boolean
    role: 'user' | 'assistant'
    onFork?: () => void
  } = $props()

  marked.setOptions({ breaks: true, gfm: true })

  /* <$supper_url> 特殊渲染语法（占位）：闭合标签解析为可点击 chip——
     http(s) 经系统浏览器打开（复用外链拦截）；term://<终端id> 拉开
     共享终端抽屉并定位；app://<快应用名> 打开快应用子窗；
     file://<绝对路径> 拉开抽屉文件页编辑（文本白名单门禁）。其他内容
     仅展示。流式未闭合时按原文转义显示。 */
  const escHtml = (s: string) =>
    s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
  marked.use({
    extensions: [
      {
        name: 'supperUrl',
        level: 'inline',
        start(src: string) {
          return src.indexOf('<$supper_url>')
        },
        tokenizer(src: string) {
          const m = src.match(/^<\$supper_url>([\s\S]*?)<\/\$supper_url>/)
          if (!m) return undefined
          return { type: 'supperUrl', raw: m[0], text: m[1].trim() }
        },
        renderer(token: any) {
          const t = String(token.text ?? '')
          const b = escHtml(t)
          if (/^https?:\/\//i.test(t)) {
            return `<a class="supper-url" data-supper-url="${b}" href="${b}">${b}</a>`
          }
          let m = t.match(/^term:\/\/([^\s]+)$/i)
          if (m) {
            const id = escHtml(m[1])
            return `<span class="supper-url act" data-supper-kind="term" data-supper-id="${id}" role="button" tabindex="0">▤ 终端 ${id}</span>`
          }
          m = t.match(/^app:\/\/([^\s]+)$/i)
          if (m) {
            const id = escHtml(m[1])
            return `<span class="supper-url act" data-supper-kind="app" data-supper-id="${id}" role="button" tabindex="0">▶ 快应用 ${id}</span>`
          }
          m = t.match(/^file:\/\/(.+)$/i)
          if (m) {
            const p = m[1].trim()
            return `<span class="supper-url act" data-supper-kind="file" data-supper-id="${escHtml(p)}" role="button" tabindex="0" title="${escHtml(p)}">📄 ${escHtml(fileBaseName(p))}</span>`
          }
          return `<a class="supper-url" data-supper-url="${b}">${b}</a>`
        },
      },
    ],
  })

  /* term:// / app:// / file:// chip 点击委托（{@html} 渲染无法直接绑事件） */
  function onBodyClick(e: MouseEvent) {
    const el = (e.target as HTMLElement).closest<HTMLElement>('span[data-supper-kind]')
    if (!el) return
    const kind = el.dataset.supperKind
    const id = el.dataset.supperId || ''
    if (kind === 'term') {
      store.openTermAt(id)
    } else if (kind === 'app') {
      void api
        .openApp(id)
        .then(() => (store.lastStatus = `正在打开快应用 ${id}`))
        .catch((err: unknown) => (store.lastStatus = `打开快应用失败：${(err as Error).message}`))
    } else if (kind === 'file') {
      /* 资源页按类型路由（registry）：文本进编辑器、图片进画板、
      html/pdf 有专属查看器、其余占位提示 */
      store.openFileAt(id)
    }
  }

  /* 模型输出渲染 markdown（XSS 消毒）；mermaid 在流结束后由 effect 替换渲染 */
  const mdHtml = $derived(text ? DOMPurify.sanitize(marked.parse(text) as string) : '')

  let bodyEl: HTMLDivElement | undefined = $state()
  let mmdSeq = 0

  /* mermaid 代码块 → svg：动态加载（包体大，不占首屏）；语法错误保留源码块 */
  $effect(() => {
    if (streaming || !bodyEl || !mdHtml.includes('language-mermaid')) return
    void renderMermaids(bodyEl)
  })

  async function renderMermaids(root: HTMLElement) {
    const blocks = [...root.querySelectorAll('pre > code.language-mermaid')] as HTMLElement[]
    if (!blocks.length) return
    const mermaid = (await import('mermaid')).default
    mermaid.initialize({ startOnLoad: false, theme: 'neutral', securityLevel: 'strict' })
    for (const code of blocks) {
      const pre = code.parentElement
      if (!pre || (pre as HTMLElement).dataset.mmd === '1') continue
      ;(pre as HTMLElement).dataset.mmd = '1'
      try {
        const { svg } = await mermaid.render('mmd-' + ++mmdSeq, code.textContent ?? '')
        const box = document.createElement('div')
        box.className = 'mermaid-box'
        box.innerHTML = svg
        pre.replaceWith(box)
      } catch {
        delete (pre as HTMLElement).dataset.mmd
      }
    }
  }

  let copied = $state(false)

  async function copyText() {
    try {
      await navigator.clipboard.writeText(text)
      copied = true
      setTimeout(() => (copied = false), 1500)
    } catch {
      /* 剪贴板不可用（非安全上下文等）静默 */
    }
  }

  /* 附件预览地址（工作目录文件服务；带版本参数——画板写回后强制刷新） */
  const fileUrl = (p?: string) =>
    p ? `/api/workspace/file?path=${encodeURIComponent(p)}${(store.imgVer[p] ?? 0) ? `&v=${store.imgVer[p]}` : ''}` : ''
</script>

{#if role === 'user'}
  <div class="user enter-rise">
    <span class="tag">你</span>
    <div class="ucontent">
      {#if files?.length}
        <div class="fchips">
          {#each files as f, i (i)}
            <button class="fchip"
              onclick={() => f.path && store.openFileAt(f.path)}
              title={f.path ? `${f.path}（点击在资源页打开——图片可编辑写回）` : f.name} disabled={!f.path}>
              {#if isImagePath(f.name)}
                {#if f.path}
                  <img src={fileUrl(f.path)} alt={f.name} loading="lazy" />
                {:else}
                  <span class="fico">🖼</span>
                {/if}
              {:else}
                <span class="fico">📄</span>
              {/if}
              <span class="fname">{f.name}</span>
            </button>
          {/each}
        </div>
      {/if}
      {#if fileRefs?.length}
        <div class="fchips">
          {#each fileRefs as r, i (i)}
            <button class="fchip ref" onclick={() => store.openFileAt(r.path)} title={`${r.path}（点击在资源页打开）`}>
              <span class="fico">🔗</span>
              <span class="fname">{r.path.split(/[/\\]/).pop()}{r.count > 1 ? ` · ${r.count} 条标注` : r.count === 1 ? ' · 1 条标注' : ''}</span>
            </button>
          {/each}
        </div>
      {/if}
      {#if images?.length}
        <div class="imgs">
          {#each images as img, i (i)}
            <img src={`data:${img.mimeType};base64,${img.data}`} alt="附件图片 {i + 1}" loading="lazy"
              onclick={() => void store.openBase64Draft(`data:${img.mimeType};base64,${img.data}`, `图片 ${i + 1}.png`)}
              title="转为画板草稿编辑" />
          {/each}
        </div>
      {/if}
      {#if text}
        <div class="bubble">{text}</div>
      {/if}
    </div>
    {#if onFork}
      <button class="forkbtn" onclick={onFork} title="从这条消息分叉：复制到此为止的对话，开一条新分支继续">⑂ 分叉</button>
    {/if}
  </div>
{:else}
  <div class="assistant">
    <span class="tag">ez</span>
    <div class="body">
      {#if reasoning}
        <details class="reasoning">
          <summary>思考过程</summary>
          <div class="reasoning-text">{reasoning}</div>
        </details>
      {/if}
      {#if text}
        <div class="md" bind:this={bodyEl} onclick={onBodyClick}>
          {@html mdHtml}
        </div>
        <div class="foot">
          {#if streaming}<span class="caret"></span>{/if}
          {#if onFork}
            <button class="forkbtn" onclick={onFork} title="从这条消息分叉：复制到此为止的对话，开一条新分支继续">⑂ 分叉</button>
          {/if}
          <button class="copy" onclick={copyText} title="复制原文">
            {copied ? '已复制 ✓' : '复制'}
          </button>
        </div>
      {:else if streaming}
        <div class="text"><span class="caret"></span></div>
      {/if}
    </div>
  </div>
{/if}

<style>
  .user,
  .assistant {
    display: flex;
    gap: 14px;
    align-items: flex-start;
  }
  /* 分叉按钮：与复制按钮同款弱化样式，hover 显形 */
  .forkbtn {
    flex: none;
    align-self: flex-end;
    font-size: 11px;
    color: var(--faint);
    padding: 2px 8px;
    border-radius: 6px;
    opacity: 0;
    transition:
      opacity var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .user:hover .forkbtn,
  .assistant:hover .forkbtn,
  .forkbtn:focus-visible {
    opacity: 1;
  }
  /* hover 分叉按钮时高亮所属消息：user/assistant 按钮垂直相邻，
     不高亮难以分辨"分叉到哪条"（误点即多带一条回复） */
  .user:has(.forkbtn:hover) .bubble,
  .user:has(.forkbtn:focus-visible) .bubble {
    outline: 1px solid color-mix(in srgb, var(--accent) 55%, transparent);
    outline-offset: 2px;
  }
  .assistant:has(.forkbtn:hover) .body,
  .assistant:has(.forkbtn:focus-visible) .body {
    outline: 1px solid color-mix(in srgb, var(--accent) 55%, transparent);
    outline-offset: 4px;
    border-radius: 6px;
  }
  .forkbtn:hover {
    color: var(--accent);
    background: var(--bg-soft);
  }
  .tag {
    flex: none;
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    margin-top: 2px;
    font-family: var(--font-mono);
    font-size: 11px;
    font-weight: 700;
    border: 1px solid var(--line-strong);
    border-radius: 6px;
    background: var(--bg);
    color: var(--fg);
  }
  .user .tag {
    background: var(--bg-invert);
    color: var(--fg-invert);
    border-color: var(--bg-invert);
  }
  /* 黑底气泡保留；全局 ::selection 是黑底，在黑气泡上选中态隐形——
     气泡内覆盖为白色半透明，复制范围清晰可见 */
  .bubble {
    background: var(--bg-invert);
    color: var(--fg-invert);
    padding: 10px 14px;
    border-radius: 12px;
    white-space: pre-wrap;
    word-break: break-word;
  }
  .bubble::selection {
    background: rgb(255 255 255 / 32%);
    color: var(--fg-invert);
  }
  /* 多模态图片：缩略网格（点击原生放大交给浏览器，保持零依赖）。
     宽度上限挂这里（参照 .user 的确定宽度）——bubble 的百分比若参照
     fit-content 的本容器会循环依赖，解析成极窄宽度致文字竖排 */
  .ucontent {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 6px;
    min-width: 0;
    max-width: 86%;
  }
  .imgs {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .imgs img {
    max-width: 240px;
    max-height: 240px;
    border-radius: 10px;
    border: 1px solid var(--line);
    cursor: zoom-in;
  }
  /* 附件 chips：图片带缩略图（工作目录文件服务），其他文件名 chip；
     path 回填前（发送瞬间）只显名不可点 */
  .fchips {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .fchip {
    display: flex;
    align-items: center;
    gap: 6px;
    border: 1px solid var(--line);
    background: var(--bg-soft);
    border-radius: 9px;
    padding: 4px 10px 4px 5px;
    max-width: 220px;
    cursor: pointer;
    transition: border-color var(--dur-fast) var(--ease-out);
  }
  .fchip:hover:not(:disabled) {
    border-color: var(--line-strong);
  }
  .fchip:disabled {
    cursor: default;
    opacity: 0.7;
  }
  .fchip img {
    width: 26px;
    height: 26px;
    object-fit: cover;
    border-radius: 5px;
    flex: none;
  }
  .fico {
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    border-radius: 5px;
    background: var(--bg);
    font-size: 13px;
    flex: none;
  }
  .fname {
    font-size: 11.5px;
    color: var(--fg);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .body {
    min-width: 0;
    flex: 1;
    padding-top: 3px;
  }
  .text {
    white-space: pre-wrap;
    word-break: break-word;
  }
  .caret {
    display: inline-block;
    width: 8px;
    height: 17px;
    margin-left: 2px;
    vertical-align: -2px;
    background: var(--fg);
    animation: caret 1s steps(1) infinite;
  }
  .reasoning {
    margin-bottom: 8px;
    border-left: 2px solid var(--line);
    padding-left: 12px;
    color: var(--muted);
    font-size: 13px;
  }
  .reasoning summary {
    cursor: pointer;
    user-select: none;
    font-size: 12px;
    letter-spacing: 0.02em;
  }
  .reasoning-text {
    white-space: pre-wrap;
    margin-top: 6px;
    line-height: 1.6;
  }
  /* 复制按钮行：右下角，弱化存在感 */
  .foot {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 2px;
    height: 22px;
  }
  .copy {
    font-size: 11px;
    color: var(--faint);
    padding: 2px 8px;
    border-radius: 6px;
    opacity: 0;
    transition:
      opacity var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .assistant:hover .copy,
  .copy:focus-visible {
    opacity: 1;
  }
  .copy:hover {
    color: var(--fg);
    background: var(--bg-soft);
  }
  /* ── markdown 正文：scoped 样式不作用于 {@html} 注入节点，全部走 :global ── */
  .md {
    line-height: 1.7;
  }
  .md :global(p) {
    margin: 6px 0;
  }
  .md :global(p:first-child) {
    margin-top: 0;
  }
  .md :global(p:last-child) {
    margin-bottom: 0;
  }
  .md :global(h1),
  .md :global(h2),
  .md :global(h3),
  .md :global(h4) {
    margin: 14px 0 6px;
    font-weight: 650;
    line-height: 1.4;
  }
  .md :global(h1) {
    font-size: 1.25em;
  }
  .md :global(h2) {
    font-size: 1.15em;
  }
  .md :global(h3),
  .md :global(h4) {
    font-size: 1.05em;
  }
  .md :global(ul),
  .md :global(ol) {
    margin: 6px 0;
    padding-left: 1.5em;
  }
  .md :global(li) {
    margin: 2px 0;
  }
  .md :global(code) {
    font-family: var(--font-mono);
    font-size: 0.88em;
    background: var(--bg-soft);
    border-radius: 5px;
    padding: 1px 5px;
  }
  .md :global(pre) {
    margin: 8px 0;
    padding: 10px 12px;
    background: var(--bg-soft);
    border-radius: 10px;
    overflow-x: auto;
  }
  .md :global(pre code) {
    background: none;
    border: none;
    padding: 0;
    font-size: 12.5px;
    line-height: 1.6;
  }
  .md :global(blockquote) {
    margin: 8px 0;
    padding: 2px 12px;
    border-left: 3px solid var(--line-strong);
    color: var(--muted);
  }
  /* 极简表格：无竖线无外框，表头仅一条深色底线，行间细线分隔 */
  .md :global(table) {
    margin: 10px 0;
    border-collapse: collapse;
    font-size: 0.94em;
  }
  .md :global(th),
  .md :global(td) {
    padding: 6px 18px 6px 0;
    text-align: left;
    vertical-align: top;
  }
  .md :global(th) {
    border-bottom: 2px solid var(--line-strong);
    font-weight: 650;
    background: none;
  }
  .md :global(td) {
    border-bottom: 1px solid var(--line);
  }
  .md :global(tr:last-child td) {
    border-bottom: none;
  }
  .md :global(a) {
    color: var(--accent);
    text-decoration: none;
  }
  .md :global(a:hover) {
    text-decoration: underline;
  }
  /* <$supper_url> 占位 chip：可点击强调块（http 外链 / 终端 / 快应用） */
  .md :global(a.supper-url),
  .md :global(span.supper-url) {
    display: inline-block;
    font-family: var(--font-mono);
    font-size: 12px;
    line-height: 1.4;
    padding: 2px 10px;
    margin: 2px 0;
    border: 1px solid var(--line-strong);
    border-radius: 999px;
    background: var(--bg-soft);
    color: var(--accent);
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    vertical-align: middle;
  }
  .md :global(span.supper-url) {
    cursor: pointer;
  }
  .md :global(a.supper-url:hover) {
    border-color: var(--accent);
    text-decoration: none;
  }
  .md :global(hr) {
    margin: 12px 0;
    border: none;
    border-top: 1px solid var(--line);
  }
  .md :global(.mermaid-box) {
    margin: 10px 0;
    padding: 12px;
    background: var(--bg-soft);
    border-radius: 10px;
    overflow-x: auto;
    text-align: center;
  }
</style>
