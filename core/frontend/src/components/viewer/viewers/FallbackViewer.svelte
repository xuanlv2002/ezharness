<script lang="ts">
  /*
  Fallback 查看器：暂无专属查看器的资源类型（ppt/zip/二进制…）的占位提示。
  */

  import type { ResTab } from '../registry'

  let { tab }: { tab: ResTab } = $props()
</script>

<div class="fallback">
  <p class="ico"><svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"><path d="M3 8a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v7a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" /><path d="M7 4V3M12 4V3M17 4V3" /></svg></p>
  <p>{tab.name}</p>
  <p class="path">{tab.path}</p>
  <p class="hint">该类型暂无专属查看器——可经系统默认程序打开，或等待后续版本（每种资源将逐步获得专属查看器）</p>
</div>

<style>
  .fallback {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 6px;
    border: 1px solid var(--line);
    border-radius: 12px;
    background: var(--bg);
    color: var(--muted);
    font-size: var(--font-size-section);
    padding: 0 24px;
    text-align: center;
  }
  .ico {
    display: grid;
    place-items: center;
    color: var(--muted);
    opacity: 0.7;
  }
  .path {
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    color: var(--faint);
    word-break: break-all;
  }
  .hint {
    font-size: var(--font-size-ui);
    color: var(--faint);
  }
</style>
