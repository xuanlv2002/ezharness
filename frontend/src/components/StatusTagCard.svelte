<script lang="ts">
  import type { StatusPayload } from '../lib/api'

  let { data, raw }: { data: StatusPayload | null; raw: string } = $props()

  const hm = $derived(data ? data.now.slice(11) : '')
  const fmt = (n: number) => (n >= 10000 ? Math.round(n / 1000) + 'k' : String(n))

  /* 中文语义化文本解析时间与水位警示（JSON 载荷渲染对齐——历史重建
     与实时同一版式）。资源变更不在快照里（见 ResChangeCard） */
  const parsed = $derived.by(() => {
    const text = raw.replace(/^<agent_status>|<\/agent_status>$/g, '').trim()
    let time = ''
    let level = ''
    let warn = false
    for (const line of text.split('\n')) {
      if (line.startsWith('当前时间：')) {
        time = line.slice('当前时间：'.length).trim().slice(11) // 取 hh:mm
      }
      if (line.includes('建议') && line.includes('整理')) {
        warn = true
        const m = line.match(/上下文水位：(\d+)\s*\/\s*(\d+)/)
        if (m) level = `${fmt(Number(m[1]))}/${fmt(Number(m[2]))}`
      }
    }
    return { time, level, warn }
  })
</script>

<!-- 仅水位异常时渲染，一条细警示行 -->
<div class="alert">
  {#if data}
    <span class="t">{hm}</span>
    {#if data.suggestCompact}
      <span class="mono">{fmt(data.ctxTokens)}/{fmt(data.ctxWindow)}</span>
      <span class="warn">推荐压缩</span>
    {/if}
  {:else}
    <span class="t">{parsed.time}</span>
    {#if parsed.warn}
      <span class="mono">{parsed.level}</span>
      <span class="warn">推荐压缩</span>
    {/if}
  {/if}
</div>

<style>
  .alert {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
    padding: 3px 12px;
    font-size: 11.5px;
    color: var(--muted);
    border-left: 2px solid #d29922;
  }
  .t {
    font-family: var(--font-mono);
    color: var(--faint);
  }
  .mono {
    font-family: var(--font-mono);
  }
  .warn {
    color: #d29922;
    font-weight: 600;
  }
</style>
