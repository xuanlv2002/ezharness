<script lang="ts">
  import { onMount } from 'svelte'
  import { api, type McpServerView } from '../lib/api'

  /* MCP = 外部工具服务器（mcp.json 热加载）。启停/添加保存后下一轮生效；
  connected 为 http 2s 探活，stdio 不探测。 */
  let servers = $state<McpServerView[]>([])
  let loaded = $state(false)
  let saving = $state(false)
  let message = $state('')
  let adding = $state(false)
  let draft = $state({ name: '', type: 'http', url: '' })

  onMount(async () => {
    try {
      const { servers: ss } = await api.getMcp()
      servers = ss ?? []
    } catch {
      message = 'MCP 数据加载失败（后端不可达）'
    }
    loaded = true
  })

  /* persist 自动保存（启停/添加即时提交，下一轮热加载生效）。 */
  async function persist() {
    saving = true
    message = ''
    try {
      await api.saveMcp({
        servers: servers.map((s) => ({
          name: s.name,
          type: s.transport,
          ...(s.transport === 'http' ? { url: s.endpoint } : {}),
          enabled: s.enabled,
        })),
      })
      message = '已保存，下一轮生效'
    } catch (e) {
      message = `保存失败：${(e as Error).message}`
    } finally {
      saving = false
    }
  }

  function toggle(s: McpServerView) {
    s.enabled = !s.enabled
    void persist()
  }

  async function addServer() {
    if (!draft.name.trim()) return
    if (draft.type === 'http' && !draft.url.trim()) return
    servers = [
      ...servers,
      {
        name: draft.name.trim(),
        transport: draft.type,
        endpoint: draft.type === 'http' ? draft.url.trim() : draft.name.trim(),
        enabled: true,
        connected: draft.type === 'stdio',
        tools: 0,
      },
    ]
    draft = { name: '', type: 'http', url: '' }
    adding = false
    void persist()
  }
</script>

<div class="page">
  <div class="head">
    <div>
      <h1>MCP</h1>
      <p class="lead">外部工具服务器——接入后 agent 获得对应能力。变更自动保存。</p>
    </div>
    {#if saving}
      <span class="saving">保存中…</span>
    {/if}
  </div>
  {#if message}
    <p class="msg">{message}</p>
  {/if}

  <div class="grid">
    {#if !loaded}
      <p class="lead">加载中…</p>
    {:else if servers.length}
      {#each servers as s (s.name)}
        <div class="card" class:off={!s.enabled}>
          <div class="top">
            <div class="glyph" class:live={s.connected && s.enabled}>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                <circle cx="5" cy="12" r="2" />
                <circle cx="19" cy="5" r="2" />
                <circle cx="19" cy="19" r="2" />
                <path d="M7 12h4M11 12l6-6M11 12l6 6" />
              </svg>
            </div>
            <span class="state" class:on={s.connected && s.enabled}>{s.enabled ? (s.connected ? '已连接' : '未连接') : '已停用'}</span>
          </div>
          <h2>{s.name}</h2>
          <p class="endpoint">
            <i>{s.transport}</i>{s.endpoint}
          </p>
          <div class="foot">
            <span class="tools">{s.tools > 0 ? `${s.tools} 个工具` : '工具数待连接后同步'}</span>
            <button class="toggle" class:on={s.enabled} onclick={() => toggle(s)} role="switch" aria-checked={s.enabled} tabindex="0">
              <i></i>
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
        <h2>暂无服务器</h2>
        <p class="endpoint">用下方表单添加，或让 agent 帮你接入。</p>
      </div>
    {/if}
  </div>

  {#if adding}
    <div class="add-form">
      <input type="text" placeholder="服务器名（如 github）" bind:value={draft.name} />
      <select bind:value={draft.type}>
        <option value="http">http</option>
        <option value="stdio">stdio</option>
      </select>
      {#if draft.type === 'http'}
        <input type="text" placeholder="https://example.com/mcp" bind:value={draft.url} />
      {/if}
      <div class="add-actions">
        <button class="add-ok" disabled={!draft.name.trim() || (draft.type === 'http' && !draft.url.trim())} onclick={addServer}>添加</button>
        <button class="add-no" onclick={() => (adding = false)}>取消</button>
      </div>
    </div>
  {:else}
    <button class="add" onclick={() => (adding = true)}>+ 添加服务器</button>
  {/if}
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
  .head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
  }
  .lead {
    font-size: 12px;
    color: var(--muted);
    margin-top: 4px;
  }
  .saving {
    font-size: 11.5px;
    color: var(--faint);
  }
  .msg {
    font-size: 11.5px;
    color: var(--muted);
    margin-top: -10px;
  }
  .add {
    align-self: flex-start;
    border: 1px dashed var(--line);
    background: transparent;
    color: var(--faint);
    border-radius: 10px;
    padding: 8px 18px;
    font-size: 12px;
  }
  .add:hover {
    border-color: var(--line-strong);
    color: var(--fg);
  }
  .add-form {
    display: flex;
    flex-direction: column;
    gap: 8px;
    border: 1px solid var(--line);
    border-radius: 12px;
    padding: 14px;
    max-width: 360px;
  }
  .add-form input,
  .add-form select {
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 8px 10px;
    font-family: var(--font-mono);
    font-size: 12px;
    outline: none;
    background: var(--bg);
    color: var(--fg);
  }
  .add-form input:focus,
  .add-form select:focus {
    border-color: var(--line-strong);
  }
  .add-actions {
    display: flex;
    gap: 8px;
  }
  .add-ok {
    border: 1px solid var(--line-strong);
    background: var(--bg-invert);
    color: var(--fg-invert);
    border-radius: 8px;
    padding: 6px 16px;
    font-size: 12px;
    font-weight: 550;
  }
  .add-ok:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .add-no {
    border: 1px solid var(--line);
    background: transparent;
    color: var(--muted);
    border-radius: 8px;
    padding: 6px 16px;
    font-size: 12px;
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
