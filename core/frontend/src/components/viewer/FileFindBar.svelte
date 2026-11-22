<script lang="ts">
  /*
  资源查看器·文件内查找条（Ctrl+F）：受控输入（query 由 ResourcePane 持有），
  计数与前后跳由父层协调到当前 viewer，只服务右侧当前文件。
  */

  let {
    query = '',
    count = 0,
    index = 0,
    onquery,
    onstep,
    onclose,
  }: {
    query?: string
    count?: number
    index?: number
    onquery: (q: string) => void
    onstep: (back: boolean) => void
    onclose: () => void
  } = $props()

  let inputEl = $state<HTMLInputElement>()

  export function focus(): void {
    inputEl?.focus()
    inputEl?.select()
  }

  function oninput(e: Event) {
    onquery((e.currentTarget as HTMLInputElement).value)
  }

  function onkey(e: KeyboardEvent) {
    if (e.isComposing) return // 中文输入法回车选词不是"下一个"
    if (e.key === 'Enter') {
      e.preventDefault()
      onstep(e.shiftKey)
    } else if (e.key === 'ArrowDown') {
      e.preventDefault()
      onstep(false)
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      onstep(true)
    } else if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      onclose()
    }
  }
</script>

<div class="findbar" onpointerdown={(e) => e.stopPropagation()}>
  <svg class="ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
    <circle cx="11" cy="11" r="7" />
    <path d="M20 20l-4-4" />
  </svg>
  <input
    bind:this={inputEl}
    value={query}
    oninput={oninput}
    onkeydown={onkey}
    placeholder="当前文件中查找…"
    spellcheck="false"
  />
  <span class="cnt">{query.trim() ? (count ? `${index + 1}/${count}` : '无结果') : ''}</span>
  <button class="nav" onclick={() => onstep(true)} disabled={!count} title="上一个（↑ / Shift+Enter）">↑</button>
  <button class="nav" onclick={() => onstep(false)} disabled={!count} title="下一个（↓ / Enter / F3）">↓</button>
  <button class="nav" onclick={onclose} title="关闭（Esc）">
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
      <path d="M6 6l12 12M18 6L6 18" />
    </svg>
  </button>
</div>

<style>
  .findbar {
    display: flex;
    align-items: center;
    gap: 6px;
    max-width: min(440px, 80%);
    padding: 5px 7px;
    background: var(--glass);
    backdrop-filter: var(--glass-blur);
    border: 1px solid var(--line);
    border-radius: 10px;
    box-shadow: 0 4px 16px rgb(0 0 0 / 10%);
    animation: float-in var(--dur-fast) var(--ease-out) both;
  }
  .ico {
    flex: none;
    width: 13px;
    height: 13px;
    color: var(--faint);
  }
  input {
    width: 150px;
    border: none;
    outline: none;
    background: transparent;
    font-family: inherit;
    font-size: var(--font-size-section);
    color: var(--fg);
  }
  input::placeholder {
    color: var(--faint);
  }
  .cnt {
    flex: none;
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    color: var(--muted);
    white-space: nowrap;
  }
  .nav {
    flex: none;
    display: grid;
    place-items: center;
    width: 22px;
    height: 22px;
    border: none;
    border-radius: 6px;
    background: transparent;
    color: var(--muted);
    font-size: var(--font-size-section);
    line-height: 1;
    cursor: pointer;
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .nav:hover:not(:disabled) {
    background: var(--line);
    color: var(--fg);
  }
  .nav:disabled {
    opacity: 0.35;
    cursor: default;
  }
  .nav svg {
    width: 11px;
    height: 11px;
  }
</style>
