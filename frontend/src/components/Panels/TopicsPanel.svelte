<script lang="ts">
  import { api, type TopicEntry, type HistoryMessage } from '../../lib/api'
  import { store } from '../../lib/store.svelte'

  let topics = $state<TopicEntry[]>([])
  let loading = $state(true)
  let openId = $state('')
  let openMsgs = $state<HistoryMessage[]>([])
  let openLoading = $state(false)

  async function load() {
    loading = true
    try {
      topics = await api.listTopics()
    } finally {
      loading = false
    }
  }
  void load()

  async function toggleOpen(t: TopicEntry) {
    if (openId === t.id) {
      openId = ''
      return
    }
    openId = t.id
    openMsgs = []
    openLoading = true
    try {
      const s = await api.getTopic(t.id)
      openMsgs = s.messages || []
    } catch {
      openMsgs = []
    } finally {
      openLoading = false
    }
  }

  async function resume(t: TopicEntry) {
    await store.resumeTopic(t.id)
    store.panel = ''
    await load()
  }

  function fmt(ts: number): string {
    const d = new Date(ts)
    return `${d.getMonth() + 1}/${d.getDate()} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
  }
</script>

<div class="panel-page">
  <h2>话题存档</h2>
  <p class="hint">换话题时自动归档（摘要 + 完整存档）。可回顾，也可回到某话题继续聊。</p>

  {#if loading}
    <div class="loading">读取中…</div>
  {:else if topics.length === 0}
    <div class="loading">还没有归档话题——对 agent 说「我们换个话题」试试。</div>
  {:else}
    <div class="list">
      {#each [...topics].reverse() as t (t.id)}
        <div class="topic" class:open={openId === t.id}>
          <button class="head" onclick={() => toggleOpen(t)}>
            <span class="title">{t.title}</span>
            <span class="meta">{fmt(t.createdAt)} · {t.msgs} 条</span>
          </button>
          <div class="summary">{t.summary}</div>
          {#if openId === t.id}
            <div class="detail">
              {#if openLoading}
                <div class="loading">加载存档…</div>
              {:else}
                <div class="msgs">
                  {#each openMsgs as m, i (i)}
                    {#if m.role === 'user'}
                      <div class="m user">{m.content}</div>
                    {:else if m.role === 'assistant' && m.content}
                      <div class="m ai">{m.content}</div>
                    {/if}
                  {/each}
                </div>
              {/if}
              <button class="resume" onclick={() => resume(t)}>回到此话题继续</button>
            </div>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .panel-page {
    padding: 20px 18px;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  h2 {
    font-size: 14px;
    font-weight: 700;
  }
  .hint {
    font-size: 11.5px;
    color: var(--muted);
    line-height: 1.6;
  }
  .loading {
    color: var(--faint);
    font-size: 12px;
    padding: 16px 0;
  }
  .list {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .topic {
    border: 1px solid var(--line);
    border-radius: 10px;
    overflow: hidden;
  }
  .topic.open {
    border-color: var(--line-strong);
  }
  .head {
    display: flex;
    flex-direction: column;
    gap: 2px;
    width: 100%;
    border: none;
    background: transparent;
    text-align: left;
    padding: 10px 12px 6px;
  }
  .head:hover {
    background: var(--bg-soft);
  }
  .title {
    font-size: 13px;
    font-weight: 600;
  }
  .meta {
    font-family: var(--font-mono);
    font-size: 10px;
    color: var(--faint);
  }
  .summary {
    font-size: 11.5px;
    color: var(--muted);
    line-height: 1.55;
    padding: 0 12px 10px;
    max-height: 72px;
    overflow: hidden;
  }
  .detail {
    border-top: 1px solid var(--line);
    padding: 10px 12px;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .msgs {
    max-height: 280px;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .m {
    font-size: 11.5px;
    line-height: 1.55;
    white-space: pre-wrap;
    word-break: break-word;
  }
  .m.user {
    color: var(--fg);
    font-weight: 550;
  }
  .m.ai {
    color: var(--muted);
  }
  .resume {
    border: 1px solid var(--line-strong);
    background: transparent;
    border-radius: 7px;
    padding: 6px 12px;
    font-size: 12px;
    text-align: center;
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .resume:hover {
    background: var(--bg-invert);
    color: var(--fg-invert);
  }
</style>
