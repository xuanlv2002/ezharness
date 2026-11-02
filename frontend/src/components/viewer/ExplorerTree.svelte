<script lang="ts">
  /*
  资源页目录树（悬浮面板，从抽屉标题栏正下方展开盖在预览区左侧，不占
  预览宽度；开关在标题栏，弹出独立窗在面板内）：浏览文件系统，点击
  文件交给 ResourcePane 打开（树里点永远正常打开，要不要对比在变更
  清单里手动选）。默认根 = 工作
  目录（可换：桌面壳原生对话框 / web 端手输路径）；选择过的目录进
  「最近」清单快速切换，当前根置顶打勾。目录懒加载按层展开（点目录
  只展开子层，树根不切换）；所在目录是 git 仓库时条目带工作区变更徽标
  （U 新 / M 改 / D 删——纯观测，不做任何 git 操作），git chip 把面板
  原地切成变更视图（点文件直接进 HEAD 对比，再点切回目录）。激活期 3s
  轮询根目录与已展开目录，AI 改盘后徽标与列表自动跟进。
  */

  import { onMount } from 'svelte'
  import { api, statusLetter, type TreeEntry } from '../../lib/api'
  import { explorer, setExplorerDocked } from '../../lib/explorerState.svelte'
  import KindIcon from './KindIcon.svelte'
  import { kindOf } from './registry'

  let {
    active = false,
    open = true,
    docked = false,
    onopen,
    onclose,
  }: {
    active?: boolean
    open?: boolean
    docked?: boolean
    onopen?: (path: string, opts?: { diff?: boolean }) => void
    onclose?: () => void
  } = $props()

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
  let changesOpen = $state(false) // git 变更清单（chip 展开，点文件进对比）
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

  /* 变更清单（git chip 展开）：当前根目录子树内的未提交文件，文件名 +
     所在目录（仓库相对），点条目直接进 HEAD 对比；删除项只陈列不可点 */
  const changeList = $derived.by(() => {
    if (!explorer.repo) return []
    const rel = explorer.dir.startsWith(explorer.repo + '/') ? explorer.dir.slice(explorer.repo.length + 1) : ''
    return Object.entries(explorer.changed)
      .filter(([k]) => (rel ? k.startsWith(rel + '/') : true))
      .map(([k, code]) => {
        const i = k.lastIndexOf('/')
        return { abs: explorer.repo + '/' + k, name: i < 0 ? k : k.slice(i + 1), dir: i < 0 ? '' : k.slice(0, i), code }
      })
      .sort((a, b) => a.name.localeCompare(b.name))
  })

  function toggleChanges() {
    changesOpen = !changesOpen
    if (changesOpen) menuOpen = false
  }

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

  /* removeRecent 把快捷目录从「最近」清单移除（工作目录常驻，不在清单里） */
  function removeRecent(path: string) {
    recent = recent.filter((p) => p !== path)
    localStorage.setItem('ez-explorer-recent', JSON.stringify(recent))
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

  /* 面板本体不搞「点外部即收」——在预览区随手点一下就收面板太烦，
     收起只认显式出口（头部 X / 资源页开关钮）。这里只管瞬态：面板外的
     点击收目录下拉；变更视图是面板的常态之一，不随外点收起 */
  function onOutside(e: PointerEvent) {
    const t = e.target as HTMLElement
    if (menuOpen && rootEl && !rootEl.contains(t)) menuOpen = false
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

<aside class="tree" class:docked class:closed={!open} bind:this={rootEl} onpointerdown={onPanelDown}>
  <header>
    <!-- 根目录按钮：完整路径（尾部优先显示），点击弹目录下拉 -->
    <button class="rootbtn" onclick={() => { menuOpen = !menuOpen; if (menuOpen) changesOpen = false }} title={explorer.dir}>
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
        <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />
      </svg>
      <span class="dpath">{explorer.dir || '目录'}</span>
      <svg class="chevdown" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
        <path d="M6 9l6 6 6-6" />
      </svg>
    </button>
    <!-- git chip = 变更清单开关：在仓库内可点（展开本目录下的未提交
         文件，点文件直接进 HEAD 对比）；不在仓库内禁用置灰 -->
    <button
      class="gitchip"
      class:clean={!rootChangedCount}
      class:on={changesOpen}
      disabled={!explorer.repo}
      title={!explorer.repo
        ? '目录不在 git 仓库内'
        : changesOpen
          ? '切回目录视图'
          : `查看本目录的未提交变更（${rootChangedCount} 个文件）——点文件直接进 HEAD 对比。树里点文件永远正常打开，被 .gitignore 忽略的文件没有变更概念`}
      onclick={toggleChanges}
    >
      git{rootChangedCount ? ` ${rootChangedCount}` : ''}
    </button>
    <span class="acts">
      <button
        class="abtn"
        class:on={docked}
        onclick={() => setExplorerDocked(!docked)}
        title={docked ? '切回悬浮（盖在预览区上，不占宽）' : '钉到左侧（占据预览宽度，宽屏推荐）'}
      >
        <svg viewBox="0 0 24 24" fill={docked ? 'currentColor' : 'none'} stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <path d="M12 17v5" />
          <path d="M9 3h6l-1 6 3 3v2H7v-2l3-3-1-6z" />
        </svg>
      </button>
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
  {#if changesOpen}
    <!-- 变更视图：目录树原地切换（点 git chip 切回目录），文件名 + 所在
         目录（仓库相对），点文件直接进 HEAD 对比 -->
    <div class="rows">
      {#each changeList as c (c.abs)}
        <button
          class="citem"
          class:del={c.code.trim() === 'D'}
          disabled={c.code.trim() === 'D'}
          onclick={() => onopen?.(c.abs, { diff: true })}
          title={c.code.trim() === 'D' ? '文件已删除' : `${c.abs}——点击进 HEAD 对比`}
        >
          <span class="cbadge {statusLetter(c.code)}">{statusLetter(c.code)}</span>
          <span class="cname">{c.name}</span>
          {#if c.dir}<span class="cdir">{c.dir}</span>{/if}
        </button>
      {:else}
        <div class="cempty">当前目录没有未提交的变更</div>
      {/each}
    </div>
  {:else}
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
  {/if}
</aside>

{#if ctx}
  <div class="ctx" style="left:{ctx.x}px; top:{ctx.y}px" role="menu">
    <button role="menuitem" disabled={!canReveal} title={canReveal ? '' : '仅桌面端可用'} onclick={revealCtx}>在资源管理器打开</button>
  </div>
{/if}

<style>
  /* 悬浮面板：absolute 盖在预览区左侧（.body 是定位上下文），不参与布局
     ——预览区始终全宽；高度按内容收缩（上限抽屉内高），不高垂到底。
     不设 overflow: hidden——目录下拉要悬浮溢出面板底缘（面板不随之长
     高），圆角裁剪由 header / .rows 各自承担 */
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
  }
  /* 钉住态：不再是悬浮层，成为 file-pane grid 的左列实体栏——整列全
     高、占布局宽，tab 条与内容只占右列；去圆角/阴影/边框，只留右缘
     分隔线。position 用 relative 不用 static：面板内的下拉/清单以树头
     定位，static 会改挂到滚动祖先上 */
  .tree.docked {
    position: relative;
    top: auto;
    left: auto;
    width: 260px; /* 最小宽抽屉里 320 太占地，钉住态收窄（文件名省略号兜底） */
    min-width: 0;
    height: auto;
    min-height: 0;
    max-height: none;
    z-index: auto;
    border: none;
    border-right: 1px solid var(--line);
    border-radius: 0;
    box-shadow: none;
    /* 展开/收起 = 宽度收放（grid auto 列跟随），头栏定宽避免挤压重排，
       面板 overflow 裁掉探出部分。钉住态面板全高，下拉清单不会越出
       底缘，overflow 不误伤 */
    overflow: hidden;
    transition:
      width 0.26s var(--ease-out),
      border-right-width 0.26s var(--ease-out);
  }
  .tree.docked.closed {
    width: 0;
    border-right-width: 0;
  }
  .tree.docked > header {
    min-width: 260px;
    box-sizing: border-box;
  }
  /* 悬浮态收起：左移淡出（absolute 不占布局，无需宽度动画）；
     visibility 延迟到动画结束再隐藏，展开时立即可见 */
  .tree:not(.docked) {
    transition:
      transform 0.22s var(--ease-out),
      opacity 0.22s var(--ease-out),
      visibility 0s linear 0s;
  }
  .tree:not(.docked).closed {
    opacity: 0;
    transform: translateX(-12px);
    pointer-events: none;
    visibility: hidden;
    transition:
      transform 0.22s var(--ease-out),
      opacity 0.22s var(--ease-out),
      visibility 0s linear 0.22s;
  }
  .tree.docked header {
    border-radius: 0;
  }
  .tree.docked .rows {
    border-radius: 0;
  }
  header {
    flex: none;
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px 8px 6px 8px;
    border-bottom: 1px solid var(--line);
    background: var(--bg-soft);
    border-radius: 11px 11px 0 0; /* 面板无 overflow 裁剪，圆角自己收 */
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
  .citem {
    display: flex;
    align-items: center;
    gap: 6px;
    width: 100%;
    padding: 4px 6px;
    border: none;
    border-radius: 7px;
    background: transparent;
    color: var(--fg);
    font: inherit;
    font-size: var(--font-size-ui);
    text-align: left;
    cursor: pointer;
    transition: background var(--dur-fast) var(--ease-out);
  }
  .citem:hover:not(:disabled) {
    background: var(--bg-soft);
  }
  .citem:disabled {
    cursor: default;
  }
  /* 已删除文件：灰显只陈列（无法打开进对比） */
  .citem.del {
    color: var(--faint);
    text-decoration: line-through;
  }
  .cbadge {
    flex: none;
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    line-height: 1;
    padding: 1px 4px;
    border-radius: 4px;
  }
  .cbadge.M {
    color: #b45309;
    background: color-mix(in srgb, #f0883e 16%, transparent);
  }
  .cbadge.U,
  .cbadge.A {
    color: #1a7f37;
    background: color-mix(in srgb, #1a7f37 12%, transparent);
  }
  .cbadge.D {
    color: #cf222e;
    background: color-mix(in srgb, #cf222e 12%, transparent);
  }
  .cname {
    flex: none;
    max-width: 55%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  /* 所在目录（仓库相对）：灰显右对齐，VSCode 源代码管理同款布局 */
  .cdir {
    flex: 1;
    min-width: 0;
    direction: rtl;
    text-align: left;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--faint);
    font-size: var(--font-size-small);
  }
  .cempty {
    padding: 6px;
    color: var(--faint);
    font-size: var(--font-size-ui);
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
  /* 变更清单展开中：绿色（有变更可对比的“健康”信号色） */
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
  /* 钉住中：图钉高亮（实心已钉） */
  .abtn.on {
    color: var(--accent, #2563eb);
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
    border-radius: 0 0 11px 11px; /* 面板无 overflow 裁剪，圆角自己收 */
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
