/* 画板元素数据：纯 JSON，可随资源 tab 跨窗口搬运 */

export type BoardPoint = { x: number; y: number }

export type BoardElement =
  | { id: number; kind: 'pen' | 'eraser'; color: string; size: number; pts: BoardPoint[] }
  | { id: number; kind: 'rect' | 'ellipse' | 'arrow'; color: string; size: number; a: BoardPoint; b: BoardPoint }
  | { id: number; kind: 'text'; color: string; size: number; x: number; y: number; text: string }

export type BoardData = BoardElement[]
