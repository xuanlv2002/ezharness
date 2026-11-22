<script lang="ts">
  import { onMount } from 'svelte'
  import { api, type ApproveLevel, type AuthSession, type OpenapiAuditEntry, type OpenapiToken } from '../lib/api'

  /* 安全：agent 操作管控（一行一策略，键=工具名或"工具名:action"；三类名单 + 档位），保存即时生效 */
  type Level = ApproveLevel
  type ListKind = 'command' | 'path' | 'mcpTool'

  const LABELS: Record<Level, string> = { ask: '审批', black: '黑名单', white: '白名单', auto: '免审' }
  const FULL: Record<Level, string> = {
    ask: '每次审批',
    black: '黑名单审批：名单外的操作直接放行',
    white: '白名单免审：仅名单内的操作放行',
    auto: '全部免审',
  }
  const listMeta: Record<ListKind, { label: string; ph: string }> = {
    command: { label: '命令词（白名单=前缀放行，黑名单=包含拦审）', ph: 'git status' },
    path: { label: '路径前缀（命中即免审）', ph: 'C:/Users/me/projects' },
    mcpTool: { label: 'server 或 server.tool', ph: 'github.create_issue' },
  }

  /* 档位可用集：路径只有白名单（无黑名单），命令/mcp 四档齐全，其余两档 */
  function levelsOf(key: string): Level[] {
    const base = baseOf(key)
    if (base === 'write_file' || base === 'edit_file') return ['ask', 'white', 'auto']
    if (base === 'terminal' || base === 'mcp.*' || key === 'term_action:send') return ['ask', 'black', 'white', 'auto']
    return ['ask', 'auto']
  }
  function kindOf(key: string): ListKind | undefined {
    const base = baseOf(key)
    if (base === 'write_file' || base === 'edit_file') return 'path'
    if (base === 'terminal' || key === 'term_action:send') return 'command'
    if (base === 'mcp.*') return 'mcpTool'
    return undefined
  }
  const baseOf = (key: string) => key.split(':')[0]

  /* 说明文案（按完整 key） */
  const descs: Record<string, string> = {
    read_file: '读取任意文件',
    grep: '递归搜索文件内容（只读）',
    write_file: '写入 / 创建文件',
    edit_file: '精确替换文件内容',
    terminal: '执行命令（独立进程一次性，阻塞到结束/超时）',
    term_tab: '共享终端标签管理（new 新建 / close 关闭；list 只读恒免审）',
    'term_action:send': '向共享终端发送命令（立即返回，不等输出）',
    'term_action:read': '读取共享终端新输出（只读）',
    browser_tab: '共享浏览器标签管理（open 新建 / select 切前台 / close 关闭；list 只读恒免审）',
    'browser_action:navigate': '打开网址',
    'browser_action:read': '读正文 / DOM / 元素 / 链接 / 子帧（只读）',
    'browser_action:eval': '页面内执行 JS（点击 / 填值 / 滚动 / 断言都走这里，可改 DOM）',
    'browser_action:console': '读 / 清控制台（只读，clear 除外）',
    'browser_action:network': '读网络请求（只读）',
    'browser_action:screenshot': '页面截图（只读）',
    'browser_action:upload': '给文件输入框传本机文件',
    task: 'fork 分身执行子任务（分身继承主 agent 策略）',
    'plan:new': '提交任务方案（需用户批准后落盘）',
    'plan:update': '同步任务进度',
    'plan:clear': '清除当前计划',
    save_app: '保存快应用清单与前端',
    load_app: '读取快应用调用文档（只读）',
    memory: '提交长期记忆 / 项目记忆变更（提案在记忆页审阅）',
    model_call: '一次性调用指定模型（识别 / 翻译 / 改写）',
    'media:image': '文生图（贵价：默认每次审批 + 每日配额；异步任务）',
    'media:audio': '文本转语音（贵价：默认每次审批 + 每日配额；异步任务）',
    'media:video': '文生视频（最贵：默认每次审批 + 每日配额最低；异步任务）',
    'media:status': '查询多媒体生成任务状态（只读）',
    'callback:list': '查看定时回调清单（只读）',
    'callback:create': '创建定时回调',
    'callback:update': '修改定时回调配置',
    'callback:delete': '删除定时回调与全部触发记录',
    'callback:enable': '启用定时回调并重算下次运行',
    'callback:disable': '停用定时回调',
    'callback:run': '立即执行一次（不改变计划，可能有副作用）',
    sleep: '定时阻塞等待（等后端就绪/轮询间隔；用户停止即中断）',
    ask_user: '向用户提问收集信息（交互工具）',
    trim_context: '模型整理压缩上下文（内部整理）',
    load_skill: '加载技能指令集（内部读取）',
    'mcp.*': 'MCP 工具调用（名单填 server 或 server.tool；发现类恒免审）',
  }

  interface RuleRow {
    key: string
    desc: string
    level: Level
    list: string[]
  }

  /* 分组展示：文件 / 终端 / 浏览器 / MCP / 系统自带（未知工具归系统自带） */
  const GROUPS: { key: string; label: string; desc: string }[] = [
    { key: 'file', label: '文件工具', desc: '读写与编辑本机文件' },
    { key: 'term', label: '终端工具', desc: '一次性命令与共享终端' },
    { key: 'browser', label: '浏览器工具', desc: '共享浏览器操控' },
    { key: 'media', label: '多媒体工具', desc: '图片 / 语音 / 视频生成与任务查询' },
    { key: 'mcp', label: 'MCP 工具', desc: '外部 MCP server 提供的工具' },
    { key: 'system', label: '系统自带工具', desc: '分身 / 快应用等内置能力' },
  ]

  function groupOf(key: string): string {
    const base = baseOf(key)
    if (base === 'grep' || /^(read|write|edit)_file$/.test(base)) return 'file'
    if (base === 'terminal' || base.startsWith('term_')) return 'term'
    if (base.startsWith('browser_')) return 'browser'
    if (base === 'media') return 'media'
    if (base.startsWith('mcp')) return 'mcp'
    return 'system'
  }

  /* 稳定排序：先按工具、再按 action，避免后端返回顺序抖动导致行乱跳 */
  const BASE_ORDER = ['read_file', 'grep', 'write_file', 'edit_file', 'terminal', 'term_tab', 'term_action', 'browser_tab', 'browser_action', 'media', 'task', 'plan', 'save_app', 'load_app', 'memory', 'model_call', 'callback', 'sleep', 'ask_user', 'trim_context', 'load_skill', 'mcp.*']
  const ACTION_ORDER: Record<string, string[]> = {
    term_action: ['send', 'read'],
    browser_action: ['navigate', 'read', 'eval', 'console', 'network', 'screenshot', 'upload'],
    media: ['image', 'audio', 'video', 'status'],
    plan: ['new', 'update', 'clear'],
    callback: ['list', 'create', 'update', 'delete', 'enable', 'disable', 'run'],
  }
  function orderOf(key: string): number {
    const base = baseOf(key)
    const bi = BASE_ORDER.indexOf(base) * 100
    const ai = key.includes(':') ? (ACTION_ORDER[base]?.indexOf(key.slice(key.indexOf(':') + 1)) ?? 0) + 1 : 0
    return bi + ai
  }

  let rules = $state<RuleRow[]>([])
  let loaded = $state(false)
  let saving = $state(false)
  let message = $state('')
  let newList = $state<Record<string, string>>({})

  const hasList = (lv: Level) => lv === 'black' || lv === 'white'

  onMount(async () => {
    try {
      const { rules: rs } = await api.getSecurity()
      rules = rs
        .map((r) => ({ key: r.key, level: r.level, list: r.list ?? [], desc: descs[r.key] ?? '' }))
        .sort((a, b) => orderOf(a.key) - orderOf(b.key))
    } catch {
      message = '策略加载失败（后端不可达）'
    }
    loaded = true
    void loadCreds()
  })

  /* ---- 凭证管理：OpenAPI 令牌 / 登录会话 / 审计 ---- */
  let oaTokens = $state<OpenapiToken[]>([])
  let sessions = $state<AuthSession[]>([])
  let audit = $state<OpenapiAuditEntry[]>([])
  let newOaName = $state('')
  let creatingOa = $state(false)
  let copiedOa = $state('')
  let credMsg = $state('')

  async function loadCreds(): Promise<void> {
    try {
      const [t, s, a] = await Promise.all([api.openapiTokens(), api.authSessions(), api.openapiAudit()])
      oaTokens = [...t.tokens].reverse()
      sessions = [...s.sessions].reverse()
      audit = a.entries
    } catch {
      credMsg = '凭证信息加载失败'
    }
  }

  async function createOa(): Promise<void> {
    if (creatingOa) return
    creatingOa = true
    credMsg = ''
    try {
      await api.createOpenapiToken(newOaName.trim())
      newOaName = ''
      await loadCreds()
    } catch (e) {
      credMsg = `生成失败：${(e as Error).message}`
    } finally {
      creatingOa = false
    }
  }

  async function revokeOa(id: string): Promise<void> {
    try {
      await api.revokeOpenapiToken(id)
      await loadCreds()
    } catch (e) {
      credMsg = `吊销失败：${(e as Error).message}`
    }
  }

  async function revokeSession(id: string): Promise<void> {
    try {
      await api.revokeSession(id)
      await loadCreds()
    } catch (e) {
      credMsg = `吊销失败：${(e as Error).message}`
    }
  }

  async function copyText(val: string, mark: string): Promise<void> {
    await navigator.clipboard.writeText(val).catch(() => {})
    copiedOa = mark
    setTimeout(() => (copiedOa = ''), 1500)
  }

  const CLIENT_LABELS: Record<string, string> = { desktop: '桌面端', web: '浏览器', cli: '终端' }
  function clientLabel(name: string): string {
    return CLIENT_LABELS[name] || name || '未知端'
  }

  const fmtTime = (iso: string) => {
    const d = new Date(iso)
    return Number.isNaN(d.getTime()) ? iso : d.toLocaleString('zh-CN', { hour12: false })
  }

  /* save 自动保存（档位/名单变更即时提交）。 */
  async function save() {
    if (!rules.length) return
    saving = true
    message = ''
    try {
      await api.saveSecurity(rules.map(({ key, level, list }) => ({ key, level, list })))
      message = '已保存，即时生效'
    } catch (e) {
      message = `保存失败：${(e as Error).message}`
    } finally {
      saving = false
    }
  }

  function setRule(r: RuleRow, lv: Level) {
    r.level = lv
    void save()
  }

  /* 合并式工具（term_action / browser_action）的逐 action 行折叠在工具行里 */
  const TOOL_LEVELS: Level[] = ['ask', 'auto']
  let expanded = $state<Record<string, boolean>>({})
  function toggleSection(base: string) {
    expanded[base] = !expanded[base]
  }
  /* 工具级一键：把该工具所有 action 设成同一档（只改允许该档的项） */
  function setAll(rows: RuleRow[], lv: Level) {
    for (const r of rows) if (levelsOf(r.key).includes(lv)) r.level = lv
    void save()
  }
  /* 全部 action 同档时返回该档，否则空（工具行两个按钮都不点亮） */
  function sharedLevel(rows: RuleRow[]): Level | '' {
    return rows.every((r) => r.level === rows[0].level) ? rows[0].level : ''
  }
  interface Section {
    base: string
    simple?: RuleRow
    rows?: RuleRow[]
  }
  /* 把一组规则切成"段落"：普通工具一段一行；合并式工具一段含它全部 action 行 */
  function sectionsOf(rs: RuleRow[]): Section[] {
    const out: Section[] = []
    const byBase: Record<string, Section> = {}
    for (const r of rs) {
      if (!r.key.includes(':')) {
        out.push({ base: r.key, simple: r })
        continue
      }
      const base = baseOf(r.key)
      if (!byBase[base]) {
        byBase[base] = { base, rows: [] }
        out.push(byBase[base])
      }
      byBase[base].rows!.push(r)
    }
    return out
  }

  function addEntry(r: RuleRow) {
    const v = (newList[r.key] || '').trim()
    if (v && !r.list.includes(v)) r.list = [...r.list, v]
    newList[r.key] = ''
    void save()
  }

  function removeEntry(r: RuleRow, entry: string) {
    r.list = r.list.filter((x) => x !== entry)
    void save()
  }
</script>

<!-- 名单编辑框（终端命令 / 文件路径 / mcp 共用） -->
{#snippet listEditor(r: RuleRow)}
  {@const kind = kindOf(r.key) ?? 'command'}
  <div
    class="list-edit"
    title={`${r.level === 'black' ? '黑名单' : '白名单'}（${listMeta[kind].label}）——${r.level === 'black' ? '命中才审批' : '命中即放行'}`}
  >
    <div class="chips">
      {#each r.list as entry (entry)}
        <span class="chip">
          {entry}
          <button class="chip-x" onclick={() => removeEntry(r, entry)} title="移除">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round">
              <path d="M6 6l12 12M18 6L6 18" />
            </svg>
          </button>
        </span>
      {/each}
      <span class="chip-add">
        <input
          type="text"
          placeholder={listMeta[kind].ph}
          bind:value={newList[r.key]}
          onkeydown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              addEntry(r)
            }
          }}
        />
        <button class="add-btn" onclick={() => addEntry(r)} disabled={!(newList[r.key] || '').trim()}>添加</button>
      </span>
    </div>
  </div>
{/snippet}

<!-- 一条策略行（sub = 折叠在工具行下的逐 action 行） -->
{#snippet ruleRow(r: RuleRow, label: string, sub: boolean)}
  <div class="rule-wrap" class:sub>
    <div class="rule">
      <div class="info">
        <span class="name">{label}</span>
        {#if r.desc}
          <span class="desc">{r.desc}</span>
        {/if}
      </div>
      <div class="seg" role="radiogroup" aria-label={r.key}>
        {#each levelsOf(r.key) as lv (lv)}
          <button class="seg-btn" class:active={r.level === lv} onclick={() => setRule(r, lv)} title={FULL[lv]}>{LABELS[lv]}</button>
        {/each}
      </div>
    </div>
    {#if kindOf(r.key) && hasList(r.level)}
      {@render listEditor(r)}
    {/if}
  </div>
{/snippet}

<div class="page">
  <h1>安全</h1>
  <p class="lead">agent 拥有整台设备的权限——这里决定哪些操作需要先经你同意。</p>

  <section>
    <div class="section-head">
      <div>
        <h2
          title="多数工具两档：每次审批 / 全部免审。终端命令与 MCP 可配黑白名单（白名单按前缀命中放行，黑名单按包含命中拦下审批）；写文件可配路径前缀白名单。合并式工具（term_action、browser_action）先展开逐 action 单独配档，也可以直接用工具行右边的「全部审批 / 全部免审」一键定。变更自动保存。"
          >工具审批策略</h2
        >
        <p class="section-desc">决定 agent 哪些操作要先征得同意；改动即时保存。</p>
      </div>
      {#if saving}
        <span class="saving">保存中…</span>
      {/if}
    </div>
    {#if message}
      <p class="msg">{message}</p>
    {/if}
    {#if !loaded}
      <p class="hint">加载中…</p>
    {:else if !rules.length}
      <p class="hint">策略为空（后端将按内置默认执行：未配置工具一律审批）。</p>
    {:else}
      {#each GROUPS as g (g.key)}
        {@const groupRules = rules.filter((r) => groupOf(r.key) === g.key)}
        {#if groupRules.length}
          <div class="group">
            <div class="group-head">
              <h3 title={g.desc}>{g.label}</h3>
              <span class="group-desc">{g.desc}</span>
            </div>
            <div class="list">
              {#each sectionsOf(groupRules) as sec (sec.simple ? `one:${sec.base}` : `actions:${sec.base}`)}
                {#if sec.simple}
                  {@render ruleRow(sec.simple, sec.simple.key, false)}
                {:else}
                  {@const rows = sec.rows!}
                  {@const shared = sharedLevel(rows)}
                  <div class="rule-wrap tool">
                    <div class="rule">
                      <div class="info tool">
                        <button class="expander" onclick={() => toggleSection(sec.base)} title="展开 / 收起逐 action">
                          {expanded[sec.base] ? '▾' : '▸'}
                        </button>
                        <div class="labels">
                          <span class="name">{sec.base}</span>
                          <span class="desc">{rows.length} 个动作{shared === 'ask' ? ' · 全部审批' : shared === 'auto' ? ' · 全部免审' : ' · 逐项'}</span>
                        </div>
                      </div>
                      <div class="seg" role="radiogroup" aria-label={`${sec.base} 全部动作`}>
                        {#each TOOL_LEVELS as lv (lv)}
                          <button class="seg-btn" class:active={shared === lv} onclick={() => setAll(rows, lv)} title={`全部${LABELS[lv]}`}>
                            {LABELS[lv]}
                          </button>
                        {/each}
                      </div>
                    </div>
                    {#if expanded[sec.base]}
                      {#each rows as r (r.key)}
                        {@render ruleRow(r, r.key.slice(r.key.indexOf(':') + 1), true)}
                      {/each}
                    {/if}
                  </div>
                {/if}
              {/each}
            </div>
          </div>
        {/if}
      {/each}
    {/if}
  </section>

  <section>
    <div class="section-head">
      <div>
        <h2 title="给脚本、快应用、外部程序调 /api/open/* 用的令牌（请求头 EzHarnessOpenToken）。OpenAPI 与登录会话完全独立，只认这枚令牌；core 启动时若没有令牌会自动生成一枚「默认」，多枚可命名、吊销即失效。"
          >OpenAPI 令牌<span class="count">{oaTokens.length}</span></h2
        >
        <p class="section-desc">OpenAPI 是独立模块，只认这里的令牌、不弹登录；core 启动自动准备一枚「默认」，脚本与终端读 openapi.json 显式传入即可，也可另生成多枚。</p>
      </div>
    </div>
    {#if credMsg}
      <p class="msg">{credMsg}</p>
    {/if}
    <div class="panel">
      <div class="create-row">
        <input type="text" bind:value={newOaName} placeholder="令牌名称，如 番茄钟后端" />
        <button class="primary" disabled={creatingOa} onclick={() => void createOa()}>{creatingOa ? '生成中…' : '生成'}</button>
      </div>
      {#if oaTokens.length}
        <ul class="rows">
          {#each oaTokens as t (t.id)}
            <li class="row">
              <div class="row-main">
                <span class="token-name">{t.name || '未命名'}</span>
                <span class="token">{t.token.slice(0, 10)}…</span>
              </div>
              <div class="row-meta">
                <span>创建于 {fmtTime(t.created_at)}</span>
              </div>
              <div class="row-actions">
                <button class="mini" onclick={() => void copyText(t.token, t.id)}>{copiedOa === t.id ? '已复制' : '复制'}</button>
                <button class="mini danger" onclick={() => void revokeOa(t.id)}>吊销</button>
              </div>
            </li>
          {/each}
        </ul>
      {:else}
        <p class="empty">令牌清单为空；重启 core 会自动补一枚「默认」令牌，也可以现在生成一枚。</p>
      {/if}
    </div>
  </section>

  <section>
    <div class="section-head">
      <div>
        <h2 title="每个端（desktop/cli/浏览器）登录后各持一枚会话令牌，持久有效；吊销后该端下次访问需重新登录。"
          >登录会话<span class="count">{sessions.length}</span></h2
        >
        <p class="section-desc">每个端登录一次就多一枚；看客户端、来源 IP 和登录时间，吊销后该端需要重新登录。</p>
      </div>
      <button class="mini" onclick={() => void loadCreds()}>刷新</button>
    </div>
    <div class="panel">
      {#if sessions.length}
        <ul class="rows">
          {#each sessions as s (s.id)}
            <li class="row">
              <div class="row-main">
                <span class="badge">{clientLabel(s.name)}</span>
                <span class="sid">#{s.id}</span>
              </div>
              <div class="row-meta">
                <span>{s.ip || '来源未知'}</span>
                <span class="sep">·</span>
                <span>{fmtTime(s.created_at)}</span>
              </div>
              <div class="row-actions">
                <button class="mini danger" onclick={() => void revokeSession(s.id)}>吊销</button>
              </div>
            </li>
          {/each}
        </ul>
      {:else}
        <p class="empty">暂无登录会话。</p>
      {/if}
    </div>
  </section>

  <section>
    <div class="section-head">
      <div>
        <h2 title="/api/open/* 的每笔调用（含被拒的 401）都落审计：谁（令牌）、何时、调了什么、结果如何。"
          >OpenAPI 调用审计<span class="count">{audit.length}</span></h2
        >
        <p class="section-desc">/api/open/* 的每笔调用都记一笔，含被拒的 401。</p>
      </div>
      <button class="mini" onclick={() => void loadCreds()}>刷新</button>
    </div>
    <div class="panel">
      {#if audit.length}
        <ul class="rows">
          {#each audit as e, i (e.time + e.path + i)}
            <li class="row">
              <div class="row-main">
                <span class="method">{e.method}</span>
                <span class="path">{e.path}</span>
              </div>
              <div class="row-meta">
                <span class="badge">{e.token_name || '未知'}</span>
                <span>{fmtTime(e.time)}</span>
              </div>
              <div class="row-actions">
                <span class="status" class:bad={e.status >= 400}>{e.status}</span>
              </div>
            </li>
          {/each}
        </ul>
      {:else}
        <p class="empty">暂无调用记录。</p>
      {/if}
    </div>
  </section>
</div>

<style>
  .page {
    flex: 1;
    min-height: 0;
    /* 全宽滚动容器（滚动条贴窗口右缘）；内容由直接子项各自限宽居中 */
    overflow-y: auto;
    padding: 40px 48px 60px;
    display: flex;
    flex-direction: column;
    gap: 30px;
  }
  h1 {
    font-size: var(--font-size-page);
    font-weight: 700;
  }
  .lead {
    font-size: var(--font-size-section);
    color: var(--muted);
    margin-top: -24px;
  }
  section {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  h2 {
    font-size: var(--font-size-body);
    font-weight: 600;
    color: var(--fg);
  }
  .count {
    margin-left: 7px;
    font-size: var(--font-size-small);
    font-weight: 500;
    color: var(--faint);
  }
  .section-desc {
    margin-top: 2px;
    font-size: var(--font-size-ui);
    color: var(--muted);
  }
  .hint {
    font-size: var(--font-size-small);
    color: var(--faint);
    margin-top: -4px;
  }
  .section-head {
    display: flex;
    align-items: flex-end;
    justify-content: space-between;
    gap: 12px;
  }
  .section-head > div {
    min-width: 0;
  }
  .saving {
    flex: none;
    font-size: var(--font-size-ui);
    color: var(--faint);
  }
  .msg {
    font-size: var(--font-size-ui);
    color: var(--muted);
    margin-top: -2px;
  }
  .group {
    display: flex;
    flex-direction: column;
    gap: 8px;
    margin-top: 12px;
  }
  .group-head {
    display: flex;
    align-items: baseline;
    gap: 8px;
    padding: 0 2px;
  }
  .group-head h3 {
    font-size: var(--font-size-ui);
    font-weight: 700;
    color: var(--fg);
  }
  .group-head h3::before {
    content: '';
    display: inline-block;
    width: 5px;
    height: 5px;
    margin-right: 6px;
    border-radius: 50%;
    background: var(--faint);
    vertical-align: 1px;
  }
  .group-desc {
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .list {
    display: flex;
    flex-direction: column;
    border: 1px solid var(--line);
    border-radius: 12px;
    overflow: hidden;
    background: var(--bg);
  }
  .rule-wrap + .rule-wrap {
    border-top: 1px solid var(--line);
  }
  /* 合并式工具行：整行浅底作组头，展开后逐 action 行回到普通白底 */
  .rule-wrap.tool {
    background: var(--bg-soft);
  }
  .info.tool {
    flex-direction: row;
    align-items: flex-start;
    gap: 7px;
  }
  .labels {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .expander {
    width: 16px;
    padding: 0;
    border: none;
    background: transparent;
    color: var(--faint);
    font-size: var(--font-size-small);
    line-height: 1.5;
    flex: none;
  }
  .expander:hover {
    color: var(--fg);
  }
  .rule {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    padding: 9px 14px;
    transition: background var(--dur-fast) var(--ease-out);
  }
  .rule:hover {
    background: color-mix(in srgb, var(--bg-soft) 55%, transparent);
  }
  .rule-wrap.tool .rule:hover {
    background: color-mix(in srgb, var(--bg-soft) 55%, var(--line));
  }
  .rule-wrap.tool .seg {
    background: color-mix(in srgb, var(--bg-soft) 55%, var(--line));
  }
  .rule-wrap.sub .rule {
    padding-left: 32px;
  }
  .info {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .name {
    font-family: var(--font-mono);
    font-size: var(--font-size-section);
    color: var(--fg);
    line-height: 1.5;
  }
  .desc {
    font-size: var(--font-size-small);
    color: var(--faint);
    line-height: 1.5;
  }
  /* 分段控件：浅槽 + 淡蓝活动块，避免一排大黑块抢视线 */
  .seg {
    flex: none;
    display: flex;
    align-items: center;
    gap: 2px;
    padding: 2px;
    border: 1px solid var(--line);
    border-radius: 8px;
    background: var(--bg-soft);
  }
  .seg-btn {
    border: none;
    background: transparent;
    border-radius: 6px;
    padding: 4px 9px;
    font-size: var(--font-size-small);
    font-weight: 500;
    color: var(--muted);
    white-space: nowrap;
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .seg-btn:hover {
    color: var(--fg);
  }
  .seg-btn.active {
    background: var(--accent-soft);
    color: var(--accent);
  }
  .list-edit {
    padding: 0 14px 12px 34px;
    display: flex;
    flex-direction: column;
    gap: 8px;
    animation: expand var(--dur-fast) var(--ease-out) both;
  }
  @keyframes expand {
    from {
      opacity: 0;
      transform: translateY(-4px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    align-items: center;
  }
  .chip {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    border: 1px solid var(--line);
    border-radius: 7px;
    padding: 4px 6px 4px 10px;
    font-family: var(--font-mono);
    font-size: var(--font-size-ui);
    color: var(--fg);
    background: var(--bg);
    max-width: 100%;
    overflow-wrap: anywhere;
  }
  .chip-x {
    display: grid;
    place-items: center;
    width: 16px;
    height: 16px;
    border: none;
    background: transparent;
    color: var(--faint);
    border-radius: 50%;
    flex: none;
  }
  .chip-x:hover {
    background: var(--line);
    color: var(--fg);
  }
  .chip-x svg {
    width: 8px;
    height: 8px;
  }
  .chip-add {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    border: 1px dashed var(--line);
    border-radius: 7px;
    padding: 2px 4px 2px 8px;
  }
  .chip-add input {
    border: none;
    outline: none;
    background: transparent;
    font-family: var(--font-mono);
    font-size: var(--font-size-ui);
    color: var(--fg);
    width: 130px;
    min-width: 0;
  }
  .chip-add input::placeholder {
    color: var(--faint);
  }
  .add-btn {
    border: none;
    background: transparent;
    color: var(--accent);
    font-size: var(--font-size-small);
    padding: 4px 8px;
  }
  .add-btn:disabled {
    opacity: 0.4;
    cursor: default;
  }

  /* 凭证管理（令牌/会话/审计）：一条条可读的行，不用小号表格 */
  .panel {
    border: 1px solid var(--line);
    border-radius: 12px;
    overflow: hidden;
  }
  .create-row {
    display: flex;
    gap: 8px;
    padding: 10px 12px;
  }
  .create-row + .rows,
  .create-row + .empty {
    border-top: 1px solid var(--line);
  }
  .create-row input {
    flex: 1;
    min-width: 0;
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 7px 10px;
    background: var(--bg);
    color: var(--fg);
    font-size: var(--font-size-ui);
    outline: none;
    transition: border-color var(--dur-fast) var(--ease-out);
  }
  .create-row input:focus {
    border-color: var(--line-strong);
  }
  .create-row input::placeholder {
    color: var(--faint);
  }
  .primary {
    flex: none;
    border: 1px solid var(--line-strong);
    background: var(--bg-invert);
    color: var(--fg-invert);
    border-radius: 8px;
    padding: 0 14px;
    font-size: var(--font-size-ui);
    font-weight: 500;
  }
  .primary:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .rows {
    list-style: none;
    display: flex;
    flex-direction: column;
  }
  .row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
    gap: 3px 12px;
    padding: 10px 12px;
  }
  .row + .row {
    border-top: 1px solid var(--line);
  }
  .row-main {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }
  .row-meta {
    grid-column: 1;
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
    min-width: 0;
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .row-actions {
    grid-column: 2;
    grid-row: 1 / span 2;
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .token-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: var(--font-ui);
    font-size: var(--font-size-ui);
    font-weight: 500;
    color: var(--fg);
  }
  .badge {
    flex: none;
    display: inline-flex;
    align-items: center;
    border: 1px solid var(--line);
    border-radius: 999px;
    padding: 1px 8px;
    background: var(--bg-soft);
    font-size: var(--font-size-small);
    color: var(--muted);
  }
  .sid,
  .token,
  .method,
  .path {
    font-family: var(--font-mono);
  }
  .sid {
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .token {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--font-size-small);
    color: var(--muted);
  }
  .method {
    flex: none;
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .path {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--font-size-small);
    color: var(--fg);
  }
  .status {
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    color: var(--muted);
  }
  .status.bad {
    color: var(--danger);
  }
  .sep {
    color: var(--line);
  }
  .empty {
    padding: 14px 12px;
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .mini {
    flex: none;
    border: 1px solid var(--line);
    background: transparent;
    color: var(--fg);
    border-radius: 6px;
    font-size: var(--font-size-small);
    padding: 3px 9px;
    cursor: pointer;
    transition:
      border-color var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .mini:hover {
    border-color: var(--accent);
    color: var(--accent);
  }
  .mini.danger:hover {
    border-color: var(--danger);
    color: var(--danger);
  }
</style>
