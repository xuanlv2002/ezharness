<script lang="ts">
  /* HTML 内嵌浏览器预览：上报宿主矩形，真实页面由 desktop 主进程的 WebContentsView 渲染 */
  import { onMount } from 'svelte'
  import { toFileUrl } from '../../../lib/respaths'
  import type { ResTab } from '../registry'

  let {
    tab,
    active = false,
    animating = false,
    blocked = false,
    flush = false,
    onSave,
  }: {
    tab: ResTab
    active?: boolean
    animating?: boolean
    blocked?: boolean
    flush?: boolean
    onSave?: () => void
  } = $props()

  let host = $state<HTMLDivElement>()
  let shown = false
  let loadedPath = ''

  function previewApi(): any | undefined {
    return (window as any).ez?.preview
  }

  function show() {
    const api = previewApi()
    if (!api || !host || !active || animating || blocked || !tab.path) {
      if (shown) {
        api?.hide()
        shown = false
      }
      return
    }
    const r = host.getBoundingClientRect()
    if (r.width < 10 || r.height < 10) return
    shown = true
    api.show({ x: Math.round(r.left), y: Math.round(r.top), width: Math.round(r.width), height: Math.round(r.height) })
  }

  onMount(() => {
    const ro = new ResizeObserver(show)
    if (host) ro.observe(host)
    previewApi()?.onSave?.(() => onSave?.())
    return () => {
      ro.disconnect()
      previewApi()?.offSave?.()
      previewApi()?.hide()
    }
  })

  /* 切文件才重新导航；显示/尺寸变化只更新位置 */
  $effect(() => {
    const path = tab.path
    animating
    blocked
    if (!active) {
      show()
      return
    }
    if (path && loadedPath !== path) {
      loadedPath = path
      previewApi()?.load(toFileUrl(path))
    }
    const id = requestAnimationFrame(show)
    return () => cancelAnimationFrame(id)
  })
</script>

<svelte:window onresize={show} onfocus={show} />

<div class="preview-host" class:flush bind:this={host}>
  {#if !previewApi()}
    <p>浏览器预览仅桌面端可用</p>
  {/if}
</div>

<style>
  .preview-host {
    flex: 1;
    min-height: 0;
    display: grid;
    place-items: center;
    border: 1px solid var(--line);
    border-radius: 12px;
    background: var(--card);
    color: var(--faint);
    overflow: hidden;
  }
  .preview-host.flush {
    border: none;
    border-radius: 0;
  }
  .preview-host p {
    font-size: var(--font-size-section);
  }
</style>
