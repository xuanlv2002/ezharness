<script lang="ts">
  /*
  空状态引导：最后一个标签也被关掉时呈现（会话不受影响，只是没有打开的
  聊天）。主按钮新建会话，弱提示从分支列表打开。
  */
  import { store } from '../lib/store.svelte'
  import Logo from './Logo.svelte'
  import EzAvatar from './EzAvatar.svelte'
</script>

<div class="chatempty">
  <div class="logo"><EzAvatar size={44} random><Logo size={44} /></EzAvatar></div>
  <h2>没有打开的会话</h2>
  <p>会话都在，只是没开标签——关掉标签不会结束任何会话。</p>
  <button class="cta" onclick={() => store.newChatTab()}>
    新建会话
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
      <path d="M12 5v14M5 12h14" />
    </svg>
  </button>
  <p class="hint">或在左侧分支列表点击会话打开</p>
</div>

<style>
  .chatempty {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 10px;
    padding-bottom: 60px;
  }
  .logo {
    width: 46px;
    height: 46px;
    opacity: 0.9;
    margin-bottom: 6px;
  }
  h2 {
    font-size: 15px;
    font-weight: 650;
  }
  p {
    font-size: 12px;
    color: var(--muted);
  }
  .hint {
    color: var(--faint);
    font-size: 11px;
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
    font-size: 12.5px;
    font-weight: 550;
  }
  .cta svg {
    width: 12px;
    height: 12px;
  }
</style>
