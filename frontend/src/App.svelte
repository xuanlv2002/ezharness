<script lang="ts">
  import Sidebar from './components/Sidebar.svelte'
  import ChatView from './components/ChatView.svelte'
  import SettingsView from './components/SettingsView.svelte'

  let view = $state<'chat' | 'settings'>('chat')
</script>

<div class="app">
  <Sidebar view={view} onNavigate={(v) => (view = v)} />
  <main>
    {#if view === 'chat'}
      <ChatView />
    {:else}
      <SettingsView />
    {/if}
  </main>
</div>

<style>
  .app {
    display: grid;
    grid-template-columns: 64px 1fr;
    height: 100%;
  }
  main {
    min-width: 0;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
</style>
