<script lang="ts">
  import { onMount } from 'svelte'
  import { api } from '../../lib/api'
  import { fileBaseName, normFileKey } from '../../lib/textfile'
  import { store } from '../../lib/store.svelte'
  import { filePane } from '../../lib/filePaneState'
  import { autoDiffOn, explorer, hasGitChange } from '../../lib/explorerState.svelte'
  import { kindOf, VIEWERS, MAX_BYTES, type ResTab } from './registry'
  import KindIcon from './KindIcon.svelte'
  import ExplorerTree from './ExplorerTree.svelte'
  import TextViewer from './viewers/TextViewer.svelte'
  import ImageViewer from './viewers/ImageViewer.svelte'
  import HtmlViewer from './viewers/HtmlViewer.svelte'
  import MarkdownViewer from './viewers/MarkdownViewer.svelte'
  import DiffViewer from './viewers/DiffViewer.svelte'

  /*
  工作区抽屉·资源查看器（file:// 的消费端，通用框架）：多 tab 按扩展名
  路由到各资源专属 viewer（registry 单一来源，新资源=新组件+此处挂载）。
  左侧目录树（可收起）浏览文件系统，默认工作目录；git 仓库内的文本文件
  可切「变更对比」（HEAD ↔ 当前，DiffViewer 只读观测）。
  磁盘是唯一真身——一切皆资源（画笔钮草稿也是 tmp 下的一张图片）：
  文本系（text/md/html 原文）单编辑器+tab 状态数据化（单实例重绑）；
  图片 tab per-tab 实例保活（画布状态不可数据化，改动防抖自动写回）。
  状态条统一：保存/Ctrl+S、添加到对话（纯文件引用 chip，经
  <reference_file> 告知模型）。磁盘变更：激活时每 2s HEAD 轮询
  Last-Modified（AI 改盘后），干净 tab 自动重载，dirty tab 只提示。
  */

  /* push 仅弹出窗口传入：状态变化即上报主进程（关窗回流取最新快照） */
  let { active = false, push }: { active?: boolean; push?: (json: string) => void } = $props()

  let tabs = $state<ResTab[]>([])
  let current = $state('')
  let pendingClose = $state('')
  let sending = $state(false)
  let textViewer = $state<TextViewer>()
  /* 目录树（悬浮面板）显隐：不占布局宽，默认收起；记忆上次选择 */
  let explorerOpen = $state(localStorage.getItem('ez-explorer-open') === '1')

  const tab = $derived(tabs.find((t) => t.key === current))
  const dirty = $derived(!!tab && tab.kind !== 'image' && tab.content !== tab.saved)
  const textish = $derived(!!tab && ['text', 'markdown', 'html'].includes(tab.kind))
  const imageTabs = $derived(tabs.filter((t) => t.kind === 'image'))
  /* 二进制类(pdf/媒体/表格/文档/幻灯片/兜底)统一从 VIEWERS 表取组件即挂 */
  const binViewer = $derived(tab && !textish && tab.kind !== 'image' ? VIEWERS[tab.kind] : undefined)

  function blankTab(part: Partial<ResTab>): ResTab {
    return {
      key: '', kind: 'text', name: '', content: '', saved: '',
      loading: false, err: '', warn: '', savedAt: '', lastMod: '',
      ...part,
    }
  }

  /* 跨窗口快照：tab 状态（含当前选中）整体搬运——草稿也是一张 tmp 图片，
     与普通资源走同一条路 */
  function serializeTabs(): string {
    return JSON.stringify({ tabs, current })
  }

  function restoreTabs(json: string) {
    try {
      const data = JSON.parse(json) as { tabs?: Partial<ResTab>[]; current?: string }
      const prev = new Map(tabs.map((t) => [t.key, t]))
      /* rev 递增：图片 tab 重挂按盘重读——另一个窗口写回的新图只落在
         磁盘上，本窗口的内存画布是旧的 */
      tabs = (data.tabs ?? []).map((t) => blankTab({ ...t, loading: false, rev: (prev.get(t.key ?? '')?.rev ?? 0) + 1 }))
      if (!tabs.some((t) => t.key === current)) current = data.current || tabs[0]?.key || ''
    } catch {
      /* 快照损坏：保持现状 */
    }
  }

  /* 本窗口唯一的 ResourcePane 实例向状态胶囊注册实现（弹出方序列化 /
     弹出窗口与回流方恢复） */
  onMount(() => {
    filePane.serialize = serializeTabs
    filePane.restore = restoreTabs
  })

  $effect(() => {
    if (push) push(serializeTabs())
  })

  /* 外部定位请求（supper_url file:// 点击 / 资源入口）：文件按需即开 */
  $effect(() => {
    const want = store.fileFocus
    if (!want) return
    store.fileFocus = ''
    void openTab(want)
  })

  /* 内存画板草稿（画笔钮/消息 base64 入口）：不落盘直接开 tab，保存时
     才 stash 成真身回填 path（draftSeq 保证多草稿 key 唯一） */
  let draftSeq = 0
  $effect(() => {
    const src = store.fileDraftSrc
    if (!src) return
    store.fileDraftSrc = ''
    draftSeq++
    const t = blankTab({ key: `draft:${draftSeq}`, kind: 'image', name: '画板草稿', draftSrc: src })
    tabs = [...tabs, t]
    current = t.key
  })

  /* 磁盘变更轮询：仅资源页激活且当前 tab 是文本系时进行 */
  $effect(() => {
    if (!active) return
    const id = setInterval(() => void pollDisk(), 2000)
    return () => clearInterval(id)
  })

  async function openTab(path: string) {
    const kind = kindOf(path)
    const key = normFileKey(path)
    if (tabs.some((t) => t.key === key)) {
      current = key
      return
    }
    const t = blankTab({ key, kind, path: path.trim(), name: fileBaseName(path.trim()) })
    tabs = [...tabs, t]
    current = key
    if (kind === 'text' || kind === 'markdown' || kind === 'html') {
      /* 必须从 $state 数组取代理版：闭包持有原始对象的话，await 后的
      属性写入不触发响应（loading 永不消失） */
      const cur = tabs[tabs.length - 1]
      cur.loading = true
      try {
        const r = await fetch('/api/workspace/file?path=' + encodeURIComponent(cur.path!), { cache: 'no-store' })
        if (!r.ok) {
          cur.err = `文件读取失败（${r.status}）`
          return
        }
        const clen = Number(r.headers.get('content-length') || 0)
        const text = await r.text()
        if (clen > MAX_BYTES || text.length > MAX_BYTES) {
          cur.err = '文件过大（>2MB），不支持在线编辑'
          return
        }
        if (text.includes('\uFFFD')) {
          cur.warn = '文件疑似非 UTF-8 编码：显示可能乱码，保存后会转为 UTF-8'
        }
        cur.content = text
        cur.saved = text
        cur.lastMod = r.headers.get('last-modified') || ''
      } catch (e) {
        cur.err = `文件读取失败：${(e as Error).message}`
      } finally {
        cur.loading = false
      }
    }
  }

  /* 目录树点开文件：当前根目录开着自动对比时，有未提交变更的文本文件
     直接进对比视图（开关在树头部的 git chip 上，按目录各自记忆）；
     面板保持开着（连续浏览多个文件），当前文件在树内高亮 */
  function openFromTree(abs: string) {
    void openTab(abs)
    if (autoDiffOn(explorer.dir) && hasGitChange(abs) && ['text', 'markdown', 'html'].includes(kindOf(abs))) {
      const t = tabs.find((x) => x.key === normFileKey(abs))
      if (t) t.diff = true
    }
  }

  /* 当前 tab 同步给目录树（树内高亮打开中的文件） */
  $effect(() => {
    explorer.activeFile = tab?.path ?? ''
  })

  /* 变更对比开关（状态条按钮；仅 git 仓库内的文本系 tab 显示） */
  function toggleDiff() {
    if (tab && textish) tab.diff = !tab.diff
  }

  /* 干净 tab 磁盘有更新时自动重载（AI 改盘后）；dirty 只提示不覆盖 */
  async function pollDisk() {
    const t = tab
    if (!t || !t.path || t.kind !== 'text' || t.loading || t.err) return
    try {
      const h = await fetch('/api/workspace/file?path=' + encodeURIComponent(t.path), {
        method: 'HEAD',
        cache: 'no-store',
      })
      if (!h.ok) return
      const lm = h.headers.get('last-modified') || ''
      if (!lm || lm === t.lastMod) return
      t.lastMod = lm
      if (t.content !== t.saved) {
        t.warn = '磁盘文件已被更新（AI 修改）：未自动加载，保存将覆盖'
        return
      }
      const y = await fetch('/api/workspace/file?path=' + encodeURIComponent(t.path), { cache: 'no-store' })
      if (!y.ok) return
      const text = await y.text()
      const st = textViewer?.scrollTop() ?? 0
      t.content = text
      t.saved = text
      t.savedAt = new Date().toTimeString().slice(0, 5)
      requestAnimationFrame(() => textViewer?.restoreScroll(st))
    } catch {
      /* 轮询失败静默，下轮再试 */
    }
  }

  function closeTab(key: string) {
    const t = tabs.find((x) => x.key === key)
    if (!t) return
    if (t.kind !== 'image' && t.content !== t.saved) {
      pendingClose = key
      return
    }
    removeTab(key)
  }

  /* ── 右键菜单（与会话标签条同款交互）：关闭 / 关闭其他 / 关闭右侧。
     批量关只立即关干净 tab；带未保存修改的保留（橙点可见），不静默丢弃 */
  let menu = $state<{ x: number; y: number; key: string } | null>(null)
  const menuKey = $derived(menu?.key ?? '')
  const menuIdx = $derived(menuKey ? tabs.findIndex((t) => t.key === menuKey) : -1)
  const hasOthers = $derived(!!menu && tabs.length > 1)
  const hasRight = $derived(menuIdx >= 0 && menuIdx < tabs.length - 1)

  function openMenu(e: MouseEvent, key: string) {
    e.preventDefault()
    menu = {
      x: Math.min(e.clientX, window.innerWidth - 150),
      y: Math.min(e.clientY, window.innerHeight - 130),
      key,
    }
  }

  function runMenu(fn: (key: string) => void) {
    const key = menu?.key
    menu = null
    if (key !== undefined) fn(key)
  }

  /* dirty 判定与 closeTab 一致（文本系未保存；图片草稿未落盘视作干净——
     草稿本就丢弃无痕） */
  function isDirty(t: ResTab): boolean {
    return t.kind !== 'image' && t.content !== t.saved
  }

  function closeBatch(targets: ResTab[]) {
    for (const t of targets) {
      if (!isDirty(t)) removeTab(t.key)
    }
  }

  function closeRightOf(key: string) {
    const i = tabs.findIndex((t) => t.key === key)
    if (i >= 0) closeBatch(tabs.slice(i + 1))
  }

  function onGlobalDown(e: PointerEvent) {
    if (menu && !(e.target as HTMLElement).closest('.tabmenu')) menu = null
  }

  /* 右键菜单里的「在资源管理器打开」：当前 tab 有真身路径才可用（草稿没有） */
  const menuTabPath = $derived(menuKey ? (tabs.find((t) => t.key === menuKey)?.path ?? '') : '')

  function revealTab(key: string) {
    const t = tabs.find((x) => x.key === key)
    if (t?.path) (window as any).ez?.window?.reveal?.(t.path)
  }

  function removeTab(key: string) {
    const i = tabs.findIndex((t) => t.key === key)
    if (i < 0) return
    tabs = tabs.filter((t) => t.key !== key)
    pendingClose = ''
    if (current === key) current = tabs[Math.min(i, tabs.length - 1)]?.key ?? ''
  }

  async function saveTab(t: ResTab): Promise<boolean> {
    if (!t.path || t.loading || t.content === t.saved) return true
    try {
      await api.saveFile(t.path, t.content)
      t.saved = t.content
      t.savedAt = new Date().toTimeString().slice(0, 5)
      t.err = ''
      t.lastMod = '' /* 下一轮轮询重新建档，避免自己的写入误报"磁盘更新" */
      return true
    } catch (e) {
      t.err = `保存失败：${(e as Error).message}`
      return false
    }
  }

  /* dirty 关闭确认的「保存并关闭」 */
  async function saveAndClose(key: string) {
    const t = tabs.find((x) => x.key === key)
    if (!t) return
    if (!(await saveTab(t))) return
    removeTab(key)
  }

  /* 添加到对话（文本系）：纯文件引用 chip 塞进输入框（发送时经
  <reference_file> 告知模型，AI read_file 真身）；dirty 先自动保存 */
  async function addToChat() {
    const t = tab
    if (!t?.path || sending || t.loading) return
    if (t.content !== t.saved && !(await saveTab(t))) {
      t.err = t.err || '尚未保存成功，未添加'
      return
    }
    sending = true
    try {
      store.pendingFileRef = { path: t.path, items: [] }
    } finally {
      sending = false
    }
  }
</script>

<svelte:window onpointerdown={onGlobalDown} onkeydown={(e) => e.key === 'Escape' && (menu = null)} />

<div class="file-pane">
  <div class="strip">
    <button
      class="exptoggle"
      class:open={explorerOpen}
      onpointerdown={(e) => e.stopPropagation()}
      onclick={() => {
        explorerOpen = !explorerOpen
        localStorage.setItem('ez-explorer-open', explorerOpen ? '1' : '0')
      }}
      title={explorerOpen ? '收起目录面板' : '打开目录面板'}
    >
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
        <rect x="3" y="4" width="18" height="16" rx="2" />
        <path d="M9 4v16" />
      </svg>
    </button>
    {#each tabs as t (t.key)}
      <button
        class="strip-item"
        class:active={current === t.key}
        onclick={() => (current = t.key)}
        onauxclick={(e) => e.button === 1 && closeTab(t.key)}
        oncontextmenu={(e) => openMenu(e, t.key)}
        title={t.path || '画板草稿（未保存）'}
      >
        <span class="kind"><KindIcon kind={t.kind} /></span>
        <span class="name">{t.name}</span>
        {#if (t.kind !== 'image' && t.content !== t.saved) || (t.kind === 'image' && !t.path)}<i class="dirty"></i>{/if}
        <span class="x" onclick={(e) => { e.stopPropagation(); closeTab(t.key) }} role="button" tabindex="-1" title="关闭">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round">
            <path d="M6 6l12 12M18 6L6 18" />
          </svg>
        </span>
      </button>
    {/each}
  </div>

  {#if pendingClose}
    <div class="confirm">
      <span>有未保存的修改</span>
      <button onclick={() => void saveAndClose(pendingClose)}>保存并关闭</button>
      <button class="ghost" onclick={() => removeTab(pendingClose)}>放弃修改</button>
      <button class="ghost" onclick={() => (pendingClose = '')}>取消</button>
    </div>
  {/if}

  <div class="body">
    <!-- 目录悬浮面板：absolute 盖在预览区左侧，不占布局宽（选完文件自动收起） -->
    {#if explorerOpen}
      <ExplorerTree {active} onopen={openFromTree} onclose={() => (explorerOpen = false)} />
    {/if}
    <!-- 文本系：单编辑器 + tab 状态数据化（单实例重绑）；diff=变更对比视图 -->
    {#if tab && textish && tab.diff}
      <DiffViewer {tab} />
    {:else if tab && tab.kind === 'text'}
      <TextViewer bind:this={textViewer} {tab} {active} onSave={() => void saveTab(tab)} />
    {:else if tab && tab.kind === 'markdown'}
      <MarkdownViewer {tab} {active} onSave={() => void saveTab(tab)} />
    {:else if tab && tab.kind === 'html'}
      <HtmlViewer {tab} {active} onSave={() => void saveTab(tab)} />
    {:else if tab && binViewer}
      {@const Viewer = binViewer}
      <Viewer {tab} {active} />
    {:else if !imageTabs.length}
      <div class="empty">
        <p>还没有打开的资源</p>
        <p class="hint">左上角目录钮打开目录面板选文件 · 对话中的 <code>文件名</code> 入口也可点击</p>
      </div>
    {/if}
    <!-- 图片 tab：per-tab 实例保活（画板画布状态不可数据化）；
         rev 变化 = 跨窗口回流，重挂按盘重读 -->
    {#each imageTabs as t (t.key)}
      <div class="img-slot" class:hidden={current !== t.key}>
        {#key t.rev ?? 0}
          <ImageViewer tab={t} active={active && current === t.key} />
        {/key}
      </div>
    {/each}
  </div>

  {#if tab && textish}
    <!-- 统一状态条（文本系）：对比 + 保存 + 添加到对话 + 轻量状态 -->
    <div class="statusbar">
        {#if explorer.repo}
          <button class="diffbtn" class:on={tab.diff} onclick={toggleDiff} title={tab.diff ? '退出对比，回到编辑' : '查看与上次提交（HEAD）的变更对比'}>
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
              <circle cx="6" cy="18" r="2.2" />
              <circle cx="18" cy="6" r="2.2" />
              <path d="M6 15.8V9a3 3 0 0 1 3-3h4" />
              <path d="M11 4l2.5 2L11 8" />
              <path d="M18 8.2V15a3 3 0 0 1-3 3h-4" />
            </svg>
            对比
          </button>
        {/if}
        <button class="savebtn" class:dirty disabled={!dirty || tab.loading} onclick={() => void saveTab(tab)} title="保存（Ctrl+S）">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z" />
          <polyline points="17 21 17 13 7 13 7 21" />
          <polyline points="7 3 7 8 15 8" />
        </svg>
        {#if dirty}<i class="dot"></i>{/if}
      </button>
      <button class="addbtn" disabled={sending || tab.loading} onclick={() => void addToChat()} title="把文件作为引用添加到输入框（发送时经 <reference_file> 告知 AI，AI 自行 read_file 真身）">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <path d="M12 5v14M5 12h14" />
        </svg>
        {sending ? '添加中…' : '添加到对话'}
      </button>
      <span class="stat">
        {#if tab.err}<i class="bad">{tab.err}</i>
        {:else if tab.warn}<i class="bad">{tab.warn}</i>
        {:else if dirty}<i>未保存</i>
        {:else if tab.savedAt}<i>已保存 {tab.savedAt}</i>
        {/if}
      </span>
      <span class="tip">
        {tab.kind === 'text' ? '编辑后 Ctrl+S 保存写回' : '可切换预览/原文'}
      </span>
    </div>
  {/if}
</div>

{#if menu}
  <div class="tabmenu" style="left:{menu.x}px; top:{menu.y}px" role="menu">
    <button role="menuitem" onclick={() => runMenu((key) => closeTab(key))}>关闭</button>
    <button role="menuitem" disabled={!hasOthers} onclick={() => runMenu((key) => closeBatch(tabs.filter((t) => t.key !== key)))}>关闭其他</button>
    <button role="menuitem" disabled={!hasRight} onclick={() => runMenu(closeRightOf)}>关闭右侧</button>
    <div class="msep"></div>
    <button role="menuitem" disabled={!menuTabPath} title={menuTabPath ? '' : '草稿尚未保存到磁盘'} onclick={() => runMenu(revealTab)}>在资源管理器打开</button>
  </div>
{/if}

<style>
  .file-pane {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    gap: 8px;
  }
  /* 目录面板开关：tab 条最左的常驻小钮（pointerdown 不冒泡——
     面板的"点外部关闭"以此识别开关钮，避免点按即关又立即重开） */
  .exptoggle {
    flex: none;
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    border: 1px solid var(--line);
    border-radius: 8px;
    background: var(--bg);
    color: var(--faint);
    cursor: pointer;
    transition:
      color var(--dur-fast) var(--ease-out),
      background var(--dur-fast) var(--ease-out),
      border-color var(--dur-fast) var(--ease-out);
  }
  .exptoggle svg {
    width: 13px;
    height: 13px;
  }
  .exptoggle:hover,
  .exptoggle.open {
    color: var(--fg);
    background: var(--bg-soft);
  }
  .exptoggle.open {
    border-color: var(--line-strong);
  }
  /* 变更对比开关（状态条）：与保存钮同族，开启态反显 */
  .diffbtn {
    flex: none;
    display: flex;
    align-items: center;
    gap: 4px;
    height: 24px;
    padding: 0 8px;
    border: 1px solid var(--line);
    border-radius: 7px;
    background: transparent;
    color: var(--muted);
    font: inherit;
    font-size: var(--font-size-ui);
    cursor: pointer;
    transition:
      color var(--dur-fast) var(--ease-out),
      background var(--dur-fast) var(--ease-out),
      border-color var(--dur-fast) var(--ease-out);
  }
  .diffbtn svg {
    width: 12px;
    height: 12px;
  }
  .diffbtn:hover {
    color: var(--fg);
    background: var(--bg-soft);
  }
  .diffbtn.on {
    border-color: var(--accent-soft, var(--accent));
    color: var(--accent, #2563eb);
    background: var(--accent-soft, color-mix(in srgb, #2563eb 10%, transparent));
    font-weight: 600;
  }
  .body {
    position: relative; /* 目录悬浮面板的定位上下文 */
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .body > :global(*) {
    flex: 1;
    min-height: 0;
  }
  .img-slot {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
  }
  .img-slot.hidden {
    display: none;
  }
  .strip {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
    flex: none;
  }
  .strip-item {
    display: flex;
    align-items: center;
    gap: 6px;
    border: 1px solid var(--line);
    background: var(--bg);
    border-radius: 9px;
    padding: 4px 6px 4px 10px;
    font-size: var(--font-size-ui);
    color: var(--muted);
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out),
      border-color var(--dur-fast) var(--ease-out);
  }
  .strip-item:hover {
    background: var(--bg-soft);
    color: var(--fg);
  }
  .strip-item.active {
    border-color: var(--accent-soft, var(--accent));
    background: var(--bg-soft);
    color: var(--fg);
    font-weight: 600;
  }
  .kind {
    flex: none;
    font-size: var(--font-size-ui);
  }
  .name {
    max-width: 140px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .dirty {
    flex: none;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: #f0883e;
  }
  .x {
    display: grid;
    place-items: center;
    width: 16px;
    height: 16px;
    border-radius: 5px;
    color: var(--faint);
  }
  .x:hover {
    background: var(--line);
    color: var(--fg);
  }
  .x svg {
    width: 9px;
    height: 9px;
  }
  /* dirty 关闭二次确认条 */
  .confirm {
    flex: none;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 10px;
    border: 1px solid #f0883e;
    border-radius: 9px;
    background: color-mix(in srgb, #f0883e 8%, var(--bg));
    font-size: var(--font-size-ui);
    color: var(--fg);
  }
  .confirm button {
    border: 1px solid var(--line-strong);
    background: var(--bg);
    color: var(--fg);
    border-radius: 7px;
    padding: 3px 10px;
    font-size: var(--font-size-ui);
    cursor: pointer;
  }
  .confirm button.ghost {
    border-color: var(--line);
    color: var(--muted);
  }
  .empty {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 6px;
    color: var(--faint);
    font-size: var(--font-size-section);
  }
  .empty .hint {
    font-size: var(--font-size-ui);
  }
  .empty code {
    font-family: var(--font-mono);
    background: var(--bg-soft);
    border-radius: 4px;
    padding: 0 4px;
  }
  /* 统一状态条 */
  .statusbar {
    flex: none;
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 24px;
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .savebtn {
    position: relative;
    flex: none;
    display: grid;
    place-items: center;
    width: 24px;
    height: 24px;
    border: 1px solid transparent;
    border-radius: 7px;
    background: transparent;
    color: var(--faint);
    cursor: pointer;
    transition:
      color var(--dur-fast) var(--ease-out),
      background var(--dur-fast) var(--ease-out),
      border-color var(--dur-fast) var(--ease-out);
  }
  .savebtn svg {
    width: 13px;
    height: 13px;
  }
  .savebtn:hover:not(:disabled) {
    color: var(--fg);
    background: var(--bg-soft);
    border-color: var(--line);
  }
  .savebtn:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .savebtn.dirty {
    color: #f0883e;
  }
  .savebtn .dot {
    position: absolute;
    top: -2px;
    right: -2px;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: #f0883e;
  }
  /* 添加到对话：保存钮旁的常驻动作钮 */
  .addbtn {
    flex: none;
    display: flex;
    align-items: center;
    gap: 5px;
    border: 1px solid var(--accent-soft, var(--accent));
    background: transparent;
    color: var(--accent, #2563eb);
    border-radius: 7px;
    padding: 3px 10px;
    font-size: var(--font-size-ui);
    font-weight: 500;
    cursor: pointer;
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .addbtn svg {
    width: 12px;
    height: 12px;
  }
  .addbtn:hover:not(:disabled) {
    background: var(--accent-soft, color-mix(in srgb, #2563eb 10%, transparent));
  }
  .addbtn:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }
  .stat {
    min-width: 0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .stat i {
    font-style: normal;
  }
  .stat .bad {
    color: #e07a4f;
  }
  .tip {
    margin-left: auto;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  /* 资源 tab 右键菜单（与会话标签条同款） */
  .tabmenu {
    position: fixed;
    z-index: 60;
    display: flex;
    flex-direction: column;
    min-width: 108px;
    padding: 4px;
    border: 1px solid var(--line);
    border-radius: 9px;
    background: var(--bg);
    box-shadow: 0 6px 20px rgb(0 0 0 / 10%);
  }
  .tabmenu button {
    border: none;
    background: transparent;
    text-align: left;
    padding: 6px 10px;
    font-size: var(--font-size-ui);
    color: var(--fg);
    border-radius: 6px;
  }
  .tabmenu button:hover:not(:disabled) {
    background: var(--bg-soft);
  }
  .tabmenu button:disabled {
    color: var(--faint);
    cursor: default;
  }
  .tabmenu .msep {
    height: 1px;
    margin: 4px 2px;
    background: var(--line);
  }
</style>
