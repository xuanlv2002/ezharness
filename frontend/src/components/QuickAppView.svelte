<script lang="ts">
  /*
  内嵌快应用页：主窗口内 iframe 承载 apps/<名>/<entry>（与 core 同源，
  页面 cookie 鉴权照旧）。应用自带页面与导航，壳上不再叠标题/动作条：
  返回用左侧导航，重新进入即刷新，后端日志在终端页「快应用后端」分组。
  */
  import { quickApp } from '../lib/quickApp.svelte'
  import { store } from '../lib/store.svelte'

  const cur = $derived(quickApp.current)
  let loading = $state(true)

  /* 换应用/入口：重新进入加载态（iframe src 由模板驱动） */
  $effect(() => {
    void cur?.path
    loading = true
  })
</script>

<div class="qapp">
  {#if cur}
    <iframe src={cur.path} title={cur.title} onload={() => (loading = false)}></iframe>
    {#if loading}
      <div class="qload">加载应用…</div>
    {/if}
  {:else}
    <div class="qempty">
      <p>还没有打开的快应用</p>
      <button onclick={() => (store.jumpView = 'tools')}>去快应用页</button>
    </div>
  {/if}
</div>

<style>
  /* 整页交给应用自己：无外框、无内边距，只裁圆角贴合窗口 */
  .qapp {
    position: relative;
    flex: 1;
    min-height: 0;
    display: flex;
    overflow: hidden;
    border-top-left-radius: 10px;
    background: var(--bg);
  }
  iframe {
    display: block;
    flex: 1;
    width: 100%;
    height: 100%;
    border: none;
    background: var(--bg);
  }
  .qload {
    position: absolute;
    inset: 0;
    display: grid;
    place-items: center;
    background: var(--bg);
    color: var(--faint);
    font-size: var(--font-size-section);
    pointer-events: none;
  }
  .qempty {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 10px;
    color: var(--faint);
    font-size: var(--font-size-section);
  }
  .qempty button {
    border: none;
    background: none;
    padding: 0;
    color: var(--accent);
    font: inherit;
    cursor: pointer;
  }
  .qempty button:hover {
    text-decoration: underline;
  }
</style>
