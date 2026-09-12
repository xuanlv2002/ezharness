<script lang="ts">
  import { onMount } from 'svelte'
  import { isDesktop } from '../lib/desktop'

  /* 桌面壳标题栏：?desktop 参数时渲染（桌面窗口 URL 带 ?desktop=1）。
     三键/关闭流程走 desktop 壳的 preload IPC（window.ez），拖拽由
     app-region 处理，浏览器访问不渲染 */
  const desktop = isDesktop

  /* desktop 壳能力（web 端为 undefined，防御性判空） */
  function ezWindow(): any | undefined {
    return (window as any).ez?.window
  }

  let maximized = $state(false)

  /* 关闭询问（页面 modal）：未配置托盘时点 X 返回 prompt=true 弹出；
     勾选「以后最小化到托盘」由壳持久化到设置，之后点 X 直接隐藏 */
  let closePrompt = $state(false)
  let trayChoice = $state(false)

  async function syncMax() {
    maximized = (await ezWindow()?.isMaximized()) ?? false
  }

  async function toggleMax() {
    maximized = (await ezWindow()?.toggleMaximize()) ?? false
  }

  async function onClose() {
    const r = await ezWindow()?.closeRequest()
    if (r?.prompt) {
      trayChoice = false
      closePrompt = true
    }
  }

  function decideClose() {
    closePrompt = false
    ezWindow()?.closeDecision(trayChoice, trayChoice)
  }

  /* 最大化状态跟随：拖拽还原/系统快捷键改变窗口态时同步按钮图标 */
  $effect(() => {
    if (!desktop) return
    void syncMax()
    window.addEventListener('resize', syncMax)
    return () => window.removeEventListener('resize', syncMax)
  })

  /* 系统级关闭（Alt+F4/任务栏）由壳转到页面弹确认框 */
  onMount(() => ezWindow()?.onClosePrompt(() => {
    trayChoice = false
    closePrompt = true
  }))
</script>

{#if desktop}
  <div class="titlebar">
    <span class="name">ezharness</span>
    <div class="btns">
      <button class="tbtn" onclick={() => ezWindow()?.minimize()} title="最小化">
        <svg viewBox="0 0 12 12"><path d="M1 6h10" stroke="currentColor" stroke-width="1.2" /></svg>
      </button>
      <button class="tbtn" onclick={toggleMax} title={maximized ? '还原' : '最大化'}>
        {#if maximized}
          <svg viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.2">
            <rect x="1.5" y="3.5" width="7" height="7" />
            <path d="M3.5 3.5v-2h7v7h-2" />
          </svg>
        {:else}
          <svg viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.2">
            <rect x="1.5" y="1.5" width="9" height="9" />
          </svg>
        {/if}
      </button>
      <button class="tbtn close" onclick={() => void onClose()} title="关闭">
        <svg viewBox="0 0 12 12"><path d="M2 2l8 8M10 2l-8 8" stroke="currentColor" stroke-width="1.2" /></svg>
      </button>
    </div>
  </div>

  {#if closePrompt}
    <div class="close-mask" role="presentation" onclick={() => (closePrompt = false)}>
      <div class="close-dialog" role="dialog" aria-modal="true" onclick={(e) => e.stopPropagation()}>
        <p class="close-q">关闭 ezharness？</p>
        <label class="close-opt">
          <input type="checkbox" bind:checked={trayChoice} />
          最小化到托盘（以后不再询问）
        </label>
        <div class="close-btns">
          <button class="cb cancel" onclick={() => (closePrompt = false)}>取消</button>
          <button class="cb ok" onclick={() => decideClose()}>关闭</button>
        </div>
      </div>
    </div>
  {/if}
{/if}

<style>
  .titlebar {
    display: flex;
    align-items: center;
    height: 34px;
    flex: none;
    background: var(--bg);
    user-select: none;
    app-region: drag;
  }
  .name {
    margin-left: 14px;
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--faint);
    pointer-events: none;
  }
  .btns {
    margin-left: auto;
    display: flex;
    height: 100%;
    app-region: no-drag;
  }
  .tbtn {
    display: grid;
    place-items: center;
    width: 44px;
    height: 100%;
    color: var(--muted);
    border-radius: 0;
    transition: background var(--dur-fast) var(--ease-out), color var(--dur-fast) var(--ease-out);
  }
  .tbtn svg {
    width: 12px;
    height: 12px;
  }
  .tbtn:hover {
    background: var(--bg-soft);
    color: var(--fg);
  }
  .tbtn.close:hover {
    background: #e81123;
    color: #fff;
  }
  /* 关闭询问：页面 modal（fixed 全屏遮罩 + 居中卡片），与产品 UI 同语言 */
  .close-mask {
    position: fixed;
    inset: 0;
    z-index: 200;
    display: grid;
    place-items: center;
    background: rgb(0 0 0 / 32%);
  }
  .close-dialog {
    width: 320px;
    padding: 18px 20px 16px;
    background: var(--bg);
    border: 1px solid var(--line);
    border-radius: 12px;
    box-shadow: 0 12px 40px rgb(0 0 0 / 18%);
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  .close-q {
    margin: 0;
    font-size: 14px;
    font-weight: 600;
    color: var(--fg);
  }
  .close-opt {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12px;
    color: var(--muted);
    cursor: pointer;
    user-select: none;
  }
  .close-opt input {
    accent-color: var(--accent);
  }
  .close-btns {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }
  .cb {
    border: 1px solid var(--line-strong);
    background: transparent;
    color: var(--fg);
    border-radius: 8px;
    padding: 6px 18px;
    font-size: 12px;
    font-weight: 550;
    cursor: pointer;
  }
  .cb.ok {
    background: var(--bg-invert);
    color: var(--fg-invert);
  }
  .cb:hover {
    opacity: 0.85;
  }
</style>
