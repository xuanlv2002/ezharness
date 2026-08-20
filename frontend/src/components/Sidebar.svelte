<script lang="ts">
  import Logo from './Logo.svelte'

  type View = 'chat' | 'settings'

  let { view, onNavigate }: { view: View; onNavigate: (v: View) => void } = $props()
</script>

<nav class="menu">
  <div class="logo" title="ezharness">
    <Logo size={32} />
  </div>
  <button
    class="nav"
    class:active={view === 'chat'}
    onclick={() => onNavigate('chat')}
    title="对话"
  >
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
      <path d="M21 11.5a8.5 8.5 0 0 1-8.5 8.5c-1.3 0-2.6-.25-3.7-.7L3 21l1.7-4.3A8.5 8.5 0 1 1 21 11.5z" />
    </svg>
  </button>
  <div class="spacer"></div>
  <button
    class="nav"
    class:active={view === 'settings'}
    onclick={() => onNavigate('settings')}
    title="设置"
  >
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09a1.65 1.65 0 0 0-1-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09a1.65 1.65 0 0 0 1.51-1 1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33h.01a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51h.01a1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82v.01a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
    </svg>
  </button>
</nav>

<style>
  .menu {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
    padding: 14px 0;
    background: var(--bg-soft);
    border-right: 1px solid var(--line);
  }
  .logo {
    display: grid;
    place-items: center;
    width: 36px;
    height: 36px;
    margin-bottom: 10px;
  }
  .nav {
    display: grid;
    place-items: center;
    width: 36px;
    height: 36px;
    border: none;
    background: transparent;
    border-radius: 9px;
    color: var(--muted);
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .nav svg {
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
  .spacer {
    flex: 1;
  }
</style>
