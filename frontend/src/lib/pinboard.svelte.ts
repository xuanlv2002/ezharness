/*
便签板的共享态：整块板经 /api/pinboard 落盘（数据目录 pinboard.json），
前端防抖提交。布局常量、色板、整理算法与归档语义都在这里——组件只做
交互。

归档语义（与方案文档一致）：doneAt 是事实，永远等于点完成那一刻；day
只表达「归到哪一天」，只在手工拖时间线时写，拖回真实完成日即删除。
*/
import { api } from './api'
import type { PinCard, PinboardData, PinRecord, PinSessionRef } from './api'

/* 便签色板（低饱和，卡片左侧 2px 竖条常驻） */
export const TINTS = [
  '#c98a5e', '#8fa08b', '#8ba0b5', '#b291a6', '#c0a86f', '#a98d7d', '#9aa88f',
  '#b58f8f', '#b08968', '#8a9a8b', '#93a7b8', '#b39bab', '#a8a084', '#9d8f9a',
]

export const CARD_DEF_W = 210
/* 默认高 126：正文之外还固定两行时间信息（记于+已持续 / 截止） */
export const CARD_DEF_H = 126
export const MIN_W = 132
export const MIN_H = 54
export const PAD = 12

const GAP_X = 12
const GAP_Y = 12
const TIDY_MIN_H = 30

export const board = $state<{
  v: number
  z: number
  cards: PinCard[]
  done: PinRecord[]
  loaded: boolean
  loading: boolean
  err: string
}>({
  v: 1,
  z: 10,
  cards: [],
  done: [],
  loaded: false,
  loading: false,
  err: '',
})

let loadP: Promise<void> | null = null
let saveTimer: ReturnType<typeof setTimeout> | undefined
let saveChain: Promise<void> = Promise.resolve() // 保存串行化：慢请求不会把旧快照盖在新快照上

/* ensurePinboard 首次使用时拉整块板（PinboardView 与状态卡共用）。 */
export async function ensurePinboard(): Promise<void> {
  if (board.loaded) return
  if (loadP) return loadP
  board.loading = true
  loadP = api
    .pinboard()
    .then((d) => {
      board.v = d.v || 1
      board.z = d.z || 10
      board.cards = Array.isArray(d.cards) ? d.cards.map(normCard) : []
      board.done = Array.isArray(d.done) ? d.done.map(normRecord) : []
      board.loaded = true
      board.err = ''
    })
    .catch((e) => {
      board.err = `便签数据加载失败：${(e as Error).message}`
    })
    .finally(() => {
      board.loading = false
      loadP = null
    })
  return loadP
}

const num = (v: unknown): number => (typeof v === 'number' && Number.isFinite(v) ? v : 0)

function randTint(): string {
  return TINTS[Math.floor(Math.random() * TINTS.length)]
}

/* 会话关联：只保留合法条目并去重（同一会话只列一次） */
function normSessions(v: unknown): PinSessionRef[] | undefined {
  if (!Array.isArray(v)) return undefined
  const seen = new Set<string>()
  const out: PinSessionRef[] = []
  for (const s of v as PinSessionRef[]) {
    if (!s || typeof s.id !== 'string' || !s.id || seen.has(s.id)) continue
    seen.add(s.id)
    const ref: PinSessionRef = { id: s.id, title: typeof s.title === 'string' ? s.title : '' }
    const created = num(s.createdAt)
    if (created > 0) ref.createdAt = created
    out.push(ref)
  }
  return out.length ? out : undefined
}

function normPending(v: unknown): string[] | undefined {
  if (!Array.isArray(v)) return undefined
  const out = (v as unknown[]).filter((id): id is string => typeof id === 'string' && !!id)
  return out.length ? out : undefined
}

/* 老数据补齐缺失字段（尺寸/颜色/时刻），保证渲染与拖拽都有确定值。
   逐字段重建而不是展开原对象——已移除的旧字段会在下一次落盘时自然消失 */
function normCard(c: PinCard): PinCard {
  const card: PinCard = {
    id: typeof c.id === 'string' && c.id ? c.id : pinId(),
    x: num(c.x),
    y: num(c.y),
    w: Math.max(MIN_W, num(c.w) || CARD_DEF_W),
    h: Math.max(MIN_H, num(c.h) || CARD_DEF_H),
    text: typeof c.text === 'string' ? c.text : '',
    created: num(c.created) || Date.now(),
    z: num(c.z) || 10,
    tint: c.tint || randTint(),
  }
  const due = num(c.due)
  if (due > 0) card.due = due
  const sessions = normSessions(c.sessions)
  if (sessions) card.sessions = sessions
  const pending = normPending(c.pendingSessionIds)
  if (pending) card.pendingSessionIds = pending
  return card
}

function normRecord(r: PinRecord): PinRecord {
  const rec: PinRecord = {
    id: typeof r.id === 'string' && r.id ? r.id : pinId(),
    text: typeof r.text === 'string' ? r.text : '',
    created: num(r.created),
    doneAt: num(r.doneAt) || Date.now(),
    tint: r.tint || randTint(),
  }
  if (typeof r.day === 'string' && r.day) rec.day = r.day
  if (typeof r.ord === 'number' && Number.isFinite(r.ord)) rec.ord = r.ord
  const sessions = normSessions(r.sessions)
  if (sessions) rec.sessions = sessions
  const pending = normPending(r.pendingSessionIds)
  if (pending) rec.pendingSessionIds = pending
  return rec
}

/* snapshot 纯数据副本（$state 代理不直接序列化） */
function snapshot(): PinboardData {
  return {
    v: board.v,
    z: board.z,
    cards: board.cards.map((c) => ({ ...c })),
    done: board.done.map((r) => ({ ...r })),
  }
}

/* saveNow 立即落盘；scheduleSave 防抖 350ms（拖拽/入力连续操作只写尾次）。
   保存按快照顺序串行执行，后一次一定落在前一次之后。 */
export function saveNow(): Promise<void> {
  clearTimeout(saveTimer)
  saveTimer = undefined
  if (!board.loaded) return saveChain
  const data = snapshot()
  saveChain = saveChain.then(async () => {
    try {
      await api.savePinboard(data)
      board.err = ''
    } catch (e) {
      board.err = `便签保存失败：${(e as Error).message}`
    }
  })
  return saveChain
}

export function scheduleSave(): void {
  if (!board.loaded) return
  clearTimeout(saveTimer)
  saveTimer = setTimeout(() => void saveNow(), 350)
}

export function pinId(): string {
  return 'c' + Date.now().toString(36) + Math.random().toString(36).slice(2, 8)
}

export function findCard(id: string): PinCard | undefined {
  return board.cards.find((c) => c.id === id)
}

export function addCard(text: string, x: number, y: number, w = CARD_DEF_W, h = CARD_DEF_H): PinCard {
  board.z += 1
  const card: PinCard = {
    id: pinId(),
    x,
    y,
    w,
    h,
    text,
    created: Date.now(),
    z: board.z,
    tint: randTint(),
  }
  board.cards = [...board.cards, card]
  scheduleSave()
  return card
}

/* patchCard 合并字段并落盘（位置/尺寸/内容/关联都走这里）。 */
export function patchCard(id: string, patch: Partial<PinCard>): void {
  board.cards = board.cards.map((c) => (c.id === id ? { ...c, ...patch } : c))
  scheduleSave()
}

/* bumpCard 拖起时提层（拖动中的卡在最上层）。 */
export function bumpCard(id: string): void {
  board.z += 1
  const z = board.z
  board.cards = board.cards.map((c) => (c.id === id ? { ...c, z } : c))
  scheduleSave()
}

export function removeCard(id: string): void {
  board.cards = board.cards.filter((c) => c.id !== id)
  scheduleSave()
}

/*
addPendingSession 卡片「新会话」开出的会话先记为待定：卡片上不显示、
也不算关联；在对话里真正发出第一条消息后由 linkPendingNote 落实。
同一张卡可以开多个，互不影响。
*/
export function addPendingSession(cardId: string, sessionId: string): void {
  const c = findCard(cardId)
  if (!c || !sessionId) return
  if (c.sessions?.some((s) => s.id === sessionId)) return
  const cur = c.pendingSessionIds ?? []
  if (cur.includes(sessionId)) return
  patchCard(cardId, { pendingSessionIds: [...cur, sessionId] })
}

/*
linkPendingNote 对话页真实发出消息后调用：把该会话挂到对应便签的关联
列表末尾（卡片右下角按顺序显示），并移出待定。未加载过便签板时先补拉。
*/
export async function linkPendingNote(sessionId: string, title = '', createdAt = 0): Promise<void> {
  if (!sessionId) return
  await ensurePinboard()
  if (!board.loaded) return
  let changed = false
  function link<T extends PinCard | PinRecord>(entry: T): T {
    if (!entry.pendingSessionIds?.includes(sessionId)) return entry
    changed = true
    const ref: PinSessionRef = { id: sessionId, title: title || '会话' }
    if (createdAt > 0) ref.createdAt = createdAt
    const sessions = [...(entry.sessions ?? []).filter((s) => s.id !== sessionId), ref]
    const pending = entry.pendingSessionIds.filter((id) => id !== sessionId)
    return { ...entry, sessions, pendingSessionIds: pending.length ? pending : undefined }
  }
  /* 卡片与已归档记录都可能带着待定会话（先完成卡片、后发消息） */
  board.cards = board.cards.map(link)
  board.done = board.done.map(link)
  if (changed) scheduleSave()
}

/* completeCard 划线入库：生成 Record（doneAt = 此刻），卡片走下板。 */
export function completeCard(id: string): PinRecord | null {
  const c = board.cards.find((x) => x.id === id)
  if (!c) return null
  const rec: PinRecord = {
    id: pinId(),
    text: c.text,
    created: c.created,
    doneAt: Date.now(),
    tint: c.tint,
  }
  /* 关联随卡片一起归档：当天详情里仍能看到并跳转 */
  if (c.sessions?.length) rec.sessions = c.sessions.map((s) => ({ ...s }))
  if (c.pendingSessionIds?.length) rec.pendingSessionIds = [...c.pendingSessionIds]
  board.cards = board.cards.filter((x) => x.id !== id)
  board.done = [rec, ...board.done]
  scheduleSave()
  return rec
}

/* restoreRecord 时间线卡拖回板上：变回待办卡（记于沿用原卡片）。 */
export function restoreRecord(id: string, x: number, y: number): PinCard | null {
  const r = board.done.find((d) => d.id === id)
  if (!r) return null
  board.done = board.done.filter((d) => d.id !== id)
  board.z += 1
  const card: PinCard = {
    id: pinId(),
    x,
    y,
    w: CARD_DEF_W,
    h: CARD_DEF_H,
    text: r.text,
    created: r.created || Date.now(),
    z: board.z,
    tint: r.tint,
  }
  /* 拖回板上时关联一起还原 */
  if (r.sessions?.length) card.sessions = r.sessions.map((s) => ({ ...s }))
  if (r.pendingSessionIds?.length) card.pendingSessionIds = [...r.pendingSessionIds]
  board.cards = [...board.cards, card]
  scheduleSave()
  return card
}

const pad2 = (n: number): string => String(n).padStart(2, '0')

export function dayKeyOf(ts: number): string {
  const d = new Date(ts)
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`
}

/* dayOf 归属日：手工归档优先，否则 = 完成日（doneAt 所在日）。 */
export function dayOf(r: PinRecord): string {
  return r.day || dayKeyOf(r.doneAt)
}

/* byDayOrder 当天内顺序：ord 升序（缺省 = 最左），同序按完成时刻倒序。 */
export function byDayOrder(a: PinRecord, b: PinRecord): number {
  const ao = a.ord ?? -1
  const bo = b.ord ?? -1
  if (ao !== bo) return ao - bo
  return b.doneAt - a.doneAt
}

/*
moveRecordDay 手工改归属日：只写 day，不动 doneAt（时间与位置永远
不互相篡改）；拖回真实完成日时删除 day 回到默认归属。落进去即当天
第一个（ord=0），目标日其余记录顺延 ord=1..n。
*/
export function moveRecordDay(id: string, target: string): void {
  const rec = board.done.find((r) => r.id === id)
  if (!rec) return
  const real = dayKeyOf(rec.doneAt)
  board.done = board.done.map((r) => {
    if (r.id !== id) return r
    const next = { ...r, ord: 0 }
    if (target === real) delete next.day
    else next.day = target
    return next
  })
  const others = board.done.filter((r) => r.id !== id && dayOf(r) === target).sort(byDayOrder)
  const ordById = new Map(others.map((r, i) => [r.id, i + 1]))
  board.done = board.done.map((r) => (ordById.has(r.id) ? { ...r, ord: ordById.get(r.id) } : r))
  void saveNow() // 跨天落位是离散动作：立即落盘，关窗/切页都不丢
}

/* reorderDay 当天内拖动排序（ids = 目标顺序，ord 持久化到 pinboard.json）。 */
export function reorderDay(day: string, ids: string[]): void {
  const ord = new Map(ids.map((id, i) => [id, i]))
  board.done = board.done.map((r) => (ord.has(r.id) && dayOf(r) === day ? { ...r, ord: ord.get(r.id) } : r))
  void saveNow() // 松手即写：排序不依赖 350ms 防抖，重启后保持
}

/* packRows 按行填充：行内从左到右、行间从上到下，行高 = 该行最高卡。
   不用最短列优先——视觉顺序必须等于排序顺序（方案文档 §5.2）。 */
function packRows(hs: number[], cols: number): { y: number[]; tallest: number } {
  const y: number[] = []
  let total = 0
  for (let start = 0; start < hs.length; start += cols) {
    const row = hs.slice(start, start + cols)
    const rowH = Math.max(...row)
    for (let i = start; i < start + row.length; i++) y[i] = total
    total += rowH
    if (start + cols < hs.length) total += GAP_Y
  }
  return { y, tallest: total }
}

/*
tidyCards 一键整理：按挂得久→新排序，按行填充；列数从少到多取第一个
装得下的方案，仍超高则逐轮等比压缩卡片高度（下限 30px）。绝不靠夹紧
坐标兜底——宁可压矮也不重叠（方案文档 §5.3）。
availW/availH = 板内容区（调用方已减去 PAD）。
*/
export function tidyCards(availW: number, availH: number): void {
  if (!board.cards.length || availW <= 0 || availH <= 0) return
  const ordered = [...board.cards].sort((a, b) => a.created - b.created || a.y - b.y || a.x - b.x)
  const maxCols = Math.max(1, Math.min(6, Math.floor((availW + GAP_X) / (176 + GAP_X))))
  let hs = ordered.map((c) => clamp(c.h, MIN_H, availH))
  let cols = 1
  let colW = availW
  let plan = packRows(hs, 1)
  for (let n = 1; n <= maxCols; n++) {
    cols = n
    colW = Math.floor((availW - GAP_X * (n - 1)) / n)
    plan = packRows(hs, n)
    if (plan.tallest <= availH) break
  }
  for (let t = 0; t < 12 && plan.tallest > availH; t++) {
    const f = Math.max(0.35, availH / plan.tallest)
    const next = hs.map((h) => Math.max(TIDY_MIN_H, Math.round(h * f)))
    if (next.every((h, i) => h === hs[i])) break // 压不动了：防死循环
    hs = next
    plan = packRows(hs, cols)
  }
  const pos = new Map<string, { x: number; y: number; w: number; h: number }>()
  ordered.forEach((c, i) => {
    pos.set(c.id, {
      x: PAD + (i % cols) * (colW + GAP_X),
      y: PAD + plan.y[i],
      w: colW,
      h: hs[i],
    })
  })
  board.cards = board.cards.map((c) => {
    const p = pos.get(c.id)
    return p ? { ...c, ...p } : c
  })
  scheduleSave()
}

function clamp(v: number, lo: number, hi: number): number {
  return Math.max(lo, Math.min(hi, v))
}
