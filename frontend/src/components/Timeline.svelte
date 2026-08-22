<script lang="ts">
  import { store } from '../lib/store.svelte'
  import Logo from './Logo.svelte'
  import MessageItem from './MessageItem.svelte'
  import ToolBlock from './ToolBlock.svelte'
  import ForkCard from './ForkCard.svelte'
  import DecisionCard from './DecisionCard.svelte'
  import StatusTagCard from './StatusTagCard.svelte'

  let el: HTMLDivElement
  let stick = true

  function onScroll() {
    if (!el) return
    stick = el.scrollHeight - el.scrollTop - el.clientHeight < 120
    // 滚顶：懒加载 compact 链上一会话，加载后保持视口位置不跳
    if (el.scrollTop < 40 && store.hasPrev && !store.loadingPrev) {
      const prevHeight = el.scrollHeight
      void store.loadPrev().then(() => {
        if (el) el.scrollTop = el.scrollHeight - prevHeight
      })
    }
  }

  $effect(() => {
    void store.tick
    if (el && stick) el.scrollTop = el.scrollHeight
  })
</script>

<div class="timeline" bind:this={el} onscroll={onScroll}>
  <div class="inner">
    {#each store.blocks as b (b.uid)}
      {#if b.kind === 'user'}
        <MessageItem text={b.text} role="user" />
      {:else if b.kind === 'assistant'}
        <MessageItem text={b.text} reasoning={b.reasoning} streaming={b.streaming} role="assistant" />
      {:else if b.kind === 'tool'}
        <ToolBlock data={b} />
      {:else if b.kind === 'fork'}
        <ForkCard fork={store.forks[b.forkId]} />
      {:else if b.kind === 'decision'}
        <div id={`decision-${b.id}`}>
          <DecisionCard data={b} />
        </div>
      {:else if b.kind === 'status'}
        <StatusTagCard data={b.data} raw={b.text} />
      {:else if b.kind === 'note'}
        <div class="note">
          <span class="line"></span>
          {b.text}
          <span class="line"></span>
        </div>
      {/if}
    {/each}
    {#if store.blocks.length === 0}
      <div class="empty">
        <div class="mark"><Logo size={56} /></div>
        <p>向 ezharness 发出第一条指令——它可全权操作本机。</p>
      </div>
    {/if}
  </div>
</div>

<style>
  .timeline {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
  }
  .inner {
    max-width: 780px;
    margin: 0 auto;
    padding: 40px 48px 24px;
    display: flex;
    flex-direction: column;
    gap: 20px;
    min-height: 100%; /* 空状态时也撑满可视区，使欢迎语垂直居中 */
  }
  .empty {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding-bottom: 80px; /* 补偿下方输入框高度，视觉重心略上提 */
    color: var(--muted);
  }
  .mark {
    display: inline-grid;
    place-items: center;
    width: 56px;
    height: 56px;
    margin-bottom: 16px;
  }
  .note {
    display: flex;
    align-items: center;
    gap: 14px;
    font-size: 11.5px;
    color: var(--faint);
    font-family: var(--font-mono);
    padding: 4px 0;
    animation: float-in var(--dur-fast) var(--ease-out) both;
  }
  .note .line {
    flex: 1;
    height: 1px;
    background: var(--line);
  }
</style>
