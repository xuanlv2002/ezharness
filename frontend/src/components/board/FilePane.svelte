<script lang="ts">
  import { api } from '../../lib/api'
  import { fileBaseName, normFileKey } from '../../lib/textfile'
  import { store } from '../../lib/store.svelte'

  /*
  工作区抽屉·文件页：多文件 tab 的简易文本编辑器（supper_url file://
  的消费端）。磁盘是唯一真身——内容读 /api/workspace/file，保存写
  /api/workspace/save（工作目录沙箱），tab 仅是视图薄壳。
  标注流（单一交互模型）：选中代码 → 选区旁浮出输入条（Enter 暂存
  / Esc 取消）→ 暂存片段加下划线；点击下划线或在标注内选中 →
  同一浮条进入编辑（改备注/删除），不新增重叠标注。「一键润色」
  把 路径+全部标注 拼成润色请求发出（dirty 先自动保存；无标注时
  润色全文）。文本编辑时标注按编辑点平移/按原文重定位。
  磁盘变更：文件页激活时每 2s HEAD 轮询 Last-Modified（AI 改盘后），
  干净 tab 自动重载（清空标注——旧偏移失效），dirty tab 只提示。
  */

  let { active = false }: { active?: boolean } = $props()

  const MAX_BYTES = 2 << 20 // 与后端 maxSaveContent 一致
  const MAX_SEL = 6000 // 发送时选中片段截断

  interface Annot {
    sel: string
    note: string
    start: number
    end: number
  }

  interface FileTab {
    key: string
    path: string
    name: string
    content: string
    saved: string
    sel: string
    selStart: number
    selEnd: number
    note: string
    annotations: Annot[]
    loading: boolean
    err: string
    warn: string
    savedAt: string
    lastMod: string
  }

  let tabs = $state<FileTab[]>([])
  let current = $state('')
  let pendingClose = $state('')
  let sending = $state(false)
  let editorEl = $state<HTMLTextAreaElement | undefined>()
  let wrapEl = $state<HTMLDivElement | undefined>()
  let hlEl = $state<HTMLDivElement | undefined>()
  let curEl = $state<HTMLElement | undefined>()
  let noteEl = $state<HTMLInputElement | undefined>()
  /* 浮动标注条/气泡位置（相对 .editor-wrap） */
  let annotPos = $state({ top: 0, left: 0 })
  /* 编辑模式：选区落在已有标注内时，浮条编辑该标注的备注而非新增 */
  let editing = $state(-1)

  const tab = $derived(tabs.find((t) => t.key === current))
  const dirty = $derived(!!tab && tab.content !== tab.saved)

  /* 装饰层分段：按标注边界与当前选区边界切 content；段归属：已暂存
  标注（下划线可点）优先，其次当前选区（cur 淡蓝高亮——textarea
  失焦后原生选区变灰不可见，高亮由装饰层负责） */
  const segs = $derived.by(() => {
    const t = tab
    if (!t || !t.content) return []
    const cuts = new Set<number>()
    for (const a of t.annotations) {
      if (a.start > a.end) continue
      if (a.start > 0) cuts.add(a.start)
      if (a.end < t.content.length) cuts.add(a.end)
    }
    let curFrom = -1
    let curTo = -1
    if (t.sel && t.selEnd > t.selStart) {
      curFrom = Math.max(0, t.selStart)
      curTo = Math.min(t.content.length, t.selEnd)
      if (curFrom > 0) cuts.add(curFrom)
      if (curTo < t.content.length) cuts.add(curTo)
    }
    const pts = [...cuts].sort((a, b) => a - b)
    const out: { text: string; mark: number; cur: boolean }[] = []
    let prev = 0
    const push = (to: number) => {
      if (to <= prev) return
      const i = t.annotations.findIndex((a) => a.start <= prev && to <= a.end && a.start < a.end)
      const cur = i < 0 && curFrom >= 0 && curFrom <= prev && to <= curTo
      out.push({ text: t.content.slice(prev, to), mark: i, cur })
      prev = to
    }
    for (const p of pts) push(p)
    push(t.content.length)
    return out
  })

  /* 外部定位请求（supper_url file:// 点击）：文件按需即开，直接消费 */
  $effect(() => {
    const want = store.fileFocus
    if (!want) return
    store.fileFocus = ''
    void openTab(want)
  })

  /* 激活时聚焦编辑器；选区出现时聚焦备注输入（直接打字） */
  $effect(() => {
    if (active && tab && !tab.sel) requestAnimationFrame(() => editorEl?.focus())
  })
  $effect(() => {
    if (tab?.sel || editing >= 0) requestAnimationFrame(() => noteEl?.focus())
  })

  /* 浮条贴着目标：优先当前选区高亮块，其次编辑中的标注下划线（末行矩形） */
  $effect(() => {
    segs
    requestAnimationFrame(placeAnnot)
  })

  function placeAnnot() {
    if (!wrapEl) return
    let rects: DOMRectList | null = null
    if (curEl) rects = curEl.getClientRects()
    if ((!rects || !rects.length) && editing >= 0 && hlEl) {
      const m = hlEl.querySelector<HTMLElement>(`[data-i="${editing}"]`)
      rects = m?.getClientRects() ?? null
    }
    if (!rects || !rects.length) return
    const last = rects[rects.length - 1]
    const w = wrapEl.getBoundingClientRect()
    annotPos = { top: last.bottom - w.top + 4, left: last.left - w.left }
  }

  /* 磁盘变更轮询：仅文件页激活时进行 */
  $effect(() => {
    if (!active) return
    const id = setInterval(() => void pollDisk(), 2000)
    return () => clearInterval(id)
  })

  async function openTab(path: string) {
    const key = normFileKey(path)
    if (tabs.some((t) => t.key === key)) {
      current = key
      return
    }
    tabs = [
      ...tabs,
      {
        key,
        path: path.trim(),
        name: fileBaseName(path.trim()),
        content: '',
        saved: '',
        sel: '',
        selStart: 0,
        selEnd: 0,
        note: '',
        annotations: [],
        loading: true,
        err: '',
        warn: '',
        savedAt: '',
        lastMod: '',
      },
    ]
    current = key
    /* 必须从 $state 数组取代理版：闭包持有原始对象的话，
    await 后的属性写入不触发响应（loading 永不消失） */
    const t = tabs[tabs.length - 1]
    try {
      const r = await fetch('/api/workspace/file?path=' + encodeURIComponent(t.path), { cache: 'no-store' })
      if (!r.ok) {
        t.err = `文件读取失败（${r.status}）——仅支持工作目录内的文本文件`
        return
      }
      const clen = Number(r.headers.get('content-length') || 0)
      const text = await r.text()
      if (clen > MAX_BYTES || text.length > MAX_BYTES) {
        t.err = '文件过大（>2MB），不支持在线编辑'
        return
      }
      if (text.includes('\uFFFD')) {
        t.warn = '文件疑似非 UTF-8 编码：显示可能乱码，保存后会转为 UTF-8'
      }
      t.content = text
      t.saved = text
      t.lastMod = r.headers.get('last-modified') || ''
    } catch (e) {
      t.err = `文件读取失败：${(e as Error).message}`
    } finally {
      t.loading = false
    }
  }

  /* 在 text 中找 s 的匹配位置，多个时取离 hint 最近的一个 */
  function findNear(text: string, s: string, hint: number): number {
    let best = -1
    let bestDist = Infinity
    let i = text.indexOf(s)
    while (i >= 0) {
      const d = Math.abs(i - hint)
      if (d < bestDist) {
        bestDist = d
        best = i
      }
      i = text.indexOf(s, i + 1)
    }
    return best
  }

  /* 干净 tab 磁盘有更新时自动重载，标注按原文重定位保留（AI 改掉的
  片段其标注随之失效丢弃并提示）；dirty 只提示不覆盖 */
  async function pollDisk() {
    const t = tab
    if (!t || t.loading || t.err) return
    try {
      const h = await fetch('/api/workspace/file?path=' + encodeURIComponent(t.path), {
        method: 'HEAD',
        cache: 'no-store',
      })
      if (!h.ok) return
      const lm = h.headers.get('last-modified') || ''
      if (!lm || lm === t.lastMod) return
      t.lastMod = lm
      if (t.content !== t.saved) {
        t.warn = '磁盘文件已被更新（AI 修改）：未自动加载，保存将覆盖'
        return
      }
      const y = await fetch('/api/workspace/file?path=' + encodeURIComponent(t.path), { cache: 'no-store' })
      if (!y.ok) return
      const text = await y.text()
      const st = editorEl?.scrollTop ?? 0
      let lost = 0
      const kept = t.annotations
        .map((a) => {
          const i = findNear(text, a.sel, a.start)
          return i >= 0 ? { ...a, start: i, end: i + a.sel.length } : (lost++, null)
        })
        .filter((a): a is Annot => !!a)
      t.content = text
      t.saved = text
      t.annotations = kept
      t.sel = ''
      t.note = ''
      editing = -1
      t.savedAt = new Date().toTimeString().slice(0, 5)
      if (lost > 0) t.warn = `文件已更新（AI 修改），${lost} 条标注因原文变更失效`
      requestAnimationFrame(() => {
        if (editorEl) editorEl.scrollTop = st
      })
    } catch {
      /* 轮询失败静默，下轮再试 */
    }
  }

  function closeTab(key: string) {
    const t = tabs.find((x) => x.key === key)
    if (!t) return
    if (t.content !== t.saved) {
      pendingClose = key
      return
    }
    removeTab(key)
  }

  function removeTab(key: string) {
    const i = tabs.findIndex((t) => t.key === key)
    if (i < 0) return
    tabs = tabs.filter((t) => t.key !== key)
    pendingClose = ''
    if (current === key) current = tabs[Math.min(i, tabs.length - 1)]?.key ?? ''
  }

  async function saveTab(t: FileTab) {
    if (t.loading || t.content === t.saved) return true
    try {
      await api.saveFile(t.path, t.content)
      t.saved = t.content
      t.savedAt = new Date().toTimeString().slice(0, 5)
      t.err = ''
      t.lastMod = '' /* 下一轮轮询重新建档，避免自己的写入误报"磁盘更新" */
      return true
    } catch (e) {
      t.err = `保存失败：${(e as Error).message}`
      return false
    }
  }

  /* dirty 关闭确认的「保存并关闭」：面向目标 tab（非当前 tab 也要可存） */
  async function saveAndClose(key: string) {
    const t = tabs.find((x) => x.key === key)
    if (!t) return
    if (!(await saveTab(t))) return
    removeTab(key)
  }

  /* Ctrl+S（preventDefault 挡 WebView2 的"保存网页"默认行为） */
  function onEditorKey(e: KeyboardEvent) {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's' && !e.isComposing) {
      e.preventDefault()
      if (tab) void saveTab(tab)
    }
  }

  function lineAt(v: string, off: number): number {
    return v.slice(0, off).split('\n').length
  }

  /* 选中捕获：mouseup/keyup/select 均为组合结束后时机，避开输入法中间态。
  选区完全落在已有标注内 → 编辑该标注备注（不新增） */
  function captureSel(e: Event) {
    if (!tab) return
    const t = e.target as HTMLTextAreaElement
    if (t.selectionStart === t.selectionEnd) {
      if (tab.sel) {
        tab.sel = ''
        tab.note = ''
        editing = -1
      }
      return
    }
    tab.sel = t.value.slice(t.selectionStart, t.selectionEnd)
    tab.selStart = t.selectionStart
    tab.selEnd = t.selectionEnd
    const ei = tab.annotations.findIndex(
      (a) => a.start <= t.selectionStart && t.selectionEnd <= a.end,
    )
    editing = ei
    tab.note = ei >= 0 ? tab.annotations[ei].note : ''

  }

  /* 编辑后修正标注偏移：编辑点前的标注不动，其后的平移，跨编辑点的
  按原文重定位（找不到则尽力平移） */
  function onEditorInput(e: Event) {
    const t = tab
    if (!t || !t.annotations.length) return
    const ta = e.target as HTMLTextAreaElement
    const np = ta.selectionStart
    const delta = ta.value.length - t.content.length
    const op = delta >= 0 ? np - delta : np
    t.annotations = t.annotations.map((a) => {
      if (a.end <= op) return a
      if (a.start >= op) return { ...a, start: a.start + delta, end: a.end + delta }
      const i = ta.value.indexOf(a.sel)
      return i >= 0 ? { ...a, start: i, end: i + a.sel.length } : { ...a, start: a.start + delta, end: a.end + delta }
    })
  }

  /* textarea 滚动同步装饰层，浮条跟随 */
  function onEditorScroll(e: Event) {
    const ta = e.target as HTMLTextAreaElement
    if (hlEl) {
      hlEl.scrollTop = ta.scrollTop
      hlEl.scrollLeft = ta.scrollLeft
    }
    placeAnnot()
  }

  /* 暂存/更新：编辑模式改目标标注的备注（区间不变），否则新增标注 */
  function stashAnnot() {
    if (!tab || !tab.sel) return
    if (editing >= 0 && tab.annotations[editing]) {
      const a = tab.annotations[editing]
      tab.annotations = [
        ...tab.annotations.slice(0, editing),
        { ...a, note: tab.note.trim() },
        ...tab.annotations.slice(editing + 1),
      ]
    } else {
      tab.annotations = [
        ...tab.annotations,
        { sel: tab.sel, note: tab.note.trim(), start: tab.selStart, end: tab.selEnd },
      ]
    }
    tab.sel = ''
    tab.note = ''
    editing = -1

    editorEl?.focus()
  }

  function dropAnnot(i: number) {
    if (!tab) return
    tab.annotations = tab.annotations.filter((_, idx) => idx !== i)
  }

  /* 点击下划线：进入该标注的编辑模式（同一浮条：改备注/删除） */
  function clickMark(i: number) {
    if (!tab || !tab.annotations[i]) return
    editing = i
    tab.sel = ''
    tab.selStart = 0
    tab.selEnd = 0
    tab.note = tab.annotations[i].note
    requestAnimationFrame(placeAnnot)
  }

  /* 浮条里的删除（编辑模式） */
  function removeEditing() {
    if (editing >= 0) dropAnnot(editing)
    if (!tab) return
    tab.sel = ''
    tab.note = ''
    editing = -1
    editorEl?.focus()
  }

  /* 一键润色：把 路径+全部标注（或当前选区）作为润色请求发出；
  无标注无选区时润色整个文件。dirty 先自动保存（AI 读最新内容） */
  async function polish() {
    if (!tab || sending || tab.loading) return
    const items = tab.annotations.map((a) => ({
      ...a,
      from: lineAt(tab.content, a.start),
      to: lineAt(tab.content, Math.max(0, a.end - 1)),
    }))
    if (tab.sel && editing < 0) {
      items.push({
        sel: tab.sel,
        note: tab.note.trim(),
        start: tab.selStart,
        end: tab.selEnd,
        from: lineAt(tab.content, tab.selStart),
        to: lineAt(tab.content, tab.selEnd - 1),
      })
    }
    if (dirty && !(await saveTab(tab))) {
      tab.err = tab.err || '尚未保存成功，未发送'
      return
    }
    const clip = (s: string) => (s.length > MAX_SEL ? s.slice(0, MAX_SEL) + '…（已截断）' : s)
    const parts: string[] = []
    if (!items.length) {
      parts.push(`请润色这个文件：${tab.path}（优化代码质量与表达，保持行为不变，直接改写文件）`)
    } else {
      parts.push(`请润色文件 ${tab.path} 的以下片段（优化代码质量与表达，保持行为不变，直接改写文件）：`)
      items.forEach((a, i) => {
        const head = items.length > 1 ? `【片段 ${i + 1}】` : ''
        const lines = a.to > a.from ? `行 ${a.from}-${a.to}` : `行 ${a.from}`
        parts.push(`${head}选中（${lines}）：\n${clip(a.sel)}${a.note ? `\n备注：${a.note}` : ''}`)
      })
    }
    sending = true
    try {
      await store.send(parts.join('\n'))
      tab.annotations = []
      tab.sel = ''
      tab.note = ''

    } finally {
      sending = false
    }
  }
  const selLines = $derived.by(() => {
    const t = tab
    if (!t) return ''
    if (!t.sel && editing >= 0 && t.annotations[editing]) {
      const a = t.annotations[editing]
      const from = lineAt(t.content, a.start)
      const to = lineAt(t.content, Math.max(0, a.end - 1))
      return to > from ? `行 ${from}-${to}` : `行 ${from}`
    }
    if (!t.sel) return ''
    const from = lineAt(t.content, t.selStart)
    const to = lineAt(t.content, t.selEnd - 1)
    return to > from ? `行 ${from}-${to}` : `行 ${from}`
  })
</script>

<div class="file-pane">
  <div class="strip">
    {#each tabs as t (t.key)}
      <button class="strip-item" class:active={current === t.key} onclick={() => (current = t.key)} title={t.path}>
        <span class="name">{t.name}</span>
        {#if t.content !== t.saved}<i class="dirty"></i>{/if}
        <span class="x" onclick={(e) => { e.stopPropagation(); closeTab(t.key) }} role="button" tabindex="-1" title="关闭">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round">
            <path d="M6 6l12 12M18 6L6 18" />
          </svg>
        </span>
      </button>
    {/each}
  </div>

  {#if pendingClose}
    <div class="confirm">
      <span>有未保存的修改</span>
      <button onclick={() => void saveAndClose(pendingClose)}>保存并关闭</button>
      <button class="ghost" onclick={() => removeTab(pendingClose)}>放弃修改</button>
      <button class="ghost" onclick={() => (pendingClose = '')}>取消</button>
    </div>
  {/if}

  <div class="editor-wrap" bind:this={wrapEl}>
    {#if tab}
      <textarea
        bind:this={editorEl}
        bind:value={tab.content}
        onkeydown={onEditorKey}
        oninput={onEditorInput}
        onmouseup={captureSel}
        onkeyup={captureSel}
        onselect={captureSel}
        onscroll={onEditorScroll}
        spellcheck="false"
        placeholder="（空文件）"
      ></textarea>
      <!-- 装饰层：与 textarea 同排版的透明文字；标注片段下划线可点击 -->
      <div class="hl" bind:this={hlEl} aria-hidden="true">
        {#each segs as s, i (i)}
          {#if s.mark >= 0}
            <span class="mark" data-i={s.mark} onclick={() => clickMark(s.mark)} title="编辑这条标注">{s.text}</span>
          {:else if s.cur}
            <span class="cur" bind:this={curEl}>{s.text}</span>
          {:else}{s.text}{/if}
        {/each}
      </div>
      {#if tab.loading}<div class="veil">加载中…</div>{/if}
      {#if tab.err}<div class="veil err">{tab.err}</div>{/if}

      <!-- 统一的标注输入条：选区旁浮出（新增/更新），点击下划线进入编辑 -->
      {#if tab.sel || editing >= 0}
        <div class="annotbar" style="top: {annotPos.top}px; left: {annotPos.left}px">
          <span class="selinfo">{selLines}</span>
          <input
            bind:this={noteEl}
            bind:value={tab.note}
            maxlength="2000"
            placeholder={editing >= 0 ? '修改这条标注的备注 · Enter 更新' : '备注（可留空）· Enter 暂存'}
            onkeydown={(e) => {
              if (e.key === 'Enter' && !e.isComposing) { e.preventDefault(); stashAnnot() }
              if (e.key === 'Escape') { tab.sel = ''; tab.note = ''; editing = -1; editorEl?.focus() }
            }}
          />
          <button class="op" onclick={stashAnnot} title={editing >= 0 ? '更新这条标注的备注' : '暂存这条标注，可继续选下一处'}>
            {editing >= 0 ? '更新' : '暂存'}
          </button>
          {#if editing >= 0}
            <button class="op del" onclick={removeEditing} title="删除这条标注">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                <path d="M6 6l12 12M18 6L6 18" />
              </svg>
            </button>
          {/if}
        </div>
      {/if}
    {:else}
      <div class="empty">
        <p>还没有打开的文件</p>
        <p class="hint">对话中出现 <code>📄 文件名</code> 入口时点击即可打开</p>
      </div>
    {/if}
  </div>

  {#if tab}
    <!-- 常驻轻量状态条 -->
    <div class="statusbar">
      <button class="savebtn" class:dirty disabled={!dirty || tab.loading} onclick={() => void saveTab(tab)} title="保存（Ctrl+S）">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z" />
          <polyline points="17 21 17 13 7 13 7 21" />
          <polyline points="7 3 7 8 15 8" />
        </svg>
        {#if dirty}<i class="dot"></i>{/if}
      </button>
      <button class="polbtn" disabled={sending || tab.loading} onclick={() => void polish()} title="把文件与标注交给 AI 润色（无标注时润色全文）">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <path d="M12 3l1.9 5.1L19 10l-5.1 1.9L12 17l-1.9-5.1L5 10l5.1-1.9z" />
          <path d="M19 15l.9 2.4L22 18l-2.1.6L19 21l-.9-2.4L16 18l2.1-.6z" />
        </svg>
        {sending ? '润色中…' : '一键润色'}
      </button>
      <span class="stat">
        {#if tab.err}<i class="bad">{tab.err}</i>
        {:else if tab.warn}<i class="bad">{tab.warn}</i>
        {:else if dirty}<i>未保存</i>
        {:else if tab.savedAt}<i>已保存 {tab.savedAt}</i>
        {/if}
      </span>
      <span class="tip">
        {#if tab.annotations.length}已暂存 {tab.annotations.length} 条标注 · {/if}选中代码可添加标注
      </span>
    </div>
  {/if}
</div>

<style>
  .file-pane {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    gap: 8px;
  }
  .strip {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
    flex: none;
  }
  .strip-item {
    display: flex;
    align-items: center;
    gap: 6px;
    border: 1px solid var(--line);
    background: var(--bg);
    border-radius: 9px;
    padding: 4px 6px 4px 10px;
    font-size: 12px;
    color: var(--muted);
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out),
      border-color var(--dur-fast) var(--ease-out);
  }
  .strip-item:hover {
    background: var(--bg-soft);
    color: var(--fg);
  }
  .strip-item.active {
    border-color: var(--accent-soft, var(--accent));
    background: var(--bg-soft);
    color: var(--fg);
    font-weight: 600;
  }
  .name {
    max-width: 140px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .dirty {
    flex: none;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: #f0883e;
  }
  .x {
    display: grid;
    place-items: center;
    width: 16px;
    height: 16px;
    border-radius: 5px;
    color: var(--faint);
  }
  .x:hover {
    background: var(--line);
    color: var(--fg);
  }
  .x svg {
    width: 9px;
    height: 9px;
  }
  /* dirty 关闭二次确认条（WebView 里原生 confirm 难看，用内联条） */
  .confirm {
    flex: none;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 10px;
    border: 1px solid #f0883e;
    border-radius: 9px;
    background: color-mix(in srgb, #f0883e 8%, var(--bg));
    font-size: 12px;
    color: var(--fg);
  }
  .confirm button {
    border: 1px solid var(--line-strong);
    background: var(--bg);
    color: var(--fg);
    border-radius: 7px;
    padding: 3px 10px;
    font-size: 11.5px;
    cursor: pointer;
  }
  .confirm button.ghost {
    border-color: var(--line);
    color: var(--muted);
  }
  .editor-wrap {
    position: relative;
    flex: 1;
    min-height: 0;
    border: 1px solid var(--line);
    border-radius: 12px;
    background: #fff;
    overflow: hidden;
  }
  textarea,
  .hl {
    /* 排版完全同构：换行/滚动槽一致，装饰层透明文字才能与真文字重合 */
    display: block;
    width: 100%;
    height: 100%;
    padding: 10px 12px;
    font-family: var(--font-mono);
    font-size: 12.5px;
    line-height: 1.6;
    tab-size: 4;
    white-space: pre-wrap;
    overflow-wrap: break-word;
    scrollbar-gutter: stable;
    box-sizing: border-box;
  }
  textarea {
    border: none;
    outline: none;
    resize: none;
    background: transparent;
    color: var(--fg);
  }
  /* 装饰层在 textarea 之上（DOM 后渲染）：整体穿透，仅标注片段可点击 */
  .hl {
    position: absolute;
    inset: 0;
    overflow: hidden;
    pointer-events: none;
    color: transparent;
    z-index: 1;
  }
  .hl .mark {
    pointer-events: auto;
    cursor: pointer;
    border-bottom: 2px solid #f0883e;
    background: color-mix(in srgb, #f0883e 12%, transparent);
    border-radius: 2px;
  }
  /* 当前选区高亮：textarea 失焦后原生选区变灰，可见性由这里负责 */
  .hl .cur {
    background: color-mix(in srgb, #2563eb 16%, transparent);
    border-radius: 2px;
  }
  .veil {
    position: absolute;
    inset: 0;
    display: grid;
    place-items: center;
    background: var(--bg);
    color: var(--faint);
    font-size: 12.5px;
    z-index: 3;
  }
  .veil.err {
    white-space: pre-wrap;
    padding: 0 20px;
    text-align: center;
    color: #e74c3c;
  }
  .empty {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 6px;
    color: var(--faint);
    font-size: 13px;
  }
  .empty .hint {
    font-size: 11.5px;
  }
  .empty code {
    font-family: var(--font-mono);
    background: var(--bg-soft);
    border-radius: 4px;
    padding: 0 4px;
  }
  /* 选区旁浮出的标注输入条 */
  .annotbar {
    position: absolute;
    z-index: 4;
    display: flex;
    align-items: center;
    gap: 8px;
    max-width: calc(100% - 16px);
    border: 1px solid var(--accent-soft, var(--accent));
    border-radius: 10px;
    background: var(--bg);
    box-shadow: 0 4px 16px rgb(0 0 0 / 10%);
    padding: 5px 8px;
    animation: annot-in var(--dur-fast) var(--ease-out) both;
  }
  @keyframes annot-in {
    from {
      opacity: 0;
      transform: translateY(4px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }
  .selinfo {
    flex: none;
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--accent, #2563eb);
  }
  .annotbar input {
    flex: 1;
    min-width: 60px;
    border: none;
    outline: none;
    background: transparent;
    color: var(--fg);
    font-size: 12.5px;
  }
  .annotbar input::placeholder {
    color: var(--faint);
  }
  .op {
    flex: none;
    border: 1px solid var(--line-strong);
    background: var(--bg);
    color: var(--fg);
    border-radius: 8px;
    padding: 4px 12px;
    font-size: 12px;
    font-weight: 550;
    cursor: pointer;
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .op.primary {
    border-color: var(--accent, #2563eb);
    color: var(--accent, #2563eb);
  }
  .op.primary:not(:disabled):hover {
    background: var(--accent-soft, color-mix(in srgb, #2563eb 10%, transparent));
  }
  .op.icon {
    display: grid;
    place-items: center;
    width: 28px;
    height: 28px;
    padding: 0;
  }
  .op.icon svg {
    width: 14px;
    height: 14px;
  }
  .op:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }
  /* 浮条里的删除标注钮（编辑模式） */
  .op.del {
    display: grid;
    place-items: center;
    width: 28px;
    height: 28px;
    padding: 0;
    border-color: var(--line);
    color: var(--muted);
  }
  .op.del svg {
    width: 12px;
    height: 12px;
  }
  .op.del:hover {
    color: #e74c3c;
    border-color: #e74c3c;
  }
  /* 常驻轻量状态条 */
  .statusbar {
    flex: none;
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 24px;
    font-size: 11px;
    color: var(--faint);
  }
  .savebtn {
    position: relative;
    flex: none;
    display: grid;
    place-items: center;
    width: 24px;
    height: 24px;
    border: 1px solid transparent;
    border-radius: 7px;
    background: transparent;
    color: var(--faint);
    cursor: pointer;
    transition:
      color var(--dur-fast) var(--ease-out),
      background var(--dur-fast) var(--ease-out),
      border-color var(--dur-fast) var(--ease-out);
  }
  .savebtn svg {
    width: 13px;
    height: 13px;
  }
  .savebtn:hover:not(:disabled) {
    color: var(--fg);
    background: var(--bg-soft);
    border-color: var(--line);
  }
  .savebtn:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .savebtn.dirty {
    color: #f0883e;
  }
  .savebtn .dot {
    position: absolute;
    top: -2px;
    right: -2px;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: #f0883e;
  }
  /* 一键润色：保存钮旁的常驻动作钮（无标注时润色全文） */
  .polbtn {
    flex: none;
    display: flex;
    align-items: center;
    gap: 5px;
    border: 1px solid var(--accent-soft, var(--accent));
    background: transparent;
    color: var(--accent, #2563eb);
    border-radius: 7px;
    padding: 3px 10px;
    font-size: 11.5px;
    font-weight: 550;
    cursor: pointer;
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .polbtn svg {
    width: 12px;
    height: 12px;
  }
  .polbtn:hover:not(:disabled) {
    background: var(--accent-soft, color-mix(in srgb, #2563eb 10%, transparent));
  }
  .polbtn:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }
  .stat {
    min-width: 0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .stat i {
    font-style: normal;
  }
  .stat .bad {
    color: #e07a4f;
  }
  .tip {
    margin-left: auto;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
</style>
