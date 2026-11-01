<script lang="ts">
  import { onMount } from 'svelte'
  import pkg from '../../package.json'
  import { api, type AppConfig } from '../lib/api'
  import { getToken } from '../lib/auth'
  import { store } from '../lib/store.svelte'

  const ez = (window as any).ez

  /* 设置页 = 应用结构配置（服务端）+ 上下文管理 + 数据/配置文件路径。 */
  let cfg = $state<AppConfig | null>(null)
  let port = $state<number | ''>('')
  let listen = $state('')
  let dataDir = $state('')
  let restarting = $state(false)
  let restartErr = $state('')
  let appChanged = $state(false)
  let showPaths = $state(false)

  /* 上下文整理水位（模型窗口百分比；0 = 关闭自动整理，模型仍可主动调 trim_context 工具） */
  let percent = $state<number | ''>('')
  let origPercent = $state<number | null>(null)
  let origExtra = $state('')
  let savingThreshold = $state(false)
  let thresholdMsg = $state('')

  /* 工作目录（terminal 默认执行目录；空 = 数据目录下 workspace/） */
  let workDir = $state('')
  let origWorkDir = $state('')
  let savingWorkDir = $state(false)
  let workDirMsg = $state('')

  /* 托盘常驻（桌面端点关闭 = 最小化到托盘；关窗时实时读取，即改即生效） */
  let closeToTray = $state(false)
  let savingTray = $state(false)

  /* 桌面悬浮球（desktop 壳启动时读取；保存成功后 IPC 通知主进程切显隐，web 端自动跳过） */
  let petEnabled = $state(true)
  let savingPet = $state(false)

  /* 调试模式（模型完整输入进日志；后端实时开关，即改即生效） */
  let debugMode = $state(false)
  let savingDebug = $state(false)

  /* 版本检查：结论在 store（启动静默查一次，这里手动查刷新它）——侧栏设置钮
     红点与下方版本区同源；本地仅留手动检查的转圈与失败原因 */
  let checking = $state(false)
  let updErr = $state('')
  const updNewer = $derived(store.updateAvailable)
  const updLatest = $derived(store.updateChecked && !store.updateAvailable)

  async function checkUpdate() {
    checking = true
    updErr = ''
    const r = await store.refreshUpdate()
    if (r.err) updErr = r.err
    checking = false
  }

  /* 会话令牌（前端↔core；展示 + 复制）与 OpenAPI 令牌（脚本/快应用用；展示 + 复制 + 重置） */
  let apiToken = $state('')
  let tokenCopied = $state(false)
  let oaToken = $state('')
  let oaCopied = $state(false)
  let oaResetting = $state(false)

  /* 单轮最大迭代次数（模型工具循环上限；0 = 默认 64，上限 128，随 Reassemble 生效） */
  let iters = $state<number | ''>('')
  let origIters = $state<number | null>(null)
  let savingIters = $state(false)
  let itersMsg = $state('')

  /* 工具结果卸载阈值（字节；超过即卸载到文件只留摘要头，0 = 默认 4096，摘要头随阈值分档封顶 1KB，随 Reassemble 生效） */
  let offload = $state<number | ''>('')
  let origOffload = $state<number | null>(null)
  let savingOffload = $state(false)
  let offloadMsg = $state('')

  /* 顶栏搜索跳转：滚动到对应分区（消费后清空锚点） */
  $effect(() => {
    const anchor = store.jumpAnchor
    if (!anchor.startsWith('settings:')) return
    store.jumpAnchor = ''
    const target = anchor.slice('settings:'.length)
    if (target === 'paths') showPaths = true
    const el = document.getElementById(`set-${target}`)
    requestAnimationFrame(() => el?.scrollIntoView({ block: 'start', behavior: 'smooth' }))
  })

  /* 输入框直接显示实际生效路径：空配置 = 数据目录下 workspace/，相对路径按
     数据目录解析，与后端 ResolveWorkDir 一致 */
  function resolveWorkDir(spec: string): string {
    const raw = spec.trim()
    if (/^(?:[a-zA-Z]:[\\/]|[\\/])/.test(raw)) return raw
    const base = dataDir.replace(/[\\/]+$/, '')
    if (!base) return raw
    const sep = base.includes('\\') ? '\\' : '/'
    return raw ? `${base}${sep}${raw.replace(/^[.][\\/]/, '')}` : `${base}${sep}workspace`
  }

  onMount(async () => {
    try {
      cfg = await api.appConfig()
      port = cfg.port
      listen = cfg.listen
      dataDir = cfg.dataDir
      appChanged = false
    } catch {
      restartErr = '配置加载失败（后端不可达）'
    }
    try {
      const st = await api.getSettings()
      percent = st.trimPercent ?? 75
      origPercent = st.trimPercent ?? 75
      const effectiveWorkDir = resolveWorkDir(st.workDir ?? '')
      workDir = effectiveWorkDir
      origWorkDir = effectiveWorkDir
      origExtra = st.systemExtra ?? ''
      closeToTray = st.closeToTray ?? false
      petEnabled = st.petEnabled ?? true
      debugMode = st.debugMode ?? false
      /* 0 = 用默认值：直接显示生效值，别让输入框停在 0 */
      const rawIters = st.maxIterations ?? 0
      iters = rawIters > 0 ? rawIters : 64
      origIters = iters
      const rawOffload = st.offloadThreshold ?? 0
      offload = rawOffload > 0 ? rawOffload : 4096
      origOffload = offload
    } catch {
      /* 上下文配置加载失败不阻塞页面 */
    }
    api.appToken().then((r) => (apiToken = r.token)).catch(() => {})
    api.openapiToken().then((r) => (oaToken = r.token)).catch(() => {})
  })

  /* 复制令牌：clipboard 失败（非安全上下文等）时选中文本让用户 Ctrl+C。 */
  async function copyToken(input: HTMLInputElement) {
    if (!apiToken) return
    try {
      await navigator.clipboard.writeText(apiToken)
      tokenCopied = true
      setTimeout(() => (tokenCopied = false), 1500)
    } catch {
      input.select()
    }
  }

  async function copyOaToken(input: HTMLInputElement) {
    if (!oaToken) return
    try {
      await navigator.clipboard.writeText(oaToken)
      oaCopied = true
      setTimeout(() => (oaCopied = false), 1500)
    } catch {
      input.select()
    }
  }

  /* 重置 OpenAPI 令牌：旧令牌即时作废，硬编码它的快应用/脚本需重新生成 */
  async function resetOaToken() {
    oaResetting = true
    try {
      const r = await api.resetOpenapiToken()
      oaToken = r.token
    } catch {
      /* 失败保持现状，下次进页面重查 */
    } finally {
      oaResetting = false
    }
  }

  async function saveTray(v: boolean) {
    savingTray = true
    try {
      await api.saveSettings({ systemExtra: origExtra, closeToTray: v })
      closeToTray = v
    } catch {
      /* 保存失败回滚开关（下次加载以服务端为准） */
    } finally {
      savingTray = false
    }
  }

  async function savePet(v: boolean) {
    savingPet = true
    try {
      await api.saveSettings({ systemExtra: origExtra, petEnabled: v })
      petEnabled = v
      /* 保存成功才切窗口显隐（持久化走上面的直连 core，IPC 只管显示） */
      ez?.pet?.setEnabled?.(v)
    } catch {
      /* 保存失败回滚开关（下次加载以服务端为准） */
    } finally {
      savingPet = false
    }
  }

  async function saveDebug(v: boolean) {
    savingDebug = true
    try {
      await api.saveSettings({ systemExtra: origExtra, debugMode: v })
      debugMode = v
    } catch {
      /* 保存失败回滚开关（下次加载以服务端为准） */
    } finally {
      savingDebug = false
    }
  }

  function checkChanged() {
    appChanged =
      !!cfg &&
      (String(port) !== String(cfg.port) ||
        listen.trim() !== cfg.listen ||
        dataDir.trim() !== cfg.dataDir)
  }

  async function saveThreshold() {
    savingThreshold = true
    thresholdMsg = ''
    try {
      // systemExtra 回传原值：保存接口是整体语义，缺省会清空
      await api.saveSettings({
        systemExtra: origExtra,
        trimPercent: Math.min(Math.max(Number(percent) || 0, 0), 100),
      })
      origPercent = Math.min(Math.max(Number(percent) || 0, 0), 100)
      thresholdMsg = '已保存（下一轮对话生效）'
    } catch (e) {
      thresholdMsg = `保存失败：${e instanceof Error ? e.message : String(e)}`
    } finally {
      savingThreshold = false
    }
  }

  async function saveIters() {
    savingIters = true
    itersMsg = ''
    try {
      const v = Math.min(Math.max(Number(iters) || 0, 0), 128)
      await api.saveSettings({ systemExtra: origExtra, maxIterations: v })
      iters = v
      origIters = v
      itersMsg = '已保存（下个对话轮生效）'
    } catch (e) {
      itersMsg = `保存失败：${e instanceof Error ? e.message : String(e)}`
    } finally {
      savingIters = false
    }
  }

  async function saveOffload() {
    savingOffload = true
    offloadMsg = ''
    try {
      const v = Math.min(Math.max(Number(offload) || 0, 0), 1048576)
      await api.saveSettings({ systemExtra: origExtra, offloadThreshold: v })
      offload = v
      origOffload = v
      offloadMsg = '已保存（下一轮对话生效）'
    } catch (e) {
      offloadMsg = `保存失败：${e instanceof Error ? e.message : String(e)}`
    } finally {
      savingOffload = false
    }
  }

  async function saveWorkDir() {
    savingWorkDir = true
    workDirMsg = ''
    try {
      await api.saveSettings({ systemExtra: origExtra, workDir: workDir.trim() })
      origWorkDir = workDir.trim()
      workDirMsg = '已保存（下一轮对话生效）'
    } catch (e) {
      workDirMsg = `保存失败：${e instanceof Error ? e.message : String(e)}`
    } finally {
      savingWorkDir = false
    }
  }

  async function restartApp() {
    restarting = true
    restartErr = ''
    try {
      const req: { port?: number; listen?: string; dataDir?: string } = {}
      if (String(port) !== String(cfg?.port)) req.port = Number(port)
      if (listen.trim() !== cfg?.listen) req.listen = listen.trim()
      if (dataDir.trim() !== cfg?.dataDir) req.dataDir = dataDir.trim()
      const res = await api.appRestart(req)
      for (let i = 0; i < 60; i++) {
        await new Promise((r) => setTimeout(r, 500))
        try {
          const h = await api.appHealth(res.url)
          if (h.boot === res.boot) {
            if (res.url === location.origin) location.reload()
            else {
              /* 换端口跳转：cookie 按 host 生效（不看端口），同 host 无需再带令牌；
                 跨 host（如 Vite dev 的 localhost 页面跳到 127.0.0.1）才带一次引导令牌 */
              const q = new URLSearchParams()
              if (new URLSearchParams(location.search).has('desktop')) q.set('desktop', '1')
              if (new URL(res.url).hostname !== location.hostname) {
                const t = getToken()
                if (t) q.set('token', t)
              }
              const qs = q.toString()
              location.assign(`${res.url}/${qs ? `?${qs}` : ''}`)
            }
            return
          }
        } catch {
          /* 新代未就绪，继续轮询 */
        }
      }
      restartErr = '等待新服务超时，请手动刷新页面'
    } catch (e) {
      restartErr = e instanceof Error ? e.message : String(e)
    } finally {
      restarting = false
    }
  }
</script>

<div class="page">
  <h1>设置</h1>

  <section id="set-server">
    <h2 title="监听、端口与数据目录是启动期配置，修改后需点「应用并重启」换代（进程内完成，数据目录变更自动迁移）；其余设置保存即生效，无需重启。监听 127.0.0.1 = 仅本机访问（默认，无防火墙弹窗），0.0.0.0 = 局域网可达（会触发防火墙授权）。">服务端配置</h2>
    <div class="grid2">
      <label class="field">
        <span>监听地址</span>
        <input type="text" bind:value={listen} oninput={checkChanged} placeholder={cfg?.listen ?? '127.0.0.1'} />
      </label>
      <label class="field">
        <span>端口</span>
        <input type="number" bind:value={port} oninput={checkChanged} min="1" max="65535" />
      </label>
      <label class="field">
        <span>数据目录</span>
        <input type="text" bind:value={dataDir} oninput={checkChanged} placeholder={cfg?.dataDir ?? ''} />
      </label>
    </div>
    <button class="primary" disabled={restarting || !appChanged} onclick={restartApp}>
      {restarting ? '重启中…' : '应用并重启'}
    </button>
    {#if restartErr}
      <p class="err">{restartErr}</p>
    {/if}
    <label class="field">
      <span>会话令牌（本次启动内有效；REST/SSE 用 X-EZ-Token 头，浏览器自动发起的导航、图片、PDF 请求用 ez_token cookie）</span>
      <span class="tokenRow">
        <input type="text" bind:value={apiToken} readonly placeholder="加载中…" />
        <button onclick={(e) => copyToken((e.currentTarget as HTMLButtonElement).previousElementSibling as HTMLInputElement)}>
          {tokenCopied ? '已复制' : '复制'}
        </button>
      </span>
    </label>
    <label class="field">
      <span>OpenAPI 令牌（脚本/快应用/外部程序调 /api/open/* 用；持久有效，重置后旧令牌立即作废）</span>
      <span class="tokenRow">
        <input type="text" bind:value={oaToken} readonly placeholder="加载中…" />
        <button onclick={(e) => copyOaToken((e.currentTarget as HTMLButtonElement).previousElementSibling as HTMLInputElement)}>
          {oaCopied ? '已复制' : '复制'}
        </button>
        <button onclick={resetOaToken} disabled={oaResetting || !oaToken}>{oaResetting ? '重置中…' : '重置'}</button>
      </span>
    </label>
  </section>

  <section id="set-context">
    <h2 title="上下文整理水位 = 模型上下文窗口 × 百分比：模型调用的 prompt tokens 超过即自动整理（早期对话就地折叠为摘要，立即生效，会话不变），换模型自动适配 （如 128K 窗口 × 75% = 96000）；0 表示关闭自动整理（模型仍可主动调用 trim_context 工具）。话题归档（总结归档开新会话）请用分支面板的「归档」按钮。">上下文管理</h2>
    <label class="field">
      <span>整理水位（窗口百分比）</span>
      <input type="number" bind:value={percent} min="0" max="100" />
    </label>
    <button
      class="primary"
      disabled={savingThreshold || Number(percent) === origPercent}
      onclick={saveThreshold}
    >
      {savingThreshold ? '保存中…' : '保存水位'}
    </button>
    {#if thresholdMsg}
      <p class="msg">{thresholdMsg}</p>
    {/if}
    <label class="field">
      <span>单轮最大迭代次数（0 = 默认 64，上限 128）</span>
      <input type="number" bind:value={iters} min="0" max="128" />
    </label>
    <button
      class="primary"
      disabled={savingIters || Number(iters) === origIters}
      onclick={saveIters}
    >
      {savingIters ? '保存中…' : '保存迭代次数'}
    </button>
    {#if itersMsg}
      <p class="msg">{itersMsg}</p>
    {/if}
    <label class="field">
      <span>工具结果卸载阈值/字节（超过即卸载到文件、上下文只留摘要头；0 = 默认 4096，上限 1MB；摘要头默认 512 字节、随阈值放宽但封顶 1KB）</span>
      <input type="number" bind:value={offload} min="0" max="1048576" />
    </label>
    <button
      class="primary"
      disabled={savingOffload || Number(offload) === origOffload}
      onclick={saveOffload}
    >
      {savingOffload ? '保存中…' : '保存卸载阈值'}
    </button>
    {#if offloadMsg}
      <p class="msg">{offloadMsg}</p>
    {/if}
  </section>

  <section id="set-workdir">
    <h2 title="terminal 命令默认执行目录，模型草稿与命令产物落这里；空 = 数据目录下 workspace/，相对路径按数据目录解析，保存时目录不存在会自动创建。">工作目录</h2>
    <div class="grid2">
      <label class="field">
        <span>工作目录路径</span>
        <input type="text" bind:value={workDir} placeholder="空 = 数据目录下 workspace/" />
      </label>
    </div>
    <button
      class="primary"
      disabled={savingWorkDir || workDir.trim() === origWorkDir}
      onclick={saveWorkDir}
    >
      {savingWorkDir ? '保存中…' : '保存'}
    </button>
    {#if workDirMsg}
      <p class="msg">{workDirMsg}</p>
    {/if}
  </section>

  <section id="set-window">
    <h2 title="托盘常驻：点关闭 = 隐藏窗口到托盘（后端继续运行），从托盘图标恢复或退出；变更即时生效。悬浮球右键可关闭，关闭后在此重新开启。">桌面窗口</h2>
    <label class="switch-row">
      <span>关闭时最小化到托盘</span>
      <input
        type="checkbox"
        checked={closeToTray}
        disabled={savingTray}
        onchange={(e) => saveTray((e.currentTarget as HTMLInputElement).checked)}
      />
    </label>
    <label class="switch-row">
      <span>桌面悬浮球</span>
      <input
        type="checkbox"
        checked={petEnabled}
        disabled={savingPet}
        onchange={(e) => savePet((e.currentTarget as HTMLInputElement).checked)}
      />
    </label>
  </section>

  <section id="set-debug">
    <h2 title="调试模式：把每次发给模型的完整输入（消息 + 工具清单）写进 data/logs/core-日期.log，排查上下文问题用；关闭时日志不记模型输入。dev.bat 启动时默认开启。">调试</h2>
    <label class="switch-row">
      <span>模型输入进日志（调试模式）</span>
      <input
        type="checkbox"
        checked={debugMode}
        disabled={savingDebug}
        onchange={(e) => saveDebug((e.currentTarget as HTMLInputElement).checked)}
      />
    </label>
  </section>

  <section id="set-update">
    <h2 class:dot={!!updNewer} title="启动时自动检查一次，也可手动检查 GitHub 最新 release；只报版本与发布页入口，下载安装仍在发布页完成，应用内不自动更新。">
      版本与更新
      {#if updNewer}<i class="updot"></i>{/if}
    </h2>
    <div class="updaterow">
      <span class="ver">
        <i>ezharness-{pkg.version}</i>
        {#if updNewer}
          <b class="state new">发现新版本 {updNewer.latest}</b>
        {:else if updLatest}
          <b class="state ok">✓ 已是最新</b>
        {/if}
      </span>
      <span class="actions">
        {#if updNewer}
          <a class="dl" href={updNewer.url} target="_blank" rel="noreferrer">前往下载</a>
        {/if}
        <button class="check" onclick={() => void checkUpdate()} disabled={checking}>
          {#if checking}<i class="spin"></i>{/if}
          {checking ? '检查中…' : '检查更新'}
        </button>
      </span>
    </div>
    {#if updErr}
      <p class="err">检查失败：{updErr}</p>
    {/if}
  </section>

  <section id="set-paths">
    <button
      class="fold"
      title="ezharness 不用数据库——所有数据就是数据目录下的一堆文件，改坏了删掉对应文件即可复位。"
      onclick={() => (showPaths = !showPaths)}
    >
      数据文件位置
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class:open={showPaths}>
        <path d="M9 6l6 6-6 6" />
      </svg>
    </button>
        {#if showPaths}
      <div class="paths">
        {#if cfg}
          {#each cfg.paths as p (p.file)}
            <div class="path-row">
              <span class="label">{p.label}<i>{p.file}</i></span>
              <span class="value">{p.path}</span>
            </div>
          {/each}
          <div class="path-row">
            <span class="label">长期记忆<i>memory/longterm</i></span>
            <span class="value">{cfg.memory.longterm}</span>
          </div>
          <div class="path-row">
            <span class="label">能力记忆<i>memory/skills</i></span>
            <span class="value">{cfg.memory.skills}</span>
          </div>
          <div class="path-row">
            <span class="label">话题记忆<i>sessions/</i></span>
            <span class="value">{cfg.memory.topics}</span>
          </div>
        {:else}
          <div class="path-row pending">
            <span>—</span>
          </div>
        {/if}
      </div>
    {/if}
  </section>
</div>

<style>
  .page {
    flex: 1;
    min-height: 0;
    /* 全宽滚动容器（滚动条贴窗口右缘）；内容由直接子项各自限宽居中 */
    overflow-y: auto;
    padding: 40px 48px 60px;
    display: flex;
    flex-direction: column;
    gap: 36px;
  }
  h1 {
    font-size: var(--font-size-page);
    font-weight: 700;
  }
  section {
    display: flex;
    flex-direction: column;
    gap: 12px;
    scroll-margin-top: 28px;
  }
  h2 {
    font-size: var(--font-size-section);
    font-weight: 600;
    color: var(--muted);
  }
  /* 有新版本：标题旁红点（与侧栏设置钮红点同源同款） */
  h2.dot {
    position: relative;
    color: var(--fg);
  }
  .updot {
    position: absolute;
    top: -1px;
    margin-left: 6px;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: #e74c3c;
  }
  .section-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .msg {
    font-size: var(--font-size-ui);
    color: var(--muted);
  }
  .err {
    font-size: var(--font-size-ui);
    color: #c0392b;
  }
  /* 版本检查行：与开关行同款 bordered 行——左版本号+状态，右操作；
     「前往下载」实底主按钮、「检查更新」细边次按钮 */
  .updaterow {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    border: 1px solid var(--line);
    border-radius: 10px;
    padding: 10px 14px;
  }
  .ver {
    display: inline-flex;
    align-items: baseline;
    gap: 10px;
    min-width: 0;
  }
  .ver i {
    font-family: var(--font-mono);
    font-style: normal;
    font-size: var(--font-size-section);
    color: var(--fg);
  }
  .state {
    font-size: var(--font-size-small);
    font-weight: 600;
    white-space: nowrap;
  }
  .state.ok {
    color: var(--muted);
  }
  .state.new {
    color: #f0883e;
  }
  .actions {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    flex: none;
  }
  .check {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    border: 1px solid var(--line-strong);
    border-radius: 8px;
    padding: 6px 14px;
    font-size: var(--font-size-ui);
    line-height: 1.35;
    color: var(--fg);
    transition:
      border-color var(--dur-fast) var(--ease-out),
      background var(--dur-fast) var(--ease-out);
  }
  .check:not(:disabled):hover {
    background: var(--bg-soft);
  }
  .check:disabled {
    cursor: default;
  }
  /* 手动检查转圈：与 ToolBlock 同款 spinner */
  .spin {
    flex: none;
    width: 10px;
    height: 10px;
    border: 1.5px solid color-mix(in srgb, var(--fg) 25%, transparent);
    border-top-color: var(--fg);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }
  @keyframes spin {
    to {
      transform: rotate(1turn);
    }
  }
  .dl {
    display: inline-flex;
    align-items: center;
    border: 1px solid var(--line-strong);
    border-radius: 8px;
    padding: 6px 14px;
    font-size: var(--font-size-ui);
    line-height: 1.35;
    font-weight: 500;
    background: var(--bg-invert);
    color: var(--fg-invert);
    text-decoration: none;
  }
  .grid2 {
    display: grid;
    grid-template-columns: 1fr 1.6fr;
    gap: 12px;
  }
  .switch-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    font-size: var(--font-size-section);
    color: var(--fg);
    border: 1px solid var(--line);
    border-radius: 10px;
    padding: 10px 14px;
    cursor: pointer;
  }
  .switch-row input {
    width: 16px;
    height: 16px;
    accent-color: var(--bg-invert);
    cursor: pointer;
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 5px;
    font-size: var(--font-size-ui);
    color: var(--muted);
  }
  .tokenRow {
    display: flex;
    gap: 8px;
    align-items: stretch;
  }
  .tokenRow input {
    flex: 1;
    min-width: 0;
    user-select: text;
  }
  input,
  select,
  textarea {
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 8px 10px;
    font-family: var(--font-mono);
    font-size: var(--font-size-section);
    outline: none;
    resize: vertical;
    background: var(--bg);
    color: var(--fg);
    transition: border-color var(--dur-fast) var(--ease-out);
  }
  textarea {
    font-family: var(--font-ui);
    font-size: var(--font-size-section);
  }
  input:focus,
  select:focus,
  textarea:focus {
    border-color: var(--line-strong);
  }
  .primary {
    align-self: flex-start;
    border: 1px solid var(--line-strong);
    background: var(--bg-invert);
    color: var(--fg-invert);
    border-radius: 8px;
    padding: 8px 18px;
    font-size: var(--font-size-section);
    font-weight: 500;
  }
  .saving {
    font-size: var(--font-size-ui);
    color: var(--faint);
  }
  .fold {
    display: flex;
    align-items: center;
    gap: 6px;
    border: none;
    background: transparent;
    padding: 0;
    font-size: var(--font-size-section);
    font-weight: 700;
    color: var(--muted);
  }
  .fold svg {
    width: 13px;
    height: 13px;
    transition: transform var(--dur-fast) var(--ease-out);
  }
  .fold svg.open {
    transform: rotate(90deg);
  }
  .paths {
    display: flex;
    flex-direction: column;
    border: 1px solid var(--line);
    border-radius: 12px;
    overflow: hidden;
  }
  .path-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 20px;
    padding: 10px 14px;
  }
  .path-row + .path-row {
    border-top: 1px solid var(--line);
  }
  .label {
    display: flex;
    flex-direction: column;
    font-size: var(--font-size-ui);
    color: var(--fg);
    white-space: nowrap;
  }
  .label i {
    font-style: normal;
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .value {
    font-family: var(--font-mono);
    font-size: var(--font-size-ui);
    color: var(--muted);
    text-align: right;
    overflow-wrap: anywhere;
  }
  .pending {
    padding: 18px 14px;
    font-size: var(--font-size-ui);
    color: var(--faint);
    font-family: var(--font-mono);
  }
</style>
