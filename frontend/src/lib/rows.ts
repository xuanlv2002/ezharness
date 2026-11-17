/*
时间线的行组装（主时间线与分身抽屉共用）：user 块独立成行，其余并入其后的回复组，组 key 用前置 user 块 uid（组内增删不换 key，流式不抖）。
trim / archive 是上下文边界，独立成行，方便时间线从边界处开始懒加载。
*/

import type { Block } from './store.svelte'

export const TOOL_GROUP_MIN = 2

export type ToolBlockB = Extract<Block, { kind: 'tool' }>
export type Seg = { type: 'one'; b: Block } | { type: 'tools'; blocks: ToolBlockB[] }
export type Row =
  | { kind: 'leader'; key: 'L'; segs: Seg[] }
  | { kind: 'user'; key: string; b: Extract<Block, { kind: 'user' }> }
  | { kind: 'marker'; key: string; b: Extract<Block, { kind: 'trim' | 'archive' }> }
  | { kind: 'group'; key: string; segs: Seg[] }

export function buildRows(blocks: Block[]): Row[] {
  const out: Row[] = []
  let segs: Seg[] = []
  let toolRun: ToolBlockB[] = []
  let curKey = 'L'
  const flushTools = () => {
    if (toolRun.length) {
      segs.push({ type: 'tools', blocks: toolRun })
      toolRun = []
    }
  }
  const flushGroup = () => {
    flushTools()
    if (!segs.length) return
    if (curKey === 'L') out.push({ kind: 'leader', key: 'L', segs })
    else out.push({ kind: 'group', key: 'g' + curKey, segs })
    segs = []
  }
  for (const b of blocks) {
    if (b.kind === 'user') {
      flushGroup()
      out.push({ kind: 'user', key: 'u' + b.uid, b })
      curKey = String(b.uid)
    } else if (b.kind === 'trim' || b.kind === 'archive') {
      flushGroup()
      out.push({ kind: 'marker', key: 'm' + b.uid, b })
      curKey = 'm' + b.uid
    } else if (b.kind === 'tool') {
      // 被分身入口卡接管的 task 工具卡不参与组装
      if (b.hidden) continue
      if (b.name === 'memory') {
        // 记忆变更卡常显：不折进工具组，避免 diff 被"N 个工具调用"盖住
        flushTools()
        segs.push({ type: 'one', b })
      } else {
        toolRun.push(b)
      }
    } else {
      flushTools()
      segs.push({ type: 'one', b })
    }
  }
  flushGroup()
  return out
}
