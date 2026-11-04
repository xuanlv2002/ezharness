/* 便签截止时间的紧迫度分档与配色：便签卡与宠物「便签」球角标共用。
   分档按距到期还剩多久取最紧的一档；无截止时间返回 null。 */

export type DueLevel = 'over' | 'h3' | 'h6' | 'h12' | 'h24' | 'later'

const HOUR = 3_600_000

/* dueLevel 距截止时长分档（now 由调用方定时推进）。 */
export function dueLevel(due: number | undefined, now: number): DueLevel | null {
  if (!due || !Number.isFinite(due)) return null
  const left = due - now
  if (left <= 0) return 'over'
  if (left <= 3 * HOUR) return 'h3'
  if (left <= 6 * HOUR) return 'h6'
  if (left <= 12 * HOUR) return 'h12'
  if (left <= 24 * HOUR) return 'h24'
  return 'later'
}

/* 色阶：越临近到期越暖；24 小时以上用绿（宽裕），已过期为危险色 */
export const DUE_COLOR: Record<DueLevel, string> = {
  over: 'var(--danger)',
  h3: '#f85149',
  h6: '#e8590c',
  h12: '#f0883e',
  h24: '#d29922',
  later: '#3fb950',
}

/* 紧迫度排序（值越小越紧）：多张便签取最紧的一档 */
export const DUE_ORDER: Record<DueLevel, number> = {
  over: 0,
  h3: 1,
  h6: 2,
  h12: 3,
  h24: 4,
  later: 5,
}

/* 记于/截止的短格式（月/日 时:分），便签小窗与顶栏预览共用 */
export function fmtShortTime(ts: number): string {
  return new Date(ts).toLocaleString('zh-CN', {
    month: 'numeric',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

/* 相对时长（分钟/小时/天），用于「还剩 / 已超期」 */
export function fmtDuration(ms: number): string {
  const m = Math.max(0, Math.floor(ms / 60000))
  if (m < 60) return `${m} 分钟`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h} 小时`
  return `${Math.floor(h / 24)} 天`
}

/* 截止文案 / 悬停提示 / 颜色：便签小窗与顶栏预览共用同一套说法 */
export function dueLabel(due: number | undefined): string {
  return due ? `截止 ${fmtShortTime(due)}` : '未设置截止'
}

export function dueHint(due: number | undefined, now: number): string {
  if (!due) return '未设置截止时间'
  return due <= now ? `已超期 ${fmtDuration(now - due)}` : `还剩 ${fmtDuration(due - now)}`
}

export function dueColor(due: number | undefined, now: number): string {
  const lv = dueLevel(due, now)
  return lv ? DUE_COLOR[lv] : 'var(--faint)'
}
