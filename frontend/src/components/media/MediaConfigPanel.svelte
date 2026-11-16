<script lang="ts">
  /* 多媒体·模型与限额：三类生成模型独立配置（media.json）+ 每日配额，变更自动保存 */
  /* 一类只启用一个：开某行开关即停用同类其余；关掉已启用的 = 整类能力关闭 */

  import { onMount } from 'svelte'
  import { api, type MediaConfig, type ModelEntry } from '../../lib/api'

  let { media = $bindable(null) }: { media?: MediaConfig | null } = $props()

  type Kind = 'image' | 'audio' | 'video'
  type HeaderPair = { key: string; value: string }

  const KINDS: {
    key: Kind
    title: string
    hint: string
    presets: { label: string; name: string }[]
  }[] = [
    {
      key: 'image',
      title: '图片生成',
      hint: '文生图。启用后 agent 经 media 工具（action=image）调用；按张计费，默认每次审批 + 每日配额。',
      presets: [
        { label: 'FLUX.1-schnell（快）', name: 'black-forest-labs/FLUX.1-schnell' },
        { label: 'Kolors', name: 'Kwai-Kolors/Kwai-Kolors' },
      ],
    },
    {
      key: 'audio',
      title: '语音合成',
      hint: '文字转语音。启用后 agent 经 media 工具（action=audio）调用；按输入计费，默认每次审批 + 每日配额。',
      presets: [{ label: 'CosyVoice2', name: 'FunAudioLLM/CosyVoice2-0.5B' }],
    },
    {
      key: 'video',
      title: '视频生成',
      hint: '文生视频。启用后 agent 经 media 工具（action=video）调用；数分钟出片，单价最高，默认每次审批 + 每日配额。',
      presets: [{ label: 'MiniMax-H3', name: 'MiniMaxAI/MiniMax-H3' }],
    },
  ]

  const SF_BASE = 'https://api.siliconflow.cn/v1'

  let saving = $state(false)
  let msg = $state('')

  let form = $state<{ kind: Kind; idx: number } | null>(null)
  let draft = $state({ name: '', baseUrl: SF_BASE, apiKey: '', pairs: [] as HeaderPair[] })

  /* 配额（挂在设置里，多媒体页统一管理） */
  let systemExtra = $state('')
  let qImg = $state<number | ''>('')
  let qAud = $state<number | ''>('')
  let qVid = $state<number | ''>('')
  let origQuota = $state('')
  let savingQuota = $state(false)
  let quotaMsg = $state('')
  const quotaDirty = $derived(origQuota !== '' && origQuota !== `${qImg}/${qAud}/${qVid}`)

  onMount(async () => {
    try {
      const st = await api.getSettings()
      systemExtra = st.systemExtra
      /* 0 = 用默认值：输入框直接显示生效值，别停在 0 */
      const eff = (v: number | undefined, def: number) => (v && v > 0 ? v : def)
      qImg = eff(st.genQuotaImage, 20)
      qAud = eff(st.genQuotaAudio, 20)
      qVid = eff(st.genQuotaVideo, 3)
      origQuota = `${qImg}/${qAud}/${qVid}`
    } catch {
      /* 配置页主体是模型；配额读取失败不阻塞 */
    }
  })

  function listOf(k: Kind): ModelEntry[] {
    return media?.[k] ?? []
  }

  function activeOf(k: Kind): ModelEntry | undefined {
    return listOf(k).find((m) => m.enabled && !m.disabled)
  }

  async function save(): Promise<void> {
    if (!media) return
    saving = true
    msg = ''
    try {
      await api.saveMedia(media)
      msg = '已保存，下一轮生效'
    } catch (e) {
      msg = `保存失败：${e instanceof Error ? e.message : String(e)}（agent 运行中请等本轮结束）`
      /* 乐观改动作废：拉回后端当前配置，别让生成台用着没生效的模型 */
      try {
        const m = await api.getMedia()
        media = { image: m.image ?? [], audio: m.audio ?? [], video: m.video ?? [] }
      } catch {
        /* 后端不可达：保持现状 */
      }
    } finally {
      saving = false
    }
  }

  function setList(k: Kind, list: ModelEntry[]): void {
    if (!media) return
    media[k] = list
  }

  /* 开关：打开 = 该类唯一启用；关闭已启用的开关 = 整类关闭 */
  function toggleKind(k: Kind, idx: number): void {
    const cur = listOf(k)
    if (!cur[idx]) return
    if (cur[idx].enabled && !cur[idx].disabled) {
      setList(k, cur.map((e) => ({ ...e, enabled: false })))
    } else {
      setList(k, cur.map((e, i) => ({ ...e, enabled: i === idx, disabled: i === idx ? false : e.disabled })))
    }
    void save()
  }

  function remove(k: Kind, idx: number): void {
    const cur = listOf(k)
    if (!cur[idx]) return
    setList(k, cur.filter((_, i) => i !== idx))
    void save()
  }

  function blankDraft(): void {
    draft = { name: '', baseUrl: SF_BASE, apiKey: '', pairs: [] }
  }

  function openAdd(k: Kind): void {
    blankDraft()
    form = { kind: k, idx: -1 }
  }

  function openEdit(k: Kind, idx: number): void {
    const m = listOf(k)[idx]
    if (!m) return
    draft = {
      name: m.name,
      baseUrl: m.baseUrl,
      apiKey: m.apiKey,
      pairs: Object.entries(m.headers ?? {}).map(([key, value]) => ({ key, value })),
    }
    form = { kind: k, idx }
  }

  function applyPreset(p: { label: string; name: string }): void {
    draft.name = p.name
    draft.baseUrl = SF_BASE
  }

  function submitForm(): void {
    if (!form) return
    const name = draft.name.trim()
    const baseUrl = draft.baseUrl.trim()
    if (!name || !baseUrl) {
      msg = '模型名与 API 端点不能为空'
      return
    }
    const headers: Record<string, string> = {}
    for (const p of draft.pairs) if (p.key.trim()) headers[p.key.trim()] = p.value
    const patch: Partial<ModelEntry> = {
      name,
      baseUrl,
      apiKey: draft.apiKey.trim(),
      headers: Object.keys(headers).length ? headers : undefined,
      vision: false,
      contextWindow: 0,
      protocol: 'openai',
    }
    const cur = listOf(form.kind)
    if (form.idx < 0) {
      setList(form.kind, [...cur, { ...patch, enabled: cur.length === 0 } as ModelEntry])
    } else {
      setList(
        form.kind,
        cur.map((e, i) => (i === form!.idx ? { ...e, ...patch } : e)),
      )
    }
    form = null
    void save()
  }

  function maskKey(k: string): string {
    if (!k) return '未配置 Key'
    return k.length > 8 ? `${k.slice(0, 6)}…${k.slice(-4)}` : '已配置'
  }

  async function saveQuota(): Promise<void> {
    savingQuota = true
    quotaMsg = ''
    try {
      const clamp = (v: number | '') => Math.min(Math.max(Number(v) || 0, 0), 1000)
      await api.saveSettings({
        systemExtra,
        genQuotaImage: clamp(qImg),
        genQuotaAudio: clamp(qAud),
        genQuotaVideo: clamp(qVid),
      })
      qImg = clamp(qImg)
      qAud = clamp(qAud)
      qVid = clamp(qVid)
      origQuota = `${qImg}/${qAud}/${qVid}`
      quotaMsg = '已保存（立即生效，agent 工具与 OpenAPI 共用同一计数）'
    } catch (e) {
      quotaMsg = `保存失败：${e instanceof Error ? e.message : String(e)}（agent 运行中请等本轮结束）`
    } finally {
      savingQuota = false
    }
  }
</script>

<div class="cfg">
  <header class="cfg-head">
    <p>三类生成模型独立配置（media.json），同一类只启用一个；关闭该类开关后 agent 调用会返回“当前无此能力”。</p>
    {#if saving}<span class="saving">保存中…</span>{/if}
  </header>
  {#if msg}<p class="msg">{msg}</p>{/if}

  {#each KINDS as k (k.key)}
    {@const active = activeOf(k.key)}
    <section class="kind">
      <header class="kind-head">
        <div class="kind-title">
          <h3>{k.title}</h3>
          <p>{k.hint}</p>
        </div>
        <span class="kstate" class:on={!!active}>{active ? `已启用 · ${active.name}` : '未启用'}</span>
      </header>

      <div class="rows">
        {#each listOf(k.key) as m, i (m.name + i)}
          <div class="row" class:on={m.enabled && !m.disabled} class:closed={m.disabled}>
            <button
              class="switch"
              class:on={m.enabled && !m.disabled}
              role="switch"
              aria-checked={m.enabled && !m.disabled}
              aria-label={`${m.name} 启用开关`}
              onclick={() => toggleKind(k.key, i)}
              title={m.enabled && !m.disabled ? '已启用（点击关闭该类能力）' : '点击启用（同类只启用一个）'}
            ><span></span></button>
            <div class="info">
              <span class="name">{m.name}</span>
              <span class="meta">{m.disabled ? '已关闭 · ' : ''}{m.baseUrl} · {maskKey(m.apiKey)}</span>
            </div>
            <div class="ops">
              <button onclick={() => openEdit(k.key, i)}>编辑</button>
              <button onclick={() => remove(k.key, i)}>删除</button>
            </div>
          </div>
        {/each}
        {#if !listOf(k.key).length}
          <div class="none">还没有配置模型</div>
        {/if}
        <button class="addrow" onclick={() => openAdd(k.key)}>+ 添加模型</button>
      </div>
    </section>
  {/each}

  <section class="quota">
    <header class="kind-head">
      <div class="kind-title">
        <h3>每日生成限额</h3>
        <p>agent 工具与 OpenAPI 共用同一份计数，跨日零点重置；0 = 默认（图片 20 / 语音 20 / 视频 3）。</p>
      </div>
    </header>
    <div class="qgrid">
      <label class="field">
        <span>图片（张/日）</span>
        <input type="number" bind:value={qImg} min="0" max="1000" />
      </label>
      <label class="field">
        <span>语音（次/日）</span>
        <input type="number" bind:value={qAud} min="0" max="1000" />
      </label>
      <label class="field">
        <span>视频（次/日）</span>
        <input type="number" bind:value={qVid} min="0" max="1000" />
      </label>
      <button class="primary" disabled={savingQuota || !quotaDirty} onclick={() => void saveQuota()}>
        {savingQuota ? '保存中…' : '保存限额'}
      </button>
    </div>
    {#if quotaMsg}<p class="msg">{quotaMsg}</p>{/if}
  </section>
</div>

{#if form}
  <div class="mask" onclick={() => (form = null)}></div>
  <div class="modal" role="dialog" aria-modal="true" aria-label={form.idx < 0 ? '添加生成模型' : '编辑生成模型'}>
    <header class="modal-head">
      <div>
        <h3>{form.idx < 0 ? '添加' : '编辑'}{KINDS.find((k) => k.key === form!.kind)?.title}模型</h3>
        <p>baseUrl 填 SiliconFlow 兼容端点（默认已填）；保存后下一轮生效。</p>
      </div>
      <button class="close" onclick={() => (form = null)} title="关闭">✕</button>
    </header>
    <div class="modal-body">
      {#if form.idx < 0}
        <div class="presets">
          <span>预设</span>
          {#each KINDS.find((k) => k.key === form!.kind)?.presets ?? [] as p (p.name)}
            <button type="button" class:on={draft.name === p.name} onclick={() => applyPreset(p)}>{p.label}</button>
          {/each}
        </div>
      {/if}
      <label class="field">
        <span>模型名</span>
        <input type="text" bind:value={draft.name} placeholder="如 black-forest-labs/FLUX.1-schnell" />
      </label>
      <label class="field">
        <span>API 端点</span>
        <input type="text" bind:value={draft.baseUrl} placeholder={SF_BASE} />
      </label>
      <label class="field">
        <span>API Key</span>
        <input type="password" bind:value={draft.apiKey} placeholder="sk-…" />
      </label>
      <div class="hdr">
        <div class="hdr-head">
          <span>请求头（可选）</span>
          <button type="button" onclick={() => (draft.pairs = [...draft.pairs, { key: '', value: '' }])}>+ 请求头</button>
        </div>
        {#each draft.pairs as p, j}
          <div class="hdr-row">
            <input type="text" placeholder="Header" bind:value={p.key} />
            <input type="text" placeholder="Value" bind:value={p.value} />
            <button type="button" class="x" onclick={() => (draft.pairs = draft.pairs.filter((_, n) => n !== j))} title="移除">✕</button>
          </div>
        {/each}
      </div>
    </div>
    <footer class="modal-foot">
      <button class="ghost" onclick={() => (form = null)}>取消</button>
      <button class="primary" onclick={submitForm}>{form.idx < 0 ? '添加' : '保存'}</button>
    </footer>
  </div>
{/if}

<style>
  .cfg {
    display: flex;
    flex-direction: column;
    gap: 0;
    max-width: 980px;
  }
  .cfg-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
    padding-bottom: 14px;
  }
  .cfg-head p,
  .kind-head p,
  .modal-head p {
    margin: 0;
    font-size: var(--font-size-small);
    color: var(--muted);
    line-height: 1.55;
  }
  .saving {
    flex: none;
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .msg {
    margin: 0 0 10px;
    font-size: var(--font-size-small);
    color: var(--muted);
  }
  /* 分区之间只用一条细线，不再卡片套卡片 */
  .kind,
  .quota {
    padding: 16px 0 18px;
    border-top: 1px solid var(--line);
  }
  .kind-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 14px;
    margin-bottom: 6px;
  }
  .kind-title {
    min-width: 0;
  }
  h3 {
    margin: 0 0 2px;
    font-size: var(--font-size-section);
    font-weight: 600;
  }
  .kstate {
    flex: none;
    max-width: 45%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--font-size-small);
    color: var(--faint);
    padding-top: 2px;
  }
  .kstate.on {
    color: var(--accent, #2563eb);
  }
  .rows {
    display: flex;
    flex-direction: column;
  }
  .row {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    align-items: center;
    gap: 12px;
    padding: 9px 0;
    border-bottom: 1px solid var(--line);
    min-width: 0;
  }
  .row.closed {
    opacity: 0.55;
  }
  /* 启用开关（比圆点直白：打开=该类唯一启用，关闭=整类能力关闭） */
  .switch {
    flex: none;
    width: 30px;
    height: 18px;
    padding: 0;
    border: none;
    border-radius: 9px;
    background: var(--line-strong);
    cursor: pointer;
    position: relative;
    transition: background var(--dur-fast) var(--ease-out);
  }
  .switch span {
    position: absolute;
    top: 2px;
    left: 2px;
    width: 14px;
    height: 14px;
    border-radius: 50%;
    background: #fff;
    box-shadow: 0 1px 2px rgb(0 0 0 / 18%);
    transition: transform var(--dur-fast) var(--ease-out);
  }
  .switch.on {
    background: var(--accent, #2563eb);
  }
  .switch.on span {
    transform: translateX(12px);
  }
  .info {
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .name {
    font-size: var(--font-size-ui);
    color: var(--fg);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .row.on .name {
    font-weight: 600;
  }
  .meta {
    font-size: var(--font-size-small);
    color: var(--faint);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .ops {
    flex: none;
    display: flex;
    gap: 4px;
  }
  .ops button {
    border: none;
    background: transparent;
    color: var(--muted);
    border-radius: 6px;
    padding: 3px 7px;
    font: inherit;
    font-size: var(--font-size-small);
    cursor: pointer;
  }
  .ops button:hover {
    color: var(--fg);
    background: var(--bg-soft);
  }
  .none {
    padding: 10px 0;
    font-size: var(--font-size-small);
    color: var(--faint);
    border-bottom: 1px solid var(--line);
  }
  .addrow {
    align-self: flex-start;
    border: none;
    background: transparent;
    color: var(--accent, #2563eb);
    font: inherit;
    font-size: var(--font-size-ui);
    padding: 10px 0 2px;
    cursor: pointer;
  }
  .qgrid {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 10px;
    align-items: end;
    margin-top: 10px;
  }
  @media (max-width: 900px) {
    .qgrid {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
  }
  .field > span {
    font-size: var(--font-size-small);
    color: var(--muted);
  }
  input {
    width: 100%;
    box-sizing: border-box;
    border: 1px solid var(--line);
    border-radius: 8px;
    background: var(--bg);
    color: var(--fg);
    font: inherit;
    font-size: var(--font-size-ui);
    padding: 7px 9px;
    outline: none;
  }
  input:focus {
    border-color: var(--accent, #2563eb);
  }
  .primary,
  .ghost {
    border: none;
    border-radius: 8px;
    padding: 7px 14px;
    font: inherit;
    font-size: var(--font-size-ui);
    cursor: pointer;
  }
  .primary {
    background: var(--accent, #2563eb);
    color: #fff;
  }
  .primary:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .ghost {
    background: transparent;
    border: 1px solid var(--line);
    color: var(--muted);
  }
  .mask {
    position: fixed;
    inset: 0;
    z-index: 40;
    background: rgb(0 0 0 / 28%);
  }
  .modal {
    position: fixed;
    z-index: 41;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    width: min(560px, calc(100vw - 40px));
    max-height: min(720px, calc(100vh - 60px));
    overflow: auto;
    background: var(--bg);
    border: 1px solid var(--line);
    border-radius: 14px;
    box-shadow: 0 24px 60px rgb(0 0 0 / 20%);
  }
  .modal-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
    padding: 16px 18px 10px;
  }
  .modal-head p {
    max-width: 460px;
  }
  .close {
    flex: none;
    border: none;
    background: transparent;
    color: var(--faint);
    font: inherit;
    cursor: pointer;
  }
  .modal-body {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 6px 18px 16px;
  }
  .presets {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .presets button {
    border: 1px solid var(--line);
    background: transparent;
    color: var(--muted);
    border-radius: 7px;
    padding: 3px 9px;
    font: inherit;
    font-size: var(--font-size-small);
    cursor: pointer;
  }
  .presets button.on {
    border-color: var(--accent, #2563eb);
    color: var(--accent, #2563eb);
  }
  .hdr {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .hdr-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    font-size: var(--font-size-small);
    color: var(--muted);
  }
  .hdr-head button {
    border: none;
    background: transparent;
    color: var(--accent, #2563eb);
    font: inherit;
    font-size: var(--font-size-small);
    cursor: pointer;
  }
  .hdr-row {
    display: grid;
    grid-template-columns: 1fr 1fr auto;
    gap: 6px;
  }
  .hdr-row .x {
    border: none;
    background: transparent;
    color: var(--faint);
    cursor: pointer;
  }
  .modal-foot {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    padding: 12px 18px;
    border-top: 1px solid var(--line);
  }
</style>
