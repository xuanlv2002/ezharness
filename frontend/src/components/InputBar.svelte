<script lang="ts">
  import { onMount } from 'svelte'

  /* 原型骨架：输入交互可用，发送逻辑待业务开发。 */
  let text = $state('')
  let focused = $state(false)
  let el: HTMLTextAreaElement | undefined = $state()

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
  .box {
    display: flex;
    gap: 8px;
    align-items: flex-end;
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
