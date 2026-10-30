<script lang="ts">
  import { onDestroy, onMount, tick, untrack } from 'svelte'
  import { store, type Attachment, type FileRef } from '../lib/store.svelte'
  import { fileBaseName, isImagePath } from '../lib/textfile'
  import type { ReasoningEffort } from '../lib/api'
  import PlanPanel from './PlanPanel.svelte'
  import SessionStatus from './SessionStatus.svelte'

  let {
    attachments = [],
    fileRefs = [],
    onRemove,
    onEditImage,
    onAddFiles,
    onClearFiles,
    onRemoveFileRef,
    onOpenFileRef,
    onClearFileRefs,
  }: {
    attachments?: Attachment[]
    fileRefs?: FileRef[]
    onRemove?: (i: number) => void
    onEditImage?: (i: number) => void
    onAddFiles?: (fs: File[]) => void
    onClearFiles?: () => void
    onRemoveFileRef?: (i: number) => void
    onOpenFileRef?: (path: string) => void
    onClearFileRefs?: () => void
  } = $props()

  let text = $state('')
  let focused = $state(false)
  let el: HTMLTextAreaElement | undefined = $state()

  /* 每标签草稿：切换标签时换出者存草稿、换入者恢复（读 text 须 untrack，
  写回自身状态再读会成环）；销毁时也存——切视图页再回来输入不丢 */
  let lastTabId = ''
  $effect(() => {
    const id = store.activeId
    if (id === lastTabId) return
    if (lastTabId) store.drafts[lastTabId] = untrack(() => text)
    text = store.drafts[id] ?? ''
    lastTabId = id
    void tick().then(() => {
      if (el) {
        el.style.height = 'auto'
        el.style.height = el.scrollHeight + 'px'
      }
    })
  })
  onDestroy(() => {
    if (lastTabId) store.drafts[lastTabId] = text
  })

  /* 快速开始示例：填入输入框并聚焦（可改后再发），高度按内容重算 */
  $effect(() => {
    const p = store.pendingPrompt
    if (!p) return
    store.pendingPrompt = null
    text = p
    void tick().then(() => {
      if (!el) return
      el.focus()
      el.setSelectionRange(p.length, p.length)
      el.style.height = 'auto'
      el.style.height = Math.min(el.scrollHeight, 200) + 'px'
    })
  })

  /* 附件缩略：真身路径走文件服务源（与聊天记录同源，带版本参数——
  画板写回后强制刷新）；path 空 = 拖入暂存中占位 */
  const thumbnails = $derived(
    attachments.map((a) => {
      const isImage = isImagePath(a.name)
      const v = a.path ? (store.imgVer[a.path] ?? 0) : 0
      return {
        name: a.name,
        path: a.path,
        isImage,
        url: isImage && a.path ? `/api/workspace/file?path=${encodeURIComponent(a.path)}${v ? `&v=${v}` : ''}` : '',
        ready: !!a.path,
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
    if (!t && !attachments.length && !fileRefs.length) return
    if (store.archivingRootId === store.activeId) {
      store.lastStatus = '正在归档当前话题，完成后即可继续对话（可先切换分支）'
      return
    }
    if (attachments.some((a) => !a.path)) {
      store.lastStatus = '附件仍在暂存中，稍候再发送'
      return
    }
    if (attachments.length > MAX_FILES) {
      store.lastStatus = `附件最多 ${MAX_FILES} 个，多余的未发送`
    }
    const sendAttachments = attachments.slice(0, MAX_FILES)
    const sendFileRefs = fileRefs.slice(0, MAX_FILES)
    text = ''
    /* 内容清空后高度即时回落（autoResize 只挂 oninput，程序清值不触发） */
    void tick().then(() => {
      if (el) {
        el.style.height = 'auto'
        el.style.height = Math.min(el.scrollHeight, 200) + 'px'
      }
    })
    onClearFiles?.()
    onClearFileRefs?.()
    /* 附件进框时已落盘 tmp（只传路径引用）；fileRefs 统一进 <reference_file>
    记录。attachments/fileRefs 必须在 onClear* 之前拷贝——props 解构是
    live getter，清空后再取读到 [] */
    await store.send(t, sendAttachments, sendFileRefs)
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

  /* ── 输入框工具条：模型切换（下一次输入生效）+ 审批三档，均为下拉 ── */
  let modelOpen = $state(false)
  let approveOpen = $state(false)
  let reasonOpen = $state(false)
  /* 思考档位（默认/关闭 + Claude Code / Codex 五档；强度档下游归一为 low/high/max） */
  const REASON_MODES = [
    { v: '', label: '思考·默认', title: '不控制思考参数，用服务端默认行为' },
    { v: 'none', label: '思考·无（关闭）', title: '关闭思考（快问快答；不支持的服务端自动忽略）' },
    { v: 'low', label: '思考·low', title: '低强度（归一 low）' },
    { v: 'medium', label: '思考·medium', title: '中强度（归一 high）' },
    { v: 'high', label: '思考·high', title: '高强度（归一 high）' },
    { v: 'xhigh', label: '思考·xhigh', title: '很高（归一 high）' },
    { v: 'max', label: '思考·max', title: '最大（归一 max）' },
  ] as const
  const APPROVE_MODES = [
    { v: '', label: '跟随安全配置', title: '按安全页的工具规则决定是否审批' },
    { v: 'off', label: '无审批', title: '全部工具直接放行（谨慎）' },
    { v: 'strict', label: '严格审批', title: '每次工具调用都需批准' },
  ] as const
  const approveMode = $derived((store.settings?.approveMode ?? '') as '' | 'off' | 'strict')
  const approveLabel = $derived(APPROVE_MODES.find((m) => m.v === approveMode)?.label ?? '跟随安全配置')
  const reasonEffort = $derived((store.settings?.reasoningEffort ?? '') as ReasoningEffort)
  const reasonLabel = $derived(REASON_MODES.find((m) => m.v === reasonEffort)?.label ?? '思考·默认')

  function toggleModelMenu() {
    approveOpen = false
    reasonOpen = false
    modelOpen = !modelOpen
    if (modelOpen) void store.ensureModels()
  }

  function pickModel(name: string) {
    modelOpen = false
    void store.activateModel(name)
  }

  function toggleReasonMenu() {
    modelOpen = false
    approveOpen = false
    reasonOpen = !reasonOpen
  }

  function pickReason(v: ReasoningEffort) {
    reasonOpen = false
    void store.setReasoningEffort(v)
  }

  function toggleApproveMenu() {
    modelOpen = false
    reasonOpen = false
    approveOpen = !approveOpen
  }

  function pickApprove(mode: '' | 'off' | 'strict') {
    approveOpen = false
    void store.setApproveMode(mode)
  }

  /* 点击外部收起两个菜单 */
  function onGlobalDown(e: MouseEvent) {
    const t = e.target as HTMLElement
    if (modelOpen && !t.closest('.model-pick')) modelOpen = false
    if (approveOpen && !t.closest('.approve-pick')) approveOpen = false
    if (reasonOpen && !t.closest('.reason-pick')) reasonOpen = false
  }
  onMount(() => {
    document.addEventListener('mousedown', onGlobalDown)
    return () => document.removeEventListener('mousedown', onGlobalDown)
  })
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
  <PlanPanel />
  {#if attachments.length || fileRefs.length}
    <div class="attachments">
      {#each thumbnails as t, i (i)}
        <div class="att" class:pending={!t.ready}>
          {#if t.isImage}
            <button class="thumb" disabled={!t.ready} onclick={() => onEditImage?.(i)}
              title={t.ready ? '打开画板编辑（保存写回原文件）' : '暂存中…'}>
              {#if t.url}<img src={t.url} alt={t.name} />{:else}<span class="draft-ico"><svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M17 3a2.85 2.85 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5L17 3z" /></svg></span>{/if}
              <span class="edit-mark">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M17 3a2.85 2.85 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5L17 3z" />
                </svg>
              </span>
            </button>
          {:else}
            <button class="file-ico as-btn" disabled={!t.ready} onclick={() => t.path && store.openFileAt(t.path)}
              title={t.ready ? '在资源页打开（查看 · 编辑 · 发给 AI）' : '暂存中…'}>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                <path d="M14 3H7a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1V7l-4-4z" />
                <path d="M14 3v4h4" />
              </svg>
            </button>
          {/if}
          <span class="att-name">{t.name}</span>
          <button class="att-x" onclick={() => onRemove?.(i)} title="移除">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
              <path d="M6 6l12 12M18 6L6 18" />
            </svg>
          </button>
        </div>
      {/each}
      {#each fileRefs as r, i (i)}
        <button class="ref" onclick={() => onOpenFileRef?.(r.path)}
          title={`${r.path}（点击在文件页打开）`}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
            <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" />
            <path d="M14 3v5h5" />
          </svg>
          <span class="ref-name">{fileBaseName(r.path)}</span>
          {#if r.items.length}<span class="ref-n">{r.items.length} 条标注</span>{/if}
          <span class="att-x" onclick={(e) => { e.stopPropagation(); onRemoveFileRef?.(i) }} role="button" tabindex="-1" title="移除引用">
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
    <button class="board-btn" onclick={() => void store.openImageDraft()} title="画板（画图/标注，添加到对话；落盘在工作目录 tmp）">
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
      <button class="send ghost" onclick={() => void send()} disabled={!text.trim() && !attachments.length && !fileRefs.length} title="终止当前轮并发送新指令">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M12 19V5" />
          <path d="M5 12l7-5 5 5" />
        </svg>
      </button>
    {:else}
      <button class="send" onclick={() => void send()} disabled={!text.trim() && !attachments.length && !fileRefs.length} title="发送">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M12 19V5" />
          <path d="M5 12l7-5 5 5" />
        </svg>
      </button>
    {/if}
  </div>
  <div class="tools">
    <!-- 工作区工具入口：终端/浏览器/资源（开着且为该页、或已弹出独立窗口时高亮） -->
    <div class="ws-entries">
      <button class="ws-btn"
        class:active={(store.termDrawerOpen && store.drawerTool === 'term') || store.popoutTools.includes('term')}
        onclick={() => store.toggleDrawerTool('term')} title="共享终端（用户与 AI 共写）">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <path d="M5 8l4 4-4 4" />
          <path d="M12 16.5h7" />
        </svg>
      </button>
      {#if (window as any).ez?.browser}
        <button class="ws-btn"
          class:active={(store.termDrawerOpen && store.drawerTool === 'browser') || store.popoutTools.includes('browser')}
          onclick={() => store.toggleDrawerTool('browser')} title="共享浏览器（AI 操控 · 实时共见）">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
            <circle cx="12" cy="12" r="9" />
            <path d="M3 12h18M12 3a15 15 0 0 1 0 18M12 3a15 15 0 0 0 0 18" />
          </svg>
        </button>
      {/if}
      <button class="ws-btn"
        class:active={(store.termDrawerOpen && store.drawerTool === 'file') || store.popoutTools.includes('file')}
        onclick={() => store.toggleDrawerTool('file')} title="资源（查看 · 编辑 · 发给 AI——文本/图片/网页等）">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" />
          <path d="M14 3v5h5" />
          <path d="M9 13h6M9 17h6" />
        </svg>
      </button>
    </div>
    <div class="reason-pick">
      <button class="model-btn" onclick={toggleReasonMenu} disabled={!store.activeId}
        title="思考强度（全局，即改即生效）">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <path d="M9 18h6M10 22h4" />
          <path d="M12 2a7 7 0 0 0-4 12.7V16h8v-1.3A7 7 0 0 0 12 2z" />
        </svg>
        <span class="model-name full">{reasonLabel}</span>
        <span class="model-name short">思考</span>
      </button>
      {#if reasonOpen}
        <div class="model-menu">
          {#each REASON_MODES as m (m.v || 'default')}
            <button class="model-item" class:on={reasonEffort === m.v} onclick={() => pickReason(m.v)} title={m.title}>
              <span class="model-name">{m.label}</span>
            </button>
          {/each}
        </div>
      {/if}
    </div>
    <div class="approve-pick">
      <button class="model-btn" onclick={toggleApproveMenu} disabled={!store.activeId}
        title="全局的工具审批档位（实时生效）">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <path d="M12 3l7 3v5c0 4.5-3 8-7 10-4-2-7-5.5-7-10V6z" />
        </svg>
        <span class="model-name full">{approveLabel}</span>
        <span class="model-name short">审批</span>
      </button>
      {#if approveOpen}
        <div class="model-menu">
          {#each APPROVE_MODES as m (m.v || 'follow')}
            <button class="model-item" class:on={approveMode === m.v} onclick={() => pickApprove(m.v)} title={m.title}>
              <span class="model-name">{m.label}</span>
            </button>
          {/each}
        </div>
      {/if}
    </div>
    <div class="model-pick">
      <button class="model-btn" onclick={toggleModelMenu} disabled={!store.activeId}
        title="切换主模型（下一次输入生效）">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <rect x="7" y="7" width="10" height="10" rx="2" />
          <path d="M3 9v6M21 9v6" />
        </svg>
        <span class="model-name">{store.status?.model || '未配置'}</span>
        {#if store.status?.modelVision}<i class="vis" title="支持视觉输入"><svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7-10-7-10-7z" /><circle cx="12" cy="12" r="3" /></svg></i>{/if}
      </button>
      {#if modelOpen}
        <div class="model-menu">
          {#if !store.mainModels.length}
            <p class="model-empty">main 槽暂无模型（设置 · 模型添加）</p>
          {:else}
            {#each store.mainModels as m (m.name)}
              <button class="model-item" class:on={m.name === store.status?.model} onclick={() => pickModel(m.name)}>
                <span class="model-name">{m.name}</span>
                {#if m.vision}<i class="vis" title="支持视觉输入"><svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7-10-7-10-7z" /><circle cx="12" cy="12" r="3" /></svg></i>{:else}<i class="vis off" title="不支持视觉输入"><svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7-10-7-10-7z" /><circle cx="12" cy="12" r="3" /></svg></i>{/if}
              </button>
            {/each}
          {/if}
        </div>
      {/if}
    </div>
    <!-- 会话级运行状态（全局通知在顶栏） -->
    <SessionStatus />
  </div>
  <p class="meta">Enter 发送 · Shift+Enter 换行</p>
</div>

<style>
  .bar {
    flex: none;
    position: relative;
    max-width: 780px;
    /* 左右各留 58：与时间线内容列对齐（48 头像位 + 10 间隙） */
    width: calc(100% - 116px);
    margin: 0 auto;
    padding-bottom: 22px;
  }
  /* 聊天记录滚到输入区后方的渐隐过渡 */
  .fade {
    position: absolute;
    left: -58px;
    right: -58px;
    bottom: 100%;
    height: 36px;
    background: linear-gradient(to top, color-mix(in srgb, var(--bg) 85%, transparent), transparent);
    pointer-events: none;
  }
  .status {
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
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
    font-size: var(--font-size-content);
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
    font-size: var(--font-size-ui);
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
    font-size: var(--font-size-small);
    color: var(--accent, #2563eb);
  }
  .att-name {
    font-size: var(--font-size-ui);
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
    background: var(--glass);
    backdrop-filter: var(--glass-blur);
    border-radius: 16px;
    padding: 12px 12px 12px 18px;
    transition:
      border-color var(--dur-fast) var(--ease-out),
      box-shadow var(--dur-fast) var(--ease-out),
      background var(--dur-fast) var(--ease-out);
  }
  .bar.focused .box {
    border-color: color-mix(in srgb, var(--accent) 45%, var(--line));
    background: color-mix(in srgb, var(--bg) 88%, transparent);
    box-shadow: 0 2px 12px rgb(0 0 0 / 6%);
  }
  textarea {
    flex: 1;
    border: none;
    outline: none;
    resize: none;
    font-family: inherit;
    font-size: var(--font-size-content);
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
  /* ── 输入框下工具条：审批档 + 模型切换（均为下拉，右对齐并排） ── */
  .tools {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    /* 始终单行：窄列下靠收缩模型名与压缩间距收住，不换行 */
    flex-wrap: nowrap;
    gap: 6px;
    padding: 6px 6px 0;
    /* 容器查询：窄列下压缩设置按钮（见下） */
    container-type: inline-size;
  }
  /* 窄列：思考/审批收成短标签，保证工具行单行且仍可辨认 */
  .model-name.short { display: none; }
  @container (max-width: 500px) {
    .reason-pick .model-btn .model-name.full,
    .approve-pick .model-btn .model-name.full {
      display: none;
    }
    .reason-pick .model-btn .model-name.short,
    .approve-pick .model-btn .model-name.short {
      display: inline;
    }
    /* 窄列再压一档按钮内边距，保证整行放得下 */
    .model-btn {
      gap: 4px;
      padding: 4px 7px;
    }
    .ws-btn {
      width: 26px;
    }
  }
  /* 工具入口靠左、设置类按钮靠右 */
  .ws-entries {
    margin-right: auto;
    display: flex;
    align-items: center;
    gap: 2px;
  }
  .ws-btn {
    display: grid;
    place-items: center;
    width: 28px;
    height: 28px;
    border-radius: 7px;
    color: var(--muted);
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .ws-btn:hover {
    background: var(--bg-soft);
    color: var(--fg);
  }
  .ws-btn.active {
    color: var(--accent);
    background: var(--accent-soft);
  }
  .ws-btn svg {
    width: 15px;
    height: 15px;
  }
  .approve-pick,
  .reason-pick,
  .model-pick {
    position: relative;
  }
  .ws-entries,
  .approve-pick,
  .reason-pick,
  .status-pick {
    flex: none;
  }
  /* 模型按钮是可压缩项：窄列下模型名省略，其余按钮不动 */
  .model-pick {
    min-width: 0;
    max-width: 280px;
  }
  .model-pick .model-btn {
    max-width: 100%;
  }
  .model-btn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    border: 1px solid var(--line);
    border-radius: 9px;
    background: var(--glass);
    color: var(--muted);
    font-size: var(--font-size-small);
    padding: 4px 9px;
    max-width: 280px;
    transition:
      border-color var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .model-btn:hover:not(:disabled) {
    border-color: var(--line-strong);
    color: var(--fg);
  }
  .model-btn:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .model-btn svg {
    width: 12px;
    height: 12px;
    flex: none;
  }
  .model-name {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .vis {
    font-style: normal;
    display: inline-grid;
    place-items: center;
    color: var(--muted);
    flex: none;
  }
  .vis.off {
    opacity: 0.3;
  }
  /* 菜单向上弹（输入条贴底） */
  .model-menu {
    position: absolute;
    right: 0;
    bottom: calc(100% + 6px);
    min-width: 220px;
    max-width: 340px;
    max-height: 260px;
    overflow-y: auto;
    background: var(--bg);
    border: 1px solid var(--line-strong);
    border-radius: 12px;
    box-shadow: 0 10px 32px rgb(0 0 0 / 16%);
    padding: 5px;
    z-index: 30;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .model-item {
    display: flex;
    align-items: center;
    gap: 8px;
    border: none;
    background: transparent;
    color: var(--muted);
    font-size: var(--font-size-small);
    font-family: var(--font-mono);
    text-align: left;
    padding: 7px 9px;
    border-radius: 8px;
    cursor: pointer;
  }
  .model-item:hover {
    background: var(--bg-soft);
    color: var(--fg);
  }
  .model-item.on {
    color: var(--accent);
    background: color-mix(in srgb, var(--accent) 8%, transparent);
  }
  .model-item .model-name {
    flex: 1;
  }
  .model-empty {
    font-size: var(--font-size-small);
    color: var(--faint);
    margin: 0;
    padding: 8px 9px;
  }
  .meta {
    position: absolute;
    left: 6px;
    bottom: 4px;
    font-size: var(--font-size-small);
    color: var(--faint);
    font-family: var(--font-mono);
    opacity: 0;
    transition: opacity var(--dur-fast) var(--ease-out);
  }
  .bar.focused .meta {
    opacity: 1;
  }
</style>
