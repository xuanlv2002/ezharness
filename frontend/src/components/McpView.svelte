<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import { api, McpAuthError, type McpServerView, type McpToolView } from '../lib/api'
  import { openExternal } from '../lib/desktop'
  import { store } from '../lib/store.svelte'

  /* MCP = 外部工具服务器（mcp.json 热加载）。页面侧可手动建立 MCP 会话
  （与 agent 的连接相互独立）：点连接查看工具清单、填参试调用。
  传输三档：http（Streamable HTTP）/ sse（旧协议 SSE）/ stdio（子进程）。 */
  let servers = $state<McpServerView[]>([])
  let loaded = $state(false)
  let saving = $state(false)
  let message = $state('')
  let adding = $state(false)
  let draft = $state(emptyDraft())
  let editing = $state(false)
  let editDraft = $state(emptyEditDraft())
  let delTarget = $state<McpServerView | null>(null)

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

  /* OAuth 认证（页面侧）：授权中 server 名 + 覆盖式授权弹窗 + 状态轮询 */
  let authBusy = $state('')
  let authModal = $state<{ server: string; opened: boolean; err: string } | null>(null)
  let authTimer: ReturnType<typeof setInterval> | null = null
  function stopAuthPoll() {
    if (authTimer) {
      clearInterval(authTimer)
      authTimer = null
    }
  }
  onDestroy(stopAuthPoll)

  /* 顶栏搜索跳转：直达指定 server 详情（消费后清空锚点） */
  $effect(() => {
    const anchor = store.jumpAnchor
    if (!anchor.startsWith('mcp:') || !loaded) return
    store.jumpAnchor = ''
    const s = servers.find((x) => x.name === anchor.slice('mcp:'.length))
    if (s) selected = s
  })

  /* 进详情时滚动归零——滚动容器在视图切换间被复用，否则停留在网格页的滚动偏移。 */
  $effect(() => {
    if (selected && detailPage) detailPage.scrollTop = 0
  })

  interface HeaderPair {
    key: string
    value: string
  }

  interface Draft {
    name: string
    desc: string
    type: 'http' | 'sse' | 'stdio'
    url: string
    pairs: HeaderPair[]
    command: string
    argsText: string
    envPairs: HeaderPair[]
  }

  function emptyDraft(): Draft {
    return { name: '', desc: '', type: 'http', url: '', pairs: [], command: '', argsText: '', envPairs: [] }
  }

  interface EditDraft {
    desc: string
    url: string
    pairs: HeaderPair[]
    command: string
    argsText: string
    envPairs: HeaderPair[]
  }

  function emptyEditDraft(): EditDraft {
    return { desc: '', url: '', pairs: [], command: '', argsText: '', envPairs: [] }
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

  /* persist 自动保存（启停/添加/删除即时提交，下一轮热加载生效；后端会顺带
  关闭已删除/禁用 server 的页面会话）。串行化：payload 在排队前取当下快照、
  请求链式排队——删除后立刻添加时并发全量覆盖会把新 server 冲掉（后到旧
  载荷胜出），排队后天然"最新快照最后落盘"。 */
  let persistChain: Promise<void> = Promise.resolve()
  function persist() {
    saving = true
    message = ''
    const payload = {
      servers: servers.map((s) => ({
        name: s.name,
        ...(s.description ? { description: s.description } : {}),
        type: s.transport,
        ...(s.transport === 'stdio'
          ? {
              command: s.command,
              args: s.args ?? [],
              ...(Object.keys(s.env ?? {}).length ? { env: s.env } : {}),
            }
          : {
              url: s.endpoint,
              ...(Object.keys(s.headers ?? {}).length ? { headers: s.headers } : {}),
            }),
        ...(s.allow?.length ? { allow: s.allow } : {}), // 透传白名单，防全量保存清掉
        ...(s.oauth_config?.enabled ? { oauth: s.oauth_config } : {}), // 透传 OAuth 配置，防全量保存清掉
        enabled: s.enabled,
      })),
    }
    persistChain = persistChain
      .then(() => api.saveMcp(payload))
      .then(() => {
        message = '已保存，下一轮生效'
      })
      .catch((e) => {
        message = `保存失败：${(e as Error).message}`
      })
      .finally(() => {
        saving = false
      })
    return persistChain
  }

  function toggle(s: McpServerView) {
    s.enabled = !s.enabled
    persist()
  }

  /* 名字即 keyed each 的 key 与后端索引键，重名会炸掉整页渲染，必须唯一。 */
  function draftValid(): boolean {
    const name = draft.name.trim()
    if (!name) return false
    if (servers.some((x) => x.name === name)) return false
    if (draft.type === 'stdio') return !!draft.command.trim()
    return !!draft.url.trim()
  }

  async function addServer() {
    if (!draftValid()) return
    const isStdio = draft.type === 'stdio'
    const args = argsOf(draft.argsText)
    const command = draft.command.trim()
    const entry: McpServerView = {
      name: draft.name.trim(),
      description: draft.desc.trim(),
      transport: draft.type,
      endpoint: isStdio ? commandLine(command, args) : draft.url.trim(),
      command: isStdio ? command : '',
      args: isStdio ? args : [],
      env: isStdio ? pairsToRecord(draft.envPairs) : {},
      enabled: true,
      connected: false,
      tools: 0,
      headers: isStdio ? {} : pairsToRecord(draft.pairs),
      oauth: false, // OAuth 自动检测：保存后探测，命中即亮未授权徽标
      authorized: false,
    }
    servers = [...servers, entry]
    draft = emptyDraft()
    adding = false
    await persist()
    if (!isStdio) void probeOAuth(entry.name)
  }

  /* probeOAuth 新建保存后探测该 server 是否需要 OAuth（探测 well-known 发现文档；
     未命中不亮徽标——后续真实调用仍会经 401 自动识别）。
     命中后必须经 servers 数组取响应式实例更新：局部 entry 是原始对象，
     直接改它不触发渲染。 */
  async function probeOAuth(name: string) {
    try {
      const { oauth } = await api.oauthProbe(name)
      if (!oauth) return
      const s = servers.find((x) => x.name === name)
      if (s) {
        s.oauth = true
        s.authorized = false
      }
    } catch {
      /* 探测失败静默 */
    }
  }

  function pairsToRecord(pairs: HeaderPair[]): Record<string, string> {
    const out: Record<string, string> = {}
    for (const p of pairs) if (p.key.trim()) out[p.key.trim()] = p.value
    return out
  }

  /* argsOf 参数文本按行拆分（每行一个，路径含空格不被拆坏）。 */
  function argsOf(text: string): string[] {
    return text
      .split('\n')
      .map((l) => l.trim())
      .filter(Boolean)
  }

  function commandLine(command: string, args: string[]): string {
    return args.length ? `${command} ${args.join(' ')}` : command
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
      url: s.transport === 'stdio' ? '' : s.endpoint,
      pairs: Object.entries(s.headers ?? {}).map(([key, value]) => ({ key, value })),
      command: s.command ?? '',
      argsText: (s.args ?? []).join('\n'),
      envPairs: Object.entries(s.env ?? {}).map(([key, value]) => ({ key, value })),
    }
  }

  function editValid(): boolean {
    const s = selected
    if (!s) return false
    if (s.transport === 'stdio') return !!editDraft.command.trim()
    return !!editDraft.url.trim()
  }

  function saveEdit() {
    const s = selected
    if (!s || !editValid()) return
    s.description = editDraft.desc.trim()
    if (s.transport === 'stdio') {
      s.command = editDraft.command.trim()
      s.args = argsOf(editDraft.argsText)
      s.env = pairsToRecord(editDraft.envPairs)
      s.endpoint = commandLine(s.command, s.args)
    } else {
      s.endpoint = editDraft.url.trim()
      s.headers = pairsToRecord(editDraft.pairs)
    }
    editing = false
    persist()
  }

  /* authorize 取授权 URL 用系统浏览器打开并轮询；已授权即刷新徽标。
     metadataUrl 来自 401（发现授权服务器用）。 */
  async function authorize(s: McpServerView, metadataUrl = '') {
    authBusy = s.name
    message = ''
    stopAuthPoll()
    authModal = { server: s.name, opened: false, err: '' }
    try {
      const { url } = await api.oauthStart(s.name, metadataUrl)
      if (!url) throw new Error('未获得授权地址（检查该 server 的 OAuth 配置）')
      openExternal(url)
      authModal = { server: s.name, opened: true, err: '' }
      const started = Date.now()
      authTimer = setInterval(async () => {
        if (Date.now() - started > 5 * 60_000) {
          stopAuthPoll()
          return
        }
        try {
          const { authorized } = await api.oauthStatus(s.name)
          if (authorized) {
            stopAuthPoll()
            s.authorized = true
            authModal = null
          }
        } catch {
          /* 下一轮重试 */
        }
      }, 1500)
    } catch (e) {
      authModal = { server: s.name, opened: false, err: (e as Error).message }
    }
    authBusy = ''
  }

  async function logout(s: McpServerView) {
    try {
      await api.oauthLogout(s.name)
    } catch {
      /* 幂等 */
    }
    s.authorized = false
  }

  /* 删除走自制居中确认框（原生 confirm 在 WebView 里贴顶，样式突兀）。 */
  function removeServer(s: McpServerView) {
    delTarget = s
  }

  function doRemove() {
    const s = delTarget
    if (!s) return
    servers = servers.filter((x) => x.name !== s.name)
    delete connectedTools[s.name]
    if (openTool.startsWith(s.name + '/')) openTool = ''
    if (selected === s) selected = null
    delTarget = null
    persist()
  }

  /* connect 建立（或复用）页面会话，成功即展开工具清单。 */
  async function connect(s: McpServerView) {
    connecting = s.name
    message = ''
    try {
      const { tools } = await api.connectMcp(s.name)
      /* 工具行 keyed by name：server 端重名/空名工具去重，防重复 key 崩渲染 */
      const list = (tools ?? []).filter((t) => t.name).filter((t, i, a) => a.findIndex((x) => x.name === t.name) === i)
      s.connected = true
      s.tools = list.length
      connectedTools[s.name] = list
    } catch (e) {
      s.connected = false
      s.tools = 0
      if (e instanceof McpAuthError) {
        // 自动检测到需 OAuth：翻出认证入口并直接发起授权（无需用户预先配置）
        s.oauth = true
        s.authorized = false
        message = `该服务需要 OAuth 认证：${s.name}`
        void authorize(s, e.metadataUrl)
      } else {
        message = `连接失败：${(e as Error).message}`
      }
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
      if (e instanceof McpAuthError) {
        const s = servers.find((x) => x.name === server)
        if (s) {
          s.oauth = true
          s.authorized = false
          void authorize(s, e.metadataUrl)
        }
        callResult = { ok: false, text: `需要 OAuth 认证：${server}。请在弹窗完成授权后重试。` }
      } else {
        callResult = { ok: false, text: (e as Error).message }
      }
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
        <div class="badges">
          <span class="state" class:on={selected.connected && selected.enabled}>
            <i></i>
            {selected.enabled ? (selected.connected ? (selected.tools >= 0 ? `已连接 · ${selected.tools} 工具` : '已连接') : '未连接') : '已停用'}
          </span>
          {#if selected.oauth}
            <span class="state" class:on={selected.authorized} class:warn={!selected.authorized}>
              <i></i>
              {selected.authorized ? '已授权' : '未授权'}
            </span>
          {/if}
        </div>
        {#if selected.description && selected.description !== selected.name}
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
        onclick={() => (sel.connected && connectedTools[sel.name]?.length ? disconnect(sel) : connect(sel))}
      >
        {connecting === selected.name ? '连接中…' : selected.connected ? (connectedTools[selected.name]?.length ? '断开会话' : '拉取工具') : '建立会话'}
      </button>
      <button class="op" onclick={startEdit}>{editing ? '收起编辑' : '编辑'}</button>
      {#if selected.oauth && selected.authorized}
        <button class="op" onclick={() => logout(sel)}>注销</button>
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
        {#if selected.transport === 'stdio'}
          <input type="text" placeholder="命令（如 python3、npx、uvx）" bind:value={editDraft.command} />
          <textarea spellcheck="false" rows="3" placeholder="参数，每行一个（如 path/to/server.py）" bind:value={editDraft.argsText}></textarea>
          {#each editDraft.envPairs as p, j}
            <div class="hdr-row">
              <input type="text" placeholder="环境变量名（如 UV_PYTHON）" bind:value={p.key} />
              <input type="text" placeholder="值（如 3.12）" bind:value={p.value} />
              <button class="hdr-x" onclick={() => (editDraft.envPairs = editDraft.envPairs.filter((_, k) => k !== j))} title="移除">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round">
                  <path d="M6 6l12 12M18 6L6 18" />
                </svg>
              </button>
            </div>
          {/each}
          <button class="hdr-add" onclick={() => (editDraft.envPairs = [...editDraft.envPairs, { key: '', value: '' }])}>+ 环境变量</button>
        {:else}
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
        {/if}
        <div class="add-actions">
          <button class="add-ok" disabled={!editValid()} onclick={saveEdit}>保存</button>
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
      {:else if selected.tools === 0}
        <p class="lead">已连接，该 server 未暴露工具。</p>
      {:else}
        <p class="lead">会话已建立，点击「拉取工具」查看工具清单。</p>
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
            <div class="badges">
              <span class="state" class:on={s.connected && s.enabled}><i></i>{s.enabled ? (s.connected ? (s.tools >= 0 ? `已连接 · ${s.tools} 工具` : '已连接') : '未连接') : '已停用'}</span>
              {#if s.oauth}
                <span class="state" class:on={s.authorized} class:warn={!s.authorized}><i></i>{s.authorized ? '已授权' : '未授权'}</span>
              {/if}
            </div>
          </div>
          <h2>{s.name}</h2>
          {#if s.description && s.description !== s.name}
            <p class="card-desc" title={s.description}>{s.description}</p>
          {/if}
          <p class="endpoint">
            <i>{s.transport}</i>{s.endpoint}
          </p>
          <div class="foot">
            <span class="tools">
              {s.connected ? (s.tools >= 0 ? `${s.tools} 个工具` : '工具未拉取') : '点击查看'}
              {#if (s.transport === 'http' || s.transport === 'sse') && Object.keys(s.headers ?? {}).length > 0}
                <i>· {Object.keys(s.headers).length} 个请求头</i>
              {/if}
            </span>
            <div class="foot-ops">
              <button
                class="conn"
                class:on={s.connected}
                class:auth={s.oauth && !s.authorized}
                disabled={connecting === s.name}
                onclick={(e) => {
                  e.stopPropagation()
                  s.connected && connectedTools[s.name]?.length ? disconnect(s) : connect(s)
                }}
              >
                {connecting === s.name ? '…' : s.connected ? (connectedTools[s.name]?.length ? '断开' : '拉取工具') : '连接'}
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

  <button class="add" onclick={() => (adding = true)}>+ 添加服务器</button>
</div>
{/if}

<!-- 添加服务器弹框：表单独立于卡片，不再把页面撑长 -->
{#if adding}
  <div class="drawer-mask" onclick={() => (adding = false)}></div>
  <div class="modal add-modal" role="dialog" aria-modal="true" aria-label="添加 MCP 服务器">
    <header class="add-head">
      <div>
        <h3>添加 MCP 服务器</h3>
        <p class="modal-hint">接入后 agent 获得对应工具，保存即热加载。</p>
      </div>
      <button class="modal-x" onclick={() => (adding = false)} title="关闭">✕</button>
    </header>
    <div class="add-form">
      <input type="text" placeholder="服务器名（如 github）" bind:value={draft.name} />
      <input type="text" placeholder="描述（供 agent 发现用，如 GitHub 仓库搜索）" bind:value={draft.desc} />
      <select bind:value={draft.type}>
        <option value="http">http</option>
        <option value="sse">sse</option>
        <option value="stdio">stdio</option>
      </select>
      {#if draft.type === 'stdio'}
        <input type="text" placeholder="命令（如 python3、npx、uvx）" bind:value={draft.command} />
        <textarea spellcheck="false" rows="3" placeholder="参数，每行一个（如 path/to/server.py）" bind:value={draft.argsText}></textarea>
        <p class="form-sec">环境变量（可选）</p>
        {#each draft.envPairs as p, j}
          <div class="hdr-row">
            <input type="text" placeholder="变量名（如 UV_PYTHON）" bind:value={p.key} />
            <input type="text" placeholder="值（如 3.12）" bind:value={p.value} />
            <button class="hdr-x" onclick={() => (draft.envPairs = draft.envPairs.filter((_, k) => k !== j))} title="移除">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round">
                <path d="M6 6l12 12M18 6L6 18" />
              </svg>
            </button>
          </div>
        {/each}
        <button class="hdr-add" onclick={() => (draft.envPairs = [...draft.envPairs, { key: '', value: '' }])}>+ 环境变量</button>
      {:else}
        <input type="text" placeholder="https://example.com/mcp" bind:value={draft.url} />
        <p class="form-sec">鉴权请求头（可选）</p>
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
    </div>
    <footer>
      <button class="btn" onclick={() => (adding = false)}>取消</button>
      <button class="btn primary" disabled={!draftValid()} onclick={addServer}>添加</button>
    </footer>
  </div>
{/if}

<!-- MCP OAuth 授权弹窗（页面侧）：去授权 → 系统浏览器打开 → 轮询状态 -->
{#if authModal}
  <div class="drawer-mask" onclick={() => (authModal = null)}></div>
  <div class="modal confirm">
    <h3>OAuth 认证</h3>
    <p class="modal-hint">
      正在为「{authModal.server}」申请授权。
      {#if authModal.opened}
        已打开系统浏览器，请在浏览器中完成授权，完成后此弹窗会自动关闭。
      {/if}
    </p>
    {#if authModal.err}
      <p class="modal-hint err">{authModal.err}</p>
    {/if}
    <footer>
      <button class="btn" onclick={() => (authModal = null)}>关闭</button>
      {#if !authModal.opened}
        <button class="btn" onclick={() => (servers.find((x) => x.name === authModal!.server) && authorize(servers.find((x) => x.name === authModal!.server)!))}>重试</button>
      {/if}
    </footer>
  </div>
{/if}

<!-- 删除 server 确认弹窗（居中，样式同 MemoryView 删除技能） -->
{#if delTarget}
  <div class="drawer-mask" onclick={() => (delTarget = null)}></div>
  <div class="modal confirm">
    <h3>删除 MCP server</h3>
    <p class="modal-hint">确定删除「{delTarget.name}」？配置将从 mcp.json 移除，已建立的页面会话会一并关闭。</p>
    <footer>
      <button class="btn" onclick={() => (delTarget = null)}>取消</button>
      <button class="btn danger" onclick={doRemove}>删除</button>
    </footer>
  </div>
{/if}

<style>
  .page {
    flex: 1;
    min-height: 0;
    /* 全宽滚动容器（滚动条贴窗口右缘）；内容由直接子项各自限宽居中 */
    overflow-y: auto;
    padding: 40px 48px 60px;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  .head h1 {
    font-size: var(--font-size-page);
    font-weight: 700;
  }
  .head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
  }
  .lead {
    font-size: var(--font-size-ui);
    color: var(--muted);
    margin-top: 4px;
  }
  .saving {
    font-size: var(--font-size-ui);
    color: var(--faint);
  }
  .msg {
    font-size: var(--font-size-ui);
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
    font-size: var(--font-size-ui);
  }
  .add:hover {
    border-color: var(--line-strong);
    color: var(--fg);
  }
  .add-form {
    display: flex;
    flex-direction: column;
    gap: 10px;
    flex: 1;
    min-height: 0;
    overflow-y: auto;
  }
  .add-form input,
  .add-form select,
  .add-form textarea {
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 8px 10px;
    font-family: var(--font-mono);
    font-size: var(--font-size-ui);
    outline: none;
    background: var(--bg);
    color: var(--fg);
  }
  .add-form textarea {
    resize: vertical;
    line-height: 1.6;
  }
  .add-form input:focus,
  .add-form select:focus,
  .add-form textarea:focus {
    border-color: var(--accent);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 12%, transparent);
  }
  /* select 去原生外观（Windows 系统控件白底与表单不协调，同 ModelsView），自绘下拉箭头 */
  .add-form select {
    appearance: none;
    padding-right: 30px;
    cursor: pointer;
    background: var(--bg) url("data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' width='10' height='6' viewBox='0 0 10 6'><path d='M1 1l4 4 4-4' fill='none' stroke='%23888' stroke-width='1.6' stroke-linecap='round' stroke-linejoin='round'/></svg>") no-repeat right 10px center;
  }
  .form-sec {
    font-size: var(--font-size-small);
    color: var(--faint);
    margin-top: 2px;
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
    font-size: var(--font-size-ui);
    font-weight: 500;
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
    font-size: var(--font-size-ui);
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
    width: 30px;
    height: 30px;
    border-radius: 9px;
    background: var(--bg-soft);
    border: 1px solid var(--line);
    color: var(--muted);
  }
  .glyph.live {
    background: color-mix(in srgb, var(--accent) 11%, transparent);
    border-color: color-mix(in srgb, var(--accent) 26%, transparent);
    color: var(--accent);
  }
  .glyph svg {
    width: 15px;
    height: 15px;
  }
  .state {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-size: var(--font-size-small);
    font-family: var(--font-mono);
    color: var(--faint);
    border: 1px solid var(--line);
    border-radius: 99px;
    padding: 3px 9px;
    line-height: 1;
    white-space: nowrap;
  }
  .state i {
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: var(--faint);
    flex: none;
  }
  .state.on {
    color: var(--accent);
    border-color: color-mix(in srgb, var(--accent) 32%, transparent);
    background: color-mix(in srgb, var(--accent) 7%, transparent);
  }
  .state.on i {
    background: var(--accent);
  }
  /* 未授权：琥珀色，提示需要操作（区别于连接态/错误态） */
  .state.warn {
    color: #b8860b;
    border-color: color-mix(in srgb, #d29922 40%, transparent);
    background: color-mix(in srgb, #d29922 10%, transparent);
  }
  .state.warn i {
    background: #d29922;
  }
  /* badges 徽标行：自动宽度、不随父级拉伸（详情标题列 / 卡片顶栏共用） */
  .badges {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
    align-self: flex-start;
  }
  h2 {
    font-size: var(--font-size-section);
    font-weight: 600;
    color: var(--fg);
    margin-top: 3px;
  }
  /* 卡片描述：展示但压缩两行，全文见 title 与详情页 */
  .card-desc {
    margin: 0;
    font-size: var(--font-size-small);
    line-height: 1.5;
    color: var(--faint);
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .detail-desc {
    font-size: var(--font-size-ui);
    color: var(--muted);
  }
  .endpoint {
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
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
    font-size: var(--font-size-small);
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
    font-size: var(--font-size-small);
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
    font-size: var(--font-size-ui);
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
    font-size: var(--font-size-page);
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
    font-size: var(--font-size-ui);
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
    font-size: var(--font-size-ui);
    padding: 5px 12px;
    border-radius: 8px;
  }
  .op:hover {
    border-color: var(--line-strong);
    color: var(--fg);
  }
  .op.danger:hover {
    border-color: var(--danger);
    color: var(--danger);
  }
  .op-label {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    font-size: var(--font-size-ui);
    color: var(--muted);
    margin-left: auto;
  }
  .conn {
    border: 1px solid var(--line);
    background: transparent;
    color: var(--muted);
    font-size: var(--font-size-small);
    font-weight: 500;
    padding: 3.5px 11px;
    border-radius: 99px;
  }
  .conn:hover {
    border-color: var(--line-strong);
    color: var(--fg);
  }
  /* 认证按钮：需要授权时用强调色，吸引导航 */
  .conn.auth {
    color: var(--accent);
    border-color: color-mix(in srgb, var(--accent) 34%, transparent);
    background: color-mix(in srgb, var(--accent) 7%, transparent);
  }
  .conn.auth:hover {
    border-color: var(--accent);
    color: var(--accent);
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
    font-size: var(--font-size-ui);
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
    font-size: var(--font-size-ui);
    font-weight: 600;
    color: var(--fg);
  }
  .tool-desc {
    font-size: var(--font-size-small);
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
    font-size: var(--font-size-small);
    line-height: 1.5;
    color: var(--muted);
    overflow-wrap: anywhere;
  }
  .call-form textarea {
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 8px 10px;
    font-family: var(--font-mono);
    font-size: var(--font-size-ui);
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
    font-size: var(--font-size-small);
    line-height: 1.6;
    color: var(--fg);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    margin: 0;
  }
  .result.err {
    color: var(--danger);
  }
  .edit-form {
    display: flex;
    flex-direction: column;
    gap: 8px;
    border-top: 1px dashed var(--line);
    padding-top: 12px;
  }
  .edit-form textarea {
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 8px 10px;
    font-family: var(--font-mono);
    font-size: var(--font-size-ui);
    line-height: 1.6;
    outline: none;
    background: var(--bg);
    color: var(--fg);
    resize: vertical;
    min-width: 0;
  }
  .edit-form textarea:focus {
    border-color: var(--line-strong);
  }
  .edit-form input {
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 8px 10px;
    font-family: var(--font-mono);
    font-size: var(--font-size-ui);
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
    /* minmax(0,…) 压掉 1fr 的内容固有宽下限:placeholder 长文不再把行撑出容器 */
    grid-template-columns: minmax(0, 1fr) minmax(0, 1.4fr) auto;
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
    color: var(--danger);
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
    font-size: var(--font-size-ui);
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

  /* ── 删除确认弹窗（居中，同 MemoryView 删除技能） ── */
  .drawer-mask {
    position: fixed;
    inset: 0;
    z-index: 58;
    background: rgb(0 0 0 / 18%);
  }
  .modal {
    position: fixed;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    z-index: 59;
    display: flex;
    flex-direction: column;
    gap: 12px;
    width: min(340px, 92vw);
    background: var(--bg);
    border: 1px solid var(--line-strong);
    border-radius: 14px;
    box-shadow: 0 12px 40px rgb(0 0 0 / 18%);
    padding: 18px 20px;
  }
  .modal h3 {
    font-size: var(--font-size-body);
    font-weight: 700;
    color: var(--fg);
    margin: 0;
  }
  /* 添加服务器：比确认框宽，表单区独立滚动，长表单不撑破视口 */
  .add-modal {
    width: min(480px, 92vw);
    max-height: 86vh;
  }
  .add-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 16px;
  }
  .modal-x {
    flex: none;
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    border: 1px solid var(--line);
    border-radius: 8px;
    background: transparent;
    color: var(--muted);
    font-size: var(--font-size-small);
    cursor: pointer;
  }
  .modal-x:hover {
    border-color: var(--line-strong);
    color: var(--fg);
  }
  .btn.primary {
    background: var(--bg-invert);
    border-color: var(--bg-invert);
    color: var(--fg-invert);
  }
  .btn.primary:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .modal-hint {
    font-size: var(--font-size-small);
    line-height: 1.6;
    color: var(--faint);
    margin: 0;
  }
  .modal-hint.err {
    color: #e5534b;
  }
  .modal footer {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }
  .btn {
    border: 1px solid var(--line);
    border-radius: 8px;
    background: transparent;
    color: var(--muted);
    font: inherit;
    font-size: var(--font-size-ui);
    padding: 7px 16px;
    cursor: pointer;
  }
  .btn:hover {
    border-color: var(--line-strong);
    color: var(--fg);
  }
  .btn.danger {
    background: #c0392b;
    border-color: #c0392b;
    color: #fff;
  }
</style>
