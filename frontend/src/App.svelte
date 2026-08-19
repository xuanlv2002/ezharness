<script lang="ts">
  import { onMount } from 'svelte'
  import { store } from './lib/store.svelte'
  import Sidebar from './components/Sidebar.svelte'
  import Timeline from './components/Timeline.svelte'
  import InputBar from './components/InputBar.svelte'
  import StatusPanel from './components/StatusPanel.svelte'

  onMount(() => {
    void store.bootstrap().catch(() => {
      store.lastStatus = '后端不可达'
    })
  })
</script>

<div class="app" class:with-panel={store.panel !== ''}>
  <Sidebar />
  <main>
    <Timeline />
    <InputBar />
  </main>
  <StatusPanel />
</div>

<style>
  .app {
    display: grid;
    grid-template-columns: 64px 0 1fr 232px;
    height: 100%;
    transition: grid-template-columns var(--dur-in) var(--ease-out);
  }
  .app.with-panel {
    grid-template-columns: 64px 300px 1fr 232px;
  }
  main {
    display: flex;
    flex-direction: column;
    min-width: 0;
    border-left: 1px solid var(--line);
  }
</style>
