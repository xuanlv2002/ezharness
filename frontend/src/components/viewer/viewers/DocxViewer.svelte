<script lang="ts">
  /*
  Word 查看器：mammoth 把 docx 转语义 HTML，DOMPurify 消毒后只读渲染；
  旧版 .doc 二进制格式 mammoth 不支持，给出转存提示。
  */

  import { onMount } from 'svelte'
  import DOMPurify from 'dompurify'
  import type { ResTab } from '../registry'

  let { tab }: { tab: ResTab; active?: boolean } = $props()

  let html = $state('')
  let loading = $state(true)
  let err = $state('')

  onMount(async () => {
    try {
      const buf = await (await fetch('/api/workspace/file?path=' + encodeURIComponent(tab.path ?? ''))).arrayBuffer()
      const mod: any = await import('mammoth/mammoth.browser')
      const mammoth = mod.default ?? mod
      const r = await mammoth.convertToHtml({ arrayBuffer: buf })
      html = DOMPurify.sanitize(r.value, { USE_PROFILES: { html: true } })
    } catch (e) {
      err = /\.doc$/i.test(tab.path ?? '')
        ? '旧版 .doc 二进制格式暂不支持预览——请在资源管理器定位后用 Word 打开,或另存为 .docx'
        : `文档解析失败：${(e as Error).message}`
    } finally {
      loading = false
    }
  })
</script>

<div class="viewer">
  {#if loading}
    <div class="veil">解析中…</div>
  {:else if err}
    <div class="veil err">{err}</div>
  {:else}
    <div class="doc">
      {@html html}
    </div>
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
  .doc {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 28px 32px;
    font-size: var(--font-size-section);
    line-height: 1.75;
    color: var(--fg);
  }
  .doc :global(h1),
  .doc :global(h2),
  .doc :global(h3) {
    margin: 18px 0 8px;
    line-height: 1.4;
  }
  .doc :global(p) {
    margin: 8px 0;
  }
  .doc :global(img) {
    max-width: 100%;
  }
  .doc :global(table) {
    border-collapse: collapse;
    margin: 10px 0;
  }
  .doc :global(td),
  .doc :global(th) {
    border: 1px solid var(--line);
    padding: 4px 10px;
    font-size: var(--font-size-section);
  }
  .veil {
    flex: 1;
    display: grid;
    place-items: center;
    color: var(--faint);
    font-size: var(--font-size-section);
    padding: 0 20px;
    text-align: center;
  }
  .veil.err {
    color: #e07a4f;
  }
</style>
