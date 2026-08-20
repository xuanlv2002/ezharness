<script lang="ts">
  import { onMount } from 'svelte'

  let {
    files = [],
    onRemove,
    onEditImage,
    onOpenBoard,
  }: {
    files?: File[]
    onRemove?: (i: number) => void
    onEditImage?: (i: number) => void
    onOpenBoard?: () => void
  } = $props()

  /* 原型骨架：输入交互可用，发送逻辑待业务开发。 */
  let text = $state('')
  let focused = $state(false)
  let el: HTMLTextAreaElement | undefined = $state()

  const thumbs = $derived(
    files.map((f) => ({
      name: f.name,
      isImage: f.type.startsWith('image/'),
      url: f.type.startsWith('image/') ? URL.createObjectURL(f) : '',
    })),
  )

  onMount(() => {
    /* rows=1 的浏览器默认高度与行高不齐，挂载即校准为单行高 */
    if (el) {
      el.style.height = 'auto'
      el.style.height = el.scrollHeight + 'px'
    }
  })

  function send() {
    if (!text.trim()) return
    text = ''
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
      e.preventDefault()
      send()
    }
  }

  function autoResize(e: Event) {
    const t = e.currentTarget as HTMLTextAreaElement
    t.style.height = 'auto'
    t.style.height = Math.min(t.scrollHeight, 200) + 'px'
  }
</script>

<div class="bar" class:focused>
  <div class="fade"></div>
  {#if files.length}
    <div class="attachments">
      {#each thumbs as t, i (i)}
        <div class="att">
          {#if t.isImage}
            <button class="thumb" onclick={() => onEditImage?.(i)} title="打开魔法画板">
              <img src={t.url} alt={t.name} />
              <span class="edit-mark">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M17 3a2.85 2.85 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5L17 3z" />
                </svg>
              </span>
            </button>
          {:else}
            <span class="file-ico">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                <path d="M14 3H7a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1V7l-4-4z" />
                <path d="M14 3v4h4" />
              </svg>
            </span>
          {/if}
          <span class="att-name">{t.name}</span>
          <button class="att-x" onclick={() => onRemove?.(i)} title="移除">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
              <path d="M6 6l12 12M18 6L6 18" />
            </svg>
          </button>
        </div>
      {/each}
    </div>
  {/if}
  <div class="box">
    <textarea
      rows="1"
      bind:this={el}
      placeholder="发出指令…"
      bind:value={text}
      onfocus={() => (focused = true)}
      onblur={() => (focused = false)}
      onkeydown={onKey}
      oninput={autoResize}
    ></textarea>
    <button class="board-btn" onclick={() => onOpenBoard?.()} title="魔法画板">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
        <path d="M12 19l7-7 3 3-7 7-3-3z" />
        <path d="M18 13l-1.5-7.5L2 2l3.5 14.5L13 18l5-5z" />
        <path d="M2 2l7.586 7.586" />
        <circle cx="11" cy="11" r="2" />
      </svg>
    </button>
    <button class="send" onclick={send} disabled={!text.trim()} title="发送">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
        <path d="M12 19V5" />
        <path d="M5 12l7-7 7 7" />
      </svg>
    </button>
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
  .board-btn {
    flex: none;
    display: grid;
    place-items: center;
    width: 34px;
    height: 34px;
    border: none;
    background: transparent;
    border-radius: 10px;
    color: var(--faint);
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .board-btn svg {
    width: 16px;
    height: 16px;
  }
  .board-btn:hover {
    background: var(--line);
    color: var(--fg);
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
  .send {
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
  .send:disabled {
    opacity: 0.2;
    cursor: default;
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
