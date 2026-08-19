<script lang="ts">
  import { store } from '../lib/store.svelte'
  import MessageItem from './MessageItem.svelte'
  import ToolBlock from './ToolBlock.svelte'
  import ForkCard from './ForkCard.svelte'
  import DecisionCard from './DecisionCard.svelte'

  let el: HTMLDivElement
  let stick = true

  function onScroll() {
    if (!el) return
    stick = el.scrollHeight - el.scrollTop - el.clientHeight < 80
  }

  $effect(() => {
    void store.tick
    if (el && stick) el.scrollTop = el.scrollHeight
  })
</script>

<div class="timeline" bind:this={el} onscroll={onScroll}>
  <div class="inner">
    {#each store.blocks as b, i (i)}
      {#if b.kind === 'user'}
        <MessageItem text={b.text} role="user" />
      {:else if b.kind === 'assistant'}
        <MessageItem text={b.text} reasoning={b.reasoning} streaming={b.streaming} role="assistant" />
      {:else if b.kind === 'tool'}
        <ToolBlock data={b} />
      {:else if b.kind === 'fork'}
        <ForkCard fork={store.forks[b.forkId]} />
      {:else if b.kind === 'decision'}
        <DecisionCard data={b} />
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
        <div class="mark">ez</div>
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
  }
  .empty {
    padding: 120px 0;
    text-align: center;
    color: var(--muted);
  }
  .mark {
    display: inline-grid;
    place-items: center;
    width: 56px;
    height: 56px;
    margin-bottom: 16px;
    background: var(--bg-invert);
    color: var(--fg-invert);
    font-family: var(--font-mono);
    font-weight: 700;
    font-size: 24px;
    border-radius: 14px;
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
