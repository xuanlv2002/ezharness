<script lang="ts">
  import { onDestroy, onMount } from 'svelte'
  import { paneTabs } from '../../lib/paneTabs.svelte'

  /*
  共享浏览器抽屉页（desktop 专用）：标签条 + 地址栏 + 内容区占位。
  真实页面由 desktop 主进程的 WebContentsView 渲染并贴靠内容区；
  本页只是 UI 框架——标签清单经 IPC 订阅主进程，新建/切换/关闭/
  导航全部转发主进程。web 端直接访问时无 desktop 能力，显示降级提示。
  active = 抽屉开着且当前是浏览器页：false 时上报零矩形让主进程藏
  视图；true 时除 ResizeObserver 外再逐帧上报 ~350ms——抽屉宽度
  过渡期间内容区恒宽、RO 不触发，但内容区 x 在动，需逐帧重贴。
  */

  let { active = false, animating = false }: { active?: boolean; animating?: boolean } = $props()

  type TabInfo = { id: string; name: string; desc?: string; origin: string; url: string; title: string; loading: boolean; active: boolean; ai?: boolean; busy?: boolean }

  /* desktop 壳能力（preload 注入；web 端 undefined） */
  function ezBrowser(): any | undefined {
    return (window as any).ez?.browser
  }

  let tabs = $state<TabInfo[]>([])
  let current = $state('')
  let addressValue = $state('')
  let addressFocused = false
  let content: HTMLElement | undefined = $state()
  let ro: ResizeObserver | undefined

  /* 当前标签与 AI 忙碌锁：忙碌期间地址栏禁用（主进程 IPC 同样拒绝导航，
     这里只是把锁表达在输入框上） */
  const curTab = $derived(tabs.find((t) => t.id === current))
  const busy = $derived(!!curTab?.busy)

  /* 地址栏跟随当前标签 url（未在编辑时） */
  $effect(() => {
    const url = curTab?.url ?? ''
    if (!addressFocused) addressValue = url
  })

  function syncTabs(payload: string) {
    try {
      tabs = JSON.parse(payload)
    } catch {
      return
    }
    /* 当前激活标签以主进程为准（它决定显示哪个视图）：跟着 active 走，
       标签条高亮与地址栏才和页面一致。不做"本地猜"——新建/关闭后本地猜
       会出现页面切了而地址栏没换。用户点标签条是反向（pickTab 上报），
       下一帧的清单再确认回来。 */
    const a = tabs.find((t) => t.active)
    current = a ? a.id : ''
    /* 清单每次变化都重报一次 rect：视图归属只认上报，窗口刚被显示/重排过
       时重报一次即重新认领。隐藏的浏览器页量出的是零/极小矩形，会被主进程
       的 <10 守卫挡掉，不会跟真正上屏的那个抢。 */
    reportRect()
  }

  function submitAddress() {
    if (!current || busy || !addressValue.trim()) return
    ezBrowser()?.navigate(current, addressValue.trim())
    ;(document.activeElement as HTMLElement | null)?.blur()
  }

  /* 内容区 rect 上报：ResizeObserver + 窗口 resize（主进程 setBounds 依据）。
     抽屉宽度过渡期间不上报（保持视图隐藏，见下面的 effect）。 */
  function reportRect() {
    if (!content || animating) return
    const rect = content.getBoundingClientRect()
    if (rect.width < 10 || rect.height < 10) return
    ezBrowser()?.reportRect({ x: Math.round(rect.left), y: Math.round(rect.top), width: Math.round(rect.width), height: Math.round(rect.height) })
  }

  onMount(async () => {
    const api = ezBrowser()
    if (!api) return // web 端降级提示（模板分支）
    api.onTabs((_payload: string) => syncTabs(_payload))
    /* 视图被摘出窗口（弹窗关闭/拖拽取消）后回挂：重新上报一次 rect */
    api.onRefreshRect?.(() => requestAnimationFrame(reportRect))
    syncTabs(await api.list())
    ro = new ResizeObserver(reportRect)
    if (content) ro.observe(content)
    requestAnimationFrame(reportRect)
  })

  /* 标签数上报顶栏角标 */
  $effect(() => (paneTabs.browser = tabs.length))

  onDestroy(() => {
    ro?.disconnect()
    paneTabs.browser = 0
  })

  /* 抽屉显隐/过渡联动视图贴靠：过渡期间报零矩形**把视图藏起来**——真实内容是
     原生 WebContentsView，跟不上 CSS 等宽动画（逐帧 IPC setBounds 必然滞后一拍，
     看着就是抖）；藏起来期间面板是纯 DOM，等宽动画丝滑。过渡结束（animating 变
     false）再报真实矩形贴上去。切换工具时若抽屉宽度也变了同样适用。 */
  $effect(() => {
    const api = ezBrowser()
    if (!api) return
    if (!active || animating) {
      api.reportRect({ x: 0, y: 0, width: 0, height: 0 })
      return
    }
    reportRect()
  })

  /* 用户点标签条才算"切换意图"：只在这里告诉主进程。不做 $effect 跟随
     current——激活标签以主进程为准，跟随会把主进程的状态再回写一遍，
     两个窗口各自回写就变成互相打断的 select 风暴（日志里成对出现的
     `select <- win#1 / win#2` 就是它）。 */
  function pickTab(id: string) {
    if (id === current) return
    current = id
    ezBrowser()?.select?.(id)
  }
</script>

<!-- onfocus：窗口被点回来时重报一次 rect 重新认领视图（宿主会被别的窗口让位时
     抢走，光靠尺寸变化不会自发重报） -->
<svelte:window onresize={reportRect} onfocus={reportRect} />

<div class="browser-pane">
  {#if !ezBrowser()}
    <div class="fallback">
      <p>浏览器抽屉仅桌面端（ezharness desktop）可用</p>
      <p class="sub">AI 的浏览器工具在桌面端运行时自动打开此抽屉；web 端暂无内嵌浏览器</p>
    </div>
  {:else}
    <div class="strip">
      {#each tabs as t (t.id)}
        <button
          class="strip-item"
          class:active={current === t.id}
          class:busy={t.busy}
          onclick={() => pickTab(t.id)}
          title="{t.name}{t.desc ? ` · ${t.desc}` : ''} · 来源 {t.origin || '?'}{t.busy ? ' · AI 正在操作此页面' : ''} · {t.url}"
        >
          {#if t.busy}
            <span class="lock" title="AI 操作中">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
                <rect x="5" y="11" width="14" height="9" rx="2" />
                <path d="M8 11V8a4 4 0 1 1 8 0v3" />
              </svg>
            </span>
          {/if}
          <span class="dot" class:loading={t.loading}></span>
          <span class="name">{t.title || t.name}</span>
          {#if t.ai}<span class="ai">AI</span>{/if}
          <span class="x" onclick={(e) => { e.stopPropagation(); ezBrowser().close(t.id) }} role="button" tabindex="-1" title="关闭">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round">
              <path d="M6 6l12 12M18 6L6 18" />
            </svg>
          </span>
        </button>
      {/each}
      <button class="new" onclick={() => ezBrowser().create()} title="新建标签">+</button>
    </div>

    <form class="address" onsubmit={(e) => { e.preventDefault(); submitAddress() }}>
      <input
        type="text"
        bind:value={addressValue}
        disabled={busy}
        placeholder={busy ? 'AI 操作中，地址栏已锁定' : current ? '输入网址回车导航' : '新建标签后可导航'}
        onfocus={() => (addressFocused = true)}
        onblur={() => (addressFocused = false)}
        spellcheck="false"
        autocomplete="off"
      />
      {#if busy}
        <span class="busytag" title="AI 的浏览器工具正在此标签上执行，地址栏已锁定，避免同时操作互相干扰">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
            <rect x="5" y="11" width="14" height="9" rx="2" />
            <path d="M8 11V8a4 4 0 1 1 8 0v3" />
          </svg>
          AI 操作中
        </span>
      {:else if curTab?.loading}
        <span class="spinner" title="加载中"></span>
      {/if}
    </form>

    <!-- 内容区占位：WebContentsView 贴靠于此（主进程管理，页面不可见） -->
    <div class="content" bind:this={content}>
      {#if !tabs.length}
        <div class="empty">
          <p>还没有打开的页面</p>
          <button onclick={() => ezBrowser().create()}>新建一个标签</button>
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .browser-pane {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    gap: 8px;
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
      color var(--dur-fast) var(--ease-out);
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
  /* AI 桥调用正在该标签执行：锁形标识 + 强调色描边（呼吸提示），
     用户此刻别抢着操作，避免人机同时动一张页面 */
  .strip-item.busy {
    border-color: var(--accent);
    animation: busypulse 1.6s ease-in-out infinite;
  }
  .lock {
    display: grid;
    place-items: center;
    width: 13px;
    height: 13px;
    color: var(--accent);
    flex: none;
  }
  .lock svg {
    width: 11px;
    height: 11px;
  }
  /* AI 建的标签的来源徽标（busy 只是执行瞬间的锁，ai 是常驻标识） */
  .ai {
    flex: none;
    font-size: 9px;
    line-height: 1;
    font-weight: 600;
    padding: 2px 4px;
    border-radius: 4px;
    background: color-mix(in srgb, var(--accent) 14%, transparent);
    color: var(--accent);
  }
  .busytag {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    flex: none;
    font-size: var(--font-size-ui);
    color: var(--accent);
  }
  .busytag svg {
    width: 12px;
    height: 12px;
  }
  @keyframes busypulse {
    50% {
      box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 18%, transparent);
    }
  }
  .name {
    max-width: 160px;
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
    font-size: var(--font-size-body);
    line-height: 1;
  }
  .new:hover {
    background: var(--bg-soft);
    color: var(--fg);
    border-style: solid;
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
    font-size: var(--font-size-ui);
    font-family: var(--font-mono);
    color: var(--fg);
    padding: 7px 0;
  }
  .address input::placeholder {
    color: var(--faint);
  }
  .address input:disabled {
    color: var(--faint);
    cursor: not-allowed;
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
  .content {
    position: relative;
    flex: 1;
    min-height: 0;
    border: 1px solid var(--line);
    border-radius: 12px;
    background: #fff;
    overflow: hidden;
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
  .fallback {
    margin: auto;
    text-align: center;
    color: var(--muted);
    font-size: var(--font-size-body);
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .fallback .sub {
    font-size: var(--font-size-ui);
    color: var(--faint);
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
