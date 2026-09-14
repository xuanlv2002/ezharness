<script lang="ts">
  /*
  图片查看器（画板）：MagicBoard 的资源化包装，一切皆资源——每张图都有
  真身路径（画笔钮草稿也是 tmp 下的一张图片）。画布改动防抖自动写回原
  路径（Ctrl+S 立即写回），缩略图版本 bump 即刻刷新（历史 chips 同步
  更新，引用同一资源处处显示最新）；「添加到对话」先写回再回流路径 chip。
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

  onMount(async () => {
    if (!tab.path) return
    loading = true
    try {
      const r = await fetch('/api/workspace/file?path=' + encodeURIComponent(tab.path), { cache: 'no-store' })
      if (!r.ok) throw new Error(`HTTP ${r.status}`)
      const blob = await r.blob()
      srcFile = new File([blob], fileBaseName(tab.path), { type: blob.type || 'image/png' })
    } catch (e) {
      err = `图片加载失败：${(e as Error).message}`
    } finally {
      loading = false
    }
  })

  /* 写回真身（合成图覆盖原路径）：保存钮与「添加到对话」共用 */
  async function writeBack(f: File): Promise<boolean> {
    if (!tab.path || saving) return false
    saving = true
    try {
      await api.saveBin(tab.path, f)
      store.bumpImg(tab.path)
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

  /* 保存写回真身（Ctrl+S） */
  async function save() {
    if (!tab.path || !board) return
    await writeBack(await board.exportFile())
  }

  /* 改动自动写回（防抖）：磁盘始终是最新真身——拖出为独立窗口、关窗回流、
     切换 tab 都不会丢未经手保存的笔画，也不需要跨窗口的额外状态交接 */
  let saveTimer: ReturnType<typeof setTimeout> | undefined
  let dirty = false // 有改动未写回（卸载时补写、以及避免用旧画布覆盖别人写的新图）
  function scheduleSave() {
    dirty = true
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
    const path = tab.path
    if (!b || !path || saving) return
    void b.exportFile().then((f) => api.saveBin(path, f)).catch(() => {})
  })

  function onKey(e: KeyboardEvent) {
    if (!active) return
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's' && !e.isComposing) {
      e.preventDefault()
      void save()
    }
  }

  /* 添加到对话：先把当前画布写回真身（onDone 给的就是最新合成图），
     再回流路径 chip——不写盘会让 AI 读到旧图 */
  async function addToChat(f: File) {
    if (!tab.path) return
    if (!(await writeBack(f))) return
    store.pendingAttachments = [{ name: fileBaseName(tab.path), path: tab.path }]
  }
</script>

<svelte:window onkeydown={onKey} />

<div class="image-viewer">
  {#if err}
    <div class="veil err">{err}</div>
  {:else if loading}
    <div class="veil">加载中…</div>
  {:else}
    <div class="canvas-host">
      <MagicBoard bind:this={board} {active} source={srcFile} doneLabel="添加到对话" onDone={(f) => void addToChat(f)} onChange={scheduleSave} />
    </div>
  {/if}
  {#if tab.path}
    <!-- 状态条（与文本系统一风格）：保存写回 + 状态（「添加到对话」在
    画板 footer 主按钮上） -->
    <div class="statusbar">
      <button class="savebtn" class:busy={saving} disabled={saving || !!err || loading} onclick={() => void save()} title="写回原文件（Ctrl+S）——历史与输入框中的同名缩略图将同步更新">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z" />
          <polyline points="17 21 17 13 7 13 7 21" />
          <polyline points="7 3 7 8 15 8" />
        </svg>
      </button>
      <span class="stat">
        {#if saving}<i>保存中…</i>{:else if savedAt}<i>已写回 {savedAt}</i>{:else}<i>改动自动写回原文件 · Ctrl+S 立即写回</i>{/if}
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
    font-size: 12.5px;
  }
  .veil.err {
    color: #e74c3c;
    padding: 0 20px;
    text-align: center;
  }
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
