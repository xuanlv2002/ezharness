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

<div class="shell">
  {#if store.settings && !store.settings.apiKey}
    <button class="setup-banner" onclick={() => (store.panel = 'settings')}>
      未配置 API Key——点击前往设置
    </button>
  {/if}
  <div class="app" class:with-panel={store.panel !== ''}>
    <Sidebar />
    <main>
      <Timeline />
      <InputBar />
    </main>
    <StatusPanel />
  </div>
</div>

<style>
  .shell {
    height: 100%;
    display: flex;
    flex-direction: column;
  }
  .setup-banner {
    flex: none;
    border: none;
    border-bottom: 1px solid var(--line);
    background: var(--bg);
    color: var(--accent);
    padding: 8px 12px;
    font-size: 12.5px;
    cursor: pointer;
    text-align: center;
  }
  .setup-banner:hover {
    background: color-mix(in srgb, var(--accent) 6%, var(--bg));
  }
  .app {
    display: grid;
    grid-template-columns: 64px 0 1fr 232px;
    flex: 1;
    min-height: 0;
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
