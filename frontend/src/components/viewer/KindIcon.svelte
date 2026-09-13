<script lang="ts">
  /*
  资源 kind → 线性 SVG 图标（与项目 stroke 图标风格一致，不依赖系统
  emoji 字体——Windows 下 🖼 等会回退成单色轮廓渲染异常）。
  */
  import type { ResourceKind } from './registry'

  let { kind }: { kind: ResourceKind } = $props()

  const bodies: Record<ResourceKind, string> = {
    /* 图片：山形 + 太阳 */
    image:
      '<rect x="3" y="5" width="18" height="14" rx="2"/><circle cx="15" cy="9.5" r="1.5"/><path d="M5.5 16.5l4-4a1.4 1.4 0 0 1 2 0l1.5 1.5a1.4 1.4 0 0 0 2 0l1-1a1.4 1.4 0 0 1 2 0l1.5 1.5"/>',
    /* Markdown：方框 + M↓ */
    markdown:
      '<rect x="3" y="5" width="18" height="14" rx="2"/><path d="M6.5 15v-6l2.4 3 2.4-3v6"/><path d="M16.5 9v6M16.5 15l-2-2M16.5 15l2-2"/>',
    /* HTML：地球 */
    html:
      '<circle cx="12" cy="12" r="9"/><path d="M3 12h18"/><path d="M12 3a13.5 13.5 0 0 1 0 18M12 3a13.5 13.5 0 0 0 0 18"/>',
    /* PDF：打开的书 */
    pdf: '<path d="M12 6.5C10.5 5 8.3 4.5 5.5 4.5c-.8 0-1.5.1-2.5.3v13.4c1-.2 1.7-.3 2.5-.3 2.8 0 5 .6 6.5 2 1.5-1.4 3.7-2 6.5-2 .8 0 1.5.1 2.5.3V4.8c-1-.2-1.7-.3-2.5-.3-2.8 0-5 .5-6.5 2z"/><path d="M12 6.5V20"/>',
    /* 文本：折角文档 */
    text: '<path d="M6 3h8l5 5v13H6z"/><path d="M14 3v5h5"/>',
    /* fallback：折角文档 + 问号 */
    fallback:
      '<path d="M6 3h8l5 5v13H6z"/><path d="M14 3v5h5"/><path d="M10.6 12.6a1.6 1.6 0 1 1 2.3 1.5c-.6.3-.9.6-.9 1.2v.2"/><circle cx="12" cy="17.2" r="0.4" fill="currentColor" stroke="none"/>',
  }
</script>

<svg
  viewBox="0 0 24 24"
  fill="none"
  stroke="currentColor"
  stroke-width="1.8"
  stroke-linecap="round"
  stroke-linejoin="round"
  aria-hidden="true"
>
  {@html bodies[kind]}
</svg>

<style>
  svg {
    width: 12px;
    height: 12px;
    flex: none;
    display: block;
  }
</style>
