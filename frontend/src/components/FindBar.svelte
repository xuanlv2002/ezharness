<script lang="ts">
  import { tick } from 'svelte'

  /*
  Ctrl+F 页面内查找——纯 DOM 自绘高亮（浏览器 Ctrl+F 的自实现）：
  遍历页面文本节点，匹配的字拆分包裹高亮 span，当前项深色并居中滚动。
  不用 Electron findInPage（它会抢文档焦点、有会话竞态，维护出过
  连串事故）；纯 DOM 无任何焦点把戏，desktop/web 同一路径。
  已知边界：xterm/共享浏览器是 canvas/独立视图不在本页 DOM，搜不到
  （浏览器对 canvas 同样搜不到）；折叠工具组内的文字不在 DOM。
  */
  let open = $state(false)
  let query = $state('')
  let ordinal = $state(0) // 当前匹配序号（1 基）
  let matches = $state(0) // 总匹配数
  let inputEl: HTMLInputElement | undefined = $state()

  /* ── 高亮引擎 ── */
  let marks: HTMLSpanElement[] = []
  let cur = -1

  /* 还原：把所有高亮 span 换回纯文本并合并（断链的——流式渲染把节点
  重建了——直接丢弃） */
  function restore() {
    for (const m of marks) {
      if (m.isConnected && m.parentNode) {
        const parent = m.parentNode
        parent.replaceChild(document.createTextNode(m.textContent || ''), m)
        parent.normalize()
      }
    }
    marks = []
    cur = -1
  }

  /* 跳过的子树：脚本样式、表单控件、可编辑区、查找栏自身 */
  const SKIP = 'script, style, noscript, textarea, input, select, [contenteditable], [data-findbar]'

  function textNodes(root: Node): Text[] {
    const out: Text[] = []
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT, {
      acceptNode(n) {
        const p = (n as Text).parentElement
        if (!p || p.closest(SKIP)) return NodeFilter.FILTER_REJECT
        return NodeFilter.FILTER_ACCEPT
      },
    })
    for (let n = walker.nextNode(); n; n = walker.nextNode()) out.push(n as Text)
    return out
  }

  /* 全文高亮：先收集文本节点再拆（拆分会改变树，不能边走边改） */
  function highlightAll(q: string) {
    restore()
    const needle = q.trim().toLowerCase()
    matches = 0
    ordinal = 0
    if (!needle) return
    for (const node of textNodes(document.body)) {
      const text = node.data
      const low = text.toLowerCase()
      if (!low.includes(needle)) continue
      const frag = document.createDocumentFragment()
      let i = 0
      let hit = low.indexOf(needle)
      while (hit >= 0) {
        if (hit > i) frag.appendChild(document.createTextNode(text.slice(i, hit)))
        const span = document.createElement('span')
        span.className = 'ezfh'
        span.textContent = text.slice(hit, hit + needle.length)
        frag.appendChild(span)
        marks.push(span)
        i = hit + needle.length
        hit = low.indexOf(needle, i)
      }
      if (i < text.length) frag.appendChild(document.createTextNode(text.slice(i)))
      node.parentNode?.replaceChild(frag, node)
    }
    matches = marks.length
  }

  /* 从视口顶往下找第一个命中（浏览器语义：新搜索定位到最近匹配） */
  function firstFromView(): number {
    for (let k = 0; k < marks.length; k++) {
      if (!marks[k].isConnected) continue
      if (marks[k].getBoundingClientRect().top >= -60) return k
    }
    return 0
  }

  function setCurrent(i: number, smooth = true) {
    if (!marks.length) {
      cur = -1
      ordinal = 0
      return
    }
    cur = ((i % marks.length) + marks.length) % marks.length
    for (let k = 0; k < marks.length; k++) marks[k].classList.toggle('ezfh-cur', k === cur)
    const m = marks[cur]
    if (m.isConnected) {
      m.scrollIntoView({ block: 'center', behavior: smooth ? 'smooth' : 'auto' })
    } else {
      /* 流式渲染把命中节点重建了：重查一次再定位 */
      highlightAll(query)
      setCurrent(Math.min(cur, Math.max(marks.length - 1, 0)), smooth)
      return
    }
    ordinal = cur + 1
    matches = marks.length
  }

  /* ── 交互 ── */
  let findTimer: ReturnType<typeof setTimeout> | undefined
  function runFind(smooth = false) {
    clearTimeout(findTimer)
    findTimer = setTimeout(() => {
      highlightAll(query)
      if (marks.length) setCurrent(firstFromView(), smooth)
    }, 60)
  }

  function onInput(e: Event) {
    query = (e.currentTarget as HTMLInputElement).value
    runFind()
  }

  function step(back = false) {
    if (!marks.length) return
    setCurrent(back ? cur - 1 : cur + 1)
  }

  function openBar() {
    open = true
    requestAnimationFrame(() => {
      inputEl?.focus()
      inputEl?.select()
      if (query.trim()) runFind()
    })
  }

  function close() {
    open = false
    clearTimeout(findTimer)
    restore()
    matches = 0
    ordinal = 0
    inputEl?.blur()
  }

  /* 全局键（capture + stopImmediatePropagation）：Ctrl+F 一律归这里；
  Esc 只关查找栏，不连坐抽屉的 Esc；F3/Shift+F3 前后跳 */
  function onWinKey(e: KeyboardEvent) {
    if ((e.ctrlKey || e.metaKey) && !e.altKey && e.key.toLowerCase() === 'f') {
      e.preventDefault()
      e.stopImmediatePropagation()
      openBar()
    } else if (open && e.key === 'Escape') {
      e.preventDefault()
      e.stopImmediatePropagation()
      close()
    } else if (open && e.key === 'F3') {
      e.preventDefault()
      e.stopImmediatePropagation()
      step(e.shiftKey)
    }
  }

  $effect(() => {
    window.addEventListener('keydown', onWinKey, { capture: true })
    return () => window.removeEventListener('keydown', onWinKey, { capture: true })
  })

  function onInputKey(e: KeyboardEvent) {
    if (e.isComposing) return // 中文输入法回车选词不是"下一个"
    if (e.key === 'Enter') {
      e.preventDefault()
      step(e.shiftKey)
    } else if (e.key === 'ArrowDown') {
      e.preventDefault()
      step(false)
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      step(true)
    } else if (e.key === 'Escape') {
      e.preventDefault()
      close()
    }
  }

  /* 组件卸载兜底清高亮 */
  $effect(() => {
    return () => restore()
  })
</script>

{#if open}
  <div class="findbar" role="search" data-findbar>
    <svg class="ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
      <circle cx="11" cy="11" r="7" />
      <path d="M20 20l-4-4" />
    </svg>
    <input
      bind:this={inputEl}
      value={query}
      oninput={onInput}
      onkeydown={onInputKey}
      placeholder="页面内搜索…"
      spellcheck="false"
    />
    <span class="cnt">{query.trim() ? (matches ? `${ordinal}/${matches}` : '无结果') : ''}</span>
    <button class="nav" onclick={() => step(true)} disabled={!matches} title="上一个（↑ / Shift+Enter）">↑</button>
    <button class="nav" onclick={() => step(false)} disabled={!matches} title="下一个（↓ / Enter / F3）">↓</button>
    <button class="nav" onclick={close} title="关闭（Esc）">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
        <path d="M6 6l12 12M18 6L6 18" />
      </svg>
    </button>
  </div>
{/if}

<style>
  .findbar {
    position: absolute;
    top: 12px;
    left: 50%;
    transform: translateX(-50%);
    z-index: 8;
    display: flex;
    align-items: center;
    gap: 6px;
    max-width: min(560px, 60%);
    padding: 6px 8px;
    background: var(--glass);
    backdrop-filter: var(--glass-blur);
    border: 1px solid var(--line);
    border-radius: 12px;
    box-shadow: 0 4px 16px rgb(0 0 0 / 8%);
    animation: float-in var(--dur-fast) var(--ease-out) both;
  }
  .ico {
    flex: none;
    width: 13px;
    height: 13px;
    color: var(--faint);
  }
  input {
    width: 170px;
    border: none;
    outline: none;
    background: transparent;
    font-family: inherit;
    font-size: 13px;
    color: var(--fg);
  }
  input::placeholder {
    color: var(--faint);
  }
  .cnt {
    flex: none;
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--muted);
    white-space: nowrap;
  }
  .nav {
    flex: none;
    display: grid;
    place-items: center;
    width: 24px;
    height: 24px;
    border: none;
    border-radius: 7px;
    background: transparent;
    color: var(--muted);
    font-size: 13px;
    line-height: 1;
    cursor: pointer;
    transition:
      background var(--dur-fast) var(--ease-out),
      color var(--dur-fast) var(--ease-out);
  }
  .nav:hover:not(:disabled) {
    background: var(--line);
    color: var(--fg);
  }
  .nav:disabled {
    opacity: 0.35;
    cursor: default;
  }
  .nav svg {
    width: 11px;
    height: 11px;
  }
</style>
