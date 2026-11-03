<script lang="ts">
  /*
  变更对比视图（文本系 + git 仓库内）：融合（unified）单列流水——HEAD
  原文与当前编辑缓冲（未保存改动也实时可见）合成一条流，变更块先删后增
  （git diff 的阅读习惯），上下文行原位穿插。只读观测——不提供任何 git
  操作；编辑/保存仍走原文本查看器路径。
  */

  import { api } from '../../../lib/api'
  import type { ResTab } from '../registry'
  import { diffRows, diffStats, unifyRows } from '../../../lib/linediff'

  let { tab }: { tab: ResTab } = $props()

  let old = $state<string | null>(null) // null = 加载中；'' = 新文件（未纳入 HEAD）
  let err = $state('')

  /* HEAD 原文只在打开/换 tab 时取一次（HEAD 不随磁盘编辑移动） */
  $effect(() => {
    const p = tab.path
    old = null
    err = ''
    if (!p) return
    api
      .gitBlob(p)
      .then((t) => (old = t))
      .catch((e) => (err = `HEAD 版本读取失败：${(e as Error).message}`))
  })

  const diff = $derived(old === null ? null : diffRows(old, tab.content))
  const lines = $derived(diff ? unifyRows(diff.rows) : [])
  const stats = $derived(diff ? diffStats(diff.rows) : { add: 0, del: 0 })
  const fresh = $derived(old === '') // 新文件：整篇都是新增
</script>

<div class="diff">
  <div class="dhead">
    <span class="htitle">变更对比</span>
    <span class="hsides"><s>HEAD（上次提交）</s>↔<b>当前内容</b></span>
    {#if !err && old !== null}
      <span class="hstat"><i class="add">+{stats.add}</i><i class="del">−{stats.del}</i></span>
    {/if}
    {#if fresh}<span class="hnote">新文件：尚未提交过</span>{/if}
    {#if diff?.truncated}<span class="hnote">过大：只对比前 6000 行</span>{/if}
    {#if err}<span class="hnote bad">{err}</span>{/if}
  </div>
  {#if old === null && !err}
    <div class="dveil">加载 HEAD 版本…</div>
  {:else}
    <div class="sheet">
      {#each lines as l}
        <div class="uline {l.t}">
          <span class="ln">{l.n || ''}</span>
          <span class="mark">{l.t === 'del' ? '−' : l.t === 'add' ? '+' : ''}</span>
          <span class="tx">{l.text}</span>
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .diff {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    border: 1px solid var(--line);
    border-radius: 12px;
    background: var(--card);
    overflow: hidden;
  }
  .dhead {
    flex: none;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 7px 12px;
    border-bottom: 1px solid var(--line);
    font-size: var(--font-size-ui);
    color: var(--muted);
    background: var(--bg-soft);
    white-space: nowrap;
    overflow: hidden;
  }
  .htitle {
    font-weight: 700;
    color: var(--fg);
  }
  .hsides {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .hsides s {
    color: var(--faint);
    text-decoration: none;
  }
  .hstat {
    display: flex;
    gap: 6px;
    font-family: var(--font-mono);
  }
  .hstat .add {
    color: #1a7f37;
    font-style: normal;
  }
  .hstat .del {
    color: #cf222e;
    font-style: normal;
  }
  .hnote {
    color: var(--faint);
  }
  .hnote.bad {
    color: #e07a4f;
  }
  .dveil {
    flex: 1;
    display: grid;
    place-items: center;
    color: var(--faint);
    font-size: var(--font-size-section);
  }
  .sheet {
    flex: 1;
    min-height: 0;
    overflow: auto;
    scrollbar-width: thin;
    scrollbar-color: var(--line-strong) transparent;
    font-family: var(--font-mono);
    font-size: var(--font-size-section);
    line-height: 1.55;
  }
  .sheet::-webkit-scrollbar {
    width: 8px;
    height: 8px;
  }
  .sheet::-webkit-scrollbar-thumb {
    background: var(--line-strong);
    border-radius: 4px;
  }
  .sheet::-webkit-scrollbar-track {
    background: transparent;
  }
  /* 融合行：行号 | 标记 | 内容；行很长折行显示，不横向滚 */
  .uline {
    display: flex;
    align-items: flex-start;
    min-width: 0;
  }
  .ln {
    flex: none;
    width: 44px;
    padding-right: 8px;
    text-align: right;
    color: var(--faint);
    user-select: none;
    background: var(--bg-soft);
  }
  .mark {
    flex: none;
    width: 16px;
    text-align: center;
    user-select: none;
    font-weight: 700;
  }
  .tx {
    flex: 1;
    min-width: 0;
    padding-right: 12px;
    white-space: pre-wrap;
    word-break: break-all;
  }
  /* 增删行文字走主题变量（浅色深字，暗色翻亮色），标记符保持饱和色 */
  .uline.del {
    background: color-mix(in srgb, #cf222e 9%, transparent);
  }
  .uline.del .mark,
  .uline.del .tx {
    color: var(--diff-del-fg);
  }
  .uline.del .mark {
    color: #cf222e;
  }
  :global(html[data-theme='dark']) .uline.del .mark {
    color: #ff7b72;
  }
  .uline.add {
    background: color-mix(in srgb, #1a7f37 9%, transparent);
  }
  .uline.add .mark,
  .uline.add .tx {
    color: var(--diff-add-fg);
  }
  .uline.add .mark {
    color: #1a7f37;
  }
  :global(html[data-theme='dark']) .uline.add .mark {
    color: #7ee787;
  }
</style>
