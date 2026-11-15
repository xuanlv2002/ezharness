<script lang="ts">
  /*
  文本编辑器的查找垫层：textarea 不能给局部文字上底色，这里在它上面
  叠一层排版完全一致的透明 <pre>——只画命中 <mark> 的背景，文字透明，
  因此底下的正文/语法高亮照常可见。滚动位置跟随 textarea；当前命中
  变化时把 textarea 滚到它的 1/3 高度处。

  TextViewer（带语法高亮垫层）与 markdown/html 的原文编辑器共用。
  排版参数（14px/24px、14/16 内边距、软折行、稳定滚动条槽）与三处
  textarea 保持一致，改字号/内边距时要一起改。
  */

  import { tick } from 'svelte'
  import { markHtml, matchRanges } from '../../../lib/findtext'

  let {
    text,
    query,
    current = 0,
    scrollEl,
  }: {
    text: string
    query: string
    current?: number
    scrollEl?: HTMLTextAreaElement
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
    })
  })
</script>

<pre class="findlayer" bind:this={layerEl} aria-hidden="true">{@html html}</pre>

<style>
  .findlayer {
    position: absolute;
    inset: 0;
    z-index: 2;
    margin: 0;
    padding: var(--ed-pady, 14px) var(--ed-padx, 16px);
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
  .findlayer :global(mark) {
    background: color-mix(in srgb, var(--accent, #2563eb) 22%, transparent);
    color: transparent;
    border-radius: 2px;
  }
  .findlayer :global(mark.cur) {
    background: color-mix(in srgb, var(--accent, #2563eb) 40%, transparent);
  }
</style>
