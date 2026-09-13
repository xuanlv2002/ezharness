<script lang="ts">
  import { onMount } from 'svelte'
  import { api, type HistoryMessage, type MemoryConfig, type MemorySkillEntry, type SessionNode } from '../lib/api'
  import { store } from '../lib/store.svelte'

  /*
  记忆 = 长期记忆/能力记忆两个文件夹（GET /api/memory/config）+ 会话树
  （GET /api/memory/tree）：话题记忆展示完整会话树——每条分支（线）从根
  到当前叶，compact 旧世代标"已归档"，fork 分叉可见；操作：回顾（只读
  展开）、切到分支（跳对话页）、手动归档、删整条线。
  */
  let { onNavigate }: { onNavigate?: (v: string) => void } = $props()

  let cfg = $state<MemoryConfig | null>(null)
  let tree = $state<SessionNode[] | null>(null)
  let message = $state('')

  /* 回顾侧边抽屉：目标节点 + 只读内容（消息 + 压缩摘要） */
  let reviewNode = $state<SessionNode | null>(null)
  let reviewMsgs = $state<HistoryMessage[]>([])
  let reviewSummary = $state('')
  let reviewLoading = $state(false)

  async function loadTree() {
    try {
      const t = await api.getMemoryTree()
      tree = t
      // 默认全展开（树规模桌面尺度；有孩子才需要进集合；虚拟根默认张开）
      expanded = new Set(['null', ...t.filter((n) => t.some((k) => k.targetId === n.id)).map((n) => n.id)])
    } catch {
      message = '会话树加载失败（后端不可达）'
    }
  }

  onMount(async () => {
    try {
      cfg = await api.getMemoryConfig()
    } catch {
      message = '记忆数据加载失败（后端不可达）'
    }
    await loadTree()
  })

  /* 回顾：侧边抽屉只读全文；压缩新叶几乎无消息，摘要段给上下文 */
  async function reviewTopic(n: SessionNode) {
    reviewNode = n
    reviewLoading = true
    try {
      const d = await api.getTopic(n.id)
      reviewMsgs = d.messages || []
      reviewSummary = (d.summary || '').replace(/<\/?compact-summary>/g, '').trim()
    } catch (e) {
      message = `回顾失败：${(e as Error).message}`
    } finally {
      reviewLoading = false
    }
  }

  function closeReview() {
    reviewNode = null
    reviewMsgs = []
    reviewSummary = ''
  }

  /* 切到分支：该节点所属线的当前叶恢复为活动会话并跳对话页 */
  async function switchLine(n: SessionNode) {
    if (!n.lineRoot) return
    try {
      await store.switchBranch(n.lineRoot)
      onNavigate?.('chat')
    } catch (e) {
      message = `切换分支失败：${(e as Error).message}`
    }
  }

  function fmtSize(n: number): string {
    if (n < 1024) return `${n} B`
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
    return `${(n / 1024 / 1024).toFixed(1)} MB`
  }

  /* 技能管理：启停（乐观更新，失败回滚）与删除（confirm 后物理删目录） */
  async function toggleSkill(s: MemorySkillEntry) {
    const next = !s.enabled
    s.enabled = next
    try {
      await api.toggleSkill(s.id, next)
    } catch (e) {
      s.enabled = !next
      message = `技能启停失败：${errText(e)}`
    }
  }

  /* 删除确认弹窗：原生 confirm 在 WebView 里贴顶，自制居中确认框 */
  let delTarget = $state<MemorySkillEntry | null>(null)

  async function doDeleteSkill() {
    const s = delTarget
    delTarget = null
    if (!s) return
    try {
      await api.deleteSkill(s.id)
      if (cfg) cfg.skills.items = cfg.skills.items.filter((x) => x.id !== s.id)
    } catch (e) {
      message = `删除技能失败：${errText(e)}`
    }
  }

  /* 新建技能弹窗：选 zip 直接上传（技能名由后端从压缩包推导） */
  let skillModal = $state(false)
  let skZip = $state<{ name: string; size: number; data: string } | null>(null)
  let skBusy = $state(false)
  let skErr = $state('')
  let zipInput = $state<HTMLInputElement | null>(null)

  function openSkillModal() {
    skillModal = true
    skZip = null
    skErr = ''
  }

  function pickZip(e: Event) {
    const f = (e.target as HTMLInputElement).files?.[0]
    if (!f) return
    const r = new FileReader()
    r.onload = () => {
      const url = String(r.result || '')
      skZip = { name: f.name, size: f.size, data: url.slice(url.indexOf(',') + 1) }
    }
    r.readAsDataURL(f)
  }

  async function createSkill() {
    if (!skZip) return
    skBusy = true
    skErr = ''
    try {
      await api.createSkill(skZip.data)
      skillModal = false
      cfg = await api.getMemoryConfig()
    } catch (e) {
      skErr = errText(e)
    } finally {
      skBusy = false
    }
  }

  /* 从 "400: {"error":"xx"}" 形态的报错里提取后端信息 */
  function errText(e: unknown): string {
    const m = /\{"error":"([^"]*)"/.exec((e as Error).message)
    return m ? m[1] : (e as Error).message
  }

  /* 会话树（目录式，节点树模型）：
     虚拟 null 根 → 顶层 = new 根（每条新线一个根节点）；归档世代沿
     compress 边向下成链（每代是上一代的子，树的纵深）；fork 挂在
     fork 源会话的父下（兄弟位：与源同级，⑂ 徽标标出处）。叶子节点
     即当前可进入的分支。guides 是各级祖先的竖向导轨列。 */
  type Row = {
    n: SessionNode | null // null = 虚拟根
    depth: number
    kids: number
    open: boolean
    guides: number
    parentTitle?: string
  }
  let expanded = $state<Set<string>>(new Set())

  function toggle(id: string) {
    const next = new Set(expanded)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    expanded = next
  }

  const treeRows = $derived.by(() => {
    if (!tree) return [] as Row[]
    const byId = new Map(tree.map((n) => [n.id, n]))
    const kids = new Map<string, SessionNode[]>() // parent id → 子节点（compress 子代 + fork 兄弟）
    const roots: SessionNode[] = []
    for (const n of tree) {
      let parent = ''
      if (n.seedKind === 'compress' && n.targetId) {
        parent = n.targetId // 归档换代：挂在旧代之下（纵深链）
      } else if (n.seedKind === 'fork' && n.targetId) {
        // 兄弟位：fork 与源会话同父（源是根则 fork 也是顶层）
        const src = byId.get(n.targetId)
        parent = src?.seedKind === 'compress' && src.targetId ? src.targetId : ''
      }
      if (parent && byId.has(parent)) {
        kids.set(parent, [...(kids.get(parent) || []), n])
      } else {
        roots.push(n)
      }
    }
    roots.sort((a, b) => b.createdAt - a.createdAt)
    const out: Row[] = []
    const nullOpen = expanded.has('null')
    out.push({ n: null, depth: 0, kids: roots.length, open: nullOpen, guides: 0 })
    if (!nullOpen) return out
    const walk = (n: SessionNode, depth: number, parentTitle: string) => {
      const ch = (kids.get(n.id) || []).sort((a, b) => b.createdAt - a.createdAt)
      const open = expanded.has(n.id)
      out.push({ n, depth, kids: ch.length, open, guides: depth - 1, parentTitle })
      if (open) for (const c of ch) walk(c, depth + 1, n.title)
    }
    for (const r of roots) walk(r, 1, '')
    return out
  })

  /* fork 徽章里的源名截断展示（全文走 title 悬浮） */
  function shortTitle(s: string | undefined, k = 16): string {
    if (!s) return '源会话'
    return s.length > k ? s.slice(0, k) + '…' : s
  }

  function fmtDate(ts: number): string {
    if (!ts) return ''
    const d = new Date(ts)
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
  }
</script>

<div class="page">
  <h1>记忆</h1>
  <p class="lead">agent 的记忆由三个文件夹构成，路径可在设置中配置。</p>
  {#if message}
    <p class="lead err">{message}</p>
  {/if}

  <!-- ── 长期记忆 ── -->
  <section>
    <header>
      <div>
        <h2>长期记忆</h2>
        <p class="hint">harness.md 索引随上下文初始加载，其余文件由 agent 按需检索。</p>
      </div>
    </header>
    <p class="dir"><span>📁</span>{cfg ? cfg.longterm.dir : '—'}</p>
    <div class="list">
      {#if cfg}
        {#if cfg.longterm.harnessMd}
          <div class="file index">
            <span class="name">harness.md</span>
            <span class="badge">初始加载</span>
            <span class="stat">{fmtSize(cfg.longterm.harnessMd.size)} · {cfg.longterm.harnessMd.mtime}</span>
          </div>
        {/if}
        {#each cfg.longterm.files as f (f.name)}
          <div class="file">
            <span class="name">{f.name}</span>
            <span class="stat">{fmtSize(f.size)} · {f.mtime}</span>
          </div>
        {/each}
        {:else}
        <div class="empty">暂无文件——agent 会把重要的用户偏好与事实沉淀到这里。</div>
      {/if}
    </div>
  </section>

  <!-- ── 能力记忆 ── -->
  <section>
    <header>
      <div>
        <h2>能力记忆</h2>
        <p class="hint">沉淀的 skill——做过一次的复杂操作固化为可复用的能力。</p>
      </div>
    </header>
    <p class="dir"><span>📁</span>{cfg ? cfg.skills.dir : '—'}</p>
    <div class="grid">
      {#if cfg}
        {#each cfg.skills.items as s (s.id)}
          <div class="card" class:off={!s.enabled}>
            <div class="card-top">
              <div class="glyph">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M12 3l1.9 5.6L20 10l-5 3.6L16.5 20 12 16.6 7.5 20 9 13.6 4 10l6.1-1.4L12 3z" />
                </svg>
              </div>
              <div class="card-ops">
                <button class="del" title="删除技能" onclick={() => (delTarget = s)}>
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                    <path d="M3 6h18M8 6V4h8v2M6 6l1 14h10l1-14M10 11v6M14 11v6" />
                  </svg>
                </button>
                <span
                  class="toggle"
                  class:on={s.enabled}
                  role="switch"
                  aria-checked={s.enabled}
                  tabindex="0"
                  onclick={() => toggleSkill(s)}
                  onkeydown={(e) => (e.key === 'Enter' || e.key === ' ') && toggleSkill(s)}
                  title={s.enabled ? '禁用技能' : '启用技能'}>
                  <i></i>
                </span>
              </div>
            </div>
            <h3>{s.name}</h3>
            <p class="card-desc">{s.desc}</p>
          </div>
        {/each}
        <button class="card ghost newskill" onclick={openSkillModal}>
          <div class="card-top">
            <div class="glyph ghost-glyph">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                <path d="M12 5v14M5 12h14" />
              </svg>
            </div>
          </div>
          <h3>新建技能</h3>
          <p class="card-desc">上传 zip 压缩包，把重复性工作流沉淀为可复用的能力。</p>
        </button>
      {:else}
        <button class="card ghost newskill" onclick={openSkillModal}>
          <div class="card-top">
            <div class="glyph ghost-glyph">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                <path d="M12 5v14M5 12h14" />
              </svg>
            </div>
          </div>
          <h3>暂无技能</h3>
          <p class="card-desc">上传 zip 新建技能，重复性工作流可沉淀为 skill。</p>
        </button>
      {/if}
    </div>
  </section>

  <!-- ── 话题记忆：完整会话树（目录式）── -->
  <section>
    <header>
      <div>
        <h2>话题记忆</h2>
        <p class="hint">完整会话树：一级 = 分支（⑂ 为分叉新线），线内向下是压缩世代；已归档不再参与恢复。</p>
      </div>
    </header>
    <p class="dir"><span>📁</span>{cfg ? cfg.topics.dir : '—'}</p>
    <div class="treelist">
      {#if treeRows.length > 0}
        {#each treeRows as row, i (`${row.n?.id ?? 'null'}-${i}`)}
          {#if row.n === null}
            <div class="trow root">
              <button class="tw" onclick={() => toggle('null')} title={row.open ? '收起全部分支' : '展开全部分支'}>
                <span class="farrow" class:open={row.open}>{row.open ? '▾' : '▸'}</span>
              </button>
              <span class="name mono">sessions</span>
              <span class="desc">{row.kids} 条分支</span>
            </div>
          {:else}
            {@const n = row.n}
            <div class="trow" class:cur={n.isActiveLine && n.isLeaf}>
              {#each Array(row.guides) as _, g (g)}
                <span class="guide"></span>
              {/each}
              {#if row.kids > 0}
                <button class="tw" onclick={() => toggle(n.id)} title={row.open ? '收起世代' : '展开世代'}>
                  <span class="farrow" class:open={row.open}>{row.open ? '▾' : '▸'}</span>
                </button>
              {:else}
                <span class="tw dot">·</span>
              {/if}
              <div class="info">
                <span class="name">
                  <span class="n-text">{n.title || '未命名会话'}</span>
                  {#if n.archived}<span class="kbadge arc">已归档</span>{/if}
                  {#if n.seedKind === 'fork'}<span class="kbadge" title={n.forkedFrom?.title ? `来自 session：${n.forkedFrom.title}` : 'fork 产生的 session'}>⑂ {shortTitle(n.forkedFrom?.title)}</span>{/if}
                  {#if n.seedKind === 'compress'}<span class="kbadge" title={row.parentTitle ? `来自「${row.parentTitle}」压缩生成` : '压缩生成'}>⇪ 压缩生成</span>{/if}
                  {#if n.msgs === 0}<span class="kbadge mut">空</span>{/if}
                </span>
                <span class="desc">{fmtDate(n.createdAt)} · {n.msgs} 条消息</span>
              </div>
              <div class="ops">
                <button class="op" onclick={() => reviewTopic(n)}>回顾</button>
                {#if n.isLeaf && n.lineRoot && !(n.isActiveLine)}
                  <button class="op" onclick={() => switchLine(n)} title="切到该分支的当前叶继续">切到分支</button>
                {/if}
                <button class="op" disabled title="手动归档：后续版本开放">归档</button>
              </div>
            </div>
          {/if}
        {/each}
      {:else if tree}
        <div class="empty">暂无会话——在对话页开聊后会沉淀到这里。</div>
      {:else}
        <div class="empty">会话树加载中…</div>
      {/if}
    </div>
  </section>
</div>

<!-- 回顾侧边抽屉：完整消息 + 压缩摘要 -->
{#if reviewNode}
  <div class="drawer-mask" onclick={closeReview}></div>
  <aside class="drawer">
    <header>
      <div class="dtitle">
        <span class="name">{reviewNode.title || '未命名会话'}</span>
        {#if reviewNode.archived}<span class="kbadge arc">已归档</span>{/if}
        {#if reviewNode.seedKind === 'fork'}<span class="kbadge">⑂</span>{/if}
        {#if reviewNode.seedKind === 'compress'}<span class="kbadge">⇪ 压缩生成</span>{/if}
      </div>
      <button class="dclose" onclick={closeReview} title="关闭">✕</button>
    </header>
    {#if reviewSummary}
      <div class="dsum">
        <div class="dsum-tag">⇪ 压缩摘要（本会话开始前的上下文）</div>
        <div class="dsum-text">{reviewSummary}</div>
      </div>
    {/if}
    <div class="dmsgs">
      {#if reviewLoading}
        <div class="rv">加载中…</div>
      {:else}
        {#each reviewMsgs as m, j (j)}
          <div class="rv" class:me={m.role === 'user'}>
            <span class="rrole">{m.role === 'assistant' ? 'agent' : m.role === 'tool' ? 'tool' : m.role}</span>
            <span class="rtext">{m.content || (m.tool_calls ? JSON.stringify(m.tool_calls) : '')}</span>
          </div>
        {/each}
        {#if reviewMsgs.length === 0 && !reviewSummary}
          <div class="rv">（无消息）</div>
        {/if}
      {/if}
    </div>
  </aside>
{/if}

<!-- 新建技能弹窗：选 zip 直接上传（技能名由后端从压缩包推导） -->
{#if skillModal}
  <div class="drawer-mask" onclick={() => (skillModal = false)}></div>
  <div class="modal">
    <header>
      <h3>新建技能</h3>
      <button class="dclose" onclick={() => (skillModal = false)} title="关闭">✕</button>
    </header>
    <div class="fld">
      <span>压缩包（.zip）</span>
      <button class="zipbtn" onclick={() => zipInput?.click()}>
        {skZip ? `${skZip.name} · ${fmtSize(skZip.size)}` : '点击选择 zip 文件'}
      </button>
      <input type="file" accept=".zip,application/zip" hidden bind:this={zipInput} onchange={pickZip} />
    </div>
    <p class="modal-hint">
      zip 需含根级 SKILL.md，scripts 等子资源一并解压。技能名自动确定：单文件夹压缩取文件夹名，平铺取 SKILL.md 里 frontmatter 的 name。
    </p>
    {#if skErr}
      <p class="modal-hint err">{skErr}</p>
    {/if}
    <footer>
      <button class="btn" disabled={skBusy} onclick={() => (skillModal = false)}>取消</button>
      <button class="btn primary" disabled={!skZip || skBusy} onclick={createSkill}>
        {skBusy ? '创建中…' : '创建'}
      </button>
    </footer>
  </div>
{/if}

<!-- 删除技能确认弹窗（居中） -->
{#if delTarget}
  <div class="drawer-mask" onclick={() => (delTarget = null)}></div>
  <div class="modal confirm">
    <h3>删除技能</h3>
    <p class="modal-hint">确定删除「{delTarget.name}」？技能目录与脚本将一并删除，不可恢复。</p>
    <footer>
      <button class="btn" onclick={() => (delTarget = null)}>取消</button>
      <button class="btn danger" onclick={doDeleteSkill}>删除</button>
    </footer>
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
    gap: 34px;
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
  .lead.err {
    color: #c0392b;
  }
  section {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 16px;
  }
  h2 {
    font-size: 13px;
    font-weight: 700;
    color: var(--muted);
  }
  .hint {
    font-size: 11px;
    color: var(--faint);
    margin-top: 2px;
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
  .dir span {
    font-size: 11px;
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

  /* 长期记忆：文件行 */
  .file {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 14px;
    transition: background var(--dur-fast) var(--ease-out);
  }
  .file + .file {
    border-top: 1px solid var(--line);
  }
  .file:hover {
    background: var(--bg-soft);
  }
  .file .name {
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--fg);
    overflow-wrap: anywhere;
  }
  .file .badge {
    flex: none;
    font-size: 10px;
    color: var(--accent);
    background: var(--accent-soft);
    border-radius: 5px;
    padding: 1px 7px;
  }
  .file .stat {
    margin-left: auto;
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--faint);
    white-space: nowrap;
  }
  .file.index {
    background: color-mix(in srgb, var(--accent) 4%, var(--bg));
  }

  /* 能力记忆：技能卡片 */
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(196px, 1fr));
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
  .card-top {
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
  .glyph svg {
    width: 16px;
    height: 16px;
  }
  .card h3 {
    font-size: 13.5px;
    font-weight: 650;
    color: var(--fg);
    margin-top: 2px;
  }
  .card-desc {
    font-size: 11.5px;
    line-height: 1.6;
    color: var(--faint);
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
    flex: 1;
  }
  .card.ghost {
    border-style: dashed;
  }
  .ghost-glyph {
    border-style: dashed;
    background: transparent;
  }
  .card.ghost h3 {
    color: var(--muted);
  }
  .card.newskill {
    cursor: pointer;
    text-align: left;
    font: inherit;
  }
  .card.newskill:hover {
    border-color: var(--line-strong);
    box-shadow: 0 4px 16px rgb(0 0 0 / 7%);
    transform: translateY(-2px);
  }
  .card-ops {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .del {
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    border: none;
    background: transparent;
    border-radius: 7px;
    color: var(--faint);
    cursor: pointer;
    opacity: 0;
    transition:
      opacity var(--dur-fast) var(--ease-out),
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .del svg {
    width: 14px;
    height: 14px;
  }
  .card:hover .del {
    opacity: 1;
  }
  .del:hover {
    background: color-mix(in srgb, #c0392b 10%, var(--bg));
    color: #c0392b;
  }
  .toggle {
    position: relative;
    width: 30px;
    height: 17px;
    border-radius: 9px;
    background: var(--line);
    transition: background var(--dur-fast) var(--ease-out);
    cursor: pointer;
    flex: none;
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

  /* ── 新建技能弹窗 ── */
  .modal {
    position: fixed;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    z-index: 59;
    width: min(400px, 92vw);
    display: flex;
    flex-direction: column;
    gap: 14px;
    background: var(--bg);
    border: 1px solid var(--line-strong);
    border-radius: 14px;
    box-shadow: 0 12px 40px rgb(0 0 0 / 18%);
    padding: 18px 20px;
  }
  .modal header {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .modal h3 {
    font-size: 14px;
    font-weight: 700;
    color: var(--fg);
  }
  .fld {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .fld > span {
    font-size: 11.5px;
    color: var(--muted);
  }
  .zipbtn {
    border: 1px dashed var(--line);
    border-radius: 8px;
    background: var(--bg-soft);
    color: var(--muted);
    font: inherit;
    font-size: 12px;
    padding: 9px 10px;
    text-align: left;
    cursor: pointer;
    overflow-wrap: anywhere;
    transition:
      border-color var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .zipbtn:hover {
    border-color: var(--line-strong);
    color: var(--fg);
  }
  .modal-hint {
    font-size: 11px;
    line-height: 1.6;
    color: var(--faint);
  }
  .modal-hint.err {
    color: #c0392b;
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
    font-size: 12px;
    padding: 7px 16px;
    cursor: pointer;
    transition:
      border-color var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .btn:not(:disabled):hover {
    border-color: var(--line-strong);
    color: var(--fg);
  }
  .btn.primary {
    background: var(--accent);
    border-color: var(--accent);
    color: #fff;
  }
  .btn.danger {
    background: #c0392b;
    border-color: #c0392b;
    color: #fff;
  }
  .btn:disabled {
    opacity: 0.45;
    cursor: default;
  }
  .modal.confirm {
    width: min(340px, 92vw);
    gap: 12px;
  }

  /* 话题记忆：会话行 */
  .info {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .skill .name {
    font-size: 12.5px;
    color: var(--fg);
    font-weight: 550;
  }
  .desc {
    font-size: 11px;
    color: var(--faint);
    overflow-wrap: anywhere;
  }

  /* 话题记忆：目录式会话树。导轨列(guide)按祖先层级等宽排列，
     竖线贯穿同级节点——折叠的子树不渲染行，导轨天然连续不错位 */
  .treelist {
    display: flex;
    flex-direction: column;
    border: 1px solid var(--line);
    border-radius: 12px;
    padding: 4px 0;
    overflow: hidden;
  }
  .trow {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px 12px 6px 8px;
    transition: background var(--dur-fast) var(--ease-out);
  }
  .trow:hover {
    background: var(--bg-soft);
  }
  .trow.cur {
    background: color-mix(in srgb, var(--accent) 7%, var(--bg));
  }
  .trow.root {
    border-bottom: 1px solid var(--line);
    border-radius: 0;
    margin-bottom: 2px;
  }
  .guide {
    flex: none;
    width: 18px;
    align-self: stretch;
    border-left: 1px solid var(--line);
  }
  .tw {
    flex: none;
    display: grid;
    place-items: center;
    width: 18px;
    height: 18px;
    border: none;
    background: transparent;
    color: var(--faint);
    font-size: 11px;
    cursor: pointer;
    border-radius: 4px;
    padding: 0;
  }
  .tw:hover {
    color: var(--fg);
    background: var(--bg-soft);
  }
  .tw.dot {
    cursor: default;
  }
  .farrow {
    display: inline-block;
    transition: transform var(--dur-fast) var(--ease-out);
  }
  .mono {
    font-family: var(--font-mono);
  }
  .trow.root .name {
    color: var(--muted);
    font-family: var(--font-mono);
    font-size: 12px;
  }
  /* 名称行 = flex：文本自然宽度、过长收缩出省略号，徽章紧跟其后不被挤走 */
  .trow .name {
    display: flex;
    align-items: center;
    min-width: 0;
    font-size: 12.5px;
    color: var(--fg);
    font-weight: 550;
  }
  .trow .name .n-text {
    flex: 0 1 auto;
    min-width: 0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .kbadge {
    flex: none;
    margin-left: 8px;
    font-size: 10px;
    font-weight: 500;
    color: var(--accent);
    background: var(--accent-soft);
    border-radius: 5px;
    padding: 1px 7px;
    vertical-align: 1px;
    max-width: 240px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .kbadge.arc {
    color: var(--muted);
    background: var(--bg-soft);
    border: 1px solid var(--line);
  }
  .kbadge.mut {
    color: var(--faint);
    background: transparent;
    border: 1px dashed var(--line);
  }
  .ops {
    display: flex;
    gap: 4px;
    flex: none;
  }
  .op {
    flex: none;
    border: none;
    background: transparent;
    color: var(--muted);
    font-size: 11.5px;
    padding: 4px 8px;
    border-radius: 6px;
    opacity: 0;
    cursor: pointer;
    transition: background var(--dur-fast) var(--ease-out), color var(--dur-fast) var(--ease-out);
  }
  .trow:hover .op {
    opacity: 1;
  }
  .op:hover:not(:disabled) {
    background: var(--bg-soft);
    color: var(--fg);
  }
  .op:disabled {
    opacity: 0.35;
    cursor: default;
  }

  /* ── 回顾侧边抽屉 ── */
  .drawer-mask {
    position: fixed;
    inset: 0;
    z-index: 58;
    background: rgb(0 0 0 / 18%);
  }
  .drawer {
    position: fixed;
    top: 0;
    right: 0;
    bottom: 0;
    z-index: 59;
    width: min(460px, 92vw);
    display: flex;
    flex-direction: column;
    background: var(--bg);
    border-left: 1px solid var(--line-strong);
    box-shadow: -8px 0 24px rgb(0 0 0 / 12%);
    animation: drawer-in 0.22s var(--ease-out) both;
  }
  @keyframes drawer-in {
    from {
      transform: translateX(24px);
      opacity: 0;
    }
    to {
      transform: translateX(0);
      opacity: 1;
    }
  }
  .drawer header {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    padding: 14px 16px;
    border-bottom: 1px solid var(--line);
  }
  .dtitle {
    display: flex;
    align-items: center;
    gap: 4px;
    min-width: 0;
  }
  .dtitle .name {
    font-size: 13px;
    font-weight: 650;
    color: var(--fg);
    overflow-wrap: anywhere;
  }
  .dclose {
    flex: none;
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    border: 1px solid var(--line);
    border-radius: 8px;
    background: transparent;
    color: var(--muted);
    cursor: pointer;
  }
  .dclose:hover {
    color: var(--fg);
    border-color: var(--line-strong);
  }
  .dsum {
    flex: none;
    margin: 12px 16px 0;
    padding: 10px 12px;
    border: 1px solid var(--line);
    border-left: 3px solid var(--accent);
    border-radius: 8px;
    background: var(--bg-soft);
  }
  .dsum-tag {
    font-size: 11px;
    color: var(--accent);
    font-weight: 600;
    margin-bottom: 6px;
  }
  .dsum-text {
    font-size: 12px;
    line-height: 1.6;
    color: var(--muted);
    white-space: pre-wrap;
    word-break: break-word;
    max-height: 180px;
    overflow-y: auto;
  }
  .dmsgs {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 12px 16px 20px;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .rv {
    display: flex;
    gap: 10px;
    font-size: 12px;
    line-height: 1.5;
  }
  .rv.me .rtext {
    color: var(--muted);
  }
  .rrole {
    flex: none;
    width: 44px;
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--faint);
    text-transform: uppercase;
    padding-top: 2px;
  }
  .rtext {
    white-space: pre-wrap;
    word-break: break-word;
  }
</style>
