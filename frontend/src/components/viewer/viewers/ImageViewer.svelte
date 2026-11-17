<script lang="ts">
  /* 图片查看器（画板）：只在内存编辑，Ctrl+S 写盘；保存前校验磁盘指纹 */

  import { onMount } from 'svelte'
  import { api } from '../../../lib/api'
  import { fileBaseName } from '../../../lib/textfile'
  import { getFileStamp, getStamp } from '../../../lib/fileStamp'
  import { store } from '../../../lib/store.svelte'
  import type { ResTab } from '../registry'
  import MagicBoard from './MagicBoard.svelte'

  let {
    tab,
    active = false,
    askOverwrite,
  }: {
    tab: ResTab
    active?: boolean
    askOverwrite?: (name: string) => Promise<boolean>
  } = $props()

  let board = $state<MagicBoard>()
  let srcFile = $state<File | null>(null)
  let loading = $state(false)
  let err = $state('')
  let saving = $state(false)

  /* SVG 不原位写回：画板导出 PNG，首次保存另存 tmp PNG 草稿 */
  const isSvg = (tab.path || '').toLowerCase().endsWith('.svg')
  const isDraft = $derived(!tab.path && !!tab.draftSrc)
  const canSave = $derived(!!tab.imageDirty || (!tab.path && !!tab.draftSrc))
  let realPath = $state(tab.savePath || '')

  onMount(() => void load())

  async function load() {
    const file = realPath || tab.path || ''
    if (!file && !tab.draftSrc) return
    err = ''
    loading = true
    try {
      const url = file ? '/api/workspace/file?path=' + encodeURIComponent(file) : tab.draftSrc!
      const r = await fetch(url, { cache: 'no-store' })
      if (!r.ok) throw new Error(`HTTP ${r.status}`)
      const blob = await r.blob()
      await checkImage(blob)
      srcFile = new File([blob], fileBaseName(file || '画板草稿.png'), { type: blob.type || 'image/png' })
      /* 带回的未保存画布保留原基线，保存时才能发现外部改动 */
      if (!tab.imageDirty) tab.diskStamp = getStamp(r)
    } catch (e) {
      err = `图片加载失败：${(e as Error).message}`
    } finally {
      loading = false
    }
  }

  /* 预验证浏览器能解码（SVG 语法错误等漏到画板会让 init 永久挂起） */
  function checkImage(blob: Blob): Promise<void> {
    const url = URL.createObjectURL(blob)
    return new Promise<void>((resolve, reject) => {
      const img = new Image()
      img.onload = () => resolve()
      img.onerror = () => reject(new Error('浏览器无法解码该图片（损坏或 SVG 含错误/外链）'))
      img.src = url
    }).finally(() => URL.revokeObjectURL(url))
  }

  /* 保存前校验磁盘指纹：被外部改过就弹窗问是否覆盖 */
  async function checkDisk(target: string): Promise<boolean> {
    if (!target || !tab.diskStamp) return true
    const stamp = await getFileStamp(target)
    if (stamp && stamp !== tab.diskStamp) return (await askOverwrite?.(tab.name)) ?? false
    return true
  }

  /* 写盘：草稿/SVG 首次 stash 成 tmp PNG，其余原位写回；返回落盘路径 */
  async function saveFile(file: File): Promise<string> {
    if (!(await checkDisk(realPath || (isDraft || isSvg ? '' : tab.path || '')))) return ''
    try {
      let p = realPath
      if (!p && (isDraft || isSvg)) p = (await api.stash([file]))[0] ?? ''
      else {
        p = p || tab.path || ''
        if (p) await api.saveBin(p, file)
      }
      if (!p) throw new Error('未取到落盘路径')
      realPath = realPath || p
      tab.savePath = realPath
      tab.path = tab.path || p
      if (tab.draftSrc && tab.name === '画板草稿') tab.name = fileBaseName(p)
      tab.draftSrc = undefined
      tab.diskStamp = (await getFileStamp(p)) ?? ''
      tab.savedAt = new Date().toTimeString().slice(0, 5)
      store.bumpImg(p)
      return p
    } catch (e) {
      store.lastStatus = `图片保存失败：${(e as Error).message}`
      return ''
    }
  }

  /* 保存后把合成图换底并清空元素，避免下次导出重复叠加 */
  async function updateAfterSave(file: File) {
    tab.imageDirty = false
    tab.board = undefined
    await board?.resetBase(file).catch(() => {})
  }

  /* Ctrl+S / 保存钮：导出当前画布并写盘 */
  async function save(): Promise<boolean> {
    if (!board || saving || !canSave) return false
    saving = true
    try {
      const file = await board.exportFile()
      const p = await saveFile(file)
      if (!p) return false
      await updateAfterSave(file)
      return true
    } finally {
      saving = false
    }
  }

  /* 画布改动只进内存，标记未保存 */
  function onBoardChange() {
    tab.imageDirty = true
    tab.board = board?.getElements()
  }

  function onKey(e: KeyboardEvent) {
    if (!active) return
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's' && !e.isComposing) {
      e.preventDefault()
      void save()
    }
  }

  /* 添加到对话：有改动/草稿先保存，再回流路径 */
  async function addToChat(f: File) {
    if ((!tab.path && !tab.draftSrc) || saving) return
    const p = realPath || (isDraft || isSvg ? '' : tab.path || '')
    if (p && !tab.imageDirty) {
      store.pendingAttachments = [{ name: fileBaseName(p), path: p }]
      return
    }
    saving = true
    try {
      const saved = await saveFile(f)
      if (!saved) return
      await updateAfterSave(f)
      store.pendingAttachments = [{ name: fileBaseName(saved), path: saved }]
    } finally {
      saving = false
    }
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
      <MagicBoard
        bind:this={board}
        {active}
        source={srcFile}
        initial={tab.board ?? []}
        doneLabel="添加到对话"
        onDone={(f) => void addToChat(f)}
        onChange={onBoardChange}
      />
    </div>
  {/if}
  {#if tab.path || tab.draftSrc}
    <!-- 状态条（与文本系统一）：保存写回 + 未保存标记 -->
    <div class="statusbar">
      <button
        class="savebtn"
        class:dirty={tab.imageDirty}
        disabled={saving || !!err || loading || !canSave}
        onclick={() => void save()}
        title="保存（Ctrl+S）"
      >
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z" />
          <polyline points="17 21 17 13 7 13 7 21" />
          <polyline points="7 3 7 8 15 8" />
        </svg>
        {#if tab.imageDirty}<i class="dot"></i>{/if}
      </button>
      <span class="stat">
        {#if saving}
          <i>保存中…</i>
        {:else if tab.imageDirty}
          <i>未保存</i>
        {:else if tab.savedAt}
          <i>已保存 {tab.savedAt}</i>
        {:else if isDraft}
          <i>内存草稿 · Ctrl+S 保存</i>
        {:else if isSvg}
          <i>SVG 另存为 PNG · Ctrl+S</i>
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
