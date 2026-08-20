<script lang="ts">
  import Logo from './Logo.svelte'

  type View = 'chat' | 'models' | 'memory' | 'knowledge' | 'tools' | 'mcp' | 'security' | 'settings'

  let {
    view,
    expanded,
    onNavigate,
    onToggle,
  }: {
    view: View
    expanded: boolean
    onNavigate: (v: View) => void
    onToggle: () => void
  } = $props()

  const items: { key: View; label: string; icon: string }[] = [
    {
      key: 'chat',
      label: '对话',
      icon: '<path d="M21 11.5a8.5 8.5 0 0 1-8.5 8.5c-1.3 0-2.6-.25-3.7-.7L3 21l1.7-4.3A8.5 8.5 0 1 1 21 11.5z"/>',
    },
    {
      key: 'models',
      label: '模型',
      icon: '<rect x="7" y="7" width="10" height="10" rx="2"/><path d="M12 2v3M12 19v3M2 12h3M19 12h3M5 5l2 2M17 17l2 2M19 5l-2 2M7 17l-2 2"/>',
    },
    {
      key: 'memory',
      label: '记忆',
      icon: '<path d="M12 6.5C10.5 5 8.5 4.5 4.5 4.5v13c4 0 6 .5 7.5 2 1.5-1.5 3.5-2 7.5-2v-13c-4 0-6 .5-7.5 2z"/><path d="M12 6.5v13"/>',
    },
    {
      key: 'knowledge',
      label: '知识库',
      icon: '<rect x="3" y="4" width="18" height="4" rx="1"/><path d="M5 8v11a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V8"/><path d="M10 12h4"/>',
    },
    {
      key: 'tools',
      label: '快应用',
      icon: '<path d="M13 2L4.5 13h6L11 22l8.5-11h-6L13 2z"/>',
    },
    {
      key: 'mcp',
      label: 'MCP',
      icon: '<circle cx="5" cy="12" r="2"/><circle cx="19" cy="5" r="2"/><circle cx="19" cy="19" r="2"/><path d="M7 12h4M11 12l6-6M11 12l6 6"/>',
    },
    {
      key: 'security',
      label: '安全',
      icon: '<path d="M12 3l7 3v5c0 4.5-3 8-7 10-4-2-7-5.5-7-10V6l7-3z"/>',
    },
    {
      key: 'settings',
      label: '设置',
      icon: '<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09a1.65 1.65 0 0 0-1-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09a1.65 1.65 0 0 0 1.51-1 1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33h.01a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51h.01a1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82v.01a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/>',
    },
  ]

  /* 工作面（对话 + agent 产出的快应用）与 harness 配置分两组 */
  const work = items.filter((i) => i.key === 'chat' || i.key === 'tools')
  const config = items.filter((i) => !work.includes(i) && i.key !== 'settings')
</script>

<nav class="menu" class:expanded>
  <div class="logo" title="ezharness">
    <Logo size={32} />
    <span class="brand">ezharness</span>
  </div>

  {#each work as it (it.key)}
    <button
      class="nav"
      class:active={view === it.key}
      onclick={() => onNavigate(it.key)}
      title={it.label}
    >
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
        {@html it.icon}
      </svg>
      <span class="label">{it.label}</span>
    </button>
  {/each}

  <div class="group-sep"></div>
  <span class="group-title">harness</span>

  {#each config as it (it.key)}
    <button
      class="nav"
      class:active={view === it.key}
      onclick={() => onNavigate(it.key)}
      title={it.label}
    >
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
        {@html it.icon}
      </svg>
      <span class="label">{it.label}</span>
    </button>
  {/each}

  <div class="spacer"></div>

  <button
    class="nav"
    class:active={view === 'settings'}
    onclick={() => onNavigate('settings')}
    title="设置"
  >
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
      {@html items[7].icon}
    </svg>
    <span class="label">设置</span>
  </button>

  <div class="sep"></div>
  <button class="nav collapse" onclick={onToggle} title={expanded ? '收起菜单' : '展开菜单'}>
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
      <path d={expanded ? 'M15 5l-7 7 7 7' : 'M9 5l7 7-7 7'} />
    </svg>
    <span class="label">{expanded ? '收起' : '展开'}</span>
  </button>
</nav>

<style>
  .menu {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 6px;
    padding: 14px 10px;
    background: var(--bg-soft);
    border-right: 1px solid var(--line);
    overflow: hidden;
  }
  .menu.expanded {
    align-items: stretch;
  }
  .logo {
    display: flex;
    align-items: center;
    gap: 0;
    height: 36px;
    margin-bottom: 8px;
    padding: 0 3px;
    transition: gap var(--dur-in) var(--ease-out);
  }
  .menu.expanded .logo {
    gap: 10px;
  }
  .brand {
    font-family: var(--font-mono);
    font-size: 13.5px;
    font-weight: 700;
    color: var(--fg);
    white-space: nowrap;
    max-width: 0;
    opacity: 0;
    overflow: hidden;
    transition:
      max-width var(--dur-in) var(--ease-out),
      opacity var(--dur-fast) var(--ease-out);
  }
  .menu.expanded .brand {
    max-width: 120px;
    opacity: 1;
  }
  .nav {
    display: flex;
    align-items: center;
    gap: 0;
    height: 36px;
    padding: 0 7px;
    border: none;
    background: transparent;
    border-radius: 9px;
    color: var(--muted);
    white-space: nowrap;
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out),
      gap var(--dur-in) var(--ease-out);
  }
  .menu.expanded .nav {
    padding: 0 9px;
  }
  .nav svg {
    flex: none;
    width: 18px;
    height: 18px;
  }
  .nav:hover {
    background: var(--line);
    color: var(--fg);
  }
  .nav.active {
    background: var(--bg-invert);
    color: var(--fg-invert);
  }
  .label {
    font-size: 13px;
    max-width: 0;
    opacity: 0;
    overflow: hidden;
    transition:
      max-width var(--dur-in) var(--ease-out),
      opacity var(--dur-fast) var(--ease-out);
  }
  .menu.expanded .label {
    max-width: 100px;
    opacity: 1;
  }
  .menu.expanded .nav {
    gap: 11px;
  }
  .spacer {
    flex: 1;
  }
  .group-sep {
    flex: none;
    height: 1px;
    align-self: stretch;
    margin: 8px 8px 6px;
    background: var(--line);
  }
  .group-title {
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--faint);
    padding: 0 9px;
    max-height: 0;
    opacity: 0;
    overflow: hidden;
    transition:
      max-height var(--dur-in) var(--ease-out),
      opacity var(--dur-fast) var(--ease-out),
      margin var(--dur-in) var(--ease-out);
  }
  .menu.expanded .group-title {
    max-height: 18px;
    opacity: 1;
    margin-bottom: 4px;
  }
  .sep {
    flex: none;
    height: 1px;
    align-self: stretch;
    margin: 6px 4px 0;
    background: var(--line);
  }
  .collapse {
    padding: 0 7px;
    margin-top: 6px;
  }
  .menu.expanded .collapse {
    padding: 0 9px;
  }
</style>
