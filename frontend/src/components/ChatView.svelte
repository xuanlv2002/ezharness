<script lang="ts">
  import Timeline from './Timeline.svelte'
  import InputBar from './InputBar.svelte'
  import StatusCard from './StatusCard.svelte'
  import NoticePanel from './NoticePanel.svelte'
  import MagicBoard from './MagicBoard.svelte'
  import { store } from '../lib/store.svelte'
  import type { Notice } from './NoticePanel.svelte'

  /* 拖拽附件：整个对话页是热区（dragenter/leave 计数防子元素抖动）。
  发送逻辑待业务开发。 */
  let files = $state<File[]>([])
  let dragging = $state(false)
  let depth = 0

  /* 魔法画板：editing 为编辑中的附件下标，null = 空白创作 */
  let boardOpen = $state(false)
  let editing: number | null = $state(null)

  function onDragEnter(e: DragEvent) {
    if (!e.dataTransfer?.types.includes('Files')) return
    e.preventDefault()
    depth++
    dragging = true
  }

  function onDragLeave() {
    if (--depth <= 0) {
      depth = 0
      dragging = false
    }
  }

  function onDragOver(e: DragEvent) {
    e.preventDefault() // 允许 drop
  }

  function onDrop(e: DragEvent) {
    e.preventDefault()
    depth = 0
    dragging = false
    for (const f of e.dataTransfer?.files ?? []) {
      files = [...files, f]
    }
  }

  function removeFile(i: number) {
    files = files.filter((_, idx) => idx !== i)
  }

  function openBoard(i: number | null) {
    editing = i
    boardOpen = true
  }

  function boardDone(f: File) {
    if (editing !== null) {
      const next = [...files]
      next[editing] = f
      files = next
    } else {
      files = [...files, f]
    }
    boardOpen = false
  }

  /* 通知内联操作 → 决策回传（联动时间线卡与通知状态） */
  function resolveNotice(id: string, action: string, input?: string) {
    const n = store.notices.find((x) => x.id === id)
    const block = store.blocks.find((b) => b.kind === 'decision' && b.id === id)
    if (!n || !block || block.kind !== 'decision') return
    if (n.kind === 'approve') {
      void store.decideApprove(block, action === 'approve', '')
    } else if (n.kind === 'ask') {
      void store.decideAnswer(block, input ?? '')
    } else if (n.kind === 'plan') {
      void store.decidePlan(block, action === 'execute' ? 'execute' : 'reject', input ?? '')
    }
  }

  function jumpToNotice(n: Notice) {
    if (!n.target) return
    document.getElementById(n.target)?.scrollIntoView({ behavior: 'smooth', block: 'center' })
  }
</script>

<div
  class="chat"
  class:dragging
  ondragenter={onDragEnter}
  ondragleave={onDragLeave}
  ondragover={onDragOver}
  ondrop={onDrop}
>
  <div class="main-col">
    <Timeline />
    <InputBar
      {files}
      onRemove={removeFile}
      onEditImage={(i) => openBoard(i)}
      onOpenBoard={() => openBoard(null)}
    />
  </div>
  <aside class="side">
    <StatusCard />
    <NoticePanel notices={store.notices} onResolve={resolveNotice} onJump={jumpToNotice} />
  </aside>
  {#if dragging}
    <div class="dropzone">
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

{#if boardOpen}
  <MagicBoard source={editing !== null ? (files[editing] ?? null) : null} onDone={boardDone} onClose={() => (boardOpen = false)} />
{/if}

<style>
  .chat {
    position: relative;
    flex: 1;
    min-height: 0;
    display: flex;
  }
  /* 主列：聊天记录 + 输入框。右侧悬浮列不占布局宽度，内容在整体
  偏左的区域居中（留出右侧给悬浮卡，窄屏时允许少量重叠） */
  .main-col {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    padding-right: 128px;
  }
  /* 右侧悬浮列：状态卡 + 通知栏浮在内容之上。容器点击穿透，
  卡片自身可交互；bottom 留出右下角 brand-foot 的位置 */
  .side {
    position: absolute;
    top: 16px;
    right: 16px;
    bottom: 30px;
    width: 232px;
    z-index: 5;
    display: flex;
    flex-direction: column;
    gap: 10px;
    pointer-events: none;
  }
  .chat > .side > :global(*) {
    pointer-events: auto;
    box-shadow: 0 4px 16px rgb(0 0 0 / 8%);
  }
  .side > :global(.panel) {
    min-height: 0; /* 通知过多时收缩，列表内部滚动 */
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
