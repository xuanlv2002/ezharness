<script lang="ts">
  import { onMount } from 'svelte'
  import { api, type AppConfig } from '../lib/api'

  /* 设置页 = 应用结构配置（服务端）+ 上下文管理 + 数据/配置文件路径。 */
  let cfg = $state<AppConfig | null>(null)
  let port = $state<number | ''>('')
  let dataDir = $state('')
  let restarting = $state(false)
  let restartErr = $state('')
  let appChanged = $state(false)
  let showPaths = $state(false)

  /* 上下文压缩水位（0 = 关闭自动压缩，模型仍可主动调 compact 工具） */
  let threshold = $state<number | ''>('')
  let origThreshold = $state<number | null>(null)
  let origExtra = $state('')
  let savingThreshold = $state(false)
  let thresholdMsg = $state('')

  onMount(async () => {
    try {
      cfg = await api.appConfig()
      port = cfg.port
      dataDir = cfg.dataDir
      appChanged = false
    } catch {
      restartErr = '配置加载失败（后端不可达）'
    }
    try {
      const st = await api.getSettings()
      threshold = st.compactThreshold ?? 0
      origThreshold = st.compactThreshold ?? 0
      origExtra = st.systemExtra ?? ''
    } catch {
      /* 上下文配置加载失败不阻塞页面 */
    }
  })

  function checkChanged() {
    appChanged = !!cfg && (String(port) !== String(cfg.port) || dataDir.trim() !== cfg.dataDir)
  }

  async function saveThreshold() {
    savingThreshold = true
    thresholdMsg = ''
    try {
      // systemExtra 回传原值：保存接口是整体语义，缺省会清空
      await api.saveSettings({
        systemExtra: origExtra,
        compactThreshold: Number(threshold) || 0,
      })
      origThreshold = Number(threshold) || 0
      thresholdMsg = '已保存（下一轮对话生效）'
    } catch (e) {
      thresholdMsg = `保存失败：${e instanceof Error ? e.message : String(e)}`
    } finally {
      savingThreshold = false
    }
  }

  async function restartApp() {
    restarting = true
    restartErr = ''
    try {
      const req: { port?: number; dataDir?: string } = {}
      if (String(port) !== String(cfg?.port)) req.port = Number(port)
      if (dataDir.trim() !== cfg?.dataDir) req.dataDir = dataDir.trim()
      const res = await api.appRestart(req)
      for (let i = 0; i < 60; i++) {
        await new Promise((r) => setTimeout(r, 500))
        try {
          const h = await api.appHealth(res.url)
          if (h.boot === res.boot) {
            if (res.url === location.origin) location.reload()
            else location.assign(res.url)
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
    <p class="hint">端口与数据目录变更需换代重启（进程内完成，数据目录变更自动迁移）。</p>
    <div class="grid2">
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
      上下文压缩水位（prompt tokens）：轮末超过即自动压缩归档并开启新会话；0
      表示关闭自动压缩（模型仍可主动调用 compact 工具，状态栏会提示推荐压缩时机）。
    </p>
    <div class="grid2">
      <label class="field">
        <span>压缩水位（tokens）</span>
        <input type="number" bind:value={threshold} min="0" step="1000" />
      </label>
    </div>
    <button
      class="primary"
      disabled={savingThreshold || Number(threshold) === origThreshold}
      onclick={saveThreshold}
    >
      {savingThreshold ? '保存中…' : '保存水位'}
    </button>
    {#if thresholdMsg}
      <p class="msg">{thresholdMsg}</p>
    {/if}
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
