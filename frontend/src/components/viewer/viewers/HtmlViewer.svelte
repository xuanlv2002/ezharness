<script lang="ts">
  /*
  HTML 查看器：预览（iframe sandbox 空属性 = 禁脚本禁同源，纯静态
  渲染，无 XSS 面；相对资源在沙箱内不解析属预期）/ 原文（textarea
  编辑，Ctrl+S 保存）切换。
  */

  import type { ResTab } from '../registry'

  let {
    tab,
    active = false,
    flush = false,
    onSave,
  }: { tab: ResTab; active?: boolean; flush?: boolean; onSave?: () => void } = $props()

  let preview = $state(true)

  function onKey(e: KeyboardEvent) {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's' && !e.isComposing) {
      e.preventDefault()
      onSave?.()
    }
  }
</script>

<div class="viewer" class:flush>
  <div class="bar">
    <div class="seg">
      <button class:active={preview} onclick={() => (preview = true)}>预览</button>
      <button class:active={!preview} onclick={() => (preview = false)}>原文</button>
    </div>
    <span class="hint">{preview ? '沙箱静态渲染（禁脚本）' : '编辑后 Ctrl+S 保存'}</span>
  </div>
  {#if tab.err}
    <div class="veil err">{tab.err}</div>
  {:else if tab.loading}
    <div class="veil">加载中…</div>
  {:else if preview}
    <iframe class="frame" title="HTML 预览" sandbox="" srcdoc={tab.content}></iframe>
  {:else}
    <div class="editor-wrap">
      <textarea
        bind:value={tab.content}
        onkeydown={onKey}
        spellcheck="false"
        placeholder="（空文件）"
      ></textarea>
    </div>
  {/if}
</div>

<style>
  .viewer {
    display: flex;
    flex-direction: column;
    gap: 8px;
    height: 100%;
    min-height: 0;
  }
  .bar {
    flex: none;
    display: flex;
    align-items: center;
    gap: 8px;
  }
  /* 预览/原文 = 分段控件（一个容器两条边，替代两颗各自带框的胶囊） */
  .seg {
    display: flex;
    align-items: center;
    gap: 2px;
    width: fit-content;
    padding: 2px;
    border: 1px solid var(--line);
    border-radius: 8px;
    background: var(--bg-soft);
  }
  .seg button {
    border: none;
    background: transparent;
    color: var(--muted);
    border-radius: 6px;
    padding: 2px 10px;
    font-size: var(--font-size-ui);
    white-space: nowrap;
    cursor: pointer;
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .seg button.active {
    background: var(--bg);
    color: var(--fg);
    font-weight: 600;
    box-shadow: 0 1px 2px rgb(0 0 0 / 8%);
  }
  .hint {
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .frame,
  .editor-wrap {
    flex: 1;
    min-height: 0;
    width: 100%;
    border: 1px solid var(--line);
    border-radius: 12px;
    background: #fff;
  }
  /* 满铺模式（目录钉住展开）：内容区是一整面，取消内层卡片边框与圆角 */
  .viewer.flush {
    gap: 0;
  }
  .viewer.flush .bar {
    padding: 4px 8px;
  }
  .viewer.flush .frame,
  .viewer.flush .editor-wrap,
  .viewer.flush .veil {
    border: none;
    border-radius: 0;
  }
  .frame {
    border: none;
  }
  .editor-wrap {
    overflow: hidden;
  }
  .editor-wrap textarea {
    display: block;
    width: 100%;
    height: 100%;
    padding: 10px 12px;
    font-family: var(--font-mono);
    font-size: var(--font-size-section);
    line-height: 1.6;
    tab-size: 4;
    white-space: pre-wrap;
    overflow-wrap: break-word;
    scrollbar-gutter: stable;
    box-sizing: border-box;
    border: none;
    outline: none;
    resize: none;
    background: transparent;
    color: var(--fg);
  }
  .veil {
    flex: 1;
    display: grid;
    place-items: center;
    border: 1px solid var(--line);
    border-radius: 12px;
    background: var(--bg);
    color: var(--faint);
    font-size: var(--font-size-section);
  }
  .veil.err {
    color: #e74c3c;
    padding: 0 20px;
    text-align: center;
  }
</style>
