<script lang="ts">
  import { onMount } from 'svelte'
  import { api, type McpServerView, type McpToolView } from '../lib/api'

  /* MCP = 外部工具服务器（mcp.json 热加载）。页面侧可手动建立 MCP 会话
  （与 agent 的连接相互独立）：点连接查看工具清单、填参试调用。 */
  let servers = $state<McpServerView[]>([])
  let loaded = $state(false)
  let saving = $state(false)
  let message = $state('')
  let adding = $state(false)
  let draft = $state({ name: '', desc: '', type: 'http', url: '', pairs: [] as HeaderPair[] })
  let editing = $state(false)
  let editDraft = $state({ desc: '', url: '', pairs: [] as HeaderPair[] })

  /* 连接态：工具清单缓存 + 当前展开的调用表单（"server/tool"）。 */
  let connectedTools = $state<Record<string, McpToolView[]>>({})
  let connecting = $state('')
  let openTool = $state('')
  let callDraft = $state('')
  let calling = $state(false)
  let callResult = $state<{ ok: boolean; text: string } | null>(null)

  /* selected 非空时进入 server 详情视图（工具/调用/编辑都在详情，卡片保持恒定高度）。 */
  let selected = $state<McpServerView | null>(null)
  let detailPage = $state<HTMLDivElement | null>(null)

  /* 进详情时滚动归零——滚动容器在视图切换间被复用，否则停留在网格页的滚动偏移。 */
  $effect(() => {
    if (selected && detailPage) detailPage.scrollTop = 0
  })

  interface HeaderPair {
    key: string
    value: string
  }

  onMount(async () => {
    try {
      const { servers: ss } = await api.getMcp()
      servers = ss ?? []
    } catch {
      message = 'MCP 数据加载失败（后端不可达）'
    }
    loaded = true
  })

  /* persist 自动保存（启停/添加/删除即时提交，下一轮热加载生效；
  后端会顺带关闭已删除/禁用 server 的页面会话）。 */
  async function persist() {
    saving = true
    message = ''
    try {
      await api.saveMcp({
        servers: servers.map((s) => ({
          name: s.name,
          ...(s.description ? { description: s.description } : {}),
          type: s.transport,
          ...(s.transport === 'http' ? { url: s.endpoint, headers: s.headers } : {}),
          ...(s.allow?.length ? { allow: s.allow } : {}), // 透传白名单，防全量保存清掉
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
        description: draft.desc.trim(),
        transport: draft.type,
        endpoint: draft.type === 'http' ? draft.url.trim() : draft.name.trim(),
        enabled: true,
        connected: false,
        tools: 0,
        headers: draft.type === 'http' ? pairsToRecord(draft.pairs) : {},
      },
    ]
    draft = { name: '', desc: '', type: 'http', url: '', pairs: [] }
    adding = false
    void persist()
  }

  function pairsToRecord(pairs: HeaderPair[]): Record<string, string> {
    const out: Record<string, string> = {}
    for (const p of pairs) if (p.key.trim()) out[p.key.trim()] = p.value
    return out
  }

  function startEdit() {
    const s = selected
    if (!s) return
    if (editing) {
      editing = false
      return
    }
    editing = true
    editDraft = {
      desc: s.description ?? '',
      url: s.endpoint,
      pairs: Object.entries(s.headers ?? {}).map(([key, value]) => ({ key, value })),
    }
  }

  function saveEdit() {
    const s = selected
    if (!s || !editDraft.url.trim()) return
    s.description = editDraft.desc.trim()
    s.endpoint = editDraft.url.trim()
    s.headers = pairsToRecord(editDraft.pairs)
    editing = false
    void persist()
  }

  function removeServer(s: McpServerView) {
    if (!confirm(`删除 MCP server「${s.name}」？`)) return
    servers = servers.filter((x) => x.name !== s.name)
    delete connectedTools[s.name]
    if (openTool.startsWith(s.name + '/')) openTool = ''
    if (selected === s) selected = null
    void persist()
  }

  /* connect 建立（或复用）页面会话，成功即展开工具清单。 */
  async function connect(s: McpServerView) {
    connecting = s.name
    message = ''
    try {
      const { tools } = await api.connectMcp(s.name)
      s.connected = true
      s.tools = (tools ?? []).length
      connectedTools[s.name] = tools ?? []
    } catch (e) {
      s.connected = false
      s.tools = 0
      message = `连接失败：${(e as Error).message}`
    }
    connecting = ''
  }

  async function disconnect(s: McpServerView) {
    try {
      await api.disconnectMcp(s.name)
    } catch {
      /* 幂等 */
    }
    s.connected = false
    s.tools = 0
    delete connectedTools[s.name]
    if (openTool.startsWith(s.name + '/')) openTool = ''
  }

  function toggleTool(s: McpServerView, t: McpToolView) {
    const key = `${s.name}/${t.name}`
    if (openTool === key) {
      openTool = ''
      return
    }
    openTool = key
    callDraft = skeleton(t.args_schema)
    callResult = null
  }

  /* skeleton 从 args_schema.properties 生成参数骨架，减少手写。 */
  function skeleton(schema: any): string {
    const props = schema?.properties
    if (!props || typeof props !== 'object' || !Object.keys(props).length) return '{\n}'
    const lines = Object.entries(props).map(([k, p]: [string, any]) => {
      const t = p?.type
      const v =
        t === 'number' || t === 'integer'
          ? 0
          : t === 'boolean'
            ? false
            : t === 'array'
              ? []
              : t === 'object'
                ? {}
                : ''
      return `  "${k}": ${JSON.stringify(v)}`
    })
    return '{\n' + lines.join(',\n') + '\n}'
  }

  /* toolBrief 参数说明一行：name (type) — description。 */
  function toolBrief(schema: any): string[] {
    const props = schema?.properties
    if (!props || typeof props !== 'object') return []
    return Object.entries(props).map(([k, p]: [string, any]) => {
      const req = Array.isArray(schema?.required) && schema.required.includes(k) ? '*' : ''
      const t = p?.type ? `${p.type}${req}` : req || 'any'
      return `${k} (${t})${p?.description ? ' — ' + p.description : ''}`
    })
  }

  async function invoke(server: string) {
    calling = true
    callResult = null
    try {
      let args: unknown = {}
      if (callDraft.trim()) args = JSON.parse(callDraft)
      const { result } = await api.callMcp(server, openTool.split('/')[1], args)
      callResult = { ok: true, text: result }
    } catch (e) {
      callResult = { ok: false, text: (e as Error).message }
    }
    calling = false
  }
</script>

{#if selected}
  {@const sel = selected}
  <div class="page" bind:this={detailPage}>
    <button class="back" onclick={() => (selected = null)}>← MCP 服务器</button>
    <div class="detail-head">
      <div class="glyph big" class:live={selected.connected && selected.enabled}>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <circle cx="5" cy="12" r="2" />
          <circle cx="19" cy="5" r="2" />
          <circle cx="19" cy="19" r="2" />
          <path d="M7 12h4M11 12l6-6M11 12l6 6" />
        </svg>
      </div>
      <div class="detail-title">
        <h1>{selected.name}</h1>
        <span class="state" class:on={selected.connected && selected.enabled}>
          {selected.enabled ? (selected.connected ? `已连接 · ${selected.tools} 工具` : '未连接') : '已停用'}
        </span>
        {#if selected.description}
          <p class="detail-desc">{selected.description}</p>
        {/if}
      </div>
    </div>
    <p class="endpoint">
      <i>{selected.transport}</i>{selected.endpoint}
    </p>
    {#if Object.keys(selected.headers ?? {}).length}
      <div class="hdr-list">
        {#each Object.entries(selected.headers) as [k, v] (k)}
          <p class="hdr-line"><i>{k}</i>{v}</p>
        {/each}
      </div>
    {/if}
    <div class="detail-ops">
      <button
        class="conn"
        class:on={selected.connected}
        disabled={connecting === selected.name}
        onclick={() => (connectedTools[sel.name]?.length ? disconnect(sel) : connect(sel))}
      >
        {connecting === selected.name ? '连接中…' : connectedTools[selected.name]?.length ? '断开会话' : '建立会话'}
      </button>
      {#if selected.transport === 'http'}
        <button class="op" onclick={startEdit}>{editing ? '收起编辑' : '编辑'}</button>
      {/if}
      <button class="op danger" onclick={() => removeServer(sel)}>删除</button>
      <span class="op-label">
        启用
        <button class="toggle" class:on={selected.enabled} onclick={() => toggle(sel)} role="switch" aria-checked={selected.enabled} tabindex="0">
          <i></i>
        </button>
      </span>
    </div>
    {#if message}
      <p class="msg">{message}</p>
    {/if}
    {#if editing}
      <div class="edit-form">
        <input type="text" placeholder="描述（agent 经 mcp_list 发现用）" bind:value={editDraft.desc} />
        <input type="text" placeholder="https://example.com/mcp" bind:value={editDraft.url} />
        {#each editDraft.pairs as p, j}
          <div class="hdr-row">
            <input type="text" placeholder="Header（如 Authorization）" bind:value={p.key} />
            <input type="text" placeholder="Value（如 Bearer xxx）" bind:value={p.value} />
            <button class="hdr-x" onclick={() => (editDraft.pairs = editDraft.pairs.filter((_, k) => k !== j))} title="移除">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round">
                <path d="M6 6l12 12M18 6L6 18" />
              </svg>
            </button>
          </div>
        {/each}
        <button class="hdr-add" onclick={() => (editDraft.pairs = [...editDraft.pairs, { key: '', value: '' }])}>+ 请求头</button>
        <div class="add-actions">
          <button class="add-ok" disabled={!editDraft.url.trim()} onclick={saveEdit}>保存</button>
          <button class="add-no" onclick={() => (editing = false)}>取消</button>
        </div>
      </div>
    {/if}
    <div class="tools-sec">
      {#if !selected.connected}
        <p class="lead">建立会话后可查看工具清单并在此试调用。</p>
      {:else if connectedTools[selected.name]?.length}
        {#each connectedTools[selected.name] as t (t.name)}
          <button class="tool-row" onclick={() => toggleTool(sel, t)}>
            <span class="tool-name">{t.name}</span>
            {#if t.description}
              <span class="tool-desc">{t.description}</span>
            {/if}
          </button>
          {#if openTool === `${selected.name}/${t.name}`}
            <div class="call-form">
              {#each toolBrief(t.args_schema) as line}
                <p class="param">{line}</p>
              {/each}
              <textarea spellcheck="false" rows="5" bind:value={callDraft}></textarea>
              <div class="add-actions">
                <button class="add-ok" disabled={calling} onclick={() => invoke(sel.name)}>{calling ? '调用中…' : '调用'}</button>
                <button class="add-no" onclick={() => (openTool = '')}>收起</button>
              </div>
              {#if callResult}
                <pre class="result" class:err={!callResult.ok}>{callResult.text}</pre>
              {/if}
            </div>
          {/if}
        {/each}
      {:else}
        <p class="lead">已连接，该 server 未暴露工具。</p>
      {/if}
    </div>
  </div>
{:else}
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
        <div
          class="card"
          class:off={!s.enabled}
          role="button"
          tabindex="0"
          onclick={() => (selected = s)}
          onkeydown={(e) => (e.key === 'Enter' ? (selected = s) : undefined)}
        >
          <div class="top">
            <div class="glyph" class:live={s.connected && s.enabled}>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                <circle cx="5" cy="12" r="2" />
                <circle cx="19" cy="5" r="2" />
                <circle cx="19" cy="19" r="2" />
                <path d="M7 12h4M11 12l6-6M11 12l6 6" />
              </svg>
            </div>
            <span class="state" class:on={s.connected && s.enabled}>{s.enabled ? (s.connected ? `已连接 · ${s.tools} 工具` : '未连接') : '已停用'}</span>
          </div>
          <h2>{s.name}</h2>
          {#if s.description}
            <p class="card-desc">{s.description}</p>
          {/if}
          <p class="endpoint">
            <i>{s.transport}</i>{s.endpoint}
          </p>
          <div class="foot">
            <span class="tools">
              {s.connected ? `${s.tools} 个工具` : '点击查看'}
              {#if s.transport === 'http' && Object.keys(s.headers ?? {}).length > 0}
                <i>· {Object.keys(s.headers).length} 个请求头</i>
              {/if}
            </span>
            <div class="foot-ops">
              <button
                class="conn"
                class:on={s.connected}
                disabled={connecting === s.name}
                onclick={(e) => {
                  e.stopPropagation()
                  connectedTools[s.name]?.length ? disconnect(s) : connect(s)
                }}
              >
                {connecting === s.name ? '…' : connectedTools[s.name]?.length ? '断开' : '连接'}
              </button>
              <button
                class="toggle"
                class:on={s.enabled}
                role="switch"
                aria-checked={s.enabled}
                tabindex="0"
                onclick={(e) => {
                  e.stopPropagation()
                  toggle(s)
                }}
              >
                <i></i>
              </button>
            </div>
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
      <input type="text" placeholder="描述（agent 经 mcp_list 发现用，如 GitHub 仓库搜索）" bind:value={draft.desc} />
      <select bind:value={draft.type}>
        <option value="http">http</option>
        <option value="stdio">stdio</option>
      </select>
      {#if draft.type === 'http'}
        <input type="text" placeholder="https://example.com/mcp" bind:value={draft.url} />
        {#each draft.pairs as p, j}
          <div class="hdr-row">
            <input type="text" placeholder="Header（如 Authorization）" bind:value={p.key} />
            <input type="text" placeholder="Value（如 Bearer xxx）" bind:value={p.value} />
            <button class="hdr-x" onclick={() => (draft.pairs = draft.pairs.filter((_, k) => k !== j))} title="移除">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round">
                <path d="M6 6l12 12M18 6L6 18" />
              </svg>
            </button>
          </div>
        {/each}
        <button class="hdr-add" onclick={() => (draft.pairs = [...draft.pairs, { key: '', value: '' }])}>+ 请求头</button>
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
{/if}

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
    min-width: 0;
    padding: 16px;
    transition:
      border-color var(--dur-fast) var(--ease-out),
      box-shadow var(--dur-fast) var(--ease-out),
      transform var(--dur-fast) var(--ease-out);
  }
  .card:not(.ghost) {
    cursor: pointer;
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
  .card-desc {
    font-size: 11px;
    color: var(--muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    margin-top: -2px;
  }
  .detail-desc {
    font-size: 11.5px;
    color: var(--muted);
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
    flex-wrap: wrap;
    row-gap: 6px;
    margin-top: auto;
    padding-top: 2px;
  }
  .tools {
    font-size: 11px;
    color: var(--muted);
    white-space: nowrap;
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
  .foot-ops {
    display: flex;
    align-items: center;
    gap: 4px;
  }
  .tools i {
    font-style: normal;
    color: var(--faint);
  }
  .back {
    align-self: flex-start;
    color: var(--muted);
    font-size: 12px;
    padding: 2px 0;
  }
  .back:hover {
    color: var(--fg);
  }
  .detail-head {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .glyph.big {
    width: 44px;
    height: 44px;
    border-radius: 12px;
  }
  .glyph.big svg {
    width: 20px;
    height: 20px;
  }
  .detail-title {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .detail-title h1 {
    font-size: 18px;
    font-weight: 700;
    line-height: 1.3;
    overflow-wrap: anywhere;
  }
  .hdr-list {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .hdr-line {
    font-family: var(--font-mono);
    font-size: 11.5px;
    color: var(--muted);
    overflow-wrap: anywhere;
  }
  .hdr-line i {
    font-style: normal;
    font-weight: 600;
    color: var(--faint);
    margin-right: 8px;
  }
  .detail-ops {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }
  .op {
    border: 1px solid var(--line);
    background: transparent;
    color: var(--muted);
    font-size: 11.5px;
    padding: 5px 12px;
    border-radius: 8px;
  }
  .op:hover {
    border-color: var(--line-strong);
    color: var(--fg);
  }
  .op.danger:hover {
    border-color: #c0392b;
    color: #c0392b;
  }
  .op-label {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    font-size: 11.5px;
    color: var(--muted);
    margin-left: auto;
  }
  .conn {
    border: 1px solid var(--line);
    background: transparent;
    color: var(--muted);
    font-size: 11px;
    padding: 3px 9px;
    border-radius: 6px;
  }
  .conn:hover {
    border-color: var(--line-strong);
    color: var(--fg);
  }
  .conn.on {
    border-color: transparent;
    background: var(--bg-soft);
    color: var(--accent);
  }
  .conn:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .detail-ops .conn {
    font-size: 12px;
    padding: 5px 14px;
  }
  .tools-sec {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin-top: 8px;
  }
  .tool-row {
    display: flex;
    flex-direction: column;
    gap: 2px;
    text-align: left;
    border: 1px solid transparent;
    background: transparent;
    border-radius: 8px;
    padding: 6px 8px;
  }
  .tool-row:hover {
    border-color: var(--line);
    background: var(--bg-soft);
  }
  .tool-name {
    font-family: var(--font-mono);
    font-size: 11.5px;
    font-weight: 600;
    color: var(--fg);
  }
  .tool-desc {
    font-size: 10.5px;
    line-height: 1.5;
    color: var(--faint);
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .call-form {
    display: flex;
    flex-direction: column;
    gap: 6px;
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 8px;
    margin-bottom: 4px;
  }
  .param {
    font-family: var(--font-mono);
    font-size: 10px;
    line-height: 1.5;
    color: var(--muted);
    overflow-wrap: anywhere;
  }
  .call-form textarea {
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 8px 10px;
    font-family: var(--font-mono);
    font-size: 11.5px;
    line-height: 1.6;
    outline: none;
    background: var(--bg);
    color: var(--fg);
    resize: vertical;
    min-width: 0;
  }
  .call-form textarea:focus {
    border-color: var(--line-strong);
  }
  .result {
    max-height: 200px;
    overflow: auto;
    background: var(--bg-soft);
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 8px 10px;
    font-family: var(--font-mono);
    font-size: 10.5px;
    line-height: 1.6;
    color: var(--fg);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    margin: 0;
  }
  .result.err {
    color: #c0392b;
  }
  .edit-form {
    display: flex;
    flex-direction: column;
    gap: 8px;
    border-top: 1px dashed var(--line);
    padding-top: 12px;
  }
  .edit-form input {
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 8px 10px;
    font-family: var(--font-mono);
    font-size: 12px;
    outline: none;
    background: var(--bg);
    color: var(--fg);
    min-width: 0;
  }
  .edit-form input:focus {
    border-color: var(--line-strong);
  }
  .hdr-row {
    display: grid;
    grid-template-columns: 1fr 1.4fr auto;
    gap: 6px;
    align-items: center;
  }
  .hdr-x {
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    border: none;
    background: transparent;
    color: var(--faint);
    border-radius: 6px;
  }
  .hdr-x:hover {
    background: var(--bg-soft);
    color: #c0392b;
  }
  .hdr-x svg {
    width: 10px;
    height: 10px;
  }
  .hdr-add {
    align-self: flex-start;
    border: none;
    background: transparent;
    color: var(--accent);
    font-size: 11.5px;
    padding: 2px 0;
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
