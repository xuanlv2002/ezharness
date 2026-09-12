<script lang="ts">
  import { onMount } from 'svelte'
  import Timeline from './Timeline.svelte'
  import InputBar from './InputBar.svelte'
  import StatusCard from './StatusCard.svelte'
  import NoticePanel from './NoticePanel.svelte'
  import ForkPanel from './ForkPanel.svelte'
  import BranchPanel from './BranchPanel.svelte'
  import { api } from '../lib/api'
  import { normFileKey } from '../lib/textfile'
  import { store, type Attachment, type FileRef } from '../lib/store.svelte'

  /* 附件（一切皆资源）：拖拽热区挂 window——抽屉/侧栏是 .chat 的兄弟
  节点，绑容器内时松手在抽屉上 drop 事件进不来（提示出现但无效）；
  dragenter/leave 计数防子元素抖动。拖入/粘贴先占位 chip（name 已知），
  path 由 /api/workspace/stash 落盘回填——chip 立即可点开（图片进
  画板、文本进资源页）。画板草稿是内存 file（发送时才 stash），真身
  图编辑写回原路径不换 chip。 */
  let attachments = $state<Attachment[]>([])
  let dragging = $state(false)
  let depth = 0

  onMount(() => {
    const onDragEnter = (e: DragEvent) => {
      if (!e.dataTransfer?.types.includes('Files')) return
      e.preventDefault()
      depth++
      dragging = true
    }
    const onDragLeave = () => {
      if (--depth <= 0) {
        depth = 0
        dragging = false
      }
    }
    const onDragOver = (e: DragEvent) => {
      if (!e.dataTransfer?.types.includes('Files')) return
      e.preventDefault() // 允许 drop（Wails 注入的监听只 preventDefault 它自己的）
    }
    const onDrop = (e: DragEvent) => {
      e.preventDefault()
      depth = 0
      dragging = false
      void addFiles([...(e.dataTransfer?.files ?? [])])
    }
    window.addEventListener('dragenter', onDragEnter)
    window.addEventListener('dragleave', onDragLeave)
    window.addEventListener('dragover', onDragOver)
    window.addEventListener('drop', onDrop)
    return () => {
      window.removeEventListener('dragenter', onDragEnter)
      window.removeEventListener('dragleave', onDragLeave)
      window.removeEventListener('dragover', onDragOver)
      window.removeEventListener('drop', onDrop)
    }
  })

  function removeAttachment(i: number) {
    attachments = attachments.filter((_, idx) => idx !== i)
  }

  /* 占位→暂存回填：批量回填按 [start, start+n) 区间对位（并发拖入
  批次互不干扰）；失败移除该批占位并提示 */
  async function addFiles(fs: File[]) {
    if (!fs.length) return
    const start = attachments.length
    attachments = [...attachments, ...fs.map((f) => ({ name: f.name, path: '' }))]
    try {
      const paths = await api.stash(fs)
      attachments = attachments.map((a, i) =>
        i >= start && i < start + fs.length ? { ...a, path: paths[i - start] ?? '' } : a,
      )
    } catch (e) {
      attachments = attachments.filter((_, i) => i < start || i >= start + fs.length)
      store.lastStatus = `附件暂存失败：${(e as Error).message}`
    }
  }

  function clearAttachments() {
    attachments = []
  }

  /* 待回流附件（查看器「添加到对话」/Wails 拖入）：草稿带 tag 时
  原位替换来源 chip（身份一致校验：file 引用相等），否则追加。本页
  未挂载时积压在 store，回对话页后首跑消费，跨页不丢。 */
  $effect(() => {
    const list = store.pendingAttachments
    if (!list?.length) return
    store.pendingAttachments = null
    let next = [...attachments]
    for (const p of list) {
      const i = /^\d+$/.test(p.tag ?? '') ? Number(p.tag) : -1
      const src = p.source
      if (i >= 0 && src && next[i]?.file === src) {
        next[i] = { name: p.name, path: p.path, file: p.file }
      } else {
        next = [...next, { name: p.name, path: p.path, file: p.file }]
      }
    }
    attachments = next
  })

  /* 文件引用 chips（资源页「添加到对话」回流）：同文件（规范化路径键）
  替换为最新标注态，避免陈旧重复；发送时统一进 <reference_file> 记录 */
  let fileRefs = $state<FileRef[]>([])

  $effect(() => {
    const r = store.pendingFileRef
    if (!r) return
    store.pendingFileRef = null
    const key = normFileKey(r.path)
    const i = fileRefs.findIndex((x) => normFileKey(x.path) === key)
    if (i >= 0) {
      const next = [...fileRefs]
      next[i] = r
      fileRefs = next
    } else {
      fileRefs = [...fileRefs, r]
    }
  })

  function removeRef(i: number) {
    fileRefs = fileRefs.filter((_, idx) => idx !== i)
  }

  function clearRefs() {
    fileRefs = []
  }

  /* 通知跳转主时间线锚点：jumpMain 置位后滚动到目标卡（跨分支切换后
  历史异步加载，tick 依赖让块到达后重试；找到即滚动并清空） */
  $effect(() => {
    if (!store.jumpMain) return
    store.tick
    const el = document.getElementById(store.jumpMain)
    if (el) {
      el.scrollIntoView({ behavior: 'smooth', block: 'center' })
      store.jumpMain = ''
    }
  })
</script>

<div class="chat" class:dragging>
  <aside class="side-left">
    <BranchPanel />
  </aside>
  <div class="main-col">
    <Timeline />
    <InputBar
      {attachments}
      fileRefs={fileRefs}
      onRemove={removeAttachment}
      onEditImage={(i) => {
        const a = attachments[i]
        if (a?.path) store.openFileAt(a.path)
        else if (a?.file) store.openDraftImage(a.file, String(i))
      }}
      onAddFiles={(fs) => void addFiles(fs)}
      onClearFiles={clearAttachments}
      onRemoveFileRef={removeRef}
      onOpenFileRef={(p) => store.openFileAt(p)}
      onClearFileRefs={clearRefs}
    />
  </div>
  <aside class="side">
    <StatusCard />
    <!-- 全局通知栏（数据跨分支轮询）：保持侧栏一列布局 -->
    <NoticePanel
      notices={store.notices}
      onResolve={(id, action, input) => {
        const n = store.notices.find((x) => x.id === id)
        if (n) void store.resolveNoticeGlobal(n, action, input)
      }}
      onJump={(n) => void store.jumpToNotice(n)}
      onDismiss={(id) => store.dismissNotice(id)}
    />
    <div class="entries">
      <!-- 工具入口：每类资源一个独立 mini 钮（开着且为该工具页时高亮） -->
      <button class="entry" class:active={store.termDrawerOpen && store.drawerTool === 'term'}
        onclick={() => store.toggleDrawerTool('term')} title="共享终端（用户与 AI 共写）">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <path d="M5 8l4 4-4 4" />
          <path d="M12 16.5h7" />
        </svg>
      </button>
      <button class="entry" class:active={store.termDrawerOpen && store.drawerTool === 'browser'}
        onclick={() => store.toggleDrawerTool('browser')} title="共享浏览器（AI 操控 · 镜像可接管）">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <circle cx="12" cy="12" r="9" />
          <path d="M3 12h18M12 3a15 15 0 0 1 0 18M12 3a15 15 0 0 0 0 18" />
        </svg>
      </button>
      <button class="entry" class:active={store.termDrawerOpen && store.drawerTool === 'file'}
        onclick={() => store.toggleDrawerTool('file')} title="资源（查看 · 编辑 · 发给 AI——文本/图片/网页等）">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" />
          <path d="M14 3v5h5" />
          <path d="M9 13h6M9 17h6" />
        </svg>
      </button>
    </div>
  </aside>
  <ForkPanel />
  {#if dragging}    <div class="dropzone">
      <div class="hint-box">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <path d="M12 16V5" />
          <path d="M7 10l5-5 5 5" />
          <path d="M4 19h16" />
        </svg>
        松开以添加附件
      </div>
    </div>
  {/if}
</div>

<style>
  .chat {
    position: relative;
    flex: 1;
    min-height: 0;
    display: flex;
  }
  /* 主列：聊天记录 + 输入框。最大宽对齐消息流/输入框自身的 780——
  收窄主列不缩内容（inner 本就 780 封顶），却能在侧栏与右侧悬浮列
  之间留出等宽留白；时间线滚动条贴内容右缘而非窗口最右 */
  .main-col {
    flex: 1;
    min-width: 0;
    max-width: 780px;
    margin: 0 auto;
    display: flex;
    flex-direction: column;
  }
  /* 左侧分支列：悬浮覆盖抽屉——展开时浮在内容之上（不占布局宽度，
  主列不动），收起只剩折叠小方块。容器点击穿透，卡片自身可交互 */
  .side-left {
    position: absolute;
    top: 16px;
    left: 16px;
    bottom: 30px;
    width: 240px;
    z-index: 5;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 10px;
    pointer-events: none;
  }
  .chat > .side-left > :global(*) {
    pointer-events: auto;
    box-shadow: 0 4px 16px rgb(0 0 0 / 8%);
  }
  .side-left > :global(.panel) {
    min-height: 0;
    width: 100%;
  }
  /* 右侧悬浮列：状态卡 + 通知栏浮在内容之上。容器点击穿透，
  卡片自身可交互；bottom 留出右下角 brand-foot 的位置 */
  .side {
    position: absolute;
    top: 16px;
    right: 16px;
    bottom: 30px;
    width: 260px;
    z-index: 5;
    display: flex;
    flex-direction: column;
    gap: 10px;
    pointer-events: none;
  }
  .chat > .side > :global(*) {
    pointer-events: auto;
  }
  /* 阴影只给卡片；entries 容器透明无背景，容器级阴影会把整组按钮
     连同间隙圈成一块白色长条，视觉上黏成一个控件 */
  .chat > .side > :global(*:not(.entries)) {
    box-shadow: 0 4px 16px rgb(0 0 0 / 8%);
  }
  .side > :global(.panel) {
    min-height: 0; /* 通知过多时收缩，列表内部滚动 */
  }
  /* 工具抽屉入口：右列通知下方，方形图标钮（与卡片同视觉语言）；
  当前工具页展开时高亮对应钮 */
  .entries {
    align-self: flex-end;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .entry {
    display: grid;
    place-items: center;
    width: 34px;
    height: 34px;
    border: 1px solid var(--line);
    background: var(--bg);
    color: var(--muted);
    border-radius: 10px;
    box-shadow: 0 4px 16px rgb(0 0 0 / 8%);
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out),
      border-color var(--dur-fast) var(--ease-out);
  }
  .entry:hover {
    background: var(--bg-soft);
    color: var(--fg);
    border-color: var(--line-strong);
  }
  .entry.active {
    border-color: var(--accent-soft, var(--accent));
    color: var(--accent, #2563eb);
    background: var(--accent-soft, color-mix(in srgb, #2563eb 8%, var(--bg)));
  }
  .entry svg {
    width: 16px;
    height: 16px;
  }
  .dropzone {
    position: absolute;
    inset: 10px;
    z-index: 10;
    display: grid;
    place-items: center;
    border: 2px dashed var(--accent);
    border-radius: 16px;
    background: color-mix(in srgb, var(--bg) 75%, transparent);
    backdrop-filter: blur(2px);
    pointer-events: none;
    animation: drop-in var(--dur-fast) var(--ease-out) both;
  }
  .hint-box {
    display: flex;
    align-items: center;
    gap: 10px;
    font-size: 14px;
    font-weight: 550;
    color: var(--accent);
    background: var(--bg);
    border: 1px solid var(--accent-soft);
    border-radius: 12px;
    padding: 12px 22px;
    box-shadow: 0 4px 16px rgb(0 0 0 / 8%);
  }
  .hint-box svg {
    width: 18px;
    height: 18px;
  }
  @keyframes drop-in {
    from {
      opacity: 0;
    }
    to {
      opacity: 1;
    }
  }
</style>
