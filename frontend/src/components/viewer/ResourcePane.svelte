<script lang="ts">
  import { api } from '../../lib/api'
  import { fileBaseName, normFileKey } from '../../lib/textfile'
  import { store } from '../../lib/store.svelte'
  import { kindOf, MAX_BYTES, type ResTab } from './registry'
  import TextViewer from './viewers/TextViewer.svelte'
  import ImageViewer from './viewers/ImageViewer.svelte'
  import HtmlViewer from './viewers/HtmlViewer.svelte'
  import MarkdownViewer from './viewers/MarkdownViewer.svelte'
  import PdfViewer from './viewers/PdfViewer.svelte'
  import FallbackViewer from './viewers/FallbackViewer.svelte'

  /*
  工作区抽屉·资源查看器（file:// 的消费端，通用框架）：多 tab 按扩展名
  路由到各资源专属 viewer（registry 单一来源，新资源=新组件+此处挂载）。
  磁盘是唯一真身——一切皆资源：文本系（text/md/html 原文）单编辑器+
  tab 状态数据化（单实例重绑）；图片 tab per-tab 实例保活（画板画布
  状态不可数据化）。状态条统一：保存/Ctrl+S、添加到对话（纯文件引用
  chip，经 <reference_file> 告知模型）。磁盘变更：激活时每 2s HEAD
  轮询 Last-Modified（AI 改盘后），干净 tab 自动重载，dirty tab 只提示。
  */

  let { active = false }: { active?: boolean } = $props()

  let tabs = $state<ResTab[]>([])
  let current = $state('')
  let pendingClose = $state('')
  let sending = $state(false)
  let textViewer = $state<TextViewer>()

  const tab = $derived(tabs.find((t) => t.key === current))
  const dirty = $derived(!!tab && tab.kind !== 'image' && tab.content !== tab.saved)
  const textish = $derived(!!tab && ['text', 'markdown', 'html'].includes(tab.kind))
  const imageTabs = $derived(tabs.filter((t) => t.kind === 'image'))

  function blankTab(part: Partial<ResTab>): ResTab {
    return {
      key: '', kind: 'text', name: '', content: '', saved: '',
      loading: false, err: '', warn: '', savedAt: '', lastMod: '',
      ...part,
    }
  }

  /* 外部定位请求（supper_url file:// 点击 / 资源入口）：文件按需即开 */
  $effect(() => {
    const want = store.fileFocus
    if (!want) return
    store.fileFocus = ''
    void openTab(want)
  })

  /* 画板草稿（画笔钮/草稿 chip 续编）：草稿 tab 全局唯一，重进即以
  chip 当前内容重建（draftSeq 驱动 {#key}） */
  $effect(() => {
    const d = store.draftOpen
    if (!d) return
    store.draftOpen = null
    const node = blankTab({
      key: 'draft', kind: 'image', name: '画板草稿',
      draftSource: d.source, draftTag: d.tag, draftSeq: d.seq,
    })
    const i = tabs.findIndex((t) => t.key === 'draft')
    if (i >= 0) tabs[i] = node
    else tabs = [...tabs, node]
    current = 'draft'
  })

  /* 磁盘变更轮询：仅资源页激活且当前 tab 是文本系时进行 */
  $effect(() => {
    if (!active) return
    const id = setInterval(() => void pollDisk(), 2000)
    return () => clearInterval(id)
  })

  async function openTab(path: string) {
    const kind = kindOf(path)
    const key = normFileKey(path)
    if (tabs.some((t) => t.key === key)) {
      current = key
      return
    }
    const t = blankTab({ key, kind, path: path.trim(), name: fileBaseName(path.trim()) })
    tabs = [...tabs, t]
    current = key
    if (kind === 'text' || kind === 'markdown' || kind === 'html') {
      /* 必须从 $state 数组取代理版：闭包持有原始对象的话，await 后的
      属性写入不触发响应（loading 永不消失） */
      const cur = tabs[tabs.length - 1]
      cur.loading = true
      try {
        const r = await fetch('/api/workspace/file?path=' + encodeURIComponent(cur.path!), { cache: 'no-store' })
        if (!r.ok) {
          cur.err = `文件读取失败（${r.status}）——仅支持工作目录内的文件`
          return
        }
        const clen = Number(r.headers.get('content-length') || 0)
        const text = await r.text()
        if (clen > MAX_BYTES || text.length > MAX_BYTES) {
          cur.err = '文件过大（>2MB），不支持在线编辑'
          return
        }
        if (text.includes('\uFFFD')) {
          cur.warn = '文件疑似非 UTF-8 编码：显示可能乱码，保存后会转为 UTF-8'
        }
        cur.content = text
        cur.saved = text
        cur.lastMod = r.headers.get('last-modified') || ''
      } catch (e) {
        cur.err = `文件读取失败：${(e as Error).message}`
      } finally {
        cur.loading = false
      }
    }
  }

  /* 干净 tab 磁盘有更新时自动重载（AI 改盘后）；dirty 只提示不覆盖 */
  async function pollDisk() {
    const t = tab
    if (!t || !t.path || t.kind !== 'text' || t.loading || t.err) return
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
      const st = textViewer?.scrollTop() ?? 0
      t.content = text
      t.saved = text
      t.savedAt = new Date().toTimeString().slice(0, 5)
      requestAnimationFrame(() => textViewer?.restoreScroll(st))
    } catch {
      /* 轮询失败静默，下轮再试 */
    }
  }

  function closeTab(key: string) {
    const t = tabs.find((x) => x.key === key)
    if (!t) return
    if (t.kind !== 'image' && t.content !== t.saved) {
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

  async function saveTab(t: ResTab): Promise<boolean> {
    if (!t.path || t.loading || t.content === t.saved) return true
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

  /* dirty 关闭确认的「保存并关闭」 */
  async function saveAndClose(key: string) {
    const t = tabs.find((x) => x.key === key)
    if (!t) return
    if (!(await saveTab(t))) return
    removeTab(key)
  }

  /* 添加到对话（文本系）：纯文件引用 chip 塞进输入框（发送时经
  <reference_file> 告知模型，AI read_file 真身）；dirty 先自动保存 */
  async function addToChat() {
    const t = tab
    if (!t?.path || sending || t.loading) return
    if (t.content !== t.saved && !(await saveTab(t))) {
      t.err = t.err || '尚未保存成功，未添加'
      return
    }
    sending = true
    try {
      store.pendingFileRef = { path: t.path, items: [] }
    } finally {
      sending = false
    }
  }
</script>

<div class="file-pane">
  <div class="strip">
    {#each tabs as t (t.key)}
      <button class="strip-item" class:active={current === t.key} onclick={() => (current = t.key)} title={t.path || '画板草稿（未保存）'}>
        <span class="kind">{t.kind === 'image' ? '🖼' : t.kind === 'markdown' ? '📝' : t.kind === 'html' ? '🌐' : t.kind === 'pdf' ? '📕' : '📄'}</span>
        <span class="name">{t.name}</span>
        {#if t.kind !== 'image' && t.content !== t.saved}<i class="dirty"></i>{/if}
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

  <div class="body">
    <!-- 文本系：单编辑器 + tab 状态数据化（单实例重绑） -->
    {#if tab && textish}
      {#if tab.kind === 'text'}
        <TextViewer bind:this={textViewer} {tab} {active} onSave={() => void saveTab(tab)} />
      {:else if tab.kind === 'markdown'}
        <MarkdownViewer {tab} {active} onSave={() => void saveTab(tab)} />
      {:else if tab.kind === 'html'}
        <HtmlViewer {tab} {active} onSave={() => void saveTab(tab)} />
      {/if}
    {:else if tab && tab.kind === 'pdf'}
      <PdfViewer {tab} />
    {:else if tab && tab.kind === 'fallback'}
      <FallbackViewer {tab} />
    {:else if !imageTabs.length}
      <div class="empty">
        <p>还没有打开的资源</p>
        <p class="hint">对话中出现 <code>📄 文件名</code> 入口时点击即可打开 · 🖌 可从输入框画笔新建图片草稿</p>
      </div>
    {/if}
    <!-- 图片 tab：per-tab 实例保活（画板画布状态不可数据化） -->
    {#each imageTabs as t (t.key)}
      <div class="img-slot" class:hidden={current !== t.key}>
        {#key t.draftSeq ?? 0}
          <ImageViewer tab={t} active={active && current === t.key} />
        {/key}
      </div>
    {/each}
  </div>

  {#if tab && textish}
    <!-- 统一状态条（文本系）：保存 + 添加到对话 + 轻量状态 -->
    <div class="statusbar">
      <button class="savebtn" class:dirty disabled={!dirty || tab.loading} onclick={() => void saveTab(tab)} title="保存（Ctrl+S）">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z" />
          <polyline points="17 21 17 13 7 13 7 21" />
          <polyline points="7 3 7 8 15 8" />
        </svg>
        {#if dirty}<i class="dot"></i>{/if}
      </button>
      <button class="addbtn" disabled={sending || tab.loading} onclick={() => void addToChat()} title="把文件作为引用添加到输入框（发送时经 <reference_file> 告知 AI，AI 自行 read_file 真身）">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <path d="M12 5v14M5 12h14" />
        </svg>
        {sending ? '添加中…' : '添加到对话'}
      </button>
      <span class="stat">
        {#if tab.err}<i class="bad">{tab.err}</i>
        {:else if tab.warn}<i class="bad">{tab.warn}</i>
        {:else if dirty}<i>未保存</i>
        {:else if tab.savedAt}<i>已保存 {tab.savedAt}</i>
        {/if}
      </span>
      <span class="tip">
        {tab.kind === 'text' ? '编辑后 Ctrl+S 保存写回' : '可切换预览/原文'}
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
  .body {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .body > :global(*) {
    flex: 1;
    min-height: 0;
  }
  .img-slot {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
  }
  .img-slot.hidden {
    display: none;
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
  .kind {
    flex: none;
    font-size: 12px;
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
  /* dirty 关闭二次确认条 */
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
  .empty {
    flex: 1;
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
  /* 统一状态条 */
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
  /* 添加到对话：保存钮旁的常驻动作钮 */
  .addbtn {
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
  .addbtn svg {
    width: 12px;
    height: 12px;
  }
  .addbtn:hover:not(:disabled) {
    background: var(--accent-soft, color-mix(in srgb, #2563eb 10%, transparent));
  }
  .addbtn:disabled {
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
