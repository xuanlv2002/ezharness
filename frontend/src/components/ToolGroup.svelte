<script lang="ts">
  import type { Block } from '../lib/store.svelte'
  import ToolBlock from './ToolBlock.svelte'

  let { blocks, open, onToggle }: { blocks: Extract<Block, { kind: 'tool' }>[]; open: boolean; onToggle: () => void } =
    $props()

  /* 名称统计（terminal ×6、read_file ×2…）与当前执行中的工具 */
  const info = $derived.by(() => {
    const m = new Map<string, number>()
    let active = ''
    for (const b of blocks) {
      const n = b.name || 'tool'
      m.set(n, (m.get(n) || 0) + 1)
      if (b.state === 'running' || b.state === 'building') active = n
    }
    return {
      list: [...m].map(([n, c]) => (c > 1 ? `${n} ×${c}` : n)).join('、'),
      active,
    }
  })
</script>

{#if open}
  {#each blocks as b (b.uid)}
    <ToolBlock data={b} />
  {/each}
  <button class="fold" onclick={onToggle}>⌃ 收起这 {blocks.length} 个工具</button>
{:else}
  <button class="gbar" onclick={onToggle}>
    {#if info.active}
      <span class="spinner"></span>
    {:else}
      <span class="dot"></span>
    {/if}
    <span class="cnt">{blocks.length} 个工具调用</span>
    <span class="names">{info.list}</span>
    {#if info.active}
      <span class="active">⚙ {info.active} 执行中…</span>
    {/if}
  </button>
{/if}

<style>
  .gbar {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    margin-left: 40px;
    border: 1px dashed var(--line-strong);
    border-radius: 10px;
    background: transparent;
    padding: 7px 12px;
    font-size: 12.5px;
    text-align: left;
    color: var(--fg);
    transition: background var(--dur-fast) var(--ease-out);
  }
  .gbar:hover {
    background: var(--bg-soft);
  }
  .dot {
    flex: none;
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--line-strong);
  }
  .spinner {
    flex: none;
    width: 10px;
    height: 10px;
    border: 1.5px solid var(--line);
    border-top-color: var(--line-strong);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
  .cnt {
    flex: none;
    font-weight: 600;
  }
  .names {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--muted);
    font-family: var(--font-mono);
    font-size: 11.5px;
  }
  .active {
    flex: none;
    margin-left: auto;
    font-size: 11px;
    color: var(--muted);
  }
  .fold {
    margin-left: 40px;
    align-self: flex-start;
    font-size: 11.5px;
    color: var(--faint);
    padding: 2px 4px;
    text-align: left;
  }
  .fold:hover {
    color: var(--fg);
  }
</style>
