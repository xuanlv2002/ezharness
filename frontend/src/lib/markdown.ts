/*
markdown 渲染结果的跨实例 LRU 缓存。

时间线每切一次页面（进设置/记忆再回聊天）都会销毁并重建整棵消息 DOM；
没有缓存时每条历史消息都要重跑 marked.parse + DOMPurify.sanitize，
大会话里这是回聊天页卡顿的主因。缓存按原文做 key（内容不可变），
超过 48KB 的长文不缓存，避免缓存内存与消息本身等比翻倍。
*/

const CACHE_MAX = 120
const CACHE_CHARS = 4 * 1024 * 1024
const CACHE_MAX_ENTRY = 48 * 1024

const cache = new Map<string, string>()
let chars = 0

/* cachedMarkdown 返回 sanitize 后的 HTML；render 只在未命中时调用。 */
export function cachedMarkdown(text: string, render: (src: string) => string): string {
  const hit = cache.get(text)
  if (hit !== undefined) {
    // LRU：命中移到队尾
    cache.delete(text)
    cache.set(text, hit)
    return hit
  }
  const html = render(text)
  if (text.length <= CACHE_MAX_ENTRY) {
    while (cache.size >= CACHE_MAX || chars + text.length > CACHE_CHARS) {
      const oldest = cache.keys().next().value
      if (oldest === undefined) break
      chars -= oldest.length
      cache.delete(oldest)
    }
    cache.set(text, html)
    chars += text.length
  }
  return html
}
