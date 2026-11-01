<script lang="ts">
  /*
  空状态引导：最后一个标签也被关掉时呈现（会话本身留在列表，关标签会
  停掉其中正在跑的一轮）。主按钮新建会话，弱提示从分支列表打开。
  */
  import { store } from '../lib/store.svelte'
  import Logo from './Logo.svelte'
  import EzAvatar from './EzAvatar.svelte'
  import QuickStart from './QuickStart.svelte'
</script>

<div class="chatempty">
  <div class="logo"><EzAvatar size={44} random><Logo size={44} /></EzAvatar></div>
  <h2>没有打开的会话</h2>
  <p>关掉的标签会停掉其中正在跑的一轮；会话本身还在左侧列表，随时可以再打开。</p>
  <button class="cta" onclick={() => store.newChatTab()}>
    新建会话
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
      <path d="M12 5v14M5 12h14" />
    </svg>
  </button>
  <p class="hint">或在左侧分支列表点击会话打开</p>
  <QuickStart />
</div>

<style>
  .chatempty {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 10px;
    padding: 0 16px 20px; /* 水平留白：窄面板下推荐卡（width:100%）不贴边 */
  }
  .logo {
    width: 46px;
    height: 46px;
    opacity: 0.9;
    margin-bottom: 6px;
  }
  h2 {
    font-size: var(--font-size-content);
    font-weight: 600;
  }
  p {
    font-size: var(--font-size-ui);
    color: var(--muted);
  }
  .hint {
    color: var(--faint);
    font-size: var(--font-size-small);
  }
  .cta {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    margin-top: 8px;
    border: 1px solid var(--line-strong);
    background: var(--bg-invert);
    color: var(--fg-invert);
    border-radius: 9px;
    padding: 8px 18px;
    font-size: var(--font-size-section);
    font-weight: 500;
  }
  .cta svg {
    width: 12px;
    height: 12px;
  }
</style>
