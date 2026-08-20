<script lang="ts">
  import { onMount } from 'svelte'
  import { api, type ApproveLevel } from '../lib/api'

  /*
  安全：对 agent 的操作管控（区别于应用设置——这里全是 agent 域）。
  四档策略：每次审批 / 黑名单审批（名单外放行）/ 白名单免审（名单内放行）/ 全部免审。
  策略数据由后端下发（settings.json 的 toolRules），保存即时生效（无需重建 agent）。
  */
  type Level = ApproveLevel
  const levels: { key: Level; label: string; full: string }[] = [
    { key: 'ask', label: '审批', full: '每次审批' },
    { key: 'black', label: '黑名单', full: '黑名单审批：名单外的操作直接放行' },
    { key: 'white', label: '白名单', full: '白名单免审：仅名单内的操作放行' },
    { key: 'auto', label: '免审', full: '全部免审' },
  ]
  type ListKind = 'command' | 'path' | 'tool'
  const listMeta: Record<ListKind, { label: string; ph: string }> = {
    command: { label: '命令或前缀', ph: 'git status' },
    path: { label: '路径或前缀', ph: 'C:\\Projects\\' },
    tool: { label: 'server 或 server.tool', ph: 'github.create_issue' },
  }

  interface RuleRow {
    tool: string
    desc: string
    level: Level
    kind: ListKind
    list: string[]
    noList?: boolean // 不支持名单档（如 task：分身继承主 agent 策略）
  }

  /* 展示元数据（说明/名单类型/约束）；档位与名单以后端下发为准 */
  const meta: Record<string, { desc: string; kind: ListKind; noList?: boolean }> = {
    read_file: { desc: '读取任意文件', kind: 'path' },
    write_file: { desc: '写入 / 创建文件', kind: 'path' },
    edit_file: { desc: '精确替换文件内容', kind: 'path' },
    bash: { desc: '执行命令', kind: 'command' },
    task: { desc: 'fork 分身执行子任务（分身继承主 agent 策略）', kind: 'tool', noList: true },
    'mcp.*': { desc: '全部 MCP 服务器的工具（细粒度后续在 MCP 页配）', kind: 'tool' },
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
      rules = rs.map((r) => ({
        tool: r.tool,
        level: r.level,
        list: r.list ?? [],
        desc: meta[r.tool]?.desc ?? '',
        kind: meta[r.tool]?.kind ?? 'tool',
        noList: meta[r.tool]?.noList,
      }))
    } catch {
      message = '策略加载失败（后端不可达）'
    }
    loaded = true
  })

  async function save() {
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
    if (r.noList && hasList(lv)) return
    r.level = lv
  }

  function addEntry(r: RuleRow) {
    const v = (newList[r.tool] || '').trim()
    if (v && !r.list.includes(v)) r.list = [...r.list, v]
    newList[r.tool] = ''
  }

  function removeEntry(r: RuleRow, entry: string) {
    r.list = r.list.filter((x) => x !== entry)
  }
</script>

<div class="page">
  <h1>安全</h1>
  <p class="lead">agent 拥有整台设备的权限——这里决定哪些操作需要先经你同意。</p>

  <section>
    <h2>工具审批策略</h2>
    <p class="hint">
      四档：每次审批 → 黑名单审批（名单外放行）→ 白名单免审（名单内放行）→ 全部免审。
      选黑/白名单时展开对应名单配置。
    </p>
    <div class="list">
      {#each rules as r (r.tool)}
        <div class="rule-wrap">
          <div class="rule">
            <div class="info">
              <span class="name">{r.tool}</span>
              <span class="desc">{r.desc}</span>
            </div>
            <div class="seg" role="radiogroup" aria-label={r.tool}>
              {#each levels as lv (lv.key)}
                <button
                  class="seg-btn"
                  class:active={r.level === lv.key}
                  class:dim={r.noList && hasList(lv.key)}
                  disabled={r.noList && hasList(lv.key)}
                  onclick={() => setRule(r, lv.key)}
                  title={lv.full}
                >
                  {lv.label}
                </button>
              {/each}
            </div>
          </div>
          {#if hasList(r.level)}
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
                    placeholder={listMeta[r.kind].ph}
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
