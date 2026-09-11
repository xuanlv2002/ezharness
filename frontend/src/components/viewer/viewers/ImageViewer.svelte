<script lang="ts">
  /*
  图片查看器（画板）：MagicBoard 的资源化包装。有 path = 编辑真身
  （一切皆资源：保存 Ctrl+S 直接写回原路径，缩略图版本 bump 即刻刷新
  ——历史 chips 同步更新，引用同一资源处处显示最新；「添加到对话」
  只引用路径不写盘，想改盘点保存）；无 path = 画板草稿（「添加到
  对话」成内存 chip，发送时才 stash 持久化，tag 匹配替换原 chip）。
  */

  import { onMount } from 'svelte'
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
    if (!tab.path) {
      srcFile = tab.draftSource ?? null
      return
    }
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

  /* 保存写回真身（Ctrl+S）：合成当前画布覆盖原路径 */
  async function save() {
    if (!tab.path || !board || saving) return
    saving = true
    try {
      const f = await board.exportFile()
      await api.saveBin(tab.path, f)
      store.bumpImg(tab.path)
      savedAt = new Date().toTimeString().slice(0, 5)
    } catch (e) {
      store.lastStatus = `图片保存失败：${(e as Error).message}`
    } finally {
      saving = false
    }
  }

  function onKey(e: KeyboardEvent) {
    if (!active) return
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's' && !e.isComposing) {
      e.preventDefault()
      void save()
    }
  }

  /* 添加到对话：真身 = 路径引用 chip（不写盘）；草稿 = 内存 chip
  （发送时才 stash），tag/source 供 ChatView 原位替换 */
  function addToChat(f: File) {
    if (tab.path) {
      store.pendingAttachments = [{ name: fileBaseName(tab.path), path: tab.path }]
    } else {
      store.pendingAttachments = [{ name: f.name, file: f, tag: tab.draftTag ?? '', source: tab.draftSource ?? null }]
    }
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
      <MagicBoard bind:this={board} {active} source={srcFile} doneLabel="添加到对话" onDone={(f) => addToChat(f)} />
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
        {#if saving}<i>保存中…</i>{:else if savedAt}<i>已写回 {savedAt}</i>{:else}<i>编辑后保存写回原文件 · 添加到对话不写盘</i>{/if}
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
