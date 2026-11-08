<script lang="ts">
  /*
  便签正文的块编辑器：直接编辑排版后的样子，不再显示 markdown 源码。
  行首输入 `# ` / `## ` / `### ` 变标题，`[] ` / `[ ] ` / `[x] ` 变待办，
  `- ` 变列表——都要先打空格才转换；待办勾选框可直接点。回车新建同类型块
  （标题回落为正文，空待办/列表项回车退出列表），块首退格去掉块样式，
  块首再退格并回上一块。值仍以 markdown 文本与卡片同步：每次输入序列化
  回 markdown。粘贴文本按行拆分；剪贴板里的图片文件交给 onimage（由卡片
  上传并贴到卡面）。
  */
  import { onDestroy, onMount } from 'svelte'
  import { imageFiles } from '../../lib/pinImage'
  import { pinEditEnter, pinEditLeave } from '../../lib/pinboard.svelte'

  type Kind = 'p' | 'h1' | 'h2' | 'h3' | 'todo' | 'bullet'

  let {
    value = '',
    focusAt = null,
    onchange,
    onsave,
    oncancel,
    onimage,
  }: {
    value?: string
    /* 进编辑时的点击点（含渲染态正文的滚动量）：光标落在点击处，
     命不中正文块就回落到末尾 */
    focusAt?: { x: number; y: number; scrollTop?: number } | null
    onchange?: (md: string) => void
    onsave?: () => void
    oncancel?: () => void
    /* 剪贴板里的图片文件（由外部上传并贴到卡面） */
    onimage?: (files: File[]) => void
  } = $props()

  let root = $state<HTMLElement>()
  let composing = false

  function parseLine(line: string): { kind: Kind; text: string; checked: boolean } {
    let m = /^(#{1,3})\s+(.*)$/.exec(line)
    if (m) return { kind: `h${m[1].length}` as Kind, text: m[2], checked: false }
    m = /^\s*(?:[-*+]\s+)?\[([ xX]?)\](?:\s(.*))?$/.exec(line)
    if (m) return { kind: 'todo', text: m[2] ?? '', checked: m[1].toLowerCase() === 'x' }
    m = /^\s*[-*+]\s+(.*)$/.exec(line)
    if (m) return { kind: 'bullet', text: m[1], checked: false }
    return { kind: 'p', text: line, checked: false }
  }

  function bodyOf(blk: HTMLElement): HTMLElement | null {
    return blk.querySelector(':scope > .btext')
  }

  function boxOf(blk: HTMLElement): HTMLElement | null {
    return blk.querySelector(':scope > .box')
  }

  function setText(blk: HTMLElement, text: string): void {
    const body = bodyOf(blk)
    if (!body) return
    body.textContent = text
    if (!text) body.append(document.createElement('br'))
  }

  function makeBlock(kind: Kind, text: string, checked = false): HTMLElement {
    const blk = document.createElement('div')
    blk.className = 'blk'
    blk.dataset.kind = kind
    if (kind === 'todo' || kind === 'bullet') {
      const box = document.createElement('span')
      box.className = 'box'
      box.contentEditable = 'false'
      box.dataset.checked = String(checked)
      if (kind === 'todo') {
        box.setAttribute('role', 'checkbox')
        box.setAttribute('aria-checked', String(checked))
      }
      blk.append(box)
    }
    const body = document.createElement('div')
    body.className = 'btext'
    blk.append(body)
    setText(blk, text)
    return blk
  }

  function fill(src: string): void {
    if (!root) return
    root.textContent = ''
    for (const line of src.split('\n')) {
      const b = parseLine(line)
      root.append(makeBlock(b.kind, b.text, b.checked))
    }
  }

  function blocks(): HTMLElement[] {
    return root ? Array.from(root.querySelectorAll<HTMLElement>(':scope > .blk')) : []
  }

  function blockOf(node: Node | null): HTMLElement | null {
    let n = node
    while (n && n !== root) {
      if (n instanceof HTMLElement && n.parentElement === root) return n
      n = n.parentNode
    }
    return null
  }

  function offsetIn(body: HTMLElement): number {
    const sel = window.getSelection()
    if (!sel || !sel.rangeCount) return 0
    const r = sel.getRangeAt(0)
    if (!body.contains(r.startContainer)) return 0
    const pre = document.createRange()
    pre.selectNodeContents(body)
    pre.setEnd(r.startContainer, r.startOffset)
    return pre.toString().length
  }

  function placeCaret(blk: HTMLElement, offset: number): void {
    const body = bodyOf(blk)
    if (!body) return
    root?.focus()
    const sel = window.getSelection()
    if (!sel) return
    const range = document.createRange()
    let acc = 0
    let placed = false
    for (const node of Array.from(body.childNodes)) {
      if (node.nodeType === Node.TEXT_NODE) {
        const len = node.textContent?.length ?? 0
        if (offset <= acc + len) {
          range.setStart(node, offset - acc)
          placed = true
          break
        }
        acc += len
      } else if (node instanceof HTMLElement && node.tagName === 'BR') {
        if (offset <= acc) {
          range.setStartBefore(node)
          placed = true
          break
        }
      }
    }
    if (placed) range.collapse(true)
    else {
      range.selectNodeContents(body)
      range.collapse(false)
    }
    sel.removeAllRanges()
    sel.addRange(range)
    keepInView(blk)
  }

  /* 光标所在块保持在编辑器视口内：回车新块、退格并块、粘贴尾块都跟着滚，
     长正文打到一半不用手动往下翻 */
  function keepInView(blk: HTMLElement): void {
    if (!root) return
    const rr = root.getBoundingClientRect()
    const br = blk.getBoundingClientRect()
    if (br.bottom > rr.bottom) root.scrollTop += br.bottom - rr.bottom + 1
    else if (br.top < rr.top) root.scrollTop -= rr.top - br.top + 1
  }

  /* 光标放到点击处：命中本编辑器的正文块才生效，否则回落末尾 */
  function caretAt(x: number, y: number): boolean {
    if (!root) return false
    const doc = document as Document & { caretRangeFromPoint?: (x: number, y: number) => Range | null }
    const r = doc.caretRangeFromPoint?.(x, y) ?? null
    const node = r?.startContainer
    if (!r || !node || !root.contains(node)) return false
    const host = node instanceof HTMLElement ? node : node.parentElement
    if (!host?.closest('.btext')) return false
    root.focus()
    const sel = window.getSelection()
    if (!sel) return false
    sel.removeAllRanges()
    sel.addRange(r)
    return true
  }

  function plain(el: HTMLElement | null): string {
    return (el?.textContent ?? '').replace(/\u00a0/g, ' ')
  }

  function serialize(): string {
    return blocks().map((blk) => {
      const kind = (blk.dataset.kind ?? 'p') as Kind
      const text = plain(bodyOf(blk))
      if (kind === 'h1') return `# ${text}`
      if (kind === 'h2') return `## ${text}`
      if (kind === 'h3') return `### ${text}`
      if (kind === 'todo') return `- [${boxOf(blk)?.dataset.checked === 'true' ? 'x' : ' '}] ${text}`
      if (kind === 'bullet') return `- ${text}`
      return text
    }).join('\n')
  }

  function emit(): void {
    onchange?.(serialize())
  }

  function toParagraph(blk: HTMLElement, caret = plain(bodyOf(blk)).length): void {
    const text = plain(bodyOf(blk))
    const next = makeBlock('p', text)
    blk.replaceWith(next)
    placeCaret(next, caret)
    emit()
  }

  function replaceBlock(blk: HTMLElement, kind: Kind, text: string, checked = false, caret = text.length): void {
    const next = makeBlock(kind, text, checked)
    blk.replaceWith(next)
    placeCaret(next, caret)
    emit()
  }

  /* 输入规则只在正文块里触发；已是标题/列表的块不再套规则。转换后光标按
     被消费的前缀长度回退，停在用户正在输入的位置 */
  function applyRule(blk: HTMLElement): boolean {
    const kind = (blk.dataset.kind ?? 'p') as Kind
    const body = bodyOf(blk)
    if (!body) return false
    const text = plain(body)
    const caret = offsetIn(body)
    const restCaret = (rest: string, consumed: number): number => Math.max(0, Math.min(rest.length, caret - consumed))
    let m: RegExpExecArray | null
    if (kind === 'p') {
      if ((m = /^(#{1,3})\s(.*)$/.exec(text))) {
        replaceBlock(blk, `h${m[1].length}` as Kind, m[2], false, restCaret(m[2], text.length - m[2].length))
        return true
      }
      if ((m = /^(?:[-*+]\s+)?\[([ xX]?)\]\s(.*)$/.exec(text))) {
        replaceBlock(blk, 'todo', m[2], m[1].toLowerCase() === 'x', restCaret(m[2], text.length - m[2].length))
        return true
      }
      if ((m = /^[-*+]\s(.*)$/.exec(text))) {
        replaceBlock(blk, 'bullet', m[1], false, restCaret(m[1], text.length - m[1].length))
        return true
      }
    } else if (kind === 'bullet') {
      if ((m = /^\[([ xX]?)\]\s(.*)$/.exec(text))) {
        replaceBlock(blk, 'todo', m[2], m[1].toLowerCase() === 'x', restCaret(m[2], text.length - m[2].length))
        return true
      }
    }
    return false
  }

  function onInput(e: Event): void {
    if (composing || (e as InputEvent).isComposing) return
    const blk = blockOf(window.getSelection()?.anchorNode ?? null)
    if (!blk || !applyRule(blk)) emit()
  }

  function onCompositionStart(): void {
    composing = true
  }

  function onCompositionEnd(): void {
    composing = false
    emit()
  }

  function onPointerDown(e: PointerEvent): void {
    const box = (e.target as HTMLElement).closest?.('.box') as HTMLElement | null
    const blk = box?.parentElement
    if (!box || !blk || !root?.contains(box) || blk.dataset.kind !== 'todo') return
    e.preventDefault()
    const checked = box.dataset.checked === 'true'
    box.dataset.checked = String(!checked)
    box.setAttribute('aria-checked', String(!checked))
    emit()
  }

  function onRootDown(e: PointerEvent): void {
    if (e.target !== root) return
    const last = blocks().at(-1)
    if (!last) return
    e.preventDefault()
    placeCaret(last, plain(bodyOf(last)).length)
  }

  function onKey(e: KeyboardEvent): void {
    if (e.isComposing) return
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      oncancel?.()
      return
    }
    if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
      e.preventDefault()
      onsave?.()
      return
    }
    if (e.key === 'Enter') {
      e.preventDefault()
      splitBlock()
      return
    }
    if (e.key === 'Backspace') backspace(e)
  }

  function splitBlock(): void {
    const sel = window.getSelection()
    if (!sel || !sel.rangeCount) return
    const blk = blockOf(sel.anchorNode)
    if (!blk) return
    const body = bodyOf(blk)
    if (!body) return
    const kind = (blk.dataset.kind ?? 'p') as Kind
    const text = plain(body)
    if ((kind === 'todo' || kind === 'bullet') && !text.trim()) {
      toParagraph(blk)
      return
    }
    const offset = offsetIn(body)
    const before = text.slice(0, offset)
    const after = text.slice(offset)
    setText(blk, before)
    const nextKind: Kind = kind === 'todo' ? 'todo' : kind === 'bullet' ? 'bullet' : 'p'
    const next = makeBlock(nextKind, after)
    blk.after(next)
    placeCaret(next, 0)
    emit()
  }

  function backspace(e: KeyboardEvent): void {
    const sel = window.getSelection()
    if (!sel || !sel.rangeCount || !sel.isCollapsed) return
    const blk = blockOf(sel.anchorNode)
    if (!blk) return
    const body = bodyOf(blk)
    if (!body || offsetIn(body) !== 0) return
    if (blk.dataset.kind !== 'p') {
      e.preventDefault()
      toParagraph(blk, 0)
      return
    }
    const prev = blk.previousElementSibling as HTMLElement | null
    const prevBody = prev ? bodyOf(prev) : null
    if (!prev || !prevBody) return
    e.preventDefault()
    const head = plain(prevBody)
    setText(prev, head + plain(body))
    blk.remove()
    placeCaret(prev, head.length)
    emit()
  }

  function onPaste(e: ClipboardEvent): void {
    const imgs = imageFiles(e.clipboardData?.items)
    if (imgs.length) {
      e.preventDefault()
      onimage?.(imgs)
      return
    }
    e.preventDefault()
    if (!root) return
    const sel = window.getSelection()
    if (!sel || !sel.rangeCount) return
    const blk = blockOf(sel.anchorNode)
    const body = blk ? bodyOf(blk) : null
    if (!blk || !body) return
    const raw = (e.clipboardData?.getData('text/plain') ?? '').replace(/\r\n?/g, '\n')
    if (!raw) return
    if (!sel.isCollapsed) sel.deleteFromDocument()
    const text = plain(body)
    const offset = offsetIn(body)
    const lines = raw.split('\n')
    setText(blk, text.slice(0, offset) + lines[0])
    let last = blk
    for (let i = 1; i < lines.length; i++) {
      const b = parseLine(lines[i])
      const next = makeBlock(b.kind, b.text, b.checked)
      last.after(next)
      last = next
    }
    placeCaret(last, plain(bodyOf(last)).length)
    emit()
  }

  onMount(() => {
    pinEditEnter()
    fill(value)
    requestAnimationFrame(() => {
      /* 先按渲染态的滚动量对齐，再按点击点放光标；没有点击点或没命中
         正文块时光标落在末尾 */
      if (focusAt && root) {
        if (focusAt.scrollTop) root.scrollTop = focusAt.scrollTop
        if (caretAt(focusAt.x, focusAt.y)) return
      }
      const last = blocks().at(-1)
      if (last) placeCaret(last, plain(bodyOf(last)).length)
      else root?.focus()
    })
  })

  onDestroy(pinEditLeave)
</script>

<div
  class="rich"
  bind:this={root}
  contenteditable="true"
  role="textbox"
  tabindex="0"
  aria-multiline="true"
  aria-label="便签正文"
  spellcheck="false"
  oninput={onInput}
  onkeydown={onKey}
  onpointerdown={onPointerDown}
  onpointerdowncapture={onRootDown}
  onpaste={onPaste}
  oncompositionstart={onCompositionStart}
  oncompositionend={onCompositionEnd}
></div>

<style>
  .rich {
    flex: 1;
    min-height: 0;
    width: 100%;
    overflow-y: auto;
    overscroll-behavior: contain;
    scrollbar-width: thin;
    scrollbar-color: color-mix(in srgb, var(--fg) 18%, transparent) transparent;
    color: var(--fg);
    font-size: var(--font-size-section);
    font-weight: 500;
    line-height: 1.6;
    outline: none;
    cursor: text;
  }
  .rich :global(.blk) {
    display: flex;
    align-items: flex-start;
    gap: 5px;
    min-height: 1.6em;
  }
  .rich :global(.blk[data-kind='h1']) {
    margin: 5px 0 2px;
  }
  .rich :global(.blk[data-kind='h2']) {
    margin: 4px 0 1px;
  }
  .rich :global(.blk[data-kind='h3']) {
    margin: 3px 0 1px;
  }
  .rich :global(.blk:first-child) {
    margin-top: 0;
  }
  .rich :global(.btext) {
    flex: 1;
    min-width: 0;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    outline: none;
  }
  .rich :global(.blk[data-kind='h1'] .btext) {
    font-size: 1.35em;
    font-weight: 700;
    line-height: 1.35;
  }
  .rich :global(.blk[data-kind='h2'] .btext) {
    font-size: 1.2em;
    font-weight: 600;
    line-height: 1.35;
  }
  .rich :global(.blk[data-kind='h3'] .btext) {
    font-size: 1.08em;
    font-weight: 600;
    line-height: 1.4;
  }
  .rich :global(.box) {
    flex: none;
    box-sizing: border-box;
    width: 13px;
    height: 13px;
    margin-top: 3px;
    border: 1.5px solid color-mix(in srgb, var(--fg) 35%, transparent);
    border-radius: 4px;
    cursor: pointer;
    transition:
      background var(--dur-fast) var(--ease-out),
      border-color var(--dur-fast) var(--ease-out);
  }
  .rich :global(.blk[data-kind='todo'] .box[data-checked='true']) {
    border-color: var(--accent);
    background: var(--accent);
    color: var(--bg);
  }
  .rich :global(.blk[data-kind='todo'] .box[data-checked='true']::after) {
    content: '✓';
    display: block;
    font-size: 9px;
    font-weight: 700;
    line-height: 10px;
    text-align: center;
  }
  .rich :global(.blk[data-kind='todo'] .box[data-checked='true'] + .btext) {
    color: var(--faint);
    text-decoration: line-through;
  }
  .rich :global(.blk[data-kind='bullet'] .box) {
    width: 4px;
    height: 4px;
    margin: 0.72em 4px 0 4px;
    border: none;
    border-radius: 50%;
    background: var(--muted);
    cursor: default;
  }
</style>
