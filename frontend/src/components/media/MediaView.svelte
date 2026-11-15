<script lang="ts">
  /*
  多媒体页：工作台（生成 + 任务流）与「模型与限额」两个分区。媒体模型
  配置与语言模型彻底分离；agent 工具和 OpenAPI 提交的生成任务都落在
  MediaService 的同一份任务档案里，工作台轮询即见。
  */

  import { onMount } from 'svelte'
  import { api, type MediaConfig } from '../../lib/api'
  import MediaWorkbench from './MediaWorkbench.svelte'
  import MediaConfigPanel from './MediaConfigPanel.svelte'

  let tab = $state<'work' | 'config'>('work')
  let media = $state<MediaConfig | null>(null)
  let loaded = $state(false)
  let err = $state('')

  onMount(async () => {
    try {
      const m = await api.getMedia()
      media = { image: m.image ?? [], audio: m.audio ?? [], video: m.video ?? [] }
    } catch (e) {
      err = `多媒体配置加载失败：${e instanceof Error ? e.message : String(e)}`
    } finally {
      loaded = true
    }
  })
</script>

<div class="page">
  <header class="head">
    <div class="title">
      <h1>多媒体</h1>
      <p class="lead">
        图片 / 语音 / 视频生成：配好模型后直接在工作台生成；agent 工具与 OpenAPI 提交的任务也会汇入同一任务流异步执行。
      </p>
    </div>
    <div class="tabs" role="tablist" aria-label="多媒体分区">
      <button role="tab" aria-selected={tab === 'work'} class:on={tab === 'work'} onclick={() => (tab = 'work')}>工作台</button>
      <button role="tab" aria-selected={tab === 'config'} class:on={tab === 'config'} onclick={() => (tab = 'config')}>模型与限额</button>
    </div>
  </header>

  {#if err}<p class="err">{err}</p>{/if}
  {#if !loaded}
    <p class="loading">加载中…</p>
  {:else}
    <div class="pane" class:hidden={tab !== 'work'}>
      <MediaWorkbench {media} onconfigure={() => (tab = 'config')} />
    </div>
    <div class="pane" class:hidden={tab !== 'config'}>
      <MediaConfigPanel bind:media />
    </div>
  {/if}
</div>

<style>
  .page {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 40px 48px 60px;
  }
  .head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 18px;
    flex-wrap: wrap;
    margin-bottom: 24px;
  }
  h1 {
    font-size: var(--font-size-page);
    font-weight: 700;
  }
  .lead {
    margin-top: 4px;
    max-width: 760px;
    font-size: var(--font-size-section);
    color: var(--muted);
    line-height: 1.6;
  }
  .tabs {
    flex: none;
    display: flex;
    gap: 2px;
    padding: 2px;
    border: 1px solid var(--line);
    border-radius: 9px;
    background: var(--bg-soft);
  }
  .tabs button {
    border: none;
    background: transparent;
    color: var(--muted);
    border-radius: 6px;
    padding: 4px 14px;
    font: inherit;
    font-size: var(--font-size-ui);
    cursor: pointer;
  }
  .tabs button.on {
    background: var(--bg);
    color: var(--fg);
    font-weight: 600;
    box-shadow: 0 1px 2px rgb(0 0 0 / 8%);
  }
  .pane.hidden {
    display: none;
  }
  .err {
    margin: 0 0 14px;
    font-size: var(--font-size-ui);
    color: var(--danger, #c0392b);
  }
  .loading {
    color: var(--faint);
    font-size: var(--font-size-ui);
  }
  @media (max-width: 760px) {
    .page {
      padding: 24px 18px 40px;
    }
  }
</style>
