<script lang="ts">
  /* 原型骨架：纯渲染层。
  MCP = 外部工具服务器（mcp.json），扩展 agent 能力。
  数据由后端下发，接口待业务开发接入，原型阶段 cfg 为 null 占位。 */
  interface McpServer {
    id: string
    name: string
    transport: 'http' | 'stdio'
    endpoint: string // http url 或 stdio 启动命令
    tools: number
    connected: boolean
    enabled: boolean
  }

  interface McpConfig {
    servers: McpServer[]
  }

  let cfg = $state<McpConfig | null>(null)
</script>

<div class="page">
  <div class="head">
    <div>
      <h1>MCP</h1>
      <p class="lead">外部工具服务器——接入后 agent 获得对应能力。</p>
    </div>
  </div>

  <div class="grid">
    {#if cfg}
      {#each cfg.servers as s (s.id)}
        <div class="card" class:off={!s.enabled}>
          <div class="top">
            <div class="glyph" class:live={s.connected}>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                <circle cx="5" cy="12" r="2" />
                <circle cx="19" cy="5" r="2" />
                <circle cx="19" cy="19" r="2" />
                <path d="M7 12h4M11 12l6-6M11 12l6 6" />
              </svg>
            </div>
            <span class="state" class:on={s.connected}>{s.connected ? '已连接' : '未连接'}</span>
          </div>
          <h2>{s.name}</h2>
          <p class="endpoint">
            <i>{s.transport}</i>{s.endpoint}
          </p>
          <div class="foot">
            <span class="tools">{s.tools} 个工具</span>
            <span class="toggle" class:on={s.enabled} role="switch" aria-checked={s.enabled} tabindex="0">
              <i></i>
            </span>
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
        <h2>暂无服务器</h2>
        <p class="endpoint">在 mcp.json 中添加，或让 agent 帮你接入。</p>
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
    gap: 16px;
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
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(216px, 1fr));
    gap: 14px;
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
  .card.off {
    opacity: 0.55;
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
  .state {
    font-size: 10.5px;
    font-family: var(--font-mono);
    color: var(--faint);
  }
  .state.on {
    color: var(--accent);
  }
  h2 {
    font-size: 13.5px;
    font-weight: 650;
    color: var(--fg);
    margin-top: 2px;
  }
  .endpoint {
    font-family: var(--font-mono);
    font-size: 11px;
    line-height: 1.6;
    color: var(--faint);
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
    overflow-wrap: anywhere;
    flex: 1;
  }
  .endpoint i {
    font-style: normal;
    font-size: 9.5px;
    font-weight: 600;
    text-transform: uppercase;
    color: var(--muted);
    border: 1px solid var(--line);
    border-radius: 4px;
    padding: 1px 5px;
    margin-right: 7px;
  }
  .foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-top: 2px;
  }
  .tools {
    font-size: 11px;
    color: var(--muted);
  }
  .toggle {
    position: relative;
    width: 30px;
    height: 17px;
    border-radius: 9px;
    background: var(--line);
    transition: background var(--dur-fast) var(--ease-out);
  }
  .toggle i {
    position: absolute;
    top: 2px;
    left: 2px;
    width: 13px;
    height: 13px;
    border-radius: 50%;
    background: var(--bg);
    transition: transform var(--dur-fast) var(--ease-out);
    box-shadow: 0 1px 2px rgb(0 0 0 / 20%);
  }
  .toggle.on {
    background: var(--accent);
  }
  .toggle.on i {
    transform: translateX(13px);
  }
  .card.ghost {
    border-style: dashed;
  }
  .ghost-glyph {
    border-style: dashed;
    background: transparent;
  }
  .card.ghost h2 {
    color: var(--muted);
  }
</style>
