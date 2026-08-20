<script lang="ts">
  import Timeline from './Timeline.svelte'
  import InputBar from './InputBar.svelte'
  import StatusCard from './StatusCard.svelte'
  import NoticePanel from './NoticePanel.svelte'
  import MagicBoard from './MagicBoard.svelte'

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
    <NoticePanel
      notices={[]}
      onResolve={(_id, _action) => {
        /* 内联回传决策，待业务接入 */
      }}
      onJump={(_n) => {
        /* 滚动定位到时间线对应卡片（含 fork 卡内审批块），待业务接入 */
      }}
    />
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
  /* 主列：聊天记录 + 输入框，在扣除右列后的空间居中 */
  .main-col {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }
  /* 右侧列：状态卡 + 通知栏 */
  .side {
    flex: none;
    width: 232px;
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 16px 16px 16px 0;
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
