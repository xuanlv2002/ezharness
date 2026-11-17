/*
时间线的行组装（主时间线与分身抽屉共用）：user 块独立成行，其余并入其后的回复组，组 key 用前置 user 块 uid（组内增删不换 key，流式不抖）。
foldBlocks 是 trim 的渲染折叠：折叠态只裁掉区块、数据保留，滚到顶后上拉时置 folded=false 自动载入。
*/

import type { Block } from './store.svelte'

export const TOOL_GROUP_MIN = 2

export type ToolBlockB = Extract<Block, { kind: 'tool' }>
export type Seg = { type: 'one'; b: Block } | { type: 'tools'; blocks: ToolBlockB[] }
export type Row =
  | { kind: 'leader'; key: 'L'; segs: Seg[] }
  | { kind: 'user'; key: string; b: Extract<Block, { kind: 'user' }> }
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

/*
foldBlocks 裁掉被折叠的 trim 区段：每个折叠态 trim 块吃掉「上一个
trim 之后 → 自己」的全部区块（trim 块自身保留作整理标记，其后的
内容永远可见）。无折叠 trim 时原数组直返（零开销快路径）。
*/
export function foldBlocks(blocks: Block[]): Block[] {
  let anyFolded = false
  const trims: number[] = []
  for (let i = 0; i < blocks.length; i++) {
    const b = blocks[i]
    if (b.kind === 'trim') {
      trims.push(i)
      if (b.folded) anyFolded = true
    }
  }
  if (!anyFolded) return blocks
  const hide = new Set<number>()
  let prev = -1
  for (const ti of trims) {
    if ((blocks[ti] as Extract<Block, { kind: 'trim' }>).folded) {
      for (let i = prev + 1; i < ti; i++) hide.add(i)
    }
    prev = ti
  }
  return blocks.filter((_, i) => !hide.has(i))
}
