<script lang="ts">
  import { store } from '../lib/store.svelte'
  import SettingsPanel from './Panels/SettingsPanel.svelte'
  import MemoryPanel from './Panels/MemoryPanel.svelte'
  import TopicsPanel from './Panels/TopicsPanel.svelte'

  function toggle(p: '' | 'settings' | 'memory' | 'topics') {
    store.panel = store.panel === p ? '' : p
  }
</script>

<nav class="rail">
  <div class="logo" title="ezharness">ez</div>
  <button class="nav" class:active={store.panel === 'settings'} onclick={() => toggle('settings')} title="设置">
    ⚙
  </button>
  <button class="nav" class:active={store.panel === 'memory'} onclick={() => toggle('memory')} title="记忆">
    ◈
  </button>
  <button class="nav" class:active={store.panel === 'topics'} onclick={() => toggle('topics')} title="话题">
    ⟲
  </button>
  <div class="spacer"></div>
  <button class="nav disabled" disabled title="知识库（即将推出）">▤</button>
  <button class="nav disabled" disabled title="工具（即将推出）">⚡</button>
</nav>

{#if store.panel !== ''}
  <aside class="panel">
    {#if store.panel === 'settings'}
      <SettingsPanel />
    {:else if store.panel === 'memory'}
      <MemoryPanel />
    {:else if store.panel === 'topics'}
      <TopicsPanel />
    {/if}
  </aside>
{/if}

<style>
  .rail {
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
    background: var(--bg-invert);
    color: var(--fg-invert);
    font-family: var(--font-mono);
    font-weight: 700;
    font-size: 14px;
    border-radius: 9px;
  }
  .nav {
    display: grid;
    place-items: center;
    width: 36px;
    height: 36px;
    border: none;
    background: transparent;
    border-radius: 9px;
    font-size: 16px;
    color: var(--muted);
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .nav:hover {
    background: var(--line);
    color: var(--fg);
  }
  .nav.active {
    background: var(--bg-invert);
    color: var(--fg-invert);
  }
  .nav.disabled {
    opacity: 0.3;
    cursor: default;
  }
  .spacer {
    flex: 1;
  }
  .panel {
    border-right: 1px solid var(--line);
    background: var(--bg);
    overflow-y: auto;
    animation: panel-in var(--dur-in) var(--ease-out) both;
  }
  @keyframes panel-in {
    from {
      opacity: 0;
      transform: translateX(-8px);
    }
    to {
      opacity: 1;
      transform: translateX(0);
    }
  }
</style>
