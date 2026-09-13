<script lang="ts">
  import { onDestroy, onMount } from 'svelte'

  /*
  共享浏览器抽屉页（desktop 专用）：标签条 + 地址栏 + 内容区占位。
  真实页面由 desktop 主进程的 WebContentsView 渲染并贴靠内容区；
  本页只是 UI 框架——标签清单经 IPC 订阅主进程，新建/切换/关闭/
  导航全部转发主进程。web 端直接访问时无 desktop 能力，显示降级提示。
  active = 抽屉开着且当前是浏览器页：false 时上报零矩形让主进程藏
  视图；true 时除 ResizeObserver 外再逐帧上报 ~350ms——抽屉宽度
  过渡期间内容区恒宽、RO 不触发，但内容区 x 在动，需逐帧重贴。
  */

  let { active = false }: { active?: boolean } = $props()

  type TabInfo = { id: string; name: string; origin: string; url: string; title: string; loading: boolean }

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

  /* 地址栏跟随当前标签 url（未在编辑时） */
  $effect(() => {
    const url = tabs.find((t) => t.id === current)?.url ?? ''
    if (!addressFocused) addressValue = url
  })

  function syncTabs(payload: string) {
    try {
      tabs = JSON.parse(payload)
    } catch {
      return
    }
    if (!tabs.length) {
      current = ''
      return
    }
    if (!current || !tabs.some((t) => t.id === current)) {
      current = tabs[tabs.length - 1].id // 新建标签自动切到（AI/用户刚开的）
    }
  }

  function submitAddress() {
    if (!current || !addressValue.trim()) return
    ezBrowser()?.navigate(current, addressValue.trim())
    ;(document.activeElement as HTMLElement | null)?.blur()
  }

  /* 内容区 rect 上报：ResizeObserver + 窗口 resize（主进程 setBounds 依据） */
  function reportRect() {
    if (!content) return
    const rect = content.getBoundingClientRect()
    if (rect.width < 10 || rect.height < 10) return
    ezBrowser()?.reportRect({ x: Math.round(rect.left), y: Math.round(rect.top), width: Math.round(rect.width), height: Math.round(rect.height) })
  }

  onMount(async () => {
    const api = ezBrowser()
    if (!api) return // web 端降级提示（模板分支）
    api.onTabs((_payload: string) => syncTabs(_payload))
    syncTabs(await api.list())
    ro = new ResizeObserver(reportRect)
    if (content) ro.observe(content)
    requestAnimationFrame(reportRect)
  })

  onDestroy(() => ro?.disconnect())

  /* 抽屉显隐联动视图贴靠/收起（零矩形绕过 reportRect 的 <10 守卫） */
  $effect(() => {
    const api = ezBrowser()
    if (!api) return
    if (!active) {
      api.reportRect({ x: 0, y: 0, width: 0, height: 0 })
      return
    }
    let frames = 21 // ~350ms（60fps）：覆盖抽屉宽度过渡全程
    const tick = () => {
      reportRect()
      if (frames-- > 0) requestAnimationFrame(tick)
    }
    tick()
  })

  $effect(() => {
    /* 标签切换时内容区不变，但主进程需要重贴激活视图 */
    if (current) ezBrowser()?.select?.(current)
  })
</script>

<svelte:window onresize={reportRect} />

<div class="browser-pane">
  {#if !ezBrowser()}
    <div class="fallback">
      <p>浏览器抽屉仅桌面端（ezharness desktop）可用</p>
      <p class="sub">AI 的 browser_* 工具在桌面端运行时自动打开此抽屉；web 端暂无内嵌浏览器</p>
    </div>
  {:else}
    <div class="strip">
      {#each tabs as t (t.id)}
        <button class="strip-item" class:active={current === t.id} onclick={() => (current = t.id)} title="{t.name} · 来源 {t.origin || '?'} · {t.url}">
          <span class="dot" class:loading={t.loading}></span>
          <span class="name">{t.title || t.name}</span>
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
    font-size: 12px;
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
    font-size: 14px;
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
    font-size: 13px;
  }
  .empty button {
    border: 1px solid var(--line-strong);
    background: var(--bg-invert);
    color: var(--fg-invert);
    border-radius: 10px;
    padding: 8px 18px;
    font-size: 13px;
    font-weight: 600;
  }
  .fallback {
    margin: auto;
    text-align: center;
    color: var(--muted);
    font-size: 14px;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .fallback .sub {
    font-size: 12px;
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
