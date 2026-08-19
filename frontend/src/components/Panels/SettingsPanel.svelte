<script lang="ts">
  import { store } from '../../lib/store.svelte'

  let model = $state('')
  let baseUrl = $state('')
  let systemExtra = $state('')
  let threshold = $state(0)
  let shell = $state('auto')
  let saving = $state(false)
  let loaded = $state(false)

  $effect(() => {
    if (store.settings && !loaded) {
      model = store.settings.model
      baseUrl = store.settings.baseUrl
      systemExtra = store.settings.systemExtra
      threshold = store.settings.rotateThreshold
      shell = store.settings.shell || 'auto'
      loaded = true
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
</style>
