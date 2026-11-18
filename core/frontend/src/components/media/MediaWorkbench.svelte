<script lang="ts">
  /* 多媒体工作台：左侧生成台（图片/语音/视频），右侧任务流。agent 工具与
     OpenAPI 提交的任务汇入同一份任务档案，这里统一展示进度与产物预览。 */

  import { onMount } from 'svelte'
  import { api, type GenQuotaMap, type MediaConfig, type MediaJob, type ModelEntry } from '../../lib/api'
  import { store } from '../../lib/store.svelte'

  let { media = null, onconfigure }: { media?: MediaConfig | null; onconfigure?: () => void } = $props()

  type Kind = 'image' | 'audio' | 'video'
  type Filter = 'all' | Kind

  const KINDS: { key: Kind; label: string; verb: string }[] = [
    { key: 'image', label: '图片', verb: '生成图片' },
    { key: 'audio', label: '语音', verb: '合成语音' },
    { key: 'video', label: '视频', verb: '生成视频' },
  ]

  let kind = $state<Kind>('image')
  let filter = $state<Filter>('all')

  let jobs = $state<MediaJob[]>([])
  let quota = $state<GenQuotaMap>({
    image: { kind: 'image', used: 0, limit: 20 },
    audio: { kind: 'audio', used: 0, limit: 20 },
    video: { kind: 'video', used: 0, limit: 3 },
  })
  let notice = $state('')
  let submitting = $state(false)
  let offline = $state(false)
  let now = $state(Date.now())

  /* ── 生成参数（各类型独立保留，切 tab 不丢） ── */
  let prompt = $state('')
  let negative = $state('')
  let size = $state('1024x1024')
  let batch = $state(1)
  let seed = $state<number | ''>('')
  let ttsText = $state('')
  let voice = $state('alex')
  let format = $state('mp3')
  let speed = $state(1)
  let vPrompt = $state('')
  let duration = $state(5)
  let resolution = $state('768p')
  let ratio = $state('16:9')
  let firstFrame = $state('')
  let lastFrame = $state('')

  let timer: ReturnType<typeof setInterval> | undefined

  const activeOf = (k: Kind): ModelEntry | undefined =>
    (media?.[k] ?? []).find((m) => m.enabled && !m.disabled)

  const activeModel = $derived(activeOf(kind))
  const filtered = $derived(filter === 'all' ? jobs : jobs.filter((j) => j.kind === filter))
  const running = $derived(jobs.filter((j) => j.status === 'queued' || j.status === 'running').length)
  const counts = $derived({
    all: jobs.length,
    image: jobs.filter((j) => j.kind === 'image').length,
    audio: jobs.filter((j) => j.kind === 'audio').length,
    video: jobs.filter((j) => j.kind === 'video').length,
  })

  async function refresh() {
    try {
      const [j, q] = await Promise.all([
        api.mediaJobs(100),
        api.mediaQuota().catch(() => null),
      ])
      jobs = j.jobs ?? []
      if (q) quota = q
      offline = false
    } catch {
      offline = true
    }
    now = Date.now()
  }

  onMount(() => {
    void refresh()
    timer = setInterval(() => void refresh(), 2000)
    return () => clearInterval(timer)
  })

  function kindLabel(k: string): string {
    return KINDS.find((x) => x.key === k)?.label ?? k
  }

  function statusLabel(s: string): string {
    return s === 'queued' ? '排队中' : s === 'running' ? '生成中' : s === 'done' ? '已完成' : s === 'failed' ? '失败' : s
  }

  async function submit() {
    const k = kind
    const model = activeOf(k)
    if (!model) {
      notice = `还没有启用${kindLabel(k)}生成模型，先去「模型与限额」配置。`
      return
    }
    if (k === 'image' && !prompt.trim()) {
      notice = '请先填写图片描述'
      return
    }
    if (k === 'audio' && !ttsText.trim()) {
      notice = '请先填写要合成的文本'
      return
    }
    if (k === 'video' && !vPrompt.trim()) {
      notice = '请先填写视频描述'
      return
    }
    notice = ''
    submitting = true
    try {
      let r
      if (k === 'image') {
        r = await api.genImage(prompt.trim(), {
          model: model.name,
          negative_prompt: negative.trim() || undefined,
          image_size: size,
          batch_size: Number(batch) || 1,
          seed: Number(seed) || 0,
          wait_seconds: 0,
        })
      } else if (k === 'audio') {
        r = await api.genSpeech(ttsText.trim(), {
          model: model.name,
          voice,
          response_format: format,
          speed: Number(speed) || 0,
          wait_seconds: 0,
        })
      } else {
        r = await api.genVideo(vPrompt.trim(), {
          model: model.name,
          duration: Number(duration) || 0,
          resolution,
          ratio,
          first_frame: firstFrame.trim() || undefined,
          last_frame: lastFrame.trim() || undefined,
          wait_seconds: 0,
        })
      }
      const job = r?.job
      if (job) jobs = [job, ...jobs.filter((j) => j.id !== job.id)]
      notice = '任务已提交，进度见右侧任务流。'
      window.setTimeout(() => void refresh(), 500)
    } catch (e) {
      notice = `提交失败：${e instanceof Error ? e.message : String(e)}`
    } finally {
      submitting = false
    }
  }

  /* 产物：一律走本机文件服务（cookie 鉴权），不用上游临时 URL */
  function fileUrl(path: string): string {
    return '/api/workspace/file?path=' + encodeURIComponent(path)
  }

  function openFile(path: string) {
    /* 抽屉只在对话/快应用页上屏：在多媒体页点“资源页”先切回工作区 */
    if (store.view !== 'chat' && store.view !== 'app') store.jumpView = 'chat'
    void store.openFileAt(path)
  }

  function insertToChat(path: string) {
    store.pendingFileRef = { path, items: [] }
    store.jumpView = 'chat'
  }

  async function copyPath(path: string) {
    try {
      await navigator.clipboard.writeText(path)
      notice = '路径已复制'
    } catch {
      notice = path
    }
  }

  function fmtSize(n?: number): string {
    if (!n) return ''
    if (n >= 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`
    return `${Math.max(1, Math.round(n / 1024))} KB`
  }

  function fmtTime(ms: number): string {
    const d = new Date(ms)
    return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
  }

  function fmtElapsed(j: MediaJob): string {
    const end = j.doneAt || now
    const s = Math.max(0, Math.round((end - j.createdAt) / 1000))
    if (s < 60) return `${s}s`
    return `${Math.floor(s / 60)}m${String(s % 60).padStart(2, '0')}s`
  }

  function quotaOf(k: Kind): { used: number; limit: number } {
    return quota[k] ?? { used: 0, limit: 0 }
  }
</script>

<div class="workbench">
  <section class="composer">
    <div class="kindtabs" role="tablist" aria-label="生成类型">
      {#each KINDS as k (k.key)}
        <button
          role="tab"
          aria-selected={kind === k.key}
          class:on={kind === k.key}
          onclick={() => {
            kind = k.key
            notice = ''
          }}>{k.label}</button
        >
      {/each}
    </div>

    {#if !activeModel}
      <div class="noconfig">
        <p>还没有启用的{kindLabel(kind)}生成模型</p>
        <button class="linkbtn" onclick={() => onconfigure?.()}>去「模型与限额」配置</button>
      </div>
    {:else}
      <div class="modelrow">
        <span class="chip">模型</span>
        <span class="modelname" title={activeModel.name}>{activeModel.name}</span>
        <span class="quota">今日 {quotaOf(kind).used}/{quotaOf(kind).limit}</span>
      </div>

      {#if kind === 'image'}
        <label class="field">
          <span>描述</span>
          <textarea
            bind:value={prompt}
            rows="4"
            placeholder="画面主体、风格、构图、光线…（越具体越稳）"
          ></textarea>
        </label>
        <label class="field">
          <span>反向提示词（可选）</span>
          <input type="text" bind:value={negative} placeholder="不想要的内容，如 模糊、低质量" />
        </label>
        <div class="row3">
          <label class="field">
            <span>尺寸</span>
            <select bind:value={size}>
              <option value="1024x1024">1024×1024</option>
              <option value="1024x768">1024×768</option>
              <option value="768x1024">768×1024</option>
              <option value="512x512">512×512</option>
            </select>
          </label>
          <label class="field">
            <span>张数</span>
            <select bind:value={batch}>
              <option value={1}>1</option>
              <option value={2}>2</option>
              <option value={3}>3</option>
              <option value={4}>4</option>
            </select>
          </label>
          <label class="field">
            <span>种子（可选）</span>
            <input type="number" bind:value={seed} placeholder="随机" />
          </label>
        </div>
      {:else if kind === 'audio'}
        <label class="field">
          <span>文本</span>
          <textarea bind:value={ttsText} rows="6" placeholder="要合成的文本（上限 5000 字符）"></textarea>
        </label>
        <div class="row3">
          <label class="field">
            <span>音色</span>
            <select bind:value={voice}>
              <option value="alex">alex（男）</option>
              <option value="benjamin">benjamin（男）</option>
              <option value="charles">charles（男）</option>
              <option value="david">david（男）</option>
              <option value="anna">anna（女）</option>
              <option value="bella">bella（女）</option>
              <option value="claire">claire（女）</option>
              <option value="diana">diana（女）</option>
            </select>
          </label>
          <label class="field">
            <span>格式</span>
            <select bind:value={format}>
              <option value="mp3">mp3</option>
              <option value="wav">wav</option>
              <option value="opus">opus</option>
              <option value="pcm">pcm</option>
            </select>
          </label>
          <label class="field">
            <span>语速</span>
            <input type="number" bind:value={speed} min="0.25" max="4" step="0.05" />
          </label>
        </div>
      {:else}
        <label class="field">
          <span>描述</span>
          <textarea
            bind:value={vPrompt}
            rows="4"
            placeholder="主体动作、场景、镜头、光线…（200 词以内更稳）"
          ></textarea>
        </label>
        <div class="row3">
          <label class="field">
            <span>时长（秒）</span>
            <select bind:value={duration}>
              <option value={5}>5</option>
              <option value={10}>10</option>
              <option value={15}>15</option>
            </select>
          </label>
          <label class="field">
            <span>分辨率</span>
            <select bind:value={resolution}>
              <option value="480p">480p（省）</option>
              <option value="768p">768p</option>
            </select>
          </label>
          <label class="field">
            <span>比例</span>
            <select bind:value={ratio}>
              <option value="16:9">16:9</option>
              <option value="9:16">9:16</option>
              <option value="1:1">1:1</option>
            </select>
          </label>
        </div>
        <div class="row2">
          <label class="field">
            <span>首帧（可选）</span>
            <input type="text" bind:value={firstFrame} placeholder="本机图片路径或 http URL" />
          </label>
          <label class="field">
            <span>尾帧（可选）</span>
            <input type="text" bind:value={lastFrame} placeholder="本机图片路径或 http URL" />
          </label>
        </div>
      {/if}

      <button class="genbtn" disabled={submitting} onclick={() => void submit()}>
        {submitting ? '提交中…' : KINDS.find((k) => k.key === kind)?.verb}
      </button>
    {/if}

    {#if notice}<p class="notice">{notice}</p>{/if}
    <p class="hint">agent 工具与 OpenAPI 提交的生成任务同样会出现在右侧任务流。</p>
  </section>

  <section class="stream">
    <header class="stream-head">
      <div class="filters">
        <button class:on={filter === 'all'} onclick={() => (filter = 'all')}>全部 <i>{counts.all}</i></button>
        {#each KINDS as k (k.key)}
          <button class:on={filter === k.key} onclick={() => (filter = k.key)}>{k.label} <i>{counts[k.key]}</i></button>
        {/each}
      </div>
      <span class="live" class:on={running > 0}>
        <i></i>{running ? `${running} 个任务进行中` : offline ? '后端不可达' : '任务流'}
      </span>
    </header>

    {#if !filtered.length}
      <div class="empty">
        <p>还没有生成任务</p>
        <p class="sub">在左侧提交一个，或让 AI 通过 media 工具发起。</p>
      </div>
    {:else}
      <div class="joblist">
        {#each filtered as j (j.id)}
          <article class="job" class:active={j.status === 'queued' || j.status === 'running'}>
            <div class="jhead">
              <span class="kindtag">{kindLabel(j.kind)}</span>
              <span class="status" class:queued={j.status === 'queued'} class:run={j.status === 'running'} class:done={j.status === 'done'} class:fail={j.status === 'failed'}>
                {statusLabel(j.status)}
              </span>
              <span class="jmeta" title={j.model}>{j.model}</span>
              <span class="jtime">{fmtTime(j.createdAt)} · {fmtElapsed(j)}</span>
            </div>
            <p class="brief">{j.brief}</p>
            {#if j.err || j.status === 'failed'}
              <p class="jerr" class:note={j.status === 'done'}>{j.err || '生成失败'}</p>
            {/if}
            {#if j.files?.length}
              <div class="files">
                {#each j.files as f (f.path)}
                  <div class="file">
                    {#if j.kind === 'image'}
                      <img src={fileUrl(f.path)} alt={f.name} loading="lazy" />
                    {:else if j.kind === 'audio'}
                      <audio controls preload="none" src={fileUrl(f.path)}></audio>
                    {:else if j.kind === 'video'}
                      <video controls preload="metadata" src={fileUrl(f.path)}></video>
                    {/if}
                    <div class="frow">
                      <span class="fname" title={f.path}>{f.name}</span>
                      <span class="fsize">{fmtSize(f.size)}</span>
                    </div>
                    <div class="factions">
                      <button onclick={() => openFile(f.path)} title="在资源查看器中打开">资源页</button>
                      <button onclick={() => insertToChat(f.path)} title="作为文件引用插入对话输入框">插入对话</button>
                      <button onclick={() => void copyPath(f.path)} title="复制绝对路径">复制路径</button>
                    </div>
                  </div>
                {/each}
              </div>
            {/if}
          </article>
        {/each}
      </div>
    {/if}
  </section>
</div>

<style>
  .workbench {
    display: grid;
    grid-template-columns: minmax(300px, 380px) minmax(0, 1fr);
    gap: 18px;
    align-items: start;
  }
  @media (max-width: 1080px) {
    .workbench {
      grid-template-columns: minmax(0, 1fr);
    }
  }
  .composer,
  .stream {
    border: 1px solid var(--line);
    border-radius: 14px;
    background: var(--card);
  }
  .composer {
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .kindtabs {
    display: flex;
    gap: 2px;
    padding: 2px;
    border: 1px solid var(--line);
    border-radius: 9px;
    background: var(--bg-soft);
    width: fit-content;
  }
  .kindtabs button {
    border: none;
    background: transparent;
    color: var(--muted);
    border-radius: 6px;
    padding: 4px 14px;
    font: inherit;
    font-size: var(--font-size-ui);
    cursor: pointer;
  }
  .kindtabs button.on {
    background: var(--bg);
    color: var(--fg);
    font-weight: 600;
    box-shadow: 0 1px 2px rgb(0 0 0 / 8%);
  }
  .modelrow {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }
  .chip {
    flex: none;
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .modelname {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    color: var(--fg);
  }
  .quota {
    flex: none;
    margin-left: auto;
    font-size: var(--font-size-small);
    color: var(--muted);
    font-variant-numeric: tabular-nums;
  }
  .noconfig {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 14px;
    border: 1px dashed var(--line-strong);
    border-radius: 10px;
    color: var(--muted);
    font-size: var(--font-size-ui);
  }
  .noconfig p {
    margin: 0;
  }
  .linkbtn {
    align-self: flex-start;
    border: none;
    background: transparent;
    color: var(--accent, #2563eb);
    font: inherit;
    font-size: var(--font-size-ui);
    padding: 0;
    cursor: pointer;
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
  textarea,
  input,
  select {
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
    resize: vertical;
  }
  textarea:focus,
  input:focus,
  select:focus {
    border-color: var(--accent, #2563eb);
  }
  .row2 {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 10px;
  }
  .row3 {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 10px;
  }
  .genbtn {
    border: none;
    border-radius: 9px;
    padding: 9px 14px;
    background: var(--accent, #2563eb);
    color: #fff;
    font: inherit;
    font-size: var(--font-size-section);
    font-weight: 600;
    cursor: pointer;
  }
  .genbtn:disabled {
    opacity: 0.55;
    cursor: default;
  }
  .notice {
    margin: 0;
    font-size: var(--font-size-small);
    color: var(--muted);
    line-height: 1.5;
  }
  .hint {
    margin: 0;
    font-size: var(--font-size-small);
    color: var(--faint);
    line-height: 1.5;
  }
  .stream {
    min-width: 0;
    display: flex;
    flex-direction: column;
  }
  .stream-head {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 12px;
    border-bottom: 1px solid var(--line);
    flex-wrap: wrap;
  }
  .filters {
    display: flex;
    gap: 4px;
  }
  .filters button {
    border: 1px solid transparent;
    background: transparent;
    color: var(--muted);
    border-radius: 7px;
    padding: 3px 9px;
    font: inherit;
    font-size: var(--font-size-ui);
    cursor: pointer;
  }
  .filters button:hover {
    background: var(--bg-soft);
  }
  .filters button.on {
    border-color: var(--line);
    background: var(--bg);
    color: var(--fg);
    font-weight: 600;
  }
  .filters i {
    font-style: normal;
    color: var(--faint);
    font-variant-numeric: tabular-nums;
  }
  .live {
    margin-left: auto;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: var(--font-size-small);
    color: var(--faint);
    white-space: nowrap;
  }
  .live i {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--faint);
  }
  .live.on {
    color: var(--accent, #2563eb);
  }
  .live.on i {
    background: var(--accent, #2563eb);
    animation: pulse 1.4s ease-in-out infinite;
  }
  @keyframes pulse {
    50% {
      opacity: 0.25;
    }
  }
  .empty {
    padding: 48px 20px;
    text-align: center;
    color: var(--faint);
  }
  .empty p {
    margin: 0;
  }
  .empty .sub {
    margin-top: 6px;
    font-size: var(--font-size-small);
  }
  .joblist {
    display: flex;
    flex-direction: column;
  }
  .job {
    padding: 12px 14px;
    border-bottom: 1px solid var(--line);
  }
  .job:last-child {
    border-bottom: none;
  }
  .jhead {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }
  .kindtag {
    flex: none;
    font-size: var(--font-size-small);
    font-weight: 600;
    color: var(--muted);
    padding: 1px 7px;
    border-radius: 6px;
    background: var(--bg-soft);
  }
  .status {
    flex: none;
    font-size: var(--font-size-small);
    font-weight: 600;
  }
  .status.queued {
    color: var(--faint);
  }
  .status.run {
    color: var(--accent, #2563eb);
  }
  .status.done {
    color: #18a058;
  }
  .status.fail {
    color: var(--danger, #c0392b);
  }
  .jmeta {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .jtime {
    flex: none;
    margin-left: auto;
    font-size: var(--font-size-small);
    color: var(--faint);
    font-variant-numeric: tabular-nums;
  }
  .brief {
    margin: 6px 0 0;
    font-size: var(--font-size-ui);
    color: var(--fg);
    line-height: 1.55;
    overflow-wrap: break-word;
  }
  .jerr {
    margin: 6px 0 0;
    font-size: var(--font-size-small);
    color: var(--danger, #c0392b);
    line-height: 1.5;
    overflow-wrap: break-word;
  }
  .jerr.note {
    color: var(--muted);
  }
  .files {
    margin-top: 10px;
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(170px, 1fr));
    gap: 10px;
  }
  .file {
    min-width: 0;
    border: 1px solid var(--line);
    border-radius: 9px;
    padding: 8px;
    background: var(--bg);
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .file img {
    width: 100%;
    max-height: 220px;
    object-fit: contain;
    border-radius: 6px;
    background: var(--bg-soft);
  }
  .file audio,
  .file video {
    width: 100%;
    border-radius: 6px;
  }
  .file video {
    max-height: 260px;
    background: #000;
  }
  .frow {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    font-size: var(--font-size-small);
  }
  .fname {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--muted);
    font-family: var(--font-mono);
  }
  .fsize {
    flex: none;
    margin-left: auto;
    color: var(--faint);
  }
  .factions {
    display: flex;
    gap: 4px;
    flex-wrap: wrap;
  }
  .factions button {
    border: 1px solid var(--line);
    background: transparent;
    color: var(--muted);
    border-radius: 6px;
    padding: 2px 7px;
    font: inherit;
    font-size: var(--font-size-small);
    cursor: pointer;
  }
  .factions button:hover {
    color: var(--fg);
    background: var(--bg-soft);
  }
</style>
