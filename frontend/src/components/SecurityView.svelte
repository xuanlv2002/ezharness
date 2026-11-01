<script lang="ts">
  import { onMount } from 'svelte'
  import { api, type ApproveLevel } from '../lib/api'

  /*
  安全：对 agent 的操作管控（区别于应用设置——这里全是 agent 域）。
  一行一条策略，键是工具名，或"工具名:action"（term_action / browser_action
  逐 action 配档）。名单概念只有三类：终端命令（terminal、term_action:send）、
  文件路径（write_file/edit_file 白名单）、mcp（server.tool）；其中命令与 mcp
  四档（审批/黑名单/白名单/免审），路径只有 审批/白名单/免审，其余工具纯按
  工具名两档（每次审批 / 全部免审）。策略数据由后端下发（toolRules.json），
  保存即时生效（无需重建 agent）。
  */
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
    save_app: '保存快应用 html',
    load_app: '读取快应用调用文档（只读）',
    callback: '定时回调管理（增删改 / 启停 / 立即执行）',
    'callback:list': '查看定时回调清单（只读）',
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
    { key: 'mcp', label: 'MCP 工具', desc: '外部 MCP server 提供的工具' },
    { key: 'system', label: '系统自带工具', desc: '分身 / 快应用等内置能力' },
  ]

  function groupOf(key: string): string {
    const base = baseOf(key)
    if (/^(read|write|edit)_file$/.test(base)) return 'file'
    if (base === 'terminal' || base.startsWith('term_')) return 'term'
    if (base.startsWith('browser_')) return 'browser'
    if (base.startsWith('mcp')) return 'mcp'
    return 'system'
  }

  /* 稳定排序：先按工具、再按 action，避免后端返回顺序抖动导致行乱跳 */
  const BASE_ORDER = ['read_file', 'write_file', 'edit_file', 'terminal', 'term_tab', 'term_action', 'browser_tab', 'browser_action', 'task', 'plan', 'save_app', 'load_app', 'callback', 'sleep', 'ask_user', 'trim_context', 'load_skill', 'mcp.*']
  const ACTION_ORDER: Record<string, string[]> = {
    term_action: ['send', 'read'],
    browser_action: ['navigate', 'read', 'eval', 'console', 'network', 'screenshot', 'upload'],
    plan: ['new', 'update', 'clear'],
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
  })

  /* persist 自动保存（档位/名单变更即时提交）。 */
  async function persist() {
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
    void persist()
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
    void persist()
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
    void persist()
  }

  function removeEntry(r: RuleRow, entry: string) {
    r.list = r.list.filter((x) => x !== entry)
    void persist()
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
      <div class="info" title={r.desc}>
        <span class="name">{label}</span>
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
      <h2
        title="多数工具两档：每次审批 / 全部免审。终端命令与 MCP 可配黑白名单（白名单按前缀命中放行，黑名单按包含命中拦下审批）；写文件可配路径前缀白名单。合并式工具（term_action、browser_action）先展开逐 action 单独配档，也可以直接用工具行右边的「全部审批 / 全部免审」一键定。变更自动保存。"
        >工具审批策略</h2
      >
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
            </div>
            <div class="list">
              {#each sectionsOf(groupRules) as sec (sec.simple ? `one:${sec.base}` : `actions:${sec.base}`)}
                {#if sec.simple}
                  {@render ruleRow(sec.simple, sec.simple.key, false)}
                {:else}
                  {@const rows = sec.rows!}
                  {@const shared = sharedLevel(rows)}
                  <div class="rule-wrap">
                    <div class="rule">
                      <div class="info tool">
                        <button class="expander" onclick={() => toggleSection(sec.base)} title="展开 / 收起逐 action">
                          {expanded[sec.base] ? '▾' : '▸'}
                        </button>
                        <span class="name">{sec.base}</span>
                        <span class="desc">{rows.length} 个动作{shared === 'ask' ? ' · 全部审批' : shared === 'auto' ? ' · 全部免审' : ' · 逐项'}</span>
                      </div>
                      <div class="seg" role="radiogroup" aria-label={sec.base}>
                        {#each TOOL_LEVELS as lv (lv)}
                          <button class="seg-btn" class:active={shared === lv} onclick={() => setAll(rows, lv)} title={FULL[lv]}>
                            全部{LABELS[lv]}
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
    gap: 10px;
  }
  h2 {
    font-size: var(--font-size-section);
    font-weight: 600;
    color: var(--muted);
  }
  .hint {
    font-size: var(--font-size-small);
    color: var(--faint);
    margin-top: -4px;
  }
  .section-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .saving {
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
    gap: 10px;
    padding: 0 2px;
  }
  .group-head h3 {
    font-size: var(--font-size-ui);
    font-weight: 700;
    color: var(--fg);
  }
  .list {
    display: flex;
    flex-direction: column;
    border: 1px solid var(--line);
    border-radius: 12px;
    overflow: hidden;
  }
  .rule-wrap + .rule-wrap {
    border-top: 1px solid var(--line);
  }
  /* 工具行（合并式工具）：展开钮 + 工具名 + 动作数，右侧一键全审/全免 */
  .info.tool {
    flex-direction: row;
    align-items: baseline;
    gap: 8px;
  }
  .expander {
    width: 16px;
    padding: 0;
    border: none;
    background: transparent;
    color: var(--faint);
    font-size: var(--font-size-small);
    line-height: 1;
    flex: none;
    align-self: center;
  }
  .expander:hover {
    color: var(--fg);
  }
  .rule {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    padding: 10px 14px;
  }
  .rule-wrap.sub .rule {
    padding-left: 26px;
  }
  .info {
    display: flex;
    flex-direction: column;
    gap: 1px;
    min-width: 0;
  }
  .name {
    font-family: var(--font-mono);
    font-size: var(--font-size-section);
    color: var(--fg);
  }
  .desc {
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .seg {
    flex: none;
    display: flex;
    border: 1px solid var(--line);
    border-radius: 9px;
    overflow: hidden;
  }
  .seg-btn {
    border: none;
    background: transparent;
    padding: 5px 11px;
    font-size: var(--font-size-small);
    color: var(--muted);
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .seg-btn + .seg-btn {
    border-left: 1px solid var(--line);
  }
  .seg-btn:hover {
    color: var(--fg);
    background: var(--bg-soft);
  }
  .seg-btn.active {
    background: var(--bg-invert);
    color: var(--fg-invert);
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
</style>
