<script lang="ts">
  import { onDestroy, onMount } from 'svelte'
  import { browserManager, type BrowserTabInfo, type FrameImage } from '../../lib/browser'
  import { store } from '../../lib/store.svelte'

  /*
  魔法看板·浏览器 tab:被控 Chromium 的镜像投影(纯监视)。顶部切换条
  (全部标签,含 AI 新建的)+ 地址栏 + 每标签一个 keep-alive 镜像视图
  (<img> 帧流)。"打开大窗"唤起真实浏览器窗口——用户在真窗口里原生
  操作(点击/键盘/IME/视频,零坐标换算),与 AI 注入共写同一页面;
  "收起大窗"把窗口移回屏幕外,镜像继续监视。视口跟随容器尺寸(后端
  1:1 出帧)。空清单不自动建标签(浏览器有下载成本,用户/AI 明确要才建)。
  */
  let { active = false }: { active?: boolean } = $props()

  let views: HTMLElement | undefined = $state()
  let tabs = $state<BrowserTabInfo[]>([])
  let current = $state('')
  let connected = $state(false)
  let status = $state<{ phase: string; message: string } | null>(null)
  /* 真窗口是否在屏幕内(打开/收起大窗按钮状态) */
  let windowShown = $state(false)
  /* 每标签最新帧(响应式:赋值即重绘对应 <img>) */
  let frameMap = $state<Record<string, FrameImage>>({})
  /* 地址栏:展示当前标签 url,focus 后可编辑回车导航 */
  let addressValue = $state('')
  let addressFocused = false

  const holders = new Map<string, HTMLElement>()
  const offs: Array<() => void> = []
  let inited = false
  let ro: ResizeObserver | undefined
  let viewportTimer: number | undefined

  /* 地址栏跟随当前标签 url(未在编辑时) */
  $effect(() => {
    const url = tabs.find((t) => t.id === current)?.url ?? ''
    if (!addressFocused) addressValue = url
  })

  /* svelte action:登记 holder 元素(输入坐标换算基准),keep-alive 不销毁;
  挂载即补报视口(标签视图刚创建/恢复显示时容器尺寸才可用) */
  function bindHolder(el: HTMLElement, id: string) {
    holders.set(id, el)
    requestAnimationFrame(reportViewport)
    return {
      update(next: string) {
        if (next !== id) {
          holders.delete(id)
          id = next
          holders.set(id, el)
        }
      },
      destroy() {
        holders.delete(id)
      },
    }
  }

  /* ── 标签同步与视口跟随 ── */

  function syncTabs(next: BrowserTabInfo[]) {
    const seen = new Set(next.map((t) => t.id))
    for (const id of Object.keys(frameMap)) {
      if (!seen.has(id)) delete frameMap[id]
    }
    const prevIds = tabs.map((t) => t.id)
    tabs = next
    if (!next.length) {
      current = ''
      return
    }
    if (!current || !seen.has(current)) {
      /* 切到新增标签(AI 刚建的)或保底第一个,并立即同步视口——
      新标签默认视口 1280x800,不同步就停在"大窗口缩小看"的错比例 */
      current = (next.find((t) => !prevIds.includes(t.id)) ?? next[0]).id
      requestAnimationFrame(reportViewport)
    }
  }

  function switchTo(id: string) {
    current = id
    requestAnimationFrame(reportViewport)
  }

  /* toggleWindow 唤起/收起真浏览器窗口:乐观更新按钮,广播帧为准。
  收起时强制重报视口——大窗期间窗口尺寸被用户改过,视口跟随须恢复。 */
  function toggleWindow() {
    if (!current) return
    windowShown = !windowShown
    browserManager.window(windowShown, current)
    if (!windowShown) {
      lastViewport = ''
      requestAnimationFrame(reportViewport)
    }
  }

  /* viewport 跟随:容器尺寸防抖上报,数值未变不重发(重复 override 会
  强制页面 re-layout,是闪烁与卡顿源);后端按 CSS 视口+DSF 布局出物理帧 */
  let lastViewport = ''
  function reportViewport() {
    if (!active || !views || !current || views.clientWidth === 0) return
    if (viewportTimer) window.clearTimeout(viewportTimer)
    viewportTimer = window.setTimeout(() => {
      viewportTimer = undefined
      const el = views
      if (!el || !current) return
      const width = el.clientWidth
      const height = el.clientHeight
      if (width < 200 || height < 200) return
      const scale = window.devicePixelRatio || 1
      const key = `${current}:${width}x${height}@${scale}`
      if (key === lastViewport) return
      lastViewport = key
      browserManager.viewport(current, width, height, scale)
    }, 150)
  }

  /* 外部定位请求（supper_url browser:// 点击）：目标标签到达清单后切换 */
  $effect(() => {
    const want = store.browserFocus
    if (!want || !tabs.some((t) => t.id === want)) return
    store.browserFocus = ''
    switchTo(want)
  })

  async function createOne(url?: string) {
    try {
      const info = await browserManager.create(undefined, url)
      if (!tabs.find((t) => t.id === info.id)) tabs = [...tabs, info]
      switchTo(info.id)
    } catch (e) {
      console.error(e)
    }
  }

  function closeOne(e: Event, id: string) {
    e.stopPropagation()
    void browserManager.close(id)
  }

  function submitAddress() {
    if (!current || !addressValue.trim()) return
    browserManager.navigate(current, addressValue.trim())
    ;(document.activeElement as HTMLElement | null)?.blur()
  }

  function init() {
    if (inited) return
    inited = true
    browserManager.connect()
    offs.push(browserManager.onTabs(syncTabs))
    offs.push(
      browserManager.onFrame((id, img) => {
        frameMap[id] = img
      }),
    )
    offs.push(
      browserManager.onSnapshot((id, img) => {
        frameMap[id] = img
      }),
    )
    offs.push(browserManager.onStatus((st) => (status = st)))
    offs.push(browserManager.onWindow((shown) => (windowShown = shown)))
    offs.push(browserManager.onState((c) => (connected = c)))

    ro = new ResizeObserver(() => reportViewport())
    if (views) ro.observe(views)

    void fetch('/api/browser/list')
      .then((r) => (r.ok ? r.json() : []))
      .then((ts: BrowserTabInfo[]) => {
        if (inited) syncTabs(ts)
      })
      .catch(() => {})
  }

  onMount(() => {
    if (active) init()
  })

  $effect(() => {
    if (active && !inited) init()
    if (active) requestAnimationFrame(reportViewport)
  })

  onDestroy(() => {
    offs.forEach((off) => off())
    ro?.disconnect()
    if (viewportTimer) window.clearTimeout(viewportTimer)
    holders.clear()
  })
</script>

<svelte:window onresize={reportViewport} />

<div class="browser-tab">
  <div class="strip">
    {#each tabs as t (t.id)}
      <button class="strip-item" class:active={current === t.id} onclick={() => switchTo(t.id)} title="{t.name} · 来源 {t.origin || '?'} · {t.url}">
        <span class="dot" class:loading={t.loading}></span>
        <span class="name">{t.name}</span>
        <span class="x" onclick={(e) => closeOne(e, t.id)} role="button" tabindex="-1" title="关闭">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round">
            <path d="M6 6l12 12M18 6L6 18" />
          </svg>
        </span>
      </button>
    {/each}
    <button class="new" onclick={() => void createOne()} title="新建标签">+</button>
    {#if tabs.length}
      <button class="win" onclick={toggleWindow} title="唤起真实浏览器窗口,直接在其中操作">
        {windowShown ? '收起大窗' : '打开大窗'}
      </button>
    {/if}
    <span class="conn" class:off={!connected}>{connected ? '' : '重连中…'}</span>
  </div>

  <form class="address" onsubmit={(e) => { e.preventDefault(); submitAddress() }}>
    <input
      type="text"
      bind:value={addressValue}
      placeholder="{current ? '输入网址回车导航' : '新建标签后可导航'}"
      onfocus={() => (addressFocused = true)}
      onblur={() => (addressFocused = false)}
      spellcheck="false"
      autocomplete="off"
    />
    {#if tabs.find((t) => t.id === current)?.loading}
      <span class="spinner" title="加载中"></span>
    {/if}
  </form>

  <div class="views" bind:this={views}>
    {#each tabs as t (t.id)}
      <div class="holder" class:hidden={current !== t.id} use:bindHolder={t.id}>
        {#if frameMap[t.id]}
          <img class="frame" src={frameMap[t.id].dataUrl} alt="" draggable="false" decoding="async" />
        {:else}
          <div class="waiting">等待画面…</div>
        {/if}
      </div>
    {/each}
    {#if !tabs.length}
      <div class="empty">
        <p>还没有打开的页面</p>
        <button onclick={() => void createOne()}>新建一个标签</button>
      </div>
    {:else if status && (status.phase === 'downloading' || status.phase === 'launching')}
      <div class="overlay">
        <span class="spinner"></span>
        <p>{status.message || '正在准备浏览器…'}</p>
      </div>
    {:else if status && status.phase === 'error'}
      <div class="overlay">
        <p class="err">{status.message}</p>
        <button onclick={() => void createOne()}>重试</button>
      </div>
    {/if}
  </div>
</div>

<style>
  .browser-tab {
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
    font-size: 12px;
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
  .dot.loading {
    background: #f1c40f;
    animation: pulse 1s ease-in-out infinite;
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
    font-size: 14px;
    line-height: 1;
  }
  .new:hover {
    background: var(--bg-soft);
    color: var(--fg);
    border-style: solid;
  }
  .win {
    margin-left: auto;
    border: 1px solid var(--line-strong);
    background: var(--bg);
    border-radius: 9px;
    padding: 4px 12px;
    font-size: 12px;
    color: var(--muted);
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .win:hover {
    background: var(--bg-soft);
    color: var(--fg);
  }
  .conn {
    font-size: 11px;
    font-family: var(--font-mono);
    color: var(--faint);
  }
  .conn:empty {
    display: none;
  }
  .address {
    display: flex;
    align-items: center;
    gap: 8px;
    flex: none;
    border: 1px solid var(--line);
    border-radius: 10px;
    background: var(--bg);
    padding: 0 10px;
  }
  .address:focus-within {
    border-color: var(--accent-soft, var(--accent));
  }
  .address input {
    flex: 1;
    border: none;
    outline: none;
    background: transparent;
    font-size: 12px;
    font-family: var(--font-mono);
    color: var(--fg);
    padding: 7px 0;
  }
  .address input::placeholder {
    color: var(--faint);
  }
  .spinner {
    width: 12px;
    height: 12px;
    border-radius: 50%;
    border: 2px solid var(--line);
    border-top-color: var(--accent);
    animation: spin 0.8s linear infinite;
    flex: none;
  }
  .views {
    position: relative;
    flex: 1;
    min-height: 0;
    border: 1px solid var(--line);
    border-radius: 12px;
    background: #fff;
    overflow: hidden;
  }
  .holder {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: stretch;
    justify-content: stretch;
    outline: none;
  }
  .holder:focus-visible {
    box-shadow: inset 0 0 0 2px var(--accent-soft, var(--accent));
  }
  .holder.hidden {
    display: none;
  }
  .frame {
    width: 100%;
    height: 100%;
    object-fit: contain; /* 等比缩放不变形;视口跟随后帧比例≈容器比例,基本无留白 */
    user-select: none;
    -webkit-user-drag: none;
  }
  .waiting {
    margin: auto;
    font-size: 12px;
    color: var(--faint);
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
    font-size: 13px;
  }
  .empty button,
  .overlay button {
    border: 1px solid var(--line-strong);
    background: var(--bg-invert);
    color: var(--fg-invert);
    border-radius: 10px;
    padding: 8px 18px;
    font-size: 13px;
    font-weight: 600;
  }
  .overlay {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 12px;
    background: color-mix(in srgb, var(--bg) 72%, transparent);
    backdrop-filter: blur(2px);
    font-size: 13px;
    color: var(--muted);
    text-align: center;
    padding: 0 24px;
  }
  .overlay .err {
    color: #c0392b;
    max-width: 100%;
    word-break: break-all;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
  @keyframes pulse {
    50% {
      opacity: 0.35;
    }
  }
</style>
