<script lang="ts">
  /*
  文本编辑器的查找垫层：在排版一致的透明 <pre> 上只画命中底色；
  TextViewer 与 markdown/html 的原文编辑器共用。
  */

  import { tick } from 'svelte'
  import { markHtml, matchRanges } from '../../../lib/findtext'

  let {
    text,
    query,
    current = 0,
    scrollEl,
    wrap = true,
  }: {
    text: string
    query: string
    current?: number
    scrollEl?: HTMLTextAreaElement
    wrap?: boolean
  } = $props()

  let layerEl = $state<HTMLPreElement>()

  const html = $derived(markHtml(text, matchRanges(text, query), current))

  /* 滚动同步：textarea 是唯一滚动源，本层只是跟随 */
  $effect(() => {
    const el = layerEl
    const target = scrollEl
    if (!el || !target) return
    const sync = () => {
      el.scrollTop = target.scrollTop
      el.scrollLeft = target.scrollLeft
    }
    target.addEventListener('scroll', sync, { passive: true })
    sync()
    return () => target.removeEventListener('scroll', sync)
  })

  /* 当前命中变化：测量 mark 在正文中的纵向位置，把编辑器滚过去 */
  $effect(() => {
    html
    current
    const el = layerEl
    const target = scrollEl
    if (!el || !target) return
    void tick().then(() => {
      const mark = el.querySelector('mark.cur')
      if (!mark || !el.isConnected || !target.isConnected) return
      const mr = mark.getBoundingClientRect()
      const lr = el.getBoundingClientRect()
      const top = mr.top - lr.top + el.scrollTop
      target.scrollTop = Math.max(0, top - target.clientHeight / 3)
      el.scrollTop = target.scrollTop
      /* 不折行的编辑器里命中可能在可视区右侧 */
      const left = mr.left - lr.left + el.scrollLeft
      target.scrollLeft = Math.max(0, left - target.clientWidth / 3)
      el.scrollLeft = target.scrollLeft
    })
  })
</script>

<pre class="findlayer" class:nowrap={!wrap} bind:this={layerEl} aria-hidden="true">{@html html}</pre>

<style>
  .findlayer {
    position: absolute;
    inset: 0;
    z-index: 2;
    margin: 0;
    padding: var(--ed-pady, 14px) var(--ed-padx, 16px);
    padding-left: var(--ed-pl, var(--ed-padx, 16px));
    box-sizing: border-box;
    font-family: var(--font-mono);
    font-size: 14px;
    line-height: var(--ed-lh, 24px);
    tab-size: 4;
    white-space: pre-wrap;
    overflow-wrap: break-word;
    overflow: hidden;
    scrollbar-gutter: stable;
    pointer-events: none;
    color: transparent;
    background: transparent;
  }
  .findlayer.nowrap {
    white-space: pre;
    overflow-wrap: normal;
  }
  .findlayer :global(mark) {
    background: color-mix(in srgb, var(--accent, #2563eb) 22%, transparent);
    color: transparent;
    border-radius: 2px;
  }
  .findlayer :global(mark.cur) {
    background: color-mix(in srgb, var(--accent, #2563eb) 40%, transparent);
  }
</style>
