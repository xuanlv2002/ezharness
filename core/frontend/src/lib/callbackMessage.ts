/*
定时回调投递到会话的 user 消息格式（后端 callback.FormatMessage）：
开头一条 <scheduled_callback> JSON 元数据，换行后是任务正文。解析成功
时时间线渲染成带任务名的回调卡；解析失败按普通用户消息原文显示。
*/

export interface CallbackMessageMeta {
  taskId: string
  title: string
  at: string
}

const OPEN = '<scheduled_callback>'
const CLOSE = '</scheduled_callback>'

export function parseCallbackMessage(text: string): { meta: CallbackMessageMeta; body: string } | null {
  if (!text.startsWith(OPEN)) return null
  const end = text.indexOf(CLOSE, OPEN.length)
  if (end < 0) return null
  try {
    const raw = JSON.parse(text.slice(OPEN.length, end)) as Partial<CallbackMessageMeta>
    if (typeof raw.title !== 'string') return null
    let body = text.slice(end + CLOSE.length)
    if (body.startsWith('\n')) body = body.slice(1)
    return {
      meta: {
        taskId: typeof raw.taskId === 'string' ? raw.taskId : '',
        title: raw.title,
        at: typeof raw.at === 'string' ? raw.at : '',
      },
      body,
    }
  } catch {
    return null
  }
}
