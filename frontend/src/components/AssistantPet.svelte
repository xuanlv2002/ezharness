<script lang="ts">
  import { onMount } from 'svelte'
  import EzAvatar from './EzAvatar.svelte'
  import { launchApp as openApp } from '../lib/apps'

  /*
  桌面宠物（pet=1 窗口）：悬浮球 + 环形菜单。
  窗口固定 288×288（不动态扩缩——展开后大矩形成热区会自锁在展开态），
  默认整窗鼠标穿透（透明区点击归下层应用）。主进程 60ms 推光标窗口内
  坐标（ez:pet-move），渲染层用 elementFromPoint 判定光标是否压在实体
  小部件上：压上 → 关穿透 + 展开环形菜单；透明处 → 保持穿透漏点。

  环形菜单：以头像（窗口中心 144,144）为圆心、右上 30°（弹出箭头）起
  顺时针、每颗固定 36°（满 6 颗恰 180°）——箭头（开主客户端）+ 铃铛
  （通知）+ 钉选快应用（≤3，点击启动，实心图钉角标点击取消）+「…」球
  （快应用 ⚡️ logo，与侧边栏「快应用」入口同图标，点击展开全部应用
  栏）。子球位置由 JS 算成绝对
  left/top（不靠 transform 位移——定位与动画解耦，transform 只做 scale
  弹出）。全部应用栏贴左缘竖列、滚轮纵向滚，条目本体点击启动、右上
  图钉角标（实心橙=已钉/空心灰=未钉）点击切换，钉满 3 个由 core 顶掉
  最早。执行中头像转圈。3s 轮询 core 只读端点。
  */

  const ez = (window as any).ez

  const C = 144 // 窗口中心（stage 288×288）
  const RING_R = 74
  const RING_START = -30 // 起点：弹出箭头，右上 30°
  const RING_STEP = 36 // 相邻球固定角距：满 6 颗（箭头+铃铛+3 钉选+…）恰好 180°
  const SIZE = { main: 36, bell: 36, app: 38, more: 34 } as const

  type PetApp = { name: string; title: string; icon?: string; pinned?: boolean }

  let dragging = false // 拖拽中：跳过 hover 判定（穿透已关，别被轮询扰动）
  let running = $state(false)
  let pending = $state(0)
  let done = $state(false)
  let apps = $state<PetApp[]>([])
  let hovered = $state(false)
  let trayOpen = $state(false)
  /* 图标加载失败的应用：回退成名字（否则 onerror 隐藏 img 只剩一个空球） */
  let iconFailed = $state<Record<string, boolean>>({})

  onMount(() => {
    ez?.pet?.passthrough?.(true)
    /* 光标在窗口内移动（主进程 60ms 推本地坐标）：压在实体小部件上就关
       穿透并展开，透明处一律穿透。展开态用「离中心 <120」做迟滞，好在球
       与球/应用栏之间的缝隙里也不塌；穿透只看 elementFromPoint，与迟滞
       无关，所以压在透明处（如桌面图标）永远漏点。 */
    ez?.pet?.onMove?.((x: number, y: number) => {
      if (dragging) return
      const dist = Math.hypot(x - C, y - C)
      const inWin = x >= 0 && y >= 0 && x <= C * 2 && y <= C * 2
      const el = document.elementFromPoint(x, y)
      const interactive = !!el?.closest('.avatar-wrap, .orb.on, .orbwrap.on, .traylist.open')
      hovered = interactive || (hovered && inWin && dist < 120)
      if (!hovered) trayOpen = false
      ez?.pet?.passthrough?.(!interactive)
    })
    const poll = async () => {
      try {
        const [topics, notices, list] = await Promise.all([
          fetch('/api/topics').then((r) => (r.ok ? r.json() : [])),
          fetch('/api/notifications').then((r) => (r.ok ? r.json() : [])),
          fetch('/api/apps').then((r) => (r.ok ? r.json() : { apps: [] })),
        ])
        running = (topics as { running?: boolean }[]).some((t) => t.running)
        const items = (notices as { items?: { kind?: string }[] }[]).flatMap((g) => g.items ?? [])
        pending = items.filter((it) => it.kind === 'approve' || it.kind === 'ask' || it.kind === 'mcp_auth' || it.kind === 'memory' || it.kind === 'callback').length
        done = items.some((it) => it.kind === 'info')
        apps = ((list as { apps?: PetApp[] }).apps ?? [])
      } catch {
        /* core 不可达：静默，下轮再试 */
      }
    }
    void poll()
    const t = setInterval(poll, 3000)
    return () => clearInterval(t)
  })

  const pinned = $derived(apps.filter((a) => a.pinned).slice(0, 3))

  /*
  环形摆位：从弹出箭头（右上 30°）起，顺时针每个固定 +RING_STEP°。
  相邻球角距恒定（不随数量摊薄），球多弧线就自然变长。
  位置直接算成 left/top（球心 - 尺寸一半），CSS 里没有定位变量。
  */
  const ring = $derived.by(() => {
    const items: { kind: 'main' | 'bell' | 'app' | 'more'; a?: PetApp }[] = [
      { kind: 'main' },
      { kind: 'bell' },
      ...pinned.map((a) => ({ kind: 'app', a }) as { kind: 'app'; a: PetApp }),
      { kind: 'more' },
    ]
    return items.map((it, i) => {
      const ang = ((RING_START + RING_STEP * i) * Math.PI) / 180
      const size = SIZE[it.kind]
      return {
        ...it,
        d: i,
        left: C + Math.cos(ang) * RING_R - size / 2,
        top: C + Math.sin(ang) * RING_R - size / 2,
      }
    })
  })

  /* 启动快应用：core 校验入口，desktop 壳开子窗 */
  async function launchApp(a: PetApp) {
    try {
      await openApp(a.name)
    } catch {
      /* core 不可达：忽略，用户可从快应用页启动 */
    }
  }

  /* 切换钉选（满 3 由 core 顶掉最早）；成功后立即刷新清单 */
  async function togglePin(name: string) {
    try {
      await fetch(`/api/apps/${encodeURIComponent(name)}/pin`, { method: 'POST' })
      const list = await fetch('/api/apps').then((r) => (r.ok ? r.json() : { apps: [] }))
      apps = ((list as { apps?: PetApp[] }).apps ?? [])
    } catch {
      /* 静默，下轮轮询自愈 */
    }
  }
</script>

<div class="pet">
  <div class="stage">
    <div class="avatar-wrap" class:dim={!hovered}>
      <EzAvatar size={72} run={running} />
      <button class="drag-handle"
        onpointerdown={(e) => {
          if (e.button !== 0) return
          /* 捕获指针：拖动中窗口随光标移动、指针可能越出窗口，若不捕获
             pointerup 收不到，拖拽停不下来 */
          ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
          dragging = true
          ez?.pet?.drag?.(true)
        }}
        onpointermove={(e) => {
          // 指针事件驱动移窗（屏幕坐标，主进程按按下点偏移换算）——比轮询平滑
          if (dragging) ez?.pet?.dragMove?.(e.screenX, e.screenY)
        }}
        onpointerup={() => {
          dragging = false
          ez?.pet?.drag?.(false)
        }}
        onpointercancel={() => {
          dragging = false
          ez?.pet?.drag?.(false)
        }}
        aria-label="拖动小助手"></button>
      {#if pending > 0}
        <i class="ndot avatar-dot red"></i>
      {:else if done}
        <i class="ndot avatar-dot green"></i>
      {/if}
    </div>

    {#each ring as s (s.kind === 'app' && s.a ? 'a' + s.a.name : s.kind)}
      {#if s.kind === 'main'}
        <button class="orb main" class:on={hovered} style={`left:${s.left}px;top:${s.top}px;transition-delay:${hovered ? 0 : 0}ms`}
          onclick={() => ez?.pet?.openMain()} title="打开主客户端">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M7 17L17 7" />
            <path d="M8 7h9v9" />
          </svg>
        </button>
      {:else if s.kind === 'bell'}
        <button class="orb bell" class:on={hovered} style={`left:${s.left}px;top:${s.top}px;transition-delay:${hovered ? 40 + s.d * 50 : 0}ms`}
          onclick={() => ez?.notice?.open?.()} title={pending > 0 ? `通知 · ${pending} 条待处理` : '通知'}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
            <path d="M18 8a6 6 0 0 0-12 0c0 7-3 9-3 9h18s-3-2-3-9" />
            <path d="M13.7 21a2 2 0 0 1-3.4 0" />
          </svg>
          {#if pending > 0}
            <i class="ndot red"></i>
          {:else if done}
            <i class="ndot green"></i>
          {/if}
        </button>
      {:else if s.kind === 'more'}
        <button class="orb more" class:on={hovered} style={`left:${s.left}px;top:${s.top}px;transition-delay:${hovered ? 40 + s.d * 50 : 0}ms`}
          onclick={() => (trayOpen = !trayOpen)} title={trayOpen ? '收起应用栏' : '全部快应用'}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
            <path d="M13 2L4.5 13h6L11 22l8.5-11h-6L13 2z" />
          </svg>
        </button>
      {:else if s.a}
        <span class="orbwrap" class:on={hovered} style={`left:${s.left - 5}px;top:${s.top - 5}px;width:48px;height:48px;transition-delay:${hovered ? 40 + s.d * 50 : 0}ms`}>
          <button class="orb app" onclick={() => void launchApp(s.a!)} title={`${s.a.title}（点击启动）`}>
            {#if s.a.icon && !iconFailed[s.a.name]}
              <img src={s.a.icon} alt="" onerror={() => (iconFailed[s.a!.name] = true)} />
            {:else}
              <span class="name">{s.a.title.slice(0, 3)}</span>
            {/if}
          </button>
          <button class="pinmark on" onclick={() => void togglePin(s.a!.name)} title="取消钉选">
            <svg viewBox="0 0 24 24" fill="currentColor" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">
              <path d="M12 17v5" />
              <path d="M9 3h6l-1 6 3 3v2H7v-2l3-3-1-6z" />
            </svg>
          </button>
        </span>
      {/if}
    {/each}

    <!-- 底部全部应用栏：… 点击展开，一行最多 3 个、滚轮横滚 -->
    <div class="traylist" class:open={hovered && trayOpen}>
      <div class="scroll">
        {#if apps.length === 0}
          <!-- 空态：竖排提示撑起托盘，别让它瘪成一条白片 -->
          <p class="empty">暂无快应用</p>
        {:else}
          {#each apps as a, i (a.name)}
            <span class="item" style={`transition-delay:${i * 24}ms`}>
              <button class="app" onclick={() => void launchApp(a)} title={`${a.title}（点击启动）`}>
                {#if a.icon && !iconFailed[a.name]}
                  <img src={a.icon} alt="" onerror={() => (iconFailed[a.name] = true)} />
                {:else}
                  <span class="name">{a.title.slice(0, 3)}</span>
                {/if}
              </button>
              <button class="pinmark" class:on={a.pinned} onclick={() => void togglePin(a.name)}
                title={a.pinned ? '取消钉选' : '钉到环形菜单（满 3 个顶掉最早）'}>
                <svg viewBox="0 0 24 24" fill={a.pinned ? 'currentColor' : 'none'} stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M12 17v5" />
                  <path d="M9 3h6l-1 6 3 3v2H7v-2l3-3-1-6z" />
                </svg>
              </button>
            </span>
          {/each}
        {/if}
      </div>
    </div>
  </div>
</div>

<style>
  /* 透明窗口：html/body 背景必须重置为透明（app.css 给 body 涂了实色） */
  :global(html),
  :global(body) {
    background: transparent !important;
    overflow: hidden;
  }

  /* 只有头像区是拖拽热区：整窗设为 drag 会把环形球/底部栏一起吞掉
     （drag 区域的 no-drag 空洞按视觉盒算，球从 scale(0.2) 弹出时空洞只剩
     中心一点；底部栏没有任何 no-drag 直接全废）——只留头像当把手。 */
  .pet {
    width: 100vw;
    height: 100vh;
    user-select: none;
  }
  .stage {
    position: relative;
    width: 100%;
    height: 100%;
  }
  .avatar-wrap {
    position: absolute;
    left: 108px; /* 144 - 72/2 */
    top: 108px;
    width: 72px;
    height: 72px;
    opacity: 0.92;
    transition: opacity 220ms var(--ease-out);
    /* 不用 app-region:drag——Windows 会把 drag 区当标题栏、右键弹系统菜单，
       拦不住也换不掉；改由 JS 上报拖拽（pointerdown→主进程随光标移窗），
       右键走主进程 context-menu 弹「关闭悬浮球」 */
  }
  .avatar-wrap.dim {
    opacity: 0.86;
  }
  .avatar-wrap :global(img) {
    pointer-events: none;
  }
  /* 拖拽把手：盖住头像的透明层，只承左键拖动；右键放行给主进程弹「关闭悬浮球」 */
  .drag-handle {
    position: absolute;
    inset: 0;
    border: none;
    background: transparent;
    padding: 0;
    cursor: grab;
  }
  .drag-handle:active {
    cursor: grabbing;
  }
  /* 头像右上角常显通知点（不悬浮也可见；与铃铛球同款配色） */
  .avatar-dot {
    top: -2px;
    right: -2px;
    width: 12px;
    height: 12px;
    border: 2px solid var(--bg);
  }

  /* 铃铛球：与客户端收起态铃铛同款图标，待处理红点 / 完成绿点浮在球上 */
  .orb.bell {
    width: 36px;
    height: 36px;
    border-radius: 50%;
    background: var(--bg);
    color: var(--muted);
    box-shadow: 0 4px 14px rgb(0 0 0 / 18%);
  }
  .orb.bell:hover {
    color: var(--fg);
    box-shadow: 0 6px 18px rgb(0 0 0 / 26%);
  }
  .orb.bell svg {
    width: 15px;
    height: 15px;
  }
  .ndot {
    position: absolute;
    top: 3px;
    right: 3px;
    width: 9px;
    height: 9px;
    border-radius: 50%;
    box-shadow: 0 0 0 2px var(--bg);
  }
  .ndot.red {
    background: #e74c3c;
  }
  .ndot.green {
    background: #2ecc71;
  }

  /*
  环形子球：left/top 由 JS 算死（定位与动画解耦），transform 只做
  scale 弹出（0 → 1 回弹）；收起态 pointer-events 关掉不挡拖拽。
  本体球（箭头/铃铛/…）自己带 .on；app 球没有 .on——它的进出动画由
  .orbwrap 承担，所以缩放/穿透只作用在「非 app 球」+ app wrapper 上，
  app 球靠继承 wrapper 的 pointer-events 跟随开关，且自身不做动画。
  */
  .orb,
  .orbwrap {
    position: absolute;
    display: grid;
    place-items: center;
    padding: 0;
    border: none;
    background: transparent;
    cursor: pointer;
  }
  .orb:not(.app),
  .orbwrap {
    opacity: 0;
    transform: scale(0.2);
    transition:
      transform 300ms cubic-bezier(0.34, 1.56, 0.64, 1),
      opacity 180ms var(--ease-out);
  }
  .orb:not(.app):not(.on),
  .orbwrap:not(.on) {
    pointer-events: none;
  }
  .orb:not(.app).on,
  .orbwrap.on {
    opacity: 1;
    transform: scale(1);
  }
  .orb.main {
    width: 36px;
    height: 36px;
    border-radius: 50%;
    background: #1f2430;
    color: #fff;
    box-shadow: 0 4px 14px rgb(0 0 0 / 22%);
  }
  .orb.main:hover {
    opacity: 0.88;
  }
  .orb.main svg {
    width: 14px;
    height: 14px;
  }
  .orb.more {
    width: 34px;
    height: 34px;
    border-radius: 50%;
    background: var(--bg);
    color: var(--muted);
    box-shadow: 0 3px 10px rgb(0 0 0 / 14%);
  }
  .orb.more:hover {
    color: var(--fg);
    box-shadow: 0 5px 14px rgb(0 0 0 / 22%);
  }
  .orb.more svg {
    width: 14px;
    height: 14px;
  }
  /* 钉选球：wrapper 48×48（球 38 + 角标出头），球在 wrapper 内居中。
     球自身不做进出动画（.orb:not(.app) 那组不命中它），动画全由 wrapper 承担 */
  .orbwrap .orb.app {
    position: static;
    width: 38px;
    height: 38px;
    border-radius: 50%;
    background: var(--bg);
    box-shadow: 0 4px 14px rgb(0 0 0 / 18%);
  }
  .orb.app:hover {
    box-shadow: 0 6px 18px rgb(0 0 0 / 26%);
  }
  .orb.app img {
    width: 24px;
    height: 24px;
    object-fit: cover;
    border-radius: 8px;
  }
  .name {
    font-size: 9px;
    font-weight: 700;
    line-height: 1.1;
    text-align: center;
    color: var(--fg);
    overflow: hidden;
  }
  .orb .name {
    max-width: 30px;
  }

  /* 图钉角标：右上角小圆钮，实心橙=已钉 / 空心灰=未钉 */
  .pinmark {
    position: absolute;
    top: 0;
    right: 0;
    display: grid;
    place-items: center;
    width: 18px;
    height: 18px;
    padding: 0;
    border: none;
    border-radius: 50%;
    background: var(--bg);
    color: var(--faint);
    cursor: pointer;
    box-shadow: 0 1px 5px rgb(0 0 0 / 22%);
  }
  .pinmark:hover {
    color: var(--fg);
  }
  .pinmark.on {
    color: #f0883e;
  }
  .pinmark svg {
    width: 10px;
    height: 10px;
  }

  /* 全部应用栏：… 点击展开（自左侧滑入），竖向一列，滚轮纵向滚动。
     尽量贴近头像又不压环形球（环最左的球是 150° 的「…」，左缘 ~x63，
     本列右缘 ~x64 只擦到 1px 圆弧投影）。
     无阴影：托盘阴影会被窗界裁出左右不对称的灰边；实色底 + 细边定
     轮廓，任何背景上都干净。 */
  .traylist {
    position: absolute;
    left: 12px;
    top: 50%;
    width: 52px;
    padding: 5px;
    border: 1px solid var(--line);
    border-radius: 14px;
    background: var(--bg);
    opacity: 0;
    transform: translate(-14px, -50%) scale(0.9);
    pointer-events: none;
    transition:
      transform 240ms cubic-bezier(0.34, 1.4, 0.64, 1),
      opacity 160ms var(--ease-out);
  }
  .traylist.open {
    opacity: 1;
    transform: translate(0, -50%);
    pointer-events: auto;
  }
  .scroll {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
    max-height: calc(100vh - 24px);
    overflow-y: auto;
    overflow-x: hidden;
    scrollbar-width: none;
    scroll-snap-type: y proximity;
  }
  .scroll::-webkit-scrollbar {
    display: none;
  }
  .item {
    position: relative;
    display: grid;
    place-items: center;
    flex: none;
    opacity: 0;
    transform: scale(0.6);
    transition:
      transform 240ms cubic-bezier(0.34, 1.56, 0.64, 1),
      opacity 150ms var(--ease-out);
    scroll-snap-align: start;
  }
  .traylist.open .item {
    opacity: 1;
    transform: none;
  }
  /* 空态提示：竖排汉字撑起托盘（52px 宽的竖列里横排只会折行） */
  .empty {
    margin: 8px 0;
    writing-mode: vertical-rl;
    font-size: var(--font-size-small);
    letter-spacing: 4px;
    color: var(--faint);
    white-space: nowrap;
  }
  .traylist .app {
    display: grid;
    place-items: center;
    width: 42px;
    height: 42px;
    padding: 0;
    border: none;
    border-radius: 50%;
    background: var(--bg);
    box-shadow: 0 3px 10px rgb(0 0 0 / 14%);
    cursor: pointer;
  }
  .traylist .app:hover {
    box-shadow: 0 5px 14px rgb(0 0 0 / 22%);
  }
  .traylist .app img {
    width: 26px;
    height: 26px;
    object-fit: cover;
    border-radius: 8px;
  }
  .traylist .app .name {
    max-width: 34px;
  }
</style>
