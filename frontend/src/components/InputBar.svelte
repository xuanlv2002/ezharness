<script lang="ts">
  import { store } from '../lib/store.svelte'

  let text = $state('')

  function send() {
    const t = text.trim()
    if (!t || store.busy) return
    if (t === '/summary') {
      text = ''
      void store.summarize()
      return
    }
    text = ''
    void store.send(t)
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

<div class="bar">
  {#if store.lastStatus}
    <div class="status">{store.lastStatus}</div>
  {/if}
  <div class="row">
    <textarea
      rows="1"
      placeholder={store.busy ? '运行中… 可点击 ⏹ 取消' : '发出指令（Enter 发送 · Shift+Enter 换行 · /summary 摘要）'}
      bind:value={text}
      onkeydown={onKey}
      oninput={autoResize}
      disabled={!store.activeId}
    ></textarea>
    {#if store.busy}
      <button class="stop" onclick={() => void store.cancel()} title="取消当前轮">⏹</button>
    {:else}
      <button class="send" onclick={send} disabled={!text.trim()} title="发送">↑</button>
    {/if}
  </div>
</div>

<style>
  .bar {
    padding: 12px 48px 20px;
    max-width: 876px;
    width: 100%;
    margin: 0 auto;
  }
  .status {
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--faint);
    padding: 0 4px 8px;
    text-align: right;
  }
  .row {
    display: flex;
    gap: 10px;
    align-items: flex-end;
    border: 1px solid var(--line-strong);
    border-radius: 14px;
    padding: 10px 10px 10px 16px;
    background: var(--bg);
    transition: box-shadow var(--dur-fast) var(--ease-out);
  }
  .row:focus-within {
    box-shadow: 0 0 0 3px var(--accent-soft);
  }
  textarea {
    flex: 1;
    border: none;
    outline: none;
    resize: none;
    font-family: inherit;
    font-size: 15px;
    line-height: 1.6;
    max-height: 200px;
    background: transparent;
    color: var(--fg);
  }
  textarea::placeholder {
    color: var(--faint);
  }
  .send,
  .stop {
    flex: none;
    width: 36px;
    height: 36px;
    border-radius: 10px;
    border: none;
    display: grid;
    place-items: center;
    font-size: 16px;
    transition:
      background var(--dur-fast) var(--ease-out),
      opacity var(--dur-fast) var(--ease-out);
  }
  .send {
    background: var(--bg-invert);
    color: var(--fg-invert);
  }
  .send:disabled {
    opacity: 0.25;
    cursor: default;
  }
  .stop {
    border: 1px solid var(--line-strong);
    background: transparent;
    color: var(--fg);
    animation: breathe 2.4s ease-in-out infinite;
  }
  .stop:hover {
    background: var(--bg-invert);
    color: var(--fg-invert);
  }
</style>
