<script lang="ts">
  import { api } from '../../lib/api'
  import { store } from '../../lib/store.svelte'

  let model = $state('')
  let baseUrl = $state('')
  let systemExtra = $state('')
  let threshold = $state(0)
  let shell = $state('auto')
  let saving = $state(false)
  let loaded = $state(false)

  let curPort = $state(0)
  let curDataDir = $state('')
  let port = $state(0)
  let dataDir = $state('')
  let restarting = $state(false)
  let restartErr = $state('')

  let appChanged = $derived(port > 0 && port !== curPort || dataDir.trim() !== curDataDir)

  $effect(() => {
    if (store.settings && !loaded) {
      model = store.settings.model
      baseUrl = store.settings.baseUrl
      systemExtra = store.settings.systemExtra
      threshold = store.settings.rotateThreshold
      shell = store.settings.shell || 'auto'
      loaded = true
      api.appConfig().then((c) => {
        curPort = c.port
        curDataDir = c.dataDir
        port = c.port
        dataDir = c.dataDir
      })
    }
  })

  async function save() {
    saving = true
    try {
      await store.saveSettings({
        model: model.trim(),
        baseUrl: baseUrl.trim(),
        systemExtra,
        rotateThreshold: threshold,
        shell,
      })
    } finally {
      saving = false
    }
  }

  async function restartApp() {
    restarting = true
    restartErr = ''
    try {
      const req: { port?: number; dataDir?: string } = {}
      if (port !== curPort) req.port = port
      if (dataDir.trim() !== curDataDir) req.dataDir = dataDir.trim()
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

<div class="panel-page">
  <h2>设置</h2>

  <label>
    <span>模型</span>
    <input type="text" bind:value={model} placeholder="deepseek-ai/DeepSeek-V3.2" />
  </label>

  <label>
    <span>API 端点</span>
    <input type="text" bind:value={baseUrl} placeholder="https://api.siliconflow.cn/v1" />
  </label>

  <label>
    <span>自动轮换水位（tokens，0 = 禁用）</span>
    <input type="number" bind:value={threshold} min="0" step="10000" />
  </label>

  <label>
    <span>Shell（bash 工具的执行器）</span>
    <select bind:value={shell}>
      <option value="auto">auto（探测：bash → pwsh → cmd）</option>
      <option value="bash">bash</option>
      <option value="pwsh">pwsh / powershell</option>
      <option value="cmd">cmd</option>
    </select>
  </label>

  <label>
    <span>系统提示追加</span>
    <textarea
      rows="5"
      bind:value={systemExtra}
      placeholder="追加到系统提示的自定义内容（角色设定、约束等）"
    ></textarea>
  </label>

  <button class="save" disabled={saving || !model.trim() || !baseUrl.trim()} onclick={save}>
    {saving ? '保存中…' : '保存'}
  </button>
  <p class="hint">变更即时生效（重建 agent），运行中需等本轮结束。</p>

  <div class="app-section">
    <h3>应用</h3>
    <label>
      <span>端口</span>
      <input type="number" bind:value={port} min="1" max="65535" />
    </label>
    <label>
      <span>数据目录（settings / sessions / topics / memory 存储位置，清空恢复默认）</span>
      <input type="text" bind:value={dataDir} placeholder={curDataDir} />
    </label>
    <button class="save" disabled={restarting || !appChanged} onclick={restartApp}>
      {restarting ? '重启中…' : '应用并重启'}
    </button>
    <p class="hint">更换目录会自动迁移数据（不覆盖已有文件）；重启后页面自动跳转新地址。</p>
    {#if restartErr}
      <p class="err">{restartErr}</p>
    {/if}
  </div>
</div>

<style>
  .panel-page {
    padding: 20px 18px;
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  h2 {
    font-size: 14px;
    font-weight: 700;
  }
  label {
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
  textarea:focus {
    border-color: var(--line-strong);
  }
  .save {
    border: 1px solid var(--line-strong);
    background: var(--bg-invert);
    color: var(--fg-invert);
    border-radius: 8px;
    padding: 8px 16px;
    font-size: 13px;
    font-weight: 550;
    transition: opacity var(--dur-fast) var(--ease-out);
  }
  .save:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .hint {
    font-size: 11px;
    color: var(--faint);
  }
  .app-section {
    margin-top: 18px;
    padding-top: 16px;
    border-top: 1px dashed var(--line);
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  .app-section h3 {
    font-size: 12px;
    font-weight: 700;
    color: var(--muted);
  }
  .err {
    font-size: 12px;
    color: #c0392b;
  }
</style>
