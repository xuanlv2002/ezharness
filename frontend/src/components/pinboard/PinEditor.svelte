<script lang="ts">
  /*
  便签编辑面板：内容 / 截止时间。保存才写回卡片；Esc 取消、
  Ctrl/⌘+Enter 保存。
  */
  import { onMount, untrack } from 'svelte'
  import type { PinCard } from '../../lib/api'
  import { patchCard, removeCard } from '../../lib/pinboard.svelte'

  let {
    card,
    onclose,
  }: {
    card: PinCard
    onclose?: () => void
  } = $props()

  const pad2 = (n: number): string => String(n).padStart(2, '0')
  function ymd(ts: number): string {
    const d = new Date(ts)
    return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`
  }
  function hm(ts: number): string {
    const d = new Date(ts)
    return `${pad2(d.getHours())}:${pad2(d.getMinutes())}`
  }

  /* 编辑草稿：打开面板时快照一次（保存才写回卡片，Esc 丢弃） */
  let text = $state(untrack(() => card.text))
  let dateStr = $state(untrack(() => (card.due ? ymd(card.due) : '')))
  let timeStr = $state(untrack(() => (card.due ? hm(card.due) : '')))
  let err = $state('')
  let confirmDel = $state(false)
  let ta = $state<HTMLTextAreaElement>()
  let nowTick = $state(Date.now()) // 底部「已持续/距截止」每 30s 重算（面板可长开）

  /* 截止时刻：日期+时间都填才算；只填日期 = 当天 23:59 */
  function dueValue(): number | undefined {
    if (!dateStr) return undefined
    const [y, m, d] = dateStr.split('-').map(Number)
    if (!y || !m || !d) return undefined
    if (timeStr) {
      const [hh, mm] = timeStr.split(':').map(Number)
      return new Date(y, m - 1, d, hh || 0, mm || 0).getTime()
    }
    return new Date(y, m - 1, d, 23, 59).getTime()
  }

  /* 底部信息条实时：记于 + 已持续 + 截止/超期 */
  const info = $derived.by(() => {
    const now = nowTick
    const parts = [`记于 ${new Date(card.created).toLocaleString('zh-CN', { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' })}`]
    parts.push(`已持续 ${dur(now - card.created)}`)
    const d = dueValue()
    if (d) parts.push(d > now ? `距截止 ${dur(d - now)}` : `已超期 ${dur(now - d)}`)
    return parts.join(' · ')
  })

  function dur(ms: number): string {
    const m = Math.max(0, Math.floor(ms / 60000))
    if (m < 60) return `${m} 分钟`
    const h = Math.floor(m / 60)
    if (h < 24) return `${h} 小时${m % 60 ? ` ${m % 60} 分` : ''}`
    return `${Math.floor(h / 24)} 天${h % 24 ? ` ${h % 24} 小时` : ''}`
  }

  onMount(() => {
    requestAnimationFrame(() => {
      ta?.focus()
      ta?.setSelectionRange(text.length, text.length)
    })
    const t = setInterval(() => (nowTick = Date.now()), 30000)
    return () => clearInterval(t)
  })

  function save() {
    const t = text.trim()
    if (!t) {
      err = '内容不能为空'
      return
    }
    patchCard(card.id, { text: t, due: dueValue() })
    onclose?.()
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.preventDefault()
      onclose?.()
    } else if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
      e.preventDefault()
      save()
    }
  }

  function del() {
    removeCard(card.id)
    onclose?.()
  }
</script>

<svelte:window onkeydown={onKey} />

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
<div class="scrim" role="presentation" onclick={() => onclose?.()}>
  <div class="panel" role="dialog" tabindex="-1" aria-label="编辑便签" onclick={(e) => e.stopPropagation()} style:--tint={card.tint}>
    <h2>编辑便签</h2>
    <textarea bind:this={ta} bind:value={text} placeholder="记点什么…（Ctrl+Enter 保存，Esc 取消）" spellcheck="false"></textarea>
    {#if err}<p class="err">{err}</p>{/if}

    <div class="duerow">
      <span class="label">截止</span>
      <input type="date" bind:value={dateStr} />
      <input type="time" bind:value={timeStr} disabled={!dateStr} />
      <button class="mini" disabled={!dateStr && !timeStr} onclick={() => { dateStr = ''; timeStr = '' }}>清除</button>
    </div>


    <p class="info">{info}</p>

    <div class="acts">
      {#if confirmDel}
        <span class="cfm">删掉这张？</span>
        <button class="mini danger" onclick={del}>删除</button>
        <button class="mini" onclick={() => (confirmDel = false)}>取消</button>
      {:else}
        <button class="mini danger-ghost" onclick={() => (confirmDel = true)}>删除</button>
      {/if}
      <span class="spacer"></span>
      <button class="mini" onclick={() => onclose?.()}>取消</button>
      <button class="primary" onclick={save}>保存</button>
    </div>
  </div>
</div>

<style>
  .scrim {
    position: fixed;
    inset: 0;
    /* 高于便签卡（渲染层级封顶 60）；日期详情（1000）在打开编辑前已收起 */
    z-index: 250;
    display: grid;
    place-items: center;
    background: color-mix(in srgb, #000 28%, transparent);
  }
  .panel {
    width: min(470px, calc(100vw - 48px));
    max-height: calc(100vh - 80px);
    overflow-y: auto;
    overflow-x: hidden;
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 14px 16px 12px;
    border: 1px solid var(--line);
    border-top: 3px solid var(--tint, var(--accent));
    border-radius: 12px;
    background: var(--bg);
    box-shadow: 0 18px 48px rgb(0 0 0 / 18%);
    animation: pop var(--dur-fast) var(--ease-out) both;
  }
  @keyframes pop {
    from {
      opacity: 0;
      transform: translateY(6px) scale(0.985);
    }
  }
  h2 {
    margin: 0;
    font-size: var(--font-size-body);
    font-weight: 700;
  }
  textarea {
    min-height: 104px;
    max-height: 40vh;
    resize: vertical;
    padding: 8px 10px;
    border: 1px solid var(--line);
    border-radius: 8px;
    background: var(--bg-soft);
    color: var(--fg);
    font: inherit;
    font-size: var(--font-size-section);
    line-height: 1.65;
    outline: none;
  }
  textarea:focus {
    border-color: var(--line-strong);
    background: var(--bg);
  }
  .err {
    margin: 0;
    color: var(--danger);
    font-size: var(--font-size-small);
  }
  .duerow {
    display: flex;
    align-items: center;
    gap: 6px;
    width: 100%;
    min-width: 0;
    font-size: var(--font-size-ui);
  }
  .label {
    flex: none;
    width: 60px;
    color: var(--muted);
    font-size: var(--font-size-small);
  }
  input[type='date'],
  input[type='time'] {
    border: 1px solid var(--line);
    border-radius: 6px;
    background: var(--bg);
    color: var(--fg);
    font: inherit;
    font-size: var(--font-size-small);
    padding: 3px 6px;
    color-scheme: inherit;
  }
  .info {
    margin: 0;
    padding: 6px 8px;
    border-radius: 7px;
    background: var(--bg-soft);
    color: var(--faint);
    font-size: var(--font-size-small);
    font-family: var(--font-mono);
  }
  .acts {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .spacer {
    flex: 1;
  }
  .cfm {
    font-size: var(--font-size-small);
    color: var(--danger);
  }
  .mini {
    border: 1px solid var(--line);
    border-radius: 6px;
    background: var(--bg);
    color: var(--fg);
    font: inherit;
    font-size: var(--font-size-small);
    padding: 3px 8px;
    cursor: pointer;
  }
  .mini:hover:not(:disabled) {
    background: var(--bg-soft);
  }
  .mini:disabled {
    opacity: 0.45;
    cursor: default;
  }
  .mini.danger {
    color: var(--danger);
    border-color: color-mix(in srgb, var(--danger) 40%, var(--line));
  }
  .mini.danger-ghost {
    color: var(--danger);
    border-color: transparent;
  }
  .primary {
    border: none;
    border-radius: 7px;
    background: var(--fg);
    color: var(--bg);
    font: inherit;
    font-size: var(--font-size-small);
    font-weight: 600;
    padding: 4px 12px;
    cursor: pointer;
  }
  .primary:hover {
    opacity: 0.9;
  }
</style>
