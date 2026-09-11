/*
registry 是资源查看器的类型路由（单一来源）：按扩展名判定资源种类，
ResourcePane 据此挂载对应 viewer 组件。新增资源种类 = 加 kind 判定 +
viewer 组件 + ResourcePane 注册挂载，框架与其余 viewer 不动。
*/

import { fileBaseName, isTextFilePath } from '../../lib/textfile'

export type ResourceKind = 'image' | 'markdown' | 'html' | 'pdf' | 'text' | 'fallback'

/* kindOf 按路径判定资源种类：image 优先（svg 归图片——画板可加载
光栅化）；markdown/html/pdf 是专属查看器；其余文本白名单归 text；
都不命中走 fallback 提示（ppt 等后续逐种补充）。 */
export function kindOf(path: string): ResourceKind {
  const base = fileBaseName(path).toLowerCase()
  const i = base.lastIndexOf('.')
  const ext = i > 0 ? base.slice(i + 1) : ''
  if (['png', 'jpg', 'jpeg', 'gif', 'webp', 'bmp', 'svg', 'ico'].includes(ext)) return 'image'
  if (ext === 'md' || ext === 'markdown') return 'markdown'
  if (ext === 'html' || ext === 'htm') return 'html'
  if (ext === 'pdf') return 'pdf'
  if (isTextFilePath(path)) return 'text'
  return 'fallback'
}

/* ResTab 是资源查看器 tab 的数据单元：文本系（text/markdown/html 原文）
沿用「单编辑器 + tab 状态数据化」——编辑内容/磁盘状态都存这里，
单实例重绑；图片 tab 是 per-tab 实例（画布状态不可数据化）。 */
export interface ResTab {
  key: string // normFileKey(path) 或 'draft'（画板草稿全局唯一）
  kind: ResourceKind
  path?: string // 草稿无真身路径
  name: string
  /* 文本系字段（image/pdf/fallback 不用） */
  content: string
  saved: string
  loading: boolean
  err: string
  warn: string
  savedAt: string
  lastMod: string
  /* 画板草稿字段（仅 kind=image 且无 path） */
  draftSource?: File | null // 续编底图
  draftTag?: string // 来源附件下标（添加到对话时替换原 chip）
  draftSeq?: number // 递增 = 画布重建
}

export const MAX_BYTES = 2 << 20 // 与后端 maxSaveContent 一致
