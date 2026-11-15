<script lang="ts">
  /*
  变更对比视图（文本系 + git 仓库内）：融合（unified）单列流水——HEAD
  原文与当前编辑缓冲（未保存改动也实时可见）合成一条流，变更块先删后增
  （git diff 的阅读习惯），上下文行原位穿插。只读观测——不提供任何 git
  操作；编辑/保存仍走原文本查看器路径。
  右侧概览轨：接管纵向滚动条——轨道上只刻增/删行位置（细色条），滑块
  即可视窗；点击/拖拽直接跳读，长文件不必全量滚动找改动。
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

  /* ── 概览轨：只取增/删行连续段（同色合并；上下文行不画——整条文件
     纹理糊上去正是上一版显脏的来源），top/height 按行占比渲染 ── */
  const segs = $derived.by(() => {
    const out: { t: 'same' | 'add' | 'del'; start: number; count: number }[] = []
    lines.forEach((l, i) => {
      const last = out[out.length - 1]
      if (last && last.t === l.t) last.count++
      else out.push({ t: l.t, start: i, count: 1 })
    })
    return out.filter((s) => s.t !== 'same')
  })

  let sheetEl = $state<HTMLDivElement>()
  let viewTop = $state(0) // 可视窗顶（滚动条轨道占比，0..1-viewH）
  let viewH = $state(1) // 可视窗高（轨道占比）

  function syncView() {
    const el = sheetEl
    if (!el || !el.scrollHeight) return
    const h = Math.min(1, el.clientHeight / el.scrollHeight)
    const max = el.scrollHeight - el.clientHeight
    viewH = h
    viewTop = max > 0 ? Math.min(1 - h, Math.max(0, (el.scrollTop / max) * (1 - h))) : 0
  }

  /* 行集变化后内容高度变了（换 tab / 编辑联动），下一帧重算可视窗 */
  $effect(() => {
    void lines.length
    requestAnimationFrame(syncView)
  })

  /* 抽屉/窗口改宽高时可视窗比例也变：跟着容器尺寸走 */
  $effect(() => {
    const el = sheetEl
    if (!el) return
    const ro = new ResizeObserver(syncView)
    ro.observe(el)
    return () => ro.disconnect()
  })

  /* 点击/拖拽跳读：点在可视窗内按抓取偏移拖动，点在轨道空白处跳到
     点按位置居中（与原生滚动条手感一致） */
  let dragging = false
  let dragOff = 0 // 抓取点到可视窗顶的内容像素偏移

  function contentY(e: PointerEvent, el: HTMLElement): number {
    const r = el.getBoundingClientRect()
    const f = Math.min(1, Math.max(0, (e.clientY - r.top) / r.height))
    return f * (sheetEl?.scrollHeight ?? 0)
  }

  function scrollToY(y: number) {
    const el = sheetEl
    if (!el) return
    const max = el.scrollHeight - el.clientHeight
    el.scrollTop = Math.min(max, Math.max(0, y))
  }

  function onRailDown(e: PointerEvent) {
    const el = e.currentTarget as HTMLElement
    const sh = sheetEl
    if (!sh) return
    el.setPointerCapture(e.pointerId)
    dragging = true
    const y = contentY(e, el)
    if (y >= sh.scrollTop && y <= sh.scrollTop + sh.clientHeight) {
      dragOff = y - sh.scrollTop
    } else {
      dragOff = sh.clientHeight / 2
      scrollToY(y - dragOff)
    }
  }

  function onRailMove(e: PointerEvent) {
    if (!dragging) return
    scrollToY(contentY(e, e.currentTarget as HTMLElement) - dragOff)
  }

  function onRailUp() {
    dragging = false
  }
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
    <div class="dwrap">
      <div class="sheet" bind:this={sheetEl} onscroll={syncView}>
        {#each lines as l}
          <div class="uline {l.t}">
            <span class="ln">{l.n || ''}</span>
            <span class="mark">{l.t === 'del' ? '−' : l.t === 'add' ? '+' : ''}</span>
            <span class="tx">{l.text}</span>
          </div>
        {/each}
      </div>
      <!-- 概览轨：替代原生纵向滚动条——增删刻度 + 可视窗滑块，点击/拖拽跳读 -->
      {#if lines.length}
        <div
          class="rail"
          class:dragging
          role="scrollbar"
          aria-label="变更分布与滚动"
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={Math.round(viewTop * 100)}
          onpointerdown={onRailDown}
          onpointermove={onRailMove}
          onpointerup={onRailUp}
          onpointercancel={onRailUp}
        >
          <div class="mview" style="top:{viewTop * 100}%; height:{viewH * 100}%"></div>
          {#each segs as s (s.t + s.start)}
            <i
              class="tick {s.t}"
              style="top:{(s.start / lines.length) * 100}%; height:{Math.max((s.count / lines.length) * 100, 0.4)}%"
            ></i>
          {/each}
        </div>
      {/if}
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
  /* 正文 + 概览轨同排：轨接管纵向滚动（原生滚动条隐去，避免两条并排） */
  .dwrap {
    flex: 1;
    min-height: 0;
    display: flex;
    padding: 6px 6px 6px 0;
  }
  .sheet {
    flex: 1;
    min-width: 0;
    min-height: 0;
    overflow: auto;
    scrollbar-width: none;
    font-family: var(--font-mono);
    font-size: var(--font-size-section);
    line-height: 1.55;
  }
  .sheet::-webkit-scrollbar {
    display: none;
  }
  /* ── 概览轨：细长滚动条槽，悬停/拖动才浮出底色 ── */
  .rail {
    position: relative;
    flex: none;
    width: 14px;
    margin-left: 4px;
    border-radius: 7px;
    user-select: none;
    cursor: pointer;
    touch-action: none;
  }
  .rail::before {
    content: '';
    position: absolute;
    inset: 0;
    border-radius: inherit;
    background: color-mix(in srgb, var(--fg) 5%, transparent);
    opacity: 0;
    transition: opacity var(--dur-fast) var(--ease-out);
  }
  .rail:hover::before,
  .rail.dragging::before {
    opacity: 1;
  }
  /* 滑块即可视窗：半透明圆角条，拖动跳读 */
  .mview {
    position: absolute;
    left: 2px;
    right: 2px;
    min-height: 14px;
    border-radius: 5px;
    background: color-mix(in srgb, var(--fg) 13%, transparent);
    pointer-events: none;
    transition: background var(--dur-fast) var(--ease-out);
  }
  .rail:hover .mview,
  .rail.dragging .mview {
    background: color-mix(in srgb, var(--fg) 26%, transparent);
  }
  /* 增/删刻度：压住滑块的细色条，一眼看出改动分布 */
  .tick {
    position: absolute;
    left: 3px;
    right: 3px;
    min-height: 2px;
    border-radius: 1px;
    pointer-events: none;
  }
  .tick.add {
    background: color-mix(in srgb, #1a7f37 78%, transparent);
  }
  .tick.del {
    background: color-mix(in srgb, #cf222e 78%, transparent);
  }
  :global(html[data-theme='dark']) .tick.add {
    background: color-mix(in srgb, #7ee787 82%, transparent);
  }
  :global(html[data-theme='dark']) .tick.del {
    background: color-mix(in srgb, #ff7b72 82%, transparent);
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
