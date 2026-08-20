<script lang="ts">
  /* 原型骨架：纯渲染层。
  知识库 = agent 的正式文档库（llmwiki 方案）：文件入库自动索引并
  生成摘要，agent 检索查阅。与记忆的区别：正经的成体系文档，
  而非零散内容。入口两个：用户上传 / agent 存入。
  数据由后端下发，接口待业务开发接入，原型阶段 cfg 为 null 占位。 */
  interface DocEntry {
    id: string
    name: string
    kind: string // md / pdf / txt / …
    summary: string
    size: number
    mtime: string
    indexed: boolean
    source: 'user' | 'agent'
  }

  interface KnowledgeConfig {
    dir: string
    docs: DocEntry[]
  }

  let cfg = $state<KnowledgeConfig | null>(null)
  let query = $state('')

  function fmtSize(n: number): string {
    if (n < 1024) return `${n} B`
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
    return `${(n / 1024 / 1024).toFixed(1)} MB`
  }
</script>

<div class="page">
  <div class="head">
    <div>
      <h1>知识库</h1>
      <p class="lead">agent 的文档库——入库自动索引与摘要，与记忆不同，这里存放成体系的正式文档。</p>
    </div>
    <button class="upload" disabled>
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <path d="M12 16V5" />
        <path d="M7 10l5-5 5 5" />
        <path d="M4 19h16" />
      </svg>
      上传文档
    </button>
  </div>

  <div class="search">
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
      <circle cx="11" cy="11" r="7" />
      <path d="M20 20l-4-4" />
    </svg>
    <input type="text" placeholder="搜索文档…" bind:value={query} />
  </div>

  <p class="dir"><span>📁</span>{cfg ? cfg.dir : '—'}</p>

  <div class="list">
    {#if cfg}
      {#each cfg.docs as d (d.id)}
        <div class="doc">
          <span class="kind">{d.kind}</span>
          <div class="info">
            <span class="name">
              {d.name}
              {#if d.source === 'agent'}<i class="src">agent 存入</i>{/if}
            </span>
            <span class="summary">{d.summary}</span>
          </div>
          <div class="stat">
            <span class="dot" class:on={d.indexed} title={d.indexed ? '已索引' : '索引中…'}></span>
            <span class="meta">{fmtSize(d.size)} · {d.mtime}</span>
          </div>
        </div>
      {/each}
    {:else}
      <div class="empty">
        暂无文档——上传文件，或让 agent 把资料归档到知识库。
      </div>
    {/if}
  </div>
</div>

<style>
  .page {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    max-width: 720px;
    width: 100%;
    margin: 0 auto;
    padding: 40px 48px 60px;
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  .head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 16px;
  }
  h1 {
    font-size: 18px;
    font-weight: 700;
  }
  .lead {
    font-size: 12px;
    color: var(--muted);
    margin-top: 4px;
    max-width: 480px;
  }
  .upload {
    flex: none;
    display: inline-flex;
    align-items: center;
    gap: 7px;
    border: 1px solid var(--line-strong);
    background: var(--bg-invert);
    color: var(--fg-invert);
    border-radius: 9px;
    padding: 8px 14px;
    font-size: 12.5px;
    font-weight: 550;
  }
  .upload svg {
    width: 13px;
    height: 13px;
  }
  .upload:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .search {
    display: flex;
    align-items: center;
    gap: 9px;
    border: 1px solid var(--line);
    background: var(--bg-soft);
    border-radius: 10px;
    padding: 8px 12px;
    transition:
      border-color var(--dur-fast) var(--ease-out),
      background var(--dur-fast) var(--ease-out);
  }
  .search:focus-within {
    border-color: var(--line-strong);
    background: var(--bg);
  }
  .search svg {
    width: 13px;
    height: 13px;
    color: var(--faint);
    flex: none;
  }
  .search input {
    flex: 1;
    border: none;
    outline: none;
    background: transparent;
    font-family: inherit;
    font-size: 13px;
    color: var(--fg);
  }
  .search input::placeholder {
    color: var(--faint);
  }
  .dir {
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--faint);
    display: flex;
    align-items: center;
    gap: 6px;
    overflow-wrap: anywhere;
  }
  .list {
    display: flex;
    flex-direction: column;
    border: 1px solid var(--line);
    border-radius: 12px;
    overflow: hidden;
  }
  .empty {
    padding: 22px 14px;
    text-align: center;
    font-size: 12px;
    color: var(--faint);
  }
  .doc {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 11px 14px;
    transition: background var(--dur-fast) var(--ease-out);
  }
  .doc + .doc {
    border-top: 1px solid var(--line);
  }
  .doc:hover {
    background: var(--bg-soft);
  }
  .kind {
    flex: none;
    font-family: var(--font-mono);
    font-size: 10px;
    font-weight: 600;
    text-transform: uppercase;
    color: var(--muted);
    border: 1px solid var(--line);
    border-radius: 5px;
    padding: 2px 6px;
    min-width: 30px;
    text-align: center;
  }
  .info {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .name {
    font-size: 12.5px;
    font-weight: 550;
    color: var(--fg);
    display: flex;
    align-items: center;
    gap: 7px;
    overflow-wrap: anywhere;
  }
  .src {
    flex: none;
    font-style: normal;
    font-size: 9.5px;
    font-weight: 500;
    color: var(--muted);
    border: 1px solid var(--line);
    border-radius: 4px;
    padding: 0 5px;
  }
  .summary {
    font-size: 11px;
    color: var(--faint);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .stat {
    flex: none;
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    border: 1.5px solid var(--faint);
  }
  .dot.on {
    border-color: var(--accent);
    background: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }
  .meta {
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--faint);
    white-space: nowrap;
  }
</style>
