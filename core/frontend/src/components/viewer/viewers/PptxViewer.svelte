<script lang="ts">
  /*
  PPT 查看器：pptx-preview 逐页渲染 pptx（自带上/下页翻页与页码）。
  旧版 .ppt 二进制格式不支持,给出转存提示。
  */

  import { onMount } from 'svelte'
  import type { ResTab } from '../registry'

  let { tab }: { tab: ResTab; active?: boolean } = $props()

  let host: HTMLDivElement | undefined = $state()
  let loading = $state(true)
  let err = $state('')

  onMount(async () => {
    try {
      const buf = await (await fetch('/api/workspace/file?path=' + encodeURIComponent(tab.path ?? ''))).arrayBuffer()
      const { init } = await import('pptx-preview')
      if (!host) return
      const previewer = init(host, { width: 960 })
      await previewer.preview(buf)
    } catch (e) {
      err = /\.ppt$/i.test(tab.path ?? '')
        ? '旧版 .ppt 二进制格式暂不支持预览——请在资源管理器定位后用 PowerPoint 打开,或另存为 .pptx'
        : `幻灯片解析失败：${(e as Error).message}`
    } finally {
      loading = false
    }
  })
</script>

<div class="viewer">
  {#if loading}
    <div class="veil">解析中…</div>
  {/if}
  {#if err}
    <div class="veil err">{err}</div>
  {:else}
    <div class="stage" bind:this={host}></div>
  {/if}
</div>

<style>
  .viewer {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    border: 1px solid var(--line);
    border-radius: 12px;
    background: var(--bg);
    overflow: hidden;
  }
  .stage {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 12px;
    padding: 16px;
  }
  .veil {
    position: absolute;
    inset: 0;
    display: grid;
    place-items: center;
    color: var(--faint);
    font-size: var(--font-size-section);
    padding: 0 20px;
    text-align: center;
  }
  .veil.err {
    position: static;
    flex: 1;
    color: #e07a4f;
  }
</style>
