<script lang="ts">
  /*
  资源页目录树（悬浮面板，盖在预览区左侧，不占预览宽度）：浏览文件
  系统，点击文件交给 ResourcePane 打开（面板随之收起）。默认根 = 工作
  目录（可换：桌面壳原生对话框 / web 端手输路径）；选择过的目录进
  「最近」清单快速切换，当前根置顶打勾。目录懒加载按层展开（点目录
  只展开子层，树根不切换）；所在目录是 git 仓库时条目带工作区变更徽标
  （U 新 / M 改 / D 删——纯观测，不做任何 git 操作）。激活期 3s 轮询
  根目录与已展开目录，AI 改盘后徽标与列表自动跟进。
  */

  import { onMount } from 'svelte'
  import { api, statusLetter, type TreeEntry } from '../../lib/api'
  import { autoDiffOn, explorer, removeAutoDiffDir, setAutoDiff } from '../../lib/explorerState.svelte'
  import KindIcon from './KindIcon.svelte'
  import { kindOf } from './registry'

  let {
    active = false,
    onopen,
    onclose,
  }: { active?: boolean; onopen?: (path: string) => void; onclose?: () => void } = $props()

  interface Listing {
    entries: TreeEntry[]
    err: string
  }

  let workdir = $state('')
  let listing = $state<Record<string, Listing>>({})
  let expanded = $state<Record<string, boolean>>({})
  let recent = $state<string[]>(safeRecent())
  let inputMode = $state(false) // web 端手输路径行
  let typedPath = $state('')
  let menuOpen = $state(false) // 根目录下拉（最近目录 + 选择目录）
  let rootEl = $state<HTMLElement | undefined>()

  function safeRecent(): string[] {
    try {
      const v = JSON.parse(localStorage.getItem('ez-explorer-recent') ?? '[]')
      return Array.isArray(v) ? v.filter((x) => typeof x === 'string') : []
    } catch {
      return []
    }
  }

  /* 目录下拉清单：工作目录常驻置顶（带 tag），当前根次之（打勾），
     再跟最近选择过的目录 */
  const menuItems = $derived.by(() => {
    const items: { path: string; workdir?: boolean }[] = []
    if (workdir) items.push({ path: workdir, workdir: true })
    if (explorer.dir && explorer.dir !== workdir) items.push({ path: explorer.dir })
    for (const p of recent) {
      if (p !== explorer.dir && p !== workdir) items.push({ path: p })
    }
    return items
  })

  /* 当前根目录的自动对比开关态（按目录各自记忆，git chip 上切换） */
  const dirAutoDiff = $derived(!!explorer.repo && autoDiffOn(explorer.dir))

  /* dir 子树内的 git 变更概况（文件夹着色用）：条数与是否含改/增 */
  function subtreeStatus(dir: string): { n: number; mod: boolean; add: boolean } {
    if (!explorer.repo || !dir.startsWith(explorer.repo + '/')) return { n: 0, mod: false, add: false }
    const rel = dir.slice(explorer.repo.length + 1)
    let n = 0
    let mod = false
    let add = false
    for (const [k, code] of Object.entries(explorer.changed)) {
      if (rel && !k.startsWith(rel + '/')) continue
      n++
      if (code === '??' || code.includes('A')) add = true
      else mod = true
    }
    return { n, mod, add }
  }

  /* git 计数只统计当前根目录子树内的变更（仓库其他位置的变更不进来，
     数字与所见目录对得上） */
  const rootChangedCount = $derived.by(() => {
    if (!explorer.repo || !explorer.dir.startsWith(explorer.repo + '/')) return 0
    const rel = explorer.dir.slice(explorer.repo.length + 1)
    return Object.keys(explorer.changed).filter((k) => (rel ? k.startsWith(rel + '/') : true)).length
  })

  /* dir 状态码：仓库相对路径 → porcelain 两字符码（无 = 未变更） */
  function codeOf(abs: string): string {
    return explorer.changed[abs.slice(explorer.repo.length + 1)] ?? ''
  }

  /*
  load 拉一层目录，成功返回规范化路径（换根用）。轮询是主要调用方，
  所以一切写入先做同值判——数据没变就不动状态（对象写入必触发整树
  重渲染）；单次请求失败不清空已有条目、git 状态失败保留上次结果——
  AI/终端并发用 git 时的锁冲突是瞬态的，闪一下徽标再闪回来就是"乱跳"。
  注意：load 不动 explorer.dir（树根），点子目录展开不会"进入"该目录。
  */
  async function load(dir: string): Promise<string | null> {
    try {
      const v = await api.tree(dir)
      const cur = listing[v.path]
      /* 同值不写（对象写入必触发整树重渲染）；上次有错这次成功则连错一起清 */
      if (!cur || cur.err || JSON.stringify(cur.entries) !== JSON.stringify(v.entries)) {
        listing = { ...listing, [v.path]: { entries: v.entries, err: '' } }
      }
      if (v.git) {
        explorer.repo = v.git.root
        if (v.git.status != null && JSON.stringify(explorer.changed) !== JSON.stringify(v.git.status)) {
          explorer.changed = v.git.status
        }
      } else if (explorer.repo || Object.keys(explorer.changed).length) {
        explorer.repo = ''
        explorer.changed = {}
      }
      return v.path
    } catch (e) {
      const msg = (e as Error).message
      const cur = listing[dir]
      if (!cur || cur.err !== msg) {
        listing = { ...listing, [dir]: { entries: cur?.entries ?? [], err: msg } }
      }
      return null
    }
  }

  /* 首次进入：记住的目录优先（失效退回工作目录），否则工作目录 */
  onMount(async () => {
    try {
      workdir = (await api.workdir()).path
    } catch {
      return
    }
    const stored = localStorage.getItem('ez-explorer-dir') ?? ''
    let root = ''
    if (stored && stored !== workdir) root = (await load(stored)) ?? ''
    if (!root) root = (await load(workdir)) ?? workdir
    explorer.dir = root
    await restoreExpanded(root)
  })

  /* 激活期轮询：根目录 + 已展开目录整体重拉（条目与 git 徽标跟进改盘） */
  $effect(() => {
    if (!active) return
    const id = setInterval(() => {
      for (const dir of Object.keys(listing)) {
        if (expanded[dir]) void load(dir)
      }
    }, 3000)
    return () => clearInterval(id)
  })

  /* 点目录只展开/收起子层（懒加载），树根不变；展开状态即记 */
  async function toggleDir(dir: string) {
    if (expanded[dir]) {
      expanded = { ...expanded, [dir]: false }
      saveExpanded()
      return
    }
    if (!listing[dir]) await load(dir)
    expanded = { ...expanded, [dir]: true }
    saveExpanded()
  }

  async function refresh() {
    await Promise.all(
      Object.keys(listing)
        .filter((d) => expanded[d])
        .map((d) => load(d)),
    )
  }

  /* 换根目录：桌面壳原生对话框，web 端退化手输路径；分隔符归一为斜杠 */
  async function pickDir() {
    const ez = (window as any).ez
    if (ez?.window?.pickDir) {
      const picked = await ez.window.pickDir()
      if (picked) await setRoot(picked.replaceAll('\\', '/'))
      return
    }
    inputMode = !inputMode
    typedPath = explorer.dir
  }

  async function applyTyped() {
    const p = typedPath.trim().replaceAll('\\', '/')
    if (p) await setRoot(p)
    inputMode = false
  }

  /* setRoot 换树根：记忆 + 进「最近」清单（当前项置顶去重，最多 6 个） */
  async function setRoot(dir: string) {
    const p = await load(dir)
    if (!p) return
    explorer.dir = p
    localStorage.setItem('ez-explorer-dir', p)
    recent = [p, ...recent.filter((x) => x !== p)].slice(0, 6)
    localStorage.setItem('ez-explorer-recent', JSON.stringify(recent))
    await restoreExpanded(p) // 该目录上次的展开状态（连同各层条目）
    inputMode = false
  }

  /* removeRecent 把快捷目录从「最近」清单移除（工作目录常驻，不在清单里）；
     联动清掉该目录的自动对比记忆 */
  function removeRecent(path: string) {
    recent = recent.filter((p) => p !== path)
    localStorage.setItem('ez-explorer-recent', JSON.stringify(recent))
    removeAutoDiffDir(path)
  }

  /* ── 展开状态记忆（按根目录分别保存）：重开面板/切走再切回都还原 ── */

  function loadExpanded(root: string): Record<string, boolean> {
    try {
      const all = JSON.parse(localStorage.getItem('ez-explorer-expanded') ?? '{}')
      const map: Record<string, boolean> = { [root]: true }
      const list = Array.isArray(all[root]) ? all[root] : []
      for (const d of list) if (typeof d === 'string') map[d] = true
      return map
    } catch {
      return { [root]: true }
    }
  }

  /* restoreExpanded 还原展开标记并补拉这些目录的条目——只还原标记的话
     子目录在 listing 里是空的，看起来就是"展开了一堆空目录" */
  async function restoreExpanded(root: string) {
    expanded = loadExpanded(root)
    await Promise.all(
      Object.keys(expanded)
        .filter((d) => d !== root && expanded[d] && !listing[d])
        .map((d) => load(d)),
    )
  }

  function saveExpanded() {
    try {
      const all = JSON.parse(localStorage.getItem('ez-explorer-expanded') ?? '{}')
      all[explorer.dir] = Object.keys(expanded).filter((d) => expanded[d])
      localStorage.setItem('ez-explorer-expanded', JSON.stringify(all))
    } catch {
      /* 存储满/损坏：放弃本次记忆，不影响功能 */
    }
  }

  /* 点面板外关闭（ResourcePane 的开关钮在 pointerdown 阶段挡掉，不误关）；
     右键菜单在面板外，点它不算"外部" */
  function onOutside(e: PointerEvent) {
    const t = e.target as HTMLElement
    if (rootEl && !rootEl.contains(t) && !t.closest('.ctx')) onclose?.()
    if (ctx && !t.closest('.ctx')) ctx = null
  }

  /* 面板内点击：不在下拉/触发钮上就收起下拉 */
  function onPanelDown(e: PointerEvent) {
    const t = e.target as HTMLElement
    if (menuOpen && !t.closest('.menu') && !t.closest('.rootbtn')) menuOpen = false
  }

  /* 行右键：在资源管理器打开（目录与文件通用；web 端无壳禁用） */
  let ctx = $state<{ x: number; y: number; path: string } | null>(null)
  const canReveal = !!(window as any).ez?.window?.reveal

  function openCtx(e: MouseEvent, path: string) {
    e.preventDefault()
    ctx = {
      x: Math.min(e.clientX, window.innerWidth - 170),
      y: Math.min(e.clientY, window.innerHeight - 60),
      path,
    }
  }

  function revealCtx() {
    const p = ctx?.path
    ctx = null
    if (p) (window as any).ez?.window?.reveal?.(p)
  }
</script>

<svelte:window onpointerdown={onOutside} onkeydown={(e) => e.key === 'Escape' && (ctx = null)} />

<aside class="tree" bind:this={rootEl} onpointerdown={onPanelDown}>
  <header>
    <!-- 根目录按钮：完整路径（尾部优先显示），点击弹目录下拉 -->
    <button class="rootbtn" onclick={() => (menuOpen = !menuOpen)} title={explorer.dir}>
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
        <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />
      </svg>
      <span class="dpath">{explorer.dir || '目录'}</span>
      <svg class="chevdown" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
        <path d="M6 9l6 6 6-6" />
      </svg>
    </button>
    <!-- git chip = 自动对比开关（按当前根目录各自记忆）：在仓库内可点
         （开启变绿，点有变更的文件直接进对比）；不在仓库内禁用置灰 -->
    <button
      class="gitchip"
      class:clean={!rootChangedCount}
      class:on={dirAutoDiff}
      disabled={!explorer.repo}
      title={!explorer.repo
        ? '目录不在 git 仓库内，无法对比'
        : dirAutoDiff
          ? '自动对比已开启：打开「有未提交变更」的文件直接进 HEAD 对比（无变更的文件正常打开）。点击关闭'
          : '点击开启自动对比：打开有未提交变更的文件时直接进 HEAD 对比（当前目录 ' +
            rootChangedCount +
            ' 个文件有变更）。被 .gitignore 忽略的文件没有变更概念'}
      onclick={() => setAutoDiff(explorer.dir, !dirAutoDiff)}
    >
      git{rootChangedCount ? ` ${rootChangedCount}` : ''}
    </button>
    <span class="acts">
      <button class="abtn" onclick={() => void refresh()} title="刷新">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <path d="M21 12a9 9 0 1 1-2.6-6.3" />
          <path d="M21 3v6h-6" />
        </svg>
      </button>
      <button class="abtn" onclick={() => onclose?.()} title="关闭目录面板">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
          <path d="M6 6l12 12M18 6L6 18" />
        </svg>
      </button>
    </span>
  </header>
  {#if menuOpen}
    <div class="menu">
      {#each menuItems as it (it.path)}
        <button class="mitem" class:cur={it.path === explorer.dir} onclick={() => { menuOpen = false; void setRoot(it.path) }} title={it.path}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
            <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />
          </svg>
          <span class="mp">{it.path}</span>
          {#if it.workdir}
            <span class="mtag">工作目录</span>
          {:else if it.path === explorer.dir}
            <span class="mark">✓</span>
          {:else}
            <span
              class="mx"
              role="button"
              tabindex="-1"
              title="从列表移除"
              onclick={(e) => { e.stopPropagation(); removeRecent(it.path) }}
            >
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round">
                <path d="M6 6l12 12M18 6L6 18" />
              </svg>
            </span>
          {/if}
        </button>
      {/each}
      <div class="msep"></div>
      <button class="mitem" onclick={() => { menuOpen = false; void pickDir() }}>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />
          <path d="M12 10.5v5M9.5 13h5" />
        </svg>
        <span class="mp">选择目录…</span>
      </button>
    </div>
  {/if}
  {#if inputMode}
    <div class="pathrow">
      <input
        bind:value={typedPath}
        onkeydown={(e) => e.key === 'Enter' && void applyTyped()}
        placeholder="输入目录绝对路径，回车打开"
        spellcheck="false"
      />
    </div>
  {/if}
  <div class="rows">
    {#snippet treeNodes(dir: string, depth: number)}
      {#each listing[dir]?.entries ?? [] as e (e.name)}
        {@const abs = `${dir}/${e.name}`}
        {#if e.dir}
          {@const st = subtreeStatus(abs)}
          <button class="row" style="padding-left:{10 + depth * 14}px" onclick={() => void toggleDir(abs)} oncontextmenu={(e) => openCtx(e, abs)} title={abs}>
            <span class="chev" class:open={expanded[abs]}>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
                <path d="M9 6l6 6-6 6" />
              </svg>
            </span>
            <svg class="ficon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
              <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />
            </svg>
            <span class="fname" class:fmod={st.mod} class:fnew={st.add && !st.mod}>{e.name}</span>
            {#if st.n}<span class="badge" class:M={st.mod} class:U={!st.mod}>{st.n}</span>{/if}
          </button>
          {#if expanded[abs]}
            {@render treeNodes(abs, depth + 1)}
            {#if !listing[abs]?.entries.length && !listing[abs]?.err}
              <div class="leafhint" style="padding-left:{24 + depth * 14}px">（空目录）</div>
            {/if}
          {/if}
        {:else}
          {@const code = codeOf(abs)}
          <button
            class="row"
            class:active={abs === explorer.activeFile}
            style="padding-left:{10 + depth * 14}px"
            onclick={() => onopen?.(abs)}
            oncontextmenu={(e) => openCtx(e, abs)}
            title={abs}
          >
            <span class="kind"><KindIcon kind={kindOf(abs)} /></span>
            <span class="fname" class:mod={code && code !== '??'} class:new={code === '??'}>{e.name}</span>
            {#if code}<span class="badge {statusLetter(code)}">{statusLetter(code)}</span>{/if}
          </button>
        {/if}
      {/each}
      {#if listing[dir]?.err && !listing[dir].entries.length}
        <div class="leafhint" style="padding-left:{10 + depth * 14}px">（无法读取：{listing[dir].err}）</div>
      {/if}
    {/snippet}
    {@render treeNodes(explorer.dir, 0)}
    {#if !listing[explorer.dir] && !listing[explorer.dir]?.err}
      <div class="leafhint" style="padding-left:10px">加载中…</div>
    {/if}
  </div>
</aside>

{#if ctx}
  <div class="ctx" style="left:{ctx.x}px; top:{ctx.y}px" role="menu">
    <button role="menuitem" disabled={!canReveal} title={canReveal ? '' : '仅桌面端可用'} onclick={revealCtx}>在资源管理器打开</button>
  </div>
{/if}

<style>
  /* 悬浮面板：absolute 盖在预览区左侧（.body 是定位上下文），不参与布局
     ——预览区始终全宽；高度按内容收缩（上限抽屉内高），不高垂到底 */
  .tree {
    position: absolute;
    top: 0;
    left: 0;
    width: 320px;
    height: auto;
    min-height: 180px;
    max-height: 100%;
    z-index: 20;
    display: flex;
    flex-direction: column;
    border: 1px solid var(--line);
    border-radius: 12px;
    background: var(--bg);
    box-shadow: 0 10px 32px rgb(0 0 0 / 14%);
    overflow: hidden;
  }
  header {
    flex: none;
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px 8px 6px 8px;
    border-bottom: 1px solid var(--line);
    background: var(--bg-soft);
    min-width: 0;
  }
  .rootbtn {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 5px;
    padding: 3px 4px 3px 6px;
    border: none;
    border-radius: 7px;
    background: transparent;
    color: var(--fg);
    font: inherit;
    font-size: var(--font-size-ui);
    font-weight: 700;
    cursor: pointer;
    transition: background var(--dur-fast) var(--ease-out);
  }
  .rootbtn:hover {
    background: var(--line);
  }
  .rootbtn > svg:first-child {
    flex: none;
    width: 12px;
    height: 12px;
    color: var(--muted);
  }
  /* 完整路径：空间不够时省略号吃掉头部、保住尾部（目录名可见） */
  .dpath {
    flex: 1;
    min-width: 0;
    direction: rtl;
    text-align: left;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .chevdown {
    flex: none;
    width: 11px;
    height: 11px;
    color: var(--faint);
  }
  /* 目录下拉：最近选择过的目录 + 打开资源管理器选择 */
  .menu {
    position: absolute;
    top: 34px;
    left: 6px;
    right: 6px;
    z-index: 2;
    display: flex;
    flex-direction: column;
    padding: 4px;
    border: 1px solid var(--line);
    border-radius: 10px;
    background: var(--bg);
    box-shadow: 0 8px 24px rgb(0 0 0 / 16%);
    max-height: 260px;
    overflow-y: auto;
    scrollbar-width: thin;
    scrollbar-color: var(--line-strong) transparent;
  }
  .menu::-webkit-scrollbar {
    width: 8px;
  }
  .menu::-webkit-scrollbar-thumb {
    background: var(--line-strong);
    border-radius: 4px;
  }
  .menu::-webkit-scrollbar-track {
    background: transparent;
  }
  .mitem {
    display: flex;
    align-items: center;
    gap: 6px;
    width: 100%;
    padding: 5px 6px;
    border: none;
    border-radius: 7px;
    background: transparent;
    color: var(--fg);
    font: inherit;
    font-size: var(--font-size-ui);
    text-align: left;
    cursor: pointer;
    white-space: nowrap;
    transition: background var(--dur-fast) var(--ease-out);
  }
  .mitem:hover {
    background: var(--bg-soft);
  }
  .mitem svg {
    flex: none;
    width: 12px;
    height: 12px;
    color: var(--muted);
  }
  .mitem .mp {
    flex: 1;
    min-width: 0;
    direction: rtl;
    text-align: left;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .mitem.cur {
    font-weight: 600;
  }
  .mitem .mark {
    flex: none;
    color: var(--accent, #2563eb);
  }
  /* 快捷目录的移除钮（工作目录/当前目录不显示） */
  .mitem .mx {
    flex: none;
    display: grid;
    place-items: center;
    width: 16px;
    height: 16px;
    border-radius: 5px;
    color: var(--faint);
  }
  .mitem .mx:hover {
    color: var(--fg);
    background: var(--line);
  }
  .mitem .mx svg {
    width: 9px;
    height: 9px;
  }
  .mtag {
    flex: none;
    font-size: var(--font-size-small);
    color: var(--accent, #2563eb);
    border: 1px solid var(--accent-soft, var(--accent));
    border-radius: 5px;
    padding: 0 5px;
    white-space: nowrap;
  }
  .msep {
    flex: none;
    height: 1px;
    margin: 4px 2px;
    background: var(--line);
  }
  .gitchip {
    flex: none;
    border: none;
    font: inherit;
    font-size: var(--font-size-small);
    font-family: var(--font-mono);
    color: #b45309;
    background: color-mix(in srgb, #f0883e 16%, transparent);
    border-radius: 5px;
    padding: 1px 5px;
    white-space: nowrap;
    cursor: pointer;
    transition:
      color var(--dur-fast) var(--ease-out),
      background var(--dur-fast) var(--ease-out);
  }
  .gitchip.clean {
    color: var(--faint);
    background: var(--line);
  }
  /* 自动对比已开启：绿色（有变更可对比的“健康”信号色） */
  .gitchip.on {
    color: #1a7f37;
    background: color-mix(in srgb, #1a7f37 16%, transparent);
    font-weight: 700;
  }
  .gitchip:disabled {
    opacity: 0.55;
    cursor: default;
  }
  .acts {
    flex: none;
    display: flex;
    gap: 2px;
  }
  .abtn {
    display: grid;
    place-items: center;
    width: 20px;
    height: 20px;
    border: none;
    background: transparent;
    color: var(--faint);
    border-radius: 6px;
    cursor: pointer;
    transition:
      color var(--dur-fast) var(--ease-out),
      background var(--dur-fast) var(--ease-out);
  }
  .abtn:hover {
    color: var(--fg);
    background: var(--line);
  }
  .abtn svg {
    width: 12px;
    height: 12px;
  }
  .pathrow {
    flex: none;
    padding: 6px 8px;
    border-bottom: 1px solid var(--line);
  }
  .pathrow input {
    width: 100%;
    border: 1px solid var(--line-strong);
    border-radius: 7px;
    padding: 4px 8px;
    font-size: var(--font-size-ui);
    font-family: var(--font-mono);
    background: var(--bg);
    color: var(--fg);
    outline: none;
    box-sizing: border-box;
  }
  .rows {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 4px 0 8px;
    scrollbar-width: thin;
    scrollbar-color: var(--line-strong) transparent;
  }
  .rows::-webkit-scrollbar {
    width: 8px;
  }
  .rows::-webkit-scrollbar-thumb {
    background: var(--line-strong);
    border-radius: 4px;
  }
  .rows::-webkit-scrollbar-track {
    background: transparent;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 5px;
    width: 100%;
    padding: 2.5px 6px 2.5px 0;
    border: none;
    background: transparent;
    color: var(--fg);
    font: inherit;
    font-size: var(--font-size-ui);
    text-align: left;
    cursor: pointer;
    white-space: nowrap;
    transition: background var(--dur-fast) var(--ease-out);
  }
  .row:hover {
    background: var(--bg-soft);
  }
  /* 当前打开的文件在树内高亮 */
  .row.active {
    background: var(--accent-soft, color-mix(in srgb, #2563eb 10%, transparent));
    color: var(--accent, #2563eb);
    font-weight: 600;
  }
  .chev {
    flex: none;
    display: grid;
    place-items: center;
    width: 12px;
    height: 12px;
    color: var(--faint);
    transition: transform var(--dur-fast) var(--ease-out);
  }
  .chev.open {
    transform: rotate(90deg);
  }
  .chev svg {
    width: 9px;
    height: 9px;
  }
  .ficon {
    flex: none;
    width: 12px;
    height: 12px;
    color: var(--muted);
  }
  .kind {
    flex: none;
    display: grid;
    place-items: center;
    width: 12px;
    color: var(--muted);
  }
  .fname {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  /* git 变更着色（VSCode 惯例：修改橙、新增绿；文件与所在文件夹同色） */
  .fname.mod,
  .fname.fmod {
    color: #b45309;
    font-weight: 600;
  }
  .fname.new,
  .fname.fnew {
    color: #1a7f37;
    font-weight: 600;
  }
  .badge {
    flex: none;
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    line-height: 1;
    padding: 1px 3px;
    border-radius: 4px;
  }
  .badge.M {
    color: #b45309;
    background: color-mix(in srgb, #f0883e 16%, transparent);
  }
  .badge.U,
  .badge.A {
    color: #1a7f37;
    background: color-mix(in srgb, #1a7f37 12%, transparent);
  }
  .badge.D {
    color: #cf222e;
    background: color-mix(in srgb, #cf222e 12%, transparent);
  }
  .leafhint {
    padding: 3px 6px;
    color: var(--faint);
    font-size: var(--font-size-ui);
  }
  /* 行右键菜单（fixed 定位，屏幕坐标） */
  .ctx {
    position: fixed;
    z-index: 60;
    display: flex;
    flex-direction: column;
    min-width: 150px;
    padding: 4px;
    border: 1px solid var(--line);
    border-radius: 9px;
    background: var(--bg);
    box-shadow: 0 6px 20px rgb(0 0 0 / 10%);
  }
  .ctx button {
    border: none;
    background: transparent;
    text-align: left;
    padding: 6px 10px;
    font-size: var(--font-size-ui);
    color: var(--fg);
    border-radius: 6px;
  }
  .ctx button:hover:not(:disabled) {
    background: var(--bg-soft);
  }
  .ctx button:disabled {
    color: var(--faint);
    cursor: default;
  }
</style>
