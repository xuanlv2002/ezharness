<script lang="ts">
  import { onMount } from 'svelte'
  import { api, type ApproveLevel } from '../lib/api'

  /*
  安全：对 agent 的操作管控（区别于应用设置——这里全是 agent 域）。
  工具行与真实工具一一对应。名单概念只有两处：终端工具按命令前缀、
  mcp 按 server.tool（这两类四档：审批/黑名单/白名单/免审）；其余工具
  纯按工具名两档（每次审批 / 全部免审）。
  策略数据由后端下发（toolRules.json），保存即时生效（无需重建 agent）。
  */
  type Level = ApproveLevel
  const simpleLevels: { key: Level; label: string; full: string }[] = [
    { key: 'ask', label: '审批', full: '每次审批' },
    { key: 'auto', label: '免审', full: '全部免审' },
  ]
  const listLevels: { key: Level; label: string; full: string }[] = [
    { key: 'ask', label: '审批', full: '每次审批' },
    { key: 'black', label: '黑名单', full: '黑名单审批：名单外的操作直接放行' },
    { key: 'white', label: '白名单', full: '白名单免审：仅名单内的操作放行' },
    { key: 'auto', label: '免审', full: '全部免审' },
  ]
  type ListKind = 'command' | 'mcpTool'
  const listMeta: Record<ListKind, { label: string; ph: string }> = {
    command: { label: '命令或前缀', ph: 'git status' },
    mcpTool: { label: 'server 或 server.tool', ph: 'github.create_issue' },
  }

  interface RuleRow {
    tool: string
    desc: string
    level: Level
    kind?: ListKind // 有名单能力的工具才有（终端系 command / mcp 系 mcpTool）
    list: string[]
  }

  /* 展示元数据（说明/名单类型）；档位与名单以后端下发为准
  （后端会把内置默认与用户档合并，全部工具都会出现在清单里） */
  const meta: Record<string, { desc: string; kind?: ListKind }> = {
    read_file: { desc: '读取任意文件' },
    write_file: { desc: '写入 / 创建文件' },
    edit_file: { desc: '精确替换文件内容' },
    terminal: { desc: '执行命令（独立进程一次性）', kind: 'command' },
    term_start: { desc: '新建共享终端（可带首条命令）', kind: 'command' },
    term_send: { desc: '向共享终端发送命令 / 控制键', kind: 'command' },
    term_read: { desc: '读取共享终端新输出' },
    term_list: { desc: '列出共享终端' },
    term_close: { desc: '关闭共享终端' },
    browser_tab: { desc: '共享浏览器标签管理（open 新建 / close 关闭；list 只读恒免审）' },
    browser_action: { desc: '页面操作（导航 / 点击 / 输入 / 按键 / 滚动）' },
    browser_read: { desc: '读页面正文 / 链接清单 / 截图（只读）' },
    image_recognize: { desc: '图片识别（识别槽模型驱动，只读）' },
    task: { desc: 'fork 分身执行子任务（分身继承主 agent 策略）' },
    save_app: { desc: '保存快应用 html' },
    ask_user: { desc: '向用户提问收集信息（交互工具）' },
    trim_context: { desc: '模型整理压缩上下文（内部整理）' },
    load_skill: { desc: '加载技能指令集（内部读取）' },
    'mcp.*': {
      desc: 'MCP 工具调用（名单填 server 或 server.tool，如 time.getCurrentTime；发现类恒免审）',
      kind: 'mcpTool',
    },
  }

  let rules = $state<RuleRow[]>([])
  let loaded = $state(false)
  let saving = $state(false)
  let message = $state('')
  let newList = $state<Record<string, string>>({})

  /* 分组展示：文件 / 终端 / 浏览器 / MCP / 系统自带（未知工具归系统自带） */
  const GROUPS: { key: string; label: string; desc: string }[] = [
    { key: 'file', label: '文件工具', desc: '读写与编辑本机文件' },
    { key: 'term', label: '终端工具', desc: '一次性命令与共享终端' },
    { key: 'browser', label: '浏览器工具', desc: '共享浏览器操控' },
    { key: 'mcp', label: 'MCP 工具', desc: '外部 MCP server 提供的工具' },
    { key: 'system', label: '系统自带工具', desc: '分身 / 图片识别 / 快应用等内置能力' },
  ]

  function groupOf(tool: string): string {
    if (/^(read|write|edit)_file$/.test(tool)) return 'file'
    if (tool === 'terminal' || tool.startsWith('term_')) return 'term'
    if (tool.startsWith('browser_')) return 'browser'
    if (tool.startsWith('mcp')) return 'mcp'
    return 'system'
  }

  const hasList = (lv: Level) => lv === 'black' || lv === 'white'

  onMount(async () => {
    try {
      const { rules: rs } = await api.getSecurity()
      rules = rs.map((r) => ({
        tool: r.tool,
        level: r.level,
        list: r.list ?? [],
        desc: meta[r.tool]?.desc ?? '',
        kind: meta[r.tool]?.kind,
      }))
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
      await api.saveSecurity(rules.map(({ tool, level, list }) => ({ tool, level, list })))
      message = '已保存，即时生效'
    } catch (e) {
      message = `保存失败：${(e as Error).message}`
    } finally {
      saving = false
    }
  }

  function setRule(r: RuleRow, lv: Level) {
    if (!r.kind && hasList(lv)) return
    r.level = lv
    void persist()
  }

  function addEntry(r: RuleRow) {
    const v = (newList[r.tool] || '').trim()
    if (v && !r.list.includes(v)) r.list = [...r.list, v]
    newList[r.tool] = ''
    void persist()
  }

  function removeEntry(r: RuleRow, entry: string) {
    r.list = r.list.filter((x) => x !== entry)
    void persist()
  }
</script>

<div class="page">
  <h1>安全</h1>
  <p class="lead">agent 拥有整台设备的权限——这里决定哪些操作需要先经你同意。</p>

  <section>
    <div class="section-head">
      <h2>工具审批策略</h2>
      {#if saving}
        <span class="saving">保存中…</span>
      {/if}
    </div>
    <p class="hint">
      大多数工具两档：每次审批 / 全部免审。终端工具可按命令前缀、MCP 可按
      server.tool 配黑白名单（选黑/白名单时展开配置）。变更自动保存。
    </p>
    {#if message}
      <p class="msg">{message}</p>
    {/if}
    {#if !loaded}
      <p class="hint">加载中…</p>
    {:else if !rules.length}
      <p class="hint">策略为空（后端将按内置默认执行：未配置工具一律审批）。</p>
    {:else}
      {#each GROUPS as g (g.key)}
        {@const groupRules = rules.filter((r) => groupOf(r.tool) === g.key)}
        {#if groupRules.length}
          <div class="group">
            <div class="group-head">
              <h3>{g.label}</h3>
              <span class="group-desc">{g.desc}</span>
            </div>
            <div class="list">
              {#each groupRules as r (r.tool)}
                <div class="rule-wrap">
                  <div class="rule">
                    <div class="info">
                      <span class="name">{r.tool}</span>
                      <span class="desc">{r.desc}</span>
                    </div>
                    <div class="seg" role="radiogroup" aria-label={r.tool}>
                      {#each (r.kind ? listLevels : simpleLevels) as lv (lv.key)}
                        <button
                          class="seg-btn"
                          class:active={r.level === lv.key}
                          onclick={() => setRule(r, lv.key)}
                          title={lv.full}
                        >
                          {lv.label}
                        </button>
                      {/each}
                    </div>
                  </div>
                  {#if r.kind && hasList(r.level)}
                    <div class="list-edit">
                      <p class="list-hint">
                        {r.level === 'black' ? '黑名单' : '白名单'}（{listMeta[r.kind].label}）——{r.level === 'black'
                          ? '命中才审批'
                          : '命中即放行'}
                      </p>
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
                            placeholder={r.kind ? listMeta[r.kind].ph : ''}
                            bind:value={newList[r.tool]}
                            onkeydown={(e) => {
                              if (e.key === 'Enter') {
                                e.preventDefault()
                                addEntry(r)
                              }
                            }}
                          />
                          <button class="add-btn" onclick={() => addEntry(r)} disabled={!(newList[r.tool] || '').trim()}>
                            添加
                          </button>
                        </span>
                      </div>
                    </div>
                  {/if}
                </div>
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
    overflow-y: auto;
    max-width: 720px;
    width: 100%;
    margin: 0 auto;
    padding: 40px 48px 60px;
    display: flex;
    flex-direction: column;
    gap: 30px;
  }
  h1 {
    font-size: 18px;
    font-weight: 700;
  }
  .lead {
    font-size: 12.5px;
    color: var(--muted);
    margin-top: -24px;
  }
  section {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  h2 {
    font-size: 13px;
    font-weight: 700;
    color: var(--muted);
  }
  .hint {
    font-size: 11px;
    color: var(--faint);
    margin-top: -4px;
  }
  .section-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .saving {
    font-size: 11.5px;
    color: var(--faint);
  }
  .msg {
    font-size: 11.5px;
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
    font-size: 12px;
    font-weight: 700;
    color: var(--fg);
  }
  .group-desc {
    font-size: 11px;
    color: var(--faint);
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
  .rule {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    padding: 10px 14px;
  }
  .info {
    display: flex;
    flex-direction: column;
    gap: 1px;
    min-width: 0;
  }
  .name {
    font-family: var(--font-mono);
    font-size: 12.5px;
    color: var(--fg);
  }
  .desc {
    font-size: 11px;
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
    font-size: 11px;
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
  .seg-btn.dim {
    opacity: 0.35;
  }
  .seg-btn:disabled {
    cursor: default;
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
  .list-hint {
    font-size: 10.5px;
    color: var(--faint);
    font-family: var(--font-mono);
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
    font-size: 11.5px;
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
    font-size: 11.5px;
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
    font-size: 11px;
    padding: 4px 8px;
  }
  .add-btn:disabled {
    opacity: 0.4;
    cursor: default;
  }
</style>
