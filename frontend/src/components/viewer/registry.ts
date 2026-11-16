/*
资源查看器的类型路由（单一来源）：按扩展名判定资源种类，ResourcePane 据此挂载对应 viewer 组件。
*/

import type { Component } from 'svelte'
import type { BoardData } from '../../lib/boardData'
import { fileBaseName, isTextFilePath } from '../../lib/textfile'
import PdfViewer from './viewers/PdfViewer.svelte'
import FallbackViewer from './viewers/FallbackViewer.svelte'
import MediaViewer from './viewers/MediaViewer.svelte'
import ExcelViewer from './viewers/ExcelViewer.svelte'
import DocxViewer from './viewers/DocxViewer.svelte'
import PptxViewer from './viewers/PptxViewer.svelte'

export type ResourceKind = 'image' | 'markdown' | 'html' | 'pdf' | 'text' | 'media' | 'excel' | 'word' | 'ppt' | 'fallback'

/* VIEWERS 统一 props({tab, active})的即挂组件表:二进制类资源(浏览器内
渲染、无可编辑状态)新增类型只加 kindOf 判定+此处一项,ResourcePane 不动。
文本系(单编辑器重绑)与图片(per-tab 画布保活)走 ResourcePane 专属路径。 */
export const VIEWERS: Partial<Record<ResourceKind, Component>> = {
  pdf: PdfViewer,
  media: MediaViewer,
  excel: ExcelViewer,
  word: DocxViewer,
  ppt: PptxViewer,
  fallback: FallbackViewer,
}

/* kindOf 按路径判定资源种类：image 优先（svg 归图片——画板可加载
光栅化）；markdown/html/pdf/excel/word/ppt/媒体 是专属查看器；
其余文本白名单归 text；都不命中走 fallback 提示。 */
export function kindOf(path: string): ResourceKind {
  const base = fileBaseName(path).toLowerCase()
  const i = base.lastIndexOf('.')
  const ext = i > 0 ? base.slice(i + 1) : ''
  if (['png', 'jpg', 'jpeg', 'gif', 'webp', 'bmp', 'svg', 'ico'].includes(ext)) return 'image'
  if (ext === 'md' || ext === 'markdown') return 'markdown'
  if (ext === 'html' || ext === 'htm') return 'html'
  if (ext === 'pdf') return 'pdf'
  if (['xlsx', 'xlsm', 'xls', 'csv', 'ods'].includes(ext)) return 'excel'
  if (ext === 'docx' || ext === 'doc') return 'word'
  if (ext === 'pptx' || ext === 'ppt') return 'ppt'
  if (['mp4', 'webm', 'mov', 'mkv', 'avi', 'm4v', 'mp3', 'wav', 'ogg', 'flac', 'm4a', 'aac'].includes(ext)) return 'media'
  if (isTextFilePath(path)) return 'text'
  return 'fallback'
}

/* ResTab 是资源查看器 tab 的数据单元：所有可编辑类型都遵循「内存编辑 +
   Ctrl+S 写盘」；文本正文与图片画板元素都存这里，跨窗口整体搬运。 */
export interface ResTab {
  key: string // normFileKey(path)
  kind: ResourceKind
  path?: string
  name: string
  /* 文本系字段（image/pdf/fallback 不用） */
  content: string
  saved: string
  loading: boolean
  err: string
  warn: string
  savedAt: string
  /* 最近一次与磁盘对齐时的指纹（ETag，回退 Last-Modified）：轮询基线 */
  diskStamp?: string
  /* 磁盘被外部改过、而本地有未保存修改：内容不被覆盖，保存前弹窗确认 */
  diskChanged?: boolean
  diff?: boolean // 变更对比视图（文本系 + 所在目录是 git 仓库时可用）
  /* 跨窗口回流后递增：图片 tab 据此重挂并按盘重读 */
  rev?: number
  /* 内存草稿底图（dataURL）：画笔钮新建的画板先不落盘，保存时才 stash 成
     tmp 真身并回填 path。可序列化——跨窗口弹出/回流照常搬运 */
  draftSrc?: string
  /* 画板有未保存改动（与文本 dirty 共用关闭确认与 tab 标记） */
  imageDirty?: boolean
  /* 画板元素（跨窗口搬运；保存成功后清空——磁盘图已含这些笔画） */
  board?: BoardData
  /* SVG 另存后的 PNG 路径（原 SVG 不变，后续编辑/重挂都读这张） */
  savePath?: string
  /* HTML 查看器模式：true/未设 = 内嵌浏览器预览，false = 原文编辑 */
  htmlPreview?: boolean
}

export const MAX_BYTES = 2 << 20 // 与后端 maxSaveContent 一致
