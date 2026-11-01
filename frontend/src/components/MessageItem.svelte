<script lang="ts">
  import { marked } from 'marked'
  import DOMPurify from 'dompurify'
  import { api, type ImagePayload } from '../lib/api'
  import { fileBaseName, isImagePath } from '../lib/textfile'
  import { launchApp } from '../lib/apps'
  import type { CallbackMessageMeta } from '../lib/callbackMessage'
  import { store } from '../lib/store.svelte'

  let {
    text,
    images,
    files,
    fileRefs,
    reasoning = '',
    streaming = false,
    role,
    bare = false,
    callback,
    onFork,
  }: {
    text: string
    images?: ImagePayload[]
    files?: { name: string; path?: string }[]
    fileRefs?: { path: string; count: number }[]
    reasoning?: string
    streaming?: boolean
    role: 'user' | 'assistant'
    bare?: boolean /* 无头像形态：回复组内由组头提供唯一头像 */
    callback?: CallbackMessageMeta /* 定时回调投递的用户消息：特殊卡头 */
    onFork?: () => void
  } = $props()

  marked.setOptions({ breaks: true, gfm: true })

  /* 回调卡头的时间：元数据是后端本机时区 YYYY-MM-DD HH:MM:SS，列表里缩到分。 */
  function callbackTime(at: string): string {
    const m = at.match(/^(\d{4})-(\d{2})-(\d{2})[ T](\d{2}):(\d{2})/)
    return m ? `${m[2]}-${m[3]} ${m[4]}:${m[5]}` : at
  }

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
          m = t.match(/^browser:\/\/([^\s]+)$/i)
          if (m) {
            const id = escHtml(m[1])
            return `<span class="supper-url act" data-supper-kind="browser" data-supper-id="${id}" role="button" tabindex="0">▣ 浏览器 ${id}</span>`
          }
          m = t.match(/^file:\/\/(.+)$/i)
          if (m) {
            const p = m[1].trim()
            return `<span class="supper-url act" data-supper-kind="file" data-supper-id="${escHtml(p)}" role="button" tabindex="0" title="${escHtml(p)}">${escHtml(fileBaseName(p))}</span>`
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
    } else if (kind === 'browser') {
      store.openBrowserAt(id)
    } else if (kind === 'app') {
      launchApp(id).catch((err: unknown) => (store.lastStatus = `打开快应用失败：${(err as Error).message}`))
    } else if (kind === 'file') {
      /* 资源页按类型路由（registry）：文本进编辑器、图片进画板、
      html/pdf 有专属查看器、其余占位提示 */
      store.openFileAt(id)
    }
  }

  /* 流式期思考只渲染尾部：超长思考逐 chunk 全文重排会卡死页面
     （每 chunk O(n)，累计 O(n²)）；结束后恢复全文 */
  const reasoningView = $derived(streaming ? reasoning.slice(-1200) : reasoning)

  /* 流式期正文同理分头/尾两段：每个 delta 全文 parse+sanitize+innerHTML
     重建是 O(n²)，长输出时主线程被打满——页面卡死无法滚动，积压的
     增量随后一次涌出。切段点取尾部窗口外的最后一个空行（块边界，且
     不落在未闭合代码围栏内），cut 不变头段不重算也不重建 DOM，尾段
     每 delta 重排（长度有界）。流结束定格切点，只重排尾段。 */
  const STREAM_TAIL = 2400
  /* cut 前进的迟滞余量：text 只在尾部追加（前缀不变），尾窗未超
     STREAM_TAIL+CUT_HYST 前沿用旧 cut——避免每 delta 都重找边界导致
     头段全文重 parse */
  const CUT_HYST = 4096
  let lastCut = -1

  function streamCut(src: string): number {
    if (lastCut > 0 && src.length - lastCut <= STREAM_TAIL + CUT_HYST) return lastCut
    const max = src.length - STREAM_TAIL
    if (max <= 0) return 0
    /* 无空行长文（整段代码/JSON）回退单换行、再退硬切：始终返回正 cut，
       否则尾段=全文，每 delta 全量 parse（O(n²)）会打满主线程 */
    let cut = src.lastIndexOf('\n\n', max)
    if (cut <= 0) cut = src.lastIndexOf('\n', max)
    if (cut <= 0) cut = max
    let fences = 0
    let openFence = -1
    for (let i = 0; i < cut; ) {
      if (src.startsWith('```', i)) {
        fences++
        if (fences % 2 === 1) openFence = i
      }
      const nl = src.indexOf('\n', i)
      if (nl === -1 || nl >= cut) break
      i = nl + 1
    }
    /* 未闭合围栏：切点退到围栏起点让围栏整体留在尾段；围栏起于文件头
       （openFence=0）时退化成尾段=全文，退回硬切保尾段有界 */
    if (fences % 2 === 1) cut = openFence > 0 ? openFence : max
    lastCut = cut
    return cut
  }

  let headCache = { cut: -1, html: '' }
  const streamView = $derived.by(() => {
    if (!text) return null
    /* 流式期切段前进；结束后定格在缓存切点——头段解析结果与 DOM 原样
       复用，只重排尾段（收尾不全文重排，长输出结束不卡顿） */
    const cut = streaming ? streamCut(text) : headCache.cut
    if (cut < 0) return null // 无缓存（回放/历史重建的消息）：走 mdHtml 全文
    if (headCache.cut !== cut) {
      const head = cut > 0 ? DOMPurify.sanitize(marked.parse(text.slice(0, cut)) as string) : ''
      headCache = { cut, html: head }
    }
    return { head: headCache.html, tail: DOMPurify.sanitize(marked.parse(text.slice(cut)) as string) }
  })

  /* 无流式头缓存的重建消息的全文渲染（XSS 消毒）；mermaid 在流结束后由 effect 替换渲染 */
  const mdHtml = $derived(text && !streaming ? DOMPurify.sanitize(marked.parse(text) as string) : '')

  /* 超长全文一次性 parse+innerHTML 会长时间占死主线程（历史重建时全部
     消息同帧全量渲染,表现为卡死后突然全部涌出）——超过阈值只渲染
     头尾两段+折叠提示,点击才展开全文(用户自选承担这次开销) */
  const FULL_LIMIT = 48 * 1024
  const HEAD_KEEP = 24 * 1024
  const TAIL_KEEP = 12 * 1024
  let expanded = $state(false)
  const clipped = $derived(!streaming && !streamView && !expanded && text.length > FULL_LIMIT)
  const clipHtml = $derived.by(() => {
    if (!clipped) return null
    let cut = text.lastIndexOf('\n\n', HEAD_KEEP)
    if (cut <= 0) cut = HEAD_KEEP
    let tailStart = text.indexOf('\n\n', text.length - TAIL_KEEP)
    if (tailStart < cut) tailStart = text.length
    return {
      hidden: tailStart - cut,
      head: DOMPurify.sanitize(marked.parse(text.slice(0, cut)) as string),
      tail: DOMPurify.sanitize(marked.parse(text.slice(tailStart)) as string),
    }
  })
  const reasoningClipped = $derived(streaming && reasoning.length > 1200)

  /* 用户气泡：超过高度折叠，按内容实际高度判断（与字号/换行无关） */
  const USER_BUBBLE_MAX_HEIGHT = 280
  let userBubbleEl = $state<HTMLDivElement>()
  let userExpanded = $state(false)
  let userClipped = $state(false)
  $effect(() => {
    text
    const bubbleEl = userBubbleEl
    if (!bubbleEl || userExpanded) return
    userClipped = bubbleEl.scrollHeight > USER_BUBBLE_MAX_HEIGHT + 1
  })

  let bodyEl: HTMLDivElement | undefined = $state()
  let mmdSeq = 0

  /* mermaid 代码块 → svg：动态加载（包体大，不占首屏）；语法错误保留源码块 */
  $effect(() => {
    if (streaming || !bodyEl) return
    const html = streamView ? streamView.head + streamView.tail : mdHtml
    if (!html.includes('language-mermaid')) return
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
    {#if onFork}
      <button class="forkbtn" onclick={onFork} title="从这条消息分叉：复制到此为止的对话，开一条新分支继续">⑂ 分叉</button>
    {/if}
    <div class="ucontent" class:cb={!!callback}>
      {#if callback}
        <div class="cbbar">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
            <circle cx="12" cy="12" r="9" />
            <path d="M12 7v5l3 2" />
          </svg>
          <span class="cblabel">定时回调</span>
          <span class="cbtitle" title={callback.title}>{callback.title}</span>
          {#if callback.at}<span class="cbtime">{callbackTime(callback.at)}</span>{/if}
          {#if callback.taskId}
            <button class="cbopen" onclick={() => store.openCallbackTask(callback.taskId)} title="打开回调任务详情">查看任务</button>
          {/if}
        </div>
      {/if}
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
                  <span class="fico"><svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="5" width="18" height="14" rx="2" /><circle cx="8.5" cy="10" r="1.5" /><path d="M21 15l-5-5L5 19" /></svg></span>
                {/if}
              {:else}
                <span class="fico"><svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" /><path d="M14 3v5h5" /></svg></span>
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
              <span class="fico"><svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M10 13a5 5 0 0 0 7.5.5l3-3a5 5 0 0 0-7-7l-1.5 1.5" /><path d="M14 11a5 5 0 0 0-7.5-.5l-3 3a5 5 0 0 0 7 7l1.5-1.5" /></svg></span>
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
        <div
          class="bubble"
          class:clipped={userClipped && !userExpanded}
          class:unfolded={userExpanded}
          bind:this={userBubbleEl}>{text}</div>
        {#if userClipped}
          <button class="more" onclick={() => (userExpanded = !userExpanded)}>
            {userExpanded ? '收起' : '展开全文'}
          </button>
        {/if}
      {/if}
    </div>
  </div>
{:else}
  <div class="assistant" class:bare>
    {#if !bare}<span class="tag">ez</span>{/if}
    <div class="body">
      {#if reasoning}
        <details class="reasoning">
          <summary>思考 · {reasoning.length} 字</summary>
          {#if reasoningClipped}<div class="rclip">…（流式中，前文暂折叠，结束后展开全文）</div>{/if}
          <div class="reasoning-text">{reasoningView}</div>
        </details>
      {/if}
      {#if text}
        <div class="md" class:live={streaming} bind:this={bodyEl} onclick={onBodyClick}>
          {#if streamView}
            <!-- 头/尾分节点：cut 不变时头段 DOM 不重建，delta 只重排尾段 -->
            {#if streamView.head}{@html streamView.head}{/if}{@html streamView.tail}
          {:else if clipHtml}
            {@html clipHtml.head}
            <button class="clipbtn" onclick={() => (expanded = true)}>
              …中间 {Math.max(1, Math.round(clipHtml.hidden / 1000))}k 字已折叠，点击展开全文（共 {Math.round(text.length / 1000)}k 字）
            </button>
            {@html clipHtml.tail}
          {:else}
            {@html mdHtml}
          {/if}
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
      {:else if onFork}
        <!-- 纯思考消息（无正文）：分叉钮悬浮右下不占布局高度，思考与
             后续工具卡紧凑衔接；锚点是同一条消息 -->
        <button class="forkbtn floating" onclick={onFork} title="从这条消息分叉：复制到此为止的对话，开一条新分支继续">⑂ 分叉</button>
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
  /* 用户消息右对齐：自己的话靠右，与助手内容左右分列 */
  .user {
    position: relative;
    justify-content: flex-end;
  }
  /* 分叉按钮：与复制按钮同款弱化样式，hover 显形 */
  .forkbtn {
    flex: none;
    font-size: var(--font-size-small);
    color: var(--faint);
    padding: 2px 8px;
    border-radius: 6px;
    opacity: 0;
    transition:
      opacity var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  /* 用户行分叉钮：排在气泡左侧、随气泡走（右对齐时紧贴气泡），贴行底 */
  .user > .forkbtn {
    align-self: flex-end;
  }
  /* 纯思考块的分叉钮：悬浮思考卡右下，不占布局高度 */
  .forkbtn.floating {
    position: absolute;
    right: 0;
    bottom: 2px;
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
  /* 非组内助手（分身面板）的「ez」徽章 */
  .tag {
    flex: none;
    display: grid;
    place-items: center;
    width: 36px;
    height: 36px;
    margin-top: 1px;
    font-size: var(--font-size-ui);
    font-weight: 700;
    border: 1px solid var(--line-strong);
    border-radius: 8px;
    background: var(--bg);
    color: var(--fg);
  }
  /* 用户气泡：带蓝调的浅色卡片（比灰底更轻），高度超过阈值折叠 */
  .bubble {
    background: color-mix(in srgb, var(--accent) 6%, var(--bg));
    color: var(--fg);
    padding: 10px 14px;
    border: 1px solid color-mix(in srgb, var(--accent) 14%, transparent);
    border-radius: 14px;
    white-space: pre-wrap;
    word-break: break-word;
    max-height: 280px;
    overflow: hidden;
  }
  .bubble.unfolded {
    max-height: none;
  }
  /* 折叠态底部渐隐，提示还有内容 */
  .bubble.clipped {
    mask-image: linear-gradient(to bottom, #000 calc(100% - 48px), transparent);
  }
  .more {
    font-size: var(--font-size-small);
    color: var(--accent);
    padding: 2px 4px;
  }
  .more:hover {
    text-decoration: underline;
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
    /* 与助手输出同宽：气泡最大占满内容列 */
    max-width: 100%;
  }
  /* 定时回调消息：卡头 + 左侧强调线的正文气泡，与普通用户输入区分 */
  .cbbar {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    max-width: 100%;
    padding: 3px 9px;
    border: 1px solid color-mix(in srgb, var(--accent) 28%, transparent);
    border-radius: 8px;
    background: var(--accent-soft);
    color: var(--accent);
    font-size: var(--font-size-small);
    line-height: 1.5;
  }
  .cbbar svg {
    flex: none;
    width: 12px;
    height: 12px;
  }
  .cblabel {
    flex: none;
    font-weight: 600;
  }
  .cbtitle {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--fg);
  }
  .cbtime {
    flex: none;
    font-family: var(--font-mono);
    color: color-mix(in srgb, var(--accent) 62%, var(--faint));
  }
  .cbopen {
    flex: none;
    padding: 0;
    border: none;
    background: transparent;
    color: var(--accent);
    font: inherit;
    font-size: var(--font-size-small);
    text-decoration: underline dotted;
    text-underline-offset: 2px;
    cursor: pointer;
  }
  .cbopen:hover {
    text-decoration-style: solid;
  }
  .ucontent.cb .bubble {
    border-left: 3px solid color-mix(in srgb, var(--accent) 55%, transparent);
    border-radius: 4px 12px 12px 4px;
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
    color: var(--muted);
    flex: none;
  }
  .fname {
    font-size: var(--font-size-ui);
    color: var(--fg);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .body {
    position: relative; /* 纯思考块的分叉钮悬浮定位的包含块 */
    min-width: 0;
    flex: 1;
    padding-top: 3px;
  }
  .assistant.bare .body {
    padding-top: 0;
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
    margin-bottom: 6px;
    border-left: 2px solid var(--line);
    padding-left: 10px;
    color: var(--muted);
    font-size: var(--font-size-section);
  }
  .reasoning summary {
    cursor: pointer;
    user-select: none;
    font-size: var(--font-size-small);
    font-variant-numeric: tabular-nums;
    letter-spacing: 0.02em;
  }
  .rclip {
    margin-top: 5px;
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .reasoning-text {
    white-space: pre-wrap;
    margin-top: 6px;
    line-height: 1.6;
    max-height: 40vh;
    overflow-y: auto;
    overscroll-behavior: contain;
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
    font-size: var(--font-size-small);
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
    /* 长路径/长串（行内 code、URL）就地断行，不撑宽消息列（pre 代码块
    white-space: pre 不受影响，横向滚动由其自身 overflow-x 承担） */
    overflow-wrap: anywhere;
  }
  /* 屏外消息跳过布局与绘制：历史重建时成百条长消息不再同帧全量渲染。
     流式中的消息除外（高度连续变化,虚拟化会让吸底/滚动跳动） */
  .md:not(.live) {
    content-visibility: auto;
    contain-intrinsic-size: auto 120px;
  }
  .md .clipbtn {
    display: block;
    width: 100%;
    margin: 6px 0;
    border: 1px dashed var(--line-strong);
    border-radius: 8px;
    background: transparent;
    padding: 6px 10px;
    font-size: var(--font-size-ui);
    color: var(--muted);
    cursor: pointer;
  }
  .md .clipbtn:hover {
    background: var(--bg-soft);
    color: var(--fg);
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
    font-weight: 600;
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
    font-size: var(--font-size-section);
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
    font-weight: 600;
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
    font-size: var(--font-size-ui);
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
