<script lang="ts">
  /*
  自绘下拉选择：触发按钮 + 浮层菜单。菜单 portal 到 body，fixed 坐标才
  相对视口——弹窗自身有 transform，内联 fixed 会被它的盒模型困住。
  回调页的筛选、会话/快应用目标与间隔单位共用它，保持同一套下拉观感。
  */

  interface SelectOption {
    value: string
    label: string
    hint?: string
    meta?: string
  }

  let {
    value = '',
    options = [],
    placeholder = '请选择',
    width = '100%',
    height = 34,
    disabled = false,
    title = '',
    onchange,
  }: {
    value?: string
    options?: SelectOption[]
    placeholder?: string
    width?: string
    height?: number
    disabled?: boolean
    title?: string
    onchange?: (value: string) => void
  } = $props()

  let open = $state(false)
  let trigger = $state<HTMLButtonElement>()
  let pos = $state({ left: 0, top: 0, width: 0, maxHeight: 280 })
  const selected = $derived(options.find((o) => o.value === value))

  /* portal：把浮层节点移到 body。 */
  function portal(node: HTMLElement) {
    document.body.appendChild(node)
    return { destroy: () => node.remove() }
  }

  function place() {
    if (!trigger) return
    const r = trigger.getBoundingClientRect()
    const widthPx = Math.max(r.width, 220)
    const left = Math.max(10, Math.min(r.left, window.innerWidth - widthPx - 10))
    const maxHeight = Math.min(300, Math.max(96, options.length * 42 + 10))
    let top = r.bottom + 4
    if (top + maxHeight > window.innerHeight - 10) {
      top = Math.max(10, r.top - maxHeight - 4)
    }
    pos = { left, top, width: widthPx, maxHeight }
  }

  function toggle() {
    if (disabled) return
    if (open) {
      open = false
      return
    }
    open = true
    place()
  }

  function choose(v: string) {
    open = false
    if (v !== value) onchange?.(v)
  }

  $effect(() => {
    if (!open) return
    const onMove = () => place()
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') open = false
    }
    window.addEventListener('scroll', onMove, true)
    window.addEventListener('resize', onMove)
    window.addEventListener('keydown', onKey)
    return () => {
      window.removeEventListener('scroll', onMove, true)
      window.removeEventListener('resize', onMove)
      window.removeEventListener('keydown', onKey)
    }
  })
</script>

<span class="root" class:disabled style:width>
  <button
    type="button"
    class="trigger"
    class:ph={!selected}
    style:height={`${height}px`}
    bind:this={trigger}
    {disabled}
    onclick={toggle}
    title={title || selected?.label || placeholder}
  >
    <span class="triglabel">{selected?.label ?? placeholder}</span>
    <svg class="chev" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
      <path d="M6 9l6 6 6-6" />
    </svg>
  </button>
</span>

{#if open}
  <div class="scrim" use:portal role="presentation" onclick={() => (open = false)}></div>
  <div
    class="menu"
    use:portal
    role="listbox"
    style:left={`${pos.left}px`}
    style:top={`${pos.top}px`}
    style:width={`${pos.width}px`}
    style:max-height={`${pos.maxHeight}px`}
  >
    {#each options as o (o.value)}
      <button
        type="button"
        class="option"
        class:sel={o.value === value}
        role="option"
        aria-selected={o.value === value}
        onclick={() => choose(o.value)}
      >
        <span class="check">{o.value === value ? '✓' : ''}</span>
        <span class="texts">
          <span class="optlabel">{o.label}</span>
          {#if o.hint}<span class="hint">{o.hint}</span>{/if}
        </span>
        {#if o.meta}<span class="meta">{o.meta}</span>{/if}
      </button>
    {/each}
  </div>
{/if}

<style>
  .root {
    display: inline-block;
    min-width: 0;
    vertical-align: middle;
  }
  .root.disabled {
    opacity: 0.55;
  }
  .trigger {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    min-width: 0;
    padding: 0 9px;
    border: 1px solid var(--line);
    border-radius: 8px;
    background: var(--bg);
    color: var(--fg);
    font: inherit;
    font-size: var(--font-size-ui);
    text-align: left;
    cursor: pointer;
  }
  .trigger:hover:not(:disabled) {
    border-color: var(--line-strong);
  }
  .trigger:focus-visible {
    outline: none;
    border-color: var(--accent);
  }
  .trigger.ph {
    color: var(--faint);
  }
  .trigger:disabled {
    cursor: default;
  }
  .triglabel {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .chev {
    flex: none;
    width: 13px;
    height: 13px;
    color: var(--faint);
  }
  .scrim {
    position: fixed;
    inset: 0;
    z-index: 78;
  }
  .menu {
    position: fixed;
    z-index: 79;
    padding: 4px;
    background: var(--bg);
    border: 1px solid var(--line-strong);
    border-radius: 10px;
    box-shadow:
      0 14px 36px rgb(0 0 0 / 16%),
      0 2px 8px rgb(0 0 0 / 6%);
    overflow-y: auto;
    animation: drop-in 130ms var(--ease-out) both;
  }
  @keyframes drop-in {
    from {
      opacity: 0;
      transform: translateY(-3px);
    }
  }
  .option {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 7px 8px;
    border-radius: 7px;
    color: var(--muted);
    font: inherit;
    font-size: var(--font-size-ui);
    text-align: left;
  }
  .option:hover {
    background: var(--bg-soft);
    color: var(--fg);
  }
  .option.sel {
    background: var(--accent-soft);
    color: var(--accent);
  }
  .check {
    flex: none;
    width: 12px;
    font-size: 11px;
    color: var(--accent);
  }
  .texts {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .optlabel {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: 500;
  }
  .hint {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .option.sel .hint {
    color: var(--accent);
    opacity: 0.72;
  }
  .meta {
    flex: none;
    font-family: var(--font-mono);
    font-size: 10px;
    color: var(--faint);
  }
</style>
