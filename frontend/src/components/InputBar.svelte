<script lang="ts">
  import { onMount } from 'svelte'
  import { store, type Att, type FileRef } from '../lib/store.svelte'
  import { fileBaseName, isImagePath, isTextFilePath } from '../lib/textfile'

  let {
    atts = [],
    refs = [],
    onRemove,
    onEditImage,
    onAddFiles,
    onClearFiles,
    onRemoveRef,
    onOpenRef,
    onClearRefs,
  }: {
    atts?: Att[]
    refs?: FileRef[]
    onRemove?: (i: number) => void
    onEditImage?: (i: number) => void
    onAddFiles?: (fs: File[]) => void
    onClearFiles?: () => void
    onRemoveRef?: (i: number) => void
    onOpenRef?: (path: string) => void
    onClearRefs?: () => void
  } = $props()

  let text = $state('')
  let focused = $state(false)
  let el: HTMLTextAreaElement | undefined = $state()

  /* 附件缩略：真身路径走文件服务源（与聊天记录同源，带版本参数——
  画板写回后强制刷新）；path 空 = 占位（拖入暂存中）或画板草稿（file） */
  const thumbs = $derived(
    atts.map((a) => {
      const isImage = isImagePath(a.name)
      const v = a.path ? (store.imgVer[a.path] ?? 0) : 0
      return {
        name: a.name,
        path: a.path,
        isImage,
        isText: isTextFilePath(a.path || a.name),
        url: isImage && a.path ? `/api/workspace/file?path=${encodeURIComponent(a.path)}${v ? `&v=${v}` : ''}` : '',
        ready: !!a.path || !!a.file,
        draft: !a.path && !!a.file,
      }
    }),
  )

  /* 附件数量上限（与后端校验一致；任意类型，拖入即暂存） */
  const MAX_FILES = 8

  onMount(() => {
    /* rows=1 的浏览器默认高度与行高不齐，挂载即校准为单行高 */
    if (el) {
      el.style.height = 'auto'
      el.style.height = el.scrollHeight + 'px'
    }
  })

  async function send() {
    const t = text.trim()
    if (!t && !atts.length && !refs.length) return
    if (store.archivingRootId === store.activeId) {
      store.lastStatus = '正在归档当前话题，完成后即可继续对话（可先切换分支）'
      return
    }
    if (atts.some((a) => !a.path && !a.file)) {
      store.lastStatus = '附件仍在暂存中，稍候再发送'
      return
    }
    if (atts.length > MAX_FILES) {
      store.lastStatus = `附件最多 ${MAX_FILES} 个，多余的未发送`
    }
    const sendAtts = atts.slice(0, MAX_FILES)
    text = ''
    onClearFiles?.()
    onClearRefs?.()
    /* 画板草稿（path 空有 file）由 store 在发送时 stash 持久化；refs
    统一进 <reference_file> 记录（不再展开进正文） */
    await store.send(t, sendAtts, refs.slice(0, MAX_FILES))
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
      e.preventDefault()
      void send()
    }
  }

  /* 粘贴文件：拦截剪贴板全部 file 项转附件（与拖拽同路，暂存回填）。
     Windows 复制任意文件 Ctrl+V 走 CF_HDROP（kind='file'）——图片、
     文本、pdf 一视同仁；纯文本粘贴（kind='string'）不受影响 */
  function onPaste(e: ClipboardEvent) {
    const items = [...(e.clipboardData?.items ?? [])]
    const files = items.filter((i) => i.kind === 'file').map((i) => i.getAsFile()).filter(Boolean)
    if (!files.length) return
    e.preventDefault()
    onAddFiles?.(files as File[])
  }

  function autoResize(e: Event) {
    const t = e.currentTarget as HTMLTextAreaElement
    t.style.height = 'auto'
    t.style.height = Math.min(t.scrollHeight, 200) + 'px'
  }
</script>

<div class="bar" class:focused>
  <div class="fade"></div>
  {#if store.archivingRootId === store.activeId}
    <div class="status">⇪ 正在归档话题…（完成后即可继续；期间可切换或新建分支）</div>
  {:else if store.busy && store.lastTool}
    <div class="status">⚙ {store.lastTool} 执行中…（点击 ⏹ 终止）</div>
  {:else if store.lastStatus}
    <div class="status">{store.lastStatus}</div>
  {/if}
  {#if atts.length || refs.length}
    <div class="attachments">
      {#each thumbs as t, i (i)}
        <div class="att" class:pending={!t.ready}>
          {#if t.isImage}
            <button class="thumb" disabled={!t.ready} onclick={() => onEditImage?.(i)}
              title={t.draft ? '画板草稿（未保存）——点击继续编辑，发送时才保存' : t.ready ? '打开画板编辑（保存写回原文件）' : '暂存中…'}>
              {#if t.url}<img src={t.url} alt={t.name} />{:else}<span class="draft-ico">🖌</span>{/if}
              <span class="edit-mark">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M17 3a2.85 2.85 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5L17 3z" />
                </svg>
              </span>
            </button>
          {:else if t.isText}
            <button class="file-ico as-btn" disabled={!t.ready} onclick={() => t.path && store.openFileAt(t.path)}
              title={t.ready ? '在资源页打开（编辑 · 标注 · 发给 AI）' : '暂存中…'}>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                <path d="M14 3H7a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1V7l-4-4z" />
                <path d="M14 3v4h4" />
              </svg>
            </button>
          {:else}
            <span class="file-ico">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                <path d="M14 3H7a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1V7l-4-4z" />
                <path d="M14 3v4h4" />
              </svg>
            </span>
          {/if}
          <span class="att-name">{t.name}{t.draft ? ' · 草稿' : ''}</span>
          <button class="att-x" onclick={() => onRemove?.(i)} title="移除">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
              <path d="M6 6l12 12M18 6L6 18" />
            </svg>
          </button>
        </div>
      {/each}
      {#each refs as r, i (i)}
        <button class="ref" onclick={() => onOpenRef?.(r.path)}
          title={`${r.path}（点击在文件页打开）`}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
            <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" />
            <path d="M14 3v5h5" />
          </svg>
          <span class="ref-name">{fileBaseName(r.path)}</span>
          {#if r.items.length}<span class="ref-n">{r.items.length} 条标注</span>{/if}
          <span class="att-x" onclick={(e) => { e.stopPropagation(); onRemoveRef?.(i) }} role="button" tabindex="-1" title="移除引用">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
              <path d="M6 6l12 12M18 6L6 18" />
            </svg>
          </span>
        </button>
      {/each}
    </div>
  {/if}
  <div class="box">
    <textarea
      rows="1"
      bind:this={el}
      placeholder={store.busy ? '运行中… 可点击 ⏹ 取消' : '发出指令…'}
      bind:value={text}
      onfocus={() => (focused = true)}
      onblur={() => (focused = false)}
      onkeydown={onKey}
      onpaste={onPaste}
      oninput={autoResize}
      disabled={!store.activeId}
    ></textarea>
    <button class="board-btn" onclick={() => store.openDraftImage()} title="画板草稿（画图/标注，添加到对话；发送时才保存）">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
        <path d="M12 19l7-7a4.95 4.95 0 1 0-7-7l-7 7v7h7z" />
        <path d="M16 8l1.5 1.5" />
      </svg>
    </button>
    {#if store.busy}
      <!-- 运行中：⏹ 终止当前轮；已输入文字时 ⬆ 可打断并改发新指令 -->
      <button class="stop" onclick={() => void store.cancel()} title="取消当前轮">
        <svg viewBox="0 0 24 24" fill="currentColor">
          <rect x="7" y="7" width="10" height="10" rx="1.5" />
        </svg>
      </button>
      <button class="send ghost" onclick={() => void send()} disabled={!text.trim() && !atts.length && !refs.length} title="终止当前轮并发送新指令">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M12 19V5" />
          <path d="M5 12l7-5 5 5" />
        </svg>
      </button>
    {:else}
      <button class="send" onclick={() => void send()} disabled={!text.trim() && !atts.length && !refs.length} title="发送">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M12 19V5" />
          <path d="M5 12l7-5 5 5" />
        </svg>
      </button>
    {/if}
  </div>
  <p class="meta">Enter 发送 · Shift+Enter 换行</p>
</div>

<style>
  .bar {
    flex: none;
    position: relative;
    max-width: 780px;
    width: calc(100% - 96px);
    margin: 0 auto;
    padding-bottom: 22px;
  }
  /* 聊天记录滚到输入区后方的渐隐过渡 */
  .fade {
    position: absolute;
    left: -48px;
    right: -48px;
    bottom: 100%;
    height: 36px;
    background: linear-gradient(to top, var(--bg), transparent);
    pointer-events: none;
  }
  .status {
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--faint);
    padding: 0 4px 8px;
    text-align: right;
  }
  .attachments {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    padding: 0 4px 10px;
  }
  .att {
    position: relative;
    display: flex;
    align-items: center;
    gap: 8px;
    border: 1px solid var(--line);
    border-radius: 10px;
    background: var(--bg);
    padding: 5px 28px 5px 6px;
    max-width: 220px;
    animation: rise var(--dur-fast) var(--ease-out) both;
  }
  .att img {
    width: 32px;
    height: 32px;
    object-fit: cover;
    border-radius: 6px;
    flex: none;
  }
  .file-ico {
    display: grid;
    place-items: center;
    width: 32px;
    height: 32px;
    border-radius: 6px;
    background: var(--bg-soft);
    color: var(--muted);
    flex: none;
  }
  .file-ico svg {
    width: 15px;
    height: 15px;
  }
  /* 文本附件的可点形态：点击进文件页编辑（与图片进画板对偶） */
  .file-ico.as-btn {
    border: none;
    padding: 0;
    cursor: pointer;
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .file-ico.as-btn:hover:not(:disabled) {
    background: var(--bg);
    color: var(--fg);
  }
  .file-ico.as-btn:disabled {
    cursor: default;
    opacity: 0.6;
  }
  /* 暂存中的占位 chip（path 未回填） */
  .att.pending {
    opacity: 0.55;
  }
  .att.pending .att-name::after {
    content: ' · 暂存中…';
    color: var(--faint);
  }
  /* 画板草稿 chip 的占位图（无真身路径，内容在内存） */
  .draft-ico {
    display: grid;
    place-items: center;
    width: 32px;
    height: 32px;
    border-radius: 6px;
    background: var(--bg-soft);
    font-size: 15px;
  }
  /* 文件引用 chip（文件页「添加到对话」产物）：路径引用不复制内容 */
  .ref {
    position: relative;
    display: flex;
    align-items: center;
    gap: 7px;
    border: 1px solid var(--accent-soft, var(--accent));
    border-radius: 10px;
    background: color-mix(in srgb, var(--accent, #2563eb) 5%, var(--bg));
    color: var(--fg);
    padding: 5px 26px 5px 8px;
    max-width: 260px;
    font-size: 11.5px;
    cursor: pointer;
    animation: rise var(--dur-fast) var(--ease-out) both;
    transition: background var(--dur-fast) var(--ease-out);
  }
  .ref:hover {
    background: color-mix(in srgb, var(--accent, #2563eb) 10%, var(--bg));
  }
  .ref svg {
    width: 14px;
    height: 14px;
    flex: none;
    color: var(--accent, #2563eb);
  }
  .ref-name {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .ref-n {
    flex: none;
    font-size: 10.5px;
    color: var(--accent, #2563eb);
  }
  .att-name {
    font-size: 11.5px;
    color: var(--fg);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .att-x {
    position: absolute;
    top: 50%;
    right: 5px;
    transform: translateY(-50%);
    display: grid;
    place-items: center;
    width: 18px;
    height: 18px;
    border: none;
    background: transparent;
    color: var(--faint);
    border-radius: 50%;
  }
  .att-x:hover {
    background: var(--line);
    color: var(--fg);
  }
  .att-x svg {
    width: 10px;
    height: 10px;
  }
  .thumb {
    position: relative;
    border: none;
    padding: 0;
    background: transparent;
    border-radius: 6px;
    flex: none;
    cursor: pointer;
  }
  .thumb img {
    display: block;
    width: 32px;
    height: 32px;
    object-fit: cover;
    border-radius: 6px;
  }
  .edit-mark {
    position: absolute;
    inset: 0;
    display: grid;
    place-items: center;
    background: rgb(0 0 0 / 45%);
    border-radius: 6px;
    color: #fff;
    opacity: 0;
    transition: opacity var(--dur-fast) var(--ease-out);
  }
  .thumb:hover .edit-mark {
    opacity: 1;
  }
  .edit-mark svg {
    width: 13px;
    height: 13px;
  }
  .box {
    display: flex;
    gap: 8px;
    align-items: center;
    border: 1px solid var(--line);
    background: var(--bg-soft);
    border-radius: 16px;
    padding: 12px 12px 12px 18px;
    transition:
      border-color var(--dur-fast) var(--ease-out),
      box-shadow var(--dur-fast) var(--ease-out),
      background var(--dur-fast) var(--ease-out);
  }
  .bar.focused .box {
    border-color: var(--line-strong);
    background: var(--bg);
    box-shadow: 0 2px 12px rgb(0 0 0 / 6%);
  }
  textarea {
    flex: 1;
    border: none;
    outline: none;
    resize: none;
    font-family: inherit;
    font-size: 15px;
    line-height: 24px;
    height: 24px;
    max-height: 200px;
    background: transparent;
    color: var(--fg);
  }
  textarea::placeholder {
    color: var(--faint);
  }
  textarea:disabled {
    opacity: 0.5;
  }
  .send,
  .stop {
    flex: none;
    width: 34px;
    height: 34px;
    border-radius: 11px;
    border: none;
    display: grid;
    place-items: center;
    background: var(--bg-invert);
    color: var(--fg-invert);
    transition: opacity var(--dur-fast) var(--ease-out);
  }
  .send svg {
    width: 15px;
    height: 15px;
  }
  /* 画板入口：发送键旁的弱化图标钮 */
  .board-btn {
    flex: none;
    width: 30px;
    height: 30px;
    border-radius: 9px;
    border: 1px solid var(--line);
    background: transparent;
    color: var(--muted);
    display: grid;
    place-items: center;
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out),
      border-color var(--dur-fast) var(--ease-out);
  }
  .board-btn:hover {
    background: var(--bg);
    color: var(--fg);
    border-color: var(--line-strong);
  }
  .board-btn svg {
    width: 15px;
    height: 15px;
  }
  /* 运行中的打断发送键：与 ⏹ 并列，弱化样式区分主操作 */
  .send.ghost {
    background: transparent;
    border: 1px solid var(--line-strong);
    color: var(--fg);
    width: 30px;
    height: 30px;
  }
  .send.ghost:disabled {
    opacity: 0.25;
  }
  .send:disabled {
    opacity: 0.2;
    cursor: default;
  }
  .stop {
    background: transparent;
    border: 1px solid var(--line-strong);
    color: var(--fg);
    animation: breathe 2.4s ease-in-out infinite;
  }
  .stop svg {
    width: 14px;
    height: 14px;
  }
  .stop:hover {
    background: var(--bg-invert);
    color: var(--fg-invert);
  }
  .meta {
    position: absolute;
    left: 6px;
    bottom: 4px;
    font-size: 10.5px;
    color: var(--faint);
    font-family: var(--font-mono);
    opacity: 0;
    transition: opacity var(--dur-fast) var(--ease-out);
  }
  .bar.focused .meta {
    opacity: 1;
  }
</style>
