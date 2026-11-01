<script lang="ts">
  import { onMount } from 'svelte'
  import { api, type AppEntry, type CallbackRun, type CallbackSpec, type CallbackTask } from '../lib/api'
  import { store } from '../lib/store.svelte'

  /*
  回调中心：全局定时任务、触发记录与新建/编辑弹窗。任务三类——向当前
  会话发消息（可每次新建会话）、触发快应用（自动启动后端并调用
  app.quick 声明的端点），或在工作目录执行 shell 脚本。会话目标存创建
  时的叶 session，归档/compact 换代后任务失败，不继承到新会话。
  */

  type KindFilter = 'all' | 'session' | 'app' | 'script'
  type StatusFilter = 'all' | 'active' | 'paused' | 'error'
  type IntervalUnit = 's' | 'm' | 'h' | 'd'

  interface Form {
    title: string
    description: string
    kind: 'session' | 'app' | 'script'
    enabled: boolean
    scheduleKind: 'once' | 'interval' | 'cron'
    at: string
    intervalValue: string
    intervalUnit: IntervalUnit
    cron: string
    sessionMode: 'session' | 'new'
    message: string
    appName: string
    appCallback: string
    scriptCommand: string
  }

  interface RunRow extends CallbackRun {
    taskId: string
    taskTitle: string
    kind: 'session' | 'app' | 'script'
  }

  let tasks = $state<CallbackTask[]>([])
  let apps = $state<AppEntry[]>([])
  let loaded = $state(false)
  let toast = $state('')
  let query = $state('')
  let kindFilter = $state<KindFilter>('all')
  let statusFilter = $state<StatusFilter>('all')
  let modal = $state<'create' | 'edit' | null>(null)
  let editingId = $state('')
  let form = $state<Form>(emptyForm())
  let saving = $state(false)
  let formError = $state('')
  let delTarget = $state<CallbackTask | null>(null)
  let toastTimer: ReturnType<typeof setTimeout> | undefined

  const editingTask = $derived(editingId ? tasks.find((t) => t.id === editingId) : undefined)
  const sessionHintLabel = $derived(
    editingTask?.session?.sessionId
      ? sessionLabel(editingTask.session.sessionId)
      : currentSessionLabel(),
  )
  const selectedApp = $derived(apps.find((a) => a.name === form.appName))
  const selectedCallbacks = $derived(selectedApp?.callbacks ?? [])
  const selectedCallback = $derived(selectedCallbacks.find((c) => c.name === form.appCallback))

  const counts = $derived.by(() => {
    let session = 0
    let app = 0
    let script = 0
    let active = 0
    let paused = 0
    let error = 0
    for (const t of tasks) {
      if (t.kind === 'session') session++
      else if (t.kind === 'app') app++
      else script++
      if (t.enabled) active++
      else paused++
      if (t.lastStatus === 'error') error++
    }
    return { all: tasks.length, session, app, script, active, paused, error }
  })

  const visible = $derived.by(() => {
    const q = query.trim().toLowerCase()
    return tasks.filter((t) => {
      if (kindFilter !== 'all' && t.kind !== kindFilter) return false
      if (statusFilter === 'active' && !t.enabled) return false
      if (statusFilter === 'paused' && t.enabled) return false
      if (statusFilter === 'error' && t.lastStatus !== 'error') return false
      if (!q) return true
      const hay = [
        t.title,
        t.description ?? '',
        t.session?.sessionId ?? '',
        t.session?.message ?? '',
        t.app?.name ?? '',
        t.app?.callback ?? '',
        t.script?.command ?? '',
      ]
        .join(' ')
        .toLowerCase()
      return hay.includes(q)
    })
  })

  const records = $derived.by((): RunRow[] => {
    const ids = new Set(visible.map((t) => t.id))
    const out: RunRow[] = []
    for (const t of tasks) {
      if (!ids.has(t.id)) continue
      for (const r of t.runs ?? []) {
        out.push({ ...r, taskId: t.id, taskTitle: t.title, kind: t.kind })
      }
    }
    out.sort((a, b) => b.at - a.at)
    return out.slice(0, 30)
  })

  const stats = $derived.by(() => {
    const start = new Date()
    start.setHours(0, 0, 0, 0)
    let today = 0
    let failures = 0
    for (const t of tasks) {
      for (const r of t.runs ?? []) {
        if (r.at < start.getTime()) continue
        today++
        if (r.status === 'error') failures++
      }
    }
    return { today, failures }
  })

  function emptyForm(): Form {
    return {
      title: '',
      description: '',
      kind: 'session',
      enabled: true,
      scheduleKind: 'interval',
      at: '',
      intervalValue: '1',
      intervalUnit: 'h',
      cron: '',
      sessionMode: 'session',
      message: '',
      appName: '',
      appCallback: '',
      scriptCommand: '',
    }
  }

  /* currentSessionLabel：当前活动会话标题；没有会话时给空串。 */
  function currentSessionLabel(): string {
    const usable = (b: (typeof store.branches)[number]) => (b.msgs ?? 0) > 0 || !!b.running
    const active = store.branches.find((b) => b.id === store.activeId && usable(b))
    const first = active ?? store.branches.find(usable)
    return first ? first.title || '未命名会话' : ''
  }

  function notify(text: string) {
    toast = text
    clearTimeout(toastTimer)
    toastTimer = setTimeout(() => (toast = ''), 2600)
  }

  async function load(silent = false) {
    try {
      const [{ callbacks }, { apps: list }] = await Promise.all([
        api.listCallbacks(),
        api.getApps().catch(() => ({ apps: [] as AppEntry[] })),
      ])
      tasks = callbacks ?? []
      apps = list ?? []
    } catch (e) {
      if (!silent) notify(`加载失败：${(e as Error).message}`)
    } finally {
      loaded = true
    }
  }

  onMount(() => {
    void load()
    const timer = setInterval(() => void load(true), 5000)
    return () => {
      clearInterval(timer)
      clearTimeout(toastTimer)
    }
  })

  function openCreate() {
    form = emptyForm()
    editingId = ''
    formError = ''
    modal = 'create'
  }

  function startEdit(t: CallbackTask) {
    form = fromTask(t)
    editingId = t.id
    formError = ''
    modal = 'edit'
  }

  function fromTask(t: CallbackTask): Form {
    const f = emptyForm()
    f.title = t.title
    f.description = t.description ?? ''
    f.kind = t.kind
    f.enabled = t.enabled
    f.scheduleKind = t.schedule.kind
    if (t.schedule.kind === 'once') {
      f.at = toLocalInput(t.schedule.at)
    } else if (t.schedule.kind === 'interval') {
      const parsed = parseEvery(t.schedule.every)
      f.intervalValue = parsed.value
      f.intervalUnit = parsed.unit
    } else {
      f.cron = t.schedule.cron ?? ''
    }
    if (t.session) {
      f.sessionMode = t.session.mode === 'new' ? 'new' : 'session'
      f.message = t.session.message
    }
    if (t.app) {
      f.appName = t.app.name
      f.appCallback = t.app.callback
    }
    if (t.script) {
      f.scriptCommand = t.script.command
    }
    return f
  }

  /* 一次性时间回填成 datetime-local 需要的本地串。 */
  function toLocalInput(at?: string): string {
    if (!at) return ''
    if (/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}/.test(at)) return at.slice(0, 16)
    if (/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}/.test(at)) return at.slice(0, 16).replace(' ', 'T')
    const d = new Date(at)
    if (Number.isNaN(d.getTime())) return ''
    const pad = (n: number) => String(n).padStart(2, '0')
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
  }

  /* Go duration → 数字 + 单位；复合时长（1h30m）折算成能整除的最大单位。 */
  function parseEvery(every?: string): { value: string; unit: IntervalUnit } {
    if (!every) return { value: '1', unit: 'h' }
    const seconds: Record<string, number> = { d: 86400, h: 3600, m: 60, s: 1 }
    let total = 0
    let matched = false
    for (const m of every.matchAll(/(\d+(?:\.\d+)?)([smhd])/g)) {
      matched = true
      total += Number(m[1]) * seconds[m[2]]
    }
    if (!matched || !Number.isFinite(total) || total <= 0) return { value: '1', unit: 'h' }
    for (const unit of ['d', 'h', 'm', 's'] as const) {
      if (total >= seconds[unit] && total % seconds[unit] === 0) {
        return { value: String(total / seconds[unit]), unit }
      }
    }
    return { value: String(total), unit: 's' }
  }

  function appChanged(e: Event) {
    const name = (e.currentTarget as HTMLSelectElement).value
    const app = apps.find((a) => a.name === name)
    form.appCallback = app?.callbacks?.[0]?.name ?? ''
  }

  function setIntervalUnit(e: Event) {
    form.intervalUnit = (e.currentTarget as HTMLSelectElement).value as IntervalUnit
  }

  /* toSpec 校验表单并生成后端配置面；返回字符串表示错误提示。 */
  function toSpec(): CallbackSpec | string {
    const title = form.title.trim()
    if (!title) return '请填写标题'
    if (title.length > 120) return '标题不能超过 120 字'

    let schedule: CallbackSpec['schedule']
    if (form.scheduleKind === 'once') {
      if (!form.at) return '请选择一次性时间'
      const at = new Date(form.at)
      if (Number.isNaN(at.getTime()) || at.getTime() <= Date.now()) {
        return '一次性时间需要晚于当前时间'
      }
      schedule = { kind: 'once', at: form.at.replace('T', ' ') }
    } else if (form.scheduleKind === 'interval') {
      const raw = form.intervalValue.trim()
      if (!/^\d+(?:\.\d+)?$/.test(raw) || Number(raw) <= 0) return '请填写有效的时间数字'
      const seconds = { s: 1, m: 60, h: 3600, d: 86400 }[form.intervalUnit]
      const total = Number(raw) * seconds
      if (total < 10) return '固定间隔不能小于 10 秒'
      if (total > 365 * 86400) return '固定间隔不能超过 365 天'
      // Go duration 没有天单位：天在提交时折算成小时
      const every = form.intervalUnit === 'd' ? `${Number(raw) * 24}h` : `${raw}${form.intervalUnit}`
      schedule = { kind: 'interval', every }
    } else {
      const cron = form.cron.trim()
      if (!cron) return '请填写 cron 表达式'
      schedule = { kind: 'cron', cron }
    }

    const spec: CallbackSpec = {
      title,
      description: form.description.trim() || undefined,
      kind: form.kind,
      enabled: form.enabled,
      schedule,
    }
    if (form.kind === 'session') {
      const message = form.message.trim()
      if (!message) return '请填写要发送的消息'
      if (message.length > 8000) return '消息不能超过 8000 字'
      if (form.sessionMode === 'session' && !currentSessionLabel()) {
        return '当前没有可绑定的会话，请先打开一个会话，或选择新建会话'
      }
      spec.session = { mode: form.sessionMode, message }
      return spec
    }
    if (form.kind === 'app') {
      if (!form.appName) return '请选择快应用'
      if (!form.appCallback) return '请选择要调用的回调端点'
      if (!selectedCallbacks.some((c) => c.name === form.appCallback)) {
        return '所选端点已不在 app.quick 声明中，请重新选择'
      }
      spec.app = { name: form.appName, callback: form.appCallback }
      return spec
    }
    const command = form.scriptCommand.trim()
    if (!command) return '请填写要执行的命令'
    if (command.length > 8000) return '命令不能超过 8000 字'
    spec.script = { command }
    return spec
  }

  async function submit() {
    const spec = toSpec()
    if (typeof spec === 'string') {
      formError = spec
      return
    }
    const editing = modal === 'edit'
    saving = true
    try {
      if (editing) await api.updateCallback(editingId, spec)
      else await api.createCallback(spec)
      modal = null
      notify(editing ? '已保存' : '已创建')
      await load(true)
    } catch (e) {
      formError = `保存失败：${(e as Error).message}`
    } finally {
      saving = false
    }
  }

  async function toggle(t: CallbackTask) {
    try {
      const r = await api.setCallbackEnabled(t.id, !t.enabled)
      notify(r.callback.enabled ? `已启用「${t.title}」` : `已停用「${t.title}」`)
      await load(true)
    } catch (e) {
      notify(`操作失败：${(e as Error).message}`)
    }
  }

  async function runNow(t: CallbackTask) {
    try {
      await api.runCallback(t.id)
      notify(`已触发「${t.title}」，后台执行中`)
      setTimeout(() => void load(true), 900)
    } catch (e) {
      notify(`触发失败：${(e as Error).message}`)
    }
  }

  async function doDelete() {
    const t = delTarget
    delTarget = null
    if (!t) return
    try {
      await api.deleteCallback(t.id)
      notify(`已删除「${t.title}」`)
      await load(true)
    } catch (e) {
      notify(`删除失败：${(e as Error).message}`)
    }
  }

  /* ── 展示辅助 ── */
  function kindLabel(kind: string): string {
    if (kind === 'session') return '会话'
    if (kind === 'app') return '快应用'
    return '脚本'
  }

  function scheduleText(t: CallbackTask): string {
    const s = t.schedule
    if (s.kind === 'once') return `一次性 ${s.at ?? ''}`
    if (s.kind === 'interval') return everyText(s.every)
    return `cron ${s.cron ?? ''}`
  }

  function everyText(every?: string): string {
    if (!every) return '固定间隔'
    const parsed = parseEvery(every)
    const labels: Record<IntervalUnit, string> = { s: '秒', m: '分钟', h: '小时', d: '天' }
    return `每 ${parsed.value} ${labels[parsed.unit]}`
  }

  function sessionLabel(id?: string): string {
    if (!id) return '-'
    return (store.branches.find((b) => (b.leafId || b.id) === id)?.title || id)
  }

  function appTitle(name?: string): string {
    if (!name) return '-'
    return apps.find((a) => a.name === name)?.title || name
  }

  function shortId(id?: string): string {
    if (!id) return ''
    return id.length > 20 ? `${id.slice(0, 10)}…${id.slice(-4)}` : id
  }

  function targetText(t: CallbackTask): string {
    if (t.kind === 'session') {
      if (!t.session) return '无目标'
      if (t.session.mode === 'new') return '每次新建会话'
      if (!t.session.sessionId) return '未绑定会话（请重新创建）'
      return `${sessionLabel(t.session.sessionId)} · ${shortId(t.session.sessionId)}`
    }
    if (t.kind === 'script') {
      return t.script ? `脚本 · ${t.script.command}` : '无目标'
    }
    if (!t.app) return '无目标'
    return `${appTitle(t.app.name)} · ${t.app.callback}`
  }

  function runTarget(r: RunRow): string {
    if (r.kind === 'app') {
      const cb = (r.target ?? '').split(' · ').slice(1).join(' · ')
      return `${appTitle(r.targetId)}${cb ? ` · ${cb}` : ''}`
    }
    if (r.kind === 'script') return r.content || '脚本'
    if ((r.target ?? '').startsWith('新建会话')) {
      return r.target + (r.targetId ? ` · ${shortId(r.targetId)}` : '')
    }
    if (r.targetId) return `${sessionLabel(r.targetId)} · ${shortId(r.targetId)}`
    return r.target || '会话'
  }

  function fmtDateTime(ts?: number): string {
    if (!ts) return '-'
    const d = new Date(ts)
    const pad = (n: number) => String(n).padStart(2, '0')
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
  }

  function nextText(ts?: number): string {
    if (!ts) return '-'
    const d = new Date(ts)
    const now = new Date()
    const pad = (n: number) => String(n).padStart(2, '0')
    const dayStart = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime()
    const t = d.getTime()
    if (t >= dayStart && t < dayStart + 86400000) return `今天 ${pad(d.getHours())}:${pad(d.getMinutes())}`
    if (t >= dayStart + 86400000 && t < dayStart + 2 * 86400000) {
      return `明天 ${pad(d.getHours())}:${pad(d.getMinutes())}`
    }
    const date = `${d.getMonth() + 1}-${d.getDate()}`
    return `${d.getFullYear() === now.getFullYear() ? date : `${d.getFullYear()}-${date}`} ${pad(d.getHours())}:${pad(d.getMinutes())}`
  }

  function recordTime(ts: number): string {
    const d = new Date(ts)
    const now = new Date()
    const pad = (n: number) => String(n).padStart(2, '0')
    const sameDay =
      d.getFullYear() === now.getFullYear() && d.getMonth() === now.getMonth() && d.getDate() === now.getDate()
    return sameDay
      ? `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
      : `${d.getMonth() + 1}-${d.getDate()} ${pad(d.getHours())}:${pad(d.getMinutes())}`
  }

  function statusText(status?: string): string {
    switch (status) {
      case 'ok':
        return '成功'
      case 'error':
        return '失败'
      case 'skipped':
        return '跳过'
      case 'missed':
        return '错过'
      case 'running':
        return '执行中'
      default:
        return status || '-'
    }
  }
</script>

<div class="page">
  <div class="head">
    <div>
      <h1>回调</h1>
      <p class="lead">
        ezharness 的定时回调中心：到点向当前会话发消息（运行中先打断，归档/换代后失败）、自动启动快应用并调用 app.quick 声明的端点，或在工作目录执行脚本。
      </p>
    </div>
    <button class="newbtn" onclick={openCreate}>
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
        <path d="M12 5v14M5 12h14" />
      </svg>
      新建回调
    </button>
  </div>

  <div class="overview">
    <div class="stat">
      <span>任务</span>
      <b>{counts.all}</b>
      <small>{counts.session} 会话 · {counts.app} 快应用 · {counts.script} 脚本</small>
    </div>
    <div class="stat">
      <span>启用中</span>
      <b>{counts.active}</b>
      <small>{counts.paused} 已停用</small>
    </div>
    <div class="stat">
      <span>今日触发</span>
      <b>{stats.today}</b>
      <small>按本机时区统计</small>
    </div>
    <div class="stat">
      <span>今日失败</span>
      <b class:bad={stats.failures > 0}>{stats.failures}</b>
      <small>按触发记录统计</small>
    </div>
  </div>

  <div class="toolbar">
    <label class="search">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round">
        <circle cx="11" cy="11" r="7" />
        <path d="M16 16l4 4" />
      </svg>
      <input bind:value={query} placeholder="搜索标题、描述或目标" />
    </label>
    <label class="select">
      <select bind:value={kindFilter}>
        <option value="all">全部类型（{counts.all}）</option>
        <option value="session">会话（{counts.session}）</option>
        <option value="app">快应用（{counts.app}）</option>
        <option value="script">脚本（{counts.script}）</option>
      </select>
    </label>
    <label class="select">
      <select bind:value={statusFilter}>
        <option value="all">全部状态</option>
        <option value="active">启用中（{counts.active}）</option>
        <option value="paused">已停用（{counts.paused}）</option>
        <option value="error">上次失败（{counts.error}）</option>
      </select>
    </label>
    <span class="shown">{visible.length} 个任务</span>
  </div>

  <div class="list">
    {#if !loaded}
      <p class="lead">加载中…</p>
    {:else if visible.length}
      {#each visible as t (t.id)}
        {@const last = t.runs?.[0]}
        <article class="task" class:off={!t.enabled}>
          <div class="rowtop">
            <div class="titlewrap">
              <span class="kind" class:app={t.kind === 'app'} class:script={t.kind === 'script'}>{kindLabel(t.kind)}</span>
              <h2>{t.title}</h2>
              <span class="state" class:off={!t.enabled}>{t.enabled ? '启用中' : '已停用'}</span>
            </div>
            <div class="actions">
              <label class="switch" title={t.enabled ? '停用' : '启用'}>
                <input type="checkbox" checked={t.enabled} onchange={() => void toggle(t)} />
                <span class="slider"></span>
              </label>
              <button class="iconbtn" disabled={t.lastStatus === 'running'} onclick={() => void runNow(t)} title="立即运行（不改变计划）">
                <svg viewBox="0 0 24 24" fill="currentColor"><path d="M8 5.5v13l11-6.5z" /></svg>
              </button>
              <button class="iconbtn" onclick={() => startEdit(t)} title="编辑">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M4 20h4L19 9l-4-4L4 16v4z" />
                  <path d="M13.5 6.5l4 4" />
                </svg>
              </button>
              <button class="iconbtn danger" onclick={() => (delTarget = t)} title="删除">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round">
                  <path d="M6 6l12 12M18 6L6 18" />
                </svg>
              </button>
            </div>
          </div>
          {#if t.description}<p class="desc">{t.description}</p>{/if}
          <div class="meta">
            <span class="pill">⏱ {scheduleText(t)}</span>
            <span class="pill" title={t.kind === 'session' ? t.session?.sessionId : t.kind === 'script' ? t.script?.command : t.app?.name}>→ {targetText(t)}</span>
            {#if t.enabled && t.nextRunAt}<span class="pill next" title={fmtDateTime(t.nextRunAt)}>下次 {nextText(t.nextRunAt)}</span>{/if}
            {#if t.runCount}<span class="pill muted">已触发 {t.runCount} 次</span>{/if}
            {#if t.lastStatus}
              <span class="pill" class:err={t.lastStatus === 'error'} class:skip={t.lastStatus === 'skipped' || t.lastStatus === 'missed'}>
                上次 {statusText(t.lastStatus)}
              </span>
            {/if}
          </div>
          {#if last}
            <div class="lastrun">
              <i class="dot" class:ok={last.status === 'ok'} class:err={last.status === 'error'} class:skip={last.status === 'skipped' || last.status === 'missed'}></i>
              <span class="lr-time" title={fmtDateTime(last.at)}>{recordTime(last.at)}</span>
              <span class="lr-status">{statusText(last.status)}</span>
              <span class="lr-text" title={last.result || last.content || ''}>
                {last.result || last.content || '—'}
              </span>
            </div>
          {:else if t.lastStatus === 'running'}
            <p class="hint small">执行中…</p>
          {:else if t.lastResult}
            <p class="hint small">{t.lastResult}</p>
          {:else}
            <p class="hint small">尚未触发</p>
          {/if}
        </article>
      {/each}
    {:else if tasks.length}
      <div class="empty">
        <p>没有匹配的任务。</p>
        <p class="hint">换个搜索词，或调整类型/状态筛选。</p>
      </div>
    {:else}
      <div class="empty">
        <p>暂无定时回调。</p>
        <p class="hint">点右上角「新建回调」，或让 agent 用 callback 工具创建。</p>
      </div>
    {/if}
  </div>

  <section class="records">
    <div class="sechead">
      <h2>回调记录</h2>
      <span>{records.length ? `最近 ${records.length} 条` : '暂无'}</span>
    </div>
    {#if records.length}
      <div class="rtable">
        <div class="rhead">
          <span>时间</span>
          <span>任务</span>
          <span>目标</span>
          <span>触发内容</span>
          <span>结果</span>
        </div>
        {#each records as r, i (`${r.taskId}:${r.at}:${i}`)}
          <div class="rrow">
            <span class="rtime" title={`${fmtDateTime(r.at)}${r.durationMs ? ` · ${r.durationMs} ms` : ''}`}>{recordTime(r.at)}</span>
            <span class="rtask">
              <i class="dot" class:ok={r.status === 'ok'} class:err={r.status === 'error'} class:skip={r.status === 'skipped' || r.status === 'missed'}></i>
              <span class="rname" title={r.taskTitle}>{r.taskTitle}</span>
              <em class="rkind" class:app={r.kind === 'app'} class:script={r.kind === 'script'}>{kindLabel(r.kind)}</em>
              {#if r.manual}<em class="rkind manual">手动</em>{/if}
            </span>
            <span class="rtarget" title={runTarget(r)}>{runTarget(r)}</span>
            <span class="rcontent" title={r.content || ''}>{r.content || '—'}</span>
            <span class="rresult" class:err={r.status === 'error'} title={r.result || ''}>
              {statusText(r.status)}{#if r.result} · {r.result}{/if}
            </span>
          </div>
        {/each}
      </div>
    {:else}
      <p class="hint">当前筛选下还没有触发记录。清空筛选，或等任务到点 / 点「立即运行」后再看。</p>
    {/if}
  </section>
</div>

{#if modal}
  <div class="mask" onclick={() => (modal = null)}></div>
  <div class="modal" role="dialog" aria-label={modal === 'edit' ? '编辑回调' : '新建回调'}>
    <header>
      <h2>{modal === 'edit' ? '编辑回调' : '新建回调'}</h2>
      <button class="close" onclick={() => (modal = null)} title="关闭">✕</button>
    </header>

    <div class="form">
      <div class="fields2">
        <label class="field">
          <span>标题</span>
          <input bind:value={form.title} placeholder="如：每日早报" />
        </label>
        <label class="field">
          <span>描述（可选）</span>
          <input bind:value={form.description} placeholder="一句话说明这次回调做什么" />
        </label>
      </div>

      <div class="section-label">触发目标</div>
      <div class="seg">
        <button class:on={form.kind === 'session'} onclick={() => (form.kind = 'session')}>会话</button>
        <button class:on={form.kind === 'app'} onclick={() => (form.kind = 'app')}>快应用</button>
        <button class:on={form.kind === 'script'} onclick={() => (form.kind = 'script')}>脚本</button>
      </div>

      {#if form.kind === 'session'}
        <div class="seg sub">
          <button class:on={form.sessionMode === 'session'} onclick={() => (form.sessionMode = 'session')}>当前会话</button>
          <button class:on={form.sessionMode === 'new'} onclick={() => (form.sessionMode = 'new')}>新建会话</button>
        </div>
        {#if form.sessionMode === 'session'}
          <p class="hint">
            创建时绑定当前会话{#if sessionHintLabel}「{sessionHintLabel}」{/if}。到点若它正在跑轮，先打断当前轮再发送；会话归档或 compact 换代后任务失败，不会跟随新会话。
          </p>
        {:else}
          <p class="hint">每次触发都会新建一条会话再发送消息；不受归档换代影响。</p>
        {/if}
        <label class="field">
          <span>消息</span>
          <textarea bind:value={form.message} rows="4" placeholder="到点后作为用户消息发送给该会话"></textarea>
        </label>
      {:else if form.kind === 'app'}
        <label class="field">
          <span>快应用</span>
          <select bind:value={form.appName} onchange={appChanged}>
            <option value="">选择快应用…</option>
            {#each apps as a (a.name)}
              <option value={a.name}>{a.title}（{a.name}）</option>
            {/each}
          </select>
        </label>
        <div class="field">
          <span>回调端点</span>
          {#if selectedCallbacks.length}
            <div class="endpointlist">
              {#each selectedCallbacks as c (c.name)}
                <button class="endpoint" class:sel={form.appCallback === c.name} onclick={() => (form.appCallback = c.name)}>
                  <span class="ep-top">
                    <b>{c.name}</b>
                    {#if c.title}<span class="ep-title">{c.title}</span>{/if}
                    <code>{c.method}</code>
                  </span>
                  <span class="ep-url" title={c.url}>{c.url}</span>
                  {#if c.description}<span class="ep-desc">{c.description}</span>{/if}
                </button>
              {/each}
            </div>
            {#if selectedCallback}
              <p class="hint">{selectedCallback.description || '触发时自动启动应用后端（若声明了 backend），再调用这个端点。'}</p>
            {/if}
          {:else if form.appName}
            <p class="hint">该应用没有在 app.quick 里声明 callbacks。让 agent 补声明后再建任务。</p>
          {:else}
            <p class="hint">先选择快应用；端点由 app.quick 的 callbacks 声明提供。</p>
          {/if}
        </div>
      {:else}
        <label class="field">
          <span>命令</span>
          <textarea bind:value={form.scriptCommand} rows="3" placeholder="./sync.sh 或 python tools/sync.py"></textarea>
        </label>
        <p class="hint">在本机工作目录执行，捕获输出尾部与退出码进触发记录；单次最长 10 分钟，超时会结束进程。</p>
      {/if}

      <div class="section-label">计划</div>
      <div class="seg">
        <button class:on={form.scheduleKind === 'once'} onclick={() => (form.scheduleKind = 'once')}>单次</button>
        <button class:on={form.scheduleKind === 'interval'} onclick={() => (form.scheduleKind = 'interval')}>固定间隔</button>
        <button class:on={form.scheduleKind === 'cron'} onclick={() => (form.scheduleKind = 'cron')}>cron</button>
      </div>
      {#if form.scheduleKind === 'once'}
        <label class="field">
          <span>时间</span>
          <input type="datetime-local" bind:value={form.at} />
        </label>
        <p class="hint">执行一次后自动停用；停机错过会标记为「错过」。</p>
      {:else if form.scheduleKind === 'interval'}
        <div class="field">
          <span>固定间隔</span>
          <div class="interval">
            <input
              type="number"
              min="1"
              step="1"
              value={form.intervalValue}
              oninput={(e) => (form.intervalValue = (e.currentTarget as HTMLInputElement).value)}
              placeholder="30"
            />
            <select value={form.intervalUnit} onchange={setIntervalUnit}>
              <option value="s">秒</option>
              <option value="m">分钟</option>
              <option value="h">小时</option>
              <option value="d">天</option>
            </select>
          </div>
        </div>
        <p class="hint">最短 10 秒；执行完从完成时刻重新计时。</p>
      {:else}
        <label class="field">
          <span>cron（分 时 日 月 周）</span>
          <input bind:value={form.cron} placeholder="0 9 * * *" />
        </label>
        <p class="hint">例：0 9 * * * 每天 09:00 · */30 * * * * 每 30 分钟 · 0 9 * * 1 每周一 09:00</p>
      {/if}

      <label class="check">
        <input type="checkbox" bind:checked={form.enabled} />
        {modal === 'create' ? '创建后启用' : '启用'}
      </label>
    </div>

    <footer>
      <button class="btn" onclick={() => (modal = null)}>取消</button>
      <button class="btn primary" disabled={saving} onclick={() => void submit()}>{saving ? '保存中…' : '保存'}</button>
    </footer>
  </div>
{/if}

{#if formError}
  <div class="mask top" onclick={() => (formError = '')}></div>
  <div class="modal small alert" role="alertdialog" aria-label="无法保存">
    <header>
      <h2>无法保存</h2>
      <button class="close" onclick={() => (formError = '')} title="关闭">✕</button>
    </header>
    <p class="alerttext">{formError}</p>
    <footer>
      <button class="btn primary" onclick={() => (formError = '')}>知道了</button>
    </footer>
  </div>
{/if}

{#if delTarget}
  <div class="mask" onclick={() => (delTarget = null)}></div>
  <div class="modal small confirm" role="dialog" aria-label="删除回调">
    <header><h2>删除回调</h2></header>
    <p class="confirmtext">确定删除「{delTarget.title}」？任务与触发记录会一并删除，不可恢复。</p>
    <footer>
      <button class="btn" onclick={() => (delTarget = null)}>取消</button>
      <button class="btn danger" onclick={() => void doDelete()}>删除</button>
    </footer>
  </div>
{/if}

{#if toast}<div class="toast">{toast}</div>{/if}

<style>
  .page {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 36px 48px 60px;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  .head,
  .overview,
  .toolbar,
  .list,
  .records {
    width: 100%;
    max-width: 1040px;
    margin-left: auto;
    margin-right: auto;
  }
  .head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 16px;
  }
  .head h1 {
    font-size: var(--font-size-page);
    font-weight: 700;
  }
  .lead {
    font-size: var(--font-size-ui);
    line-height: 1.6;
    color: var(--muted);
    margin-top: 4px;
  }
  .newbtn {
    flex: none;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 34px;
    padding: 0 14px;
    border: 1px solid var(--line-strong);
    border-radius: 9px;
    background: var(--bg-invert);
    color: var(--fg-invert);
    font: inherit;
    font-size: var(--font-size-ui);
    font-weight: 500;
    white-space: nowrap;
    cursor: pointer;
  }
  .newbtn svg {
    width: 13px;
    height: 13px;
  }

  /* ── 概览 ── */
  .overview {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 10px;
  }
  .stat {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 12px 14px;
    border: 1px solid var(--line);
    border-radius: 11px;
    background: var(--bg-soft);
  }
  .stat > span {
    font-size: var(--font-size-small);
    color: var(--muted);
  }
  .stat b {
    font-family: var(--font-mono);
    font-size: 22px;
    font-weight: 650;
    line-height: 1.2;
    color: var(--fg);
  }
  .stat b.bad {
    color: var(--danger);
  }
  .stat small {
    min-height: 14px;
    font-size: var(--font-size-small);
    color: var(--faint);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* ── 筛选 ── */
  .toolbar {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  .search {
    flex: 1;
    min-width: 190px;
    display: flex;
    align-items: center;
    gap: 7px;
    height: 32px;
    padding: 0 10px;
    border: 1px solid var(--line);
    border-radius: 8px;
    background: var(--bg);
    color: var(--faint);
  }
  .search:focus-within {
    border-color: var(--accent);
  }
  .search svg {
    flex: none;
    width: 13px;
    height: 13px;
  }
  .search input {
    flex: 1;
    min-width: 0;
    border: none;
    outline: none;
    background: transparent;
    color: var(--fg);
    font: inherit;
    font-size: var(--font-size-ui);
  }
  .select {
    position: relative;
    flex: none;
  }
  .select select {
    appearance: none;
    height: 32px;
    padding: 0 28px 0 10px;
    border: 1px solid var(--line);
    border-radius: 8px;
    background: var(--bg);
    color: var(--muted);
    font: inherit;
    font-size: var(--font-size-ui);
    cursor: pointer;
    outline: none;
  }
  .select::after {
    content: '';
    position: absolute;
    right: 11px;
    top: 50%;
    width: 5px;
    height: 5px;
    border-right: 1.5px solid var(--faint);
    border-bottom: 1.5px solid var(--faint);
    transform: translateY(-65%) rotate(45deg);
    pointer-events: none;
  }
  .select select:focus {
    border-color: var(--accent);
    color: var(--fg);
  }
  .shown {
    margin-left: auto;
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    color: var(--faint);
    white-space: nowrap;
  }

  /* ── 任务卡 ── */
  .list {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .task {
    display: flex;
    flex-direction: column;
    gap: 8px;
    border: 1px solid var(--line);
    border-radius: 12px;
    background: var(--bg);
    padding: 14px 16px;
    transition: border-color var(--dur-fast) var(--ease-out), box-shadow var(--dur-fast) var(--ease-out);
  }
  .task:hover {
    border-color: var(--line-strong);
    box-shadow: 0 4px 16px rgb(0 0 0 / 5%);
  }
  .task.off {
    opacity: 0.72;
  }
  .rowtop {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
  }
  .titlewrap {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
    flex-wrap: wrap;
  }
  .titlewrap h2 {
    font-size: var(--font-size-body);
    font-weight: 600;
    color: var(--fg);
    overflow-wrap: anywhere;
  }
  .kind {
    flex: none;
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    color: var(--accent);
    background: var(--accent-soft);
    border-radius: 5px;
    padding: 1px 6px;
  }
  .kind.app {
    color: #b7791f;
    background: color-mix(in srgb, #f0b429 20%, transparent);
  }
  .kind.script {
    color: #6b46c1;
    background: color-mix(in srgb, #805ad5 18%, transparent);
  }
  .state {
    flex: none;
    font-size: var(--font-size-small);
    color: #2f855a;
  }
  .state.off {
    color: var(--faint);
  }
  .actions {
    flex: none;
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }
  .iconbtn {
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    border: 1px solid var(--line);
    border-radius: 7px;
    background: transparent;
    color: var(--muted);
    font: inherit;
    cursor: pointer;
  }
  .iconbtn svg {
    width: 12px;
    height: 12px;
  }
  .iconbtn:hover {
    background: var(--bg-soft);
    color: var(--fg);
  }
  .iconbtn.danger:hover {
    color: var(--danger);
  }
  .iconbtn:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .switch {
    position: relative;
    width: 32px;
    height: 18px;
    flex: none;
  }
  .switch input {
    position: absolute;
    opacity: 0;
    width: 0;
    height: 0;
  }
  .slider {
    position: absolute;
    inset: 0;
    border-radius: 9px;
    background: var(--line-strong);
    transition: background var(--dur-fast) var(--ease-out);
    cursor: pointer;
  }
  .slider::before {
    content: '';
    position: absolute;
    top: 2px;
    left: 2px;
    width: 14px;
    height: 14px;
    border-radius: 50%;
    background: #fff;
    transition: transform var(--dur-fast) var(--ease-out);
  }
  .switch input:checked + .slider {
    background: var(--accent);
  }
  .switch input:checked + .slider::before {
    transform: translateX(14px);
  }
  .desc {
    font-size: var(--font-size-ui);
    line-height: 1.6;
    color: var(--muted);
  }
  .meta {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
  }
  .pill {
    max-width: 100%;
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    color: var(--muted);
    background: var(--bg-soft);
    border-radius: 6px;
    padding: 2px 7px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .pill.next {
    color: var(--accent);
    background: var(--accent-soft);
  }
  .pill.muted {
    color: var(--faint);
  }
  .pill.err {
    color: var(--danger);
  }
  .pill.skip {
    color: #b7791f;
  }
  .lastrun {
    display: flex;
    align-items: center;
    gap: 7px;
    min-width: 0;
    padding-top: 2px;
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .lastrun .lr-time {
    font-family: var(--font-mono);
    flex: none;
  }
  .lastrun .lr-status {
    flex: none;
    color: var(--muted);
  }
  .lastrun .lr-text {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .dot {
    flex: none;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--faint);
  }
  .dot.ok {
    background: #2f855a;
  }
  .dot.err {
    background: var(--danger);
  }
  .dot.skip {
    background: #d69e2e;
  }
  :global(html[data-theme='dark']) .state {
    color: #68d391;
  }
  :global(html[data-theme='dark']) .kind.app,
  :global(html[data-theme='dark']) .rkind.app {
    color: #e0a82e;
  }
  :global(html[data-theme='dark']) .kind.script,
  :global(html[data-theme='dark']) .rkind.script {
    color: #b794f4;
  }
  :global(html[data-theme='dark']) .dot.ok {
    background: #68d391;
  }
  .empty {
    border: 1px dashed var(--line);
    border-radius: 12px;
    padding: 28px 16px;
    text-align: center;
    color: var(--faint);
    font-size: var(--font-size-ui);
  }
  .empty .hint {
    margin-top: 4px;
  }
  .hint {
    font-size: var(--font-size-small);
    line-height: 1.6;
    color: var(--faint);
  }
  .hint.small {
    margin-top: -2px;
  }

  /* ── 回调记录 ── */
  .records {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding-top: 4px;
  }
  .sechead {
    display: flex;
    align-items: baseline;
    gap: 8px;
  }
  .sechead h2 {
    font-size: var(--font-size-section);
    font-weight: 600;
    color: var(--fg);
  }
  .sechead span {
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .rtable {
    border: 1px solid var(--line);
    border-radius: 11px;
    overflow: hidden;
    background: var(--bg);
  }
  .rhead,
  .rrow {
    display: grid;
    grid-template-columns: 88px minmax(135px, 1.05fr) minmax(150px, 1.2fr) minmax(175px, 1.65fr) minmax(140px, 1.35fr);
    gap: 10px;
    align-items: center;
    padding: 7px 12px;
  }
  .rhead {
    background: var(--bg-soft);
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .rrow {
    font-size: var(--font-size-small);
    color: var(--muted);
    border-top: 1px solid var(--line);
  }
  .rrow:hover {
    background: var(--bg-soft);
  }
  .rtime {
    font-family: var(--font-mono);
    color: var(--faint);
    white-space: nowrap;
  }
  .rtask {
    display: flex;
    align-items: center;
    gap: 5px;
    min-width: 0;
  }
  .rname {
    min-width: 0;
    color: var(--fg);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .rkind {
    flex: none;
    font-style: normal;
    font-size: 10px;
    color: var(--faint);
    border: 1px solid var(--line);
    border-radius: 4px;
    padding: 0 3px;
  }
  .rkind.app {
    color: #b7791f;
    border-color: color-mix(in srgb, #f0b429 35%, var(--line));
  }
  .rkind.script {
    color: #6b46c1;
    border-color: color-mix(in srgb, #805ad5 35%, var(--line));
  }
  .rkind.manual {
    color: var(--accent);
    border-color: color-mix(in srgb, var(--accent) 35%, var(--line));
  }
  .rtarget,
  .rcontent,
  .rresult {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .rtarget {
    font-family: var(--font-mono);
  }
  .rresult {
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    white-space: normal;
    line-height: 1.45;
  }
  .rresult.err {
    color: var(--danger);
  }

  /* ── 弹窗 ── */
  .mask {
    position: fixed;
    inset: 0;
    z-index: 58;
    background: rgb(0 0 0 / 18%);
  }
  .mask.top {
    z-index: 60;
  }
  .modal {
    position: fixed;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    z-index: 59;
    width: min(620px, 94vw);
    max-height: 88vh;
    display: flex;
    flex-direction: column;
    background: var(--bg);
    border: 1px solid var(--line-strong);
    border-radius: 14px;
    box-shadow: 0 16px 48px rgb(0 0 0 / 20%);
    overflow: hidden;
  }
  .modal.small {
    width: min(380px, 92vw);
  }
  .modal.alert {
    z-index: 61;
  }
  .modal header {
    flex: none;
    display: flex;
    align-items: center;
    padding: 14px 18px;
    border-bottom: 1px solid var(--line);
  }
  .modal header h2 {
    font-size: var(--font-size-body);
    font-weight: 700;
    flex: 1;
  }
  .close {
    border: none;
    background: transparent;
    color: var(--faint);
    font: inherit;
    padding: 2px 5px;
    cursor: pointer;
  }
  .close:hover {
    color: var(--fg);
  }
  .form {
    min-height: 0;
    overflow-y: auto;
    padding: 14px 18px 18px;
    display: flex;
    flex-direction: column;
    gap: 11px;
  }
  .fields2 {
    display: grid;
    grid-template-columns: 1fr 1.2fr;
    gap: 10px;
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 5px;
    min-width: 0;
  }
  .field > span {
    font-size: var(--font-size-small);
    color: var(--muted);
  }
  .field input,
  .field select,
  .field textarea {
    width: 100%;
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
  .field input:focus,
  .field select:focus,
  .field textarea:focus {
    border-color: var(--accent);
  }
  .section-label {
    margin-top: 3px;
    font-size: var(--font-size-small);
    font-weight: 600;
    color: var(--faint);
    letter-spacing: 0.02em;
  }
  .seg {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
  }
  .seg button {
    border: 1px solid var(--line);
    border-radius: 8px;
    background: transparent;
    color: var(--muted);
    font: inherit;
    font-size: var(--font-size-ui);
    padding: 5px 12px;
    cursor: pointer;
  }
  .seg button:hover {
    border-color: var(--line-strong);
    color: var(--fg);
  }
  .seg button.on {
    background: var(--accent-soft);
    border-color: var(--accent);
    color: var(--accent);
    font-weight: 500;
  }
  .seg.sub {
    margin-top: -3px;
  }
  .seg.sub button {
    padding: 3px 9px;
    font-size: var(--font-size-small);
    border-radius: 6px;
  }
  .endpointlist {
    display: flex;
    flex-direction: column;
    gap: 4px;
    max-height: 190px;
    overflow-y: auto;
    padding: 4px;
    border: 1px solid var(--line);
    border-radius: 9px;
    background: var(--bg-soft);
  }
  .endpoint {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 6px 8px;
    border: 1px solid transparent;
    border-radius: 7px;
    background: transparent;
    color: var(--muted);
    font: inherit;
    text-align: left;
    cursor: pointer;
  }
  .endpoint:hover {
    background: var(--bg);
    border-color: var(--line);
  }
  .endpoint.sel {
    background: var(--bg);
    border-color: var(--accent);
    box-shadow: inset 2px 0 0 var(--accent);
  }
  .ep-top {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    font-size: var(--font-size-ui);
  }
  .ep-top b {
    color: var(--fg);
    font-family: var(--font-mono);
    font-size: var(--font-size-small);
  }
  .ep-title {
    color: var(--muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .ep-top code {
    flex: none;
    margin-left: auto;
    font-family: var(--font-mono);
    font-size: 10px;
    color: var(--faint);
  }
  .ep-url {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: var(--font-mono);
    font-size: 10px;
    color: var(--faint);
  }
  .ep-desc {
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .interval {
    display: flex;
    gap: 6px;
  }
  .interval input {
    flex: 1;
    min-width: 0;
  }
  .interval select {
    flex: none;
    width: 88px;
  }
  .check {
    display: flex;
    align-items: center;
    gap: 7px;
    font-size: var(--font-size-ui);
    color: var(--muted);
  }
  .alerttext {
    padding: 14px 18px;
    font-size: var(--font-size-ui);
    line-height: 1.7;
    color: var(--muted);
    overflow-wrap: anywhere;
    white-space: pre-wrap;
  }
  .confirmtext {
    padding: 4px 18px 14px;
    font-size: var(--font-size-ui);
    line-height: 1.6;
    color: var(--muted);
  }
  .modal footer {
    flex: none;
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    padding: 12px 18px;
    border-top: 1px solid var(--line);
  }
  .btn {
    border: 1px solid var(--line);
    border-radius: 8px;
    background: transparent;
    color: var(--muted);
    font: inherit;
    font-size: var(--font-size-ui);
    padding: 7px 16px;
    cursor: pointer;
  }
  .btn:hover {
    border-color: var(--line-strong);
    color: var(--fg);
  }
  .btn.primary {
    background: var(--accent);
    border-color: var(--accent);
    color: #fff;
  }
  .btn.primary:hover {
    background: var(--accent);
    color: #fff;
  }
  .btn.danger {
    background: #c0392b;
    border-color: #c0392b;
    color: #fff;
  }
  .btn:disabled {
    opacity: 0.5;
  }
  .toast {
    position: fixed;
    right: 28px;
    bottom: 28px;
    z-index: 70;
    max-width: min(420px, 88vw);
    padding: 9px 14px;
    border: 1px solid var(--line-strong);
    border-radius: 9px;
    background: var(--bg-invert);
    color: var(--fg-invert);
    font-size: var(--font-size-ui);
    box-shadow: 0 8px 26px rgb(0 0 0 / 18%);
  }

  @media (max-width: 920px) {
    .page {
      padding: 24px 18px 48px;
    }
    .overview {
      grid-template-columns: 1fr 1fr;
    }
    .fields2 {
      grid-template-columns: 1fr;
    }
    .rhead {
      display: none;
    }
    .rrow {
      grid-template-columns: 76px minmax(0, 1fr) auto;
      gap: 4px 10px;
      align-items: start;
      padding: 9px 12px;
    }
    .rrow .rtime {
      grid-column: 1;
      grid-row: 1;
    }
    .rrow .rtask {
      grid-column: 2;
      grid-row: 1;
    }
    .rrow .rresult {
      grid-column: 3;
      grid-row: 1;
      max-width: 38vw;
      text-align: right;
    }
    .rrow .rtarget,
    .rrow .rcontent {
      grid-column: 2 / 4;
      font-size: 10px;
    }
    .rrow .rtarget {
      grid-row: 2;
    }
    .rrow .rcontent {
      grid-row: 3;
    }
  }
</style>
