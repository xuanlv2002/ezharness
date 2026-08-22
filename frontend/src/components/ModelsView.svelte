<script lang="ts">
  import { onMount } from 'svelte'
  import { api, type ModelEntry, type ModelsConfig } from '../lib/api'

  /*
  「主模型 + 能力槽」四类：main（驱动 agent 循环，可直接选多模态模型）/
  vision（主模型无视觉时转写兜底）/ image（文生图）/ audio（语音合成）。
  数据 GET/PUT /api/models，每槽至多启用一个，保存后重建 agent。
  */
  type SlotKey = keyof ModelsConfig
  const kinds: { key: SlotKey; title: string; hint: string }[] = [
    { key: 'main', title: '主模型', hint: '驱动对话与工具循环，可直接选用多模态模型' },
    { key: 'vision', title: '多模态模型', hint: '主模型无视觉能力时，用于图片识别转写' },
    { key: 'image', title: '图片生成', hint: '主模型按需调用的文生图工具' },
    { key: 'audio', title: '声音生成', hint: '主模型按需调用的语音合成工具' },
  ]

  let cfg = $state<ModelsConfig | null>(null)
  let loaded = $state(false)
  let saving = $state(false)
  let message = $state('')
  let adding = $state<SlotKey | null>(null)
  let draft = $state({ name: '', baseUrl: '', apiKey: '' })
  let editing = $state<{ slot: SlotKey; idx: number } | null>(null)
  let editDraft = $state({ name: '', baseUrl: '', apiKey: '' })

  onMount(async () => {
    try {
      cfg = await api.getModels()
      /* 后端空槽可能为 null（Go nil slice），兜底为空数组 */
      for (const k of kinds) {
        if (!cfg[k.key]) cfg[k.key] = []
      }
    } catch {
      message = '模型数据加载失败（后端不可达）'
    }
    loaded = true
  })

  /* persist 自动保存（所有变更即时提交，busy 时 409 提示等待本轮结束）。 */
  async function persist() {
    if (!cfg) return
    saving = true
    message = ''
    try {
      await api.saveModels(cfg)
      message = '已保存，下一轮生效'
    } catch (e) {
      message = `保存失败：${(e as Error).message}（agent 运行中请等本轮结束）`
    } finally {
      saving = false
    }
  }

  function enable(slot: SlotKey, idx: number) {
    if (!cfg) return
    cfg[slot] = cfg[slot].map((e, i) => ({ ...e, enabled: i === idx }))
    void persist()
  }

  function remove(slot: SlotKey, idx: number) {
    if (!cfg) return
    cfg[slot] = cfg[slot].filter((_, i) => i !== idx)
    void persist()
  }

  function addEntry(slot: SlotKey) {
    if (!cfg || !draft.name.trim() || !draft.baseUrl.trim()) return
    cfg[slot] = [
      ...cfg[slot],
      {
        name: draft.name.trim(),
        baseUrl: draft.baseUrl.trim(),
        apiKey: draft.apiKey.trim(),
        enabled: cfg[slot].length === 0,
        tokens: 0,
        cost: 0,
      },
    ]
    draft = { name: '', baseUrl: '', apiKey: '' }
    adding = null
    void persist()
  }

  function editEntry(slot: SlotKey, idx: number) {
    if (!cfg) return
    const m = cfg[slot][idx]
    editing = { slot, idx }
    editDraft = { name: m.name, baseUrl: m.baseUrl, apiKey: m.apiKey }
  }

  function saveEdit() {
    if (!cfg || !editing) return
    const { slot, idx } = editing
    if (!editDraft.name.trim() || !editDraft.baseUrl.trim()) return
    cfg[slot] = cfg[slot].map((e, i) =>
      i === idx ? { ...e, name: editDraft.name.trim(), baseUrl: editDraft.baseUrl.trim(), apiKey: editDraft.apiKey.trim() } : e,
    )
    editing = null
    void persist()
  }

  function maskKey(k: string): string {
    if (!k) return '未配置 Key'
    return k.length > 8 ? `${k.slice(0, 6)}…${k.slice(-4)}` : '已配置'
  }

  function fmtTokens(n: number): string {
    if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
    if (n >= 1000) return `${(n / 1000).toFixed(1)}k`
    return String(n)
  }

  function defaultBase(): string {
    return 'https://api.siliconflow.cn/v1'
  }
</script>

<div class="page">
  <div class="head">
    <div>
      <h1>模型</h1>
      <p class="lead">主模型驱动 agent，能力槽按需兜底——每类启用一个，变更自动保存。</p>
    </div>
    {#if saving}
      <span class="saving">保存中…</span>
    {/if}
  </div>
  {#if message}
    <p class="msg">{message}</p>
  {/if}

  {#if !loaded}
    <p class="lead">加载中…</p>
  {:else if cfg}
    {#each kinds as k (k.key)}
      <section>
        <h2>{k.title}</h2>
        <p class="hint">{k.hint} · 每类启用一个。</p>
        <div class="list">
          {#each cfg[k.key] as m, i (m.name + i)}
            <div class="model-wrap">
              <div class="model" class:enabled={m.enabled}>
                <button class="dot" class:on={m.enabled} onclick={() => enable(k.key, i)} title={m.enabled ? '已启用' : '点击启用'}></button>
                <div class="info">
                  <span class="name">{m.name}</span>
                  <span class="meta">{m.baseUrl} · {maskKey(m.apiKey)}</span>
                </div>
                <div class="usage">
                  <span class="tokens" title="累计用量">{m.tokens > 0 ? fmtTokens(m.tokens) : '0'} tokens</span>
                  <button class="del" onclick={() => editEntry(k.key, i)} title="编辑">编辑</button>
                  <button class="del" onclick={() => remove(k.key, i)} title="删除">删除</button>
                </div>
              </div>
              {#if editing && editing.slot === k.key && editing.idx === i}
                <div class="add-form">
                  <input type="text" placeholder="模型名" bind:value={editDraft.name} />
                  <input type="text" placeholder="API 端点" bind:value={editDraft.baseUrl} />
                  <input type="password" placeholder="API Key" bind:value={editDraft.apiKey} />
                  <div class="add-actions">
                    <button class="add-ok" disabled={!editDraft.name.trim() || !editDraft.baseUrl.trim()} onclick={saveEdit}>确定</button>
                    <button class="add-no" onclick={() => (editing = null)}>取消</button>
                  </div>
                </div>
              {/if}
            </div>
          {/each}
          {#if cfg[k.key].length === 0}
            <div class="model none">暂无模型</div>
          {/if}
          {#if adding === k.key}
            <div class="add-form">
              <input type="text" placeholder="模型名（如 deepseek-ai/DeepSeek-V3.2）" bind:value={draft.name} />
              <input type="text" placeholder={defaultBase()} bind:value={draft.baseUrl} />
              <input type="password" placeholder="API Key" bind:value={draft.apiKey} />
              <div class="add-actions">
                <button class="add-ok" disabled={!draft.name.trim() || !draft.baseUrl.trim()} onclick={() => addEntry(k.key)}>添加</button>
                <button class="add-no" onclick={() => (adding = null)}>取消</button>
              </div>
            </div>
          {:else}
            <button class="add" onclick={() => (adding = k.key)}>+ 添加模型</button>
          {/if}
        </div>
      </section>
    {/each}
  {/if}
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
    gap: 30px;
  }
  .head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
  }
  h1 {
    font-size: 18px;
    font-weight: 700;
  }
  .lead {
    font-size: 12.5px;
    color: var(--muted);
    margin-top: 4px;
  }
  .msg {
    font-size: 11.5px;
    color: var(--muted);
    margin-top: -20px;
  }
  .saving {
    font-size: 11.5px;
    color: var(--faint);
  }
  section {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  h2 {
    font-size: 13px;
    font-weight: 700;
    color: var(--muted);
  }
  .hint {
    font-size: 11px;
    color: var(--faint);
    margin-top: -4px;
  }
  .list {
    display: flex;
    flex-direction: column;
    border: 1px solid var(--line);
    border-radius: 12px;
    overflow: hidden;
  }
  .model-wrap + .model-wrap {
    border-top: 1px solid var(--line);
  }
  .model-wrap .add-form {
    border-top: 1px dashed var(--line);
  }
  .model {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 11px 14px;
  }
  .model-wrap + .model-wrap + .add,
  .model-wrap + .add,
  .add + .model-wrap,
  .none + .add {
    border-top: 1px solid var(--line);
  }
  .model.enabled {
    background: color-mix(in srgb, var(--accent) 5%, var(--bg));
  }
  .dot {
    flex: none;
    width: 9px;
    height: 9px;
    border-radius: 50%;
    border: 1.5px solid var(--faint);
    background: transparent;
    padding: 0;
  }
  .dot.on {
    border-color: var(--accent);
    background: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }
  .info {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .name {
    font-family: var(--font-mono);
    font-size: 12.5px;
    color: var(--fg);
    overflow-wrap: anywhere;
  }
  .meta {
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--faint);
    overflow-wrap: anywhere;
  }
  .usage {
    flex: none;
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .tokens {
    font-family: var(--font-mono);
    font-size: 11.5px;
    color: var(--muted);
    white-space: nowrap;
  }
  .del {
    border: none;
    background: transparent;
    color: var(--faint);
    font-size: 11.5px;
    padding: 4px 8px;
    border-radius: 6px;
    opacity: 0;
    transition: opacity var(--dur-fast) var(--ease-out);
  }
  .model:hover .del {
    opacity: 1;
  }
  .del:hover {
    color: #c0392b;
    background: color-mix(in srgb, #c0392b 8%, transparent);
  }
  .none {
    font-size: 12px;
    color: var(--faint);
    padding: 16px 14px;
  }
  .add {
    border: none;
    background: transparent;
    padding: 10px 14px;
    text-align: left;
    font-size: 12px;
    color: var(--faint);
  }
  .add:hover {
    color: var(--fg);
  }
  .add-form {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 12px 14px;
  }
  .add-form input {
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 8px 10px;
    font-family: var(--font-mono);
    font-size: 12px;
    outline: none;
    background: var(--bg);
    color: var(--fg);
  }
  .add-form input:focus {
    border-color: var(--line-strong);
  }
  .add-actions {
    display: flex;
    gap: 8px;
  }
  .add-ok {
    border: 1px solid var(--line-strong);
    background: var(--bg-invert);
    color: var(--fg-invert);
    border-radius: 8px;
    padding: 6px 16px;
    font-size: 12px;
    font-weight: 550;
  }
  .add-ok:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .add-no {
    border: 1px solid var(--line);
    background: transparent;
    color: var(--muted);
    border-radius: 8px;
    padding: 6px 16px;
    font-size: 12px;
  }
</style>
