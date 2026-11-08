<script lang="ts">
  import { onMount } from 'svelte'
  import { api, type ModelEntry, type ModelsConfig, type HourStats } from '../lib/api'
  import { store } from '../lib/store.svelte'
  import { theme } from '../lib/theme.svelte'
  import * as echarts from 'echarts/core'
  import { LineChart } from 'echarts/charts'
  import { GridComponent, LegendComponent, TooltipComponent } from 'echarts/components'
  import { CanvasRenderer } from 'echarts/renderers'
  import type { ECharts } from 'echarts/core'

  echarts.use([LineChart, GridComponent, LegendComponent, TooltipComponent, CanvasRenderer])

  /*
  「主模型 + 能力槽」三类：main（驱动 agent 循环的唯一对话模型，支持
  视觉则图片直接进上下文，否则不能读图）/ image（图片生成）/
  audio（声音生成）——能力槽配置并启用后向 agent 暴露对应
  工具（image/audio 的工具后续版本提供）。数据 GET/PUT /api/models，
  每槽至多启用一个，保存后重建 agent。
  */
  type SlotKey = keyof ModelsConfig
  const kinds: { key: SlotKey; title: string; hint: string }[] = [
    { key: 'main', title: '主模型', hint: '驱动对话与工具循环；勾选"支持视觉"则图片直接进上下文，未勾选则模型无法处理图片' },
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

  /* 添加/编辑共用同一个弹框与草稿：列表行内不再展开长表单 */
  type ModelForm = { mode: 'add'; slot: SlotKey } | { mode: 'edit'; slot: SlotKey; idx: number }
  let form = $state<ModelForm | null>(null)
  let formDraft = $state(blankDraft())
  const formSlotTitle = $derived(form ? (kinds.find((k) => k.key === form.slot)?.title ?? '') : '')

  /* 协议下拉：与输入条菜单同款（原生 select 的弹出菜单不跟主题，展开也有卡顿） */
  const PROTOCOLS = [
    { v: 'openai', label: 'OpenAI 兼容', hint: 'chat/completions 请求格式' },
    { v: 'responses', label: 'Responses', hint: 'OpenAI Responses 请求格式' },
    { v: 'anthropic', label: 'Anthropic', hint: 'Claude messages 请求格式' },
  ] as const
  let protocolOpen = $state(false)
  const protocolLabel = $derived(PROTOCOLS.find((p) => p.v === formDraft.protocol)?.label ?? formDraft.protocol)

  /* 上下文窗口快捷档（与现有 1M=1024000 的千进口径一致） */
  const CTX_PRESETS = [
    { label: '128K', v: 128000 },
    { label: '256K', v: 256000 },
    { label: '512K', v: 512000 },
    { label: '1M', v: 1024000 },
  ]

  /* 添加弹框的厂商预设：回填模型名/端点/窗口/视觉/协议，用户只需补 API Key */
  const PRESETS = [
    { label: '智谱 GLM', name: 'glm-5.3', baseUrl: 'https://open.bigmodel.cn/api/paas/v4', contextWindow: 1024000, vision: false, protocol: 'openai' },
    { label: 'DeepSeek 官方', name: 'deepseek-flash', baseUrl: 'https://api.deepseek.com/v1', contextWindow: 1024000, vision: true, protocol: 'openai' },
    { label: '硅基流动 DeepSeek', name: 'deepseek-ai/DeepSeek-V3.2', baseUrl: 'https://api.siliconflow.cn/v1', contextWindow: 128000, vision: false, protocol: 'openai' },
  ]
  let apiKeyEl: HTMLInputElement | undefined

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
    hourly = await api.getHourlyStats().catch(() => [])
    loaded = true
  })

  onMount(() => {
    const close = (e: MouseEvent) => {
      if (protocolOpen && !(e.target as HTMLElement).closest('.proto-pick')) protocolOpen = false
    }
    document.addEventListener('mousedown', close)
    return () => document.removeEventListener('mousedown', close)
  })

  /* 用量趋势（按小时分桶，近 30 天保留；范围 24h/7d/30d）。入口是页头
     常驻按钮，弹窗展示——没有数据时也能看到入口与空态。 */
  let hourly = $state<HourStats[]>([])
  let statsOpen = $state(false)
  const RANGES = [
    { key: '24h', label: '24 小时', hours: 24 },
    { key: '7d', label: '7 天', hours: 24 * 7 },
    { key: '30d', label: '30 天', hours: 24 * 30 },
  ] as const
  let range = $state<(typeof RANGES)[number]['key']>('24h')
  const rangeLabel = $derived(RANGES.find((r) => r.key === range)?.label ?? '')

  /* 以当前整点为终点生成连续小时槽，缺数据补 0（空闲小时折线如实归零） */
  const statsWindow = $derived.by(() => {
    const now = new Date()
    const end = new Date(now.getFullYear(), now.getMonth(), now.getDate(), now.getHours()).getTime()
    const hours = RANGES.find((r) => r.key === range)?.hours ?? 24
    const byStart = new Map(hourly.map((h) => [h.start, h]))
    const slots = []
    for (let i = hours - 1; i >= 0; i--) {
      const start = end - i * 3_600_000
      const h = byStart.get(start)
      slots.push({ start, in: h?.in ?? 0, out: h?.out ?? 0, cache: h?.cache ?? 0, turns: h?.turns ?? 0, models: h?.models })
    }
    return slots
  })
  const statsTotals = $derived.by(() => {
    let inTok = 0
    let outTok = 0
    let cacheTok = 0
    let turns = 0
    for (const h of statsWindow) {
      inTok += h.in
      outTok += h.out
      cacheTok += h.cache
      turns += h.turns
    }
    return { inTok, outTok, cacheTok, turns }
  })
  const PALETTE = ['#58a6ff', '#3fb950', '#d29a63', '#bc8cff', '#f85149', '#39c5cf', '#e3b341']

  /* 每模型一条折线 + 全部总量线；图例由 ECharts 接管显隐 */
  const trend = $derived.by(() => {
    if (!statsWindow.some((h) => h.in + h.out > 0)) return null
    const names = [...new Set(statsWindow.flatMap((h) => Object.keys(h.models ?? {})))]
    const rows = [
      { name: '全部', color: cssVar('--accent', '#2563eb'), width: 2.2, values: statsWindow.map((h) => h.in + h.out) },
      ...names.map((n, i) => ({
        name: n,
        color: PALETTE[(i + 1) % PALETTE.length],
        width: 1.5,
        values: statsWindow.map((h) => (h.models?.[n]?.in ?? 0) + (h.models?.[n]?.out ?? 0)),
      })),
    ]
    return {
      rows,
      totals: Object.fromEntries(rows.map((r) => [r.name, r.values.reduce((a, b) => a + b, 0)])),
    }
  })
  let chartEl = $state<HTMLDivElement>()
  let chart = $state<ECharts>()

  function cssVar(name: string, fallback: string): string {
    const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
    return v || fallback
  }

  const HTML_ESCAPES: Record<string, string> = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }
  const escapeHtml = (s: string) => s.replace(/[&<>"']/g, (c) => HTML_ESCAPES[c] ?? c)

  function buildOption(t: NonNullable<typeof trend>) {
    void theme.current // 读一下主题：切换时下面 cssVar 取到的已是新色，effect 依赖它整图重绘
    const line = cssVar('--line', '#e8eaf1')
    const faint = cssVar('--faint', '#a6acba')
    const muted = cssVar('--muted', '#626a78')
    const fg = cssVar('--fg', '#26272d')
    const font = cssVar('--font-ui', 'system-ui, sans-serif')
    return {
      animationDuration: 200,
      grid: { left: 52, right: 18, top: 36, bottom: 26 },
      /* 图例放右上：圆形色点 + 名称（数值用浅色富文本），与坐标轴标签分开 */
      legend: {
        type: 'scroll',
        top: 2,
        right: 4,
        icon: 'circle',
        itemWidth: 7,
        itemHeight: 7,
        itemGap: 16,
        textStyle: {
          color: muted,
          fontSize: 11,
          fontFamily: font,
          rich: { sum: { color: faint, fontSize: 10.5, padding: [0, 0, 0, 3] } },
        },
        formatter: (name: string) => `${name} {sum|${fmtTokens(t.totals[name] ?? 0)}}`,
      },
      tooltip: {
        trigger: 'axis',
        axisPointer: {
          type: 'line',
          lineStyle: { color: cssVar('--line-strong', '#43474f'), type: [4, 4] },
        },
        backgroundColor: cssVar('--bg', '#fbfcfe'),
        borderColor: line,
        borderWidth: 1,
        padding: 0,
        extraCssText: 'border-radius:10px;box-shadow:0 10px 28px rgb(0 0 0 / 12%);overflow:hidden;',
        textStyle: { color: fg, fontSize: 11.5, fontFamily: font },
        formatter: (params: unknown) => {
          const list = (Array.isArray(params) ? params : [params]) as {
            value?: [number, number]
            color?: string
            seriesName?: string
          }[]
          if (!list.length) return ''
          const d = new Date(list[0].value?.[0] ?? 0)
          const head = `${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')} ${String(d.getHours()).padStart(2, '0')}:00`
          const rows = list
            .map(
              (p) => `<div style="display:flex;align-items:center;gap:6px;padding:3px 10px">
                <span style="flex:none;width:6px;height:6px;border-radius:50%;background:${p.color ?? muted}"></span>
                <span style="flex:1;color:${muted}">${escapeHtml(p.seriesName ?? '')}</span>
                <b style="color:${fg};font-weight:600">${fmtTokens(p.value?.[1] ?? 0)}</b>
              </div>`,
            )
            .join('')
          return `<div style="padding:7px 0 8px;min-width:170px">
            <div style="padding:0 10px 5px;color:${faint};font-size:10.5px">${head}</div>${rows}</div>`
        },
      },
      xAxis: {
        type: 'time',
        axisLine: { lineStyle: { color: line } },
        axisTick: { show: false },
        axisLabel: { color: faint, fontSize: 10.5, fontFamily: font, hideOverlap: true },
        splitLine: { show: false },
      },
      yAxis: {
        type: 'value',
        splitNumber: 4,
        axisLabel: { color: faint, fontSize: 10.5, fontFamily: font, formatter: (v: number) => fmtTokens(v) },
        splitLine: { lineStyle: { color: line } },
      },
      series: t.rows.map((r) => ({
        name: r.name,
        type: 'line',
        showSymbol: false,
        smooth: false,
        emphasis: { focus: 'series' },
        lineStyle: { width: r.width, color: r.color },
        itemStyle: { color: r.color },
        data: statsWindow.map((h, i) => [h.start, r.values[i]]),
      })),
    }
  }

  /* 弹窗打开时初始化图表；关闭（元素消失）即销毁 */
  $effect(() => {
    if (!statsOpen || !chartEl) return
    const c = echarts.init(chartEl)
    chart = c
    return () => {
      c.dispose()
      if (chart === c) chart = undefined
    }
  })

  /* 数据/范围变化时整图重设（notMerge，避免旧序列残留） */
  $effect(() => {
    const t = trend
    const c = chart
    if (!c) return
    if (!t) {
      c.clear()
      return
    }
    c.setOption(buildOption(t), true)
    c.resize()
  })

  onMount(() => {
    const onResize = () => chart?.resize()
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  })

  /* persist 自动保存（所有变更即时提交，busy 时 409 提示等待本轮结束）。 */
  async function persist() {
    if (!cfg) return
    saving = true
    message = ''
    try {
      await api.saveModels(cfg)
      store.invalidateModels() // 输入框的模型切换器缓存失效，下次打开重新拉取
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
      cfg[slot] = cfg[slot].map((e, i) => ({ ...e, enabled: i === idx, disabled: i === idx ? false : e.disabled }))
    }
    void persist()
  }

  /* 关闭切换：关闭的模型保留配置但不可被调用（model_call/task/主循环都跳过） */
  function toggleClose(slot: SlotKey, idx: number) {
    if (!cfg) return
    cfg[slot] = cfg[slot].map((e, i) => (i === idx ? { ...e, disabled: !e.disabled, enabled: !e.disabled ? false : e.enabled } : e))
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

  function blankDraft() {
    return { name: '', baseUrl: '', apiKey: '', contextWindow: 128000, protocol: 'openai', vision: false, pairs: [] as HeaderPair[] }
  }

  function openAdd(slot: SlotKey) {
    formDraft = blankDraft()
    protocolOpen = false
    form = { mode: 'add', slot }
  }

  function applyPreset(p: (typeof PRESETS)[number]) {
    formDraft.name = p.name
    formDraft.baseUrl = p.baseUrl
    formDraft.contextWindow = p.contextWindow
    formDraft.vision = p.vision
    formDraft.protocol = p.protocol
    apiKeyEl?.focus()
  }

  function openEdit(slot: SlotKey, idx: number) {
    if (!cfg) return
    const m = cfg[slot][idx]
    formDraft = {
      name: m.name,
      baseUrl: m.baseUrl,
      apiKey: m.apiKey,
      contextWindow: m.contextWindow || 128000,
      protocol: m.protocol || 'openai',
      vision: !!m.vision,
      pairs: Object.entries(m.headers ?? {}).map(([key, value]) => ({ key, value })),
    }
    protocolOpen = false
    form = { mode: 'edit', slot, idx }
  }

  function submitForm() {
    if (!cfg || !form) return
    const name = formDraft.name.trim()
    const baseUrl = formDraft.baseUrl.trim()
    if (!name || !baseUrl) return
    const headers = pairsToRecord(formDraft.pairs)
    const entry = {
      name,
      baseUrl,
      apiKey: formDraft.apiKey.trim(),
      headers: Object.keys(headers).length ? headers : undefined,
      vision: formDraft.vision,
      contextWindow: Number(formDraft.contextWindow) || 0,
      protocol: formDraft.protocol || 'openai',
    }
    if (form.mode === 'add') {
      cfg[form.slot] = [...cfg[form.slot], { ...entry, enabled: cfg[form.slot].length === 0 }]
    } else {
      const { slot, idx } = form
      cfg[slot] = cfg[slot].map((e, i) => (i === idx ? { ...e, ...entry } : e))
    }
    form = null
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
    <div class="head-right">
      {#if saving}
        <span class="saving">保存中…</span>
      {/if}
      <button class="stats-btn" onclick={() => (statsOpen = true)} title="查看按小时用量趋势">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round">
          <path d="M4 19V5M4 19h16" />
          <path d="M8 16v-4M12 16V8M16 16v-6" />
        </svg>
        用量统计
      </button>
    </div>
  </div>
  {#if message}
    <p class="msg">{message}</p>
  {/if}

  {#if !loaded}
    <p class="lead">加载中…</p>
  {:else if cfg}
    {#each kinds as k (k.key)}
      <section>
        <h2 title={`${k.hint} · 每类启用一个。`}>{k.title}</h2>
        <div class="list">
          {#each cfg[k.key] as m, i (m.name + i)}
            <div class="model" class:enabled={m.enabled} class:closed={m.disabled}>
              <button class="dot" class:on={m.enabled && !m.disabled} onclick={() => enable(k.key, i)} title={m.enabled && !m.disabled ? (k.key === 'main' ? '已启用（主模型必启用）' : '已启用（点击停用）') : '点击启用'}></button>
              <div class="info">
                <span class="name">{m.name}</span>
                <span class="meta">{m.disabled ? '已关闭 · ' : ''}{m.protocol && m.protocol !== 'openai' ? `【${m.protocol}】` : ''}{m.baseUrl} · {maskKey(m.apiKey)}{m.contextWindow ? ` · ${fmtTokens(m.contextWindow)} ctx` : ''}{m.vision ? ' · 视觉' : ''}{Object.keys(m.headers ?? {}).length ? ` · ${Object.keys(m.headers ?? {}).length} 个请求头` : ''}</span>
              </div>
              <div class="usage">
                <span class="tokens" title="累计用量（输入/输出/缓存命中）">↑{fmtTokens(m.inTokens || 0)} ↓{fmtTokens(m.outTokens || 0)}{m.cacheTokens ? ` ⚡${fmtTokens(m.cacheTokens)}` : ''}</span>
                <button class="del" onclick={() => openEdit(k.key, i)} title="编辑">编辑</button>
                <button class="del" onclick={() => toggleClose(k.key, i)} title={m.disabled ? '重新开启（恢复可调用）' : '关闭（保留配置,不可被调用）'}>{m.disabled ? '开启' : '关闭'}</button>
                <button class="del" onclick={() => remove(k.key, i)} title="删除">删除</button>
              </div>
            </div>
          {/each}
          {#if cfg[k.key].length === 0}
            <div class="model none">暂无模型</div>
          {/if}
          <button class="add" onclick={() => openAdd(k.key)}>+ 添加模型</button>
        </div>
      </section>
    {/each}
  {/if}
</div>

{#if statsOpen}
  <div class="stats-mask" onclick={() => (statsOpen = false)}></div>
  <div class="stats-modal" role="dialog" aria-modal="true" aria-label="用量统计">
    <header class="stats-head">
      <div>
        <h3>用量统计</h3>
        <p class="stats-sub">最近 30 天按小时汇总输入/输出 tokens（输入含缓存命中），点图例显隐。</p>
      </div>
      <button class="stats-close" onclick={() => (statsOpen = false)} title="关闭">✕</button>
    </header>
    <div class="range-bar">
      {#each RANGES as r (r.key)}
        <button class="range-btn" class:active={range === r.key} onclick={() => (range = r.key)}>{r.label}</button>
      {/each}
    </div>
    <div class="stats-sum">
      <div class="stat"><span class="stat-k">输入</span><span class="stat-v">↑{fmtTokens(statsTotals.inTok)}</span></div>
      <div class="stat"><span class="stat-k">输出</span><span class="stat-v">↓{fmtTokens(statsTotals.outTok)}</span></div>
      <div class="stat"><span class="stat-k">缓存命中</span><span class="stat-v">⚡{fmtTokens(statsTotals.cacheTok)}</span></div>
      <div class="stat"><span class="stat-k">轮数</span><span class="stat-v">{statsTotals.turns}</span></div>
    </div>
    {#if trend}
      <div class="stats-chart" bind:this={chartEl}></div>
    {:else}
      <div class="stats-empty">
        <p>最近 {rangeLabel}还没有用量记录。</p>
        <p class="sub">发起几轮对话后，这里会自动按小时汇总。</p>
      </div>
    {/if}
  </div>
{/if}

<!-- 添加/编辑模型：同一弹框形态，列表不再内嵌长表单 -->
{#if form}
  <div class="form-mask" onclick={() => (form = null)}></div>
  <div class="form-modal" role="dialog" aria-modal="true" aria-label={form.mode === 'add' ? '添加模型' : '编辑模型'}>
    <header class="form-head">
      <div>
        <h3>{form.mode === 'add' ? '添加模型' : '编辑模型'}</h3>
        <p class="form-sub">{formSlotTitle} · 变更保存后下一轮生效</p>
      </div>
      <button class="form-close" onclick={() => (form = null)} title="关闭">✕</button>
    </header>
    <div class="form-body">
      {#if form.mode === 'add'}
        <div class="ctx-quick">
          <span class="cq-label">预设</span>
          {#each PRESETS as p (p.label)}
            <button type="button" class:active={formDraft.name === p.name && formDraft.baseUrl === p.baseUrl} onclick={() => applyPreset(p)}>{p.label}</button>
          {/each}
        </div>
      {/if}
      <div class="form-grid">
        <label class="fld">
          <span>模型名</span>
          <input type="text" placeholder="如 deepseek-ai/DeepSeek-V3.2" bind:value={formDraft.name} />
        </label>
        <div class="fld">
          <span>API 协议</span>
          <div class="proto-pick">
            <button type="button" class="proto-btn" onclick={() => (protocolOpen = !protocolOpen)}
              title="API 协议：决定请求格式（chat/completions / responses / Claude messages）">
              <span>{protocolLabel}</span>
              <svg class="chev" viewBox="0 0 10 6" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">
                <path d="M1 1l4 4 4-4" />
              </svg>
            </button>
            {#if protocolOpen}
              <div class="proto-menu">
                {#each PROTOCOLS as p (p.v)}
                  <button type="button" class="proto-item" class:on={formDraft.protocol === p.v}
                    onclick={() => { formDraft.protocol = p.v; protocolOpen = false }} title={p.hint}>{p.label}</button>
                {/each}
              </div>
            {/if}
          </div>
        </div>
      </div>
      <label class="fld">
        <span>API 端点</span>
        <input type="text" placeholder={defaultBase()} bind:value={formDraft.baseUrl} />
      </label>
      <label class="fld">
        <span>API Key</span>
        <input type="password" placeholder="sk-…" bind:this={apiKeyEl} bind:value={formDraft.apiKey} />
      </label>
      <label class="fld">
        <span>上下文窗口（tokens）</span>
        <input type="number" bind:value={formDraft.contextWindow} title="上下文窗口（tokens），水位与压缩按此计算；0 表示未知（按 128k 兜底）" />
      </label>
      <div class="ctx-quick">
        <span class="cq-label">快捷</span>
        {#each CTX_PRESETS as p (p.v)}
          <button type="button" class:active={Number(formDraft.contextWindow) === p.v} onclick={() => (formDraft.contextWindow = p.v)}>{p.label}</button>
        {/each}
      </div>
      <label class="vision-ck" title="勾选表示模型支持多模态视觉输入（可发图片）；未勾选时带图请求会自动省略图片，防止不支持视觉的模型报错卡死会话">
        <input type="checkbox" bind:checked={formDraft.vision} />
        <span>支持视觉（图片输入）</span>
      </label>
      <div class="hdr-list">
        <div class="hdr-head">
          <span>请求头</span>
          <button class="hdr-add" onclick={() => (formDraft.pairs = [...formDraft.pairs, { key: '', value: '' }])}>+ 请求头</button>
        </div>
        {#each formDraft.pairs as p, j}
          <div class="hdr-row">
            <input type="text" placeholder="Header（如 X-Org-Id）" bind:value={p.key} />
            <input type="text" placeholder="Value（如 org-123）" bind:value={p.value} />
            <button class="hdr-x" onclick={() => (formDraft.pairs = formDraft.pairs.filter((_, k2) => k2 !== j))} title="移除">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round">
                <path d="M6 6l12 12M18 6L6 18" />
              </svg>
            </button>
          </div>
        {/each}
      </div>
    </div>
    <footer class="form-actions">
      <button class="add-no" onclick={() => (form = null)}>取消</button>
      <button class="add-ok" disabled={!formDraft.name.trim() || !formDraft.baseUrl.trim()} onclick={submitForm}>
        {form.mode === 'add' ? '添加' : '保存'}
      </button>
    </footer>
  </div>
{/if}

<style>
  .page {
    flex: 1;
    min-height: 0;
    /* 全宽滚动容器（滚动条贴窗口右缘）；内容由直接子项各自限宽居中 */
    overflow-y: auto;
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
    font-size: var(--font-size-page);
    font-weight: 700;
  }
  .lead {
    font-size: var(--font-size-section);
    color: var(--muted);
    margin-top: 4px;
  }
  .msg {
    font-size: var(--font-size-ui);
    color: var(--muted);
    margin-top: -20px;
  }
  .head-right {
    flex: none;
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .saving {
    font-size: var(--font-size-ui);
    color: var(--faint);
  }
  .stats-btn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 5px 12px;
    border: 1px solid var(--line);
    border-radius: 8px;
    background: var(--bg);
    color: var(--muted);
    font: inherit;
    font-size: var(--font-size-ui);
    cursor: pointer;
    transition:
      border-color var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .stats-btn:hover {
    border-color: var(--line-strong);
    color: var(--fg);
  }
  .stats-btn svg {
    width: 14px;
    height: 14px;
  }
  section {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .range-bar {
    display: flex;
    gap: 6px;
  }
  .range-btn {
    border: 1px solid var(--line);
    background: transparent;
    color: var(--muted);
    font-size: var(--font-size-small);
    padding: 3px 12px;
    border-radius: 999px;
    cursor: pointer;
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out),
      border-color var(--dur-fast) var(--ease-out);
  }
  .range-btn:hover {
    background: var(--bg-soft);
    color: var(--fg);
  }
  .range-btn.active {
    border-color: var(--accent);
    color: var(--accent);
    background: color-mix(in srgb, var(--accent) 8%, transparent);
  }
  .stats-chart {
    width: 100%;
    height: 240px;
    border: 1px solid var(--line);
    border-radius: 12px;
    background: var(--bg-soft);
  }
  h2 {
    font-size: var(--font-size-section);
    font-weight: 600;
    color: var(--muted);
  }
  .list {
    display: flex;
    flex-direction: column;
    border: 1px solid var(--line);
    border-radius: 12px;
    overflow: hidden;
  }
  .model {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 11px 14px;
  }
  .model + .model,
  .model + .add {
    border-top: 1px solid var(--line);
  }
  .model.enabled {
    background: color-mix(in srgb, var(--accent) 5%, var(--bg));
  }
  .model.closed {
    opacity: 0.55;
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
    font-size: var(--font-size-section);
    color: var(--fg);
    overflow-wrap: anywhere;
  }
  .meta {
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
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
    font-size: var(--font-size-ui);
    color: var(--muted);
    white-space: nowrap;
  }
  .del {
    border: none;
    background: transparent;
    color: var(--faint);
    font-size: var(--font-size-ui);
    padding: 4px 8px;
    border-radius: 6px;
    opacity: 0;
    transition: opacity var(--dur-fast) var(--ease-out);
  }
  .model:hover .del {
    opacity: 1;
  }
  .del:hover {
    color: var(--danger);
    background: color-mix(in srgb, #c0392b 8%, transparent);
  }
  .none {
    font-size: var(--font-size-ui);
    color: var(--faint);
    padding: 16px 14px;
  }
  .add {
    border: none;
    background: transparent;
    padding: 10px 14px;
    text-align: left;
    font-size: var(--font-size-ui);
    color: var(--faint);
  }
  .add:hover {
    color: var(--fg);
  }
  .form-body input {
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 8px 10px;
    font-family: var(--font-mono);
    font-size: var(--font-size-ui);
    outline: none;
    background: var(--bg);
    color: var(--fg);
  }
  .form-body input:focus {
    border-color: var(--line-strong);
  }
  /* 视觉开关：checkbox 不吃表单输入框样式，横排一行 */
  .vision-ck {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: var(--font-size-ui);
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
  /* 协议下拉：外观与输入条菜单一致（自绘，不受 Windows 原生菜单干扰） */
  .proto-pick {
    position: relative;
  }
  .proto-btn {
    width: 100%;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 8px 10px;
    background: var(--bg);
    color: var(--fg);
    font-family: var(--font-mono);
    font-size: var(--font-size-ui);
    text-align: left;
    cursor: pointer;
  }
  .proto-btn:focus-visible {
    border-color: var(--line-strong);
    outline: none;
  }
  .proto-btn .chev {
    width: 10px;
    height: 6px;
    flex: none;
    color: var(--muted);
  }
  .proto-menu {
    position: absolute;
    top: calc(100% + 4px);
    left: 0;
    right: 0;
    z-index: 5;
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 5px;
    background: var(--bg);
    border: 1px solid var(--line-strong);
    border-radius: 12px;
    box-shadow: 0 10px 32px rgb(0 0 0 / 16%);
  }
  .proto-item {
    border: none;
    background: transparent;
    color: var(--muted);
    font-size: var(--font-size-small);
    font-family: var(--font-mono);
    text-align: left;
    padding: 7px 9px;
    border-radius: 8px;
    cursor: pointer;
  }
  .proto-item:hover {
    background: var(--bg-soft);
    color: var(--fg);
  }
  .proto-item.on {
    color: var(--accent);
    background: color-mix(in srgb, var(--accent) 8%, transparent);
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
    color: var(--danger);
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
    font-size: var(--font-size-ui);
    padding: 2px 0;
  }
  /* 上下文窗口快捷档：小 chip 一排，当前值高亮 */
  .ctx-quick {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .cq-label {
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .ctx-quick button {
    border: 1px solid var(--line);
    background: transparent;
    color: var(--muted);
    font-size: var(--font-size-small);
    font-family: var(--font-mono);
    padding: 2px 10px;
    border-radius: 999px;
    cursor: pointer;
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out),
      border-color var(--dur-fast) var(--ease-out);
  }
  .ctx-quick button:hover {
    background: var(--bg-soft);
    color: var(--fg);
  }
  .ctx-quick button.active {
    border-color: var(--accent);
    color: var(--accent);
    background: color-mix(in srgb, var(--accent) 8%, transparent);
  }
  .add-ok {
    border: 1px solid var(--line-strong);
    background: var(--bg-invert);
    color: var(--fg-invert);
    border-radius: 8px;
    padding: 6px 16px;
    font-size: var(--font-size-ui);
    font-weight: 500;
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
    font-size: var(--font-size-ui);
  }

  /* ── 添加/编辑模型弹框 ── */
  .form-mask {
    position: fixed;
    inset: 0;
    z-index: 60;
    background: rgb(0 0 0 / 18%);
  }
  .form-modal {
    position: fixed;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    z-index: 61;
    width: min(560px, 92vw);
    max-height: 86vh;
    display: flex;
    flex-direction: column;
    background: var(--bg);
    border: 1px solid var(--line-strong);
    border-radius: 14px;
    box-shadow: 0 12px 40px rgb(0 0 0 / 18%);
  }
  .form-head {
    flex: none;
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 16px;
    padding: 18px 20px 12px;
  }
  .form-head h3 {
    font-size: var(--font-size-body);
    font-weight: 700;
    color: var(--fg);
  }
  .form-sub {
    font-size: var(--font-size-small);
    color: var(--faint);
    margin-top: 2px;
  }
  .form-close {
    flex: none;
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    border: 1px solid var(--line);
    border-radius: 8px;
    background: transparent;
    color: var(--muted);
    font-size: var(--font-size-small);
    cursor: pointer;
  }
  .form-close:hover {
    border-color: var(--line-strong);
    color: var(--fg);
  }
  .form-body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 2px 20px 12px;
  }
  .form-grid {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 150px;
    gap: 10px;
  }
  @media (max-width: 520px) {
    .form-grid {
      grid-template-columns: 1fr;
    }
  }
  .fld {
    display: flex;
    flex-direction: column;
    gap: 5px;
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .hdr-list {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .hdr-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .form-actions {
    flex: none;
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    padding: 12px 20px 18px;
    border-top: 1px solid var(--line);
  }

  /* ── 用量统计弹窗 ── */
  .stats-mask {
    position: fixed;
    inset: 0;
    z-index: 60;
    background: rgb(0 0 0 / 18%);
  }
  .stats-modal {
    position: fixed;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    z-index: 61;
    width: min(720px, 92vw);
    max-height: 86vh;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 18px 20px 20px;
    background: var(--bg);
    border: 1px solid var(--line-strong);
    border-radius: 14px;
    box-shadow: 0 12px 40px rgb(0 0 0 / 18%);
  }
  .stats-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 16px;
  }
  .stats-head h3 {
    font-size: var(--font-size-body);
    font-weight: 700;
    color: var(--fg);
  }
  .stats-sub {
    font-size: var(--font-size-small);
    color: var(--faint);
    margin-top: 2px;
  }
  .stats-close {
    flex: none;
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    border: 1px solid var(--line);
    border-radius: 8px;
    background: transparent;
    color: var(--muted);
    font-size: var(--font-size-small);
    cursor: pointer;
  }
  .stats-close:hover {
    border-color: var(--line-strong);
    color: var(--fg);
  }
  .stats-sum {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }
  .stat {
    flex: 1 1 110px;
    display: flex;
    flex-direction: column;
    gap: 1px;
    padding: 8px 12px;
    border: 1px solid var(--line);
    border-radius: 10px;
    background: var(--bg-soft);
  }
  .stat-k {
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .stat-v {
    font-family: var(--font-mono);
    font-size: var(--font-size-section);
    font-weight: 600;
    color: var(--fg);
  }
  .stats-empty {
    padding: 30px 12px;
    border: 1px dashed var(--line);
    border-radius: 12px;
    text-align: center;
    font-size: var(--font-size-section);
    color: var(--muted);
  }
  .stats-empty .sub {
    margin-top: 4px;
    font-size: var(--font-size-small);
    color: var(--faint);
  }
</style>
