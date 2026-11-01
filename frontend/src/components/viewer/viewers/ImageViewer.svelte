<script lang="ts">
  /*
  图片查看器（画板）：MagicBoard 的资源化包装。两类来源——
  真身图片（tab.path）：画布改动防抖自动写回原路径（Ctrl+S 立即写回），
  缩略图版本 bump 即刻刷新（历史 chips 同步更新）；
  内存草稿（tab.draftSrc，画笔钮/消息 base64 入口）：只活在本 tab，
  不落盘——点开画板不产生文件；保存（Ctrl+S/写回钮）或「添加到对话」
  时才 stash 成 tmp 真身并回填 path，此后与真身图片同一条路。
  */

  import { onDestroy, onMount } from 'svelte'
  import { api } from '../../../lib/api'
  import { fileBaseName } from '../../../lib/textfile'
  import { store } from '../../../lib/store.svelte'
  import type { ResTab } from '../registry'
  import MagicBoard from './MagicBoard.svelte'

  let { tab, active = false }: { tab: ResTab; active?: boolean } = $props()

  let board = $state<MagicBoard>()
  let srcFile = $state<File | null>(null)
  let loading = $state(false)
  let err = $state('')
  let saving = $state(false)
  let savedAt = $state('')

  /* SVG 不原位写回：画板导出的是 PNG，覆盖 .svg 会把文件改成「扩展名
     svg 的 PNG 内容」，之后按 SVG 解码必失败（文件被破坏）。画笔产物
     首次落盘即分流为 tmp 下的 PNG 草稿（realPath），此后写回都写它，
     原 SVG 永不被改写 */
  const isSvg = (tab.path || '').toLowerCase().endsWith('.svg')
  const isDraft = $derived(!tab.path && !!tab.draftSrc) // 内存草稿（未落盘）
  let realPath = $state('')

  onMount(() => void load())

  /* decodeCheck 预验证浏览器可解码（onload/onerror 都挂）：SVG 语法
     损坏等解码失败若漏到画板，MagicBoard 的 init 会永久挂起 */
  function decodeCheck(blob: Blob): Promise<void> {
    const url = URL.createObjectURL(blob)
    return new Promise<void>((resolve, reject) => {
      const img = new Image()
      img.onload = () => resolve()
      img.onerror = () => reject(new Error('浏览器无法解码该图片（损坏或 SVG 含错误/外链）'))
      img.src = url
    }).finally(() => URL.revokeObjectURL(url))
  }

  async function load() {
    if (!tab.path && !tab.draftSrc) return
    err = ''
    loading = true
    try {
      const url = tab.path
        ? '/api/workspace/file?path=' + encodeURIComponent(tab.path)
        : tab.draftSrc!
      const r = await fetch(url, { cache: 'no-store' })
      if (!r.ok) throw new Error(`HTTP ${r.status}`)
      const blob = await r.blob()
      await decodeCheck(blob)
      srcFile = new File([blob], fileBaseName(tab.path || '画板草稿.png'), { type: blob.type || 'image/png' })
    } catch (e) {
      err = `图片加载失败：${(e as Error).message}`
    } finally {
      loading = false
    }
  }

  /* persist 落盘分流并返回真身路径：内存草稿 stash 成 tmp PNG（保存即
     创建文件的时刻）；普通图片原位覆盖；SVG 首次落盘 stash 成 tmp PNG
     草稿（此后 realPath 固定，原位覆盖这张草稿） */
  async function persist(f: File): Promise<string | null> {
    if (realPath) {
      await api.saveBin(realPath, f)
      return realPath
    }
    if (isDraft || isSvg) {
      const [p] = await api.stash([f])
      return p ?? null
    }
    await api.saveBin(tab.path!, f)
    return tab.path!
  }

  /* 写回真身：保存钮与「添加到对话」共用（草稿在这里首次落盘） */
  async function writeBack(f: File): Promise<boolean> {
    if ((!tab.path && !tab.draftSrc) || saving) return false
    saving = true
    try {
      const p = await persist(f)
      if (!p) throw new Error('未取到落盘路径')
      realPath = realPath || p
      tab.path = tab.path || p // 草稿就此有了真身，后续写回全部对齐
      if (tab.draftSrc && tab.name === '画板草稿') tab.name = fileBaseName(p)
      store.bumpImg(p)
      savedAt = new Date().toTimeString().slice(0, 5)
      dirty = false
      return true
    } catch (e) {
      store.lastStatus = `图片保存失败：${(e as Error).message}`
      return false
    } finally {
      saving = false
    }
  }

  /* 保存写回真身（Ctrl+S）；草稿首次保存 = 落盘成 tmp 文件 */
  async function save() {
    if ((!tab.path && !tab.draftSrc) || !board) return
    await writeBack(await board.exportFile())
  }

  /* 真身图片：改动自动写回（防抖），磁盘始终是最新真身——拖出为独立
     窗口、关窗回流、切换 tab 都不会丢未经手保存的笔画。内存草稿不自动
     写盘：只有显式保存（保存钮/Ctrl+S/添加到对话）才创建文件 */
  let saveTimer: ReturnType<typeof setTimeout> | undefined
  let dirty = false // 有改动未写回（卸载时补写、以及避免用旧画布覆盖别人写的新图）
  function scheduleSave() {
    dirty = true
    if (isDraft) return // 草稿：只标脏，不落盘
    clearTimeout(saveTimer)
    saveTimer = setTimeout(() => {
      saveTimer = undefined
      if (saving) return scheduleSave() // 上一次写回未回来：稍后重试，别丢这次改动
      void save()
    }, 400)
  }

  onDestroy(() => {
    clearTimeout(saveTimer)
    /* 关 tab / 卸载时把还在防抖窗口里的改动补写回。只在脏时写：跨窗口
       回流会重挂本组件，干净画布写回会覆盖另一个窗口刚写的新图 */
    if (!dirty) return
    const b = board
    if (!b || !tab.path || saving) return
    void b.exportFile().then((f) => persist(f)).catch(() => {})
  })

  function onKey(e: KeyboardEvent) {
    if (!active) return
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's' && !e.isComposing) {
      e.preventDefault()
      void save()
    }
  }

  /* 添加到对话：先把当前画布写回真身（草稿就此首次落盘；onDone 给的就
     是最新合成图），再回流路径 chip——不写盘会让 AI 读到旧图 */
  async function addToChat(f: File) {
    if (!tab.path && !tab.draftSrc) return
    if (!(await writeBack(f))) return
    const p = realPath || tab.path || ''
    store.pendingAttachments = [{ name: fileBaseName(p), path: p }]
  }
</script>

<svelte:window onkeydown={onKey} />

<div class="image-viewer">
  {#if err}
    <div class="veil err">
      {err}
      <button class="retry" onclick={() => void load()}>重试</button>
    </div>
  {:else if loading}
    <div class="veil">加载中…</div>
  {:else}
    <div class="canvas-host">
      <MagicBoard bind:this={board} {active} source={srcFile} doneLabel="添加到对话" onDone={(f) => void addToChat(f)} onChange={scheduleSave} />
    </div>
  {/if}
  {#if tab.path || tab.draftSrc}
    <!-- 状态条（与文本系统一风格）：保存写回 + 状态（「添加到对话」在
    画板 footer 主按钮上；草稿首次保存才落盘成文件） -->
    <div class="statusbar">
      <button class="savebtn" class:busy={saving} disabled={saving || !!err || loading} onclick={() => void save()} title={isDraft ? '保存到工作目录 tmp（Ctrl+S）——落盘成文件后方可添加到对话' : isSvg ? '另存 PNG 草稿（Ctrl+S）——原 SVG 文件不变，历史与输入框中的缩略图同步更新' : '写回原文件（Ctrl+S）——历史与输入框中的同名缩略图将同步更新'}>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z" />
          <polyline points="17 21 17 13 7 13 7 21" />
          <polyline points="7 3 7 8 15 8" />
        </svg>
      </button>
      <span class="stat">
        {#if saving}
          <i>保存中…</i>
        {:else if isDraft}
          <i>内存草稿 · 保存后落盘（添加到对话会自动保存）</i>
        {:else if savedAt && isSvg && realPath}
          <i>画笔已存 {fileBaseName(realPath)} · {savedAt}（原 SVG 不变）</i>
        {:else if savedAt}
          <i>已写回 {savedAt}</i>
        {:else if isSvg}
          <i>SVG 不原位写回 · 画笔自动另存 PNG 草稿</i>
        {:else}
          <i>改动自动写回原文件 · Ctrl+S 立即写回</i>
        {/if}
      </span>
    </div>
  {/if}
</div>

<style>
  .image-viewer {
    display: flex;
    flex-direction: column;
    gap: 8px;
    height: 100%;
    min-height: 0;
  }
  .canvas-host {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .veil {
    flex: 1;
    display: grid;
    place-items: center;
    border: 1px solid var(--line);
    border-radius: 12px;
    background: var(--bg);
    color: var(--faint);
    font-size: var(--font-size-section);
  }
  .veil.err {
    color: #e74c3c;
    padding: 0 20px;
    text-align: center;
    display: grid;
    gap: 10px;
    justify-items: center;
  }
  .veil.err .retry {
    border: 1px solid var(--line-strong);
    border-radius: 8px;
    background: var(--bg-soft);
    color: var(--fg);
    font-size: var(--font-size-ui);
    padding: 4px 14px;
    cursor: pointer;
  }
  .veil.err .retry:hover {
    border-color: var(--accent);
    color: var(--accent);
  }
  .statusbar {
    flex: none;
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 24px;
    font-size: var(--font-size-small);
    color: var(--faint);
  }
  .savebtn {
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
  .savebtn.busy {
    color: #f0883e;
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
</style>
