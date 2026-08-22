<script lang="ts">
  import { onMount } from 'svelte'
  import { api, type AppConfig, type Settings } from '../lib/api'

  /* 设置页 = 应用结构配置（服务端）+ Agent 行为 + 数据/配置文件路径。全部后端下发。 */
  let cfg = $state<AppConfig | null>(null)
  let port = $state<number | ''>('')
  let dataDir = $state('')
  let restarting = $state(false)
  let restartErr = $state('')
  let appChanged = $state(false)

  let behavior = $state<Settings>({ systemExtra: '', shell: 'auto' })
  let savingBehavior = $state(false)
  let behaviorMsg = $state('')
  let showPaths = $state(false)

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
      behavior = await api.getSettings()
    } catch {
      /* 行为设置加载失败不阻塞页面 */
    }
  })

  function checkChanged() {
    appChanged = !!cfg && (String(port) !== String(cfg.port) || dataDir.trim() !== cfg.dataDir)
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

  /* 行为设置自动保存：select 变更即存，textarea 失焦（change）即存。 */
  async function saveBehavior() {
    savingBehavior = true
    behaviorMsg = ''
    try {
      await api.saveSettings(behavior)
      behaviorMsg = '已保存，下一轮生效'
    } catch (e) {
      behaviorMsg = `保存失败：${(e as Error).message}`
    } finally {
      savingBehavior = false
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
    <div class="section-head">
      <h2>Agent 行为</h2>
      {#if savingBehavior}
        <span class="saving">保存中…</span>
      {/if}
    </div>
    <label class="field">
      <span>Shell（bash 工具的执行器）</span>
      <select bind:value={behavior.shell} onchange={() => void saveBehavior()}>
        <option value="auto">auto（探测：bash → pwsh → cmd）</option>
        <option value="bash">bash</option>
        <option value="pwsh">pwsh / powershell</option>
        <option value="cmd">cmd</option>
      </select>
    </label>
    <label class="field">
      <span>系统提示追加</span>
      <textarea
        rows="4"
        bind:value={behavior.systemExtra}
        onchange={() => void saveBehavior()}
        placeholder="追加到系统提示的自定义内容（角色设定、约束等），失焦自动保存"
      ></textarea>
    </label>
    {#if behaviorMsg}
      <p class="msg">{behaviorMsg}</p>
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
