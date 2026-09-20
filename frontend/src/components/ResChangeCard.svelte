<script lang="ts">
  /* 资源变更卡：remind 变更段按需插入的 <resource_change> 消息 /
     resource.change 事件渲染（单一来源，实时与历史重建同款；块内
     available_ 清单行是给模型的，卡片不展示）。着色：新增/用户操作绿，
     移除/退出/关闭红，其余中性。 */
  let { items }: { items: string[] } = $props()

  const cls = (s: string) => {
    if (s.startsWith('新增') || s.startsWith('用户')) return 'add'
    if (s.startsWith('移除') || s.includes('已退出') || s.includes('已关闭')) return 'del'
    return ''
  }
</script>

<div class="reschange">
  {#each items as it, i (i)}
    <span class="row {cls(it)}">{it}</span>
  {/each}
</div>

<style>
  .reschange {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 3px 12px;
    font-size: 11.5px;
    color: var(--muted);
    border-left: 2px solid var(--line-strong);
    animation: float-in var(--dur-fast) var(--ease-out) both;
  }
  .row {
    font-family: var(--font-mono);
  }
  .add {
    color: #3fb950;
  }
  .del {
    color: #f85149;
  }
</style>
