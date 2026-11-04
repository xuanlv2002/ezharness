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

/* 卡面宽到多少时，会话列与时间并排（PinCard 的 .wide 同值） */
export const WIDE_W = 320

/* sessionRows 底栏会话行数：新会话 + 最近 1 个 + 更多会话 */
export function sessionRows(n: number): number {
  return 1 + (n > 0 ? 1 : 0) + (n > 1 ? 1 : 0)
}

/*
minCardHeight 卡片最小高度：底栏（两行时间 + 会话）与一行正文固定常显，
多出来的正文在内部滚动。行高与边距和 PinCard 的 CSS 对齐（11px/1.5、
会话行距 2、正文上下 padding 19、一行正文约 21、边框 2）。宽卡
（≥WIDE_W）会话列与时间并排，窄卡上下叠。
*/
export function minCardHeight(w: number, rows: number): number {
  const times = 6 + 2 * 16.5 // padding-top + 两行时间
  const r = Math.max(1, Math.min(3, rows))
  const links = 5 + r * 16.5 + (r - 1) * 2
  const foot = w >= WIDE_W ? Math.max(times, links + 1) : times + links
  return Math.ceil(foot) + 19 + 21 + 2
}

const GAP_X = 12
const GAP_Y = 12

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
let saveInFlight = false

/* applyPinboard 用服务端数据覆盖共享态。 */
function applyPinboard(d: PinboardData): void {
  board.v = d.v || 1
  board.z = d.z || 10
  board.cards = Array.isArray(d.cards) ? d.cards.map(normCard) : []
  board.done = Array.isArray(d.done) ? d.done.map(normRecord) : []
  board.loaded = true
  board.err = ''
}

/* ensurePinboard 首次使用时拉整块板（PinboardView 与状态卡共用）。 */
export async function ensurePinboard(): Promise<void> {
  if (board.loaded) return
  if (loadP) return loadP
  board.loading = true
  loadP = api
    .pinboard()
    .then(applyPinboard)
    .catch((e) => {
      board.err = `便签数据加载失败：${(e as Error).message}`
    })
    .finally(() => {
      board.loading = false
      loadP = null
    })
  return loadP
}

/* reloadPinboard 强制重拉（便签小窗轮询用）；失败保留旧数据只记错误。 */
export async function reloadPinboard(): Promise<void> {
  try {
    applyPinboard(await api.pinboard())
  } catch (e) {
    board.err = `便签数据加载失败：${(e as Error).message}`
  }
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

/* 图片只认本机便签图片 URL（板文件里不落任意外链） */
function normImages(v: unknown): string[] | undefined {
  if (!Array.isArray(v)) return undefined
  const out = (v as unknown[]).filter(
    (u): u is string => typeof u === 'string' && /^\/api\/pinboard\/images\/img-[0-9a-f]{32}\.(png|jpg|webp|gif)$/.test(u),
  )
  return out.length ? out : undefined
}

/* 卡面图片摆放：只认板里现存图片的条目，坐标/比例越界就夹回 0..1 */
function normImgpos(v: unknown, images: string[] | undefined): PinCard['imgpos'] {
  if (!v || typeof v !== 'object' || !images?.length) return undefined
  const out: NonNullable<PinCard['imgpos']> = {}
  for (const [src, raw] of Object.entries(v as Record<string, unknown>)) {
    if (!images.includes(src) || !raw || typeof raw !== 'object') continue
    const b = raw as Record<string, unknown>
    const w = num(b.w)
    const ar = num(b.ar)
    if (w <= 0 || ar <= 0) continue
    const frac = (n: unknown): number => Math.max(0, Math.min(1, num(n)))
    out[src] = { x: frac(b.x), y: frac(b.y), w: Math.min(1, w), ar }
  }
  return Object.keys(out).length ? out : undefined
}

/* 老数据补齐缺失字段（尺寸/颜色/时刻），保证渲染与拖拽都有确定值。
   逐字段重建而不是展开原对象——已移除的旧字段会在下一次落盘时自然消失 */
function normCard(c: PinCard): PinCard {
  const sessions = normSessions(c.sessions)
  const w = Math.max(MIN_W, num(c.w) || CARD_DEF_W)
  const card: PinCard = {
    id: typeof c.id === 'string' && c.id ? c.id : pinId(),
    x: num(c.x),
    y: num(c.y),
    w,
    h: Math.max(MIN_H, minCardHeight(w, sessionRows(sessions?.length ?? 0)), num(c.h) || CARD_DEF_H),
    text: typeof c.text === 'string' ? c.text : '',
    created: num(c.created) || Date.now(),
    z: num(c.z) || 10,
    tint: c.tint || randTint(),
  }
  const due = num(c.due)
  if (due > 0) card.due = due
  if (typeof c.ord === 'number' && Number.isFinite(c.ord)) card.ord = c.ord
  const images = normImages(c.images)
  if (images) card.images = images
  const imgpos = normImgpos(c.imgpos, images)
  if (imgpos) card.imgpos = imgpos
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
  const images = normImages(r.images)
  if (images) rec.images = images
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
    saveInFlight = true
    try {
      await api.savePinboard(data)
      board.err = ''
    } catch (e) {
      board.err = `便签保存失败：${(e as Error).message}`
    } finally {
      saveInFlight = false
    }
  })
  return saveChain
}

export function scheduleSave(): void {
  if (!board.loaded) return
  clearTimeout(saveTimer)
  saveTimer = setTimeout(() => void saveNow(), 350)
}

/* pinboardBusy 本地还有未落盘的改动（小窗轮询/主窗聚焦刷新前避开，
   免得读到旧板把刚做的改动盖掉）。 */
export function pinboardBusy(): boolean {
  return saveTimer !== undefined || saveInFlight
}

export function pinId(): string {
  return 'c' + Date.now().toString(36) + Math.random().toString(36).slice(2, 8)
}

export function findCard(id: string): PinCard | undefined {
  return board.cards.find((c) => c.id === id)
}

/*
orderedCards 小窗/顶栏预览的展示顺序：手工顺序（ord）优先；没排过的卡
（新记的）排在已排的后面，彼此按截止从紧到松、同档新记的在前。整板都
没排过时就是纯截止排序。
*/
export function orderedCards(cards: PinCard[]): PinCard[] {
  return [...cards].sort((a, b) => {
    if (a.ord !== undefined || b.ord !== undefined) {
      if (a.ord === undefined) return 1
      if (b.ord === undefined) return -1
      if (a.ord !== b.ord) return a.ord - b.ord
    }
    const ad = a.due ?? Number.MAX_SAFE_INTEGER
    const bd = b.due ?? Number.MAX_SAFE_INTEGER
    if (ad !== bd) return ad - bd
    return b.created - a.created
  })
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

/* reorderCards 便签小窗手工排序（ids = 目标顺序，ord 持久化到 pinboard.json）。 */
export function reorderCards(ids: string[]): void {
  const ord = new Map(ids.map((id, i) => [id, i]))
  board.cards = board.cards.map((c) => (ord.has(c.id) ? { ...c, ord: ord.get(c.id) } : c))
  void saveNow() // 松手即写：排序不依赖防抖，重启后保持
}

/* patchRecord 改已归档记录的字段（当天详情里双击编辑/勾待办）。 */
export function patchRecord(id: string, patch: Partial<PinRecord>): void {
  board.done = board.done.map((r) => (r.id === id ? { ...r, ...patch } : r))
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
  /* 图片与关联随卡片一起归档：当天详情里仍能看到并跳转 */
  if (c.images?.length) rec.images = [...c.images]
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
  /* 拖回板上时图片与关联一起还原 */
  if (r.images?.length) card.images = [...r.images]
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
装得下的方案，仍超高则逐轮等比压缩卡片高度（不低于底栏+一行正文的
minCardHeight）。绝不靠夹紧坐标兜底——宁可压矮也不重叠（方案文档 §5.3）。
availW/availH = 板内容区（调用方已减去 PAD）。
*/
export function tidyCards(availW: number, availH: number): void {
  if (!board.cards.length || availW <= 0 || availH <= 0) return
  const ordered = [...board.cards].sort((a, b) => a.created - b.created || a.y - b.y || a.x - b.x)
  const rows = ordered.map((c) => sessionRows(c.sessions?.length ?? 0))
  const minsFor = (w: number): number[] => ordered.map((_, i) => minCardHeight(w, rows[i]))
  const maxCols = Math.max(1, Math.min(6, Math.floor((availW + GAP_X) / (176 + GAP_X))))
  let mins = minsFor(availW)
  let hs = ordered.map((c, i) => Math.max(MIN_H, mins[i], Math.min(c.h, availH)))
  let cols = 1
  let colW = availW
  let plan = packRows(hs, 1)
  for (let n = 1; n <= maxCols; n++) {
    cols = n
    colW = Math.floor((availW - GAP_X * (n - 1)) / n)
    mins = minsFor(colW)
    hs = ordered.map((c, i) => Math.max(MIN_H, mins[i], Math.min(c.h, availH)))
    plan = packRows(hs, n)
    if (plan.tallest <= availH) break
  }
  for (let t = 0; t < 12 && plan.tallest > availH; t++) {
    const f = Math.max(0.35, availH / plan.tallest)
    const next = hs.map((h, i) => Math.max(mins[i], Math.round(h * f)))
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

/*
fitCards 窗口缩小时把越出板面的卡片收回可见区：整块按同一比例缩小
（x/y/w/h 一起缩，相对排布不变），宽不低于 MIN_W、高不低于底栏+一行
正文的 minCardHeight，最后夹紧到板内。只缩不放——窗口变大不改用户
手工摆好的尺寸。availW/availH = 板面尺寸（含内边距）。
*/
export function fitCards(availW: number, availH: number): void {
  if (!board.cards.length) return
  const roomW = availW - 2 * PAD
  const roomH = availH - 2 * PAD
  if (roomW <= 0 || roomH <= 0) return
  let extW = 0
  let extH = 0
  for (const c of board.cards) {
    extW = Math.max(extW, c.x + c.w - PAD)
    extH = Math.max(extH, c.y + c.h - PAD)
  }
  const f = Math.min(1, roomW / Math.max(1, extW), roomH / Math.max(1, extH))
  if (f >= 0.999) return
  board.cards = board.cards.map((c) => {
    const w = Math.max(MIN_W, Math.round(c.w * f))
    const minH = minCardHeight(w, sessionRows(c.sessions?.length ?? 0))
    const h = Math.max(MIN_H, minH, Math.round(c.h * f))
    const x = Math.round(clamp(PAD + (c.x - PAD) * f, PAD, Math.max(PAD, availW - PAD - w)))
    const y = Math.round(clamp(PAD + (c.y - PAD) * f, PAD, Math.max(PAD, availH - PAD - h)))
    return { ...c, x, y, w, h }
  })
  scheduleSave()
}

function clamp(v: number, lo: number, hi: number): number {
  return Math.max(lo, Math.min(hi, v))
}
