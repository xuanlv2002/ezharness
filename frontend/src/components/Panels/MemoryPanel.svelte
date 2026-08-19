<script lang="ts">
  import { api } from '../../lib/api'

  let content = $state('')
  let loading = $state(true)
  let saving = $state(false)
  let saved = $state(false)

  async function load() {
    loading = true
    try {
      const r = await api.getMemory()
      content = r.content
    } finally {
      loading = false
    }
  }
  void load()

  async function save() {
    saving = true
    try {
      await api.saveMemory(content)
      saved = true
      setTimeout(() => (saved = false), 1600)
    } finally {
      saving = false
    }
  }
</script>

<div class="panel-page">
  <h2>长期记忆</h2>
  <p class="hint">
    memory.md 的内容每轮注入 system——agent 也会通过文件工具更新它。
    写在这里的偏好与事实，agent 一直记得。
  </p>
  {#if loading}
    <div class="loading">读取中…</div>
  {:else}
    <textarea
      rows="18"
      bind:value={content}
      placeholder="# 记忆&#10;&#10;比如：用户偏好中文回答；项目用 Go；常用的目录是 …"
    ></textarea>
    <button class="save" disabled={saving} onclick={save}>
      {saving ? '保存中…' : saved ? '✓ 已保存' : '保存'}
    </button>
  {/if}
</div>

<style>
  .panel-page {
    padding: 20px 18px;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  h2 {
    font-size: 14px;
    font-weight: 700;
  }
  .hint {
    font-size: 11.5px;
    color: var(--muted);
    line-height: 1.6;
  }
  textarea {
    flex: 1;
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 10px;
    font-family: var(--font-mono);
    font-size: 12.5px;
    line-height: 1.6;
    outline: none;
    resize: vertical;
    min-height: 300px;
    transition: border-color var(--dur-fast) var(--ease-out);
  }
  textarea:focus {
    border-color: var(--line-strong);
  }
  .loading {
    color: var(--faint);
    font-size: 12px;
    padding: 20px 0;
  }
  .save {
    border: 1px solid var(--line-strong);
    background: var(--bg-invert);
    color: var(--fg-invert);
    border-radius: 8px;
    padding: 8px 16px;
    font-size: 13px;
    font-weight: 550;
  }
  .save:disabled {
    opacity: 0.4;
  }
</style>
