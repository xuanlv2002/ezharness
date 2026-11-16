<script lang="ts">
  import { api, type McpServerView, type MemorySkillEntry } from '../lib/api'
  import { store } from '../lib/store.svelte'

  /* 标题栏搜索：技能 / MCP / 设置三类通查。技能与 MCP 首次聚焦时拉取，
     设置项为静态清单；选中后写 store.jumpAnchor 供目标页滚动定位。 */
  let { onNavigate }: { onNavigate: (v: string) => void } = $props()

  type Kind = 'skill' | 'mcp' | 'settings'
  interface Item {
    kind: Kind
    label: string
    hint: string
    view: string
    anchor: string
  }

  const SETTINGS: { label: string; hint: string; anchor: string }[] = [
    { label: '服务端配置', hint: '监听、端口、数据目录、API 令牌', anchor: 'server' },
    { label: '上下文管理', hint: '整理水位、迭代次数、卸载阈值', anchor: 'context' },
    { label: '工作目录', hint: 'terminal 默认执行目录', anchor: 'workdir' },
    { label: '桌面窗口', hint: '托盘常驻、桌面悬浮球', anchor: 'window' },
    { label: '调试', hint: '模型输入日志', anchor: 'debug' },
    { label: '数据文件位置', hint: '数据目录与配置文件路径', anchor: 'paths' },
  ]

  const GROUP_TITLE: Record<Kind, string> = { skill: '技能', mcp: 'MCP', settings: '设置' }

  let q = $state('')
  let open = $state(false)
  let active = $state(0)
  let inputEl = $state<HTMLInputElement | null>(null)

  let skills = $state<MemorySkillEntry[]>([])
  let servers = $state<McpServerView[]>([])
  let loaded = $state(false)
  let loading = $state(false)

  async function load() {
    if (loaded || loading) return
    loading = true
    try {
      const [m, s] = await Promise.allSettled([api.getMemoryConfig(), api.getMcp()])
      if (m.status === 'fulfilled') skills = m.value.skills.items
      if (s.status === 'fulfilled') servers = s.value.servers ?? []
      loaded = true
    } finally {
      loading = false
    }
  }

  const all = $derived<Item[]>([
    ...skills.map((s) => ({ kind: 'skill' as const, label: s.name, hint: s.desc, view: 'memory', anchor: 'skills' })),
    ...servers.map((s) => ({
      kind: 'mcp' as const,
      label: s.name,
      hint: s.description || s.transport,
      view: 'mcp',
      anchor: s.name,
    })),
    ...SETTINGS.map((s) => ({ kind: 'settings' as const, label: s.label, hint: s.hint, view: 'settings', anchor: s.anchor })),
  ])

  const hits = $derived.by(() => {
    const k = q.trim().toLowerCase()
    if (!k) return all
    return all.filter((i) => `${i.label} ${i.hint}`.toLowerCase().includes(k))
  })

  const groups = $derived.by(() => {
    const out: { kind: Kind; items: Item[] }[] = []
    for (const kind of ['skill', 'mcp', 'settings'] as const) {
      const items = hits.filter((i) => i.kind === kind)
      if (items.length) out.push({ kind, items })
    }
    return out
  })

  const flat = $derived(groups.flatMap((g) => g.items))

  $effect(() => {
    if (active > flat.length - 1) active = Math.max(0, flat.length - 1)
  })

  function reset() {
    active = 0
  }

  function choose(it: Item) {
    store.jumpAnchor = `${it.view}:${it.anchor}`
    onNavigate(it.view)
    q = ''
    open = false
    inputEl?.blur()
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      open = true
      active = Math.min(active + 1, flat.length - 1)
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      active = Math.max(active - 1, 0)
    } else if (e.key === 'Enter') {
      if (flat[active]) choose(flat[active])
    } else if (e.key === 'Escape') {
      open = false
      inputEl?.blur()
    }
  }

  function focusInput() {
    inputEl?.focus()
    inputEl?.select()
  }
</script>

<svelte:window
  onpointerdown={(e) => {
    if (open && !(e.target as HTMLElement).closest('.topsearch')) open = false
  }}
  onkeydown={(e) => {
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
      e.preventDefault()
      focusInput()
    }
  }}
/>

<div class="topsearch">
  <label class="box">
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
      <circle cx="11" cy="11" r="7" />
      <path d="M20 20l-4-4" />
    </svg>
    <input
      bind:this={inputEl}
      bind:value={q}
      placeholder="搜索技能、MCP、设置"
      spellcheck="false"
      autocomplete="off"
      oninput={() => {
        open = true
        reset()
      }}
      onfocus={() => {
        open = true
        void load()
      }}
      onkeydown={onKey}
    />
    {#if q}
      <button
        class="clear"
        title="清空"
        onclick={() => {
          q = ''
          reset()
          focusInput()
        }}
      >
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round">
          <path d="M6 6l12 12M18 6L6 18" />
        </svg>
      </button>
    {:else}
      <kbd>Ctrl K</kbd>
    {/if}
  </label>

  {#if open}
    <div class="panel">
      {#if !flat.length}
        <p class="empty">{loading ? '加载中…' : '没有匹配项'}</p>
      {:else}
        {#each groups as g (g.kind)}
          <p class="gtitle">{GROUP_TITLE[g.kind]}</p>
          {#each g.items as it}
            <button
              class="row"
              class:active={flat[active] === it}
              onclick={() => choose(it)}
              onpointerenter={() => (active = flat.indexOf(it))}
            >
              <span class="ico" class:skill={it.kind === 'skill'} class:mcp={it.kind === 'mcp'}>
                {#if it.kind === 'skill'}
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linejoin="round">
                    <path d="M13 2L4.5 13h6L11 22l8.5-11h-6L13 2z" />
                  </svg>
                {:else if it.kind === 'mcp'}
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9">
                    <circle cx="5" cy="12" r="2" /><circle cx="19" cy="5" r="2" /><circle cx="19" cy="19" r="2" />
                    <path d="M7 12h4M11 12l6-6M11 12l6 6" />
                  </svg>
                {:else}
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round">
                    <path d="M4 7h9M17 7h3M4 17h5M13 17h7" />
                    <circle cx="15" cy="7" r="2" /><circle cx="11" cy="17" r="2" />
                  </svg>
                {/if}
              </span>
              <span class="txt">
                <b>{it.label}</b>
                {#if it.hint}<i>{it.hint}</i>{/if}
              </span>
            </button>
          {/each}
        {/each}
      {/if}
    </div>
  {/if}
</div>

<style>
  .topsearch {
    position: relative;
    width: 320px;
    app-region: no-drag;
  }
  .box {
    display: flex;
    align-items: center;
    gap: 6px;
    height: 25px;
    padding: 0 7px;
    border-radius: 7px;
    background: var(--bg);
    color: var(--faint);
    box-shadow: inset 0 0 0 1px var(--line);
    transition:
      box-shadow var(--dur-fast) var(--ease-out),
      background var(--dur-fast) var(--ease-out);
  }
  .box:focus-within {
    box-shadow:
      inset 0 0 0 1px color-mix(in srgb, var(--accent) 42%, transparent),
      0 0 0 3px color-mix(in srgb, var(--accent) 10%, transparent);
  }
  .box svg {
    flex: none;
    width: 12px;
    height: 12px;
  }
  .box input {
    flex: 1;
    min-width: 0;
    border: none;
    outline: none;
    background: transparent;
    font-family: inherit;
    font-size: var(--font-size-ui);
    color: var(--fg);
  }
  .box input::placeholder {
    color: var(--faint);
  }
  .box kbd {
    flex: none;
    padding: 0 4px;
    border: 1px solid var(--line);
    border-radius: 4px;
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    line-height: 16px;
    color: var(--faint);
  }
  .clear {
    flex: none;
    display: grid;
    place-items: center;
    width: 15px;
    height: 15px;
    border-radius: 4px;
    color: var(--faint);
  }
  .clear:hover {
    background: var(--bg-soft);
    color: var(--fg);
  }
  .clear svg {
    width: 9px;
    height: 9px;
  }
  .panel {
    position: absolute;
    top: calc(100% + 6px);
    left: 50%;
    margin-left: -200px;
    z-index: 120;
    width: 400px;
    max-height: 380px;
    overflow-y: auto;
    padding: 6px;
    border: 1px solid var(--line);
    border-radius: 12px;
    background: var(--bg);
    box-shadow: 0 16px 44px rgb(0 0 0 / 13%);
    animation: float-in var(--dur-fast) var(--ease-out) both;
    app-region: no-drag;
  }
  .gtitle {
    padding: 6px 8px 3px;
    font-size: var(--font-size-small);
    font-weight: 600;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--faint);
  }
  .row {
    display: flex;
    align-items: center;
    gap: 9px;
    width: 100%;
    padding: 6px 8px;
    border-radius: 8px;
    text-align: left;
    color: var(--fg);
  }
  .row.active {
    background: var(--bg-soft);
  }
  .ico {
    flex: none;
    display: grid;
    place-items: center;
    width: 22px;
    height: 22px;
    border-radius: 6px;
    background: var(--bg-soft);
    color: var(--muted);
  }
  .row.active .ico {
    background: var(--bg);
  }
  .ico.skill {
    color: #b7791f;
  }
  .ico.mcp {
    color: var(--accent);
  }
  .ico svg {
    width: 13px;
    height: 13px;
  }
  .txt {
    display: flex;
    flex-direction: column;
    min-width: 0;
    line-height: 1.35;
  }
  .txt b {
    font-size: var(--font-size-ui);
    font-weight: 600;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .txt i {
    font-style: normal;
    font-size: var(--font-size-small);
    color: var(--faint);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .empty {
    padding: 18px 8px;
    text-align: center;
    font-size: var(--font-size-ui);
    color: var(--faint);
  }
</style>
