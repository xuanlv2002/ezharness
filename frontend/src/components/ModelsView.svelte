<script lang="ts">
  import { onMount } from 'svelte'
  import { api, type ModelEntry, type ModelsConfig } from '../lib/api'

  /*
  「主模型 + 能力槽」四类：main（驱动 agent 循环的唯一对话模型，支持
  视觉则图片直接进上下文，否则图片落盘并引导工具识别）/ vision
  （图片识别：启用后 agent 获得 image_recognize 工具）/ image（图片
  生成）/ audio（声音生成）——能力槽配置并启用后向 agent 暴露对应
  工具（image/audio 的工具后续版本提供）。数据 GET/PUT /api/models，
  每槽至多启用一个，保存后重建 agent。
  */
  type SlotKey = keyof ModelsConfig
  const kinds: { key: SlotKey; title: string; hint: string }[] = [
    { key: 'main', title: '主模型', hint: '驱动对话与工具循环；勾选"支持视觉"则图片直接进上下文，否则图片存文件并引导用工具识别' },
    { key: 'vision', title: '图片识别', hint: '启用后 agent 获得图片识别工具（image_recognize），可识别任意图片文件' },
    { key: 'image', title: '图片生成', hint: '启用后向 agent 暴露文生图工具（工具后续版本提供）' },
    { key: 'audio', title: '声音生成', hint: '启用后向 agent 暴露语音合成工具（工具后续版本提供）' },
  ]

  interface HeaderPair {
    key: string
    value: string
  }

  let cfg = $state<ModelsConfig | null>(null)
  let loaded = $state(false)
  let saving = $state(false)
  let message = $state('')
  let adding = $state<SlotKey | null>(null)
  let draft = $state({ name: '', baseUrl: '', apiKey: '', contextWindow: 128000, protocol: 'openai', vision: false, pairs: [] as HeaderPair[] })
  let editing = $state<{ slot: SlotKey; idx: number } | null>(null)
  let editDraft = $state({ name: '', baseUrl: '', apiKey: '', contextWindow: 128000, protocol: 'openai', vision: false, pairs: [] as HeaderPair[] })

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

  /* 启用切换：点亮目标为槽内唯一启用；再次点击已启用的条目 = 停用整槽
  （main 槽除外——对话必须由主模型驱动，后端也会兜底点亮首条） */
  function enable(slot: SlotKey, idx: number) {
    if (!cfg) return
    if (cfg[slot][idx].enabled) {
      if (slot === 'main') return
      cfg[slot] = cfg[slot].map((e) => ({ ...e, enabled: false }))
    } else {
      cfg[slot] = cfg[slot].map((e, i) => ({ ...e, enabled: i === idx }))
    }
    void persist()
  }

  function remove(slot: SlotKey, idx: number) {
    if (!cfg) return
    cfg[slot] = cfg[slot].filter((_, i) => i !== idx)
    void persist()
  }

  /* pairsToRecord 过滤空键后转 map（与 McpView 同模式）。 */
  function pairsToRecord(pairs: HeaderPair[]): Record<string, string> {
    const out: Record<string, string> = {}
    for (const p of pairs) if (p.key.trim()) out[p.key.trim()] = p.value
    return out
  }

  function addEntry(slot: SlotKey) {
    if (!cfg || !draft.name.trim() || !draft.baseUrl.trim()) return
    const headers = pairsToRecord(draft.pairs)
    cfg[slot] = [
      ...cfg[slot],
      {
        name: draft.name.trim(),
        baseUrl: draft.baseUrl.trim(),
        apiKey: draft.apiKey.trim(),
        ...(Object.keys(headers).length ? { headers } : {}),
        enabled: cfg[slot].length === 0,
        ...(draft.vision ? { vision: true } : {}),
        contextWindow: Number(draft.contextWindow) || 0,
        protocol: draft.protocol || 'openai',
      },
    ]
    draft = { name: '', baseUrl: '', apiKey: '', contextWindow: 128000, protocol: 'openai', vision: false, pairs: [] }
    adding = null
    void persist()
  }

  function editEntry(slot: SlotKey, idx: number) {
    if (!cfg) return
    const m = cfg[slot][idx]
    editing = { slot, idx }
    editDraft = {
      name: m.name,
      baseUrl: m.baseUrl,
      apiKey: m.apiKey,
      contextWindow: m.contextWindow || 128000,
      protocol: m.protocol || 'openai',
      vision: !!m.vision,
      pairs: Object.entries(m.headers ?? {}).map(([key, value]) => ({ key, value })),
    }
  }

  function saveEdit() {
    if (!cfg || !editing) return
    const { slot, idx } = editing
    if (!editDraft.name.trim() || !editDraft.baseUrl.trim()) return
    const headers = pairsToRecord(editDraft.pairs)
    cfg[slot] = cfg[slot].map((e, i) =>
      i === idx
        ? {
            ...e,
            name: editDraft.name.trim(),
            baseUrl: editDraft.baseUrl.trim(),
            apiKey: editDraft.apiKey.trim(),
            headers: Object.keys(headers).length ? headers : undefined, // 删光时清掉旧值
            ...(editDraft.vision ? { vision: true } : { vision: false }),
            contextWindow: Number(editDraft.contextWindow) || 0,
            protocol: editDraft.protocol || 'openai',
          }
        : e,
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
                <button class="dot" class:on={m.enabled} onclick={() => enable(k.key, i)} title={m.enabled ? (k.key === 'main' ? '已启用（主模型必启用）' : '已启用（点击停用）') : '点击启用'}></button>
                <div class="info">
                  <span class="name">{m.name}</span>
                  <span class="meta">{m.protocol && m.protocol !== 'openai' ? `【${m.protocol}】` : ''}{m.baseUrl} · {maskKey(m.apiKey)}{m.contextWindow ? ` · ${fmtTokens(m.contextWindow)} ctx` : ''}{m.vision ? ' · 👁 视觉' : ''}{Object.keys(m.headers ?? {}).length ? ` · ${Object.keys(m.headers ?? {}).length} 个请求头` : ''}</span>
                </div>
                <div class="usage">
                  <span class="tokens" title="累计用量（输入/输出/缓存命中）">↑{fmtTokens(m.inTokens || 0)} ↓{fmtTokens(m.outTokens || 0)}{m.cacheTokens ? ` ⚡${fmtTokens(m.cacheTokens)}` : ''}</span>
                  <button class="del" onclick={() => editEntry(k.key, i)} title="编辑">编辑</button>
                  <button class="del" onclick={() => remove(k.key, i)} title="删除">删除</button>
                </div>
              </div>
              {#if editing && editing.slot === k.key && editing.idx === i}
                <div class="add-form">
                  <input type="text" placeholder="模型名" bind:value={editDraft.name} />
                  <select bind:value={editDraft.protocol} title="API 协议：决定请求格式（chat/completions / responses / Claude messages）">
                    <option value="openai">OpenAI 兼容</option>
                    <option value="responses">Responses</option>
                    <option value="anthropic">Anthropic</option>
                  </select>
                  <input type="text" placeholder="API 端点" bind:value={editDraft.baseUrl} />
                  <input type="password" placeholder="API Key" bind:value={editDraft.apiKey} />
                  <input type="number" placeholder="上下文窗口（tokens）" bind:value={editDraft.contextWindow} title="上下文窗口（tokens），水位与压缩按此计算；0 表示未知（按 128k 兜底）" />
                  <label class="vision-ck" title="勾选表示模型支持多模态视觉输入（可发图片）；未勾选时带图请求会自动省略图片，防止不支持视觉的模型报错卡死会话">
                    <input type="checkbox" bind:checked={editDraft.vision} />
                    <span>支持视觉（图片输入）</span>
                  </label>
                  {#each editDraft.pairs as p, j}
                    <div class="hdr-row">
                      <input type="text" placeholder="Header（如 X-Org-Id）" bind:value={p.key} />
                      <input type="text" placeholder="Value（如 org-123）" bind:value={p.value} />
                      <button class="hdr-x" onclick={() => (editDraft.pairs = editDraft.pairs.filter((_, k2) => k2 !== j))} title="移除">
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round">
                          <path d="M6 6l12 12M18 6L6 18" />
                        </svg>
                      </button>
                    </div>
                  {/each}
                  <button class="hdr-add" onclick={() => (editDraft.pairs = [...editDraft.pairs, { key: '', value: '' }])}>+ 请求头</button>
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
              <select bind:value={draft.protocol} title="API 协议：决定请求格式（chat/completions / responses / Claude messages）">
                <option value="openai">OpenAI 兼容</option>
                <option value="responses">Responses</option>
                <option value="anthropic">Anthropic</option>
              </select>
              <input type="text" placeholder={defaultBase()} bind:value={draft.baseUrl} />
              <input type="password" placeholder="API Key" bind:value={draft.apiKey} />
              <input type="number" placeholder="上下文窗口（tokens）" bind:value={draft.contextWindow} title="上下文窗口（tokens），水位与压缩按此计算；0 表示未知（按 128k 兜底）" />
              <label class="vision-ck" title="勾选表示模型支持多模态视觉输入（可发图片）；未勾选时带图请求会自动省略图片，防止不支持视觉的模型报错卡死会话">
                <input type="checkbox" bind:checked={draft.vision} />
                <span>支持视觉（图片输入）</span>
              </label>
              {#each draft.pairs as p, j}
                <div class="hdr-row">
                  <input type="text" placeholder="Header（如 X-Org-Id）" bind:value={p.key} />
                  <input type="text" placeholder="Value（如 org-123）" bind:value={p.value} />
                  <button class="hdr-x" onclick={() => (draft.pairs = draft.pairs.filter((_, k2) => k2 !== j))} title="移除">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round">
                      <path d="M6 6l12 12M18 6L6 18" />
                    </svg>
                  </button>
                </div>
              {/each}
              <button class="hdr-add" onclick={() => (draft.pairs = [...draft.pairs, { key: '', value: '' }])}>+ 请求头</button>
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
  .add-form input,
  .add-form select {
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 8px 10px;
    font-family: var(--font-mono);
    font-size: 12px;
    outline: none;
    background: var(--bg);
    color: var(--fg);
  }
  .add-form input:focus,
  .add-form select:focus {
    border-color: var(--line-strong);
  }
  /* 视觉开关：checkbox 不吃表单输入框样式，横排一行 */
  .vision-ck {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12px;
    color: var(--muted);
    cursor: pointer;
    user-select: none;
  }
  .vision-ck input {
    width: 14px;
    height: 14px;
    accent-color: var(--accent);
    cursor: pointer;
  }
  /* select 去原生外观（Windows 白底系统控件与表单不协调），自绘下拉箭头 */
  .add-form select {
    appearance: none;
    padding-right: 30px;
    cursor: pointer;
    background: var(--bg) url("data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' width='10' height='6' viewBox='0 0 10 6'><path d='M1 1l4 4 4-4' fill='none' stroke='%23888' stroke-width='1.6' stroke-linecap='round' stroke-linejoin='round'/></svg>") no-repeat right 10px center;
  }
  .hdr-row {
    display: grid;
    /* minmax(0,…) 压掉 1fr 的内容固有宽下限:placeholder 长文不再把行撑出容器 */
    grid-template-columns: minmax(0, 1fr) minmax(0, 1.4fr) auto;
    gap: 6px;
    align-items: center;
  }
  .hdr-x {
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    border: none;
    background: transparent;
    color: var(--faint);
    border-radius: 6px;
  }
  .hdr-x:hover {
    background: var(--bg-soft);
    color: #c0392b;
  }
  .hdr-x svg {
    width: 10px;
    height: 10px;
  }
  .hdr-add {
    align-self: flex-start;
    border: none;
    background: transparent;
    color: var(--accent);
    font-size: 11.5px;
    padding: 2px 0;
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
