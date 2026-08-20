<script lang="ts">
  import { onMount } from 'svelte'
  import pkg from '../package.json'
  import { store } from './lib/store.svelte'
  import Sidebar from './components/Sidebar.svelte'
  import ChatView from './components/ChatView.svelte'
  import ModelsView from './components/ModelsView.svelte'
  import MemoryView from './components/MemoryView.svelte'
  import KnowledgeView from './components/KnowledgeView.svelte'
  import ToolsView from './components/ToolsView.svelte'
  import McpView from './components/McpView.svelte'
  import SecurityView from './components/SecurityView.svelte'
  import SettingsView from './components/SettingsView.svelte'

  let view = $state<'chat' | 'models' | 'memory' | 'knowledge' | 'tools' | 'mcp' | 'security' | 'settings'>('chat')
  let expanded = $state(false)

  onMount(() => {
    void store.bootstrap().catch(() => {
      store.lastStatus = '后端不可达'
    })
  })
</script>

<div class="app" class:expanded>
  <Sidebar view={view} expanded={expanded} onNavigate={(v) => (view = v)} onToggle={() => (expanded = !expanded)} />
  <main>
    {#if view === 'chat'}
      <ChatView />
    {:else if view === 'models'}
      <ModelsView />
    {:else if view === 'memory'}
      <MemoryView />
    {:else if view === 'knowledge'}
      <KnowledgeView />
    {:else if view === 'tools'}
      <ToolsView />
    {:else if view === 'mcp'}
      <McpView />
    {:else if view === 'security'}
      <SecurityView />
    {:else}
      <SettingsView />
    {/if}
  </main>
</div>

<footer class="brand-foot">
  <span>ezharness v{pkg.version}</span>
  <span>·</span>
  <a href="https://github.com/xuanlv2002/ezloop" target="_blank" rel="noreferrer">powered by ezloop</a>
</footer>

<style>
  .app {
    display: grid;
    grid-template-columns: 64px 1fr;
    height: 100%;
    transition: grid-template-columns var(--dur-in) var(--ease-out);
  }
  .app.expanded {
    grid-template-columns: 176px 1fr;
  }
  main {
    min-width: 0;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .brand-foot {
    position: fixed;
    right: 12px;
    bottom: 8px;
    z-index: 50;
    display: flex;
    gap: 6px;
    font-family: var(--font-mono);
    font-size: 10px;
    color: var(--faint);
    user-select: none;
  }
  .brand-foot a {
    color: var(--faint);
    text-decoration: none;
    border-bottom: 1px dotted transparent;
    transition: color var(--dur-fast) var(--ease-out);
  }
  .brand-foot a:hover {
    color: var(--accent);
  }
</style>
