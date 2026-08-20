<script lang="ts">
  /* 原型骨架：纯渲染层。
  快捷工具 = agent 生成的 html 等小工具，统一存放在一个文件夹，
  此页以卡片呈现并可快速启动。数据由后端下发，接口待业务开发
  接入，原型阶段 cfg 为 null 占位。 */
  interface ToolEntry {
    id: string
    name: string
    desc: string
    kind: string // html / …
    mtime: string
  }

  interface ToolsConfig {
    dir: string
    tools: ToolEntry[]
  }

  let cfg = $state<ToolsConfig | null>(null)
</script>

<div class="page">
  <div class="head">
    <div>
      <h1>快应用</h1>
      <p class="lead">agent 生成的小工具（html 等）都在这里，一键启动。</p>
    </div>
  </div>

  <p class="dir"><span>📁</span>{cfg ? cfg.dir : '—'}</p>

  <div class="grid">
    {#if cfg}
      {#each cfg.tools as t (t.id)}
        <div class="card" role="button" tabindex="0">
          <div class="top">
            <div class="glyph" class:live={t.kind === 'html'}>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                <path d="M8 6L3 12l5 6" />
                <path d="M16 6l5 6-5 6" />
              </svg>
            </div>
            <span class="kind">{t.kind}</span>
          </div>
          <h2>{t.name}</h2>
          <p class="desc">{t.desc}</p>
          <div class="foot">
            <span class="time">{t.mtime}</span>
            <button class="run" disabled>
              启动
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <path d="M7 17L17 7" />
                <path d="M8 7h9v9" />
              </svg>
            </button>
          </div>
        </div>
      {/each}
    {:else}
      <div class="card ghost">
        <div class="top">
          <div class="glyph ghost-glyph">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
              <path d="M12 5v14M5 12h14" />
            </svg>
          </div>
        </div>
        <h2>暂无工具</h2>
        <p class="desc">让 agent 做一个——「帮我写个 xx 的 html 小工具」，生成后自动出现在这里。</p>
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
  .head h1 {
    font-size: 18px;
    font-weight: 700;
  }
  .lead {
    font-size: 12px;
    color: var(--muted);
    margin-top: 4px;
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
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(196px, 1fr));
    gap: 14px;
    margin-top: 4px;
  }
  .card {
    display: flex;
    flex-direction: column;
    gap: 8px;
    border: 1px solid var(--line);
    border-radius: 14px;
    background: var(--bg);
    padding: 16px;
    transition:
      border-color var(--dur-fast) var(--ease-out),
      box-shadow var(--dur-fast) var(--ease-out),
      transform var(--dur-fast) var(--ease-out);
  }
  .card:not(.ghost):hover {
    border-color: var(--line-strong);
    box-shadow: 0 4px 16px rgb(0 0 0 / 7%);
    transform: translateY(-2px);
  }
  .top {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
  }
  .glyph {
    display: grid;
    place-items: center;
    width: 34px;
    height: 34px;
    border-radius: 10px;
    background: var(--bg-soft);
    border: 1px solid var(--line);
    color: var(--muted);
  }
  .glyph.live {
    background: var(--bg-invert);
    border-color: var(--bg-invert);
    color: var(--fg-invert);
  }
  .glyph svg {
    width: 16px;
    height: 16px;
  }
  .kind {
    font-family: var(--font-mono);
    font-size: 10px;
    font-weight: 600;
    text-transform: uppercase;
    color: var(--muted);
    border: 1px solid var(--line);
    border-radius: 5px;
    padding: 2px 6px;
  }
  h2 {
    font-size: 13.5px;
    font-weight: 650;
    color: var(--fg);
    margin-top: 2px;
  }
  .desc {
    font-size: 11.5px;
    line-height: 1.6;
    color: var(--faint);
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
    flex: 1;
  }
  .foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-top: 2px;
  }
  .time {
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--faint);
  }
  .run {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    border: 1px solid var(--line-strong);
    background: var(--bg-invert);
    color: var(--fg-invert);
    border-radius: 8px;
    padding: 5px 12px;
    font-size: 11.5px;
    font-weight: 550;
  }
  .run svg {
    width: 11px;
    height: 11px;
  }
  .run:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .card.ghost {
    border-style: dashed;
    color: var(--faint);
  }
  .ghost-glyph {
    border-style: dashed;
    background: transparent;
  }
  .card.ghost h2 {
    color: var(--muted);
  }
</style>
