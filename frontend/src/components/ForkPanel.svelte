<script lang="ts">
  /*
  分身抽屉：task 分身的独立聊天框。主时间线只留入口卡（ForkCard），
  点击打开本抽屉查看完整执行记录（任务输入→思考→工具→决策→交付）。
  分身的审批/询问决策卡路由到此（通知栏点击跳转定位），非模态半开——
  主对话仍可见。多分身时顶部 tab 切换。
  */
  import { store } from '../lib/store.svelte'
  import MessageItem from './MessageItem.svelte'
  import ToolBlock from './ToolBlock.svelte'
  import DecisionCard from './DecisionCard.svelte'

  let body: HTMLDivElement | undefined = $state()
  let stick = $state(true) // 贴底才跟随流式（上翻看历史时不拽回）
  let lastFid = ''

  const fork = $derived(store.forks[store.activeForkId])

  function onScroll() {
    if (!body) return
    stick = body.scrollHeight - body.scrollTop - body.clientHeight < 120
  }

  /* 切换分身滚动归零；通知跳转定位决策卡；运行中且贴底才跟随流式 */
  $effect(() => {
    if (!fork || !body) return
    if (fork.id !== lastFid) {
      lastFid = fork.id
      stick = true
      body.scrollTop = 0
    }
    void store.tick
    if (store.jumpDecision) {
      const el = body.querySelector(`#decision-${store.jumpDecision}`)
      if (el) {
        el.scrollIntoView({ behavior: 'smooth', block: 'center' })
        stick = false // 用户在读这张卡，别再拽底
        store.jumpDecision = ''
        return
      }
    }
    if (fork.status === 'running' && stick) body.scrollTop = body.scrollHeight
  })
</script>

{#if fork}
  <aside class="drawer">
    <header class="head">
      {#if fork.status === 'running'}
        <span class="breathe"></span>
      {:else}
        <span class="dot"></span>
      {/if}
      <span class="fid">{fork.id}</span>
      <span class="task" title={fork.task}>{fork.task || '子任务'}</span>
      <span class="state" class:running={fork.status === 'running'}>
        {fork.status === 'running' ? '运行中' : fork.stopReason || 'done'}
      </span>
      <button class="close" onclick={() => store.closeFork()} title="关闭">✕</button>
    </header>

    <div class="body" bind:this={body} onscroll={onScroll}>
      {#if fork.blocks.length === 0}
        <div class="placeholder">
          {#if fork.status === 'done'}
            {fork.answer ? '执行记录加载中…' : '（无执行记录）'}
          {:else}
            <span class="caret"></span>
          {/if}
        </div>
      {/if}
      {#each fork.blocks as b (b.uid)}
        {#if b.kind === 'user'}
          <MessageItem text={b.text} role="user" />
        {:else if b.kind === 'assistant'}
          <MessageItem text={b.text} reasoning={b.reasoning} streaming={b.streaming} role="assistant" />
        {:else if b.kind === 'tool'}
          <ToolBlock data={b} />
        {:else if b.kind === 'decision'}
          <div id={`decision-${b.id}`}>
            <DecisionCard data={b} />
          </div>
        {:else if b.kind === 'note'}
          <div class="note">{b.text}</div>
        {/if}
      {/each}
      {#if fork.status === 'done' && fork.answer}
        <div class="answer">
          <div class="label">最终交付</div>
          <div class="text">{fork.answer}</div>
        </div>
      {/if}
    </div>
  </aside>
{/if}

<style>
  .drawer {
    position: absolute;
    top: 0;
    right: 0;
    bottom: 0;
    width: min(520px, 48vw);
    /* 高于 brand-foot（fixed z50）：打开期间盖住右下角脚标，关闭恢复 */
    z-index: 55;
    display: flex;
    flex-direction: column;
    background: var(--glass);
    backdrop-filter: var(--glass-blur);
    border-left: 1px solid var(--line);
    box-shadow: -12px 0 32px rgb(0 0 0 / 10%);
    animation: slide-in var(--dur, 0.22s) var(--ease-out, ease-out) both;
  }
  @keyframes slide-in {
    from {
      transform: translateX(24px);
      opacity: 0;
    }
    to {
      transform: translateX(0);
      opacity: 1;
    }
  }
  .head {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 14px;
    border-bottom: 1px solid var(--line);
    flex: none;
  }
  .breathe {
    flex: none;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--accent);
    animation: breathe 2.4s ease-in-out infinite;
  }
  @keyframes breathe {
    0%,
    100% {
      opacity: 0.35;
      transform: scale(0.85);
    }
    50% {
      opacity: 1;
      transform: scale(1.05);
    }
  }
  .dot {
    flex: none;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--line-strong);
  }
  .fid {
    font-family: var(--font-mono);
    font-size: 12px;
    font-weight: 700;
    color: var(--accent);
    flex: none;
  }
  .task {
    flex: 1;
    min-width: 0;
    font-size: 13px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .state {
    flex: none;
    font-size: 11px;
    font-family: var(--font-mono);
    color: var(--faint);
  }
  .state.running {
    color: var(--accent);
  }
  .close {
    flex: none;
    border: none;
    background: transparent;
    color: var(--muted);
    font-size: 13px;
    cursor: pointer;
    padding: 2px 6px;
    border-radius: 6px;
  }
  .close:hover {
    background: var(--bg-soft);
    color: var(--fg);
  }
  .body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    overflow-x: hidden; /* 长路径/长参数只换行不横滚 */
    scrollbar-gutter: stable; /* 预留滚动条槽：内容加载不因滚动条出现而晃动 */
    padding: 14px 16px;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  /* 子项不压缩：flex 列容器溢出时默认 shrink 会把消息块挤扁（伪乱序） */
  .body > :global(*) {
    flex: none;
  }
  .placeholder {
    color: var(--faint);
    font-size: 12.5px;
    text-align: center;
    padding: 24px 0;
  }
  .caret {
    display: inline-block;
    width: 7px;
    height: 15px;
    vertical-align: -2px;
    background: var(--fg);
    animation: caret 1s steps(1) infinite;
  }
  @keyframes caret {
    50% {
      opacity: 0;
    }
  }
  .note {
    font-size: 12px;
    color: var(--muted);
    text-align: center;
  }
  .answer {
    border: 1px solid var(--accent-soft, var(--line-strong));
    border-radius: 12px;
    padding: 10px 14px;
    background: var(--bg-soft);
  }
  .answer .label {
    font-size: 10px;
    letter-spacing: 0.08em;
    color: var(--accent);
    margin-bottom: 4px;
  }
  .answer .text {
    font-size: 13px;
    white-space: pre-wrap;
    word-break: break-word;
    line-height: 1.65;
  }
</style>
