<script lang="ts">
  /*
  PDF 查看器：iframe 直指工作目录文件服务（inline，WebView2/Chromium
  内置 PDF 渲染器——查看/翻页/缩放由原生控件承担）。
  */

  import type { ResTab } from '../registry'

  let { tab }: { tab: ResTab } = $props()
</script>

<div class="viewer">
  {#if tab.err}
    <div class="veil err">{tab.err}</div>
  {:else}
    <iframe
      class="frame"
      title="PDF 查看"
      src={'/api/workspace/file?path=' + encodeURIComponent(tab.path ?? '')}
    ></iframe>
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
    background: #fff;
    overflow: hidden;
  }
  .frame {
    flex: 1;
    width: 100%;
    border: none;
  }
  .veil {
    flex: 1;
    display: grid;
    place-items: center;
    color: var(--faint);
    font-size: 12.5px;
  }
  .veil.err {
    color: #e74c3c;
    padding: 0 20px;
    text-align: center;
  }
</style>
