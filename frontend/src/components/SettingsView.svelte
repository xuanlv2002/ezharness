<script lang="ts">
  import { onMount } from 'svelte'
  import { api, type AppConfig } from '../lib/api'

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

  /* 单轮最大迭代次数（模型工具循环上限；0 = 默认 64，上限 128，随 Reassemble 生效） */
  let iters = $state<number | ''>('')
  let origIters = $state<number | null>(null)
  let savingIters = $state(false)
  let itersMsg = $state('')

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
      workDir = st.workDir ?? ''
      origWorkDir = st.workDir ?? ''
      origExtra = st.systemExtra ?? ''
      closeToTray = st.closeToTray ?? false
      iters = st.maxIterations ?? 64
      origIters = st.maxIterations ?? 64
    } catch {
      /* 上下文配置加载失败不阻塞页面 */
    }
  })

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
      const v = Math.min(Math.max(Number(iters) || 0, 0), 50)
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
              // 换端口跳转：桌面窗口的标题栏靠 ?desktop=1 渲染，跳转目标需补上
              const desktop = new URLSearchParams(location.search).has('desktop')
              location.assign(desktop ? `${res.url}/?desktop=1` : res.url)
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

  <section>
    <h2>服务端配置</h2>
    <p class="hint">监听、端口与数据目录是启动期配置，修改后需点「应用并重启」换代（进程内完成，数据目录变更自动迁移）；其余设置保存即生效，无需重启。监听 127.0.0.1 = 仅本机访问（默认，无防火墙弹窗），0.0.0.0 = 局域网可达（会触发防火墙授权）。</p>
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
  </section>

  <section>
    <h2>上下文管理</h2>
    <p class="hint">
      上下文整理水位 = 模型上下文窗口 × 百分比：模型调用的 prompt tokens
      超过即自动整理（早期对话就地折叠为摘要，立即生效，会话不变），换模型自动适配
      （如 128K 窗口 × 75% = 96000）；0 表示关闭自动整理（模型仍可主动调用
      trim_context 工具）。话题归档（总结归档开新会话）请用分支面板的「归档」按钮。
    </p>
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
    <label class="field">
      <span>单轮最大迭代次数（0 = 默认 64，上限 128）</span>
      <input type="number" bind:value={iters} min="0" max="50" />
    </label>
    <button
      class="primary"
      disabled={savingIters || Number(iters) === origIters}
      onclick={saveIters}
    >
      {savingIters ? '保存中…' : '保存迭代次数'}
    </button>
    {#if thresholdMsg}
      <p class="msg">{thresholdMsg}</p>
    {/if}
    {#if itersMsg}
      <p class="msg">{itersMsg}</p>
    {/if}
  </section>

  <section>
    <h2>工作目录</h2>
    <p class="hint">
      terminal 命令默认执行目录，模型草稿与命令产物落这里；空 =
      数据目录下 workspace/，相对路径按数据目录解析，保存时目录不存在会自动创建。
    </p>
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

  <section>
    <h2>桌面窗口</h2>
    <p class="hint">托盘常驻：点关闭 = 隐藏窗口到托盘（后端继续运行），从托盘图标恢复或退出；变更即时生效。</p>
    <label class="switch-row">
      <span>关闭时最小化到托盘</span>
      <input
        type="checkbox"
        checked={closeToTray}
        disabled={savingTray}
        onchange={(e) => saveTray((e.currentTarget as HTMLInputElement).checked)}
      />
    </label>
  </section>

  <section>
    <button class="fold" onclick={() => (showPaths = !showPaths)}>
      数据文件位置
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class:open={showPaths}>
        <path d="M9 6l6 6-6 6" />
      </svg>
    </button>
    <p class="hint">ezharness 不用数据库——所有数据就是数据目录下的一堆文件，改坏了删掉对应文件即可复位。</p>
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
    overflow-y: auto;
    max-width: 720px;
    width: 100%;
    margin: 0 auto;
    padding: 40px 48px 60px;
    display: flex;
    flex-direction: column;
    gap: 36px;
  }
  h1 {
    font-size: 18px;
    font-weight: 700;
  }
  section {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  h2 {
    font-size: 13px;
    font-weight: 700;
    color: var(--muted);
  }
  .section-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .hint {
    font-size: 11px;
    color: var(--faint);
    margin-top: -6px;
  }
  .msg {
    font-size: 11.5px;
    color: var(--muted);
  }
  .err {
    font-size: 11.5px;
    color: #c0392b;
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
    font-size: 13px;
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
    font-size: 12px;
    color: var(--muted);
  }
  input,
  select,
  textarea {
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 8px 10px;
    font-family: var(--font-mono);
    font-size: 12.5px;
    outline: none;
    resize: vertical;
    background: var(--bg);
    color: var(--fg);
    transition: border-color var(--dur-fast) var(--ease-out);
  }
  textarea {
    font-family: var(--font-ui);
    font-size: 13px;
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
    font-size: 13px;
    font-weight: 550;
  }
  .saving {
    font-size: 11.5px;
    color: var(--faint);
  }
  .fold {
    display: flex;
    align-items: center;
    gap: 6px;
    border: none;
    background: transparent;
    padding: 0;
    font-size: 13px;
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
    font-size: 12px;
    color: var(--fg);
    white-space: nowrap;
  }
  .label i {
    font-style: normal;
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--faint);
  }
  .value {
    font-family: var(--font-mono);
    font-size: 11.5px;
    color: var(--muted);
    text-align: right;
    overflow-wrap: anywhere;
  }
  .pending {
    padding: 18px 14px;
    font-size: 12px;
    color: var(--faint);
    font-family: var(--font-mono);
  }
</style>
