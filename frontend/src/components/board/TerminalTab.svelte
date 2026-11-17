<script lang="ts">
  import { onDestroy, onMount } from 'svelte'
  import { Terminal, type ITheme } from '@xterm/xterm'
  import { FitAddon } from '@xterm/addon-fit'
  import { Unicode11Addon } from '@xterm/addon-unicode11'
  import '@xterm/xterm/css/xterm.css'
  import { termManager, type TermInfo } from '../../lib/term'
  import { store } from '../../lib/store.svelte'
  import { theme, type Theme } from '../../lib/theme.svelte'
  import { paneTabs } from '../../lib/paneTabs.svelte'
  import AppLogo from '../AppLogo.svelte'
  import { api, type AppEntry } from '../../lib/api'

  /* 魔法看板·终端 tab：每终端一个 xterm 实例（切走不丢屏）；快应用后端
     单独分组、关闭需显式确认——长驻服务混在干活终端里容易误关。 */
  let { active = false }: { active?: boolean } = $props()

  let root: HTMLElement | undefined = $state()
  let sessions = $state<TermInfo[]>([])
  let current = $state('')
  let connected = $state(false)

  const terms = new Map<string, { term: Terminal; fit: FitAddon; uni: Unicode11Addon }>()
  /* hello 快照可能先于 holder 挂载到达（$state 渲染是异步的），先缓存，
  holder action 挂载时 flush */
  const pendingSnaps = new Map<string, Uint8Array>()
  const offs: Array<() => void> = []
  let started = false
  let ro: ResizeObserver | undefined

  const enc = new TextEncoder()

  /* ── 快应用后端分组与关闭保护 ── */
  const APP_ORIGIN = '快应用·'
  const isAppTerm = (s: TermInfo) => s.origin.startsWith(APP_ORIGIN)
  const workTerms = $derived(sessions.filter((s) => !isAppTerm(s)))
  const appTerms = $derived(sessions.filter(isAppTerm))
  const appNameOf = (s: TermInfo) => s.origin.slice(APP_ORIGIN.length)

  /* 后端条目用应用自己的头像（清单里的 icon，缺省首字块）而不是统一的
     闪电 emoji——emoji 带固定色，与主题和项目图标风格都不搭 */
  let appMeta = $state<Record<string, { title: string; icon: string }>>({})
  function metaOf(name: string) {
    return appMeta[name] ?? { title: name, icon: '' }
  }

  async function loadAppMeta() {
    try {
      const { apps } = await api.getApps()
      const map: Record<string, { title: string; icon: string }> = {}
      for (const a of (apps ?? []) as AppEntry[]) map[a.name] = { title: a.title, icon: a.icon ?? '' }
      appMeta = map
    } catch {
      /* core 不可达：保留现状（首字块兜底） */
    }
  }

  /* 活着的后端终端关闭：弹框显式确认，不做隐性二次点击 */
  let stopTarget = $state<TermInfo | null>(null)

  function closeOne(e: Event, s: TermInfo) {
    e.stopPropagation()
    if (isAppTerm(s) && !s.exited) {
      stopTarget = s
      return
    }
    void termManager.close(s.id)
  }

  function stopBackend() {
    const s = stopTarget
    stopTarget = null
    if (s) void termManager.close(s.id)
  }

  /* 终端双色板：跟应用主题（暗板取 GitHub Dark 系，ANSI 全段在深底上可辨）。 */
  const TERM_THEMES: Record<Theme, ITheme> = {
    light: {
      background: '#ffffff',
      foreground: '#0a0a0a',
      cursor: '#0a0a0a',
      selectionBackground: '#dbeafe',
      black: '#0a0a0a',
      red: '#c0392b',
      green: '#1e8449',
      yellow: '#b7950b',
      blue: '#2563eb',
      magenta: '#8e44ad',
      cyan: '#148f77',
      white: '#58d68d',
      brightBlack: '#7f8c8d',
      brightRed: '#e74c3c',
      brightGreen: '#2ecc71',
      brightYellow: '#f1c40f',
      brightBlue: '#5dade2',
      brightMagenta: '#af7ac5',
      brightCyan: '#48c9b0',
      brightWhite: '#ffffff',
    },
    dark: {
      background: '#1e2025',
      foreground: '#d7dbe2',
      cursor: '#d7dbe2',
      selectionBackground: '#2c5a92',
      black: '#454b54',
      red: '#ff7b72',
      green: '#3fb950',
      yellow: '#d29922',
      blue: '#58a6ff',
      magenta: '#bc8cff',
      cyan: '#39c5cf',
      white: '#b9bfc9',
      brightBlack: '#6e7681',
      brightRed: '#ffa198',
      brightGreen: '#56d364',
      brightYellow: '#e3b341',
      brightMagenta: '#d2a8ff',
      brightCyan: '#56d4dd',
      brightWhite: '#e6e8eb',
    },
  }

  /* 主题切换时给所有已建实例换肤（新建实例在 newTerm 里取当下主题） */
  $effect(() => {
    const t = TERM_THEMES[theme.current]
    for (const { term } of terms.values()) term.options.theme = t
  })

  /* 实例选项：真实终端与测尺寸的探针共用同一套字体，列行数才对得上 */
  function termOptions() {
    return {
      allowProposedApi: true, // unicode.activeVersion 是 proposed API,不开会抛异常中断 open
      fontSize: 13,
      fontFamily: getComputedStyle(document.documentElement).getPropertyValue('--font-mono') || 'Consolas, monospace',
      cursorBlink: true,
      scrollback: 5000,
      theme: TERM_THEMES[theme.current],
    }
  }

  function newTerm(id: string) {
    const term = new Terminal(termOptions())
    const fit = new FitAddon()
    const uni = new Unicode11Addon()
    term.loadAddon(fit)
    term.loadAddon(uni)
    term.unicode.activeVersion = '11'
    /* Ctrl/Cmd+V 放行给浏览器原生 paste：xterm 默认把 ctrl+字母转成控制
       字符（V→0x16）并吞掉 keydown，原生粘贴永不触发；返回 false 不拦，
       helper textarea 上的 paste 监听会接手走 onData。
       Ctrl/Cmd+C 有选区时放行原生 copy（xterm 的 copy 监听写剪贴板），
       无选区维持 \x03 中断——Windows Terminal 同款语义 */
    term.attachCustomKeyEventHandler((ev) => {
      /* Ctrl（Windows/Linux）与 Cmd（macOS）都放行给原生复制粘贴 */
      if (ev.type !== 'keydown' || !(ev.ctrlKey || ev.metaKey) || ev.altKey) return true
      const k = ev.key.toLowerCase()
      if (k === 'v') return false
      if (k === 'c' && term.hasSelection()) return false
      return true
    })
    term.onData((d) => termManager.input(id, enc.encode(d)))
    return { term, fit, uni }
  }

  /* svelte action:terminal holder 挂载时 open 对应 xterm 实例 */
  function holder(el: HTMLElement, id: string) {
    let t = terms.get(id)
    if (!t) {
      t = newTerm(id)
      terms.set(id, t)
    }
    t.term.open(el)
    const snap = pendingSnaps.get(id)
    if (snap) {
      pendingSnaps.delete(id)
      t.term.write(snap)
    }
    if (id === current) {
      t.fit.fit()
      t.term.focus()
    }
    return {
      destroy() {
        /* keep-alive:切 tab/收起不销毁;销毁仅发生在终端关闭或组件卸载 */
      },
    }
  }

  function syncSessions(ss: TermInfo[]) {
    const seen = new Set(ss.map((s) => s.id))
    for (const id of [...terms.keys()]) {
      if (!seen.has(id)) {
        terms.get(id)?.term.dispose()
        terms.delete(id)
      }
    }
    const prevIds = sessions.map((s) => s.id)
    sessions = ss
    if (!ss.length) {
      current = ''
      return
    }
    if (!current || !seen.has(current)) {
      /* 切换到新增终端(AI 刚建的)或保底第一个 */
      const fresh = ss.find((s) => !prevIds.includes(s.id) && !s.exited)
      current = (fresh ?? ss.find((s) => !s.exited) ?? ss[0]).id
    }
  }

  function switchTo(id: string) {
    current = id
    requestAnimationFrame(() => {
      const t = terms.get(id)
      t?.fit.fit()
      termManager.resize(id, t?.term.cols ?? 80, t?.term.rows ?? 24)
      t?.term.focus()
    })
  }

  /* 外部定位请求（supper_url term:// 点击）：目标终端到达清单后切换。
     依赖 sessions（$state）——抽屉首次打开 WS hello 到达时重试，不丢。 */
  $effect(() => {
    const want = store.termFocus
    if (!want || !sessions.some((s) => s.id === want)) return
    store.termFocus = ''
    switchTo(want)
  })

  /* paneSize 量看板的实际列行数:已有实例直接复用(同容器、常驻 fit);
     首建时在容器里起一个隐藏探针(与 holder 同几何)跑一次 fit。量不出来
     返回 0,后端退回默认尺寸。 */
  function paneSize(): { cols: number; rows: number } {
    const t = terms.get(current) ?? [...terms.values()][0]
    if (t) return { cols: t.term.cols, rows: t.term.rows }
    if (!root) return { cols: 0, rows: 0 }
    /* 探针节点要自己写几何:Svelte 的 scoped 样式只命中模板里的 .holder,
       运行时创建的节点带不上作用域哈希 */
    const el = document.createElement('div')
    el.style.cssText = 'position:absolute;inset:6px 8px;visibility:hidden'
    root.appendChild(el)
    const probe = new Terminal(termOptions())
    const fit = new FitAddon()
    probe.loadAddon(fit)
    probe.open(el)
    let size = { cols: 0, rows: 0 }
    try {
      fit.fit()
      size = { cols: probe.cols, rows: probe.rows }
    } catch {
      /* 容器尺寸不可测:交给后端默认 */
    }
    probe.dispose()
    el.remove()
    return size
  }

  async function createOne() {
    try {
      const { cols, rows } = paneSize()
      const info = await termManager.create(undefined, undefined, cols, rows)
      /* 乐观合并:不等 WS terminals 帧(连接异常时也能立即显示) */
      if (!sessions.find((s) => s.id === info.id)) sessions = [...sessions, info]
      switchTo(info.id)
    } catch (e) {
      console.error(e)
    }
  }

  function fitCurrent() {
    if (!active || !root || root.clientWidth === 0) return
    const t = terms.get(current)
    if (!t) return
    try {
      t.fit.fit()
    } catch {
      return
    }
    termManager.resize(current, t.term.cols, t.term.rows)
  }

  function connect() {
    if (started) return
    started = true
    termManager.connect()
    offs.push(termManager.onSessions(syncSessions))
    offs.push(
      termManager.onData((id, bytes) => {
        terms.get(id)?.term.write(bytes)
      }),
    )
    offs.push(
      termManager.onSnapshot((id, bytes) => {
        const t = terms.get(id)
        if (t) {
          t.term.reset()
          t.term.write(bytes)
        } else {
          pendingSnaps.set(id, bytes)
        }
      }),
    )
    offs.push(termManager.onState((c) => (connected = c)))

    ro = new ResizeObserver(() => fitCurrent())
    if (root) ro.observe(root)

    /* hello 前先拉一次清单(WS 尚未 hello 时兜底;连接后 hello 会再同步) */
    void fetch('/api/terminal/list')
      .then((r) => (r.ok ? r.json() : []))
      .then((ss: TermInfo[]) => {
        if (started) syncSessions(ss)
      })
      .catch(() => {})
  }

  onMount(() => {
    if (active) connect()
    /* 后端条目头像：启动拉一次；快应用页钉选/新建/卸载后广播，跟着刷新 */
    void loadAppMeta()
    const onApps = () => void loadAppMeta()
    window.addEventListener('ez:apps-changed', onApps)
    return () => window.removeEventListener('ez:apps-changed', onApps)
  })

  $effect(() => {
    if (active && !started) connect()
    if (active) requestAnimationFrame(fitCurrent)
  })

  /* 会话数上报顶栏角标 */
  $effect(() => (paneTabs.term = sessions.length))

  onDestroy(() => {
    offs.forEach((off) => off())
    ro?.disconnect()
    terms.forEach((t) => t.term.dispose())
    terms.clear()
    paneTabs.term = 0
  })
</script>

<div class="term-tab">
  <div class="strip">
    {#if workTerms.length && appTerms.length}<span class="grouplab">工作</span>{/if}
    {#each workTerms as s (s.id)}
      <button class="strip-item" class:active={current === s.id} onclick={() => switchTo(s.id)} title="{s.name}{s.desc ? ` · ${s.desc}` : ''} · 来源 {s.origin || '?'}">
        <span class="dot" class:exited={s.exited}></span>
        <span class="name">{s.name}</span>
        <span class="x" onclick={(e) => closeOne(e, s)} role="button" tabindex="-1" title="关闭">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round">
            <path d="M6 6l12 12M18 6L6 18" />
          </svg>
        </span>
      </button>
    {/each}
    {#if appTerms.length}
      <span class="grouplab" title="快应用的后端服务终端（agent 启动应用时拉起；应用卸载时自动收掉）">快应用后端</span>
      {#each appTerms as s (s.id)}
        <button class="strip-item app" class:active={current === s.id} onclick={() => switchTo(s.id)} title="{s.name}{s.desc ? ` · ${s.desc}` : ''} · 来源 {s.origin || '?'}">
          <span class="dot" class:exited={s.exited}></span>
          <AppLogo
            size={13}
            name={appNameOf(s)}
            title={metaOf(appNameOf(s)).title}
            icon={metaOf(appNameOf(s)).icon}
          />
          <span class="name">{appNameOf(s)}</span>
          <span class="x" onclick={(e) => closeOne(e, s)} role="button" tabindex="-1" title="关闭（先确认再停止后端）">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round">
              <path d="M6 6l12 12M18 6L6 18" />
            </svg>
          </span>
        </button>
      {/each}
    {/if}
    <button class="new" onclick={() => void createOne()} title="新建终端">+</button>
    <span class="conn" class:off={!connected}>{connected ? '' : '重连中…'}</span>
  </div>

  <div class="terms" bind:this={root}>
    {#each sessions as s (s.id)}
      <div class="holder" class:hidden={current !== s.id} use:holder={s.id}></div>
    {/each}
    {#if !sessions.length}
      <div class="empty">
        <p>还没有终端</p>
        <button onclick={() => void createOne()}>新建一个终端</button>
      </div>
    {/if}
  </div>

  {#if stopTarget}
    <div class="mask" onclick={() => (stopTarget = null)}></div>
    <div class="modal" role="dialog" aria-label="停止快应用后端">
      <p class="mtitle">停止「{metaOf(appNameOf(stopTarget)).title}」的后端？</p>
      <p class="mtext">后端服务会随之退出，应用将不可用。</p>
      <div class="macts">
        <button class="btn" onclick={() => (stopTarget = null)}>取消</button>
        <button class="btn danger" onclick={stopBackend}>停止后端</button>
      </div>
    </div>
  {/if}
</div>

<style>
  .term-tab {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    gap: 10px;
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
  /* 分组标签：后端组在工作终端之后，弱化存在但一眼可分 */
  .grouplab {
    flex: none;
    padding: 0 2px;
    font-size: var(--font-size-small);
    color: var(--faint);
    user-select: none;
    white-space: nowrap;
  }
  .name {
    max-width: 120px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: #2ecc71;
    flex: none;
  }
  .dot.exited {
    background: #e74c3c;
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
  .new {
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    border: 1px dashed var(--line-strong);
    background: transparent;
    border-radius: 9px;
    color: var(--muted);
    font-size: var(--font-size-body);
    line-height: 1;
  }
  .new:hover {
    background: var(--bg-soft);
    color: var(--fg);
    border-style: solid;
  }
  .conn {
    margin-left: auto;
    font-size: var(--font-size-small);
    font-family: var(--font-mono);
    color: var(--faint);
  }
  .conn:empty {
    display: none;
  }
  .terms {
    position: relative;
    flex: 1;
    min-height: 0;
    border: 1px solid var(--line);
    border-radius: 12px;
    background: var(--bg);
    overflow: hidden;
    padding: 6px 8px;
  }
  .holder {
    position: absolute;
    inset: 6px 8px;
  }
  .holder.hidden {
    display: none;
  }
  .empty {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 12px;
    color: var(--faint);
    font-size: var(--font-size-section);
  }
  .empty button {
    border: 1px solid var(--line-strong);
    background: var(--bg-invert);
    color: var(--fg-invert);
    border-radius: 10px;
    padding: 8px 18px;
    font-size: var(--font-size-section);
    font-weight: 600;
  }
  /* 停止后端确认弹框：遮罩 + 居中卡片，两个动作键，无多余分割线 */
  .mask {
    position: fixed;
    inset: 0;
    z-index: 70;
    background: rgb(0 0 0 / 18%);
  }
  .modal {
    position: fixed;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    z-index: 71;
    width: min(340px, 88vw);
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 18px;
    background: var(--bg);
    border: 1px solid var(--line-strong);
    border-radius: 12px;
    box-shadow: 0 16px 48px rgb(0 0 0 / 20%);
  }
  .mtitle {
    font-size: var(--font-size-body);
    font-weight: 600;
    color: var(--fg);
  }
  .mtext {
    font-size: var(--font-size-ui);
    line-height: 1.6;
    color: var(--muted);
  }
  .macts {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 4px;
  }
  .btn {
    border: 1px solid var(--line);
    border-radius: 8px;
    background: transparent;
    color: var(--muted);
    font: inherit;
    font-size: var(--font-size-ui);
    padding: 6px 14px;
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
  .btn.danger:hover {
    background: #a93226;
    border-color: #a93226;
  }
</style>
